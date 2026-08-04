package core

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/holder"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/server/bare"
)

type HolderServiceServer struct {
	Server
	go_app.HolderServiceServer
}

func NewHolderServiceServer(s Server) HolderServiceServer {
	return HolderServiceServer{s, s.Next().Holder()}
}

func (s Server) Holder() go_app.HolderServiceServer {
	return NewHolderServiceServer(s)
}

func (s HolderServiceServer) Add(ctx context.Context, req *go_app.HolderAddRequest) (*go_app.Holder, error) {
	if req.GetTenant() == nil {
		// A Holder only exists within a Tenant.
		return nil, status.Error(codes.InvalidArgument, "tenant: must be given")
	}
	if err := CheckId(req.GetId()); err != nil {
		return nil, err
	}

	alias, err := ParseAlias(req.GetAlias())
	if err != nil {
		return nil, err
	}

	req.SetAlias(alias)
	if req.GetName() == "" {
		req.SetName(alias)
	}

	return s.HolderServiceServer.Add(ctx, req)
}

// ListLimit is the most Holders [HolderServiceServer.List] answers with.
const ListLimit = 100

// List answers with the Holders that match any of the given filters, or with
// every Holder if there is none.
//
// It is not a CRUD operation, so nothing generates it; this is the plainest
// thing that works and it is meant to be rewritten. A real one would page,
// order and filter by what the app is actually asked about.
func (s HolderServiceServer) List(ctx context.Context, req *go_app.HolderListRequest) (*go_app.HolderListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	q := db.Holder.Query()
	if fs := req.GetFilters(); len(fs) > 0 {
		ps := make([]predicate.Holder, 0, len(fs))
		for i, f := range fs {
			p, err := bare.HolderPick(f.GetRef())
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(holder.Or(ps...))
	}

	// The Tenant comes with each of them: almost everything that reads a list
	// wants to know whose they are, `server/gate` before anyone else.
	q.WithTenant()

	// Oldest first, so that what is answered does not depend on the order the
	// database happens to hold the rows in.
	us, err := q.Order(holder.ByDateCreated()).Limit(ListLimit).All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*go_app.Holder, len(us))
	for i, u := range us {
		items[i] = u.Proto()
	}

	return go_app.HolderListResponse_builder{Items: items}.Build(), nil
}

func (s HolderServiceServer) Patch(ctx context.Context, req *go_app.HolderPatchRequest) (*go_app.Holder, error) {
	if req.HasAlias() {
		alias, err := ParseAlias(req.GetAlias())
		if err != nil {
			return nil, err
		}

		req.SetAlias(alias)
	}

	return s.HolderServiceServer.Patch(ctx, req)
}

func (s HolderServiceServer) Apply(ctx context.Context, req *go_app.HolderApplyRequest) (*go_app.Holder, error) {
	if err := checkAlias(holderEntity, req.GetPatch()); err != nil {
		return nil, err
	}

	return s.HolderServiceServer.Apply(ctx, req)
}
