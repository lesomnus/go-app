package core_test

import (
	"context"
	"testing"

	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

func TestUserAdd(t *testing.T) {
	t.Run("added under the given tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")

		v, err := c.User().Add(ctx, go_app.UserAddRequest_builder{
			Tenant: tenant.Ref(),
			Alias:  " John ",
		}.Build())
		x.NoError(err)
		x.Equal("john", v.GetAlias())
		x.Equal("john", v.GetName())
		x.Equal(tenant.GetId(), v.GetTenant().GetId())
	}))
	t.Run("tenant must be given", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.User().Add(ctx, go_app.UserAddRequest_builder{Alias: "john"}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
	t.Run("tenant must exist", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.User().Add(ctx, go_app.UserAddRequest_builder{
			Tenant: go_app.TenantByAlias("acme"),
			Alias:  "john",
		}.Build())
		x.ErrCode(codes.NotFound, err)
	}))
	t.Run("alias is taken in the tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		c.CreateUser(ctx, x, tenant.Ref(), "john")

		_, err := c.User().Add(ctx, go_app.UserAddRequest_builder{
			Tenant: tenant.Ref(),
			Alias:  "john",
		}.Build())
		x.ErrCode(codes.AlreadyExists, err)
	}))
	t.Run("alias is free in another tenant", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")

		u := c.CreateUser(ctx, x, acme.Ref(), "john")
		v := c.CreateUser(ctx, x, hooli.Ref(), "john")
		x.NotEqual(u.GetId(), v.GetId())
	}))
}

func TestUserList(t *testing.T) {
	aliases := func(res *go_app.UserListResponse) []string {
		vs := []string{}
		for _, v := range res.GetItems() {
			vs = append(vs, v.GetAlias())
		}

		return vs
	}

	t.Run("every user if there is no filter", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")
		c.CreateUser(ctx, x, acme.Ref(), "john")
		c.CreateUser(ctx, x, acme.Ref(), "jane")
		c.CreateUser(ctx, x, hooli.Ref(), "erlich")

		v, err := c.User().List(ctx, &go_app.UserListRequest{})
		x.NoError(err)
		x.ElementsMatch([]string{"john", "jane", "erlich"}, aliases(v))
	}))
	t.Run("nothing if there is no user", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.User().List(ctx, &go_app.UserListRequest{})
		x.NoError(err)
		x.Empty(v.GetItems())
	}))
	t.Run("the users the filters point at", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		john := c.CreateUser(ctx, x, acme.Ref(), "john")
		c.CreateUser(ctx, x, acme.Ref(), "jane")
		erlich := c.CreateUser(ctx, x, acme.Ref(), "erlich")

		v, err := c.User().List(ctx, go_app.UserListRequest_builder{
			Filters: []*go_app.UserFilter{
				go_app.UserFilter_builder{Ref: john.Ref()}.Build(),
				go_app.UserFilter_builder{Ref: go_app.UserBySlug("erlich", acme.Ref())}.Build(),
			},
		}.Build())
		x.NoError(err)
		x.ElementsMatch([]string{"john", "erlich"}, aliases(v))
		x.Equal(john.GetId(), v.GetItems()[0].GetId())
		x.NotEqual(erlich.GetId(), john.GetId())
	}))
	t.Run("a filter without a key is rejected", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.User().List(ctx, go_app.UserListRequest_builder{
			Filters: []*go_app.UserFilter{{}},
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}

func TestUserGet(t *testing.T) {
	t.Run("by slug", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateUser(ctx, x, tenant.Ref(), "john")

		u, err := c.User().Get(ctx, go_app.UserGetBySlug("john", tenant.Ref()))
		x.NoError(err)
		x.Equal(v.GetId(), u.GetId())
	}))
	t.Run("with the tenant it belongs to", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		c.CreateUser(ctx, x, tenant.Ref(), "john")

		// Edges are not read unless they are selected.
		req := go_app.UserGetBySlug("john", tenant.Ref()).
			WithSelect(func(s *go_app.UserSelect) {
				s.SetTenant(go_app.TenantSelect_builder{All: z.Ptr(true)}.Build())
			})

		u, err := c.User().Get(ctx, req)
		x.NoError(err)
		x.Equal("acme", u.GetTenant().GetAlias())
	}))
	t.Run("not found", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")

		_, err := c.User().Get(ctx, go_app.UserGetBySlug("john", tenant.Ref()))
		x.ErrCode(codes.NotFound, err)
	}))
}
