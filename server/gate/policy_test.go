package gate_test

import (
	"context"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/frame"
	"github.com/lesomnus/go-app/server/gate"
)

// policy is one a deployment might inject: it answers what a test told it to,
// and counts how often it was asked.
type policy struct {
	mays   atomic.Int64
	wheres atomic.Int64

	// may and where are what it answers. Nil is yes, and everything.
	may   func(c gate.Call) error
	where func(c gate.Call) (frame.Tenants, error)
}

var _ gate.Policy = (*policy)(nil)

func (p *policy) May(_ context.Context, c gate.Call) error {
	p.mays.Add(1)
	if p.may == nil {
		return nil
	}

	return p.may(c)
}

func (p *policy) Where(_ context.Context, c gate.Call) (frame.Tenants, error) {
	p.wheres.Add(1)
	if p.where == nil {
		return frame.Everything, nil
	}

	return p.where(c)
}

// behind serves the app with `p` injected and answers with a client of it, the
// way `cmd/serve.go` would have built one.
func behind(x *ox.X, c *ox.Client, p *policy) *ox.Client {
	x.TB().Helper()

	c.Server.Policy = p
	v := ox.NewClient(x.TB(), c.Server)
	x.TB().Cleanup(func() { v.Close() })

	return v
}

func TestPolicyIsConsulted(t *testing.T) {
	t.Run("a refusal is the caller's answer", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		pc := behind(x, c, &policy{
			may: func(gate.Call) error {
				return status.Error(codes.PermissionDenied, "not today")
			},
		})

		_, err := pc.Holder().List(ctx, &go_app.HolderListRequest{})
		x.ErrCode(codes.PermissionDenied, err)
	}))

	t.Run("it is told who asked and for what", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		var got gate.Call
		pc := behind(x, c, &policy{
			may: func(c gate.Call) error {
				got = c
				return nil
			},
		})

		_, err := pc.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)

		x.Equal(go_app.HolderService_List_FullMethodName, got.Action)
		x.Equal(c.Server.Admin.GetId(), got.Actor.GetId())
	}))

	// The reason it is an interceptor and not one of the hooks [gate.Wall]
	// installs. Those run once per query, and a Get that asks for the Tenant
	// too runs more than one -- so a policy asked from in there is asked
	// several times for one call, over a network if it has one.
	t.Run("once per request, however many queries that is", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		tn := c.CreateTenant(ctx, x, "acme")
		v := c.CreateHolder(ctx, x, tn.Ref(), "john")

		p := &policy{}
		pc := behind(x, c, p)

		_, err := pc.Holder().Get(ctx, go_app.HolderGetById(v.GetId()).
			WithSelect(func(s *go_app.HolderSelect) {
				s.SetAll(true)
				s.SetTenant(go_app.TenantSelect_builder{}.Build())
			}))
		x.NoError(err)

		x.EqualValues(1, p.mays.Load())
		x.EqualValues(1, p.wheres.Load())
	}))

	t.Run("what it answers is what every read is narrowed by", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		pr := setup(ctx, x, c)

		// The root admin, who without a policy sees every Tenant.
		pc := behind(x, c, &policy{
			where: func(gate.Call) (frame.Tenants, error) {
				return frame.Only(id(x, pr.hooli)), nil
			},
		})

		vs, err := pc.Holder().List(ctx, &go_app.HolderListRequest{})
		x.NoError(err)

		as := []string{}
		for _, u := range vs.GetItems() {
			as = append(as, u.GetAlias())
		}
		x.ElementsMatch([]string{"admin", "erlich"}, as, "hooli's, because the policy said so")

		// And a generated read is narrowed by the same answer.
		_, err = pc.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.ErrCode(codes.NotFound, err)
	}))

	// A credential can only take away from what a policy allows, which is the
	// same rule as for the wall and is why the meet happens after.
	t.Run("a credential still narrows what it answers", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		pr := setup(ctx, x, c)

		pc := behind(x, c, &policy{
			where: func(gate.Call) (frame.Tenants, error) {
				return frame.Only(id(x, pr.hooli)), nil
			},
		})

		// The policy says hooli; the credential says acme. Neither is in both.
		narrow := as(ctx, x, pc, c.Server.Admin, frame.Whole().In(id(x, pr.acme)))

		vs, err := pc.Holder().List(narrow, &go_app.HolderListRequest{})
		x.NoError(err)
		x.Empty(vs.GetItems())
	}))
}
