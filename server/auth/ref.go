package auth

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"

	go_app "github.com/lesomnus/go-app/go_app"
)

// ParseRef reads a Holder written as one of the two ways anything else names
// one: an identifier, or an alias within a Tenant.
//
//	0f9b2c...            the identifier, as a UUID or as its bytes in hex
//	acme/admin           the alias, within the alias of its Tenant
//
// Every handler that carries a name spells it this way -- what a caller writes
// in a header and what a certificate says are the same name, so they are read
// by the same code and go wrong in the same way.
func ParseRef(v string) (*go_app.HolderRef, error) {
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

// RefOf spells a Holder the way [ParseRef] reads it.
func RefOf(v *go_app.Holder) string {
	if id := v.GetId(); len(id) > 0 {
		return hex.EncodeToString(id)
	}

	return v.GetTenant().GetAlias() + "/" + v.GetAlias()
}
