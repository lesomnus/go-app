package gate_test

import (
	"context"
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
	t.Run("what was not asked for does not come back", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		// The tenant is read to decide whether this is allowed, and taken back
		// out because the caller did not ask for it.
		v, err := c.Holder().Get(ctx, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.False(v.HasTenant())

		u, err := c.Holder().Get(ctx, go_app.HolderGetById(p.john.GetId()).
			WithSelect(func(s *go_app.HolderSelect) {
				s.SetTenant(go_app.TenantSelect_builder{}.Build())
			}))
		x.NoError(err)
		x.True(u.HasTenant())
		x.Equal(p.acme.GetId(), u.GetTenant().GetId())
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
	t.Run("adding to another tenant is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: p.hooli.Ref(),
			Alias:  "gilfoyle",
		}.Build())
		x.ErrCode(codes.PermissionDenied, err)

		_, err = c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: p.acme.Ref(),
			Alias:  "gilfoyle",
		}.Build())
		x.NoError(err)
	}))
	t.Run("changing one of another tenant is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		name := "Erlich"
		_, err := c.Holder().Patch(ctx, go_app.HolderPatchRequest_builder{
			Ref:  p.erlich.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.NotFound, err)

		_, err = c.Holder().Erase(ctx, p.erlich.Ref())
		x.ErrCode(codes.NotFound, err)
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

		_, err = c.Tenant().Get(ctx, go_app.TenantGetById(core.RootId[:]))
		x.ErrCode(codes.NotFound, err)
	}))
	t.Run("putting up another one is not mine to do", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)
		ctx = c.AsHolder(ctx, p.john)

		_, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "pied-piper"}.Build())
		x.ErrCode(codes.PermissionDenied, err)

		_, err = c.Tenant().Erase(ctx, p.hooli.Ref())
		x.ErrCode(codes.PermissionDenied, err)
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
	t.Run("whoever administers the deployment is not walled in", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// The context of a test is already this one; said again for the record.
		ctx = c.AsRoot(ctx)

		v, err := c.Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
		x.Equal(p.erlich.GetId(), v.GetId())

		_, err = c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "pied-piper"}.Build())
		x.NoError(err)
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
