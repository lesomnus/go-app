package cmd

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lesomnus/go-app/cmd/config"
	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/otx/log"
	"github.com/lesomnus/xli"
	"github.com/lesomnus/xli/flg"
	"github.com/lesomnus/z"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const (
	// dbCheckInterval is how often the database is asked whether it is still
	// there, and dbCheckTimeout is how long it is given to answer.
	dbCheckInterval = 5 * time.Second
	dbCheckTimeout  = 3 * time.Second
)

// watchDb reports the app as serving for as long as the database answers. The
// empty service name stands for the server as a whole, which is what a load
// balancer or a container runtime asks about.
func watchDb(ctx context.Context, h *health.Server, db *config.Db) {
	const service = ""

	t := time.NewTicker(dbCheckInterval)
	defer t.Stop()

	for {
		// The status is not ours to set once the server is shutting down.
		if s := dbStatus(ctx, db); ctx.Err() == nil {
			h.SetServingStatus(service, s)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// dbStatus is SERVING for as long as the database answers.
func dbStatus(ctx context.Context, db *config.Db) grpc_health_v1.HealthCheckResponse_ServingStatus {
	ctx, cancel := context.WithTimeout(ctx, dbCheckTimeout)
	defer cancel()

	if err := db.Ping(ctx); err != nil {
		log.From(ctx).WarnContext(ctx, "database is not reachable", slog.String("error", err.Error()))
		return grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	return grpc_health_v1.HealthCheckResponse_SERVING
}

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

			s := core.New(db.Client)
			if _, err := core.EnsureRoot(ctx, s); err != nil {
				return z.Err(err, "ensure the root tenant")
			}

			opts := grpcx.ServerOptions(ctx)
			opts = append(opts, c.Server.GrpcOptions()...)
			opts = append(opts, grpc.Creds(creds))

			srv := grpc.NewServer(opts...)
			go_app.RegisterServer(srv, s)

			health_srv := health.NewServer()
			grpc_health_v1.RegisterHealthServer(srv, health_srv)

			if c.Server.ServesReflection() {
				reflection.Register(srv)
			}

			// The app is no healthier than the database it runs on.
			go watchDb(ctx, health_srv, db)

			lis, err := net.Listen("tcp", c.Server.Addr)
			if err != nil {
				return z.Err(err, "listen")
			}

			l := log.From(ctx)
			l.Info("serving grpc",
				slog.String("addr", lis.Addr().String()),
				slog.Bool("tls", c.Server.TLS.Active()),
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
