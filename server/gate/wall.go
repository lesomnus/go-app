package gate

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/audit"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/internal/ent/tenant"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
)

// Wall answers with the scopes that put every read behind the Tenant it belongs
// to. It is the whole of the rule this package used to spell out one RPC at a
// time.
//
// It is installed on the innermost server rather than applied here, which reads
// backwards and is the point. Narrowing what a caller may see is a predicate,
// and a predicate belongs in the query; done from in front it is an override of
// Get, Patch, Apply and Erase, once per entity and once more for every entity
// added afterwards. That is what this replaced -- thirteen overrides across
// three entities, one of which carried a bug that was fixed in one copy and
// left in the next.
//
// So the wall is stated here, with the rest of the rules, and enforced where
// the statement runs:
//
//	sink, err := bare.NewServer(db, bare.WithScope(gate.Wall()))
//	s, err := go_app.Build(sink, core.Build(), audit.Build(), gate.Build())
//
// What is left in this package is what is genuinely not a predicate: whether a
// Tenant may be put up or taken down, and which Tenant a Holder may be added
// to. Those are about a row that does not exist yet, so there is nothing to
// narrow.
func Wall() bare.Scopes {
	// Each of these is the same shape: everything, or the rows that hang off
	// the Tenants in scope. The identifiers are the frame's, so a failure to
	// read one is the app disagreeing with itself.
	ids := func(ctx context.Context) ([]uuid.UUID, bool, error) {
		s, err := Scope(ctx)
		if err != nil || s.All() {
			return nil, true, err
		}

		vs, err := s.Ids()
		return vs, false, err
	}

	return bare.Scopes{
		// A Holder is inside the Tenant it belongs to.
		Holder: func(ctx context.Context) (predicate.Holder, error) {
			vs, all, err := ids(ctx)
			if all || err != nil {
				return nil, err
			}

			return holder.HasTenantWith(tenant.IDIn(vs...)), nil
		},

		// A Tenant is inside itself, which is what a Tenant being a wall comes
		// down to: from inside one there is exactly one.
		Tenant: func(ctx context.Context) (predicate.Tenant, error) {
			vs, all, err := ids(ctx)
			if all || err != nil {
				return nil, err
			}

			return tenant.IDIn(vs...), nil
		},

		// A row of the trail belongs to the Tenant that was acting, which is
		// not the Tenant of whatever it was written to: whoever administers the
		// deployment may write into any Tenant, and the row saying so is
		// theirs. It is the same wall either way -- what one Tenant did is not
		// visible from another.
		Audit: func(ctx context.Context) (predicate.Audit, error) {
			vs, all, err := ids(ctx)
			if all || err != nil {
				return nil, err
			}

			return audit.TenantIDIn(vs...), nil
		},
	}
}

// Scope is which Tenants the caller of ctx may see.
//
// A request with no frame is refused rather than served as anybody, and there
// is no scope that means "everything, because nobody asked". Some calls do have
// to go around the wall -- working out who is calling happens before there is a
// frame to be walled by, and `core.EnsureRoot` runs before there is anybody to
// be -- and they go around it by being handed a server the wall was never
// installed on. That is a wiring decision somebody can read; a scope that
// opened up whenever the frame was missing would be the same decision made
// silently, everywhere, including wherever a frame goes missing by mistake.
func Scope(ctx context.Context) (Tenants, error) {
	f, err := actor(ctx)
	if err != nil {
		return Tenants{}, err
	}

	// Whoever holds the root Tenant administers the deployment, and is the one
	// caller no wall is about.
	v := f.Tenant()
	if bytes.Equal(v.GetId(), core.RootId[:]) {
		return Everything, nil
	}

	return Only(v), nil
}

// Tenants is a set of Tenants, or all of them.
//
// A set, and not the one Tenant the caller belongs to beside a flag saying
// whether the wall applies to them. "Everything" and "my own" are the two
// answers there are today, and spelling the second as "not the first" is what
// would make a third expensive: a deployment that lets a resource be shared
// with another Tenant, or transferred to one, has callers who may see two, and
// every place that read the flag would have to learn about it. Here that is a
// longer list and nothing else.
type Tenants struct {
	all bool
	vs  []*go_app.Tenant
}

// Everything is the scope of a caller no wall is about.
var Everything = Tenants{all: true}

// Only is the scope of a caller who may see the given Tenants and no others.
func Only(vs ...*go_app.Tenant) Tenants {
	return Tenants{vs: vs}
}

// All reports whether this is every Tenant there is, in which case there is
// nothing to narrow by and [Tenants.Ids] says nothing.
func (t Tenants) All() bool { return t.all }

// Ids is what a query narrows by.
func (t Tenants) Ids() ([]uuid.UUID, error) {
	vs := make([]uuid.UUID, len(t.vs))
	for i, v := range t.vs {
		k, err := uuid.FromBytes(v.GetId())
		if err != nil {
			// These came out of the database with the actor, so this is the
			// app disagreeing with itself rather than a request being wrong.
			return nil, status.Errorf(codes.Internal, "a tenant in scope has no identifier: %s", err)
		}

		vs[i] = k
	}

	return vs, nil
}

// Picks reports whether the given reference names a Tenant in this scope.
//
// A reference names a Tenant by identifier or by alias, and both are known for
// every Tenant in scope, so neither costs a query. A scope that ever holds a
// Tenant this app has not already read would have to look it up here.
func (t Tenants) Picks(ref *go_app.TenantRef) bool {
	if t.all {
		return true
	}

	for _, v := range t.vs {
		if ref.Picks(v) {
			return true
		}
	}

	return false
}
