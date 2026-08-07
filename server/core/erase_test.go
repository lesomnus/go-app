package core_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/tenant"
	"github.com/lesomnus/go-app/internal/ox"
)

// row reads a Holder straight out of the database, past every rule, which is
// the only way to see one that was erased.
func row(ctx context.Context, x *ox.X, c *ox.Client, v *go_app.Holder) *ent.Holder {
	x.TB().Helper()

	k, err := uuid.FromBytes(v.GetId())
	x.NoError(err)

	u, err := c.Server.Db.Holder.Get(ctx, k)
	x.NoError(err)

	return u
}

func TestEraseHolder(t *testing.T) {
	t.Run("the row stays and says it is gone", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		_, err := c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		_, err = c.Holder().Get(ctx, go_app.HolderGetById(v.GetId()))
		x.ErrCode(codes.NotFound, err)

		x.NotNil(row(ctx, x, c, v).DateErased)
	}))

	t.Run("it is gone from a list too", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		// The list is written by hand, so it is the read that would have been
		// left behind had it asked the scope hook rather than HolderNarrow.
		before, err := c.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)

		_, err = c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		after, err := c.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Len(after.GetItems(), len(before.GetItems())-1)

		for _, u := range after.GetItems() {
			x.NotEqual(v.GetId(), u.GetId())
		}
	}))

	// The reason a Holder is erased softly at all. Every row of the trail names
	// the Holder that did something; deleted, that identifier answers to
	// nothing and the trail goes on saying who while nobody can find out who.
	t.Run("the trail can still say who it was", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		name := "Johnny"
		_, err := c.Holder().Patch(c.AsHolder(ctx, v), go_app.HolderPatchRequest_builder{
			Ref:  v.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		_, err = c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		vs, err := c.Audit().List(ctx, go_app.AuditListRequest_builder{
			Filters: []*go_app.AuditFilter{
				go_app.AuditFilter_builder{ObjectId: v.GetId()}.Build(),
			},
		}.Build())
		x.NoError(err)
		x.NotEmpty(vs.GetItems())

		// The identifier the trail holds still answers to a row, and the row
		// still says which Holder it was.
		x.Equal("john", row(ctx, x, c, v).Alias)
	}))

	t.Run("an erased holder cannot say who is calling", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		as := c.AsHolder(ctx, v)
		_, err := c.Holder().Get(as, go_app.HolderGetById(v.GetId()))
		x.NoError(err, "while they are here")

		_, err = c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		// `server/auth` looks a Holder up to work out who is calling, and that
		// read is narrowed like every other one. A credential that named
		// somebody who has been erased names nobody.
		_, err = c.Holder().Get(as, go_app.HolderGetById(v.GetId()))
		x.ErrCode(codes.Unauthenticated, err)
	}))

	t.Run("the alias comes free again", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		_, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{
			Tenant: tn.Ref(),
			Alias:  "john",
		}.Build())
		x.ErrCode(codes.AlreadyExists, err)

		_, err = c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		u := c.CreateHolder(ctx, x, tn.Ref(), "john")
		x.NotEqual(v.GetId(), u.GetId())

		// And the new one is the one that answers to the name.
		w, err := c.Holder().Get(ctx, go_app.HolderGetBySlug("john", tn.Ref()))
		x.NoError(err)
		x.Equal(u.GetId(), w.GetId())
	}))

	t.Run("erasing it again erases nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		_, err := c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)
		first := *row(ctx, x, c, v).DateErased

		_, err = c.Holder().Erase(ctx, v.Ref())
		x.NoError(err)

		x.True(first.Equal(*row(ctx, x, c, v).DateErased),
			"the second call matched nothing, so it stamped nothing")
	}))
}

// TestEraseTenant is the other half of the decision: a Tenant is taken away.
func TestEraseTenant(t *testing.T) {
	t.Run("a tenant is erased for real", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")

		_, err := c.Tenant().Erase(ctx, tn.Ref())
		x.NoError(err)

		k, err := uuid.FromBytes(tn.GetId())
		x.NoError(err)

		// Not there at all, rather than there and saying it is gone.
		n, err := c.Server.Db.Tenant.Query().Where(tenant.IDEQ(k)).Count(ctx)
		x.NoError(err)
		x.Zero(n)
	}))

	// The cost of soft deletion at the join between two entities, and the
	// reason `core` has anything to say about erasing a Tenant at all. A
	// Holder erased softly keeps its row, and the row keeps a foreign key to
	// its Tenant -- so without the cascade, a Tenant that ever had a Holder
	// could never be erased, however many of them had been "erased" first.
	t.Run("it takes its holders with it, erased ones included", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		live := c.CreateHolder(ctx, x, tn.Ref(), "john")
		gone := c.CreateHolder(ctx, x, tn.Ref(), "erlich")

		_, err := c.Holder().Erase(ctx, gone.Ref())
		x.NoError(err)

		// A row is still there for the erased one, which is the whole point of
		// erasing softly -- and it is what would hold the foreign key.
		x.NotNil(row(ctx, x, c, gone).DateErased)

		_, err = c.Tenant().Erase(ctx, tn.Ref())
		x.NoError(err)

		k, err := uuid.FromBytes(tn.GetId())
		x.NoError(err)

		n, err := c.Server.Db.Holder.Query().Where(holder.HasTenantWith(tenant.IDEQ(k))).Count(ctx)
		x.NoError(err)
		x.Zero(n, "the admin, the live one and the erased one, all gone")

		// And the live one is not reachable either, which it would be if the
		// cascade had only taken the ones that were already stamped.
		_, err = c.Holder().Get(ctx, go_app.HolderGetById(live.GetId()))
		x.ErrCode(codes.NotFound, err)
	}))

	t.Run("erasing one that is not there succeeds", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Tenant().Erase(ctx, go_app.TenantByAlias("nobody"))
		x.NoError(err)
	}))
}
