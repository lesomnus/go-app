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

	// Ungated is the stack without `server/gate` and without the wall it
	// states, which is what a deployment does its own work through -- putting
	// up a Tenant, and whatever else is not asked for from inside one.
	//
	// It is the whole of what used to be a comparison against a well-known
	// identifier. A test reaches it with [Client.Ungated], and that it has to
	// be reached rather than become is the point: the capability is a server
	// somebody was handed.
	Ungated go_app.Server

	// Tokens is the bearer store this app is served with, so that a test can
	// travel as a credential that allows less than its Holder does. Add one
	// and reach for it with [Client.AsBearer].
	Tokens *auth.MemTokenStore

	// Policy is what the servers consult about a caller, and is nothing unless
	// a test says otherwise -- which is what a deployment that injects none
	// gets. Set it before making the client that should see it.
	Policy gate.Policy

	// Limit is how often one caller may call, and is nothing unless a test says
	// otherwise -- which is also what `go-app.yaml` says unless a deployment
	// writes a rate down. Set it before making the client that should meet it.
	Limit grpcx.Limiter

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
	rec := audit.NewRecorder()

	// Without the wall, which is what works out who is calling and what puts
	// the root Tenant there before anybody exists; see cmd/serve.go.
	sink, err := bare.NewServer(c, bare.WithRecorder(rec))
	x.NoError(err)

	walled, err := bare.NewServer(c, bare.WithRecorder(rec), bare.WithScope(gate.Wall()))
	x.NoError(err)

	v, err := go_app.Build(walled, core.Build(), audit.Build(), gate.Build())
	x.NoError(err)

	// The same stack without the layer that says what a caller may do, on the
	// server the wall is not installed on.
	ungated, err := go_app.Build(sink, core.Build(), audit.Build())
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

		Tokens: auth.NewMemTokenStore(),

		Db:      c,
		Root:    root,
		Admin:   admin,
		Sink:    sink,
		Ungated: ungated,

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
	// First, so that everything behind it writes into the log of the test: the
	// panic recovery, and the record every call leaves. It has to be a stats
	// handler rather than an interceptor for the second of those -- the log is
	// one too, and a stats handler never sees what an interceptor put in the
	// context.
	opts := []grpc.ServerOption{
		grpcx.Seed(func(ctx context.Context) context.Context {
			return log.Into(ctx, s.log)
		}),
	}
	opts = append(opts, grpcx.ServerOptions(s.tb.Context())...)
	// And not [grpcx.Closed], so `Patch` and `Apply` are served here while a
	// deployment closes them (`server.general_writes`). It is the one place a
	// test does not travel what a caller travels, and it is deliberate: they
	// are how the servers write, and the tests of this repository are what
	// demonstrate them. An app made from this template tests the RPCs it wrote
	// by hand instead, and those are served either way. See the README.

	// The same way the app works out who is calling, so a test travels that
	// road as well. `Plain` is what says who without anything to check; the
	// token store in front of it is empty unless a test puts something in it,
	// and is there because a header has nowhere to carry an attenuation and a
	// token does.
	opts = append(opts, auth.Interceptor(
		auth.Seq(auth.Bearer(s.Tokens), auth.Plain()),
		auth.ServerResolver(s.Sink),
		auth.PublicDefault,
	)...)
	// And behind it, in the order the app installs them: how often that caller
	// may call, and then what they may see.
	opts = append(opts, grpcx.Limit(s.Limit, gate.ByTenant())...)
	opts = append(opts, gate.Interceptor(s.Policy)...)
	opts = append(opts, grpc.Creds(insecure.NewCredentials()))

	g := grpc.NewServer(opts...)
	go_app.RegisterServer(g, v)

	return g
}
