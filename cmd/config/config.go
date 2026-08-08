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
	Db     DbConfig //template:db
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
	//
	// That last rule catches people out with secrets. `${env:API_KEY}` makes
	// *reading the file* fail on a machine that has no key, so `config` and
	// `version` stop working for somebody who has no reason to hold one. Write
	// `${env:API_KEY:-}` and let the command that needs it be the one to
	// refuse; see [Config.Evaluate].
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

// Evaluate completes the configuration and says whether it is usable: it fills
// in what was left unsaid, normalizes what was said loosely, and refuses what
// cannot be meant. Everything that is true of the configuration as a document
// belongs here, values included -- a duration that is negative, a CIDR that
// does not parse, two settings that contradict each other.
//
// What does **not** belong here is "this must be set" for something only one
// command uses. A credential that only `serve` needs, refused here, is a
// `config` and a `version` that fail on a machine that was never going to serve
// anything -- and a test suite that cannot construct a Config without it. Build
// it in the command instead, which fails just as early for the command that
// cares and not at all for the ones that do not.
//
// The line is not "shape here, values in the command". It is: **is this true of
// the configuration, or is it a requirement of one thing that reads it.**
func (c *Config) Evaluate() error {
	// Before anything else, since it is about the shape of this struct rather
	// than about what is in it: two fields folding to one environment variable
	// means one of them is unreachable, and which one depends on declaration
	// order. See [CheckEnvNames].
	if err := CheckEnvNames(c); err != nil {
		return err
	}

	z.FallbackP(&c.Server.Addr, ":50051")
	//template:begin db
	z.FallbackP(&c.Db.Driver, "sqlite3")
	z.FallbackP(&c.Db.Dsn, "file:go-app.db?_pragma=foreign_keys(1)")
	//template:end db
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
