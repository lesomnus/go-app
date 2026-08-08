package config

import (
	"fmt"
	"os"
	"slices"

	"github.com/goccy/go-yaml"
	"github.com/lesomnus/mkot"
	"github.com/lesomnus/z"

	"github.com/lesomnus/go-app/server/auth"
)

var DefaultConfigPaths = []string{
	"go-app.yaml",
	"go-app.yml",
}

type Config struct {
	path string

	Server ServerConfig
	Auth   AuthConfig
	Db     DbConfig
	Greet  GreetConfig

	Otel OtelConfig
}

func ReadFromFile(p string) (*Config, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, z.Err(err, "open")
	}

	// "${env:NAME}" and "${env:NAME:-default}" are resolved before the file is
	// read, the way the OpenTelemetry Collector resolves them, so a secret can
	// be named in the file without being written in it. A name that is neither
	// set nor given a default is an error rather than an empty string. Write
	// "$$" for a literal dollar sign.
	b, err = mkot.ExpandEnv(b)
	if err != nil {
		return nil, z.Err(err, "expand")
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, z.Err(err, "decode")
	}

	c.path = p
	return &c, nil
}

func (c *Config) Path() string {
	return c.path
}

func (c *Config) Evaluate() error {
	// Before anything else, since it is about the shape of this struct rather
	// than about what is in it: two fields folding to one environment variable
	// means one of them is unreachable, and which one depends on declaration
	// order. See [CheckEnvNames].
	if err := CheckEnvNames(c); err != nil {
		return err
	}

	z.FallbackP(&c.Server.Addr, ":50051")
	z.FallbackP(&c.Db.Driver, "sqlite3")
	z.FallbackP(&c.Db.Dsn, "file:go-app.db?_pragma=foreign_keys(1)")
	z.FallbackP(&c.Greet.Format, "Hello, %s!")

	// A certificate says who is calling only if it was checked against
	// something. Without a bundle to check it against, no connection ever
	// carries a verified chain, and the method would sit there answering
	// "nobody said anything" for the life of the server.
	if slices.Contains(c.Auth.Methods, auth.MethodMTLS) && c.Server.TLS.ClientCAFile == "" {
		return fmt.Errorf("auth.methods has %q but server.tls.client_ca_file is not set, so no certificate would ever be verified", auth.MethodMTLS)
	}

	// Building it here rather than at the first request, so that a token that
	// names nobody, or a method nobody wrote, is a server that does not start
	// rather than one that refuses everyone.
	if _, err := c.Auth.Handler(); err != nil {
		return err
	}

	return nil
}
