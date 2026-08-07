package spin_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/spin"
)

// layer is one of a stack, spinning what it was given and nothing else.
type layer struct {
	go_app.Overlay

	spin spin.Func
}

func (l layer) Spin(ctx context.Context) error { return l.spin(ctx) }

// quiet is a layer with nothing to run, which is what most of them are.
type quiet struct {
	go_app.Overlay
}

// stack builds a stack of the given layers, innermost last.
func stack(vs ...func(next go_app.Server) go_app.Server) go_app.Server {
	var s go_app.Server = go_app.UnimplementedServer{}
	for i := len(vs) - 1; i >= 0; i-- {
		s = vs[i](s)
	}

	return s
}

func spinning(f spin.Func) func(next go_app.Server) go_app.Server {
	return func(next go_app.Server) go_app.Server {
		return layer{go_app.NewOverlay(next), f}
	}
}

func silent() func(next go_app.Server) go_app.Server {
	return func(next go_app.Server) go_app.Server {
		return quiet{go_app.NewOverlay(next)}
	}
}

// waits runs [spin.All] and answers with a function that waits for it to
// return, failing the test if it does not.
func waits(t *testing.T, ctx context.Context, s go_app.Server) func() {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		spin.All(ctx, s)
	}()

	return func() {
		t.Helper()

		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("spin.All did not return")
		}
	}
}

func TestAll(t *testing.T) {
	t.Run("every layer that has something to run is started", func(t *testing.T) {
		x := require.New(t)

		var a, b atomic.Int64
		ctx, cancel := context.WithCancel(t.Context())

		// A layer with nothing to run in the middle, which is what most of them
		// are and is why this is a question asked of the stack rather than a
		// method every one of them has to write.
		wait := waits(t, ctx, stack(
			spinning(func(ctx context.Context) error { a.Add(1); <-ctx.Done(); return nil }),
			silent(),
			spinning(func(ctx context.Context) error { b.Add(1); <-ctx.Done(); return nil }),
		))

		x.Eventually(func() bool { return a.Load() == 1 && b.Load() == 1 }, time.Second, time.Millisecond)

		cancel()
		wait()
	})

	t.Run("nothing to run is not an error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// A deployment with no background work calls this anyway, and it
		// answers as soon as there is nothing keeping it.
		waits(t, ctx, stack(silent(), silent()))()
	})

	t.Run("one that failed is started again", func(t *testing.T) {
		x := require.New(t)

		var n atomic.Int64
		ctx, cancel := context.WithCancel(t.Context())

		wait := waits(t, ctx, stack(spinning(func(ctx context.Context) error {
			if n.Add(1) < 3 {
				return errors.New("not yet")
			}

			<-ctx.Done()
			return nil
		})))

		// A sweep that failed once because the database blinked is not a reason
		// to stop. The wait between tries is [spin.Retry].
		x.Eventually(func() bool { return n.Load() == 3 },
			10*spin.Retry, spin.Retry/4)

		cancel()
		wait()
	})

	t.Run("one that finished is not", func(t *testing.T) {
		x := require.New(t)

		var n atomic.Int64
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		// Answering nil is a loop saying it is done, which is not the same as
		// one that fell over.
		waits(t, ctx, stack(spinning(func(context.Context) error {
			n.Add(1)
			return nil
		})))()

		x.Equal(int64(1), n.Load())
	})

	t.Run("shutting down is not a failure", func(t *testing.T) {
		x := require.New(t)

		var n atomic.Int64
		ctx, cancel := context.WithCancel(t.Context())

		wait := waits(t, ctx, stack(spinning(func(ctx context.Context) error {
			n.Add(1)
			<-ctx.Done()

			// What a loop that was reading something when the app was told to
			// go answers with. Started again, it would be started again for as
			// long as the shutdown took.
			return ctx.Err()
		})))

		x.Eventually(func() bool { return n.Load() == 1 }, time.Second, time.Millisecond)
		cancel()
		wait()

		x.Equal(int64(1), n.Load())
	})
}

func TestEvery(t *testing.T) {
	t.Run("runs at once and then on the tick", func(t *testing.T) {
		x := require.New(t)

		var n atomic.Int64
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		f := spin.Every(time.Millisecond, func(context.Context) error {
			n.Add(1)
			return nil
		})

		go f(ctx) //nolint:errcheck

		// The first pass is not an interval away: a loop whose first run is a
		// whole hour off does nothing at all for a deployment that restarts
		// more often than that.
		x.Eventually(func() bool { return n.Load() > 3 }, time.Second, time.Millisecond)
	})

	t.Run("shutting down ends it, and is not a failure", func(t *testing.T) {
		x := require.New(t)

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		x.NoError(spin.Every(time.Hour, func(context.Context) error { return nil })(ctx))
	})
}
