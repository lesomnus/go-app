package ox

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/bare"
)

// bufSize is the buffer of the in-memory listener; large enough that a message
// never blocks on it.
const bufSize = 1 << 20

// Client talks to a [Server] over an in-memory connection, so a test goes
// through the same gRPC stack the app is served with without opening a port.
type Client struct {
	tb testing.TB

	Server *Server
	Conn   *grpc.ClientConn

	go_app.Client

	grpc *grpc.Server
	wg   sync.WaitGroup

	bare *Client
}

func NewClient(tb testing.TB, s *Server) *Client {
	tb.Helper()
	return newClient(tb, s, s.Grpc())
}

func newClient(tb testing.TB, s *Server, g *grpc.Server) *Client {
	tb.Helper()
	x := require.New(tb)

	l := bufconn.Listen(bufSize)
	conn, err := grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return l.DialContext(ctx)
		}),
	)
	x.NoError(err)

	v := &Client{
		tb: tb,

		Server: s,
		Conn:   conn,
		Client: go_app.NewClient(conn),

		grpc: g,
	}
	v.wg.Go(func() {
		if err := g.Serve(l); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			// Not `require`, which must be called from the test goroutine.
			tb.Errorf("serve: %s", err)
		}
	})

	return v
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

	// Without the wall. It is stated by `server/gate` and enforced from inside
	// this server, so it is one of the rules in front even though it does not
	// run there -- and this client is for arranging a state the rules would
	// refuse. A test that wants to see the wall travels the ordinary client.
	s.Scope = bare.Scopes{}

	c.bare = newClient(c.tb, c.Server, c.Server.GrpcOf(s))
	return c.bare
}

func (c *Client) Close() error {
	if c.bare != nil {
		c.bare.Close()
		c.bare = nil
	}

	c.Conn.Close()
	c.grpc.GracefulStop()
	c.wg.Wait()
	return nil
}

// AsHolder says the call is from the given Holder, the way a caller of the app
// says it. Every call made with the context it returns is served as them.
func (c *Client) AsHolder(ctx context.Context, v *go_app.Holder) context.Context {
	return auth.PlainProvider(auth.PlainOf(v)).Provide(ctx)
}

// AsNobody says nothing about who is calling, which is what a caller that has
// not authenticated looks like.
func (c *Client) AsNobody(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}

	md = md.Copy()
	md.Delete("authorization")

	return metadata.NewOutgoingContext(ctx, md)
}

// AsRoot says the call is from whoever administers the deployment, which is
// what a test is unless it says otherwise.
func (c *Client) AsRoot(ctx context.Context) context.Context {
	return c.AsHolder(ctx, c.Server.Admin)
}

// CreateTenant adds a Tenant, failing the test if it cannot.
func (c *Client) CreateTenant(ctx context.Context, x *X, alias string) *go_app.Tenant {
	x.TB().Helper()

	v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: alias}.Build())
	x.NoError(err)

	return v
}

// CreateHolder adds a Holder of the given Tenant, failing the test if it cannot.
func (c *Client) CreateHolder(ctx context.Context, x *X, tenant *go_app.TenantRef, alias string) *go_app.Holder {
	x.TB().Helper()

	v, err := c.Holder().Add(ctx, go_app.HolderAddRequest_builder{Tenant: tenant, Alias: alias}.Build())
	x.NoError(err)

	return v
}
