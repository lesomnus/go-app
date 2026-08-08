package core

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/entpage"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/coffee"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/server/bare"
)

type CoffeeServiceServer struct {
	Server
	go_app.CoffeeServiceServer
}

func NewCoffeeServiceServer(s Server) CoffeeServiceServer {
	return CoffeeServiceServer{s, s.Next().Coffee()}
}

func (s Server) Coffee() go_app.CoffeeServiceServer {
	return NewCoffeeServiceServer(s)
}

func (s CoffeeServiceServer) Add(ctx context.Context, req *go_app.CoffeeAddRequest) (*go_app.Coffee, error) {
	if req.GetRoaster() == nil {
		// A Coffee only exists as somebody's.
		return nil, status.Error(codes.InvalidArgument, "roaster: must be given")
	}
	if err := CheckId(req.GetId()); err != nil {
		return nil, err
	}

	alias, err := ParseAlias(req.GetAlias())
	if err != nil {
		return nil, err
	}

	req.SetAlias(alias)
	if req.GetName() == "" {
		req.SetName(alias)
	}

	return s.CoffeeServiceServer.Add(ctx, req)
}

const (
	// PageSize is how many Coffees [CoffeeServiceServer.List] answers with
	// when the request does not say, and PageLimit is the most it will answer
	// with however loudly the request asks.
	PageSize  = 50
	PageLimit = 100

	// FilterLimit is how many filters one List may carry.
	//
	// Each of them is a predicate in the same query, so the request is what
	// says how much of the database to read -- and a request that says "all of
	// it, in one statement" is one somebody sends by accident long before
	// anybody sends it on purpose. Unlike the page size this is refused rather
	// than clamped: asking for more rows than there are is a caller being
	// generous with themselves, and dropping half the filters would answer a
	// question nobody asked.
	FilterLimit = 32
)

// listOrder is how the Coffees come back: oldest first, so that what is
// answered does not depend on the order the database happens to hold the rows
// in, and by identifier where two were made in the same instant.
//
// The identifier is not decoration. A cursor cannot tell two rows apart that
// are equal in every column of the order, so the page after the first of them
// either repeats the second or skips it -- and rows written by one request are
// stamped a moment apart at best. The last column of an order has to be unique,
// and a key always is.
var listOrder = []entpage.Order{
	{Column: coffee.FieldDateCreated},
	{Column: coffee.FieldID},
}

// List answers with the Coffees that match any of the given filters, or with
// every Coffee if there is none, a page at a time.
//
// It is not a CRUD operation, so nothing generates it. **The filtering is meant
// to be rewritten** -- it is the plainest thing that works, and a real one
// filters by what the app is actually asked about. The other two thirds of this
// are not:
//
// The scope, because a hand-written list is exactly the read the generated
// servers do not make, and it has to be in the query rather than over the
// answer -- a list cut short at a limit and filtered afterwards is one that any
// Roaster can push the others out of by making enough Coffees of its own.
//
// And the paging, because it is the same for every entity and is the half that
// is easy to get wrong. It is a keyset and not an offset: the cursor names the
// last row of the page before, so a Coffee added ahead of the page does not
// shift it and a caller reading through never sees a row twice or misses one.
// See `runtime/entpage`.
func (s CoffeeServiceServer) List(ctx context.Context, req *go_app.CoffeeListRequest) (*go_app.CoffeeListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	sc, err := s.Scope()
	if err != nil {
		return nil, err
	}

	// Through the same function the generated reads go through, and not by
	// asking the scope hook: narrowing a read of a Coffee means the wall *and*
	// leaving out the ones that were erased, and a list that reached past it
	// for the hook alone would quietly answer with the erased ones.
	q := db.Coffee.Query()
	if p, err := bare.CoffeeNarrow(ctx, sc, nil); err != nil {
		return nil, err
	} else if p != nil {
		q.Where(p)
	}

	if fs := req.GetFilters(); len(fs) > 0 {
		if len(fs) > FilterLimit {
			return nil, status.Errorf(codes.InvalidArgument,
				"filters: %d of them, and %d is the most one list carries", len(fs), FilterLimit)
		}

		ps := make([]predicate.Coffee, 0, len(fs))
		for i, f := range fs {
			// A filter that names nothing is refused by Pick, which reads the
			// reference and says which part of it was wrong.
			p, err := bare.CoffeePick(f.GetRef())
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(coffee.Or(ps...))
	}

	if v := req.GetAfter(); v != "" {
		var (
			at time.Time
			id uuid.UUID
		)
		if err := entpage.Decode(v, &at, &id); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		p, err := entpage.After(listOrder, []any{at, id})
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "after: %s", err)
		}

		q.Where(p)
	}

	// The Roaster comes with each of them: almost everything that reads a list
	// wants to know whose they are.
	q.WithRoaster()

	// One more than the page, which is how "is there another page" is answered
	// without a second query and without a count. The extra row is dropped
	// before the answer is built; it was only ever asked for to see whether it
	// was there.
	size := entpage.Size(int(req.GetSize()), PageSize, PageLimit)
	us, err := q.
		Order(coffee.ByDateCreated(), coffee.ByID()).
		Limit(size + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}

	more := len(us) > size
	if more {
		us = us[:size]
	}

	items := make([]*go_app.Coffee, len(us))
	for i, u := range us {
		items[i] = u.Proto()
	}

	res := go_app.CoffeeListResponse_builder{Items: items}.Build()
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

func (s CoffeeServiceServer) Patch(ctx context.Context, req *go_app.CoffeePatchRequest) (*go_app.Coffee, error) {
	if req.HasAlias() {
		alias, err := ParseAlias(req.GetAlias())
		if err != nil {
			return nil, err
		}

		req.SetAlias(alias)
	}

	return s.CoffeeServiceServer.Patch(ctx, req)
}

func (s CoffeeServiceServer) Apply(ctx context.Context, req *go_app.CoffeeApplyRequest) (*go_app.Coffee, error) {
	if err := checkAlias(coffeeEntity, req.GetPatch()); err != nil {
		return nil, err
	}

	return s.CoffeeServiceServer.Apply(ctx, req)
}
