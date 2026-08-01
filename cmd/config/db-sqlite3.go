package config

import (
	"entgo.io/ent/dialect"

	// SQLite driver. It runs SQLite compiled to Wasm, so it needs neither cgo
	// nor a system library.
	//
	// Note that foreign keys are off by default in SQLite; the DSN should ask
	// for them, e.g. "file:data.db?_pragma=foreign_keys(1)".
	_ "github.com/ncruces/go-sqlite3/driver"
)

func init() {
	RegisterDriver("sqlite3", dialect.SQLite)
}
