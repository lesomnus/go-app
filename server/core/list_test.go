package core_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/core"
)

// page asks for one page, and fails the test if it cannot be had.
func page(ctx context.Context, x *ox.X, c *ox.Client, size int32, after string) *go_app.HolderListResponse {
	x.TB().Helper()

	v, err := c.Holder().List(ctx, go_app.HolderListRequest_builder{
		Size:  z.Ptr(size),
		After: z.Ptr(after),
	}.Build())
	x.NoError(err)

	return v
}

// walk reads the whole list a page at a time and answers with every alias it
// saw, in the order it saw them.
func walk(ctx context.Context, x *ox.X, c *ox.Client, size int32) []string {
	x.TB().Helper()

	var (
		vs    []string
		after string
	)
	for range 100 { // a bound, so a cursor that never advances fails rather than hangs
		res := page(ctx, x, c, size, after)
		for _, v := range res.GetItems() {
			vs = append(vs, v.GetAlias())
		}

		after = res.GetNext()
		if after == "" {
			return vs
		}
	}

	x.FailNow("the list never ended")
	return nil
}

func TestList(t *testing.T) {
	// seed puts `n` Holders in beside the admin that comes with the Tenant, and
	// answers with every alias there is, oldest first -- which is the order the
	// list is read in.
	seed := func(ctx context.Context, x *ox.X, c *ox.Client, n int) []string {
		x.TB().Helper()

		v := c.CreateTenant(ctx, x, "acme")

		vs := []string{"admin", "admin"} // the root's and acme's
		for i := range n {
			alias := fmt.Sprintf("h-%03d", i)
			c.CreateHolder(ctx, x, v.Ref(), alias)
			vs = append(vs, alias)
		}

		return vs
	}

	t.Run("a page at a time reads every row once", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		want := seed(ctx, x, c, 25)

		// The whole of what paging has to get right: not a row twice, not a row
		// missed, in the order the list declares.
		x.Equal(want, walk(ctx, x, c, 4))
	}))

	t.Run("the pages are the size that was asked for", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, 25)

		v := page(ctx, x, c, 4, "")
		x.Len(v.GetItems(), 4)
		x.NotEmpty(v.GetNext())
	}))

	t.Run("the last page says there is no next", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, 2)

		// Four rows and a page of four: the page is full, and it is still the
		// last one. Answering with a cursor here would send a caller back for
		// an empty page, which is the off-by-one everybody writes once.
		v := page(ctx, x, c, 4, "")
		x.Len(v.GetItems(), 4)
		x.Empty(v.GetNext())
	}))

	t.Run("a size nobody asked for is the usual one", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, core.PageSize+10)

		v := page(ctx, x, c, 0, "")
		x.Len(v.GetItems(), core.PageSize)
	}))

	t.Run("a size past the cap is the cap", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, core.PageLimit+10)

		// Not an error: a caller asking for more than there is meant no harm.
		// It is not the whole table either.
		v := page(ctx, x, c, 1_000_000, "")
		x.Len(v.GetItems(), core.PageLimit)
	}))

	t.Run("a row added ahead of the page does not shift it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		want := seed(ctx, x, c, 10)
		acme, err := c.Tenant().Get(ctx, go_app.TenantGetByAlias("acme"))
		x.NoError(err)

		first := page(ctx, x, c, 4, "")
		x.Len(first.GetItems(), 4)

		// This is why it is a cursor and not an offset. The list is read oldest
		// first, so a Holder added now lands at the *end*; with an offset it
		// would still be a row that was counted, and something in the middle
		// would be seen twice or not at all.
		c.CreateHolder(ctx, x, acme.Ref(), "z-late")
		want = append(want, "z-late")

		var vs []string
		for _, v := range first.GetItems() {
			vs = append(vs, v.GetAlias())
		}
		for after := first.GetNext(); after != ""; {
			res := page(ctx, x, c, 4, after)
			for _, v := range res.GetItems() {
				vs = append(vs, v.GetAlias())
			}
			after = res.GetNext()
		}

		x.Equal(want, vs)
	}))

	t.Run("what is not a cursor is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, 2)

		_, err := c.Holder().List(ctx, go_app.HolderListRequest_builder{
			After: z.Ptr("not a cursor"),
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}
