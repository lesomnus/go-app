package watch_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lesomnus/signals"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/watch"
)

// listen subscribes before the test does anything, and answers with a function
// that takes the next event or fails if none arrives.
//
// The buffer is generous: a subscriber of a hard signal that falls behind is
// cut off, and a test that was cut off should fail as the test it is rather
// than as a timeout in the next one.
func listen(x *ox.X, c *ox.Client) func() watch.Event {
	x.TB().Helper()

	ch, stop := c.Server.Events.Subscribe(64)
	x.TB().Cleanup(func() { stop() })

	return func() watch.Event {
		x.TB().Helper()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		v, ok := signals.Recv(ctx, ch)
		x.True(ok, "no event was published")

		return v
	}
}

// drained answers with everything published so far and nothing after it.
func drained(ch <-chan watch.Event) []watch.Event {
	var vs []watch.Event
	for {
		select {
		case v := <-ch:
			vs = append(vs, v)
		default:
			return vs
		}
	}
}

func TestWatch(t *testing.T) {
	t.Run("a call that wrote says who, what, and what it did", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		next := listen(x, c)

		beans := c.CreateRoaster(ctx, x, "beans")
		ethiopia := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		// Two calls, and the Roaster's is the first of them.
		v := next()
		x.Equal(go_app.RoasterService_Add_FullMethodName, v.Method)
		x.Equal("somebody", v.Actor.Subject, "the caller, read from the frame")

		// The request as it arrived and the response as it was answered. The
		// response of an Add is the row that was written, which is why the
		// changes below do not carry it again.
		req, ok := v.Request.(*go_app.RoasterAddRequest)
		x.True(ok)
		x.Equal("beans", req.GetAlias())

		res, ok := v.Response.(*go_app.Roaster)
		x.True(ok)
		x.Equal(beans.GetId(), res.GetId())

		// And what the call actually wrote.
		x.Len(v.Changes, 1)
		x.Equal(go_app.RoasterService_Add_FullMethodName, v.Changes[0].By)
		x.Equal(go_app.RoasterService_Add_FullMethodName, v.Changes[0].Method,
			"every write of one call says the one thing the caller asked for")

		// The second call is the Coffee's, and is one write.
		v = next()
		x.Equal(go_app.CoffeeService_Add_FullMethodName, v.Method)
		x.Len(v.Changes, 1)
		x.Equal(ethiopia.GetId(), v.Response.(*go_app.Coffee).GetId())
	}))

	t.Run("a read is not news", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")

		ch, stop := c.Server.Events.Subscribe(8)
		defer stop()

		_, err := c.Roaster().Get(ctx, go_app.RoasterGetById(beans.GetId()))
		x.NoError(err)

		// Most of what a server does is read, and an event per read is a
		// firehose of nothing.
		x.Empty(drained(ch))
	}))

	t.Run("a call that failed says nothing", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		c.CreateRoaster(ctx, x, "beans")

		ch, stop := c.Server.Events.Subscribe(8)
		defer stop()

		// Refused by `server/core` before anything is written.
		_, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{
			Alias: "beans",
		}.Build())
		x.ErrCode(codes.AlreadyExists, err)

		x.Empty(drained(ch))
	}))

	t.Run("everybody listening hears it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		a := listen(x, c)
		b := listen(x, c)

		c.CreateRoaster(ctx, x, "beans")

		x.Equal(go_app.RoasterService_Add_FullMethodName, a().Method)
		x.Equal(go_app.RoasterService_Add_FullMethodName, b().Method)
	}))

	t.Run("a subscriber that falls behind is cut off", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// One event of room, and nothing reading it.
		ch, stop := c.Server.Events.Subscribe(1)
		defer stop()

		c.CreateRoaster(ctx, x, "beans")
		c.CreateRoaster(ctx, x, "peaks")

		// The first fits; the second finds no room and closes the channel
		// rather than blocking the call that published it or skipping it in
		// silence. A closed channel is a thing a watcher notices.
		v, ok := <-ch
		x.True(ok)
		x.Equal("beans", v.Request.(*go_app.RoasterAddRequest).GetAlias())

		_, ok = <-ch
		x.False(ok, "the subscription is closed, not merely empty")
	}))
}

// TestNothingIsPublishedForAWriteThatWasUndone is the reason the recorder only
// remembers.
//
// A call can write one row, have it recorded, and then fail on the next --
// erasing a Roaster takes its Coffees with it, in one transaction, and any of
// those writes can be the one that does not go through. Everything before it is
// rolled back, so the recorder is left holding writes that did not happen.
//
// It is driven directly rather than through the stack, because arranging that
// failure through the served servers is arranging it out of what they happen to
// do today, and this is about what the interceptor promises whatever they do.
func TestNothingIsPublishedForAWriteThatWasUndone(t *testing.T) {
	x := require.New(t)

	sig := watch.Signal()
	ch, stop := sig.Subscribe(8)
	defer stop()

	w := watch.New(sig)
	rec := w.Recorder()

	_, err := w.Unary()(
		t.Context(), &go_app.RoasterAddRequest{},
		&grpc.UnaryServerInfo{FullMethod: go_app.RoasterService_Add_FullMethodName},
		func(ctx context.Context, _ any) (any, error) {
			// The first write, recorded from inside its transaction...
			x.NoError(rec.Record(ctx, bare.Server{}, bare.Change{
				By:  go_app.RoasterService_Add_FullMethodName,
				Key: uuid.New(),
			}))

			// ...and then the call fails, taking it with it.
			return nil, status.Error(codes.Aborted, "the next write did not go through")
		})
	x.Equal(codes.Aborted, status.Code(err))

	// Published from inside the recorder, this is the event a watcher would
	// have acted on.
	x.Empty(drained(ch))
}
