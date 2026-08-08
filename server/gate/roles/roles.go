// Package roles answers [gate.Policy] out of a table of roles and bindings,
// which is what an integration with a policy engine looks like from this side.
//
// It is a **sample**, and the line it draws is the one `server/gate` draws:
// this app is a resource server. It does not define roles, it does not bind
// anybody to one, and it serves no RPC for editing either -- what is here reads
// a table it was handed and answers the one question a request asks. Where the
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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lesomnus/go-app/server/gate"
)

// Any is the action that stands for every RPC there is. Written out rather than
// left implicit, since a list that means "everything" by being empty is the one
// that means it by accident.
const Any = "*"

// Role is a set of actions, named where it is bound.
type Role struct {
	// Actions names the RPCs this role allows, by the name gRPC knows them by:
	// "/go_app.CoffeeService/Get". A name whose method part is [Any] allows
	// every method of that service -- "/go_app.CoffeeService/*" -- and [Any] on
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

// Binding gives a subject a Role.
type Binding struct {
	// Subject is who holds the role, spelled the way whoever vouched for them
	// spells it; see [frame.Actor].
	//
	// There is no subject an anonymous caller has, so nothing here can bind one
	// -- what an anonymous caller may do is `gate.Anonymous`, in front of this.
	Subject string

	// Role names one of [Table.Roles]. A name nothing answers to is refused
	// when the table is stored, rather than being a binding that quietly
	// allows nothing.
	Role string
}

// Table is what a [Policy] answers from.
type Table struct {
	// Roles is every role there is, by name.
	Roles map[string]Role

	// Bindings is who holds which of them. A subject may appear more than once,
	// and what they may do is the union: one binding that allows an action is
	// enough.
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

// held is a table with the bindings already gathered by subject, so that a
// request is a map lookup rather than a walk of every binding there is.
type held struct {
	roles map[string]Role
	by    map[string][]Binding
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
		by:    make(map[string][]Binding),
	}
	for k, r := range t.Roles {
		v.roles[k] = r
	}
	for i, b := range t.Bindings {
		if _, ok := v.roles[b.Role]; !ok {
			return fmt.Errorf("binding %d names a role nothing defines: %q", i, b.Role)
		}

		v.by[b.Subject] = append(v.by[b.Subject], b)
	}

	p.at.Store(v)
	return nil
}

// May answers whether any role this caller holds allows the RPC they asked for.
//
// Which row is not asked here, and that is the division [gate.Policy] draws:
// this is the point question, asked before the handler, so it cannot know which
// row the request is about. A rule about a particular row is a predicate and
// belongs in the query; see [gate.Policy].
func (p *Policy) May(_ context.Context, c gate.Call) error {
	for _, b := range p.bindings(c) {
		if p.at.Load().roles[b.Role].Allows(c.Action) {
			return nil
		}
	}

	// PermissionDenied and not NotFound: this is about the caller and the RPC,
	// neither of which is a secret from the caller.
	return status.Errorf(codes.PermissionDenied, "no role of yours allows %s", c.Action)
}

// bindings is what this caller holds, and nothing for a caller the table has
// never heard of -- which every anonymous one is, since nobody can be bound to
// a subject that is not one.
func (p *Policy) bindings(c gate.Call) []Binding {
	return p.at.Load().by[c.Actor.Subject]
}
