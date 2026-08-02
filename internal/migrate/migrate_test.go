package migrate_test

import (
	"context"
	"database/sql"
	"net/url"
	"testing"

	"entgo.io/ent/dialect"
	"github.com/ncruces/go-sqlite3/driver"
	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/stretchr/testify/require"

	"github.com/lesomnus/go-app/internal/migrate"
)

// open returns an empty SQLite database that lives in memory. The migrations
// that are shipped are for PostgreSQL, but the machinery is the same, and this
// is the one database a test can have all to itself.
func open(t *testing.T) *sql.DB {
	t.Helper()

	db, err := driver.Open(memdb.TestDB(t, url.Values{"_pragma": {"foreign_keys(1)"}}))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	return db
}

func tables(ctx context.Context, t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	require.NoError(t, err)
	defer rows.Close()

	vs := []string{}
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		vs = append(vs, v)
	}
	require.NoError(t, rows.Err())

	return vs
}

func TestPlanApply(t *testing.T) {
	x := require.New(t)
	ctx := t.Context()

	dir, err := migrate.OpenDir(t.TempDir())
	x.NoError(err)

	// Nothing is written yet, so the plan is the whole schema.
	fs, err := migrate.Plan(ctx, dir, open(t), dialect.SQLite, "init")
	x.NoError(err)
	x.Len(fs, 1)
	x.Contains(fs[0].Name(), "_init.sql")
	x.Contains(string(fs[0].Bytes()), `CREATE TABLE `+"`tenant`")

	// What was written is what is applied.
	db := open(t)
	pending, err := migrate.Pending(ctx, db, dialect.SQLite, dir)
	x.NoError(err)
	x.Len(pending, 1)

	applied, err := migrate.Apply(ctx, db, dialect.SQLite, dir)
	x.NoError(err)
	x.Len(applied, 1)
	x.Subset(tables(ctx, t, db), []string{"tenant", "holder", migrate.RevisionTable})

	// Applying again is not applying anything.
	pending, err = migrate.Pending(ctx, db, dialect.SQLite, dir)
	x.NoError(err)
	x.Empty(pending)

	applied, err = migrate.Apply(ctx, db, dialect.SQLite, dir)
	x.NoError(err)
	x.Empty(applied)

	// The schema did not move, so there is nothing left to plan.
	fs, err = migrate.Plan(ctx, dir, open(t), dialect.SQLite, "noop")
	x.NoError(err)
	x.Empty(fs)
}

func TestOpenDir(t *testing.T) {
	t.Run("a file that was changed after it was written is refused", func(t *testing.T) {
		x := require.New(t)
		ctx := t.Context()

		p := t.TempDir()
		dir, err := migrate.OpenDir(p)
		x.NoError(err)

		fs, err := migrate.Plan(ctx, dir, open(t), dialect.SQLite, "init")
		x.NoError(err)
		x.Len(fs, 1)

		x.NoError(dir.WriteFile(fs[0].Name(), []byte("SELECT 1;")))

		_, err = migrate.OpenDir(p)
		x.ErrorContains(err, "atlas.sum")
	})
}
