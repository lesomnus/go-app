package core_test

import (
	"context"
	"testing"

	"github.com/lesomnus/protobuf-patch/patch"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

func TestTenantApply(t *testing.T) {
	t.Run("a document changes what it names", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		v, err := c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("name")).Assign(patch.Str("Acme, Inc.")),
			),
		}.Build())
		x.NoError(err)
		x.Equal("Acme, Inc.", v.GetName())
		x.Equal("acme", v.GetAlias())
	}))
	t.Run("one map entry is edited without the rest being read", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		v, err := c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.MapStr("tier")).In(patch.Name("labels")).Assign(patch.Str("free")),
			),
		}.Build())
		x.NoError(err)
		x.Equal(map[string]string{"tier": "free"}, v.GetLabels())

		// This is what a PatchRequest has no shape for: the other entry is
		// still there, and it was never sent.
		v, err = c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.MapStr("region")).In(patch.Name("labels")).Assign(patch.Str("eu")),
			),
		}.Build())
		x.NoError(err)
		x.Equal(map[string]string{"tier": "free", "region": "eu"}, v.GetLabels())
	}))
	t.Run("a test that does not hold writes nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		_, err := c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("name")).Test(patch.Str("somebody else")),
				patch.Target(patch.Name("name")).Assign(patch.Str("Acme, Inc.")),
			),
		}.Build())
		x.ErrCode(codes.FailedPrecondition, err)

		u, err := c.Tenant().Get(ctx, v.Pick())
		x.NoError(err)
		x.Equal("acme", u.GetName())
	}))
	t.Run("an alias is taken as it is spelled", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v := c.CreateTenant(ctx, x, "acme")

		// A document states an operation rather than a value to be tidied up,
		// so `core` refuses a spelling it would have had to change instead of
		// storing something other than what was asked for.
		_, err := c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("alias")).Assign(patch.Str(" Acme-Corp ")),
			),
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)

		// And it is refused outright when no spelling of it would do.
		_, err = c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("alias")).Assign(patch.Str("acme corp")),
			),
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)

		// Said the way it is stored, it goes through.
		u, err := c.Tenant().Apply(ctx, go_app.TenantApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Tenant",
				patch.Target(patch.Name("alias")).Assign(patch.Str("acme-corp")),
			),
		}.Build())
		x.NoError(err)
		x.Equal("acme-corp", u.GetAlias())
	}))
}

func TestHolderApply(t *testing.T) {
	t.Run("a document changes what it names", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		v, err := c.Holder().Apply(ctx, go_app.HolderApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Holder",
				patch.Target(patch.Name("desc")).Assign(patch.Str("the one who asks")),
			),
		}.Build())
		x.NoError(err)
		x.Equal("the one who asks", v.GetDesc())
	}))
	t.Run("an alias is taken as it is spelled", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tenant.Ref(), "john")

		_, err := c.Holder().Apply(ctx, go_app.HolderApplyRequest_builder{
			Ref: v.Ref(),
			Patch: patch.MustNew("go_app.Holder",
				patch.Target(patch.Name("alias")).Assign(patch.Str("John")),
			),
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}
