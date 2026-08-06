package grpcx_test

import (
	"context"
	"testing"

	"buf.build/go/protovalidate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/grpcx"
)

// checked runs a request through the validating interceptor and reports whether
// the handler ran, and what the caller was told.
func checked(t *testing.T, req any) (bool, error) {
	t.Helper()

	v, err := protovalidator(t)
	require.NoError(t, err)

	var ran bool
	_, err = grpcx.ValidateUnary(v)(t.Context(), req, &grpc.UnaryServerInfo{}, func(context.Context, any) (any, error) {
		ran = true
		return nil, nil
	})

	return ran, err
}

func TestValidate(t *testing.T) {
	id := make([]byte, 16)

	t.Run("a request that keeps to its own definition is served", func(t *testing.T) {
		x := require.New(t)

		ran, err := checked(t, go_app.AuditListRequest_builder{
			Filters: []*go_app.AuditFilter{
				go_app.AuditFilter_builder{ObjectId: id}.Build(),
			},
		}.Build())
		x.True(ran)
		x.NoError(err)
	})

	t.Run("an identifier that is not one is refused", func(t *testing.T) {
		x := require.New(t)

		ran, err := checked(t, go_app.AuditListRequest_builder{
			Filters: []*go_app.AuditFilter{
				go_app.AuditFilter_builder{ObjectId: []byte("nope")}.Build(),
			},
		}.Build())
		x.False(ran, "the server never sees it")
		x.Equal(codes.InvalidArgument, status.Code(err))
	})

	t.Run("a request that asks for unbounded work is refused", func(t *testing.T) {
		x := require.New(t)

		// Each filter is a predicate in the same query, so the request is what
		// says how much of the database to look at.
		fs := make([]*go_app.AuditFilter, 64)
		for i := range fs {
			fs[i] = go_app.AuditFilter_builder{ObjectId: id}.Build()
		}

		_, err := checked(t, go_app.AuditListRequest_builder{Filters: fs}.Build())
		x.Equal(codes.InvalidArgument, status.Code(err))
	})

	t.Run("what is optional stays optional", func(t *testing.T) {
		x := require.New(t)

		// `tenant_id` is sixteen bytes when it is there at all, and saying
		// nothing is saying nothing rather than saying zero bytes.
		ran, err := checked(t, &go_app.AuditListRequest{})
		x.True(ran)
		x.NoError(err)
	})

	t.Run("something that is not a message is left alone", func(t *testing.T) {
		x := require.New(t)

		ran, err := checked(t, "not a message")
		x.True(ran)
		x.NoError(err)
	})
}

// protovalidator is the validator the app is served with, built the way
// [grpcx.Validate] builds it.
func protovalidator(t *testing.T) (protovalidate.Validator, error) {
	t.Helper()
	return protovalidate.New()
}
