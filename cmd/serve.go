package cmd

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

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
		},

		Handler: xli.OnRun(func(ctx context.Context, cmd *xli.Command, next xli.Next) error {
			c := use_config.Must(ctx)

			flg.VisitP(cmd, "addr", &c.Server.Addr)
			if flg.VisitP(cmd, "tls-cert", &c.Server.TLS.CertFile) {
				c.Server.TLS.Enabled = true
			}
			flg.VisitP(cmd, "tls-key", &c.Server.TLS.KeyFile)

			creds, err := c.Server.TLS.Credentials()
			if err != nil {
				return z.Err(err, "load tls credentials")
			}

			srv := grpc.NewServer(grpc.Creds(creds))

			// Register application services here as they are generated, e.g.:
			//   go_app.RegisterUserServiceServer(srv, userService)

			health_srv := health.NewServer()
			grpc_health_v1.RegisterHealthServer(srv, health_srv)
			reflection.Register(srv)

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
