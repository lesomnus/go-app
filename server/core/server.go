// Package core implements the rules that hold no matter where the app runs.
//
// It is a middleware: it validates and completes the requests it cares about
// and lets the next server, usually the generated `server/bare`, do the actual
// work.
package core

import (
	"entgo.io/ent/dialect"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/go-app/server/bare"
)

var _ go_app.Server = Server{}

// Every layer of a stack has to be rebindable for any of it to be, and a
// layer that is not is only found out when a transaction is started. This is
// what makes forgetting it a compile error instead.
var _ enttx.Binder[go_app.Server] = Server{}

type Server struct {
	go_app.Overlay
}

func NewServer(next go_app.Server) Server {
	return Server{go_app.NewOverlay(next)}
}

// New builds the default stack: the core rules in front of the generated
// servers that talk to the database.
//
// It fails on a client whose dialect the generated servers write no SQL for,
// which is settled here rather than at the first `Apply` that would have needed
// it.
func New(db *ent.Client, opts ...bare.Option) (Server, error) {
	v, err := bare.NewServer(db, opts...)
	if err != nil {
		return Server{}, err
	}

	return NewServer(v), nil
}

// WithDriver answers with this stack running on `drv`, which is how several
// servers are put on one transaction; see [enttx.Begin].
//
// Every layer writes this, and it cannot be inherited from [go_app.Overlay]:
// the overlay holds what is behind this server but has no way to make this
// server again. Nor can the caller reach past it -- a layer left out of the
// rebinding is left out of the stack, and the requests inside the transaction
// would go around it.
func (s Server) WithDriver(drv dialect.Driver) (go_app.Server, error) {
	next, err := enttx.Rebind(s.Next(), drv)
	if err != nil {
		return nil, err
	}

	return NewServer(next), nil
}

// Db returns the client of the generated server behind this one, which is what
// owns the connection.
//
// A service written by hand usually has to query the database itself, and this
// is how it is reached without every server in the stack having to carry a
// client of its own.
func (s Server) Db() (*ent.Client, error) {
	v, ok := go_app.Find[bare.Server](s)
	if !ok {
		return nil, status.Error(codes.Internal, "no database in the server stack")
	}

	return v.Db, nil
}

// Scope returns what the generated server behind this one narrows its queries
// with, so that a service written by hand narrows its own the same way.
//
// A `List` is the case this exists for. It is not a CRUD operation, so nothing
// generates it and nothing narrows it; written without this it would answer
// with every row and leave the caller's own to be picked out of the answer --
// which is wrong twice over. It reads rows the caller may not see, and a list
// cut short at a limit before it is filtered is one that any caller can push
// another's rows out of by making enough of their own.
func (s Server) Scope() (bare.Scope, error) {
	v, ok := go_app.Find[bare.Server](s)
	if !ok {
		// Not [bare.Unscoped]: a stack with no database is a stack that cannot
		// answer, and a scope that narrows nothing is the wrong thing to hand
		// back to whoever is about to read with it.
		return nil, status.Error(codes.Internal, "no database in the server stack")
	}

	return v.Scope, nil
}

// Build makes a [go_app.Builder] of this server so that it can be stacked with
// the others. A named type rather than a [go_app.BuilderFunc], since that is
// what names the builder if it fails, and it is where the options this server
// takes would be held.
func Build() go_app.Builder {
	return builder{}
}

type builder struct{}

func (builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next), nil
}
