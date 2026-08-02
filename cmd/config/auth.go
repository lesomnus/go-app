package config

import (
	"google.golang.org/grpc"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/auth"
)

type AuthConfig struct {
	// Plain believes a caller that says who it is:
	//
	//	authorization: Plain <tenant-alias>/<holder-alias>
	//
	// Nothing is checked, so it is for development and for a server nobody
	// untrusted can reach. It is off unless it is turned on, and turning it on
	// is said out loud at startup.
	Plain bool `yaml:"plain"`
}

// Handler is how a caller says who it is. Nothing is bundled that checks
// anything, so a server that is configured with none of them serves only what
// is public.
func (c AuthConfig) Handler() auth.Handler {
	hs := []auth.Handler{}
	if c.Plain {
		hs = append(hs, auth.Plain())
	}

	return auth.Seq(hs...)
}

// GrpcOptions works out who is calling, looking them up on `sink`, which is
// the server that answers out of the database and not out of the rules.
func (c AuthConfig) GrpcOptions(sink go_app.Server) []grpc.ServerOption {
	return auth.Interceptor(
		c.Handler(),
		auth.ServerResolver(sink),
		auth.PublicDefault,
	)
}
