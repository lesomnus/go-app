package gate

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/frame"
)

// Interceptor works out what a call may see and puts it on the frame, so that
// everything behind reads it rather than working it out again.
//
// It is where `p` is consulted, and it is consulted **once**. The hooks [Wall]
// installs run once per query and a request makes several -- a Get reads a row
// and then an edge, an Apply reads, writes and reads back -- so a policy asked
// from in there would be asked several times for one call, over a network if it
// is the sort that has one. This is the same shape `server/auth` uses: an
// interceptor works out who the caller is, and no layer asks again.
//
// A nil `p` is not a missing piece. The deployment then sees what this app
// shows on its own: everybody sees their own Tenant, and nobody sees more. The
// interface is the seam; see [Policy].
//
// It must be installed behind whatever says who is calling, since it reads the
// actor. A call that arrives with no frame at all -- health, reflection, and
// whatever else is public -- is passed through untouched.
func Interceptor(p Policy) []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			ctx, err := decide(ctx, p, info.FullMethod)
			if err != nil {
				return nil, err
			}

			return handler(ctx, req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			ctx, err := decide(ss.Context(), p, info.FullMethod)
			if err != nil {
				return err
			}

			return handler(srv, grpcx.StreamWithContext(ss, ctx))
		}),
	}
}

// decide asks whatever there is to ask and answers with the context the handler
// is served with.
func decide(ctx context.Context, p Policy, method string) (context.Context, error) {
	f, ok := frame.From(ctx)
	if !ok {
		// Nobody vouched for this, which for a method that got this far means
		// it is one served to anybody. There is nothing to decide and nothing
		// behind it that reads a scope.
		return ctx, nil
	}

	c := Call{Actor: f.Actor, Action: method}
	if p != nil {
		if err := p.May(ctx, c); err != nil {
			return nil, err
		}
	}

	t, err := holds(ctx, p, f, c)
	if err != nil {
		return nil, err
	}

	// The credential last, because it can only take away. A token naming every
	// Tenant, held by somebody who may see one, still sees one.
	return frame.Into(ctx, f.WithScope(t.Meet(f.Grant))), nil
}

// holds is what the Holder may see, before the credential is met with it.
//
// Without a policy it is their own Tenant, and there is no caller it is not.
// **There is deliberately no superuser here** -- nothing compares an identifier
// against a well-known one and answers "everything". A privilege granted by
// being a particular row is one that cannot be revoked, cannot be narrowed and
// does not show up anywhere it is used; it is the sort that is discovered by
// whoever finds the row.
//
// What the deployment itself has to do, it does through a server this wall was
// never installed on -- which is how `core.EnsureRoot` runs before there is
// anybody to be, and is what an operator's path is made of. That capability is
// a server instance somebody had to be handed, rather than a comparison
// anybody can satisfy.
func holds(ctx context.Context, p Policy, f *frame.Frame, c Call) (frame.Tenants, error) {
	if p != nil {
		return p.Where(ctx, c)
	}

	k, err := uuid.FromBytes(f.Tenant().GetId())
	if err != nil {
		// It came out of the database with the actor, so this is the app
		// disagreeing with itself rather than a request being wrong.
		return frame.Nothing, status.Errorf(codes.Internal, "the caller's tenant has no identifier: %s", err)
	}

	return frame.Only(k), nil
}
