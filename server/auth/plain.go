package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc/metadata"

	"github.com/lesomnus/go-app/server/frame"
)

// MethodPlain is what [Plain] calls itself.
const MethodPlain = "plain"

// PlainScheme is the authorization scheme [Plain] reads and [PlainProvider]
// writes.
const PlainScheme = "Plain"

// Plain believes whatever the caller says it is:
//
//	authorization: Plain <subject>
//
// There is nothing to check and it checks nothing, which is the point: a test
// or a hand written call says who it is and gets on with it. It must not be
// reachable by anyone who is not already trusted to say the truth.
//
// The subject is whatever string a deployment's real issuer would have put
// there, and nothing here reads it -- so a test can use a name and production
// can use whatever an IdP calls people, and the same code serves both.
func Plain() Handler {
	return HandlerFunc(func(ctx context.Context) (Identity, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return Identity{}, ErrNoCredential
		}

		for _, v := range md.Get("authorization") {
			rest, ok := strings.CutPrefix(v, PlainScheme+" ")
			if !ok {
				continue
			}

			subject := strings.TrimSpace(rest)
			if subject == "" {
				// Something was said, and it was not a name; that is not the
				// same as saying nothing.
				return Identity{}, fmt.Errorf("%s: %w", PlainScheme, errSaysNothing)
			}

			// A header has nowhere to carry an attenuation, so it narrows
			// nothing and says so rather than leaving the zero Grant, which
			// allows nothing at all.
			return Identity{
				Method: MethodPlain,
				Actor:  frame.Actor{Subject: subject},
				Grant:  frame.Whole(),
			}, nil
		}

		return Identity{}, ErrNoCredential
	})
}

// PlainProvider says who the caller is in the way [Plain] reads.
//
// It replaces whatever was said before rather than adding to it. Two answers
// to "who is calling" is not twice as much information; it is a question with
// no answer, and the one that would win is whichever came first.
func PlainProvider(v string) Provider {
	return ProviderFunc(func(ctx context.Context) context.Context {
		md, ok := metadata.FromOutgoingContext(ctx)
		if !ok {
			md = metadata.MD{}
		} else {
			md = md.Copy()
		}

		md.Set("authorization", PlainScheme+" "+v)
		return metadata.NewOutgoingContext(ctx, md)
	})
}

// errSaysNothing is a credential that is there and names nobody.
var errSaysNothing = errors.New("says nothing")
