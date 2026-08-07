package gate_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/lesomnus/protobuf-patch/patch"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/core"
)

// two tenants, and somebody in each of them.
type pair struct {
	acme  *go_app.Tenant
	hooli *go_app.Tenant

	john   *go_app.Holder
	erlich *go_app.Holder
}

func setup(ctx context.Context, x *ox.X, c *ox.Client) pair {
	x.TB().Helper()

	var v pair
	v.acme = c.CreateTenant(ctx, x, "acme")
	v.hooli = c.CreateTenant(ctx, x, "hooli")
	v.john = c.CreateHolder(ctx, x, v.acme.Ref(), "john")
	v.erlich = c.CreateHolder(ctx, x, v.hooli.Ref(), "erlich")

	return v
}

func TestHolder(t *testing.T) {
	t.Run("one of my own is mine to see", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		v, err := c.Holder().Get(ctx, go_app.HolderGetBySlug("john", p.acme.Ref()))
		x.NoError(err)
		x.Equal(p.john.GetId(), v.GetId())
	}))
	t.Run("one of another tenant is not there at all", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		// Named by identifier, so nothing but the answer says whose it is.
		_, err := c.Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.ErrCode(codes.NotFound, err)

		// And named by tenant and alias, which is the same answer.
		_, err = c.Holder().Get(ctx, go_app.HolderGetBySlug("erlich", p.hooli.Ref()))
		x.ErrCode(codes.NotFound, err)
	}))
	t.Run("the selection is the caller's alone", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		as := c.AsHolder(ctx, p.john)

		// Naming no selection asks for the row, and the row is what comes
		// back -- for a Holder of one Tenant exactly as for whoever
		// administers the deployment. The wall reads nothing to decide, since
		// it is a predicate on the query rather than a look at the answer, so
		// there is nothing it has to add to the selection and nothing it has
		// to take back out.
		//
		// It used to add the Tenant to any selection that did not have it,
		// read it, and clear it again -- which meant the same request answered
		// two different rows depending on who asked.
		v, err := c.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.True(v.HasTenant())
		x.Equal(p.acme.GetId(), v.GetTenant().GetId())

		// And a selection that names one thing is a selection that names only
		// it.
		v, err = c.Holder().Get(as, go_app.HolderGetById(p.john.GetId()).
			WithSelect(func(s *go_app.HolderSelect) { s.SetAlias(true) }))
		x.NoError(err)
		x.Equal("john", v.GetAlias())
		x.False(v.HasTenant())
	}))
	t.Run("a list is only of my own", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		v, err := c.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)

		vs := []string{}
		for _, u := range v.GetItems() {
			vs = append(vs, u.GetAlias())
		}
		// The admin of acme, and john. Not erlich, and not the other admins.
		x.ElementsMatch([]string{"admin", "john"}, vs)
	}))
	t.Run("a list of my own survives a deployment full of other people's", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// More Holders in the other Tenant than a whole answer holds.
		for i := range core.PageLimit + 20 {
			c.CreateHolder(ctx, x, p.hooli.Ref(), fmt.Sprintf("filler-%d", i))
		}

		v, err := c.Holder().List(c.AsHolder(ctx, p.john), &go_app.HolderListRequest{})
		x.NoError(err)

		vs := []string{}
		for _, u := range v.GetItems() {
			vs = append(vs, u.GetAlias())
		}

		// The wall is part of the query, so the limit is taken over acme's
		// Holders and not over everybody's. Filtered after the fact instead,
		// this answer would be empty -- the first hundred rows would all be
		// hooli's and none of them would survive the filter -- and any Tenant
		// could blank another's list by making enough Holders of its own.
		x.ElementsMatch([]string{"admin", "john"}, vs)
	}))
	t.Run("adding to another tenant is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		// NotFound rather than a refusal, which is the wall's rule everywhere
		// else: that another Tenant exists is itself something not to say. It
		// used to answer PermissionDenied here alone, which said "there is a
		// hooli and it is not yours" to anyone who guessed the name.
		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: p.hooli.Ref(),
			Alias:  "gilfoyle",
		}.Build())
		x.ErrCode(codes.NotFound, err)

		// And a Tenant that is not there at all is the same answer, so the two
		// cannot be told apart.
		_, err = c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: go_app.TenantByAlias("nobody"),
			Alias:  "gilfoyle",
		}.Build())
		x.ErrCode(codes.NotFound, err)

		_, err = c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: p.acme.Ref(),
			Alias:  "gilfoyle",
		}.Build())
		x.NoError(err)
	}))
	t.Run("changing one of another tenant changes nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		as := c.AsHolder(ctx, p.john)

		name := "Erlich"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  p.erlich.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.NotFound, err)

		// Erasing what is not there succeeds, and out of the wall is not
		// there, so this succeeds and erases nothing. It reads odd and it is
		// the honest answer: an erase is idempotent, and one that answered
		// NotFound for a row that exists but is not yours would be telling a
		// caller apart from the case where the row never existed at all.
		_, err = c.Holder().Erase(as, p.erlich.Ref())
		x.NoError(err)

		// Which is the assertion that matters.
		v, err := c.Bare().Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
		x.Equal("erlich", v.GetAlias())
	}))
	t.Run("a patch document is no way around the wall", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		_, err := c.Holder().Apply(ctx, go_app.HolderApplyRequest_builder{
			Ref: p.erlich.Ref(),
			Patch: patch.MustNew("go_app.Holder",
				patch.Target(patch.Name("name")).Assign(patch.Str("Erlich")),
			),
		}.Build())
		x.ErrCode(codes.NotFound, err)

		// Nothing was written on the way to being refused.
		v, err := c.Bare().Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
		x.Equal(p.erlich.GetName(), v.GetName())
	}))
}

func TestTenant(t *testing.T) {
	t.Run("mine is the only one there is", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		v, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.NoError(err)
		x.Equal(p.acme.GetId(), v.GetId())

		_, err = c.Tenant().Get(ctx, go_app.TenantGetByAlias("hooli"))
		x.ErrCode(codes.NotFound, err)

		_, err = c.Tenant().Get(ctx, c.Server.Root.Pick())
		x.ErrCode(codes.NotFound, err)
	}))
	t.Run("putting one up is nobody's to do from in here", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// Unimplemented and not a refusal, and the same answer to everybody --
		// the admin of a Tenant and the admin of the first one alike. It is not
		// about who is asking; a Tenant is put up by the deployment, through a
		// server this layer is not in front of.
		for _, ctx := range []context.Context{c.AsHolder(ctx, p.john), c.AsRoot(ctx)} {
			_, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "pied-piper"}.Build())
			x.ErrCode(codes.Unimplemented, err)

			_, err = c.Tenant().Erase(ctx, p.hooli.Ref())
			x.ErrCode(codes.Unimplemented, err)
		}
	}))
	t.Run("another one is not mine to patch, by either road", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		name := "Hooli"
		_, err := c.Tenant().Patch(ctx, go_app.TenantPatchRequest_builder{
			Ref:  p.hooli.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.NotFound, err)

		_, err = c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: p.hooli.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("name")).Assign(patch.Str(name)),
			),
		}.Build())
		x.ErrCode(codes.NotFound, err)

		// And it is mine to do to my own.
		_, err = c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: p.acme.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("name")).Assign(patch.Str("Acme")),
			),
		}.Build())
		x.NoError(err)
	}))
	// There used to be a caller the wall was not about: whoever held the first
	// Tenant, told apart by an identifier this app kept as a constant. There is
	// not one now. A privilege granted by being a particular row cannot be
	// revoked, cannot be narrowed, and does not appear anywhere it is used.
	t.Run("nobody is outside the wall, not even the first tenant's admin", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsRoot(ctx)

		_, err := c.Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.ErrCode(codes.NotFound, err)

		_, err = c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)

		// What the deployment does, it does through a server this layer is not
		// in front of. That is a thing somebody was handed, not a thing they
		// are.
		v, err := c.Ungated().Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
		x.Equal(p.erlich.GetId(), v.GetId())
	}))
}

func TestNobody(t *testing.T) {
	t.Run("a call from nobody is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Tenant().Get(c.AsNobody(ctx), go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.Unauthenticated, err)
	}))
	t.Run("a caller who is not here is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		ctx = c.AsHolder(ctx, go_app.Holder_builder{
			Alias:  "nobody",
			Tenant: go_app.Tenant_builder{Alias: "nowhere"}.Build(),
		}.Build())

		_, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.Unauthenticated, err)
	}))
}
