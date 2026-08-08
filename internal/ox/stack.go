package ox

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"testing"

	"github.com/lesomnus/otx/log"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/lesomnus/go-app/internal/grpcx"
	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/gate"
)

// bufSize is the buffer of the in-memory listener; large enough that a message
// never blocks on it.
const bufSize = 1 << 20

// Stack is the interceptor chain the app is served with, and nothing about what
// it serves.
//
// It is separated from [Server] on purpose, and the seam is the store: this
// file knows about authentication, rate limiting and the gate, and it does not
// know that there is a database or that there are entities. An app that keeps
// the transport and replaces the store keeps this and writes its own [Server];
// see docs/DESIGN.md, "What a variant may remove".
type Stack struct {
	tb testing.TB

	// Log is where everything served through this writes.
	Log *slog.Logger

	// Tokens is the bearer store this app is served with, so that a test can
	// travel as a credential that allows less than its holder does.
	Tokens *auth.MemTokenStore

	// Gate is what `server/gate` decides with, and is nothing unless a test
	// says otherwise: a caller who said who they are may do anything, and an
	// anonymous one may do nothing.
	Gate []gate.Option

	// Limit is how often one caller may call, and is nothing unless a test says
	// otherwise -- which is also what `go-app.yaml` says unless a deployment
	// writes a rate down.
	Limit grpcx.Limiter

	// Extra is installed behind the gate and in front of the handler, which is
	// where `cmd/serve.go` puts the watch interceptor. An app with nothing to
	// put there leaves it empty.
	Extra []grpc.ServerOption
}

func NewStack(tb testing.TB) *Stack {
	tb.Helper()

	return &Stack{
		tb:     tb,
		Log:    logger(tb),
		Tokens: auth.NewMemTokenStore(),
	}
}

// Options is the chain, in the order `cmd/serve.go` installs it -- so a test
// travels the same road a request does in production. The credentials are the
// one thing that differs, since the connection never leaves the process.
func (s *Stack) Options() []grpc.ServerOption {
	// First, so that everything behind it writes into the log of the test: the
	// panic recovery, and the record every call leaves. It has to be a stats
	// handler rather than an interceptor for the second of those -- the log is
	// one too, and a stats handler never sees what an interceptor put in the
	// context.
	opts := []grpc.ServerOption{
		grpcx.Seed(func(ctx context.Context) context.Context {
			return log.Into(ctx, s.Log)
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
	// may call, and what they may do.
	opts = append(opts, grpcx.Limit(s.Limit, gate.BySubject())...)
	opts = append(opts, gate.Interceptor(s.Gate...)...)
	opts = append(opts, s.Extra...)
	opts = append(opts, grpc.Creds(insecure.NewCredentials()))

	return opts
}

// Serve builds a gRPC server with this chain, registers whatever `register`
// registers, and starts it on an in-memory listener.
//
// What comes back is the connection to it and the way to stop it. Nothing binds
// a port, so tests do not race each other for one.
func (s *Stack) Serve(register func(g *grpc.Server)) (*grpc.ClientConn, func()) {
	s.tb.Helper()
	x := require.New(s.tb)

	g := grpc.NewServer(s.Options()...)
	register(g)

	l := bufconn.Listen(bufSize)
	conn, err := grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return l.DialContext(ctx)
		}),
	)
	x.NoError(err)

	var wg sync.WaitGroup
	wg.Go(func() {
		if err := g.Serve(l); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			// Not `require`, which must be called from the test goroutine.
			s.tb.Errorf("serve: %s", err)
		}
	})

	return conn, func() {
		conn.Close()
		g.GracefulStop()
		wg.Wait()
	}
}

// As says the call is from the given subject, the way a caller of the app says
// it. Every call made with the context it returns is served as them.
func (s *Stack) As(ctx context.Context, subject string) context.Context {
	return auth.PlainProvider(subject).Provide(ctx)
}

// AsBearer says the call comes with the given token, which is how a test
// travels as a credential that allows less than its subject does. Put one in
// [Stack.Tokens] first.
func (s *Stack) AsBearer(ctx context.Context, token string) context.Context {
	return auth.BearerProvider(token).Provide(ctx)
}

// AsNobody says nothing about who is calling, which is what an anonymous caller
// looks like. It is a caller like any other; see `server/frame`.
func (s *Stack) AsNobody(ctx context.Context) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ctx
	}

	md = md.Copy()
	md.Delete("authorization")

	return metadata.NewOutgoingContext(ctx, md)
}
