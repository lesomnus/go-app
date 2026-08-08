package watch_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
)

// recving reads a stream on a goroutine of its own -- so that a test which
// expects nothing does not have to block to find out -- and answers with a
// function that takes the next message and one that fails if another arrives.
func recving[T any](x *ox.X, recv func() ([]T, error)) (func() []T, func()) {
	x.TB().Helper()

	type got struct {
		items []T
		err   error
	}
	ch := make(chan got, 8)
	go func() {
		defer close(ch)
		for {
			items, err := recv()
			ch <- got{items, err}
			if err != nil {
				return
			}
		}
	}()

	next := func() []T {
		x.TB().Helper()

		select {
		case v := <-ch:
			x.NoError(v.err)
			return v.items
		case <-time.After(3 * time.Second):
			x.FailNow("nothing was sent")
			return nil
		}
	}
	quiet := func() {
		x.TB().Helper()

		// Long enough that a message on its way would have arrived, since what
		// is under test is that one was not sent at all.
		select {
		case v := <-ch:
			x.NoError(v.err)
			x.FailNow("something was sent", "%v", v.items)
		case <-time.After(200 * time.Millisecond):
		}
	}

	return next, quiet
}

// watching opens a Coffee Watch.
func watching(ctx context.Context, x *ox.X, c *ox.Client, fs ...*go_app.CoffeeFilter) (func() []*go_app.CoffeeWatchItem, func()) {
	x.TB().Helper()

	stream, err := c.Coffee().Watch(ctx, go_app.CoffeeWatchRequest_builder{Filters: fs}.Build())
	x.NoError(err)

	return recving(x, func() ([]*go_app.CoffeeWatchItem, error) {
		res, err := stream.Recv()
		return res.GetItems(), err
	})
}

// only is the one item of a message, failing the test if there are more.
func only(x *ox.X, vs []*go_app.CoffeeWatchItem) *go_app.CoffeeWatchItem {
	x.TB().Helper()

	x.Len(vs, 1)
	return vs[0]
}

func TestCoffeeWatch(t *testing.T) {
	t.Run("the first message is what is already there", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		next, _ := watching(ctx, x, c)

		// A client does not have to List and then subscribe and race the two.
		// What is here is sent first, and it is not something anybody asked
		// for, so no action is said.
		v := only(x, next())
		x.Equal(ethiopia.GetId(), v.GetId())
		x.NotNil(v.GetValue())
		x.Empty(v.GetAction())
	}))

	t.Run("a change is sent as the row is now", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		next, _ := watching(ctx, x, c)
		next() // the snapshot

		name := "Johnny"
		_, err := c.Coffee().Patch(ctx, go_app.CoffeePatchRequest_builder{
			Ref:  ethiopia.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)

		v := only(x, next())
		x.Equal(ethiopia.GetId(), v.GetId())
		x.Equal("Johnny", v.GetValue().GetName(), "the row as it is, not what changed about it")

		// And what the caller asked for, which is what an RPC written by hand
		// would be here under.
		x.Equal(go_app.CoffeeService_Patch_FullMethodName, v.GetAction())
	}))

	t.Run("one that was erased arrives with no value", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		next, _ := watching(ctx, x, c)
		next()

		_, err := c.Coffee().Erase(ctx, ethiopia.Ref())
		x.NoError(err)

		// Absence is the whole of how a removal is said. There is no flag, and
		// nothing distinguishes "erased" from "no longer yours" -- which is the
		// point; see the proto.
		v := only(x, next())
		x.Equal(ethiopia.GetId(), v.GetId())
		x.Nil(v.GetValue())
		x.Equal(go_app.CoffeeService_Erase_FullMethodName, v.GetAction())
	}))

	t.Run("what the filters do not name is never sent", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
		kenya := c.CreateCoffee(ctx, x, beans.Ref(), "kenya")

		next, quiet := watching(ctx, x, c, go_app.CoffeeFilter_builder{
			Ref: go_app.CoffeeById(ethiopia.GetId()),
		}.Build())

		// The snapshot is what `List` would have answered with, which is the
		// filter as a query.
		x.Equal(ethiopia.GetId(), only(x, next()).GetId())

		// And the stream is the same filter, read from a row rather than from
		// a query. The two are the same rule written twice, which is why they
		// are tested against each other here.
		name := "Janey"
		_, err := c.Coffee().Patch(ctx, go_app.CoffeePatchRequest_builder{
			Ref:  kenya.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)
		quiet()

		name = "Johnny"
		_, err = c.Coffee().Patch(ctx, go_app.CoffeePatchRequest_builder{
			Ref:  ethiopia.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)
		x.Equal("Johnny", only(x, next()).GetValue().GetName())
	}))

	t.Run("a filter by slug reads the same both ways", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
		c.CreateCoffee(ctx, x, beans.Ref(), "kenya")

		next, _ := watching(ctx, x, c, go_app.CoffeeFilter_builder{
			Ref: go_app.CoffeeBySlug("ethiopia", beans.Ref()),
		}.Build())

		x.Equal(ethiopia.GetId(), only(x, next()).GetId())

		name := "Johnny"
		_, err := c.Coffee().Patch(ctx, go_app.CoffeePatchRequest_builder{
			Ref:  ethiopia.Ref(),
			Name: &name,
		}.Build())
		x.NoError(err)
		x.Equal("Johnny", only(x, next()).GetValue().GetName())
	}))

	t.Run("a read is not a change", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		next, quiet := watching(ctx, x, c)
		next()

		_, err := c.Coffee().Get(ctx, go_app.CoffeeGetById(ethiopia.GetId()))
		x.NoError(err)

		quiet()
	}))

	t.Run("everybody watching is told", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		// Nothing matches yet, so there is no first message to read: a stream
		// that has nothing to say says nothing rather than saying so.
		a, _ := watching(ctx, x, c)
		b, _ := watching(ctx, x, c)

		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
		x.Equal(ethiopia.GetId(), only(x, a()).GetId())
		x.Equal(ethiopia.GetId(), only(x, b()).GetId())
	}))

	t.Run("more filters than one watch carries is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		fs := make([]*go_app.CoffeeFilter, 64)
		for i := range fs {
			fs[i] = go_app.CoffeeFilter_builder{Ref: go_app.CoffeeById(make([]byte, 16))}.Build()
		}

		stream, err := c.Coffee().Watch(ctx, go_app.CoffeeWatchRequest_builder{Filters: fs}.Build())
		x.NoError(err, "the stream opens before the server has read the request")

		_, err = stream.Recv()
		x.ErrCode(codes.InvalidArgument, err)
	}))
}
