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

var roasterService = strings.TrimSuffix(go_app.RoasterService_Get_FullMethodName, "Get")

type RoasterServiceServer struct {
	Server
	go_app.RoasterServiceServer
}

func NewRoasterServiceServer(s Server) RoasterServiceServer {
	return RoasterServiceServer{s, s.Next().Roaster()}
}

func (s Server) Roaster() go_app.RoasterServiceServer {
	return NewRoasterServiceServer(s)
}

// Watch answers with the Roasters this caller may see, as they are now and as
// they change. It is `CoffeeService.Watch` in every way; see there.
//
// For most callers this is one Roaster, and the stream is then a way of being
// told when their own is renamed or taken away. It is more than one for a
// caller a [gate.Policy] said so about, which is the case the wall alone has no
// answer for and the reason `RoasterService.List` exists at all.
func (s RoasterServiceServer) Watch(req *go_app.RoasterWatchRequest, out grpc.ServerStreamingServer[go_app.RoasterWatchResponse]) error {
	ctx := out.Context()

	if n := len(req.GetFilters()); n > core.FilterLimit {
		return status.Errorf(codes.InvalidArgument,
			"filters: %d of them, and %d is the most one watch carries", n, core.FilterLimit)
	}

	return stream(ctx, s.w, roasterService,
		func(sent seen) error { return s.snapshot(ctx, req, out, sent) },
		func(ks map[uuid.UUID]string, sent seen) error {
			items := make([]*go_app.RoasterWatchItem, 0, len(ks))
			for k, action := range ks {
				v, err := s.read(ctx, req, k)
				if err != nil {
					return err
				}
				if v == nil && !sent[k] {
					continue
				}

				sent[k] = v != nil
				items = append(items, go_app.RoasterWatchItem_builder{
					Id:     k[:],
					Value:  v,
					Action: &action,
				}.Build())
			}
			if len(items) == 0 {
				return nil
			}

			return out.Send(go_app.RoasterWatchResponse_builder{Items: items}.Build())
		})
}

func (s RoasterServiceServer) snapshot(
	ctx context.Context,
	req *go_app.RoasterWatchRequest,
	out grpc.ServerStreamingServer[go_app.RoasterWatchResponse],
	sent seen,
) error {
	var after string
	for {
		res, err := s.RoasterServiceServer.List(ctx, go_app.RoasterListRequest_builder{
			Filters: req.GetFilters(),
			After:   &after,
		}.Build())
		if err != nil {
			return err
		}

		items := make([]*go_app.RoasterWatchItem, 0, len(res.GetItems()))
		for _, v := range res.GetItems() {
			k, err := uuid.FromBytes(v.GetId())
			if err != nil {
				return status.Errorf(codes.Internal, "a roaster without an identifier: %s", err)
			}

			sent[k] = true
			items = append(items, go_app.RoasterWatchItem_builder{Id: v.GetId(), Value: v}.Build())
		}
		if len(items) > 0 {
			if err := out.Send(go_app.RoasterWatchResponse_builder{Items: items}.Build()); err != nil {
				return err
			}
		}

		after = res.GetNext()
		if after == "" {
			return nil
		}
	}
}

func (s RoasterServiceServer) read(ctx context.Context, req *go_app.RoasterWatchRequest, k uuid.UUID) (*go_app.Roaster, error) {
	v, err := s.RoasterServiceServer.Get(ctx, go_app.RoasterGetById(k[:]))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}

		return nil, err
	}
	if !matchesRoaster(req.GetFilters(), v) {
		return nil, nil
	}

	return v, nil
}

// matchesRoaster is `bare.RoasterPick` written again in Go; see [matchesCoffee]
// for why that is said out loud rather than hidden.
func matchesRoaster(fs []*go_app.RoasterFilter, v *go_app.Roaster) bool {
	if len(fs) == 0 {
		return true
	}

	for _, f := range fs {
		if matchesRoasterRef(f.GetRef(), v) {
			return true
		}
	}

	return false
}

func matchesRoasterRef(r *go_app.RoasterRef, v *go_app.Roaster) bool {
	switch r.WhichKey() {
	case go_app.RoasterRef_Id_case:
		return bytes.Equal(r.GetId(), v.GetId())

	case go_app.RoasterRef_Alias_case:
		return r.GetAlias() == v.GetAlias()

	default:
		return false
	}
}
