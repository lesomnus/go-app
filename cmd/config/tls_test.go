package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/lesomnus/go-app/cmd/config"
)

func TestTLSActive(t *testing.T) {
	t.Run("nothing written down is no TLS", func(t *testing.T) {
		x := require.New(t)

		var c config.TLSConfig
		x.False(c.Active())

		creds, err := c.Credentials()
		x.NoError(err)
		x.Equal(insecure.NewCredentials(), creds)
	})

	t.Run("a client CA is asking for TLS, and used not to count", func(t *testing.T) {
		x := require.New(t)

		// This is the configuration `Evaluate` demands for `auth.methods:
		// [mtls]`, and it used to be served with insecure credentials: no
		// handshake, no certificate ever presented, and `mtls` answering
		// "nobody said anything" for the life of the process.
		//
		// Authentication failed closed there and confidentiality failed open,
		// which is the half nothing reported.
		c := config.TLSConfig{ClientCAFile: "ca.pem"}
		x.True(c.Active())

		_, err := c.Credentials()
		x.ErrorContains(err, "cert_file", "mutual TLS with no certificate has to be refused")
	})

	t.Run("each half on its own is still asking for TLS", func(t *testing.T) {
		x := require.New(t)

		for _, c := range []config.TLSConfig{
			{Enabled: true},
			{CertFile: "cert.pem"},
			{KeyFile: "key.pem"},
		} {
			x.True(c.Active())

			_, err := c.Credentials()
			x.Error(err, "half a key pair is a server that must not start")
		}
	})
}
