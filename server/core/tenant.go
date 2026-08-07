package core

import (
	"context"

	"github.com/lesomnus/z"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/tenant"
	"github.com/lesomnus/go-app/server/bare"
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
	if err := CheckId(req.GetId()); err != nil {
		return nil, err
	}

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

// Erase takes the Holders with it, for real, and then the Tenant.
//
// This is what soft deletion costs at the join between two entities, and it is
// worth reading once. A Holder is erased softly, so `Holder.Erase` leaves the
// row -- and the row holds a foreign key to its Tenant. Without this, erasing a
// Tenant that ever had a Holder would fail on that key, for ever, however many
// Holders had been "erased" first. Soft deletion does not cascade; the entity
// that owns the others has to say what happens to them.
//
// What it says here is what erasing a Tenant already meant: it takes everything
// in it with it. So the Holders are deleted rather than stamped -- there is
// nothing left for them to belong to, and a row kept so that the trail can name
// it is a row kept for a trail that is about a Tenant nobody can reach either.
//
// The two are one transaction, since half of this is worse than neither: the
// Holders gone and the Tenant still there is a Tenant nobody administers.
func (s TenantServiceServer) Erase(ctx context.Context, req *go_app.TenantRef) (*emptypb.Empty, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	k, err := bare.TenantGetKey(ctx, db, req)
	if err != nil {
		// Nothing to erase is not a failure to erase; hand it on and let the
		// server behind answer the way it answers for anything else that is
		// not there.
		if status.Code(err) == codes.NotFound {
			return s.TenantServiceServer.Erase(ctx, req)
		}

		return nil, err
	}

	drv, tx, err := enttx.Begin(ctx, db.Driver())
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	next, err := enttx.Rebind(s.Next(), drv)
	if err != nil {
		return nil, err
	}

	if _, err := db.WithDriver(drv).Holder.Delete().
		Where(holder.HasTenantWith(tenant.IDEQ(k))).
		Exec(ctx); err != nil {
		return nil, err
	}

	v, err := next.Tenant().Erase(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
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

func (s TenantServiceServer) Apply(ctx context.Context, req *go_app.TenantApplyRequest) (*go_app.Tenant, error) {
	if err := checkAlias(tenantEntity, req.GetPatch()); err != nil {
		return nil, err
	}

	return s.TenantServiceServer.Apply(ctx, req)
}
