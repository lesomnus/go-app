package watch_test

import (
	"context"
	"testing"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

// watchingTenants is [watching] for Tenants.
func watchingTenants(ctx context.Context, x *ox.X, c *ox.Client, fs ...*go_app.TenantFilter) (func() []*go_app.TenantWatchItem, func()) {
	x.TB().Helper()

	stream, err := c.Tenant().Watch(ctx, go_app.TenantWatchRequest_builder{Filters: fs}.Build())
	x.NoError(err)

	return recving(x, func() ([]*go_app.TenantWatchItem, error) {
		res, err := stream.Recv()
		return res.GetItems(), err
	})
}

func TestTenantWatch(t *testing.T) {
	t.Run("a caller is told about their own", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		as := c.AsAdminOf(ctx, x, acme)

		next, quiet := watchingTenants(as, x, c)

		// For most callers the wall makes this exactly one, and the stream is a
		// way of being told when their own is renamed or taken away.
		vs := next()
		x.Len(vs, 1)
		x.Equal(acme.GetId(), vs[0].GetId())

		name := "Acme, Inc."
		_, err := c.Tenant().Patch(as, go_app.TenantPatchRequest_builder{
			Ref:  acme.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs = next()
		x.Len(vs, 1)
		x.Equal("Acme, Inc.", vs[0].GetValue().GetName())
		x.Equal(go_app.TenantService_Patch_FullMethodName, vs[0].GetAction())

		// And another Tenant put up beside it is not theirs to hear about.
		c.CreateTenant(ctx, x, "hooli")
		quiet()
	}))

	t.Run("one that was taken down arrives with no value", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		as := c.AsAdminOf(ctx, x, acme)

		next, _ := watchingTenants(as, x, c)
		next()

		// A Tenant is erased for real rather than softly, so absence is the only
		// thing there could ever have been to say about one that is gone. It is
		// taken down from outside, since it is not something asked for from
		// inside one.
		_, err := c.Ungated().Tenant().Erase(ctx, acme.Ref())
		x.NoError(err)

		vs := next()
		x.Len(vs, 1)
		x.Equal(acme.GetId(), vs[0].GetId())
		x.Nil(vs[0].GetValue())
	}))
}
