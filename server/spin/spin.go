// Package spin runs what the layers of a server stack have to do besides
// answering requests.
//
// A request is not the only reason a server does something. Rows have to be
// swept, caches warmed, leases renewed -- work that belongs to a layer, needs
// what that layer holds, and happens on a clock rather than because somebody
// asked. This is where a layer says so.
//
// # Why it is not a method on the stack
//
// `go_app.Server` is generated, and adding to it means rewriting every layer,
// every Overlay and every helper above. The README calls that out and keeps one
// exception -- `enttx.Binder` -- for the one case where it is the right trade:
// rebinding a stack must not *skip* a layer, or the rebuilt stack is missing it
// and requests inside the transaction go around it.
//
// This is the other case, and it is the ordinary one. Starting a layer that has
// nothing to start is nothing, and skipping one loses nothing, so it is a
// question asked of the stack -- [All] walks it and starts whatever answers to
// [Spinner]. A layer with no background work writes not one line.
//
// # What a failure means
//
// A loop that returns an error is logged and started again after a wait, and
// one that returns nil has finished and is not. Neither takes the process down.
//
// That is a decision rather than a default, and it is the conservative half of
// a real trade: a sweep that failed once because the database blinked is not a
// reason to stop serving requests, and a sweep that has been failing for an
// hour is a thing nobody notices. This answers the first and leaves the second
// to whatever reads the log -- a deployment that would rather fall over says so
// by having its loop take the process down itself.
package spin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lesomnus/otx/log"

	go_app "github.com/lesomnus/go-app/go_app"
)

// Retry is how long a loop that failed waits before it is started again.
const Retry = 3 * time.Second

// Spinner is a layer with something to run for as long as the app is running.
//
// It is answered by the layer itself, so what it runs has whatever the layer
// has: the servers behind it, the database it can `Find`, the rules it applies.
// Work that goes *through* a layer's own rules is the point -- a sweep that
// wrote around them would be a second implementation of the app.
//
// The context is canceled when the app is shutting down, and a Spin must return
// when it is. Whatever it holds it releases before returning; nothing here waits
// forever on its behalf.
type Spinner interface {
	Spin(ctx context.Context) error
}

// Named is a [Spinner] that says what to call it in the log. A layer that is
// not one is called by its type.
type Named interface {
	SpinName() string
}

// All starts every [Spinner] in `s` and returns when they have all stopped,
// which is when `ctx` is done.
//
// It is called once, where the server is built and before it is served. A
// deployment with nothing to spin calls it anyway and it returns as soon as the
// context does.
func All(ctx context.Context, s go_app.Server) {
	var (
		wg sync.WaitGroup
		l  = log.From(ctx)
	)
	for v := range go_app.Iter(s) {
		u, ok := v.(Spinner)
		if !ok {
			continue
		}

		wg.Go(func() { run(ctx, u) })
	}

	// The layers are started and this waits, rather than each of them being
	// left to whoever called: a background loop that outlives the shutdown that
	// was supposed to stop it is the one that is still holding a connection
	// when the process is told to go.
	l.DebugContext(ctx, "spinning")
	wg.Wait()
}

// run keeps one loop going until the app is shutting down.
func run(ctx context.Context, s Spinner) {
	l := log.From(ctx).With(slog.String("spin", name(s)))
	ctx = log.Into(ctx, l)

	l.InfoContext(ctx, "spin up")
	defer l.InfoContext(ctx, "spin down")

	for {
		err := s.Spin(ctx)
		switch {
		case err == nil:
			// It is finished. A loop that meant to keep going says so by not
			// returning, and one that returns has said it is done.
			return

		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// The app is shutting down, which is not a failure and is not
			// something to be told about twice.
			return
		}

		l.ErrorContext(ctx, "spin failed", slog.String("error", err.Error()))

		select {
		case <-ctx.Done():
			return
		case <-time.After(Retry):
		}
	}
}

// name is what a loop is called in the log.
func name(s Spinner) string {
	if v, ok := s.(Named); ok {
		return v.SpinName()
	}

	return fmt.Sprintf("%T", s)
}

// Every answers with a [Spinner.Spin] that runs `f` every `d`, until the app is
// shutting down.
//
// The tick is the wait *between* runs and not a schedule: a pass that takes
// longer than `d` does not double up on itself, and a pass that is skipped is
// not made up afterwards. That is what almost every sweep wants, and the one
// that wants a wall-clock schedule wants a schedule and not this.
//
// It runs `f` once as it starts. A loop whose first pass is a whole interval
// away is one that does nothing at all for a deployment that restarts more
// often than that.
func Every(d time.Duration, f func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		for {
			if err := f(ctx); err != nil {
				return err
			}

			select {
			case <-ctx.Done():
				// Not an error. The app is shutting down and this is finished.
				return nil
			case <-time.After(d):
			}
		}
	}
}

// Func is a [Spinner] made of a function, for a layer that would rather write
// one than a method -- and for a test.
type Func func(ctx context.Context) error

func (f Func) Spin(ctx context.Context) error { return f(ctx) }
