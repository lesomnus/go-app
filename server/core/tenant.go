package core

import (
	"context"

	"github.com/lesomnus/z"

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

func (s TenantServiceServer) Add(ctx context.Context, req *go_app.TenantAddRequest) (*go_app.Tenant, error) {
	alias, err := ParseAlias(req.GetAlias())
	if err != nil {
		return nil, err
	}

	req.SetAlias(alias)
	if req.GetName() == "" {
		// A Tenant is always displayed by its name, so make one up rather than
		// showing nothing.
		req.SetName(alias)
	}

	v, err := s.TenantServiceServer.Add(ctx, req)
	if err != nil {
		return nil, err
	}

	// A Tenant with nobody in it is a Tenant nobody can do anything with, so
	// it comes with the Holder that administers it.
	_, err = s.Next().Holder().Add(ctx, go_app.HolderAddRequest_builder{
		Tenant: v.Ref(),
		Alias:  AdminAlias,
		Name:   "Admin",
	}.Build())
	if err != nil {
		// The two writes are not one, so the half that was written is taken
		// back rather than left behind.
		if _, err := s.Next().Tenant().Erase(ctx, v.Ref()); err != nil {
			return nil, z.Err(err, "add the admin holder, and the tenant could not be taken back either")
		}

		return nil, z.Err(err, "add the admin holder")
	}

	return v, nil
}

func (s TenantServiceServer) Patch(ctx context.Context, req *go_app.TenantPatchRequest) (*go_app.Tenant, error) {
	if req.HasAlias() {
		alias, err := ParseAlias(req.GetAlias())
		if err != nil {
			return nil, err
		}

		req.SetAlias(alias)
	}

	return s.TenantServiceServer.Patch(ctx, req)
}
