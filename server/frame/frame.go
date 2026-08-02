// Package frame carries who a request is from.
//
// A frame is put into the context once, by whatever authenticated the caller,
// and read from it by whatever has to decide what the caller may do. A context
// without one is a request nobody has vouched for; see `server/auth`.
package frame

import (
	"context"

	"github.com/lesomnus/z"

	go_app "github.com/lesomnus/go-app/go_app"
)

var use = z.NewUse[*Frame]()

// Frame is what is known about a request other than what it asks for.
type Frame struct {
	// Actor is who the request is from. It is a Holder that was read from the
	// database, not one the caller described, so what it says about itself can
	// be relied on.
	Actor *go_app.Holder
}

func New(actor *go_app.Holder) *Frame {
	return &Frame{Actor: actor}
}

func Into(ctx context.Context, v *Frame) context.Context {
	return use.Into(ctx, v)
}

func From(ctx context.Context) (*Frame, bool) {
	return use.From(ctx)
}

// Must is From for the places that are only reached behind authentication, so
// a missing frame is a wiring mistake rather than a request to refuse.
func Must(ctx context.Context) *Frame {
	return use.Must(ctx)
}

// Tenant is the Tenant the actor belongs to.
func (f *Frame) Tenant() *go_app.Tenant {
	return f.Actor.GetTenant()
}

// TenantRef is the Tenant the actor belongs to, as a reference.
func (f *Frame) TenantRef() *go_app.TenantRef {
	return f.Actor.GetTenant().Ref()
}
