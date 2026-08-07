package watch

import (
	"entgo.io/ent/dialect"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"

	go_app "github.com/lesomnus/go-app/go_app"
)

var _ go_app.Server = Server{}

// Every layer of a stack has to be rebindable for any of it to be, and a
// layer that is not is only found out when a transaction is started. This is
// what makes forgetting it a compile error instead.
var _ enttx.Binder[go_app.Server] = Server{}

// Server serves the `Watch` of every entity that has one, and forwards
// everything else.
//
// It is a layer rather than something bolted to the side, and what that buys is
// the read: a `Watch` answers by *reading*, through the servers behind it, with
// the context of the caller who asked. So the wall is the wall -- the same
// predicate in the same query as every other read -- and nothing here has to
// know what a Tenant is.
//
// Where it goes in the stack matters. It is behind `server/gate`, so a caller
// who may not ask has already been refused; and in front of `server/core`, so
// that the list it reads is the hand-written one with its filters and its
// paging rather than a query of its own.
type Server struct {
	go_app.Overlay

	w *Watch
}

func NewServer(next go_app.Server, w *Watch) Server {
	return Server{go_app.NewOverlay(next), w}
}

// WithDriver answers with this stack running on `drv`. Every layer writes it;
// see [core.Server.WithDriver] for why it cannot be inherited.
func (s Server) WithDriver(drv dialect.Driver) (go_app.Server, error) {
	next, err := enttx.Rebind(s.Next(), drv)
	if err != nil {
		return nil, err
	}

	return NewServer(next, s.w), nil
}

// Build makes a [go_app.Builder] of this layer so that it can be stacked with
// the others.
func (w *Watch) Build() go_app.Builder {
	return builder{w}
}

type builder struct {
	w *Watch
}

func (b builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next, b.w), nil
}
