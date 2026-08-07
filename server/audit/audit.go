// Package audit keeps the trail of what was changed, and by whom.
//
// It is two things that do not look alike. [Recorder] is what writes the trail:
// it is not a layer of the server stack at all but a `bare.Recorder`, handed to
// the generated servers so that every write reports itself from inside the
// transaction that makes it. [Server] is the layer, and it is here for the
// other half of the story -- the trail is written by the app and read by
// people, and nobody gets to write one by hand.
//
// The recorder sits below every rule in the stack on purpose. What is recorded
// should be what was written, and `server/core` normalizes requests on the way
// down -- an alias is trimmed and folded before it is stored. A trail kept in
// front of that would say the caller wrote " Johnny " into a row that holds
// "johnny".
package audit

import (
	"context"

	"entgo.io/ent/dialect"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

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

// WithDriver answers with this stack running on `drv`. Every layer writes it;
// see [core.Server.WithDriver] for why it cannot be inherited.
func (s Server) WithDriver(drv dialect.Driver) (go_app.Server, error) {
	next, err := enttx.Rebind(s.Next(), drv)
	if err != nil {
		return nil, err
	}

	return NewServer(next), nil
}

// Db returns the client of the generated server at the end of the stack, which
// is what List queries with.
func (s Server) Db() (*ent.Client, error) {
	v, ok := go_app.Find[bare.Server](s)
	if !ok {
		return nil, status.Error(codes.Internal, "no database in the server stack")
	}

	return v.Db, nil
}

// Scope returns what that server narrows its queries with, so that List
// narrows its own the same way. See [core.Server.Scope].
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

func Build() go_app.Builder {
	return builder{}
}

type builder struct{}

func (builder) Build(next go_app.Server) (go_app.Server, error) {
	return NewServer(next), nil
}

// AuditServiceServer serves the trail, and what it does not name it refuses.
//
// This is the one server in the app that is built the other way up. Everywhere
// else a layer embeds the server behind it and overrides what it has something
// to say about, so anything it says nothing about is forwarded; here it embeds
// the unimplemented server, so anything it says nothing about is refused and
// only what is named below is handed on.
//
// The difference is what happens to an RPC added later. `AuditService.List` was
// added by hand once already (`proto.svc/go_app/audit_svc.ext.proto`), and the
// day somebody adds a `Prune` for retention, or an `EraseMany`, the other shape
// would forward it to the server that writes -- and the trail would have become
// editable through the served stack with nothing in this file to review.
type AuditServiceServer struct {
	Server
	go_app.UnimplementedAuditServiceServer

	// next is the server this one is in front of, reached by name rather than
	// by embedding so that nothing is forwarded to it by accident.
	next go_app.AuditServiceServer
}

func NewAuditServiceServer(s Server) AuditServiceServer {
	return AuditServiceServer{Server: s, next: s.Next().Audit()}
}

func (s Server) Audit() go_app.AuditServiceServer {
	return NewAuditServiceServer(s)
}

// Get hands the request on, which is the whole of what this layer has to say
// about reading one row. Who may read it is `server/gate`'s to decide.
func (s AuditServiceServer) Get(ctx context.Context, req *go_app.AuditGetRequest) (*go_app.Audit, error) {
	return s.next.Get(ctx, req)
}

// errWritten is the answer to anyone who would put something in the trail, or
// take something out of it.
//
// The RPCs exist because the schema says the entity is a full CRUD one, and
// that is worth keeping: a test arranges a trail with them and reads it back,
// which is far plainer than reaching around the stack into the database. What
// is not worth keeping is the ability to do it to a deployment, where a trail
// that can be edited is not evidence of anything.
//
// Unimplemented rather than PermissionDenied, because this is not about who is
// asking. Nobody may, and no credential changes that.
func errWritten(what string) error {
	return status.Errorf(codes.Unimplemented, "the trail is written by what it is about, not %s", what)
}

func (s AuditServiceServer) Add(ctx context.Context, req *go_app.AuditAddRequest) (*go_app.Audit, error) {
	return nil, errWritten("by hand")
}

func (s AuditServiceServer) Patch(ctx context.Context, req *go_app.AuditPatchRequest) (*go_app.Audit, error) {
	return nil, errWritten("edited afterwards")
}

func (s AuditServiceServer) Apply(ctx context.Context, req *go_app.AuditApplyRequest) (*go_app.Audit, error) {
	return nil, errWritten("edited afterwards")
}

func (s AuditServiceServer) Erase(ctx context.Context, req *go_app.AuditRef) (*emptypb.Empty, error) {
	return nil, errWritten("taken back")
}
