package ox

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/bare"
)

// Client talks to a [Server] over an in-memory connection, so a test goes
// through the same gRPC stack the app is served with without opening a port.
type Client struct {
	tb testing.TB

	Server *Server
	Conn   *grpc.ClientConn

	go_app.Client

	stop func()
	bare *Client
}

func NewClient(tb testing.TB, s *Server) *Client {
	tb.Helper()
	return newClient(tb, s, s.Grpc)
}

func newClient(tb testing.TB, s *Server, register func(*grpc.Server)) *Client {
	tb.Helper()

	conn, stop := s.Stack.Serve(register)

	return &Client{
		tb: tb,

		Server: s,
		Conn:   conn,
		Client: go_app.NewClient(conn),

		stop: stop,
	}
}

// As, AsBearer and AsNobody are [Stack]'s, and are here so that a test says
// `c.As(...)` rather than reaching through to the stack for it.
func (c *Client) As(ctx context.Context, subject string) context.Context {
	return c.Server.Stack.As(ctx, subject)
}

func (c *Client) AsBearer(ctx context.Context, token string) context.Context {
	return c.Server.Stack.AsBearer(ctx, token)
}

func (c *Client) AsNobody(ctx context.Context) context.Context {
	return c.Server.Stack.AsNobody(ctx)
}

// Bare returns a client of the innermost server, the one that runs the queries
// without any of the rules the servers in front of it impose. Use it to set up
// a state the app would refuse to create.
func (c *Client) Bare() go_app.Client {
	if c.bare != nil {
		return c.bare
	}

	// Named rather than found at the end of the stack: what a test wants here
	// is the server that talks to the database, and that is a server it can
	// name, not just whichever one happens to be last.
	s, ok := go_app.Find[bare.Server](c.Server.Server)
	require.True(c.tb, ok, "the stack has no bare server")

	// Nothing narrows what it reads. This app installs no scope, so this is
	// what the served one does too -- it is here so that an app which grows
	// one still has a client that can arrange a state the rules would refuse.
	s.Scope = bare.Unscoped{}

	c.bare = newClient(c.tb, c.Server, c.Server.GrpcOf(s))
	return c.bare
}

func (c *Client) Close() error {
	if c.bare != nil {
		c.bare.Close()
		c.bare = nil
	}

	c.stop()
	return nil
}

// CreateRoaster adds a Roaster, failing the test if it cannot.
func (c *Client) CreateRoaster(ctx context.Context, x *X, alias string) *go_app.Roaster {
	x.TB().Helper()

	v, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{Alias: alias}.Build())
	x.NoError(err)

	return v
}

// CreateCoffee adds a Coffee of the given Roaster, failing the test if it
// cannot.
func (c *Client) CreateCoffee(ctx context.Context, x *X, roaster *go_app.RoasterRef, alias string) *go_app.Coffee {
	x.TB().Helper()

	v, err := c.Coffee().Add(ctx, go_app.CoffeeAddRequest_builder{
		Roaster: roaster,
		Alias:   alias,
	}.Build())
	x.NoError(err)

	return v
}
