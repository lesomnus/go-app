// Package ox holds the test fixtures of the app.
//
// A test asks for a client and gets a whole app behind it: an empty database
// in memory, the server stack of `server/core`, and a gRPC connection that
// never leaves the process.
//
//	func TestTenantAdd(t *testing.T) {
//		t.Run("alias is normalized", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
//			v, err := c.Tenant().Add(ctx, go_app.TenantAddRequest_builder{Alias: " Acme "}.Build())
//			x.NoError(err)
//			x.Equal("acme", v.GetAlias())
//		}))
//	}
package ox

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// X is what a test asserts with; it is a [require.Assertions] that knows about
// gRPC.
type X struct {
	tb testing.TB
	*require.Assertions
}

func NewX(tb testing.TB) *X {
	return &X{tb: tb, Assertions: require.New(tb)}
}

func (x *X) TB() testing.TB {
	return x.tb
}

// ErrCode asserts that `err` is a gRPC error of the given code, reporting both
// codes by name if it is not.
func (x *X) ErrCode(expected codes.Code, err error) {
	x.tb.Helper()

	actual := status.Code(err)
	if expected == actual {
		return
	}

	x.Fail(fmt.Sprintf(""+
		"Code not equal: \n"+
		"expected: %2d %s\n"+
		"actual  : %2d %s\n"+
		"error   : %v\n",
		expected, expected,
		actual, actual,
		err,
	))
}

// T runs `run` against a fresh app. The context it hands over is the one of the
// test, so it is canceled as soon as the test is done.
func T(run func(ctx context.Context, x *X, c *Client)) func(t *testing.T) {
	return func(t *testing.T) {
		s := NewServer(t)
		c := NewClient(t, s)
		defer c.Close()

		run(t.Context(), NewX(t), c)
	}
}
