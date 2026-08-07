package gate_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/internal/ox"
)

// counted serves the app with `l` counting the calls and answers with a client
// of it, the way `cmd/serve.go` would have built one.
//
// It is made after the test has arranged what it is about, so that putting the
// Tenants up is not what spends the tokens.
func counted(x *ox.X, c *ox.Client, l grpcx.Limiter) *ox.Client {
	x.TB().Helper()

	c.Server.Limit = l
	v := ox.NewClient(x.TB(), c.Server)
	x.TB().Cleanup(func() { v.Close() })

	return v
}

func TestLimit(t *testing.T) {
	t.Run("a caller past their line is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// One a second, one at a time: the second call of a test is over the
		// line however fast the machine is.
		d := counted(x, c, grpcx.NewLimiter(1, 1))
		as := d.AsHolder(ctx, p.john)

		_, err := d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)

		// ResourceExhausted and not PermissionDenied: nothing about what john
		// may do changed, and the same call a moment later is served.
		_, err = d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.ResourceExhausted, err)
	}))

	t.Run("the refusal says how long to wait", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		d := counted(x, c, grpcx.NewLimiter(1, 1))
		as := d.AsHolder(ctx, p.john)

		_, err := d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		_, err = d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))

		// Over the wire and out the other side, since a detail a client cannot
		// read is a client that asks again at once.
		s, ok := status.FromError(err)
		x.True(ok)

		var retry time.Duration
		for _, v := range s.Details() {
			if v, ok := v.(*errdetails.RetryInfo); ok {
				retry = v.GetRetryDelay().AsDuration()
			}
		}
		x.Positive(retry)
		x.LessOrEqual(retry, time.Second)
	}))

	t.Run("one tenant does not spend another's", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		d := counted(x, c, grpcx.NewLimiter(1, 1))

		john := d.AsHolder(ctx, p.john)
		_, err := d.Holder().Get(john, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		_, err = d.Holder().Get(john, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.ResourceExhausted, err)

		// The whole of what a per-Tenant limit is for: acme having a bad
		// afternoon is not hooli's problem.
		erlich := d.AsHolder(ctx, p.erlich)
		_, err = d.Holder().Get(erlich, go_app.HolderGetById(p.erlich.GetId()))
		x.NoError(err)
	}))

	t.Run("everybody in a tenant shares one", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		d := counted(x, c, grpcx.NewLimiter(1, 1))

		john := d.AsHolder(ctx, p.john)
		_, err := d.Holder().Get(john, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)

		// The other half of the same decision, and the one worth a test:
		// counting per Holder would be a limit anybody could raise by adding
		// another Holder.
		admin := d.AsAdminOf(ctx, x, p.acme)
		_, err = d.Holder().Get(admin, go_app.HolderGetById(p.john.GetId()))
		x.ErrCode(codes.ResourceExhausted, err)
	}))

	t.Run("nothing is counted when nothing counts", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// What a deployment that wrote no rate gets: the chain it had before,
		// and not an interceptor that says yes.
		d := counted(x, c, nil)
		as := d.AsHolder(ctx, p.john)

		for range 4 {
			_, err := d.Holder().Get(as, go_app.HolderGetById(p.john.GetId()))
			x.NoError(err)
		}
	}))
}
