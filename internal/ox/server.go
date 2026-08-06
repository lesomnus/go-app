package ox

import (
	"context"
	"log/slog"
	"net/url"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lesomnus/otx/log"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/audit"
	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/gate"
)

// Server is the app under test, backed by a database that lives in memory and
// is thrown away with the test that made it.
type Server struct {
	tb  testing.TB
	log *slog.Logger

	// Db is the client the servers run their queries with. Reach for it to
	// look at the database directly; it knows the driver and the dialect it
	// runs on, so a server built by hand needs nothing else.
	Db *ent.Client

	// Root is the Tenant that administers the deployment, which is there
	// before any test does anything, and Admin is who holds it. A test is
	// served as Admin unless it says otherwise.
	Root  *go_app.Tenant
	Admin *go_app.Holder

	// Sink is the server that answers out of the database, which is what the
	// authentication looks callers up on.
	Sink go_app.Server

	go_app.Server
}

func NewServer(tb testing.TB) *Server {
	tb.Helper()
	x := require.New(tb)

	// A database of its own per test, deleted once the test is done.
	dsn := memdb.TestDB(tb, url.Values{"_pragma": {"foreign_keys(1)"}})
	db, err := driver.Open(dsn)
	x.NoError(err)

	// The database is held in memory and every test is small, so a single
	// connection is enough and it keeps SQLite from reporting a busy database.
	db.SetMaxOpenConns(1)

	c := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, db)))
	tb.Cleanup(func() { c.Close() })
	x.NoError(c.Schema.Create(tb.Context()))

	// The stack the app is served with, whole: the rules that hold everywhere,
	// the gate that says who may ask for what, and the trail the writes leave
	// behind them.
	sink, err := bare.NewServer(c, bare.WithRecorder(audit.NewRecorder()))
	x.NoError(err)
	v, err := go_app.Build(sink, core.Build(), audit.Build(), gate.Build())
	x.NoError(err)

	// Every deployment has the root Tenant, so every test does too. It is made
	// around the gate, since there is nobody to be yet.
	ctx := tb.Context()
	root, err := core.EnsureRoot(ctx, core.NewServer(sink))
	x.NoError(err)
	admin, err := core.Admin(ctx, core.NewServer(sink), root.Ref())
	x.NoError(err)

	return &Server{
		tb:  tb,
		log: logger(tb),

		Db:    c,
		Root:  root,
		Admin: admin,
		Sink:  sink,

		Server: v,
	}
}

// Grpc serves the app on a new gRPC server.
func (s *Server) Grpc() *grpc.Server {
	return s.GrpcOf(s)
}

// GrpcOf serves `v` with the options the app is served with, so that a test
// travels the same stack a request does in production. The credentials are the
// one thing that differs, since the connection never leaves the process.
func (s *Server) GrpcOf(v go_app.Server) *grpc.Server {
	// First, so that everything behind it, the panic recovery included, writes
	// into the log of the test.
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			return handler(log.Into(ctx, s.log), req)
		}),
		grpc.ChainStreamInterceptor(func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			return handler(srv, grpcx.StreamWithContext(ss, log.Into(ss.Context(), s.log)))
		}),
	}
	opts = append(opts, grpcx.ServerOptions(s.tb.Context(), grpcx.DefaultTimeout)...)
	// The same way the app works out who is calling, so a test travels that
	// road as well; `Plain` is what says who without anything to check.
	opts = append(opts, auth.Interceptor(auth.Plain(), auth.ServerResolver(s.Sink), auth.PublicDefault)...)
	opts = append(opts, grpc.Creds(insecure.NewCredentials()))

	g := grpc.NewServer(opts...)
	go_app.RegisterServer(g, v)

	return g
}
