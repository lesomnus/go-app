package audit

import (
	"context"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/audit"
	"github.com/lesomnus/go-app/internal/ent/predicate"
)

// ListLimit is the most rows [AuditServiceServer.List] answers with.
const ListLimit = 100

// List answers with the trail of whatever the filters name, or with the whole
// of it if there is none.
//
// A filter is the identifier of the row, which is the question this whole thing
// is for and the index the rows are stored under. Nothing narrower is offered
// and nothing wider: what kind of thing it was is not stored, because an
// identifier already answers that for anything that still exists.
//
// Newest first, which is the opposite of the lists elsewhere in this app and is
// the point: what a trail is asked is what happened, and the answer starts with
// what happened last. Like `core.HolderServiceServer.List` it does not page,
// and a deployment that keeps a trail long enough to care will want it to.
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
	// The narrowing is the same one the generated servers apply to every row
	// they read; a hand-written list is the read they do not make, so it is
	// asked for here rather than inherited.
	if sc.Audit != nil {
		p, err := sc.Audit(ctx)
		if err != nil {
			return nil, err
		}
		if p != nil {
			q.Where(p)
		}
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

	// The identifier settles a tie. Two rows of one transaction are stamped a
	// moment apart and a stamp has only so many digits, so without it the two
	// come back in whatever order the database happens to hold them -- and the
	// order is the one thing a trail is read for.
	vs, err := q.
		Order(audit.ByDateCreated(entsql.OrderDesc()), audit.ByID(entsql.OrderDesc())).
		Limit(ListLimit).
		All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*go_app.Audit, len(vs))
	for i, v := range vs {
		items[i] = v.Proto()
	}

	return go_app.AuditListResponse_builder{Items: items}.Build(), nil
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
