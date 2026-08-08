package cmd

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/internal/httpx"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/go-app/server/spin"
	"github.com/lesomnus/go-app/server/watch"
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
	// httpHeaderTimeout is how long the second listener waits for a request's
	// headers. Without it a connection that says nothing holds a goroutine for
	// as long as it likes, which is what `grpc.Server` caps on its own and
	// `http.Server` does not.
	httpHeaderTimeout = 10 * time.Second

	// httpShutdownGrace is how long it is given to finish what it is serving.
	httpShutdownGrace = 5 * time.Second
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

			// And the other end of the same hook: what a call changed, told to
			// whoever is listening once the call has answered.
			//
			// Nothing in this binary listens. An app made from this template
			// subscribes here --
			//
			//	c, stop := events.Subscribe(64)
			//	go func() {
			//		defer stop()
			//		for v := range c { ... }
			//	}()
			//
			// -- or deletes both of these. See `server/watch`.
			events := watch.Signal()
			wat := watch.New(events)

			// The server that talks to the database.
			//
			// What a write reports is said to it rather than to the stack, and
			// for a reason worth knowing: every RPC that changes anything has
			// to report itself from inside the transaction that changes it,
			// and only the server that runs the statement is inside one.
			sink, err := bare.NewServer(db.Client, bare.WithRecorder(wat.Recorder()))
			if err != nil {
				return z.Err(err, "build the server that talks to the database")
			}

			// Watch is behind the gate, so a caller who may not ask has already
			// been refused, and in front of core, so the list it reads is the
			// hand-written one with its filters and its paging.
			s, err := go_app.Build(sink, core.Build(), wat.Build(), gate.Build())
			if err != nil {
				return z.Err(err, "build server")
			}

			// What the layers have to do besides answering requests, started
			// before anything is served and stopped before the process goes.
			// Nothing in this app spins; see `server/spin`.
			spun := make(chan struct{})
			defer func() { cancel(); <-spun }()
			go func() {
				defer close(spun)
				spin.All(ctx, s)
			}()

			auth_opts, err := c.Auth.GrpcOptions()
			if err != nil {
				return z.Err(err, "build authentication")
			}

			opts := grpcx.ServerOptions(ctx, grpcx.WithDeadline(c.Server.CallTimeout()))
			opts = append(opts, c.Server.GrpcOptions()...)
			opts = append(opts, auth_opts...)
			// Behind the authentication too, since what a call is counted
			// against is who is making it, and in front of everything below,
			// since a caller over their line should not be able to ask for the
			// work of deciding what they may see. Nothing unless
			// `server.limit.rate` says so.
			opts = append(opts, grpcx.Limit(c.Server.Limiter(), gate.BySubject())...)
			// Behind the authentication, since it reads who the caller is. What
			// an anonymous caller may do is said here and nowhere else; a
			// deployment that has more to say injects a `gate.Policy`, and
			// nothing is injected here.
			opts = append(opts, gate.Interceptor(c.Server.GateOptions()...)...)
			// Outside the handler and inside everything that could refuse the
			// call, so that what is published is what was served.
			opts = append(opts, wat.Interceptor()...)
			opts = append(opts, grpcx.Closed(c.Server.Closed())...)
			opts = append(opts, grpc.Creds(creds))

			srv := grpc.NewServer(opts...)
			go_app.RegisterServer(srv, s)

			health_srv := health.NewServer()
			grpc_health_v1.RegisterHealthServer(srv, health_srv)

			if c.Server.AllowReflection {
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
			served := []any{
				slog.String("addr", lis.Addr().String()),
				slog.Bool("tls", c.Server.TLS.Active()),
				// In the order they are tried, since which one answers first
				// is the whole of what a fallback means.
				slog.Any("auth", c.Auth.Methods),
			}
			if lim := c.Server.Limit; lim.Limits() {
				// Said here rather than left to whoever reads the file, since
				// a refused call is otherwise a mystery to whoever is holding
				// the log of it.
				served = append(served,
					slog.Float64("limit.rate", lim.Rate),
					slog.Int("limit.burst", lim.BurstOr()),
				)
			}
			l.Info("serving grpc", served...)

			serve_err := make(chan error, 1)
			go func() { serve_err <- srv.Serve(lis) }()

			// The second listener, if there is one: grpc-web translated into
			// the same gRPC server, and whatever a deployment serves over
			// ordinary HTTP. It is a listener of its own rather than one port
			// serving both, since gRPC through `net/http` gives up the
			// transport gRPC brings; see `internal/httpx`.
			var web *http.Server
			if h := c.Server.Http; h.Serves() {
				opts := httpx.Options{
					Health: health_srv,
					Pprof:  h.AllowPprof,
				}
				if h.AllowGrpcWeb {
					opts.Grpc = srv
					opts.Origins = h.Origin()
				}

				web = &http.Server{
					Addr:              h.Addr,
					Handler:           httpx.Handler(opts),
					ReadHeaderTimeout: httpHeaderTimeout,
				}

				l.Info("serving http",
					slog.String("addr", h.Addr),
					slog.Bool("grpc_web", h.AllowGrpcWeb),
					slog.Bool("pprof", h.AllowPprof),
				)
				go func() {
					if err := web.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
						serve_err <- z.Err(err, "serve http")
					}
				}()
			}

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
			if web != nil {
				// Given a moment of its own: it is the listener a probe is on,
				// and one that is refused reads as a process that died rather
				// than one that is going.
				ctx, done := context.WithTimeout(context.WithoutCancel(ctx), httpShutdownGrace)
				defer done()

				if err := web.Shutdown(ctx); err != nil {
					l.Warn("http did not shut down cleanly", slog.String("error", err.Error()))
				}
			}
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
