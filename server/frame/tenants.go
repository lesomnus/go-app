package frame

import (
	"slices"

	"github.com/google/uuid"
)

// Tenants is a set of Tenants, or all of them.
//
// A set, and not the one Tenant the caller belongs to beside a flag saying
// whether the wall applies to them. There are more answers than two: a
// credential carries an attenuation ([Grant]) and what a caller may see is the
// meet of the two, a policy may answer with several, and a deployment that
// later lets a resource be shared with another Tenant adds another. Each of
// those is a longer list and nothing else.
//
// Identifiers rather than Tenants, because that is what a query narrows by and
// what two scopes are intersected on. Nothing needs the rest of the row.
type Tenants struct {
	all bool
	ids []uuid.UUID
}

// Everything is every Tenant there is.
//
// Nothing in this app answers with it. The wall gives a caller their own Tenant
// and no more, and there is no identifier it compares against to decide
// otherwise -- what the deployment does for itself, it does through a server
// the wall was never installed on. It is here because a [gate.Policy] may
// answer with it, which is a deployment saying so rather than this app
// assuming it.
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
func (t Tenants) Meet(g Grant) Tenants {
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
