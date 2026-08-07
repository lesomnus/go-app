package gate

import (
	"bytes"
	"context"
	"slices"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/internal/ent/audit"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/internal/ent/tenant"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/frame"
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

		return s.Ids(), false, nil
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
// Two things narrow it and they are asked in that order. What the *Holder* may
// see is the wall, and a deployment can say more about it than this does; see
// [Policy]. What the *credential* they came with allows is then met with that,
// and can only take away -- a token that names every Tenant, held by somebody
// who may see one, still sees one.
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
		return Nothing, err
	}

	t, err := holds(ctx, f)
	if err != nil {
		return Nothing, err
	}

	return t.Meet(f.Grant), nil
}

// holds is what the Holder may see, before the credential is met with it.
func holds(ctx context.Context, f *frame.Frame) (Tenants, error) {
	if p := policy; p != nil {
		v, err := p.Where(ctx, f.Actor, method(ctx))
		return v, err
	}

	// Whoever holds the root Tenant administers the deployment, and is the one
	// caller no wall is about.
	v := f.Tenant()
	if bytes.Equal(v.GetId(), core.RootId[:]) {
		return Everything, nil
	}

	k, err := uuid.FromBytes(v.GetId())
	if err != nil {
		// It came out of the database with the actor, so this is the app
		// disagreeing with itself rather than a request being wrong.
		return Nothing, status.Errorf(codes.Internal, "the caller's tenant has no identifier: %s", err)
	}

	return Only(k), nil
}

// Tenants is a set of Tenants, or all of them.
//
// A set, and not the one Tenant the caller belongs to beside a flag saying
// whether the wall applies to them. "Everything" and "my own" are the two
// answers a Holder alone gives, and spelling the second as "not the first" is
// what would make a third expensive. There are already more than two: a
// credential carries an attenuation ([frame.Grant]), and what a caller may see
// is the meet of the two. A deployment that later lets a resource be shared
// with another Tenant adds a fourth, and it is a longer list and nothing else.
//
// Identifiers rather than Tenants, because that is what a query narrows by and
// what two scopes are intersected on. Nothing needs the rest of the row.
type Tenants struct {
	all bool
	ids []uuid.UUID
}

// Everything is the scope of a caller no wall is about.
var Everything = Tenants{all: true}

// Nothing is the scope of a caller who may see none, which is what the meet of
// two disjoint scopes is. It narrows a query to no rows at all rather than to
// all of them; see [Tenants.Ids].
var Nothing = Tenants{}

// Only is the scope of a caller who may see the given Tenants and no others.
func Only(ids ...uuid.UUID) Tenants {
	return Tenants{ids: ids}
}

// All reports whether this is every Tenant there is, in which case there is
// nothing to narrow by and [Tenants.Ids] says nothing.
func (t Tenants) All() bool { return t.all }

// Ids is what a query narrows by.
//
// An empty answer is not the same as no narrowing, and the difference is the
// whole safety of this: `IDIn()` with nothing renders as `WHERE FALSE`, so a
// caller who may see no Tenant sees no rows. Read the other way round it would
// be a scope that opened up as it ran out.
func (t Tenants) Ids() []uuid.UUID { return t.ids }

// Meet answers with what is in both this scope and `g`, which is how a
// credential narrows what its Holder may see.
//
// It only ever narrows. A grant that names Tenants its Holder cannot see does
// not reach them: the meet of "my own" and "every tenant there is" is my own.
func (t Tenants) Meet(g frame.Grant) Tenants {
	if g.AnyTenant() {
		return t
	}
	if t.all {
		return Only(g.TenantIds()...)
	}

	vs := make([]uuid.UUID, 0, min(len(t.ids), len(g.TenantIds())))
	for _, v := range t.ids {
		if slices.Contains(g.TenantIds(), v) {
			vs = append(vs, v)
		}
	}

	return Only(vs...)
}
