package gate

import (
	"context"

	"github.com/lesomnus/go-app/server/frame"
)

// Policy is what a deployment consults about a caller, and is deliberately not
// implemented here.
//
// This app is a resource server and not an authorization server. It reads
// credentials and enforces what it is told; it does not mint tokens and it does
// not define roles. Which roles exist, who is bound to them, and how that is
// edited is a deployment's own decision, changes for its own reasons, and is
// what a policy engine is for -- this package holds the question, an
// implementation is injected, and nothing here takes a dependency on a running
// service to say so.
//
// It is unset by default, and that is not a placeholder: a deployment with no
// Policy behaves exactly as this app does on its own -- whoever said who they
// are may do anything, and an anonymous caller may do what [Anonymous] named.
// The interface is the seam, not a requirement.
//
// # It asks about the call and never about the row
//
// [Policy.May] is asked once per request, **before the handler**, so it must
// not need the row: a request may name one by an alias, and resolving that is a
// query in front of the query. Ask it about the *kind* of thing, which is what
// a method name already says.
//
// A rule about a particular row is a different shape of thing and does not
// belong in an interceptor at all. It belongs in the query, as a predicate --
// `bare.Scope` is the hook the generated servers put one into every read they
// build, and it is not used here because this app has nothing to narrow a read
// by. An app that does -- rows with an owner, a tenant, a visibility -- installs
// one there rather than asking this per row. See the `kind/server` branch, whose
// whole subject is that.
//
// # What it must not be
//
// Asked more than once per request. Whatever implements this is consulted once,
// in front, by [Interceptor] -- the same way `server/auth` carries who the
// caller is rather than working it out again at each layer.
//
// A reason to wait on the network. The answer is a function of the actor and
// the method, with nothing of the request in it, so it can be held as a
// snapshot and evaluated in process -- which is what Kubernetes does with RBAC,
// and what makes an authorization service that is briefly unreachable not an
// outage. An implementation that does call out owes the caller the same
// distinction `server/auth` makes: `Unavailable` when it could not find out,
// which is not the caller's fault, rather than a refusal that reads as theirs.
type Policy interface {
	// May reports whether this call may be made at all. A refusal is answered
	// to the caller as it is, so it should be a status.
	May(ctx context.Context, c Call) error
}

// Call is one call, as a policy sees it: who, and what they asked for.
//
// There is no field for the row. That is not an omission -- see [Policy].
type Call struct {
	// Actor is who is calling, and is [frame.Anonymous] for a caller nobody
	// vouched for. A policy that has nothing to say about those should say
	// nothing about them rather than assume they were refused already: what
	// [Anonymous] let through, it let through.
	Actor frame.Actor

	// Action is the RPC gRPC dispatched, by the name it knows it by, such as
	// "/go_app.CoffeeService/Patch". It is what the caller asked for rather
	// than the write it became.
	Action string
}
