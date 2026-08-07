package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/lesomnus/z"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/entpage"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/predicate"
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

// tenantListOrder is how the Tenants come back: oldest first, and by identifier
// where two were made in the same instant. See [listOrder] for why the last
// column of an order has to be unique.
var tenantListOrder = []entpage.Order{
	{Column: tenant.FieldDateCreated},
	{Column: tenant.FieldID},
}

// List answers with the Tenants that match any of the given filters, or with
// every Tenant this caller may see if there is none, a page at a time.
//
// For most callers that is one, and the list is worth having anyway. A
// deployment that injects a [gate.Policy] may hand somebody several, and
// without this there is no way for them to ask which -- the wall answers
// NotFound for what is not theirs and says nothing about what is. It is also
// what `Watch` reads its first message from.
//
// The shape is `HolderServiceServer.List`'s, and so are the reasons; see there.
func (s TenantServiceServer) List(ctx context.Context, req *go_app.TenantListRequest) (*go_app.TenantListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	sc, err := s.Scope()
	if err != nil {
		return nil, err
	}

	q := db.Tenant.Query()
	if p, err := bare.TenantNarrow(ctx, sc, nil); err != nil {
		return nil, err
	} else if p != nil {
		q.Where(p)
	}

	if fs := req.GetFilters(); len(fs) > 0 {
		if len(fs) > FilterLimit {
			return nil, status.Errorf(codes.InvalidArgument,
				"filters: %d of them, and %d is the most one list carries", len(fs), FilterLimit)
		}

		ps := make([]predicate.Tenant, 0, len(fs))
		for i, f := range fs {
			p, err := bare.TenantPick(f.GetRef())
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(tenant.Or(ps...))
	}

	if v := req.GetAfter(); v != "" {
		var (
			at time.Time
			id uuid.UUID
		)
		if err := entpage.Decode(v, &at, &id); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		p, err := entpage.After(tenantListOrder, []any{at, id})
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		q.Where(p)
	}

	size := entpage.Size(int(req.GetSize()), PageSize, PageLimit)
	us, err := q.
		Order(tenant.ByDateCreated(), tenant.ByID()).
		Limit(size + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}

	more := len(us) > size
	if more {
		us = us[:size]
	}

	items := make([]*go_app.Tenant, len(us))
	for i, u := range us {
		items[i] = u.Proto()
	}

	res := go_app.TenantListResponse_builder{Items: items}.Build()
	if more {
		last := us[len(us)-1]

		next, err := entpage.Encode(last.DateCreated, last.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "next: %s", err)
		}

		res.SetNext(next)
	}

	return res, nil
}
