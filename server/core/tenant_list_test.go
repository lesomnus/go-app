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

// TestTenantList is the smaller half of [TestList]: the paging and the cursor
// are the same code and are tested there, so what is under test here is that
// this list is walled, filtered, and paged at all.
func TestTenantList(t *testing.T) {
	t.Run("a caller is answered with their own", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		c.CreateTenant(ctx, x, "hooli")

		// The wall makes this one, which is what makes the list worth having
		// anyway: without it there is no way to ask *which*, since the wall
		// answers NotFound for what is not theirs and says nothing about what
		// is.
		v, err := c.Tenant().List(c.AsAdminOf(ctx, x, acme), &go_app.TenantListRequest{})
		x.NoError(err)
		x.Len(v.GetItems(), 1)
		x.Equal(acme.GetId(), v.GetItems()[0].GetId())
	}))

	t.Run("a filter narrows it further", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		acme := c.CreateTenant(ctx, x, "acme")
		as := c.AsAdminOf(ctx, x, acme)

		v, err := c.Tenant().List(as, go_app.TenantListRequest_builder{
			Filters: []*go_app.TenantFilter{
				go_app.TenantFilter_builder{Ref: go_app.TenantByAlias("acme")}.Build(),
			},
		}.Build())
		x.NoError(err)
		x.Len(v.GetItems(), 1)

		// And one that names something else answers with nothing rather than
		// with everything.
		v, err = c.Tenant().List(as, go_app.TenantListRequest_builder{
			Filters: []*go_app.TenantFilter{
				go_app.TenantFilter_builder{Ref: go_app.TenantByAlias("hooli")}.Build(),
			},
		}.Build())
		x.NoError(err)
		x.Empty(v.GetItems())
	}))

	t.Run("a page at a time reads every row once", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// Through the ungated stack, which sees every Tenant: what is under
		// test is the paging and not the wall.
		want := []string{"root"}
		for i := range 9 {
			alias := fmt.Sprintf("t-%03d", i)
			c.CreateTenant(ctx, x, alias)
			want = append(want, alias)
		}

		var (
			got   []string
			after string
		)
		for range 100 {
			res, err := c.Ungated().Tenant().List(ctx, go_app.TenantListRequest_builder{
				Size:  z.Ptr(int32(4)),
				After: z.Ptr(after),
			}.Build())
			x.NoError(err)

			for _, v := range res.GetItems() {
				got = append(got, v.GetAlias())
			}

			after = res.GetNext()
			if after == "" {
				break
			}
		}

		x.Equal(want, got)
	}))

	t.Run("more filters than one list carries is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		fs := make([]*go_app.TenantFilter, core.FilterLimit+1)
		for i := range fs {
			fs[i] = go_app.TenantFilter_builder{Ref: go_app.TenantById(make([]byte, 16))}.Build()
		}

		_, err := c.Ungated().Tenant().List(ctx, go_app.TenantListRequest_builder{Filters: fs}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}
