package roles_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/go-app/server/gate/roles"
)

const (
	get   = go_app.HolderService_Get_FullMethodName
	patch = go_app.HolderService_Patch_FullMethodName
)

func TestRoleAllows(t *testing.T) {
	x := require.New(t)

	r := roles.Role{Actions: []string{get}}
	x.True(r.Allows(get))
	x.False(r.Allows(patch))

	// A whole service, which is the shape a role usually wants: naming every
	// RPC of one is a list that goes stale the day an RPC is added.
	whole := roles.Role{Actions: []string{"/go_app.HolderService/*"}}
	x.True(whole.Allows(get))
	x.True(whole.Allows(patch))
	x.False(whole.Allows(go_app.TenantService_Get_FullMethodName))

	// And it matches the whole service name, so one that merely starts the same
	// way is a different service.
	x.False(roles.Role{Actions: []string{"/go_app.Holder/*"}}.Allows(get))

	x.True(roles.Role{Actions: []string{roles.Any}}.Allows(get))
	x.False(roles.Role{}.Allows(get), "a role that names nothing allows nothing")
}

// call is one call as a policy sees it, by identifiers rather than by rows.
func call(actor, tenant uuid.UUID, action string) gate.Call {
	return gate.Call{
		Actor: go_app.Holder_builder{
			Id:     actor[:],
			Tenant: go_app.Tenant_builder{Id: tenant[:]}.Build(),
		}.Build(),
		Action: action,
	}
}

func TestStore(t *testing.T) {
	x := require.New(t)

	who := uuid.New()
	reader := map[string]roles.Role{"reader": {Actions: []string{get}}}

	p, err := roles.New(roles.Table{
		Roles:    reader,
		Bindings: []roles.Binding{{Holder: who, Role: "reader"}},
	})
	x.NoError(err)

	// A binding naming a role nothing defines would allow less than whoever
	// wrote the table meant, and would read as access lost for no reason.
	err = p.Store(roles.Table{
		Roles:    reader,
		Bindings: []roles.Binding{{Holder: who, Role: "raeder"}},
	})
	x.ErrorContains(err, "raeder")

	// The refusal is whole: what it was answering from is what it goes on
	// answering from.
	x.NoError(p.May(t.Context(), call(who, uuid.New(), get)))

	// And a table that is good replaces it from the next call onwards.
	x.NoError(p.Store(roles.Table{Roles: reader}))
	x.Equal(codes.PermissionDenied, status.Code(p.May(t.Context(), call(who, uuid.New(), get))))
}

// pair is two Tenants and somebody in each of them.
type pair struct {
	acme  *go_app.Tenant
	hooli *go_app.Tenant

	john   *go_app.Holder
	erlich *go_app.Holder
}

func (p pair) id(x *ox.X, v interface{ GetId() []byte }) uuid.UUID {
	x.TB().Helper()

	k, err := uuid.FromBytes(v.GetId())
	x.NoError(err)

	return k
}

// served arranges the Tenants, builds the table out of them, and answers with a
// client of the app served behind it -- the way `cmd/serve.go` would have built
// one, and the reason the client is made last.
func served(ctx context.Context, x *ox.X, c *ox.Client, mk func(p pair) roles.Table) (pair, *roles.Policy, *ox.Client) {
	x.TB().Helper()

	var p pair
	p.acme = c.CreateTenant(ctx, x, "acme")
	p.hooli = c.CreateTenant(ctx, x, "hooli")
	p.john = c.CreateHolder(ctx, x, p.acme.Ref(), "john")
	p.erlich = c.CreateHolder(ctx, x, p.hooli.Ref(), "erlich")

	pol, err := roles.New(mk(p))
	x.NoError(err)

	c.Server.Policy = pol
	d := ox.NewClient(x.TB(), c.Server)
	x.TB().Cleanup(func() { d.Close() })

	return p, pol, d
}

func TestPolicyServed(t *testing.T) {
	// Two roles, of which these tests hand out the smaller one. The other is
	// here so that "no role of yours allows it" is about the binding rather
	// than about the table having nothing to say.
	defined := map[string]roles.Role{
		"reader": {Actions: []string{get}},
		"admin":  {Actions: []string{"/go_app.HolderService/*"}},
	}

	t.Run("a binding that names no tenant is the wall, said in a table", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, _, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{
				Roles:    defined,
				Bindings: []roles.Binding{{Holder: p.id(x, p.john), Role: "reader"}},
			}
		})
		as := d.AsHolder(ctx, p.john)

		v, err := d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.Equal(p.john.GetId(), v.GetId())

		// His own Tenant and no more, which is exactly what he had before there
		// was a policy at all.
		_, err = d.Holder().Get(as, go_app.HolderGetById(p.erlich.GetId()))
		x.ErrCode(codes.NotFound, err)
	}))

	t.Run("what no role allows is refused before the handler", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, _, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{
				Roles:    defined,
				Bindings: []roles.Binding{{Holder: p.id(x, p.john), Role: "reader"}},
			}
		})
		as := d.AsHolder(ctx, p.john)

		// PermissionDenied and not NotFound: which RPCs there are is not a
		// secret from the caller, and this is about the RPC. Which rows exist
		// is the predicate's business, and it says NotFound of its own accord.
		name := "Johnny"
		_, err := d.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  p.john.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.PermissionDenied, err)

		// And nothing was written. Read out of the database rather than through
		// a client, since every client of this server goes through the policy
		// and there is nobody it would let read this.
		u, err := c.Server.Db.Holder.Get(ctx, p.id(x, p.john))
		x.NoError(err)
		x.Equal(p.john.GetName(), u.Name)
	}))

	t.Run("a binding elsewhere reaches what the wall alone cannot", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, _, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{
				Roles: defined,
				Bindings: []roles.Binding{{
					Holder:  p.id(x, p.john),
					Role:    "reader",
					Tenants: []uuid.UUID{p.id(x, p.hooli)},
				}},
			}
		})
		as := d.AsHolder(ctx, p.john)

		// The whole of what a policy is for: john is held by acme and reads
		// hooli, which no rule in this app grants and no credential could.
		v, err := d.Holder().Get(as, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
		x.Equal(p.erlich.GetId(), v.GetId())

		// And it *replaces* the wall rather than adding to it, so the binding
		// that says hooli is a caller who has left his own Tenant behind. That
		// is the trap worth a test: a table that means "and his own" says so.
		_, err = d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.NotFound, err)
	}))

	t.Run("a caller the table says nothing about sees nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, _, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{Roles: defined}
		})

		// Not "everything, because nothing was said". A policy that opened up
		// as it ran out of things to say would be the worst kind of silence.
		_, err := d.Holder().Get(d.AsHolder(ctx, p.john), go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.PermissionDenied, err)
	}))

	t.Run("everywhere is every tenant, and a deployment had to write it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, _, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{
				Roles: defined,
				Bindings: []roles.Binding{{
					Holder:     p.id(x, p.john),
					Role:       "reader",
					Everywhere: true,
				}},
			}
		})
		as := d.AsHolder(ctx, p.john)

		// The superuser this app does not have, put back by a table somebody
		// can edit and revoke -- which is the difference from a comparison
		// against a well-known identifier. See docs/AUTH.md.
		for _, v := range []*go_app.Holder{p.john, p.erlich} {
			u, err := d.Holder().Get(as, go_app.HolderGetById(v.GetId()))
			x.NoError(err)
			x.Equal(v.GetId(), u.GetId())
		}

		// Still only what the role allows. Seeing every Tenant is not doing
		// everything in them.
		name := "Johnny"
		_, err := d.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  p.john.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.PermissionDenied, err)
	}))

	t.Run("a table stored while it is being served is the next call's", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p, pol, d := served(ctx, x, c, func(p pair) roles.Table {
			return roles.Table{
				Roles:    defined,
				Bindings: []roles.Binding{{Holder: p.id(x, p.john), Role: "reader"}},
			}
		})
		as := d.AsHolder(ctx, p.john)

		_, err := d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)

		// What an engine pushing an update looks like from here: no restart, no
		// call waiting on anything, and the request already in flight answered
		// from the table it started with.
		x.NoError(pol.Store(roles.Table{Roles: defined}))

		_, err = d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.PermissionDenied, err)
	}))
}
