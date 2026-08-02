// Package gate decides what the caller of a request may do with it.
//
// Who the caller is was settled before this: `server/auth` put it in the frame
// of the request. What is left is whether that caller may see or change the
// thing being asked about, and here that is one rule - a Tenant is a wall, and
// what is inside it is not visible from outside.
//
// It is a sample. An app with more to say about who may do what says it here,
// in front of the rules that hold wherever it runs.
package gate

import (
	"bytes"
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/frame"
)

var _ go_app.Server = Server{}

type Server struct {
	server.Overlay
}

func NewServer(next go_app.Server) Server {
	return Server{server.NewOverlay(next)}
}

func Build() server.Builder {
	return builder{}
}

type builder struct{}

func (builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next), nil
}

// errNotFound is what is answered about something the caller may not see. That
// it exists is itself something not to say.
func errNotFound(what string) error {
	return status.Errorf(codes.NotFound, "%s not found", what)
}

// errForbidden is for what the caller can see but may not do. Saying "no" here
// gives nothing away that the caller does not already know.
func errForbidden(what string) error {
	return status.Errorf(codes.PermissionDenied, "not yours to %s", what)
}

// actor is who the request is from. A request that got this far without one is
// a server that was built without anything in front of it that says who is
// calling, so it is refused rather than served as nobody.
func actor(ctx context.Context) (*frame.Frame, error) {
	f, ok := frame.From(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "who is asking?")
	}

	return f, nil
}

// unbounded reports whether the caller is held by the Tenant that administers
// the deployment, which is the one Tenant that is not a wall.
func unbounded(f *frame.Frame) bool {
	return bytes.Equal(f.Tenant().GetId(), core.RootId[:])
}

// within reports whether the given Tenant is the caller's own.
func within(f *frame.Frame, v *go_app.Tenant) bool {
	return bytes.Equal(v.GetId(), f.Tenant().GetId())
}

// names reports whether the given reference is to the caller's own Tenant. A
// reference names a Tenant either by identifier or by alias, and the caller's
// own is known both ways, so neither has to be looked up.
func names(f *frame.Frame, ref *go_app.TenantRef) bool {
	return ref.Picks(f.Tenant())
}
