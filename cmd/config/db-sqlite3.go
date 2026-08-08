package config

import (
	"entgo.io/ent/dialect"

	// SQLite driver. It runs SQLite compiled to Wasm, so it needs neither cgo
	// nor a system library.
	//
	// Note that foreign keys are off by default in SQLite; the DSN should ask
	// for them, e.g. "file:data.db?_pragma=foreign_keys(1)".
	_ "github.com/ncruces/go-sqlite3/driver"

	// A database that lives in memory and goes when the process does, reached
	// by naming it in the DSN: "file:/whatever.db?vfs=memdb". It is what the
	// tests run on and what `go-app.yaml` ships, so that the template runs with
	// nothing around it and leaves nothing behind.
	//
	// Every connection that names the same file gets the same database, which
	// is what makes it usable by a server rather than only by one goroutine --
	// and is also why `db.max_open_conns` is worth setting to 1 on it: SQLite
	// takes one writer, and a second connection asking to write reports a busy
	// database rather than waiting.
	_ "github.com/ncruces/go-sqlite3/vfs/memdb"
)

func init() {
	RegisterDriver("sqlite3", dialect.SQLite)
}
