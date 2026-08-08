package watch

import (
	"bytes"
	"context"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/core"
)

// coffeeService is the prefix of every RPC of the Coffee service, which is how
// a change is known to be about a Coffee. The service is named for the entity
// it is about, so the name carries it; see `bare.Change.By`.
var coffeeService = strings.TrimSuffix(go_app.CoffeeService_Get_FullMethodName, "Get")

type CoffeeServiceServer struct {
	Server
	go_app.CoffeeServiceServer
}

func NewCoffeeServiceServer(s Server) CoffeeServiceServer {
	return CoffeeServiceServer{s, s.Next().Coffee()}
}

func (s Server) Coffee() go_app.CoffeeServiceServer {
	return NewCoffeeServiceServer(s)
}

// Watch answers with the Coffees this caller may see, as they are now and as
// they change.
//
// What is sent is **state and never a delta**, which is what makes a stream
// that missed something still correct: the next item about a Coffee carries the
// whole of it, so a client converges rather than replays. It is also what makes
// the first message safe to duplicate against the ones after it.
//
// Three things it does in an order that matters:
//
//  1. Subscribe, *before* reading anything. A stream that read first would lose
//     whatever changed while it was reading, and no amount of care afterwards
//     would tell it that it had.
//  2. Send what matches now, page by page, through the same `List` a caller
//     would have called. That is the snapshot, and it is why a client does not
//     have to list and then subscribe and race the two.
//  3. Then, per event, read the row back and send it as it is now.
//
// The read in (3) is what keeps the wall out of this file. A Coffee is fetched
// through the servers behind this one, with the context of the caller who
// asked, so it is walled by the same predicate as every other read there is;
// a Coffee they may not see comes back NotFound and is never sent. The filters
// are the caller's own and are tested here, which is a different kind of thing:
// getting a filter wrong shows a caller a row of *theirs* that they asked not
// to be shown, and getting the wall wrong shows them somebody else's.
func (s CoffeeServiceServer) Watch(req *go_app.CoffeeWatchRequest, out grpc.ServerStreamingServer[go_app.CoffeeWatchResponse]) error {
	ctx := out.Context()

	// Refused before anything is subscribed to, and with the same bound the
	// list it reads has: a watch is that list, over and over.
	if n := len(req.GetFilters()); n > core.FilterLimit {
		return status.Errorf(codes.InvalidArgument,
			"filters: %d of them, and %d is the most one watch carries", n, core.FilterLimit)
	}

	return stream(ctx, s.w, coffeeService,
		func(sent seen) error { return s.snapshot(ctx, req, out, sent) },
		func(ks map[uuid.UUID]string, sent seen) error {
			items := make([]*go_app.CoffeeWatchItem, 0, len(ks))
			for k, action := range ks {
				v, err := s.read(ctx, req, k)
				if err != nil {
					return err
				}
				if v == nil && !sent[k] {
					// Not theirs, or not what they asked for, and they have
					// never been told otherwise. There is nothing to say.
					continue
				}

				sent[k] = v != nil
				items = append(items, go_app.CoffeeWatchItem_builder{
					Id:     k[:],
					Value:  v,
					Action: &action,
				}.Build())
			}
			if len(items) == 0 {
				return nil
			}

			return out.Send(go_app.CoffeeWatchResponse_builder{Items: items}.Build())
		})
}

// snapshot sends everything that matches now, in the pages `List` answers with.
func (s CoffeeServiceServer) snapshot(
	ctx context.Context,
	req *go_app.CoffeeWatchRequest,
	out grpc.ServerStreamingServer[go_app.CoffeeWatchResponse],
	sent seen,
) error {
	var after string
	for {
		res, err := s.CoffeeServiceServer.List(ctx, go_app.CoffeeListRequest_builder{
			Filters: req.GetFilters(),
			After:   &after,
		}.Build())
		if err != nil {
			return err
		}

		items := make([]*go_app.CoffeeWatchItem, 0, len(res.GetItems()))
		for _, v := range res.GetItems() {
			k, err := uuid.FromBytes(v.GetId())
			if err != nil {
				return status.Errorf(codes.Internal, "a coffee without an identifier: %s", err)
			}

			sent[k] = true
			// No action: nobody asked for this, it is what is already there.
			items = append(items, go_app.CoffeeWatchItem_builder{Id: v.GetId(), Value: v}.Build())
		}
		if len(items) > 0 {
			if err := out.Send(go_app.CoffeeWatchResponse_builder{Items: items}.Build()); err != nil {
				return err
			}
		}

		after = res.GetNext()
		if after == "" {
			return nil
		}
	}
}

// read answers with the Coffee as it is now, and nil for one this caller may no
// longer see -- erased, out of their Roasters, or out of the filters they named.
func (s CoffeeServiceServer) read(ctx context.Context, req *go_app.CoffeeWatchRequest, k uuid.UUID) (*go_app.Coffee, error) {
	v, err := s.CoffeeServiceServer.Get(ctx, go_app.CoffeeGetById(k[:]))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}

		return nil, err
	}
	if !matchesCoffee(req.GetFilters(), v) {
		return nil, nil
	}

	return v, nil
}

// matchesCoffee reports whether `v` is one of what the filters name, and true
// for a watch that named none.
//
// It is `bare.CoffeePick` written again, in Go rather than as a predicate, and
// that is worth knowing about rather than hiding: a snapshot is a query and a
// stream is a row that has already been read, and no one spelling serves both.
// The two are held together by a test that puts the same filters through both
// roads -- see coffee_test.go -- and by both being this small.
func matchesCoffee(fs []*go_app.CoffeeFilter, v *go_app.Coffee) bool {
	if len(fs) == 0 {
		return true
	}

	// Any of them, the way `List` reads them.
	for _, f := range fs {
		if matchesCoffeeRef(f.GetRef(), v) {
			return true
		}
	}

	return false
}

func matchesCoffeeRef(r *go_app.CoffeeRef, v *go_app.Coffee) bool {
	switch r.WhichKey() {
	case go_app.CoffeeRef_Id_case:
		return bytes.Equal(r.GetId(), v.GetId())

	case go_app.CoffeeRef_Slug_case:
		k := r.GetSlug()
		return k.GetAlias() == v.GetAlias() && matchesRoasterRef(k.GetRoaster(), v.GetRoaster())

	default:
		// A reference that names nothing was refused by the snapshot, which
		// runs first and reads it the same way every other read does.
		return false
	}
}
