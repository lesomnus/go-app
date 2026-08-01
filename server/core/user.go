package core

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/internal/ent/predicate"
	"github.com/lesomnus/go-app/internal/ent/user"
	"github.com/lesomnus/go-app/server/bare"
)

type UserServiceServer struct {
	Server
	go_app.UserServiceServer
}

func NewUserServiceServer(s Server) UserServiceServer {
	return UserServiceServer{s, s.Next().User()}
}

func (s Server) User() go_app.UserServiceServer {
	return NewUserServiceServer(s)
}

func (s UserServiceServer) Add(ctx context.Context, req *go_app.UserAddRequest) (*go_app.User, error) {
	if req.GetTenant() == nil {
		// A User only exists within a Tenant.
		return nil, status.Error(codes.InvalidArgument, "tenant: must be given")
	}

	alias, err := ParseAlias(req.GetAlias())
	if err != nil {
		return nil, err
	}

	req.SetAlias(alias)
	if req.GetName() == "" {
		req.SetName(alias)
	}

	return s.UserServiceServer.Add(ctx, req)
}

// ListLimit is the most Users [UserServiceServer.List] answers with.
const ListLimit = 100

// List answers with the Users that match any of the given filters, or with
// every User if there is none.
//
// It is not a CRUD operation, so nothing generates it; this is the plainest
// thing that works and it is meant to be rewritten. A real one would page,
// order and filter by what the app is actually asked about.
func (s UserServiceServer) List(ctx context.Context, req *go_app.UserListRequest) (*go_app.UserListResponse, error) {
	db, err := s.Db()
	if err != nil {
		return nil, err
	}

	q := db.User.Query()
	if fs := req.GetFilters(); len(fs) > 0 {
		ps := make([]predicate.User, 0, len(fs))
		for i, f := range fs {
			p, err := bare.UserPick(f.GetRef())
			if err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "filters[%d]: %s", i, err)
			}

			ps = append(ps, p)
		}

		q.Where(user.Or(ps...))
	}

	// Oldest first, so that what is answered does not depend on the order the
	// database happens to hold the rows in.
	us, err := q.Order(user.ByDateCreated()).Limit(ListLimit).All(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]*go_app.User, len(us))
	for i, u := range us {
		items[i] = u.Proto()
	}

	return go_app.UserListResponse_builder{Items: items}.Build(), nil
}

func (s UserServiceServer) Patch(ctx context.Context, req *go_app.UserPatchRequest) (*go_app.User, error) {
	if req.HasAlias() {
		alias, err := ParseAlias(req.GetAlias())
		if err != nil {
			return nil, err
		}

		req.SetAlias(alias)
	}

	return s.UserServiceServer.Patch(ctx, req)
}
