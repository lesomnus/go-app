package gate

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
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
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) && !names(f, req.GetTenant()) {
		return nil, errForbidden("add a holder to")
	}

	return s.HolderServiceServer.Add(ctx, req)
}

func (s HolderServiceServer) Get(ctx context.Context, req *go_app.HolderGetRequest) (*go_app.Holder, error) {
	v, err := s.get(ctx, req)
	if err != nil {
		return nil, err
	}

	return v, nil
}

func (s HolderServiceServer) Patch(ctx context.Context, req *go_app.HolderPatchRequest) (*go_app.Holder, error) {
	if _, err := s.pick(ctx, req.GetRef()); err != nil {
		return nil, err
	}

	return s.HolderServiceServer.Patch(ctx, req)
}

// Apply is Patch by another road: what it may change is stated in a document
// rather than in the request, but it is about the same Holder, named the same
// way, so the same reference has to be the caller's.
func (s HolderServiceServer) Apply(ctx context.Context, req *go_app.HolderApplyRequest) (*go_app.Holder, error) {
	if _, err := s.pick(ctx, req.GetRef()); err != nil {
		return nil, err
	}

	return s.HolderServiceServer.Apply(ctx, req)
}

func (s HolderServiceServer) Erase(ctx context.Context, req *go_app.HolderRef) (*emptypb.Empty, error) {
	if _, err := s.pick(ctx, req); err != nil {
		return nil, err
	}

	return s.HolderServiceServer.Erase(ctx, req)
}

func (s HolderServiceServer) List(ctx context.Context, req *go_app.HolderListRequest) (*go_app.HolderListResponse, error) {
	res, err := s.HolderServiceServer.List(ctx, req)
	if err != nil {
		return nil, err
	}

	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if unbounded(f) {
		return res, nil
	}

	// Answered with, rather than asked for: the request has no way to say
	// "mine" yet. A list that is long enough to be cut off is a list that has
	// to say it in the query instead; see `core.ListLimit`.
	vs := make([]*go_app.Holder, 0, len(res.GetItems()))
	for _, v := range res.GetItems() {
		if within(f, v.GetTenant()) {
			vs = append(vs, v)
		}
	}

	return go_app.HolderListResponse_builder{Items: vs}.Build(), nil
}

// pick reads what a reference is about and refuses it if it is not the
// caller's to see.
func (s HolderServiceServer) pick(ctx context.Context, ref *go_app.HolderRef) (*go_app.Holder, error) {
	return s.get(ctx, go_app.HolderGetRequest_builder{Ref: ref}.Build())
}

// get answers the request and makes sure what comes back is the caller's. The
// Tenant is asked for whether the caller wanted it or not, since there is no
// other way to tell, and taken back out if they did not.
func (s HolderServiceServer) get(ctx context.Context, req *go_app.HolderGetRequest) (*go_app.Holder, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}

	asked := req.GetSelect().HasTenant()
	if !unbounded(f) && !asked {
		req.WithSelect(func(sel *go_app.HolderSelect) {
			sel.SetTenant(go_app.TenantSelect_builder{}.Build())
		})
	}

	v, err := s.HolderServiceServer.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	if unbounded(f) {
		return v, nil
	}

	if !within(f, v.GetTenant()) {
		return nil, errNotFound("Holder")
	}
	if !asked {
		v.ClearTenant()
	}

	return v, nil
}
