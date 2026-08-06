// Package gate decides what the caller of a request may do with it.
//
// Who the caller is was settled before this: `server/auth` put it in the frame
// of the request. What is left is whether that caller may see or change the
// thing being asked about, and here that is one rule - a Tenant is a wall, and
// what is inside it is not visible from outside.
//
// Most of that rule is stated here and enforced elsewhere. Narrowing what a
// caller may see is a predicate, and a predicate belongs in the query, so it is
// [Wall] and it is installed on the innermost server. What is left in the
// layers of this package is what is not a predicate: whether a Tenant may be
// put up or taken down, and which Tenant a Holder may be added to, both of
// which are about a row that does not exist yet.
//
// It is a sample. An app with more to say about who may do what says it here,
// in front of the rules that hold wherever it runs.
package gate

import (
	"context"

	"entgo.io/ent/dialect"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/frame"
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

// errForbidden is for what the caller can see but may not do. Saying "no" here
// gives nothing away that the caller does not already know.
//
// There is no errNotFound beside it any more. What a caller may not see is not
// answered by this package at all: it is a row the query did not match, and
// what says so is the server that ran the query. See [Wall].
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
