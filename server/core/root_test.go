package core_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
)

// noHolders is a server that refuses to add a Holder, and does everything else
// the way the one behind it does.
type noHolders struct {
	go_app.Overlay
}

func (s noHolders) Holder() go_app.HolderServiceServer {
	return noHolderService{s.Next().Holder()}
}

type noHolderService struct {
	go_app.HolderServiceServer
}

func (noHolderService) Add(context.Context, *go_app.HolderAddRequest) (*go_app.Holder, error) {
	return nil, errors.New("no")
}

func TestRoot(t *testing.T) {
	t.Run("it is there before anything happens", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Tenant().Get(ctx, go_app.TenantGetById(core.RootId[:]))
		x.NoError(err)
		x.Equal(core.RootAlias, v.GetAlias())

		// The same one, whichever way it is asked for.
		u, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias(core.RootAlias))
		x.NoError(err)
		x.Equal(v.GetId(), u.GetId())
		x.Equal(core.RootId[:], u.GetId())
	}))
	t.Run("somebody administers it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Holder().Get(ctx, go_app.HolderGetBySlug(core.AdminAlias, go_app.TenantById(core.RootId[:])))
		x.NoError(err)
		x.Equal(core.AdminAlias, v.GetAlias())
	}))
	t.Run("asking for it again changes nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		before, err := c.Tenant().Get(ctx, go_app.TenantGetById(core.RootId[:]))
		x.NoError(err)

		// What every start does, around the gate: there is nobody to be when a
		// deployment is made.
		after, err := core.EnsureRoot(ctx, core.NewServer(c.Server.Sink))
		x.NoError(err)
		x.Equal(before.GetDateCreated().AsTime(), after.GetDateCreated().AsTime())

		v, err := c.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Len(v.GetItems(), 1)
	}))
	t.Run("it cannot be added twice", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Tenant().Add(ctx, core.Root())
		x.ErrCode(codes.AlreadyExists, err)
	}))
}

func TestAdmin(t *testing.T) {
	t.Run("a tenant comes with one", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tenant := c.CreateTenant(ctx, x, "acme")

		v, err := core.Admin(ctx, core.NewServer(c.Server.Sink), tenant.Ref())
		x.NoError(err)
		x.Equal(core.AdminAlias, v.GetAlias())
		x.Equal("Admin", v.GetName())
	}))
	t.Run("a tenant that was not added by the rules has none", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// The bare server writes the row and nothing else, which is how a
		// tenant without an admin comes about.
		v, err := c.Bare().Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "acme"}.Build())
		x.NoError(err)

		_, err = core.Admin(ctx, core.NewServer(c.Server.Sink), v.Ref())
		x.ErrorIs(err, core.ErrNoAdmin)
	}))
	t.Run("the tenant is taken back if it cannot be given one", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// A stack whose holders cannot be added, which is the one thing a real
		// database will not do on demand.
		sink, err := bare.NewServer(c.Server.Db)
		x.NoError(err)
		s := core.NewServer(noHolders{go_app.NewOverlay(sink)})

		_, err = s.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: "acme"}.Build())
		x.ErrorContains(err, "add the admin holder")

		// Half a tenant is not left behind.
		_, err = c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)
	}))
}
