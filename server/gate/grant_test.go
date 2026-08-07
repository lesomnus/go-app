package gate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/frame"
)

// as answers with a context carrying a token that stands for `who` and allows
// `grant` of what they allow.
func as(ctx context.Context, x *ox.X, c *ox.Client, who *go_app.Holder, grant frame.Grant) context.Context {
	x.TB().Helper()

	token := "t-" + who.GetAlias() + "-" + uuid.NewString()
	c.Server.Tokens.Add(token, who.Ref(), grant, time.Time{})

	return c.AsBearer(ctx, token)
}

func id(x *ox.X, v *go_app.Tenant) uuid.UUID {
	x.TB().Helper()

	k, err := uuid.FromBytes(v.GetId())
	x.NoError(err)

	return k
}

func TestGrantNarrowsActions(t *testing.T) {
	t.Run("what it names is served", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		read := as(ctx, x, c, p.john, frame.Whole().To(
			go_app.HolderService_Get_FullMethodName,
		))

		v, err := c.Holder().Get(read, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.Equal(p.john.GetId(), v.GetId())
	}))

	t.Run("what it does not name is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		read := as(ctx, x, c, p.john, frame.Whole().To(
			go_app.HolderService_Get_FullMethodName,
		))

		// The Holder may do this; the credential was not made for it. Refused
		// in front of the handler, so nothing was read and nothing was written.
		name := "Johnny"
		_, err := c.Holder().Patch(read, go_app.HolderPatchRequest_builder{
			Ref:  p.john.Ref(),
			Name: &name,
		}.Build())
		x.ErrCode(codes.PermissionDenied, err)

		u, err := c.Bare().Holder().Get(ctx, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.Equal(p.john.GetName(), u.GetName())
	}))

	t.Run("a credential that names no action allows none", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// Which is also what a store that forgot to fill a Grant in hands out:
		// the zero value allows nothing, so the failure is one somebody sees.
		none := as(ctx, x, c, p.john, frame.Grant{})

		_, err := c.Holder().Get(none, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.PermissionDenied, err)
	}))
}

func TestGrantNarrowsTenants(t *testing.T) {
	t.Run("naming what its holder may see changes nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// John may see acme, and so may this credential.
		only := as(ctx, x, c, p.john, frame.Whole().In(id(x, p.acme)))

		v, err := c.Tenant().Get(only, go_app.TenantGetByAlias("acme"))
		x.NoError(err)
		x.Equal(p.acme.GetId(), v.GetId())
	}))

	t.Run("a list is narrowed by it too", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// What john sees with a credential that narrows nothing.
		whole, err := c.Holder().List(c.AsHolder(ctx, p.john), &go_app.HolderListRequest{})
		x.NoError(err)
		x.Len(whole.GetItems(), 2, "acme's admin, and john")

		// And with one that names a Tenant he is not in, which leaves nothing
		// in both.
		only := as(ctx, x, c, p.john, frame.Whole().In(id(x, p.hooli)))

		vs, err := c.Holder().List(only, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Empty(vs.GetItems())
	}))

	// The whole of what an attenuation is: it takes away and never adds.
	t.Run("it cannot reach what its holder cannot", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// John may see acme. The credential says hooli, which is not his to
		// give himself.
		reach := as(ctx, x, c, p.john, frame.Whole().In(id(x, p.hooli)))

		_, err := c.Tenant().Get(reach, go_app.TenantGetByAlias("hooli"))
		x.ErrCode(codes.NotFound, err)

		// And it has narrowed him out of his own, which is the honest result of
		// meeting two sets that do not overlap.
		_, err = c.Tenant().Get(reach, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)
	}))

	t.Run("a credential that names no tenant sees none", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// An empty set is empty. Read the other way -- as "no narrowing" -- it
		// would be a credential that saw everything by saying nothing.
		none := as(ctx, x, c, p.john, frame.Whole().In())

		_, err := c.Tenant().Get(none, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)

		vs, err := c.Holder().List(none, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Empty(vs.GetItems())
	}))
}

func TestGrantWhole(t *testing.T) {
	t.Run("a header carries none, and narrows nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// `Plain` has nowhere to put an attenuation, so it says so rather than
		// leaving the zero Grant -- which would refuse every call this app is
		// tested with.
		v, err := c.Holder().Get(c.AsHolder(ctx, p.john), go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.Equal(p.john.GetId(), v.GetId())
	}))
}
