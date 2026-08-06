// Package grpcx holds the plumbing the app is served with: what every call
// goes through before it reaches a service server.
package grpcx

import (
	"context"
	"time"

	"github.com/lesomnus/otx"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

// ServerOptions returns the options the app is served with. Every call is
// traced, measured, logged, given a deadline if it brought none, checked
// against what its own definition says about it and, if it panics, reported as
// an error rather than taken as a reason to end the process.
//
// It fails when the `buf.validate` constraints in the schema do not compile,
// which is a deployment that does not start rather than one that serves
// unchecked requests.
//
// The order matters. The log is written outside the recovery so that a call
// that panicked is logged like any other one that failed, and outside the
// deadline so that a call that ran out of time is logged as such rather than
// not at all. Validation is inside both, and before the handler, so a refused
// request never reaches a server.
func ServerOptions(ctx context.Context, timeout time.Duration) ([]grpc.ServerOption, error) {
	validate, err := Validate()
	if err != nil {
		return nil, err
	}

	opts := []grpc.ServerOption{Inherit(ctx), Otel(ctx)}
	opts = append(opts, Log()...)
	opts = append(opts, Recover()...)
	opts = append(opts, Deadline(timeout)...)
	opts = append(opts, validate...)

	return opts, nil
}

// Inherit hands the telemetry of `ctx` over to every call. gRPC builds the
// context of a call out of a background one, so without this a handler is
// served with nothing of what the app was started with and everything it
// logs goes nowhere.
func Inherit(ctx context.Context) grpc.ServerOption {
	o := otx.From(ctx)
	return grpc.StatsHandler(ctxHandler{func(ctx context.Context) context.Context {
		return otx.Into(ctx, o)
	}})
}

// ctxHandler is a [stats.Handler] that does nothing but seed the context of a
// call, which is the only place gRPC lets one do so.
type ctxHandler struct {
	f func(ctx context.Context) context.Context
}

func (h ctxHandler) TagRPC(ctx context.Context, _ *stats.RPCTagInfo) context.Context {
	return h.f(ctx)
}

func (h ctxHandler) TagConn(ctx context.Context, _ *stats.ConnTagInfo) context.Context {
	return ctx
}

func (ctxHandler) HandleRPC(context.Context, stats.RPCStats) {}

func (ctxHandler) HandleConn(context.Context, stats.ConnStats) {}

// Otel instruments the calls with the providers held by `ctx`.
//
// The propagator comes from `ctx` as well, and it is what continues the trace
// a caller started rather than beginning one of its own. Note that this is the
// caller's word: see the note on trust in README.md before putting the server
// where anyone can reach it.
func Otel(ctx context.Context) grpc.ServerOption {
	ps := otx.Providers(ctx)
	return grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(ps.Tracer()),
		otelgrpc.WithMeterProvider(ps.Meter()),
		// Without this the global propagator is used, which is an empty
		// composite unless something set it, so every call would start a trace
		// of its own and nothing would ever join up.
		otelgrpc.WithPropagators(otx.Propagator(ctx)),
	))
}

// serverStream carries a context of its own, since a [grpc.ServerStream] hands
// over the one it was made with.
type serverStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s serverStream) Context() context.Context {
	return s.ctx
}

// StreamWithContext returns `ss` with `ctx` as its context.
func StreamWithContext(ss grpc.ServerStream, ctx context.Context) grpc.ServerStream {
	if ss.Context() == ctx {
		return ss
	}

	return serverStream{ss, ctx}
}
