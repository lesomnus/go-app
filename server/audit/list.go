package audit

import (
	"context"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/entpage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/audit"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/server/bare"
)

const (
	// PageSize is how many rows [AuditServiceServer.List] answers with when the
	// request does not say, and PageLimit is the most it will answer with
	// however loudly the request asks.
	PageSize  = 50
	PageLimit = 100

	// FilterLimit is how many filters one List may carry; see
	// [core.FilterLimit] for why it is refused rather than clamped.
	FilterLimit = 32
)

// listOrder is how the trail is read: newest first, which is the opposite of
// the lists elsewhere in this app and is the point -- what a trail is asked is
// what happened, and the answer starts with what happened last.
//
// The identifier settles a tie, and it is not decoration. Two rows of one
// transaction are stamped a moment apart and a stamp has only so many digits,
// so without it the two come back in whatever order the database happens to
// hold them -- and a cursor cannot tell apart two rows that are equal in every
// column of the order, so the page after the first of them would either repeat
// the second or skip it.
var listOrder = []entpage.Order{
	{Column: audit.FieldDateCreated, Desc: true},
	{Column: audit.FieldID, Desc: true},
}

// List answers with the trail of whatever the filters name, or with the whole
// of it if there is none, a page at a time.
//
// A filter is the identifier of the row, which is the question this whole thing
// is for and the index the rows are stored under. Nothing narrower is offered
// and nothing wider: what kind of thing it was is not stored, because an
// identifier already answers that for anything that still exists.
func (s AuditServiceServer) List(ctx context.Context, req *go_app.AuditListRequest) (*go_app.AuditListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	sc, err := s.Scope()
	if err != nil {
		return nil, err
	}

	q := db.Audit.Query()

	// Whose trail it is narrows the query rather than the answer. Cutting the
	// answer short afterwards is what a limit does, and a limit taken across
	// every Tenant is one that any of them can push the others out of: write a
	// hundred rows of your own and everybody else's trail answers "nothing
	// happened". Which is worse than an error, because it reads like one.
	//
	// Through the same function the generated reads go through, rather than by
	// asking the scope hook: what narrows a read is the wall today and would be
	// the wall and something else tomorrow, and a list that reached past it for
	// the hook alone would be the one read that missed the something else.
	if p, err := bare.AuditNarrow(ctx, sc.Audit, nil); err != nil {
		return nil, err
	} else if p != nil {
		q.Where(p)
	}

	// A filter, and not the wall: the wall is above. This is how whoever can
	// see more than one Tenant asks about one of them.
	if req.HasTenantId() {
		k, err := uuid.FromBytes(req.GetTenantId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "tenant_id: %s", err)
		}

		q.Where(audit.TenantIDEQ(k))
	}

	if fs := req.GetFilters(); len(fs) > 0 {
		if len(fs) > FilterLimit {
			return nil, status.Errorf(codes.InvalidArgument,
				"filters: %d of them, and %d is the most one list carries", len(fs), FilterLimit)
		}

		ps := make([]predicate.Audit, 0, len(fs))
		for i, f := range fs {
			p, err := pick(f)
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(audit.Or(ps...))
	}

	// Where the page before left off. A trail only grows at the newest end and
	// is read from there, so a keyset is what makes reading further back
	// possible at all: an offset would have to count past every row written
	// since the caller started reading.
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

	// One more than the page, to know whether there is another one without a
	// second query and without a count.
	size := entpage.Size(int(req.GetSize()), PageSize, PageLimit)
	vs, err := q.
		Order(audit.ByDateCreated(entsql.OrderDesc()), audit.ByID(entsql.OrderDesc())).
		Limit(size + 1).
		All(ctx)
	if err != nil {
		return nil, err
	}

	more := len(vs) > size
	if more {
		vs = vs[:size]
	}

	items := make([]*go_app.Audit, len(vs))
	for i, v := range vs {
		items[i] = v.Proto()
	}

	res := go_app.AuditListResponse_builder{Items: items}.Build()
	if more {
		last := vs[len(vs)-1]

		next, err := entpage.Encode(last.DateCreated, last.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "next: %s", err)
		}

		res.SetNext(next)
	}

	return res, nil
}

// pick turns a filter into the predicate that selects what it names.
func pick(f *go_app.AuditFilter) (predicate.Audit, error) {
	ps := make([]predicate.Audit, 0, 1)
	if f.HasObjectId() {
		k, err := uuid.FromBytes(f.GetObjectId())
		if err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "object_id: %s", err)
		}

		ps = append(ps, audit.ObjectIDEQ(k))
	}
	if len(ps) == 0 {
		return nil, status.Error(codes.InvalidArgument, "a filter that names nothing")
	}

	return audit.And(ps...), nil
}
