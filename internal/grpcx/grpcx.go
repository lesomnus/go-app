// Package grpcx holds the plumbing the app is served with: what every call
// goes through before it reaches a service server.
package grpcx

import (
	"context"

	"github.com/lesomnus/otx"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/stats"
)

// ServerOptions returns the options the app is served with. Every call is
// traced, measured, logged and, if it panics, reported as an error rather than
// taken as a reason to end the process.
//
// The order matters: the log is written outside the recovery so that a call
// that panicked is logged like any other one that failed.
func ServerOptions(ctx context.Context) []grpc.ServerOption {
	opts := []grpc.ServerOption{Inherit(ctx), Otel(ctx)}
	opts = append(opts, Log()...)
	opts = append(opts, Recover()...)

	return opts
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
func Otel(ctx context.Context) grpc.ServerOption {
	ps := otx.Providers(ctx)
	return grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(ps.Tracer()),
		otelgrpc.WithMeterProvider(ps.Meter()),
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
