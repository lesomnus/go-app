package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/entpage"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/coffee"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/internal/ent/roaster"
	"github.com/lesomnus/go-app/server/bare"
)

type RoasterServiceServer struct {
	Server
	go_app.RoasterServiceServer
}

func NewRoasterServiceServer(s Server) RoasterServiceServer {
	return RoasterServiceServer{s, s.Next().Roaster()}
}

func (s Server) Roaster() go_app.RoasterServiceServer {
	return NewRoasterServiceServer(s)
}

func (s RoasterServiceServer) Add(ctx context.Context, req *go_app.RoasterAddRequest) (*go_app.Roaster, error) {
	if err := CheckId(req.GetId()); err != nil {
		return nil, err
	}

	alias, err := ParseAlias(req.GetAlias())
	if err != nil {
		return nil, err
	}

	req.SetAlias(alias)
	if req.GetName() == "" {
		// A Roaster is displayed by its name, so make one up rather than
		// showing nothing.
		req.SetName(alias)
	}

	return s.RoasterServiceServer.Add(ctx, req)
}

// Erase takes the Coffees with it, for real, and then the Roaster.
//
// This is what soft deletion costs at the join between two entities, and it is
// worth reading once. A Coffee is erased softly, so `Coffee.Erase` leaves the
// row -- and the row holds a foreign key to its Roaster. Without this, erasing
// a Roaster that ever had a Coffee would fail on that key, for ever, however
// many Coffees had been "erased" first. **Soft deletion does not cascade**, and
// a foreign key does not care that a row is "gone"; the entity that owns the
// others has to say what happens to them.
//
// What it says here is what erasing a Roaster already meant: it takes
// everything of theirs with it. The Coffees are deleted rather than stamped --
// an identifier is kept taken so that nothing points at the wrong coffee, and
// there is no coffee to point at once the Roaster is gone either.
//
// The two are one transaction, since half of this is worse than neither.
func (s RoasterServiceServer) Erase(ctx context.Context, req *go_app.RoasterRef) (*emptypb.Empty, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	k, err := bare.RoasterGetKey(ctx, db, req)
	if err != nil {
		// Nothing to erase is not a failure to erase; hand it on and let the
		// server behind answer the way it answers for anything else that is
		// not there.
		if status.Code(err) == codes.NotFound {
			return s.RoasterServiceServer.Erase(ctx, req)
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

	if _, err := db.WithDriver(drv).Coffee.Delete().
		Where(coffee.HasRoasterWith(roaster.IDEQ(k))).
		Exec(ctx); err != nil {
		return nil, err
	}

	v, err := next.Roaster().Erase(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return v, nil
}

func (s RoasterServiceServer) Patch(ctx context.Context, req *go_app.RoasterPatchRequest) (*go_app.Roaster, error) {
	if req.HasAlias() {
		alias, err := ParseAlias(req.GetAlias())
		if err != nil {
			return nil, err
		}

		req.SetAlias(alias)
	}

	return s.RoasterServiceServer.Patch(ctx, req)
}

func (s RoasterServiceServer) Apply(ctx context.Context, req *go_app.RoasterApplyRequest) (*go_app.Roaster, error) {
	if err := checkAlias(roasterEntity, req.GetPatch()); err != nil {
		return nil, err
	}

	return s.RoasterServiceServer.Apply(ctx, req)
}

// roasterListOrder is how the Roasters come back: oldest first, and by identifier
// where two were made in the same instant. See [listOrder] for why the last
// column of an order has to be unique.
var roasterListOrder = []entpage.Order{
	{Column: roaster.FieldDateCreated},
	{Column: roaster.FieldID},
}

// List answers with the Roasters that match any of the given filters, or with
// every Roaster this caller may see if there is none, a page at a time.
//
// For most callers that is one, and the list is worth having anyway. A
// deployment that injects a [gate.Policy] may hand somebody several, and
// without this there is no way for them to ask which -- the wall answers
// NotFound for what is not theirs and says nothing about what is. It is also
// what `Watch` reads its first message from.
//
// The shape is `HolderServiceServer.List`'s, and so are the reasons; see there.
func (s RoasterServiceServer) List(ctx context.Context, req *go_app.RoasterListRequest) (*go_app.RoasterListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	sc, err := s.Scope()
	if err != nil {
		return nil, err
	}

	q := db.Roaster.Query()
	if p, err := bare.RoasterNarrow(ctx, sc, nil); err != nil {
		return nil, err
	} else if p != nil {
		q.Where(p)
	}

	if fs := req.GetFilters(); len(fs) > 0 {
		if len(fs) > FilterLimit {
			return nil, status.Errorf(codes.InvalidArgument,
				"filters: %d of them, and %d is the most one list carries", len(fs), FilterLimit)
		}

		ps := make([]predicate.Roaster, 0, len(fs))
		for i, f := range fs {
			p, err := bare.RoasterPick(f.GetRef())
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(roaster.Or(ps...))
	}

	if v := req.GetAfter(); v != "" {
		var (
			at time.Time
			id uuid.UUID
		)
		if err := entpage.Decode(v, &at, &id); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		p, err := entpage.After(roasterListOrder, []any{at, id})
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		q.Where(p)
	}

	size := entpage.Size(int(req.GetSize()), PageSize, PageLimit)
	us, err := q.
		Order(roaster.ByDateCreated(), roaster.ByID()).
		Limit(size + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}

	more := len(us) > size
	if more {
		us = us[:size]
	}

	items := make([]*go_app.Roaster, len(us))
	for i, u := range us {
		items[i] = u.Proto()
	}

	res := go_app.RoasterListResponse_builder{Items: items}.Build()
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
