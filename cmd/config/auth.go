package config

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	go_app "github.com/lesomnus/go-app/go_app"
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
	//	methods: []               nobody can say, so only what is public is served
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
	// Holders maps a Holder to the token that stands for it:
	//
	//	bearer:
	//	  holders:
	//	    acme/admin:
	//	      token: "${env:ADMIN_TOKEN}"
	//
	// A Holder is named the way anything else names one: `<tenant>/<alias>`,
	// or its identifier.
	Holders map[string]TokenConfig `yaml:"holders"`

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
// take away from what the Holder it names could do. Saying nothing takes
// nothing away, which is what a token with no attenuation is.
type TokenConfig struct {
	// Token is the secret itself.
	Token string `yaml:"token"`

	// Tenants narrows the token to these, by identifier. Saying none leaves it
	// usable wherever the Holder may act.
	//
	// By identifier and not by alias, which is the sample showing its seams: a
	// configuration file is read before anything is connected, so there is
	// nothing to resolve an alias against. A real store is a table, where the
	// identifier is what is there anyway.
	Tenants []string `yaml:"tenants"`

	// Actions narrows the token to these RPCs, by the name gRPC knows them by
	// -- "/go_app.HolderService/Get". Saying none leaves it usable for every
	// RPC the Holder may call.
	Actions []string `yaml:"actions"`
}

// Grant is what this token allows of what its Holder allows.
func (c TokenConfig) Grant() (frame.Grant, error) {
	v := frame.Whole()
	if len(c.Tenants) > 0 {
		ks := make([]uuid.UUID, len(c.Tenants))
		for i, t := range c.Tenants {
			k, err := uuid.Parse(t)
			if err != nil {
				return frame.Grant{}, fmt.Errorf("tenants[%d]: %w", i, err)
			}

			ks[i] = k
		}

		v = v.In(ks...)
	}
	if len(c.Actions) > 0 {
		v = v.To(c.Actions...)
	}

	return v, nil
}

// Store builds the sample token store.
func (c BearerConfig) Store(now time.Time) (auth.TokenStore, error) {
	s := auth.NewMemTokenStore()

	var exp time.Time
	if c.TTL > 0 {
		exp = now.Add(c.TTL)
	}

	for holder, tc := range c.Holders {
		if tc.Token == "" {
			return nil, fmt.Errorf("bearer.holders[%q]: no token", holder)
		}

		ref, err := auth.ParseRef(holder)
		if err != nil {
			return nil, fmt.Errorf("bearer.holders: %w", err)
		}

		grant, err := tc.Grant()
		if err != nil {
			return nil, fmt.Errorf("bearer.holders[%q].%w", holder, err)
		}

		s.Add(tc.Token, ref, grant, exp)
	}

	return s, nil
}

// Believes reports whether a caller can be believed about who they are, which
// is what the server says out loud when it starts.
func (c AuthConfig) Believes() bool {
	return slices.Contains(c.Methods, auth.MethodPlain)
}

// Handler is how a caller says who it is. A server configured with no method
// serves only what is public.
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

// GrpcOptions works out who is calling, looking them up on `sink`, which is
// the server that answers out of the database and not out of the rules.
func (c AuthConfig) GrpcOptions(sink go_app.Server) ([]grpc.ServerOption, error) {
	h, err := c.Handler()
	if err != nil {
		return nil, err
	}

	return auth.Interceptor(h, auth.ServerResolver(sink), auth.PublicDefault), nil
}
