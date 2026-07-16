package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/lesomnus/z"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type TLSConfig struct {
	// Enabled turns on TLS. It is implied when CertFile and KeyFile are set.
	Enabled bool `yaml:"enabled"`

	// CertFile is the path to the PEM-encoded server certificate.
	CertFile string `yaml:"cert_file"`
	// KeyFile is the path to the PEM-encoded server private key.
	KeyFile string `yaml:"key_file"`

	// ClientCAFile is the path to a PEM-encoded CA bundle used to verify
	// client certificates. Setting it enables mutual TLS.
	ClientCAFile string `yaml:"client_ca_file"`
}

// Active reports whether TLS should be used for the server.
func (c TLSConfig) Active() bool {
	return c.Enabled || c.CertFile != "" || c.KeyFile != ""
}

// Credentials builds gRPC transport credentials from the TLS configuration.
// It returns insecure credentials when TLS is not configured, so the caller
// can pass the result to grpc.Creds unconditionally.
func (c TLSConfig) Credentials() (credentials.TransportCredentials, error) {
	if !c.Active() {
		return insecure.NewCredentials(), nil
	}
	if c.CertFile == "" || c.KeyFile == "" {
		return nil, fmt.Errorf("both cert_file and key_file must be set to enable TLS")
	}

	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err != nil {
		return nil, z.Err(err, "load key pair")
	}

	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// Enable mutual TLS when a client CA bundle is provided.
	if c.ClientCAFile != "" {
		pem, err := os.ReadFile(c.ClientCAFile)
		if err != nil {
			return nil, z.Err(err, "read client ca")
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificates found in %q", c.ClientCAFile)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(cfg), nil
}
