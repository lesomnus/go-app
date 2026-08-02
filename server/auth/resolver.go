package auth

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
)

// ServerResolver looks the claim up on the given server, which is normally the
// innermost one: the rules about who may see whom are not the rules that
// decide who is asking.
func ServerResolver(s go_app.Server) Resolver {
	return ResolverFunc(func(ctx context.Context, id Identity) (*go_app.Holder, error) {
		if id.Ref == nil {
			return nil, fmt.Errorf("names nobody: %w", ErrNoCredential)
		}

		req := go_app.HolderGetRequest_builder{Ref: id.Ref}.Build()
		// The Tenant travels with the actor, since almost every rule about a
		// request is about the Tenant it is from.
		req.WithSelect(func(s *go_app.HolderSelect) {
			s.SetAll(true)
			s.SetTenant(go_app.TenantSelect_builder{All: ptrTrue()}.Build())
		})

		v, err := s.Holder().Get(ctx, req)
		if err != nil {
			if status.Code(err) == codes.NotFound {
				return nil, fmt.Errorf("names nobody who is here: %w", ErrNoCredential)
			}

			return nil, err
		}

		return v, nil
	})
}

func ptrTrue() *bool {
	v := true
	return &v
}
