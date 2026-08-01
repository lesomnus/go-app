// Package core implements the rules that hold no matter where the app runs.
//
// It is a middleware: it validates and completes the requests it cares about
// and lets the next server, usually the generated `server/bare`, do the actual
// work.
package core

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/go-app/server"
	"github.com/lesomnus/go-app/server/bare"
)

var _ go_app.Server = Server{}

type Server struct {
	server.Overlay
}

func NewServer(next go_app.Server) Server {
	return Server{server.NewOverlay(next)}
}

// New builds the default stack: the core rules in front of the generated
// servers that talk to the database.
func New(db *ent.Client) Server {
	return NewServer(bare.NewServer(db))
}

// Db returns the client of the generated server behind this one, which is what
// owns the connection.
//
// A service written by hand usually has to query the database itself, and this
// is how it is reached without every server in the stack having to carry a
// client of its own.
func (s Server) Db() (*ent.Client, error) {
	v, ok := server.Find[bare.Server](s)
	if !ok {
		return nil, status.Error(codes.Internal, "no database in the server stack")
	}

	return v.Db, nil
}

// Build makes a [server.Builder] of this server so that it can be stacked with
// the others. A named type rather than a [server.BuilderFunc], since that is
// what names the builder if it fails, and it is where the options this server
// takes would be held.
func Build() server.Builder {
	return builder{}
}

type builder struct{}

func (builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next), nil
}
