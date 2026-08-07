package gate

import (
	"context"

	"github.com/google/uuid"

	"github.com/lesomnus/go-app/internal/ent/audit"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/internal/ent/tenant"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/frame"
)

// Wall answers with the scope that puts every read behind the Tenant it belongs
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
func Wall() bare.Scope {
	return wall{}
}

// wall is the [bare.Scope] [Wall] answers with: one method per entity, all of
// them the same shape.
//
// It embeds [bare.Unscoped] and so says nothing about an entity it has no
// method for. That is not laziness -- it is what makes adding an entity to the
// schema a decision rather than a compile error here, and the decision is
// whether the new thing is inside a Tenant. An entity that is stays out of
// every read until somebody writes its method, which is the wrong way round;
// so the rule for this app is that an entity added here gets a method, and the
// test in wall_test.go is what says so out loud.
type wall struct {
	bare.Unscoped
}

// ids is which Tenants the caller may see, and whether that is all of them. The
// identifiers are the frame's, so a failure to read one is the app disagreeing
// with itself.
func (wall) ids(ctx context.Context) ([]uuid.UUID, bool, error) {
	s, err := Scope(ctx)
	if err != nil || s.All() {
		return nil, true, err
	}

	return s.Ids(), false, nil
}

// HolderScope: a Holder is inside the Tenant it belongs to.
func (w wall) HolderScope(ctx context.Context) (predicate.Holder, error) {
	vs, all, err := w.ids(ctx)
	if all || err != nil {
		return nil, err
	}

	return holder.HasTenantWith(tenant.IDIn(vs...)), nil
}

// TenantScope: a Tenant is inside itself, which is what a Tenant being a wall
// comes down to -- from inside one there is exactly one.
func (w wall) TenantScope(ctx context.Context) (predicate.Tenant, error) {
	vs, all, err := w.ids(ctx)
	if all || err != nil {
		return nil, err
	}

	return tenant.IDIn(vs...), nil
}

// AuditScope: a row of the trail belongs to the Tenant that was acting, which
// is not the Tenant of whatever it was written to. A caller from elsewhere
// writing into acme leaves a row that is theirs, not acme's. It is the same
// wall either way -- what one Tenant did is not visible from another.
func (w wall) AuditScope(ctx context.Context) (predicate.Audit, error) {
	vs, all, err := w.ids(ctx)
	if all || err != nil {
		return nil, err
	}

	return audit.TenantIDIn(vs...), nil
}

// Scope is which Tenants the caller of ctx may see, which was worked out once
// in front and is read from the frame here; see [Interceptor].
//
// A request with no frame is refused rather than served as anybody, and there
// is no scope that means "everything, because nobody asked". Some calls do have
// to go around the wall -- working out who is calling happens before there is a
// frame to be walled by, and `core.EnsureRoot` runs before there is anybody to
// be -- and they go around it by being handed a server the wall was never
// installed on. That is a wiring decision somebody can read; a scope that
// opened up whenever the frame was missing would be the same decision made
// silently, everywhere, including wherever a frame goes missing by mistake.
func Scope(ctx context.Context) (frame.Tenants, error) {
	f, err := actor(ctx)
	if err != nil {
		return frame.Nothing, err
	}

	return f.Scope, nil
}
