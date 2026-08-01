// Package migrate plans and applies the versioned migrations of the database.
//
// The ent schema is the one description of what the database should look like;
// planning turns a change of it into a file of SQL statements that brings a
// database from the shape it has to the shape it should have. The files are
// kept in the repository, reviewed like any other code, and applied in order.
//
// Nothing here talks to a service or needs a tool of its own: the planning and
// the applying are both done by the atlas packages ent already depends on.
package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"ariga.io/atlas/sql/migrate"
	atpostgres "ariga.io/atlas/sql/postgres"
	atsqlite "ariga.io/atlas/sql/sqlite"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	"github.com/lesomnus/z"

	entmigrate "github.com/lesomnus/go-app/internal/ent/migrate"
)

// DefaultDir is where the migration files are kept.
const DefaultDir = "migrations"

// Dialect is what the migration files are written for.
//
// The app runs on any database ent speaks, but a migration is SQL and SQL is
// not the same everywhere, so the files that are shipped are for one database
// only. That one is PostgreSQL; see the migration guide in README.md for what
// it takes to make it another.
const Dialect = dialect.Postgres

// OpenDir opens the directory of migration files at `path` and makes sure none
// of them was touched after it was written, which is what `atlas.sum` records.
func OpenDir(path string) (*migrate.LocalDir, error) {
	d, err := migrate.NewLocalDir(path)
	if err != nil {
		return nil, z.Err(err, "open %q", path)
	}
	if err := migrate.Validate(d); err != nil {
		return nil, z.Err(err, "%q does not match its atlas.sum", path)
	}

	return d, nil
}

// Plan writes the migration files that bring a database to the state the ent
// schema describes, and returns the files it wrote.
//
// `dev` is a dev database: an empty database of the same kind as the one the
// app runs on, onto which the migration files that are already written are
// replayed to work out what the current state is. It is written to and emptied
// again, so it must not be a database anyone cares about.
func Plan(ctx context.Context, dir *migrate.LocalDir, dev *sql.DB, dialect string, name string) ([]migrate.File, error) {
	before, err := dir.Files()
	if err != nil {
		return nil, z.Err(err, "read the migration directory")
	}

	m, err := entschema.NewMigrate(entsql.OpenDB(dialect, dev),
		entschema.WithDir(dir),
		// One statement per version, which is what the executor reads back.
		entschema.WithFormatter(migrate.DefaultFormatter),
		// Replay what is already written instead of trusting the state the dev
		// database happens to be in.
		entschema.WithMigrationMode(entschema.ModeReplay),
		// Plan the destructive changes as well; they are reviewed before they
		// are applied, and a change that is silently skipped is worse.
		entschema.WithDropColumn(true),
		entschema.WithDropIndex(true),
	)
	if err != nil {
		return nil, z.Err(err, "new migrate")
	}
	if err := m.NamedDiff(ctx, name, entmigrate.Tables...); err != nil {
		return nil, z.Err(err, "plan")
	}

	after, err := dir.Files()
	if err != nil {
		return nil, z.Err(err, "read the migration directory")
	}

	return after[len(before):], nil
}

// Pending returns the migration files that were not applied yet, in the order
// they are to be applied.
func Pending(ctx context.Context, db *sql.DB, dialect string, dir migrate.Dir) ([]migrate.File, error) {
	ex, err := executor(ctx, db, dialect, dir)
	if err != nil {
		return nil, err
	}

	fs, err := ex.Pending(ctx)
	if err != nil {
		if errors.Is(err, migrate.ErrNoPendingFiles) {
			return nil, nil
		}

		return nil, z.Err(err, "read what is pending")
	}

	return fs, nil
}

// Apply runs every migration file that was not applied yet and returns them.
// A file is recorded as applied only once every statement in it has run.
func Apply(ctx context.Context, db *sql.DB, dialect string, dir migrate.Dir) ([]migrate.File, error) {
	ex, err := executor(ctx, db, dialect, dir)
	if err != nil {
		return nil, err
	}

	fs, err := ex.Pending(ctx)
	if err != nil {
		if errors.Is(err, migrate.ErrNoPendingFiles) {
			return nil, nil
		}

		return nil, z.Err(err, "read what is pending")
	}
	if err := ex.ExecuteN(ctx, len(fs)); err != nil {
		return nil, z.Err(err, "execute")
	}

	return fs, nil
}

func executor(ctx context.Context, db *sql.DB, dialect string, dir migrate.Dir) (*migrate.Executor, error) {
	drv, err := driver(db, dialect)
	if err != nil {
		return nil, err
	}

	rrw, err := NewRevisions(ctx, db, dialect)
	if err != nil {
		return nil, err
	}

	v, err := migrate.NewExecutor(drv, dir, rrw)
	if err != nil {
		return nil, z.Err(err, "new executor")
	}

	return v, nil
}

// driver adapts a connection to the atlas driver of the dialect it speaks. Add
// a case to migrate another kind of database; the drivers live under
// `ariga.io/atlas/sql`.
func driver(db *sql.DB, d string) (migrate.Driver, error) {
	switch d {
	case dialect.Postgres:
		return atpostgres.Open(db)
	case dialect.SQLite:
		return atsqlite.Open(db)
	default:
		return nil, fmt.Errorf("nothing migrates a %q database yet: see internal/migrate", d)
	}
}
