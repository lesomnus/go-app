package gate

import (
	"context"

	"github.com/lesomnus/go-app/server/frame"
)

// keySubject is written in front of the subject so that a key is never empty --
// an empty key is how [grpcx.Limit] is told a call is not counted -- and so
// that a key of one kind is safe to compose with another.
const keySubject = "subject:"

// keyAnonymous is what every caller nobody vouched for is counted against.
const keyAnonymous = keySubject + "-"

// BySubject counts a call against whoever the caller is, and counts every
// anonymous caller against one bucket between them.
//
// That last part is the whole of what this can honestly do, and it is worth
// knowing before the number is chosen. An anonymous caller has nothing to be
// told apart by: an address is the load balancer's, or a company's, and this
// app is behind at least one of those. So a limit here protects the app from
// anonymous traffic *in total* and does nothing about one anonymous caller
// among many -- which is the layer in front's to do, where the addresses are
// real.
//
// A call nobody vouched for at all -- the app calling itself -- is not counted.
func BySubject() func(ctx context.Context, method string) string {
	return func(ctx context.Context, _ string) string {
		f, ok := frame.From(ctx)
		if !ok {
			return ""
		}
		if f.Actor.IsAnonymous() {
			return keyAnonymous
		}

		return keySubject + f.Actor.Subject
	}
}
