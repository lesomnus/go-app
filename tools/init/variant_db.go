package main

import (
	"embed"
	"io/fs"
)

// The database half, which is what `kind/server` has and the other branches do
// not.
//
// It is in a file of its own rather than in `variant.go` for a reason that is
// about branches and not about tidiness: this repository keeps one per kind of
// app, and a list that differed per branch would put the difference in a shared
// file -- the one place every merge from `main` would land. A file that only
// exists on the branches that have the thing never conflicts with anything.
//
// A branch that adds a variant adds a file like this one. Nothing else changes.

//go:embed all:files/no-db
var dbFiles embed.FS

func init() {
	// Rooted at the directory, so what it holds is a tree to write over the
	// repository as-is.
	files, err := fs.Sub(dbFiles, "files/no-db")
	if err != nil {
		panic(err)
	}

	register("db", variant{
		Tag:   "db",
		Files: files,
		Brief: "no database: drop ent, the generated servers, the migrations and the sample entities",
		Paths: []string{
			// The store itself.
			"internal/ent",
			"internal/migrate",
			"server/bare",
			"migrations",
			"cmd/migrate.go",
			"cmd/config/db.go",
			"cmd/config/db-pgx.go",
			"cmd/config/db-sqlite3.go",
			"cmd/config/db_test.go",
			"scripts/db-init.sql",

			// What only exists because writes go through a database this app
			// owns: an event published from the transaction that made it, and
			// the loops that would run beside it.
			"server/watch",
			"server/spin",

			// The entity codegen. It exists to produce a database backend, so
			// without one it generates nothing anybody wants.
			"proto.svc",
			"buf.gen.svc.yaml",
			"scripts/gen-ent.sh",
			"scripts/gen-service.sh",

			// The sample, and everything generated from it.
			"proto/go_app",
			"go_app",
			"ui/src/gen",
			"ui/src/useCoffees.ts",
			"server/core/coffee.go",
			"server/core/roaster.go",
			"server/core/patch.go",
			"server/core/id.go",
			"server/core/server.go",
			"server/core/core_test.go",

			// The layer that binds the stack to an ent transaction, and the
			// tests that name the sample's RPCs.
			//
			// cmd/health.go is *not* here, and that is the point of the
			// `Store` interface in it: readiness following the store is a rule
			// about every store, so the file stays and an app answers `Ping`
			// with whatever it has.
			"server/gate/gate.go",
			"server/gate/gate_test.go",
			"server/gate/roles/roles_test.go",

			// The half of the test fixture that knows about the store. The
			// other half -- the interceptor chain, the assertions, the
			// in-memory connection -- is in internal/ox/stack.go and stays.
			"internal/ox/server.go",
			"internal/ox/client.go",
		},
		Note: `What is left is the transport and nothing behind it: the two listeners,
	authentication, the gate, the rate limit, the configuration and the health
	server. There are no entities, no service and no store.

	  1. Write proto/<pkg>/<thing>.proto -- messages and a service, by hand.
	     There is no entity codegen any more, so what you write is what is served.
	  2. ./scripts/gen-go.sh
	  3. Implement the service in server/core and register it in cmd/serve.go,
	     where a marker says where the sample used to be.
	  4. Build the test fixture on internal/ox.Stack, which is the half of the old
	     one that does not care what the store is.

	docs/EXTENDING.md has the long version.`,
	})
}
