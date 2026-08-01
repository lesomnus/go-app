package config

import (
	"entgo.io/ent/dialect"

	// PostgreSQL driver. It is a pure Go driver so the binary can still be
	// built with CGO_ENABLED=0.
	_ "github.com/jackc/pgx/v5/stdlib"
)

func init() {
	RegisterDriver("pgx", dialect.Postgres)
}
