// Package auth says who a caller is.
//
// It is two steps, kept apart because they change for different reasons. A
// [Handler] reads whatever the transport carries - a header, a certificate, a
// token - and says who the caller claims to be. A [Resolver] looks that claim
// up and answers with the Holder it belongs to, or with nothing. What the
// second one hands back is what the request is served as, and it comes from
// the database rather than from the caller.
//
// Three handlers are written here, and they differ in one thing: where the
// name comes from.
//
//	Plain   the caller writes it in a header, and is believed
//	MTLS    the certificate the connection was made with carries it
//	Bearer  the token carries nothing, and is exchanged for a name
//
// The first two read a name that is already there, so they are pure functions
// of the request. [Bearer] has to ask something, which is what makes it the
// interesting one: it can fail in a way the caller did not cause, and
// [ErrUnavailable] is how it says so.
//
// [Plain] believes whatever it is told, and is for development and for tests.
// It must not be reachable by anyone who is not already trusted to say the
// truth; see the note in README.md on where that boundary is.
//
// # What a credential says, and what it does not
//
// Every handler here answers the same question -- who is this -- and none of
// them answers a second one: what may they do. A credential either names a
// Holder or it does not; naming one grants everything that Holder can do. A
// token that meant "john, but only for reading" would need somewhere to put
// the "only", and there is nowhere: the frame of a request carries the
// actor and nothing beside it, and every rule in `server/gate` reads the actor alone. If
// that ever has to change, it changes there first and here second.
package auth

import (
	"context"
	"errors"
	"io"

	go_app "github.com/lesomnus/go-app/go_app"
)

// ErrNoCredential is what a handler reports when the request carries nothing
// it knows how to read. It is not a refusal: the next handler is asked, and if
// none of them find anything the call is Unauthenticated.
//
// A credential that is there but wrong is a different thing entirely, and must
// be reported as itself so that it is not read as "nobody asked".
var ErrNoCredential = io.EOF

// ErrUnavailable is what a handler reports when it could not find out whether
// the credential is good -- the store it would have asked is not answering.
//
// It is a third answer, and it exists because a handler that has to look
// something up can fail in a way the caller did not cause. Told
// Unauthenticated, a caller throws away a token that is perfectly good and
// goes to get another one, from the thing that is already down. Told
// Unavailable, it waits.
//
// It does not fall through to the next handler either. Somebody presented a
// credential; serving them as whatever the next handler makes of them would be
// answering a question nobody asked, possibly as somebody else.
var ErrUnavailable = errors.New("auth: cannot say whether the credential is good")

// Identity is who a caller claims to be, before anything has been looked up.
// A Holder is named either by its id or by its alias within a Tenant, which is
// the same way anything else names one.
type Identity struct {
	// Method is what the claim was read from, for the log and for nothing
	// else. A rule that turns on the way somebody authenticated is a rule that
	// will be wrong one day.
	Method string

	Ref *go_app.HolderRef
}

// Handler reads a claim out of the context a call is served with.
type Handler interface {
	// Handle returns who the caller claims to be. It wraps
	// [ErrNoCredential] if the request carries nothing it can read.
	Handle(ctx context.Context) (Identity, error)
}

type HandlerFunc func(ctx context.Context) (Identity, error)

func (f HandlerFunc) Handle(ctx context.Context) (Identity, error) {
	return f(ctx)
}

// Seq asks each handler in turn and takes the first claim any of them finds. A
// handler that finds nothing is passed over; one that finds something wrong,
// or that cannot tell, stops the search.
//
// That is what makes a fallback safe. `Seq(Bearer(store), MTLS())` means "the
// token if there is one, otherwise the certificate" -- not "the token, and if
// anything at all goes wrong, the certificate". A token that is expired, or
// one this server cannot check right now, must not quietly become a different
// caller with different authority.
func Seq(hs ...Handler) Handler {
	return HandlerFunc(func(ctx context.Context) (Identity, error) {
		for _, h := range hs {
			v, err := h.Handle(ctx)
			if err == nil {
				return v, nil
			}
			if !errors.Is(err, ErrNoCredential) {
				return Identity{}, err
			}
		}

		return Identity{}, ErrNoCredential
	})
}

// Resolver turns a claim into the Holder it is about.
type Resolver interface {
	// Resolve returns the Holder the identity names, or an error wrapping
	// [ErrNoCredential] if there is no such Holder. A caller that names
	// somebody who does not exist is no better off than one that named
	// nobody.
	Resolve(ctx context.Context, id Identity) (*go_app.Holder, error)
}

type ResolverFunc func(ctx context.Context, id Identity) (*go_app.Holder, error)

func (f ResolverFunc) Resolve(ctx context.Context, id Identity) (*go_app.Holder, error) {
	return f(ctx, id)
}

// Provider puts a credential into the context of an outgoing call, which is
// the other half of a [Handler]. It is what a client of this app, or this app
// calling another one, uses.
type Provider interface {
	Provide(ctx context.Context) context.Context
}

type ProviderFunc func(ctx context.Context) context.Context

func (f ProviderFunc) Provide(ctx context.Context) context.Context {
	return f(ctx)
}
