package gate

import (
	"context"

	"github.com/lesomnus/go-app/server/frame"
)

// keyTenant is written in front of the identifier so that a key is never empty
// -- an empty key is how [grpcx.Limit] is told a call is not counted, and a
// caller who somehow has no Tenant should share a bucket rather than escape
// one. It is also what makes a key of one kind safe to compose with another.
const keyTenant = "tenant:"

// ByTenant counts a call against the Tenant the caller is held by, which is
// what a per-Tenant limit means: the Tenant is who a deployment has a
// relationship with, and it is the level a share can be given at.
//
// Not the Holder, and not the credential. Both are things a Tenant makes as
// many of as it likes, so counting either would be a limit anybody could raise
// by asking for another one. What a limit per Holder is good for is the
// opposite problem -- one runaway client inside a Tenant starving the rest of
// it -- and an app that wants that counts against both, with the Tenant's line
// above the Holder's.
//
// A call nobody vouched for is not counted. Health and reflection are what
// reach a handler without a frame here, and a flood of calls that never
// authenticate is not something this can see anyway; see the README, "How much
// one caller may ask for".
//
// The method is ignored, so every RPC comes out of one bucket. An app that
// wants a `List` to be scarcer than a `Get` puts the method in the key and
// gives the limiter a rate per key, which is the same mechanism with a finer
// key rather than a second one.
func ByTenant() func(ctx context.Context, method string) string {
	return func(ctx context.Context, _ string) string {
		f, ok := frame.From(ctx)
		if !ok {
			return ""
		}

		// The raw identifier: it is what the row is keyed by, it is already in
		// hand, and nothing reads this but the map it is a key of.
		return keyTenant + string(f.Tenant().GetId())
	}
}
