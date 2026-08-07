package core_test

import (
	"context"
	"testing"

	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

func TestHolderAdd(t *testing.T) {
	t.Run("added under the given tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, tenant)

		v, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: tenant.Ref(),
			Alias:  " John ",
		}.Build())
		x.NoError(err)
		x.Equal("john", v.GetAlias())
		x.Equal("john", v.GetName())
		x.Equal(tenant.GetId(), v.GetTenant().GetId())
	}))
	t.Run("tenant must be given", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{Alias: "john"}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
	t.Run("tenant must exist", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: go_app.TenantByAlias("acme"),
			Alias:  "john",
		}.Build())
		x.ErrCode(codes.NotFound, err)
	}))
	t.Run("alias is taken in the tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, tenant)
		c.CreateHolder(ctx, x, tenant.Ref(), "john")

		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: tenant.Ref(),
			Alias:  "john",
		}.Build())
		x.ErrCode(codes.AlreadyExists, err)
	}))
	t.Run("alias is free in another tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")

		u := c.CreateHolder(ctx, x, acme.Ref(), "john")
		v := c.CreateHolder(ctx, x, hooli.Ref(), "john")
		x.NotEqual(u.GetId(), v.GetId())
	}))
}

func TestHolderList(t *testing.T) {
	aliases := func(res *go_app.HolderListResponse) []string {
		vs := []string{}
		for _, v := range res.GetItems() {
			vs = append(vs, v.GetAlias())
		}

		return vs
	}

	t.Run("every holder if there is no filter", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")
		c.CreateHolder(ctx, x, acme.Ref(), "john")
		c.CreateHolder(ctx, x, acme.Ref(), "jane")
		c.CreateHolder(ctx, x, hooli.Ref(), "erlich")

		// Through the ungated stack, since this is about what the list itself
		// answers with rather than about what a caller may see of it.
		v, err := c.Ungated().Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)
		// One admin for each of the three tenants: root, acme and hooli.
		x.ElementsMatch([]string{
			"admin", "admin", "admin",
			"john", "jane", "erlich",
		}, aliases(v))
	}))
	t.Run("only the admins when nothing else was added", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Equal([]string{"admin"}, aliases(v))
	}))
	t.Run("the holders the filters point at", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, acme)
		john := c.CreateHolder(ctx, x, acme.Ref(), "john")
		c.CreateHolder(ctx, x, acme.Ref(), "jane")
		erlich := c.CreateHolder(ctx, x, acme.Ref(), "erlich")

		v, err := c.Holder().List(ctx, go_app.HolderListRequest_builder{
			Filters: []*go_app.HolderFilter{
				go_app.HolderFilter_builder{Ref: john.Ref()}.Build(),
				go_app.HolderFilter_builder{Ref: go_app.HolderBySlug("erlich", acme.Ref())}.Build(),
			},
		}.Build())
		x.NoError(err)
		x.ElementsMatch([]string{"john", "erlich"}, aliases(v))
		x.Equal(john.GetId(), v.GetItems()[0].GetId())
		x.NotEqual(erlich.GetId(), john.GetId())
	}))
	t.Run("a filter without a key is rejected", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Holder().List(ctx, go_app.HolderListRequest_builder{
			Filters: []*go_app.HolderFilter{{}},
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}

func TestHolderGet(t *testing.T) {
	t.Run("by slug", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, tenant)
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		u, err := c.Holder().Get(ctx, go_app.HolderGetBySlug("john", tenant.Ref()))
		x.NoError(err)
		x.Equal(v.GetId(), u.GetId())
	}))
	t.Run("with the tenant it belongs to", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, tenant)
		c.CreateHolder(ctx, x, tenant.Ref(), "john")

		// Edges are not read unless they are selected.
		req := go_app.HolderGetBySlug("john", tenant.Ref()).
			WithSelect(func(s *go_app.HolderSelect) {
				s.SetTenant(go_app.TenantSelect_builder{All: z.Ptr(true)}.Build())
			})

		u, err := c.Holder().Get(ctx, req)
		x.NoError(err)
		x.Equal("acme", u.GetTenant().GetAlias())
	}))
	t.Run("not found", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		ctx = c.AsAdminOf(ctx, x, tenant)

		_, err := c.Holder().Get(ctx, go_app.HolderGetBySlug("john", tenant.Ref()))
		x.ErrCode(codes.NotFound, err)
	}))
}
