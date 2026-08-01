package core_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/core"
)

func TestTenantAdd(t *testing.T) {
	t.Run("alias is normalized", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "  Acme-Corp "}.Build())
		x.NoError(err)
		x.Equal("acme-corp", v.GetAlias())
	}))
	t.Run("name falls back to the alias", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "acme"}.Build())
		x.NoError(err)
		x.Equal("acme", v.GetName())
	}))
	t.Run("name is kept if given", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "acme", Name: "Acme, Inc."}.Build())
		x.NoError(err)
		x.Equal("Acme, Inc.", v.GetName())
	}))
	t.Run("alias must be a slug", func(t *testing.T) {
		tcs := []struct {
			desc  string
			alias string
		}{
			{desc: "empty", alias: ""},
			{desc: "blank", alias: "   "},
			{desc: "not alphanumeric", alias: "acme corp"},
			{desc: "leading hyphen", alias: "-acme"},
			{desc: "trailing hyphen", alias: "acme-"},
			{desc: "repeated hyphen", alias: "acme--corp"},
			{desc: "too long", alias: strings.Repeat("a", core.AliasMaxLen+1)},
		}
		for _, tc := range tcs {
			t.Run(tc.desc, ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
				_, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: tc.alias}.Build())
				x.ErrCode(codes.InvalidArgument, err)
			}))
		}
	})
	t.Run("alias is taken", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		c.CreateTenant(ctx, x, "acme")

		_, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "ACME"}.Build())
		x.ErrCode(codes.AlreadyExists, err)
	}))
}

func TestTenantPatch(t *testing.T) {
	t.Run("alias is normalized", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		alias := " Acme-Corp "
		v, err := c.Tenant().Patch(ctx, go_app.TenantPatchRequest_builder{Ref: v.Ref(), Alias: &alias}.Build())
		x.NoError(err)
		x.Equal("acme-corp", v.GetAlias())
	}))
	t.Run("alias must be a slug", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		alias := "acme corp"
		_, err := c.Tenant().Patch(ctx, go_app.TenantPatchRequest_builder{Ref: v.Ref(), Alias: &alias}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}

func TestTenantGet(t *testing.T) {
	t.Run("by alias", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		u, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.NoError(err)
		x.Equal(v.GetId(), u.GetId())
	}))
	t.Run("not found", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)
	}))
}

// The bare client skips the rules of `server/core`, which is how a test
// arranges a state the app itself would not create.
func TestTenantBare(t *testing.T) {
	t.Run("rules are not applied", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Bare().Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "ACME CORP"}.Build())
		x.NoError(err)
		x.Equal("ACME CORP", v.GetAlias())
		x.Empty(v.GetName())

		// It is the same database.
		u, err := c.Tenant().Get(ctx, v.Pick())
		x.NoError(err)
		x.Equal(v.GetId(), u.GetId())
	}))
}
