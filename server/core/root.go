package core

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
)

const (
	// RootAlias is the Tenant every deployment has, whoever holds it. It is
	// the one nobody creates and nobody is allowed to erase, and it is where
	// whoever administers the deployment itself lives.
	RootAlias = "root"

	// AdminAlias is the Holder that comes with a Tenant. Adding a Tenant with
	// nobody in it leaves nobody able to do anything with it.
	AdminAlias = "admin"
)

// RootId is the identifier of the root Tenant. It is a constant so that
// anything that has to name the root - a configuration, a fixture, a query run
// by hand - can name it without asking the database first. It spells "root" in
// the bytes it is written with.
var RootId = uuid.MustParse("726f6f74-0000-0000-0000-000000000000")

// Root is what the root Tenant looks like.
func Root() *go_app.TenantAddRequest {
	return go_app.TenantAddRequest_builder{
		Id:    RootId[:],
		Alias: RootAlias,
		Name:  "Root",
		Desc:  "The tenant that administers this deployment.",
	}.Build()
}

// EnsureRoot adds the root Tenant, and the Holder that administers it, unless
// they are already there. It is idempotent, so it can be run at every start,
// and it goes through this server rather than around it so that the root
// Tenant is made the same way as any other.
func EnsureRoot(ctx context.Context, s go_app.Server) (*go_app.Tenant, error) {
	v, err := s.Tenant().Get(ctx, go_app.TenantGetById(RootId[:]))
	switch {
	case err == nil:
		return v, nil
	case status.Code(err) != codes.NotFound:
		return nil, z.Err(err, "look for the root tenant")
	}

	v, err = s.Tenant().Add(ctx, Root())
	if err != nil {
		// Another one got there first, which is not a race anybody loses.
		if status.Code(err) == codes.AlreadyExists {
			return s.Tenant().Get(ctx, go_app.TenantGetById(RootId[:]))
		}

		return nil, z.Err(err, "add the root tenant")
	}

	return v, nil
}

// ErrNoAdmin is what a Tenant without its Holder is: something went wrong
// between the two writes that make one.
var ErrNoAdmin = errors.New("the tenant has no admin")

// Admin is the Holder that administers the given Tenant.
func Admin(ctx context.Context, s go_app.Server, tenant *go_app.TenantRef) (*go_app.Holder, error) {
	v, err := s.Holder().Get(ctx, go_app.HolderGetBySlug(AdminAlias, tenant))
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrNoAdmin
		}

		return nil, err
	}

	return v, nil
}
