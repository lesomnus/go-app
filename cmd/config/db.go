package config

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/lesomnus/go-app/internal/ent"
	"github.com/lesomnus/z"
)

type DbConfig struct {
	// Driver is the name of the database/sql driver to open the connection
	// with, e.g. "sqlite3" or "pgx". The drivers are registered by the
	// `db-*.go` files next to this one and an unknown name is reported along
	// with the ones that are available.
	Driver string `yaml:"driver"`

	// Dialect is the SQL dialect ent speaks to the database, one of "mysql",
	// "postgres" or "sqlite3". It is derived from Driver if it is empty, which
	// works for every driver registered with RegisterDriver.
	Dialect string `yaml:"dialect"`

	// Dsn is the data source name given to the driver, e.g.
	// "postgres://user:password@host:5432/database?sslmode=disable" or
	// "file:data.db?_pragma=foreign_keys(1)".
	Dsn string `yaml:"dsn"`

	// MaxOpenConns limits the number of open connections to the database.
	// Zero means unlimited.
	MaxOpenConns int `yaml:"max_open_conns"`
	// MaxIdleConns limits the number of idle connections in the pool.
	MaxIdleConns int `yaml:"max_idle_conns"`

	// Migrate runs ent's auto migration on startup. It is convenient during
	// development but a versioned migration is preferred in production.
	Migrate bool `yaml:"migrate"`
}

// dialects holds the ent dialect each known database/sql driver speaks.
var dialects = map[string]string{}

// RegisterDriver records that the driver named `driver` speaks `dialect`, so
// that a configuration only has to name the driver. Adding support for another
// database is a file next to this one that blank imports the driver and calls
// RegisterDriver from its init.
func RegisterDriver(driver string, dialect string) {
	dialects[driver] = dialect
}

// Drivers lists the registered drivers in lexical order.
func Drivers() []string {
	return slices.Sorted(maps.Keys(dialects))
}

func (c DbConfig) dialect() (string, error) {
	if c.Dialect != "" {
		return c.Dialect, nil
	}

	v, ok := dialects[c.Driver]
	if !ok {
		return "", fmt.Errorf("unknown driver %q: it must be one of %v, or db.dialect must be given", c.Driver, Drivers())
	}

	return v, nil
}

// Db is a connected database. It is an ent client, with the connection behind
// it kept at hand so that the app can tell whether the database is still
// there. The caller owns it and must close it.
type Db struct {
	*ent.Client

	db *sql.DB
}

// Ping reports whether the database can still be reached.
func (d *Db) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

// Open connects to the database.
func (c DbConfig) Open(ctx context.Context) (*Db, error) {
	dialect, err := c.dialect()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(c.Driver, c.Dsn)
	if err != nil {
		return nil, z.Err(err, "open")
	}
	if c.MaxOpenConns > 0 {
		db.SetMaxOpenConns(c.MaxOpenConns)
	}
	if c.MaxIdleConns > 0 {
		db.SetMaxIdleConns(c.MaxIdleConns)
	}

	// Fail fast if the database is not reachable.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, z.Err(err, "ping")
	}

	return &Db{
		Client: ent.NewClient(ent.Driver(entsql.OpenDB(dialect, db))),
		db:     db,
	}, nil
}
