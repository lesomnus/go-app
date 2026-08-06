package gate

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
)

type TenantServiceServer struct {
	Server
	go_app.TenantServiceServer
}

func NewTenantServiceServer(s Server) TenantServiceServer {
	return TenantServiceServer{s, s.Next().Tenant()}
}

func (s Server) Tenant() go_app.TenantServiceServer {
	return NewTenantServiceServer(s)
}

// Add is for whoever administers the deployment. A Tenant is the wall every
// other rule is about, so putting up another one is not something that happens
// from inside one.
//
// It is here rather than in [Wall] for the same reason [HolderServiceServer.Add]
// is: the row does not exist yet, and a caller who may see nothing is not the
// same as a caller who may create nothing.
func (s TenantServiceServer) Add(ctx context.Context, req *go_app.TenantAddRequest) (*go_app.Tenant, error) {
	if err := administers(ctx, "add a tenant to this deployment"); err != nil {
		return nil, err
	}

	return s.TenantServiceServer.Add(ctx, req)
}

// Erase, likewise, and it takes everything in it with it.
//
// Unlike Add this one *could* be a predicate -- a Tenant that is not the
// caller's own is already out of scope, so the erase would quietly erase
// nothing. That is the wrong answer here. Taking down the Tenant you are
// standing in is a mistake worth being told about rather than one that silently
// succeeds, so it is refused by name.
func (s TenantServiceServer) Erase(ctx context.Context, req *go_app.TenantRef) (*emptypb.Empty, error) {
	if err := administers(ctx, "erase a tenant"); err != nil {
		return nil, err
	}

	return s.TenantServiceServer.Erase(ctx, req)
}

// administers answers nil for whoever administers the deployment, which is the
// caller no wall is about, and refuses `what` to everybody else.
func administers(ctx context.Context, what string) error {
	if _, err := actor(ctx); err != nil {
		return err
	}

	t, err := Scope(ctx)
	if err != nil {
		return err
	}
	if !t.All() {
		return errForbidden(what)
	}

	return nil
}
