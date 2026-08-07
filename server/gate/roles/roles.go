// Package roles answers [gate.Policy] out of a table of roles and bindings,
// which is what an integration with a policy engine looks like from this side.
//
// It is a **sample**, and the line it draws is the one `server/gate` draws:
// this app is a resource server. It does not define roles, it does not bind
// anybody to one, and it serves no RPC for editing either -- what is here reads
// a table it was handed and answers the two questions a request asks. Where the
// table comes from is a deployment's own business: a file it watches, a
// Kubernetes controller, a Zanzibar-shaped service it lists from. An app made
// from this template either fills [Table] from whichever of those it has, or
// deletes this package and implements [gate.Policy] against its engine directly.
//
// # Why the table is a snapshot
//
// [Policy.Store] swaps the whole table at once, and a request never waits on
// anything. That is not an optimization, it is the property that makes the seam
// usable: a request answered from the last table that arrived is a request
// answered while the engine that produces them is down. An implementation that
// asks over the network per call has turned every authorization outage into a
// total one -- and owes callers `Unavailable` rather than a refusal, since
// "we could not find out" is not "you may not".
//
// This is the shape Kubernetes RBAC has, and it works because the question does
// not depend on the request body. See [gate.Policy] for why [gate.Policy.May]
// must not need the row it is about.
package roles

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/server/frame"
	"github.com/lesomnus/go-app/server/gate"
)

// Any is the action that stands for every RPC there is, and the Tenant nothing
// narrows. Written out rather than left implicit, since a list that means
// "everything" by being empty is the one that means it by accident.
const Any = "*"

// Role is a set of actions, named where it is bound.
type Role struct {
	// Actions names the RPCs this role allows, by the name gRPC knows them by:
	// "/go_app.HolderService/Get". A name whose method part is [Any] allows
	// every method of that service -- "/go_app.HolderService/*" -- and [Any] on
	// its own allows every RPC.
	//
	// It is the method and never the entity, because the method is what a
	// caller asked for and the entity is only what it happened to touch. See
	// [gate.Call].
	Actions []string
}

// Allows reports whether this role allows the given RPC.
func (r Role) Allows(action string) bool {
	for _, v := range r.Actions {
		switch {
		case v == Any || v == action:
			return true

		case strings.HasSuffix(v, "/"+Any):
			if strings.HasPrefix(action, strings.TrimSuffix(v, Any)) {
				return true
			}
		}
	}

	return false
}

// Binding gives a Holder a Role, somewhere.
type Binding struct {
	// Holder is who holds the role.
	Holder uuid.UUID

	// Role names one of [Table.Roles]. A name nothing answers to is refused
	// when the table is stored, rather than being a binding that quietly
	// allows nothing.
	Role string

	// Tenants is where the role holds. Naming none is the Tenant the Holder is
	// held by, which is what this app answers with no policy at all -- so a
	// table of bindings that name no Tenant behaves exactly like the wall.
	Tenants []uuid.UUID

	// Everywhere makes the role hold in every Tenant there is, and is the one
	// thing in this repository that produces [frame.Everything].
	//
	// Read it twice before writing it. It is the superuser this app
	// deliberately does not have: a caller who satisfies a condition and is
	// thereby outside the wall, which is exactly the shape that was taken out
	// of `server/gate`. The difference -- and it is a real one -- is that this
	// is a deployment saying so, in a table it can edit and revoke, rather than
	// this app assuming it about whoever holds a particular row. What the
	// deployment does for *itself* still wants the ungated stack instead; see
	// docs/AUTH.md.
	Everywhere bool
}

// Table is what a [Policy] answers from.
type Table struct {
	// Roles is every role there is, by name.
	Roles map[string]Role

	// Bindings is who holds which of them, where. A Holder may appear more than
	// once, and what they may do is the union: one binding that allows an
	// action is enough, and the Tenants of every binding that allows it are
	// added together.
	Bindings []Binding
}

var _ gate.Policy = (*Policy)(nil)

// Policy answers out of the table it was last given.
//
// The zero value is not usable; make one with [New]. It is safe for concurrent
// use, and [Policy.Store] may be called while requests are being served.
type Policy struct {
	at atomic.Pointer[held]
}

// held is a table with the bindings already gathered by Holder, so that a
// request is a map lookup rather than a walk of every binding there is.
type held struct {
	roles map[string]Role
	by    map[uuid.UUID][]Binding
}

// New answers with a policy that reads `t`.
func New(t Table) (*Policy, error) {
	p := &Policy{}
	if err := p.Store(t); err != nil {
		return nil, err
	}

	return p, nil
}

// Store makes `t` what this policy answers from, from the next call onwards. A
// call already being served is answered from the table it started with, which
// is the only answer that is any answer at all.
//
// A table that names a role nothing defines is refused whole, and the policy
// goes on reading the one before it. The alternative is a binding that allows
// nothing, which reads as a caller who lost their access for no reason anybody
// can find.
func (p *Policy) Store(t Table) error {
	v := &held{
		roles: make(map[string]Role, len(t.Roles)),
		by:    make(map[uuid.UUID][]Binding),
	}
	for k, r := range t.Roles {
		v.roles[k] = r
	}
	for i, b := range t.Bindings {
		if _, ok := v.roles[b.Role]; !ok {
			return fmt.Errorf("binding %d names a role nothing defines: %q", i, b.Role)
		}

		v.by[b.Holder] = append(v.by[b.Holder], b)
	}

	p.at.Store(v)
	return nil
}

// May answers whether any role this caller holds allows the RPC they asked for.
//
// Where they hold it is not asked here, and that is the division [gate.Policy]
// draws: this is the point question, and it is asked before the handler, so it
// cannot know which row -- or which Tenant's row -- the request is about. A
// caller who may read Holders somewhere gets past this and is then narrowed to
// the somewhere by [Policy.Where].
func (p *Policy) May(_ context.Context, c gate.Call) error {
	for _, b := range p.bindings(c) {
		if p.at.Load().roles[b.Role].Allows(c.Action) {
			return nil
		}
	}

	// PermissionDenied and not NotFound: this is about the caller and the RPC,
	// neither of which is a secret from the caller. What is a secret -- which
	// rows exist -- is decided by the predicate, and answers NotFound of its
	// own accord.
	return status.Errorf(codes.PermissionDenied, "no role of yours allows %s", c.Action)
}

// Where answers with every Tenant a role that allows this RPC holds in.
//
// It replaces the wall rather than adding to it: with a policy installed,
// `gate` asks this and nothing else, so a table that says nothing about a
// caller is a caller who sees no rows. That is the right way round -- the other
// reading is a policy that opens up as it runs out of things to say -- and it
// is also why a [Binding] that names no Tenant means the Holder's own: the
// wall's answer, said in this table's words.
func (p *Policy) Where(_ context.Context, c gate.Call) (frame.Tenants, error) {
	held := p.at.Load()

	var (
		own  *uuid.UUID
		vs   []uuid.UUID
		seen = map[uuid.UUID]bool{}
	)
	for _, b := range p.bindings(c) {
		if !held.roles[b.Role].Allows(c.Action) {
			continue
		}
		if b.Everywhere {
			return frame.Everything, nil
		}
		if len(b.Tenants) == 0 {
			if own == nil {
				k, err := uuid.FromBytes(c.Actor.GetTenant().GetId())
				if err != nil {
					// It came out of the database with the actor, so this is
					// the app disagreeing with itself rather than the table
					// being wrong.
					return frame.Nothing, status.Errorf(codes.Internal, "the caller's tenant has no identifier: %s", err)
				}

				own = &k
			}

			b.Tenants = []uuid.UUID{*own}
		}

		for _, k := range b.Tenants {
			if seen[k] {
				continue
			}

			seen[k] = true
			vs = append(vs, k)
		}
	}

	// Nothing rather than everything for a caller nothing was said about; see
	// [frame.Tenants.Ids] for why an empty set is a query that matches no rows
	// rather than one that matches all of them.
	return frame.Only(vs...), nil
}

// bindings is what this caller holds, and nothing for a caller the table has
// never heard of.
func (p *Policy) bindings(c gate.Call) []Binding {
	k, err := uuid.FromBytes(c.Actor.GetId())
	if err != nil {
		return nil
	}

	return p.at.Load().by[k]
}
