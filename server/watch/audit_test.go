package watch_test

import (
	"context"
	"testing"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

// watchingTrail is [watching] for the trail.
func watchingTrail(ctx context.Context, x *ox.X, c *ox.Client, fs ...*go_app.AuditFilter) (func() []*go_app.Audit, func()) {
	x.TB().Helper()

	stream, err := c.Audit().Watch(ctx, go_app.AuditWatchRequest_builder{Filters: fs}.Build())
	x.NoError(err)

	return recving(x, func() ([]*go_app.Audit, error) {
		res, err := stream.Recv()
		return res.GetItems(), err
	})
}

func TestAuditWatch(t *testing.T) {
	t.Run("rows arrive as they are written, and nothing before them", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		as := c.AsAdminOf(ctx, x, acme)

		// Whatever the setup wrote is already on the trail, and a stream that
		// opens now is told none of it: there is nothing to converge on, and
		// what happened before is what List is for.
		next, quiet := watchingTrail(as, x, c)
		quiet()

		// By acme's admin, so the row of the trail is acme's to read: the
		// trail is walled by who was acting and not by what was touched.
		_, err := c.Holder().Add(as, go_app.HolderAddRequest_builder{
			Tenant: acme.Ref(),
			Alias:  "john",
		}.Build())
		x.NoError(err)

		vs := next()
		x.Len(vs, 1)
		x.Equal(go_app.HolderService_Add_FullMethodName, vs[0].GetAction())
		x.Equal(acme.GetId(), vs[0].GetTenantId())
	}))

	t.Run("what another tenant did is never sent", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		hooli := c.CreateTenant(ctx, x, "hooli")

		as := c.AsAdminOf(ctx, x, acme)
		next, quiet := watchingTrail(as, x, c)

		// Written by hooli's admin, so the row is hooli's. The wall on the
		// trail is the actor's Tenant, and the read is what applies it.
		hooli_admin := c.AsAdminOf(ctx, x, hooli)
		_, err := c.Holder().Add(hooli_admin, go_app.HolderAddRequest_builder{
			Tenant: hooli.Ref(),
			Alias:  "erlich",
		}.Build())
		x.NoError(err)
		quiet()

		_, err = c.Holder().Add(as, go_app.HolderAddRequest_builder{
			Tenant: acme.Ref(),
			Alias:  "john",
		}.Build())
		x.NoError(err)
		x.Len(next(), 1, "and their own still arrives")
	}))

	t.Run("what the filters do not name is never sent", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		as := c.AsAdminOf(ctx, x, acme)

		john := c.CreateHolder(ctx, x, acme.Ref(), "john")
		jane := c.CreateHolder(ctx, x, acme.Ref(), "jane")

		next, quiet := watchingTrail(as, x, c, go_app.AuditFilter_builder{
			ObjectId: john.GetId(),
		}.Build())

		name := "Janey"
		_, err := c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  jane.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)
		quiet()

		name = "Johnny"
		_, err = c.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:  john.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs := next()
		x.Len(vs, 1)
		x.Equal(john.GetId(), vs[0].GetObjectId())
	}))
}
