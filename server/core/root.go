package core

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/lesomnus/z"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
)

const (
	// RootAlias is the first Tenant, which every deployment has because a
	// deployment with none has nobody who can authenticate at all.
	//
	// It is not privileged. Nothing anywhere compares a caller against it and
	// answers "everything": a privilege granted by being a particular row is
	// one that cannot be revoked or narrowed, and does not appear anywhere it
	// is used. What the deployment has to do for itself, it does through a
	// server the wall was never installed on; see `gate`.
	RootAlias = "root"

	// AdminAlias is the Holder that comes with a Tenant. Adding a Tenant with
	// nobody in it leaves nobody able to do anything with it.
	AdminAlias = "admin"
)

// NobodyId is the identifier that names nobody, and so is the identifier
// nothing may hold.
//
// A request may say what identifier it wants, which is what makes this worth a
// rule: the audit trail writes this one for a write nobody asked for, so a
// Holder that held it could act and be recorded as the deployment writing to
// itself. Non-repudiation is most of what a trail is for, and one field of one
// request should not be able to end it.
var NobodyId = uuid.Nil

// CheckId refuses an identifier a row may not be stored under. An empty one is
// no identifier at all, which is a request asking for whatever the database
// settles on, and that is allowed.
func CheckId(v []byte) error {
	if len(v) == 0 {
		return nil
	}
	if bytes.Equal(v, NobodyId[:]) {
		return status.Error(codes.InvalidArgument, "id: that one names nobody, so nothing may hold it")
	}

	return nil
}

// Root is what the first Tenant looks like. Its identifier is the database's to
// choose, like every other one -- there used to be a constant here, and it was
// a constant so that a caller could be compared against it.
func Root() *go_app.TenantAddRequest {
	return go_app.TenantAddRequest_builder{
		Alias: RootAlias,
		Name:  "Root",
		Desc:  "The first tenant of this deployment.",
	}.Build()
}

// EnsureRoot adds the first Tenant, and the Holder that administers it, unless
// they are already there. It is idempotent, so it can be run at every start,
// and it goes through this server rather than around it so that the first
// Tenant is made the same way as any other.
//
// Idempotent by the alias, which is unique, rather than by an identifier this
// package chose. It must be handed a server the wall is not on: putting up a
// Tenant is not something asked for from inside one, and at this point there is
// nobody to be inside one anyway.
func EnsureRoot(ctx context.Context, s go_app.Server) (*go_app.Tenant, error) {
	v, err := s.Tenant().Get(ctx, go_app.TenantGetByAlias(RootAlias))
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
			return s.Tenant().Get(ctx, go_app.TenantGetByAlias(RootAlias))
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
