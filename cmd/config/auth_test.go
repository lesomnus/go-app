package config_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"

	"github.com/lesomnus/go-app/cmd/config"
	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/auth"
)

func incoming(v string) context.Context {
	md := metadata.MD{}
	if v != "" {
		md.Set("authorization", v)
	}
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestAuthMethods(t *testing.T) {
	t.Run("the order is the order they are tried", func(t *testing.T) {
		x := require.New(t)

		c := config.AuthConfig{
			Methods: []string{"bearer", "plain"},
			Bearer: config.BearerConfig{
				Holders: map[string]string{"acme/admin": "s3cret"},
			},
		}

		h, err := c.Handler()
		x.NoError(err)

		// The token when there is one.
		v, err := h.Handle(incoming("Bearer s3cret"))
		x.NoError(err)
		x.Equal(auth.MethodBearer, v.Method)

		// The next method when there is not.
		v, err = h.Handle(incoming("Plain hooli/erlich"))
		x.NoError(err)
		x.Equal(auth.MethodPlain, v.Method)

		// And a token that is not one refuses rather than falling through.
		_, err = h.Handle(incoming("Bearer nope"))
		x.ErrorIs(err, auth.ErrUnknownToken)
	})

	t.Run("nothing configured serves only what is public", func(t *testing.T) {
		x := require.New(t)

		h, err := config.AuthConfig{}.Handler()
		x.NoError(err)

		_, err = h.Handle(incoming("Plain acme/admin"))
		x.ErrorIs(err, auth.ErrNoCredential)
	})

	t.Run("a method nobody wrote is refused at startup", func(t *testing.T) {
		x := require.New(t)

		_, err := config.AuthConfig{Methods: []string{"oauth"}}.Handler()
		x.ErrorContains(err, "oauth")
	})

	t.Run("plain is what the server says out loud", func(t *testing.T) {
		x := require.New(t)

		x.True(config.AuthConfig{Methods: []string{"plain", "mtls"}}.Believes())
		x.False(config.AuthConfig{Methods: []string{"bearer", "mtls"}}.Believes())
	})
}

func TestBearerConfig(t *testing.T) {
	ref := go_app.HolderBySlug("admin", go_app.TenantByAlias("acme"))

	t.Run("a holder that is not a name is refused at startup", func(t *testing.T) {
		x := require.New(t)

		_, err := config.BearerConfig{
			Holders: map[string]string{"Acme Corporation": "s3cret"},
		}.Store(time.Now())
		x.Error(err)

		_, err = config.BearerConfig{
			Holders: map[string]string{"acme/admin": ""},
		}.Store(time.Now())
		x.ErrorContains(err, "no token")
	})

	t.Run("ttl is counted from when the server started", func(t *testing.T) {
		x := require.New(t)

		at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		s, err := config.BearerConfig{
			Holders: map[string]string{"acme/admin": "s3cret"},
			TTL:     time.Hour,
		}.Store(at)
		x.NoError(err)

		mem, ok := s.(*auth.MemTokenStore)
		x.True(ok)

		mem.Now = func() time.Time { return at.Add(30 * time.Minute) }
		v, err := mem.Lookup(t.Context(), "s3cret")
		x.NoError(err)
		x.Equal(ref.GetSlug().GetAlias(), v.GetSlug().GetAlias())

		mem.Now = func() time.Time { return at.Add(2 * time.Hour) }
		_, err = mem.Lookup(t.Context(), "s3cret")
		x.ErrorIs(err, auth.ErrUnknownToken)
	})

	t.Run("no ttl never expires", func(t *testing.T) {
		x := require.New(t)

		at := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
		s, err := config.BearerConfig{
			Holders: map[string]string{"acme/admin": "s3cret"},
		}.Store(at)
		x.NoError(err)

		mem := s.(*auth.MemTokenStore)
		mem.Now = func() time.Time { return at.AddDate(10, 0, 0) }
		_, err = mem.Lookup(t.Context(), "s3cret")
		x.NoError(err)
	})
}

func TestAuthEvaluate(t *testing.T) {
	t.Run("mtls without a bundle to check against is refused", func(t *testing.T) {
		x := require.New(t)

		c := config.Config{}
		c.Auth.Methods = []string{"mtls"}
		x.ErrorContains(c.Evaluate(), "client_ca_file")

		c.Server.TLS.ClientCAFile = "/dev/null"
		x.NoError(c.Evaluate())
	})
}
