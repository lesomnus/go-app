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

var tenantService = strings.TrimSuffix(go_app.TenantService_Get_FullMethodName, "Get")

type TenantServiceServer struct {
	Server
	go_app.TenantServiceServer
}

func NewTenantServiceServer(s Server) TenantServiceServer {
	return TenantServiceServer{s, s.Next().Tenant()}
}

func (s Server) Tenant() go_app.TenantServiceServer {
	return NewTenantServiceServer(s)
}

// Watch answers with the Tenants this caller may see, as they are now and as
// they change. It is `HolderService.Watch` in every way; see there.
//
// For most callers this is one Tenant, and the stream is then a way of being
// told when their own is renamed or taken away. It is more than one for a
// caller a [gate.Policy] said so about, which is the case the wall alone has no
// answer for and the reason `TenantService.List` exists at all.
func (s TenantServiceServer) Watch(req *go_app.TenantWatchRequest, out grpc.ServerStreamingServer[go_app.TenantWatchResponse]) error {
	ctx := out.Context()

	if n := len(req.GetFilters()); n > core.FilterLimit {
		return status.Errorf(codes.InvalidArgument,
			"filters: %d of them, and %d is the most one watch carries", n, core.FilterLimit)
	}

	return stream(ctx, s.w, tenantService,
		func(sent seen) error { return s.snapshot(ctx, req, out, sent) },
		func(ks map[uuid.UUID]string, sent seen) error {
			items := make([]*go_app.TenantWatchItem, 0, len(ks))
			for k, action := range ks {
				v, err := s.read(ctx, req, k)
				if err != nil {
					return err
				}
				if v == nil && !sent[k] {
					continue
				}

				sent[k] = v != nil
				items = append(items, go_app.TenantWatchItem_builder{
					Id:     k[:],
					Value:  v,
					Action: &action,
				}.Build())
			}
			if len(items) == 0 {
				return nil
			}

			return out.Send(go_app.TenantWatchResponse_builder{Items: items}.Build())
		})
}

func (s TenantServiceServer) snapshot(
	ctx context.Context,
	req *go_app.TenantWatchRequest,
	out grpc.ServerStreamingServer[go_app.TenantWatchResponse],
	sent seen,
) error {
	var after string
	for {
		res, err := s.TenantServiceServer.List(ctx, go_app.TenantListRequest_builder{
			Filters: req.GetFilters(),
			After:   &after,
		}.Build())
		if err != nil {
			return err
		}

		items := make([]*go_app.TenantWatchItem, 0, len(res.GetItems()))
		for _, v := range res.GetItems() {
			k, err := uuid.FromBytes(v.GetId())
			if err != nil {
				return status.Errorf(codes.Internal, "a tenant without an identifier: %s", err)
			}

			sent[k] = true
			items = append(items, go_app.TenantWatchItem_builder{Id: v.GetId(), Value: v}.Build())
		}
		if len(items) > 0 {
			if err := out.Send(go_app.TenantWatchResponse_builder{Items: items}.Build()); err != nil {
				return err
			}
		}

		after = res.GetNext()
		if after == "" {
			return nil
		}
	}
}

func (s TenantServiceServer) read(ctx context.Context, req *go_app.TenantWatchRequest, k uuid.UUID) (*go_app.Tenant, error) {
	v, err := s.TenantServiceServer.Get(ctx, go_app.TenantGetById(k[:]))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, nil
		}

		return nil, err
	}
	if !matchesTenant(req.GetFilters(), v) {
		return nil, nil
	}

	return v, nil
}

// matchesTenant is `bare.TenantPick` written again in Go; see [matchesHolder]
// for why that is said out loud rather than hidden.
func matchesTenant(fs []*go_app.TenantFilter, v *go_app.Tenant) bool {
	if len(fs) == 0 {
		return true
	}

	for _, f := range fs {
		if matchesTenantRef(f.GetRef(), v) {
			return true
		}
	}

	return false
}

func matchesTenantRef(r *go_app.TenantRef, v *go_app.Tenant) bool {
	switch r.WhichKey() {
	case go_app.TenantRef_Id_case:
		return bytes.Equal(r.GetId(), v.GetId())

	case go_app.TenantRef_Alias_case:
		return r.GetAlias() == v.GetAlias()

	default:
		return false
	}
}
