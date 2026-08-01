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
	"google.golang.org/grpc/test/bufconn"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server"
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

	s := server.TerminalOf(c.Server.Server)
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

// CreateTenant adds a Tenant, failing the test if it cannot.
func (c *Client) CreateTenant(ctx context.Context, x *X, alias string) *go_app.Tenant {
	x.TB().Helper()

	v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: alias}.Build())
	x.NoError(err)

	return v
}

// CreateUser adds a User of the given Tenant, failing the test if it cannot.
func (c *Client) CreateUser(ctx context.Context, x *X, tenant *go_app.TenantRef, alias string) *go_app.User {
	x.TB().Helper()

	v, err := c.User().Add(ctx, go_app.UserAddRequest_builder{Tenant: tenant, Alias: alias}.Build())
	x.NoError(err)

	return v
}
