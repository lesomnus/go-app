package ox

import (
	"context"
	"net/url"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/lesomnus/signals"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/core"
	"github.com/lesomnus/go-app/server/gate"
	"github.com/lesomnus/go-app/server/watch"
)

// Server is the app under test, backed by a database that lives in memory and
// is thrown away with the test that made it.
//
// Everything store-shaped is here rather than in [Stack]: the database, the
// generated servers, and the events they publish. An app that replaces the
// store replaces this file and keeps the other one.
type Server struct {
	// Stack is how it is served -- the interceptors, the authentication and
	// the gate. Set what it holds before making the client that should meet it.
	*Stack

	// Db is the client the servers run their queries with. Reach for it to
	// look at the database directly; it knows the driver and the dialect it
	// runs on, so a server built by hand needs nothing else.
	Db *ent.Client

	// Sink is the server that answers out of the database, without any of the
	// rules in front of it.
	Sink go_app.Server

	// Events is what a call that changed something is published to, so that a
	// test can subscribe and see what a watcher would. It is installed the way
	// `cmd/serve.go` installs it, and nothing listens unless a test does.
	Events signals.Signal[watch.Event]

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

	stack := NewStack(tb)
	// Outside the handler and inside everything that could refuse the call, so
	// that what is published is what was served.
	stack.Extra = wat.Interceptor()

	return &Server{
		Stack: stack,

		Db:     c,
		Sink:   sink,
		Events: events,

		Server: v,
	}
}

// Grpc registers the whole app.
func (s *Server) Grpc(g *grpc.Server) {
	go_app.RegisterServer(g, s.Server)
}

// GrpcOf registers `v` instead, which is how [Client.Bare] serves the innermost
// server through the same chain.
func (s *Server) GrpcOf(v go_app.Server) func(*grpc.Server) {
	return func(g *grpc.Server) { go_app.RegisterServer(g, v) }
}

// T runs `run` against a fresh app. The context it hands over is the one of the
// test, so it is canceled as soon as the test is done.
func T(run func(ctx context.Context, x *X, c *Client)) func(t *testing.T) {
	return func(t *testing.T) {
		s := NewServer(t)
		c := NewClient(t, s)
		defer c.Close()

		// As somebody, since a test is usually not about being nobody. One
		// that is says so with [Client.AsNobody].
		run(c.As(t.Context(), "somebody"), NewX(t), c)
	}
}
