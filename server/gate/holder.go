package gate

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
)

type HolderServiceServer struct {
	Server
	go_app.HolderServiceServer
}

func NewHolderServiceServer(s Server) HolderServiceServer {
	return HolderServiceServer{s, s.Next().Holder()}
}

func (s Server) Holder() go_app.HolderServiceServer {
	return NewHolderServiceServer(s)
}

// Add is the one thing about a Holder this layer still says, and it is here
// because it is the one that is not a predicate: the row does not exist yet, so
// there is nothing to narrow. Reading one, changing one and erasing one are all
// [Wall].
//
// The check is a read of the Tenant through the wall rather than a comparison
// against the scope, which costs a query and is worth it. A reference names a
// Tenant by identifier or by alias, and answering "is this one of mine" without
// a query means holding every Tenant in scope in full -- fine while that is the
// caller's own and wrong as soon as it is a list a credential or a policy
// narrowed to. Reading it through the wall asks the same question of the same
// predicate every other read uses, and a Tenant the caller may not see is one
// they cannot add to, without this file knowing why.
func (s HolderServiceServer) Add(ctx context.Context, req *go_app.HolderAddRequest) (*go_app.Holder, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}

	// NotFound and not a refusal, which is the same answer every other read of
	// a Tenant gives and for the same reason: that one exists is itself
	// something a caller who may not see it should not be told. It also gets a
	// Tenant that simply is not there right, which comparing a reference
	// against the scope did not -- whoever administers the deployment may add
	// to any Tenant, so being told "not yours" about one they mistyped was an
	// answer about the wrong thing.
	if _, err := s.Server.Next().Tenant().Get(ctx, go_app.TenantGetRequest_builder{
		Ref: req.GetTenant(),
	}.Build()); err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, errNotFound("Tenant")
		}

		return nil, err
	}

	return s.HolderServiceServer.Add(ctx, req)
}
