package core

import (
	"context"

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

	return s.TenantServiceServer.Add(ctx, req)
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
