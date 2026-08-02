package auth

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/lesomnus/otx/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/frame"
)

// Public reports whether a method is served without asking who is calling.
type Public func(method string) bool

// PublicDefault is what is answered to anyone: whether the server is up, and
// what it offers. Neither says anything about what is in it.
func PublicDefault(method string) bool {
	return strings.HasPrefix(method, "/grpc.health.v1.Health/") ||
		strings.HasPrefix(method, "/grpc.reflection.")
}

// Interceptor works out who is calling and puts it in the frame of the
// request, so that everything behind it can ask rather than work it out again.
//
// A call that already has a frame is left alone. That is a call that did not
// come in over the wire - one server calling another in the same process - and
// it was vouched for when it did come in.
func Interceptor(h Handler, r Resolver, public Public) []grpc.ServerOption {
	if public == nil {
		public = func(string) bool { return false }
	}

	of := func(ctx context.Context, method string) (context.Context, error) {
		if _, ok := frame.From(ctx); ok {
			return ctx, nil
		}

		id, err := h.Handle(ctx)
		if err == nil {
			var actor, err = r.Resolve(ctx, id)
			if err == nil {
				log.From(ctx).DebugContext(ctx, "authenticated",
					slog.String("auth.method", id.Method),
					slog.String("actor.alias", actor.GetAlias()),
					slog.String("actor.tenant", actor.GetTenant().GetAlias()),
				)

				return frame.Into(ctx, frame.New(actor)), nil
			}

			// Fall through: somebody said something and it did not name anyone
			// who is here, which is a bad credential and not a missing one.
			if !errors.Is(err, ErrNoCredential) {
				return nil, err
			}
		} else if !errors.Is(err, ErrNoCredential) {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}

		if public(method) {
			return ctx, nil
		}

		return nil, status.Error(codes.Unauthenticated, "who is asking?")
	}

	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			ctx, err := of(ctx, info.FullMethod)
			if err != nil {
				return nil, err
			}

			return handler(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			ctx, err := of(ss.Context(), info.FullMethod)
			if err != nil {
				return err
			}

			return handler(srv, grpcx.StreamWithContext(ss, ctx))
		}),
	}
}

// Inject says who the caller is on every outgoing call.
func Inject(p Provider) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(func(ctx context.Context, method string, req, res any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
			return invoke(p.Provide(ctx), method, req, res, cc, opts...)
		}),
		grpc.WithChainStreamInterceptor(func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, stream grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
			return stream(p.Provide(ctx), desc, cc, method, opts...)
		}),
	}
}
