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
	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/go-app/server/watch"
	"github.com/lesomnus/signals"
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

	// Sink is the server that answers out of the database, without any of the
	// rules in front of it.
	Sink go_app.Server

	// Tokens is the bearer store this app is served with, so that a test can
	// travel as a credential that allows less than its Holder does. Add one
	// and reach for it with [Client.AsBearer].
	Tokens *auth.MemTokenStore

	// Gate is what `server/gate` decides with, and is nothing unless a test
	// says otherwise: a caller who said who they are may do anything, and an
	// anonymous one may do nothing. Set it before making the client that should
	// meet it.
	Gate []gate.Option

	// Limit is how often one caller may call, and is nothing unless a test says
	// otherwise -- which is also what `go-app.yaml` says unless a deployment
	// writes a rate down. Set it before making the client that should meet it.
	Limit grpcx.Limiter

	// Events is what a call that changed something is published to, so that a
	// test can subscribe and see what a watcher would. It is installed the way
	// `cmd/serve.go` installs it, and nothing listens unless a test does.
	Events signals.Signal[watch.Event]

	// watch is the recorder-and-interceptor pair [Server.Events] is fed by.
	watch *watch.Watch

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

	// And what a call that changed something is published to, the way the app
	// installs it; see cmd/serve.go.
	events := watch.Signal()
	wat := watch.New(events)

	// The stack the app is served with, whole.
	sink, err := bare.NewServer(c, bare.WithRecorder(wat.Recorder()))
	x.NoError(err)

	v, err := go_app.Build(sink, core.Build(), wat.Build(), gate.Build())
	x.NoError(err)

	return &Server{
		tb:  tb,
		log: logger(tb),

		Tokens: auth.NewMemTokenStore(),
		Events: events,
		watch:  wat,

		Db:   c,
		Sink: sink,

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
	)...)
	// And behind it, in the order the app installs them: how often that caller
	// may call, what they may see, and what is said about what they changed.
	opts = append(opts, grpcx.Limit(s.Limit, gate.BySubject())...)
	opts = append(opts, gate.Interceptor(s.Gate...)...)
	opts = append(opts, s.watch.Interceptor()...)
	opts = append(opts, grpc.Creds(insecure.NewCredentials()))

	g := grpc.NewServer(opts...)
	go_app.RegisterServer(g, v)

	return g
}
