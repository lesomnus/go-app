package cmd

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/audit"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/otx/log"
	"github.com/lesomnus/xli"
	"github.com/lesomnus/xli/flg"
	"github.com/lesomnus/z"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func NewCmdServe() *xli.Command {
	return &xli.Command{
		Name:  "serve",
		Brief: "serve the gRPC server",

		Flags: flg.Flags{
			&flg.String{Name: "addr", Brief: "address to listen on"},
			&flg.String{Name: "tls-cert", Brief: "path to TLS certificate file"},
			&flg.String{Name: "tls-key", Brief: "path to TLS private key file"},
			&flg.String{Name: "db-dsn", Brief: "database connection string"},
			&flg.Switch{Name: "db-migrate", Brief: "run auto migration on startup"},
		},

		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			c := use_config.Must(ctx)

			flg.VisitP(cmd, "addr", &c.Server.Addr)
			if flg.VisitP(cmd, "tls-cert", &c.Server.TLS.CertFile) {
				c.Server.TLS.Enabled = true
			}
			flg.VisitP(cmd, "tls-key", &c.Server.TLS.KeyFile)
			flg.VisitP(cmd, "db-dsn", &c.Db.Dsn)
			flg.VisitP(cmd, "db-migrate", &c.Db.Migrate)

			creds, err := c.Server.TLS.Credentials()
			if err != nil {
				return z.Err(err, "load tls credentials")
			}

			db, err := c.Db.Open(ctx)
			if err != nil {
				return z.Err(err, "open database")
			}
			defer db.Close()

			if c.Db.Migrate {
				if err := db.Schema.Create(ctx); err != nil {
					return z.Err(err, "migrate database")
				}
			}

			// Canceled once this command returns, which is what stops the
			// tasks it leaves running in the background.
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			// The server that talks to the database, twice: once as it is, and
			// once with the wall on it.
			//
			// Two things are said to it rather than to the stack, and for the
			// same reason -- both are about the statement that runs. The trail
			// is kept by the servers that do the writing, since every RPC that
			// changes anything has to report itself from inside the
			// transaction that changes it (`server/audit`). The wall is stated
			// by the layer that holds the rules and enforced in the query,
			// since narrowing what a caller may see is a predicate and a
			// predicate belongs in the WHERE (`server/gate`).
			rec := audit.NewRecorder()

			// `sink` has no wall, and the two things below are why. Working out
			// who is calling happens before there is anybody to be walled by,
			// and the root Tenant is put there before anybody exists at all.
			// Going around the wall is a wiring decision that should be
			// readable in one place rather than a rule that quietly opens up
			// whenever nobody is asking.
			sink, err := bare.NewServer(db.Client, bare.WithRecorder(rec))
			if err != nil {
				return z.Err(err, "build the server that talks to the database")
			}

			walled, err := bare.NewServer(db.Client, bare.WithRecorder(rec), bare.WithScope(gate.Wall()))
			if err != nil {
				return z.Err(err, "build the server that talks to the database")
			}

			s, err := go_app.Build(walled, core.Build(), audit.Build(), gate.Build())
			if err != nil {
				return z.Err(err, "build server")
			}

			// Before anything is served, and around the gate rather than
			// through it: there is nobody to be yet.
			if _, err := core.EnsureRoot(ctx, core.NewServer(sink)); err != nil {
				return z.Err(err, "ensure the root tenant")
			}

			auth_opts, err := c.Auth.GrpcOptions(sink)
			if err != nil {
				return z.Err(err, "build authentication")
			}

			opts := grpcx.ServerOptions(ctx, c.Server.CallTimeout())
			opts = append(opts, c.Server.GrpcOptions()...)
			opts = append(opts, auth_opts...)
			opts = append(opts, grpcx.Closed(c.Server.Closed())...)
			opts = append(opts, grpc.Creds(creds))

			srv := grpc.NewServer(opts...)
			go_app.RegisterServer(srv, s)

			health_srv := health.NewServer()
			grpc_health_v1.RegisterHealthServer(srv, health_srv)

			if c.Server.ServesReflection() {
				reflection.Register(srv)
			}

			// The process is here, and stays here until it is shut down. See
			// cmd/health.go for why this is not the same answer as the one
			// below it.
			health_srv.SetServingStatus(ServiceLiveness, grpc_health_v1.HealthCheckResponse_SERVING)

			// The app has nothing to serve without the database it runs on.
			go watchDb(ctx, health_srv, db, dbCheckInterval)

			lis, err := net.Listen("tcp", c.Server.Addr)
			if err != nil {
				return z.Err(err, "listen")
			}

			l := log.From(ctx)
			if c.Auth.Believes() {
				l.Warn("callers are believed when they say who they are",
					slog.String("auth", "plain"),
				)
			}
			l.Info("serving grpc",
				slog.String("addr", lis.Addr().String()),
				slog.Bool("tls", c.Server.TLS.Active()),
				// In the order they are tried, since which one answers first
				// is the whole of what a fallback means.
				slog.Any("auth", c.Auth.Methods),
			)

			serve_err := make(chan error, 1)
			go func() { serve_err <- srv.Serve(lis) }()

			sig := make(chan os.Signal, 2)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(sig)

			// Wait for the server to fail, the context to be canceled, or an
			// interrupt/termination signal.
			select {
			case err := <-serve_err:
				return z.ErrIf(err, "serve")
			case <-ctx.Done():
			case <-sig:
			}

			// Stop accepting new connections and let in-flight RPCs finish.
			l.Info("shutting down")
			cancel()
			// Tell whoever is watching to send the traffic elsewhere.
			health_srv.Shutdown()
			go srv.GracefulStop()

			// A second signal forces the server to stop immediately.
			select {
			case err := <-serve_err:
				return z.ErrIf(err, "serve")
			case <-sig:
				l.Warn("force stop")
				srv.Stop()
				<-serve_err
				return nil
			}
		}),
	}
}
