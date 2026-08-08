// Package frame carries who a request is from.
//
// A frame is put into the context once, by whatever read the credential, and
// read from it by whatever has to decide what the caller may do. **Every
// request has one**, including one nobody vouched for: that caller is
// [Anonymous], which is a caller like any other rather than the absence of one.
//
// That is worth saying twice, because the alternative is where this sort of
// thing goes wrong. A request with no frame is a question nothing has an answer
// to -- and every answer that suggests itself is bad. Refuse it, and the
// deployment's own calls have to go around the layer that refuses. Serve it as
// nobody-in-particular, and the code that decides what nobody-in-particular may
// do is somewhere else, unwritten, defaulting to whatever a zero value means.
// Making the anonymous caller a caller means the question is asked and answered
// in one place, for everybody.
package frame

import (
	"context"

	"github.com/lesomnus/z"
)

var use = z.NewUse[*Frame]()

// Frame is what is known about a request other than what it asks for.
type Frame struct {
	// Actor is who the request is from, as whoever vouched for them says.
	Actor Actor

	// Grant is what the credential they came with allows, which is at most what
	// the Actor allows. See [Grant].
	Grant Grant
}

// Actor is who a request is from.
//
// It is **not a row of this app**. This app has no users -- it has Coffees --
// and who somebody is comes from whoever vouched for them: a token's subject, a
// certificate's name, a header in development. What that string means is the
// issuer's business and nothing here compares it with anything but itself.
//
// A deployment with more than one issuer has to make the subject say which,
// since two issuers can call two different people the same thing.
type Actor struct {
	// Subject is who they are, spelled the way whoever vouched for them spells
	// it.
	//
	// Empty is [Anonymous] and is the only meaning empty has. There is no
	// "unknown but present": a credential that was read and named nobody is a
	// credential that was refused.
	Subject string

	// Claims is whatever else came with them, and is read by whatever put it
	// there. Nothing in this app looks inside it.
	//
	// It is here so that a deployment whose policy turns on something the
	// issuer said -- a group, a plan, a scope -- has somewhere to carry it
	// without every layer growing a field.
	Claims map[string]string
}

// Anonymous is a caller nobody vouched for, which is a caller like any other.
var Anonymous = Actor{}

// IsAnonymous reports whether nobody vouched for this caller.
func (a Actor) IsAnonymous() bool { return a.Subject == "" }

// New answers with the frame of a request from `actor`, carrying a credential
// that allows `grant`.
//
// The grant is an argument rather than a field set afterwards because there is
// no safe thing for it to default to: [Grant]'s zero value allows nothing, so a
// caller who forgot would build a frame that can do nothing -- which is the
// right way round, and still better not to leave to whether somebody
// remembered.
func New(actor Actor, grant Grant) *Frame {
	return &Frame{Actor: actor, Grant: grant}
}

// Nobody is the frame of a request nobody vouched for. The grant is whole,
// since there is no credential to have narrowed anything: what an anonymous
// caller may do is a rule about them and not an attenuation of a token.
func Nobody() *Frame {
	return New(Anonymous, Whole())
}

func Into(ctx context.Context, v *Frame) context.Context {
	return use.Into(ctx, v)
}

func From(ctx context.Context) (*Frame, bool) {
	return use.From(ctx)
}

// Must is [From] for the places that are only reached behind the interceptor
// that puts one there, so a missing frame is a wiring mistake rather than a
// request to refuse.
func Must(ctx context.Context) *Frame {
	return use.Must(ctx)
}

// Of is who `ctx` is from, and [Anonymous] for a context that has no frame at
// all -- a job, a test, the app calling itself.
//
// It is the reading to reach for outside the served path. Inside it there is
// always a frame, and a missing one is a mistake worth [Must]'s panic.
func Of(ctx context.Context) Actor {
	f, ok := From(ctx)
	if !ok {
		return Anonymous
	}

	return f.Actor
}
