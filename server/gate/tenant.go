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
func (s TenantServiceServer) Add(ctx context.Context, req *go_app.TenantAddRequest) (*go_app.Tenant, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) {
		return nil, errForbidden("add a tenant to this deployment")
	}

	return s.TenantServiceServer.Add(ctx, req)
}

// Erase, likewise, and it takes everything in it with it.
func (s TenantServiceServer) Erase(ctx context.Context, req *go_app.TenantRef) (*emptypb.Empty, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) {
		return nil, errForbidden("erase a tenant")
	}

	return s.TenantServiceServer.Erase(ctx, req)
}

func (s TenantServiceServer) Get(ctx context.Context, req *go_app.TenantGetRequest) (*go_app.Tenant, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) && !names(f, req.GetRef()) {
		return nil, errNotFound("Tenant")
	}

	return s.TenantServiceServer.Get(ctx, req)
}

func (s TenantServiceServer) Patch(ctx context.Context, req *go_app.TenantPatchRequest) (*go_app.Tenant, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) && !names(f, req.GetRef()) {
		return nil, errNotFound("Tenant")
	}

	return s.TenantServiceServer.Patch(ctx, req)
}

// Apply is Patch by another road: what it may change is stated in a document
// rather than in the request, but it is about the same Tenant, named the same
// way, so it is the same wall. What the document says is for the server behind
// this one; whether it may be said at all is settled here.
func (s TenantServiceServer) Apply(ctx context.Context, req *go_app.TenantApplyRequest) (*go_app.Tenant, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) && !names(f, req.GetRef()) {
		return nil, errNotFound("Tenant")
	}

	return s.TenantServiceServer.Apply(ctx, req)
}
