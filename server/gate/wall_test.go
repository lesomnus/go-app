package gate_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/frame"
	"github.com/lesomnus/go-app/server/gate"
)

// TestWallCoversEveryEntity is the one that fails when an entity is added.
//
// A [bare.Scope] has a method per entity, and the wall embeds [bare.Unscoped]
// so that it does not have to write out the ones it has nothing to say about.
// Here it has something to say about all of them -- everything in this app is
// inside a Tenant -- so an entity added to the schema would arrive with
// Unscoped's answer, which is "no opinion", which is every row.
//
// That would compile, pass every other test, and serve the new thing to
// everybody. So this asks each method what it answers rather than trusting that
// somebody remembered: an entity added without a method here narrows nothing,
// and this is what says so.
func TestWallCoversEveryEntity(t *testing.T) {
	x := require.New(t)

	// A caller who may see one Tenant, which is the case every method has
	// something to say about. `frame.Everything` would be the case where
	// answering with no predicate is right, and would prove nothing.
	f := frame.New(&go_app.Holder{}, frame.Whole()).WithScope(frame.Only(uuid.New()))
	ctx := frame.Into(t.Context(), f)

	var (
		scope = reflect.TypeOf((*bare.Scope)(nil)).Elem()
		w     = reflect.ValueOf(gate.Wall())
	)
	x.Positive(scope.NumMethod())

	for i := range scope.NumMethod() {
		name := scope.Method(i).Name

		out := w.MethodByName(name).Call([]reflect.Value{reflect.ValueOf(ctx)})
		x.True(out[1].IsNil(), "%s: %v", name, out[1])
		x.False(out[0].IsNil(), "%s answers with no predicate, so the wall says nothing about it", name)
	}
}

// And the other half of the same statement: what the wall says about a caller
// who may see everything is nothing, since there is nothing to narrow to.
func TestWallNarrowsNothingForEverything(t *testing.T) {
	x := require.New(t)

	f := frame.New(&go_app.Holder{}, frame.Whole()).WithScope(frame.Everything)
	ctx := frame.Into(t.Context(), f)

	p, err := gate.Wall().TenantScope(ctx)
	x.NoError(err)
	x.Nil(p)
}

// A request nobody vouched for is refused rather than served as anybody. It is
// the wall's answer and not a rule in front of it, which is what makes going
// around it a server the wall was never installed on.
func TestWallRefusesAFramelessRequest(t *testing.T) {
	x := require.New(t)

	_, err := gate.Wall().TenantScope(context.Background())
	x.Error(err)
}
