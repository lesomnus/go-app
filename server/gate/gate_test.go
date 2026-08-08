package gate_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/frame"
	"github.com/lesomnus/go-app/server/gate"
)

// behind serves the app with the given options and answers with a client of it,
// the way `cmd/serve.go` would have built one.
func behind(x *ox.X, c *ox.Client, opts ...gate.Option) *ox.Client {
	x.TB().Helper()

	c.Server.Gate = opts
	v := ox.NewClient(x.TB(), c.Server)
	x.TB().Cleanup(func() { v.Close() })

	return v
}

func TestAnonymousReads(t *testing.T) {
	x := require.New(t)

	// The generated reads, by their suffix.
	x.True(gate.AnonymousReads(go_app.CoffeeService_Get_FullMethodName))
	x.True(gate.AnonymousReads(go_app.CoffeeService_List_FullMethodName))
	x.True(gate.AnonymousReads(go_app.CoffeeService_Watch_FullMethodName))

	x.False(gate.AnonymousReads(go_app.CoffeeService_Add_FullMethodName))
	x.False(gate.AnonymousReads(go_app.CoffeeService_Erase_FullMethodName))
	x.False(gate.AnonymousReads(go_app.CoffeeService_Patch_FullMethodName))

	// And an RPC written by hand is closed until somebody opens it, which is
	// the whole reason this is a list of what is *allowed*: `Rename` is a
	// write, it is not spelled `Patch`, and a rule that named the writes would
	// have let it through.
	x.False(gate.AnonymousReads("/go_app.CoffeeService/Rename"))
}

func TestAnonymous(t *testing.T) {
	t.Run("nobody may do anything unless it was said", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		d := behind(x, c)
		as := d.AsNobody(ctx)

		// Unauthenticated and not PermissionDenied: the two say different
		// things to do about it, and this one is fixed by saying who you are.
		_, err := d.Roaster().Get(as, go_app.RoasterGetById(beans.GetId()))
		x.ErrCode(codes.Unauthenticated, err)
	}))

	t.Run("what was said is what they may do", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		d := behind(x, c, gate.WithAnonymous(gate.AnonymousReads))
		as := d.AsNobody(ctx)

		v, err := d.Roaster().Get(as, go_app.RoasterGetById(beans.GetId()))
		x.NoError(err)
		x.Equal(beans.GetId(), v.GetId())

		// And no more than that. A catalogue anybody may read and only a caller
		// who said who they are may change is the shape most of this is for.
		_, err = d.Roaster().Add(as, go_app.RoasterAddRequest_builder{Alias: "peaks"}.Build())
		x.ErrCode(codes.Unauthenticated, err)
	}))

	t.Run("saying who you are is what changes it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		d := behind(x, c)

		// The same call, by somebody. Nothing here asks *which* somebody --
		// that is a `gate.Policy`, and none is injected.
		_, err := d.Roaster().Add(d.As(ctx, "anna"),
			go_app.RoasterAddRequest_builder{Alias: "peaks"}.Build())
		x.NoError(err)
	}))
}

// policy is one a deployment might inject.
type policy struct {
	err  error
	saw  []gate.Call
	nils int
}

func (p *policy) May(_ context.Context, c gate.Call) error {
	p.saw = append(p.saw, c)
	if p.err == nil {
		p.nils++
	}

	return p.err
}

func TestPolicy(t *testing.T) {
	t.Run("it is asked once per call, and refuses in its own words", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := &policy{err: status.Error(codes.FailedPrecondition, "not on a tuesday")}
		d := behind(x, c, gate.WithPolicy(p))

		_, err := d.Roaster().Add(d.As(ctx, "anna"),
			go_app.RoasterAddRequest_builder{Alias: "beans"}.Build())

		// What it answered, as it answered it: it is the only thing here that
		// knows why.
		x.ErrCode(codes.FailedPrecondition, err)
		x.ErrorContains(err, "tuesday")

		// Once, for a call that makes several queries behind it.
		x.Len(p.saw, 1)
		x.Equal(go_app.RoasterService_Add_FullMethodName, p.saw[0].Action)
		x.Equal("anna", p.saw[0].Actor.Subject)
	}))

	t.Run("an anonymous caller reaches it only if they were let in", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		// Refused in front of it, so it is never asked: what [gate.Anonymous]
		// did not name is not a question for a policy.
		p := &policy{}
		d := behind(x, c, gate.WithPolicy(p))
		_, err := d.Roaster().Get(d.AsNobody(ctx), go_app.RoasterGetById(beans.GetId()))
		x.ErrCode(codes.Unauthenticated, err)
		x.Empty(p.saw)

		// And with the reads open, the policy is asked about them -- as
		// [frame.Anonymous], which it may have something to say about.
		q := &policy{}
		e := behind(x, c, gate.WithPolicy(q), gate.WithAnonymous(gate.AnonymousReads))
		_, err = e.Roaster().Get(e.AsNobody(ctx), go_app.RoasterGetById(beans.GetId()))
		x.NoError(err)
		x.Len(q.saw, 1)
		x.Equal(frame.Anonymous, q.saw[0].Actor)
	}))
}
