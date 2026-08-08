package config

import (
	"fmt"
	"slices"
	"time"

	"google.golang.org/grpc"

	"github.com/lesomnus/go-app/server/auth"
	"github.com/lesomnus/go-app/server/frame"
)

type AuthConfig struct {
	// Methods is how a caller may say who it is, tried in the order they are
	// written. The first one that finds a credential answers; one that finds a
	// bad credential, or cannot tell whether it is good, refuses the call
	// rather than letting the next one have a go.
	//
	//	methods: [bearer, mtls]   a token, and the certificate for whoever has none
	//	methods: [plain, mtls]    the same, believing what the caller says
	//	methods: [mtls]           the certificate and nothing else
	//	methods: []               nobody can say, so every caller is anonymous
	//
	// The order is the point. Falling back the other way -- the certificate
	// first -- would mean a caller that presented both is served as the
	// certificate, and the token they went to the trouble of getting is never
	// read.
	Methods []string `yaml:"methods"`

	// Bearer is the sample token store. See [BearerConfig].
	Bearer BearerConfig `yaml:"bearer"`
}

// BearerConfig is a token store written in the configuration file, which is a
// sample and not a way to run anything.
//
// Tokens are secrets, and secrets in a configuration file are secrets in a
// backup, in a terminal history and in whatever reads the file to check
// something else. A real store is a table or an issuer; what this shows is the
// shape of one, not a place to keep them.
type BearerConfig struct {
	// Subjects maps who a token stands for to the token itself:
	//
	//	bearer:
	//	  subjects:
	//	    ci:
	//	      token: "${env:CI_TOKEN}"
	//
	// The subject is an opaque string. In a real deployment it is whatever the
	// issuer calls people, and nothing in this app reads it.
	Subjects map[string]TokenConfig `yaml:"subjects"`

	// TTL is how long each of them is honoured for, counted from when the
	// server started. Zero means they do not expire.
	//
	// It is here because a token having a life of its own is the thing that
	// makes it different from a header or a certificate, and a sample that
	// left it out would be a sample of the easy half.
	TTL time.Duration `yaml:"ttl"`
}

// TokenConfig is one sample token: the secret, and what it may be used for.
//
// What it may be used for is an attenuation and never a widening -- it can only
// take away from what the subject it names could do. Saying nothing takes
// nothing away, which is what a token with no attenuation is.
type TokenConfig struct {
	// Token is the secret itself.
	Token string `yaml:"token"`

	// Actions narrows the token to these RPCs, by the name gRPC knows them by
	// -- "/go_app.CoffeeService/Get". Saying none leaves it usable for every
	// RPC its subject may call.
	Actions []string `yaml:"actions"`
}

// Grant is what this token allows of what its subject allows.
func (c TokenConfig) Grant() frame.Grant {
	if len(c.Actions) == 0 {
		return frame.Whole()
	}

	return frame.To(c.Actions...)
}

// Store builds the sample token store.
func (c BearerConfig) Store(now time.Time) (auth.TokenStore, error) {
	s := auth.NewMemTokenStore()

	var exp time.Time
	if c.TTL > 0 {
		exp = now.Add(c.TTL)
	}

	for subject, tc := range c.Subjects {
		if tc.Token == "" {
			return nil, fmt.Errorf("bearer.subjects[%q]: no token", subject)
		}
		if subject == "" {
			// The empty subject is what anonymous is, and a token that stood
			// for nobody would be a credential that authenticated as one.
			return nil, fmt.Errorf("bearer.subjects: one of them names nobody")
		}

		s.Add(tc.Token, frame.Actor{Subject: subject}, tc.Grant(), exp)
	}

	return s, nil
}

// Believes reports whether a caller can be believed about who they are, which
// is what the server says out loud when it starts.
func (c AuthConfig) Believes() bool {
	return slices.Contains(c.Methods, auth.MethodPlain)
}

// Handler is how a caller says who it is. A server configured with no method
// has only anonymous callers.
func (c AuthConfig) Handler() (auth.Handler, error) {
	hs := make([]auth.Handler, 0, len(c.Methods))
	for i, m := range c.Methods {
		switch m {
		case auth.MethodPlain:
			hs = append(hs, auth.Plain())

		case auth.MethodMTLS:
			hs = append(hs, auth.MTLS())

		case auth.MethodBearer:
			store, err := c.Bearer.Store(time.Now())
			if err != nil {
				return nil, err
			}
			hs = append(hs, auth.Bearer(store))

		default:
			return nil, fmt.Errorf("auth.methods[%d]: %q is not a way to say who is calling; it must be one of %v",
				i, m, []string{auth.MethodPlain, auth.MethodMTLS, auth.MethodBearer})
		}
	}

	return auth.Seq(hs...), nil
}

// GrpcOptions works out who is calling. A caller who says nothing is served as
// anonymous rather than refused; what an anonymous caller may do is
// `server/gate`'s, and is `server.allow_anonymous_reads`.
func (c AuthConfig) GrpcOptions() ([]grpc.ServerOption, error) {
	h, err := c.Handler()
	if err != nil {
		return nil, err
	}

	return auth.Interceptor(h), nil
}
