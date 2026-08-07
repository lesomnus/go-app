package gate_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/protobuf-orm/protoc-gen-orm-ent/runtime/enttx"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/frame"
)

// Putting the stack on a transaction rebuilds every layer of it. What must not
// happen is a layer being left out along the way -- for this one, that would be
// a wall that is there outside the transaction and gone inside it.
//
// These call the servers directly rather than through gRPC, since a rebound
// stack is not the one being served; the frame is put into the context by hand
// because that is what the interceptor would have done.
// mustId reads an identifier the way a scope holds one.
func mustId(x *ox.X, v []byte) uuid.UUID {
	x.TB().Helper()

	k, err := uuid.FromBytes(v)
	x.NoError(err)

	return k
}

func TestWithDriver(t *testing.T) {
	t.Run("the wall is still there inside the transaction", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		p := setup(ctx, x, c)

		// Read past the wall, since this is arranging the test rather than
		// being it: what is under test is further down.
		john, err := c.Ungated().Holder().Get(ctx, go_app.HolderGetById(p.john.GetId()).
			WithSelect(func(s *go_app.HolderSelect) {
				s.SetTenant(go_app.TenantSelect_builder{}.Build())
			}))
		x.NoError(err)

		drv, tx, err := enttx.Begin(ctx, c.Server.Db.Driver())
		x.NoError(err)
		defer tx.Rollback()

		s, err := enttx.Rebind(c.Server.Server, drv)
		x.NoError(err)

		// A request nobody vouched for is still refused, which is the gate
		// answering: had it been dropped from the rebuilt stack, `core` and the
		// bare server would have served this.
		_, err = s.Holder().Get(ctx, go_app.HolderGetById(p.erlich.GetId()))
		x.ErrCode(codes.Unauthenticated, err)

		// And another tenant's Holder is still not there to be seen.
		//
		// The frame says what john may see, because nothing else will: what
		// works out a scope is an interceptor, and a call made straight to the
		// servers does not pass one. A frame that says nothing sees nothing,
		// which is the right way for that to fail and is why it is said here.
		as := frame.Into(ctx, frame.New(john, frame.Whole()).
			WithScope(frame.Only(mustId(x, john.GetTenant().GetId()))))
		_, err = s.Holder().Get(as, go_app.HolderGetById(p.erlich.GetId()))
		x.ErrCode(codes.NotFound, err)

		// `core` is in it too: it normalizes the alias it is given.
		alias := " Johnny "
		v, err := s.Holder().Patch(as, go_app.HolderPatchRequest_builder{
			Ref:   john.Ref(),
			Alias: &alias,
		}.Build())
		x.NoError(err)
		x.Equal("johnny", v.GetAlias())

		// The write is inside the transaction, so letting it go undoes it. The
		// connection is single, so nothing is read from outside until then.
		x.NoError(tx.Rollback())

		u, err := c.Ungated().Holder().Get(ctx, go_app.HolderGetById(p.john.GetId()))
		x.NoError(err)
		x.Equal("john", u.GetAlias())
	}))
}
