package core_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/coffee"
	"github.com/lesomnus/go-app/internal/ox"
	"github.com/lesomnus/go-app/server/core"
)

func TestAdd(t *testing.T) {
	t.Run("an alias is normalized rather than refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		// A request field is a value the caller wants stored, so trimming and
		// folding it is a courtesy. A patch document is not, and is refused;
		// see `core.checkAlias`.
		v, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{Alias: "  Beans "}.Build())
		x.NoError(err)
		x.Equal("beans", v.GetAlias())

		// And is displayed by something rather than by nothing.
		x.Equal("beans", v.GetName())
	}))

	t.Run("an alias that is not one is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		for _, alias := range []string{"", "Beans & Co.", "-beans", "beans--co"} {
			_, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{Alias: alias}.Build())
			x.ErrCode(codes.InvalidArgument, err)
		}
	}))

	t.Run("a coffee has to be somebody's", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Coffee().Add(ctx, go_app.CoffeeAddRequest_builder{Alias: "ethiopia"}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))

	t.Run("the identifier that names nobody is not one to hold", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{
			Id:    core.NobodyId[:],
			Alias: "beans",
		}.Build())
		x.ErrCode(codes.InvalidArgument, err)
	}))
}

// TestEraseSoftly is what a soft erasure is for and what it costs, in one test.
func TestEraseSoftly(t *testing.T) {
	t.Run("an erased coffee is gone, and its identifier stays taken", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		v := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		_, err := c.Coffee().Erase(ctx, v.Ref())
		x.NoError(err)

		// Gone to every read there is, because the predicate that says so is in
		// the query rather than in a rule somebody has to remember.
		_, err = c.Coffee().Get(ctx, go_app.CoffeeGetById(v.GetId()))
		x.ErrCode(codes.NotFound, err)

		// And still there, which is the point: nothing else will ever be
		// answered by that identifier.
		u, err := c.Server.Db.Coffee.Get(ctx, mustId(x, v.GetId()))
		x.NoError(err)
		x.NotNil(u.DateErased)
	}))

	t.Run("the alias comes free again", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		v := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")

		// Taken while it is there.
		_, err := c.Coffee().Add(ctx, go_app.CoffeeAddRequest_builder{
			Roaster: beans.Ref(),
			Alias:   "ethiopia",
		}.Build())
		x.ErrCode(codes.AlreadyExists, err)

		x.NoError(erase(ctx, c, v))

		// And free once it is not, which is the partial index doing it: an
		// identifier is forever and a name is not.
		u, err := c.Coffee().Add(ctx, go_app.CoffeeAddRequest_builder{
			Roaster: beans.Ref(),
			Alias:   "ethiopia",
		}.Build())
		x.NoError(err)
		x.NotEqual(v.GetId(), u.GetId())
	}))

	t.Run("an alias is only taken within its roaster", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		peaks := c.CreateRoaster(ctx, x, "peaks")

		c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
		c.CreateCoffee(ctx, x, peaks.Ref(), "ethiopia")
	}))
}

// TestEraseARoaster is the lesson soft deletion teaches at a join, and the
// reason `core` has anything to say about `Erase` at all.
func TestEraseARoaster(t *testing.T) {
	t.Run("it takes its coffees with it, erased ones included", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		beans := c.CreateRoaster(ctx, x, "beans")
		here := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
		gone := c.CreateCoffee(ctx, x, beans.Ref(), "kenya")

		// Soft deletion does not cascade and a foreign key does not care that a
		// row is "gone", so without `core.RoasterServiceServer.Erase` this
		// would fail on that key -- for ever, however many Coffees had been
		// erased first.
		x.NoError(erase(ctx, c, gone))

		_, err := c.Roaster().Erase(ctx, beans.Ref())
		x.NoError(err)

		for _, v := range []*go_app.Coffee{here, gone} {
			n, err := c.Server.Db.Coffee.Query().
				Where(coffee.IDEQ(mustId(x, v.GetId()))).
				Count(ctx)
			x.NoError(err)
			x.Zero(n, "for real, since there is nothing left for it to belong to")
		}
	}))

	t.Run("erasing what is not there is not a failure", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		_, err := c.Roaster().Erase(ctx, go_app.RoasterByAlias("nobody"))
		x.NoError(err)
	}))
}

func TestList(t *testing.T) {
	// seed puts `n` Coffees in and answers with every alias there is, oldest
	// first -- which is the order the list is read in.
	seed := func(ctx context.Context, x *ox.X, c *ox.Client, n int) []string {
		x.TB().Helper()

		beans := c.CreateRoaster(ctx, x, "beans")

		vs := make([]string, 0, n)
		for i := range n {
			alias := fmt.Sprintf("c-%03d", i)
			c.CreateCoffee(ctx, x, beans.Ref(), alias)
			vs = append(vs, alias)
		}

		return vs
	}

	page := func(ctx context.Context, x *ox.X, c *ox.Client, size int32, after string) *go_app.CoffeeListResponse {
		x.TB().Helper()

		v, err := c.Coffee().List(ctx, go_app.CoffeeListRequest_builder{
			Size:  z.Ptr(size),
			After: z.Ptr(after),
		}.Build())
		x.NoError(err)

		return v
	}

	t.Run("a page at a time reads every row once", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		want := seed(ctx, x, c, 25)

		var (
			got   []string
			after string
		)
		for range 100 { // a bound, so a cursor that never advances fails rather than hangs
			res := page(ctx, x, c, 4, after)
			for _, v := range res.GetItems() {
				got = append(got, v.GetAlias())
			}

			after = res.GetNext()
			if after == "" {
				break
			}
		}

		// The whole of what paging has to get right: not a row twice, not a row
		// missed, in the order the list declares.
		x.Equal(want, got)
	}))

	t.Run("the last page says there is no next", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, 4)

		// Four rows and a page of four: the page is full and is still the last
		// one. A cursor here would send the caller back for an empty page.
		v := page(ctx, x, c, 4, "")
		x.Len(v.GetItems(), 4)
		x.Empty(v.GetNext())
	}))

	t.Run("a size nobody asked for is the usual one, and one past the cap is the cap", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, core.PageLimit+10)

		x.Len(page(ctx, x, c, 0, "").GetItems(), core.PageSize)
		x.Len(page(ctx, x, c, 1_000_000, "").GetItems(), core.PageLimit)
	}))

	t.Run("an erased one is not in it", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		seed(ctx, x, c, 3)

		v := page(ctx, x, c, 10, "")
		x.Len(v.GetItems(), 3)

		x.NoError(erase(ctx, c, v.GetItems()[0]))
		x.Len(page(ctx, x, c, 10, "").GetItems(), 2,
			"a hand-written list goes through the same narrowing the generated reads do")
	}))

	t.Run("more filters than one list carries is refused", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		fs := make([]*go_app.CoffeeFilter, core.FilterLimit+1)
		for i := range fs {
			fs[i] = go_app.CoffeeFilter_builder{Ref: go_app.CoffeeById(make([]byte, 16))}.Build()
		}

		// Refused rather than clamped, which is the opposite of what the page
		// size gets: each filter is a predicate in the same query, so this is
		// the request saying how much of the database to read.
		_, err := c.Coffee().List(ctx, go_app.CoffeeListRequest_builder{Filters: fs}.Build())
		x.ErrCode(codes.InvalidArgument, err)

		_, err = c.Coffee().List(ctx, go_app.CoffeeListRequest_builder{Filters: fs[:core.FilterLimit]}.Build())
		x.NoError(err)
	}))
}

// erase takes a Coffee away, and is here because every test that is about what
// erasure *did* has to do it first.
func erase(ctx context.Context, c *ox.Client, v *go_app.Coffee) error {
	_, err := c.Coffee().Erase(ctx, v.Ref())
	return err
}

// mustId reads an identifier the way the database holds one.
func mustId(x *ox.X, v []byte) uuid.UUID {
	x.TB().Helper()

	k, err := uuid.FromBytes(v)
	x.NoError(err)

	return k
}
