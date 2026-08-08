package watch_test

import (
	"context"
	"testing"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

// watchingRoasters is [watching] for Roasters.
func watchingRoasters(ctx context.Context, x *ox.X, c *ox.Client, fs ...*go_app.RoasterFilter) (func() []*go_app.RoasterWatchItem, func()) {
	x.TB().Helper()

	stream, err := c.Roaster().Watch(ctx, go_app.RoasterWatchRequest_builder{Filters: fs}.Build())
	x.NoError(err)

	return recving(x, func() ([]*go_app.RoasterWatchItem, error) {
		res, err := stream.Recv()
		return res.GetItems(), err
	})
}

func TestRoasterWatch(t *testing.T) {
	t.Run("a caller is told about their own", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		next, _ := watchingRoasters(ctx, x, c)

		vs := next()
		x.Len(vs, 1)
		x.Equal(beans.GetId(), vs[0].GetId())

		name := "Beans & Co."
		_, err := c.Roaster().Patch(ctx, go_app.RoasterPatchRequest_builder{
			Ref:  beans.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		vs = next()
		x.Len(vs, 1)
		x.Equal("Beans & Co.", vs[0].GetValue().GetName())
		x.Equal(go_app.RoasterService_Patch_FullMethodName, vs[0].GetAction())

		// And another Roaster put up beside it arrives too: there is no wall
		// here, and a watcher who wants one Roaster names it in the filters.
		peaks := c.CreateRoaster(ctx, x, "peaks")
		x.Equal(peaks.GetId(), next()[0].GetId())

		// Which is what naming it does.
		one, quiet := watchingRoasters(ctx, x, c, go_app.RoasterFilter_builder{
			Ref: go_app.RoasterById(beans.GetId()),
		}.Build())
		x.Equal(beans.GetId(), one()[0].GetId())

		c.CreateRoaster(ctx, x, "hills")
		quiet()
	}))

	t.Run("one that was taken down arrives with no value", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		next, _ := watchingRoasters(ctx, x, c)
		next()

		// A Roaster is erased for real rather than softly, so absence is the only
		// thing there could ever have been to say about one that is gone. It is
		// taken down from outside, since it is not something asked for from
		// inside one.
		_, err := c.Roaster().Erase(ctx, beans.Ref())
		x.NoError(err)

		vs := next()
		x.Len(vs, 1)
		x.Equal(beans.GetId(), vs[0].GetId())
		x.Nil(vs[0].GetValue())
	}))
}
