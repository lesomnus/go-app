package gate

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/frame"
)

// Anonymous names the RPCs a caller nobody vouched for may make.
//
// A closed list of what is *allowed*. The other way round -- naming the writes
// and letting everything else through -- reads the same today and is wrong the
// day somebody writes `Rename`: it is a write, it is not called `Patch`, and it
// would have been open to everybody with nothing anywhere to say so.
//
// Nil is none, which is the answer to give when nobody has thought about it.
type Anonymous func(method string) bool

// AnonymousReads is the shape most deployments want: a catalogue anybody may
// read and only a caller who said who they are may change.
//
// It matches **by suffix**, so it names every RPC called `Get`, `List` or
// `Watch` -- the ones this app generates, and any written by hand that happen
// to be spelled that way.
//
// That last part is the trap, and it is worth stating rather than discovering.
// `List` is exactly the RPC [EXTENDING.md] tells somebody to write by hand, and
// a hand-written one is usually hand-written *because* it does something a
// generated read does not -- narrowing to the caller's own rows, most often. It
// is opened by this along with the rest, and no reading of the name would
// suggest that.
//
// So this is a convenience for an app whose reads really are a catalogue. An
// app where a read means something is better off naming its reads, which is
// what [AnonymousOnly] is for:
//
//	gate.WithAnonymous(gate.AnonymousOnly(
//		go_app.CoffeeService_Get_FullMethodName,
//		go_app.CoffeeService_List_FullMethodName,
//	))
//
// [EXTENDING.md]: https://github.com/lesomnus/go-app/blob/kind/server/docs/EXTENDING.md
func AnonymousReads(method string) bool {
	for _, v := range []string{"/Get", "/List", "/Watch"} {
		if strings.HasSuffix(method, v) {
			return true
		}
	}

	return false
}

// AnonymousOnly names the RPCs exactly, and is the one to reach for when being
// wrong is expensive.
//
// The generated `..._FullMethodName` constants are what to pass, so an RPC that
// is renamed is a compile error here rather than a call that quietly stops
// being allowed -- or quietly starts.
func AnonymousOnly(methods ...string) Anonymous {
	allowed := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		allowed[m] = struct{}{}
	}

	return func(method string) bool {
		_, ok := allowed[method]
		return ok
	}
}

// Option adjusts what [Interceptor] decides.
type Option func(*options)

type options struct {
	policy    Policy
	anonymous Anonymous
}

// WithPolicy has `p` consulted about every call. See [Policy].
func WithPolicy(p Policy) Option {
	return func(o *options) { o.policy = p }
}

// WithAnonymous lets a caller nobody vouched for make the calls `f` names.
// Unset is none of them.
func WithAnonymous(f Anonymous) Option {
	return func(o *options) { o.anonymous = f }
}

// Interceptor decides what a call may do, once, in front of the handler.
//
// **Once** is the point. A request makes several queries and would ask several
// times; a policy asked from in there would be asked several times for one
// call, over a network if it is the sort that has one. This is the same shape
// `server/auth` uses: an interceptor works out who the caller is, and no layer
// asks again.
//
// It must be installed behind whatever says who is calling, since it reads the
// actor. A call that arrives with no frame at all is one that did not come in
// over the wire -- the app calling itself -- and is passed through untouched.
func Interceptor(opts ...Option) []grpc.ServerOption {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}

	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			if err := o.decide(ctx, info.FullMethod); err != nil {
				return nil, err
			}

			return handler(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			ctx := ss.Context()
			if err := o.decide(ctx, info.FullMethod); err != nil {
				return err
			}

			return handler(srv, grpcx.StreamWithContext(ss, ctx))
		}),
	}
}

// decide answers with what the caller is told, and nil for a call that may go
// on.
func (o options) decide(ctx context.Context, method string) error {
	f, ok := frame.From(ctx)
	if !ok {
		// Nobody vouched for this and nobody was asked to: it did not come in
		// over the wire. The app calling itself is not a caller.
		return nil
	}

	if f.Actor.IsAnonymous() && !o.allowsAnonymous(method) {
		// Unauthenticated and not PermissionDenied, because the two say
		// different things to do about it: this one is fixed by saying who you
		// are, and a caller who has is told the other.
		return status.Error(codes.Unauthenticated, "who is asking?")
	}

	if o.policy != nil {
		return o.policy.May(ctx, Call{Actor: f.Actor, Action: method})
	}

	return nil
}

func (o options) allowsAnonymous(method string) bool {
	if o.anonymous == nil {
		return false
	}

	return o.anonymous(method)
}
