// Package gate decides what the caller of a request may do with it.
//
// Who the caller is was settled before this: `server/auth` put it in the frame
// of the request, and **every request has one** -- a caller nobody vouched for
// is [frame.Anonymous] rather than nobody at all. What is left is what they may
// do, and this app has one rule of its own about that:
//
//	an anonymous caller may make the calls that were named, and no others.
//
// A closed list of what is allowed rather than an open list of what is not, and
// the difference is what happens the day somebody writes an RPC. `Rename` is a
// write; a rule that let anonymous callers make anything that is not spelled
// `Add`, `Erase`, `Patch` or `Apply` would have opened it, quietly, to
// everybody. Nothing written down is nothing allowed.
//
// Everything finer than that is a deployment's, and [Policy] is where it is
// injected. This app is a resource server: it reads credentials and enforces
// what it is told, and it does not define roles or decide who holds them. See
// `server/gate/roles` for what one implementation looks like -- nothing wires
// it in.
package gate

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

// Server is the layer, and it overrides nothing.
//
// That is worth a sentence rather than a shrug. What this package decides is
// decided **once, in front**, by [Interceptor] -- before the handler, out of
// the frame and the method, with nothing of the request in it. A layer that
// asked again per RPC would be the same question answered once per entity,
// forever, and the copies would drift.
//
// It is here so that the stack has a name for where those rules live, and so
// that an app that grows a rule about a *particular* RPC has the layer to put
// it in.
type Server struct {
	go_app.Overlay
}

func NewServer(next go_app.Server) Server {
	return Server{go_app.NewOverlay(next)}
}

// WithDriver answers with this stack running on `drv`. Every layer writes it;
// see [core.Server.WithDriver] for why it cannot be inherited.
func (s Server) WithDriver(drv dialect.Driver) (go_app.Server, error) {
	next, err := enttx.Rebind(s.Next(), drv)
	if err != nil {
		return nil, err
	}

	return NewServer(next), nil
}

func Build() go_app.Builder {
	return builder{}
}

type builder struct{}

func (builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next), nil
}
