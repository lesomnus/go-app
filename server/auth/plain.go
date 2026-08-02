package auth

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"

	go_app "github.com/lesomnus/go-app/go_app"
)

// MethodPlain is what [Plain] calls itself.
const MethodPlain = "plain"

// PlainScheme is the authorization scheme [Plain] reads and [PlainProvider]
// writes.
const PlainScheme = "Plain"

// Plain believes whatever the caller says it is:
//
//	authorization: Plain <holder-id>
//	authorization: Plain <tenant-alias>/<holder-alias>
//
// There is nothing to check and it checks nothing, which is the point: a test
// or a hand written call says who it is and gets on with it. It must not be
// reachable by anyone who is not already trusted to say the truth.
func Plain() Handler {
	return HandlerFunc(func(ctx context.Context) (Identity, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return Identity{}, ErrNoCredential
		}

		for _, v := range md.Get("authorization") {
			rest, ok := strings.CutPrefix(v, PlainScheme+" ")
			if !ok {
				continue
			}

			ref, err := parsePlain(strings.TrimSpace(rest))
			if err != nil {
				// Something was said, and it was not a name; that is not the
				// same as saying nothing.
				return Identity{}, fmt.Errorf("%s: %w", PlainScheme, err)
			}

			return Identity{Method: MethodPlain, Ref: ref}, nil
		}

		return Identity{}, ErrNoCredential
	})
}

// PlainProvider says who the caller is in the way [Plain] reads.
//
// It replaces whatever was said before rather than adding to it. Two answers
// to "who is calling" is not twice as much information; it is a question with
// no answer, and the one that would win is whichever came first.
func PlainProvider(v string) Provider {
	return ProviderFunc(func(ctx context.Context) context.Context {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.MD{}
		} else {
			md = md.Copy()
		}

		md.Set("authorization", PlainScheme+" "+v)
		return metadata.NewOutgoingContext(ctx, md)
	})
}

// PlainOf spells a Holder the way [PlainProvider] wants it.
func PlainOf(v *go_app.Holder) string {
	if id := v.GetId(); len(id) > 0 {
		return hex.EncodeToString(id)
	}

	return v.GetTenant().GetAlias() + "/" + v.GetAlias()
}

// parsePlain reads what is either an identifier or a tenant and an alias.
func parsePlain(v string) (*go_app.HolderRef, error) {
	if v == "" {
		return nil, fmt.Errorf("says nothing")
	}

	if tenant, alias, ok := strings.Cut(v, "/"); ok {
		if tenant == "" || alias == "" {
			return nil, fmt.Errorf("%q is neither an id nor a tenant and an alias", v)
		}

		return go_app.HolderBySlug(alias, go_app.TenantByAlias(tenant)), nil
	}

	// An identifier, written either as a UUID or as the bytes of one.
	if id, err := uuid.Parse(v); err == nil {
		return go_app.HolderById(id[:]), nil
	}
	if id, err := hex.DecodeString(v); err == nil && len(id) == len(uuid.UUID{}) {
		return go_app.HolderById(id), nil
	}

	return nil, fmt.Errorf("%q is neither an id nor a tenant and an alias", v)
}
