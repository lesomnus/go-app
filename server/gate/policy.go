package gate

import (
	"context"

	"google.golang.org/grpc"

	go_app "github.com/lesomnus/go-app/go_app"
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
// Policy behaves exactly as this app always has, with the Tenant wall and the
// two or three rules in this package. The interface is the seam, not a
// requirement.
//
// # Two questions, because there are two
//
// [Policy.May] answers a point: may this happen at all. It is asked once per
// request, before the handler, so it must not need the row -- a request may name
// one by an alias, and resolving that is a query in front of the query. Ask it
// about the *kind* of thing, which is what a method name already says, and
// leave anything about a particular row to [Policy.Where] and to the predicate
// it becomes.
//
// [Policy.Where] answers a set: which Tenants this caller may act in. A set,
// because a list is not a boolean. Asked for a boolean per row instead, a list
// has to fetch rows it may not answer with and drop them afterwards -- which
// cannot be paged, and which any Tenant can use to push another's rows out of
// an answer by making enough of its own.
//
// # What it must not be
//
// Asked more than once per request. The hooks [Wall] installs run per *query*,
// and a request makes several. Whatever implements this is consulted once, in
// front, and the answer is carried on the frame -- the same way `server/auth`
// carries who the caller is rather than working it out again at each layer.
//
// A reason to wait on the network. The answer is a function of the actor and
// the method, with nothing of the request in it, so it can be held as a
// snapshot and evaluated in process -- which is what Kubernetes does with RBAC,
// and what makes an authorization service that is briefly unreachable not an
// outage. An implementation that does call out owes the caller the same
// distinction `server/auth` makes: `Unavailable` when it could not find out,
// which is not the caller's fault, rather than a refusal that reads as theirs.
type Policy interface {
	// May reports whether the caller may make this call at all. A refusal is
	// answered to the caller as it is, so it should be a status.
	May(ctx context.Context, d Decision) error

	// Where answers with the Tenants the actor may take `action` in.
	Where(ctx context.Context, actor *go_app.Holder, action string) (Tenants, error)
}

// Decision is one call, as a policy sees it: who, and what they asked for.
//
// There is no field for the row. That is not an omission -- see [Policy].
type Decision struct {
	Actor *go_app.Holder

	// Action is the RPC gRPC dispatched, by the name it knows it by, such as
	// "/go_app.HolderService/Patch". It is the same string the audit trail
	// stores, and for the same reason: it is what the caller asked for rather
	// than the leg of it that did the work.
	Action string
}

// policy is what this deployment was built with, and nothing until it is told.
//
// A package variable rather than a field of [Server], because what asks it is
// [Wall], which is installed on the innermost server and is not a layer of the
// stack at all. A field would have to be threaded through the one thing that is
// deliberately not part of the stack.
var policy Policy

// SetPolicy tells this package what to consult. It is for a deployment to call
// once, before anything is served.
func SetPolicy(v Policy) { policy = v }

// method is the RPC gRPC dispatched, or "" for a call that did not come in over
// the wire -- one server calling another in process, which is how a
// hand-written RPC reaches the ones it is built from.
func method(ctx context.Context) string {
	v, _ := grpc.Method(ctx)
	return v
}
