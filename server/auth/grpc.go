package auth

import (
	"context"
	"errors"
	"log/slog"

	"github.com/lesomnus/otx/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/frame"
)

// Interceptor works out who is calling and puts it in the frame of the
// request, so that everything behind it can ask rather than work it out again.
//
// **A request that carries no credential is not refused.** It is served as
// [frame.Anonymous], and what an anonymous caller may do is `server/gate`'s to
// say. So every request behind this has a frame, which is what keeps this app
// from having the case that has no good answer -- see the note at the top of
// `server/frame`.
//
// A credential that is *there and wrong* is refused, and that is a different
// thing entirely. Somebody presented something; serving them as nobody would be
// answering a question they did not ask.
//
// A call that already has a frame is left alone. That is a call that did not
// come in over the wire - one server calling another in the same process - and
// it was vouched for when it did come in.
func Interceptor(h Handler) []grpc.ServerOption {
	of := func(ctx context.Context, method string) (context.Context, error) {
		if _, ok := frame.From(ctx); ok {
			return ctx, nil
		}

		id, err := h.Handle(ctx)
		if err != nil {
			if !errors.Is(err, ErrNoCredential) {
				return nil, statusOf(err)
			}

			// Nobody said anything, which is a caller and not an error.
			return frame.Into(ctx, frame.Nobody()), nil
		}

		// Who called what, for every RPC there is -- the reads that leave no
		// other trace included. Which RPC is not said here: `grpcx.Log` puts
		// the service and the method on the logger every line of a call is
		// written with, so this one carries them without asking, as does
		// everything a handler writes.
		log.From(ctx).DebugContext(ctx, "authenticated",
			slog.String("auth.method", id.Method),
			slog.String("actor.subject", id.Actor.Subject),
		)

		// What the credential allows is checked here, once, and not by whatever
		// is about to run. It is not a rule about the caller -- `server/gate`
		// holds those, and this narrows whatever it decides -- it is the
		// credential saying it was not made for this, which is a question about
		// the request and not about the row it is going to touch.
		if !id.Grant.Allows(method) {
			return nil, status.Errorf(codes.PermissionDenied,
				"%s: this credential is not for that", method)
		}

		return frame.Into(ctx, frame.New(id.Actor, id.Grant)), nil
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

// statusOf answers a handler's refusal with the code that says what the caller
// should do about it.
//
// Unauthenticated means "that credential is no good", and a caller who is told
// it throws the credential away and goes to get another one. A handler that
// could not reach the thing it asks must not say that: it would send every
// caller at once to an issuer that is, by assumption, already having a bad
// day, and each of them would discard a token that was never wrong.
func statusOf(err error) error {
	if s, ok := status.FromError(err); ok {
		return s.Err()
	}
	if errors.Is(err, ErrUnavailable) {
		return status.Error(codes.Unavailable, err.Error())
	}

	return status.Error(codes.Unauthenticated, err.Error())
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
