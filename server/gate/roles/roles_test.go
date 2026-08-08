package roles_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/frame"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/go-app/server/gate/roles"
)

const (
	get   = go_app.CoffeeService_Get_FullMethodName
	patch = go_app.CoffeeService_Patch_FullMethodName
)

func TestRoleAllows(t *testing.T) {
	x := require.New(t)

	r := roles.Role{Actions: []string{get}}
	x.True(r.Allows(get))
	x.False(r.Allows(patch))

	// A whole service, which is the shape a role usually wants: naming every
	// RPC of one is a list that goes stale the day an RPC is added.
	whole := roles.Role{Actions: []string{"/go_app.CoffeeService/*"}}
	x.True(whole.Allows(get))
	x.True(whole.Allows(patch))
	x.False(whole.Allows(go_app.RoasterService_Get_FullMethodName))

	// And it matches the whole service name, so one that merely starts the same
	// way is a different service.
	x.False(roles.Role{Actions: []string{"/go_app.Coffee/*"}}.Allows(get))

	x.True(roles.Role{Actions: []string{roles.Any}}.Allows(get))
	x.False(roles.Role{}.Allows(get), "a role that names nothing allows nothing")
}

// call is one call as a policy sees it.
func call(subject, action string) gate.Call {
	return gate.Call{
		Actor:  frame.Actor{Subject: subject},
		Action: action,
	}
}

func TestStore(t *testing.T) {
	x := require.New(t)

	const who = "anna"
	reader := map[string]roles.Role{"reader": {Actions: []string{get}}}

	p, err := roles.New(roles.Table{
		Roles:    reader,
		Bindings: []roles.Binding{{Subject: who, Role: "reader"}},
	})
	x.NoError(err)

	// A binding naming a role nothing defines would allow less than whoever
	// wrote the table meant, and would read as access lost for no reason.
	err = p.Store(roles.Table{
		Roles:    reader,
		Bindings: []roles.Binding{{Subject: who, Role: "raeder"}},
	})
	x.ErrorContains(err, "raeder")

	// The refusal is whole: what it was answering from is what it goes on
	// answering from.
	x.NoError(p.May(t.Context(), call(who, get)))

	// And a table that is good replaces it from the next call onwards.
	x.NoError(p.Store(roles.Table{Roles: reader}))
	x.Equal(codes.PermissionDenied, status.Code(p.May(t.Context(), call(who, get))))
}
