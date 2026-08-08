package main

import "embed"

// replacements are the files a variant writes in place of what it removed.
//
// A marker cannot do this job. The template has to compile with every marker
// still in it, so a marked region can never be a second declaration of
// something -- two `App` components or two `connect` functions do not both
// compile, whichever one is about to be deleted. Where a variant replaces a
// whole file rather than part of one, the replacement lives here and is copied
// over the tree.
//
// It is a cost worth naming: these are files that have to be kept working
// alongside the ones they replace, and CI builds the variant precisely so that
// they are. Keep them few. Everything that can be a marker should be one.
//
//go:embed all:files
var replacements embed.FS

// A variant is a part of the template that can be left out.
//
// There is one, and adding a second should wait until somebody has actually
// wanted it. Every variant is a shape this repository promises to keep working,
// which means a column in CI and a thing to fix when the template moves -- so a
// variant nobody asked for is a cost with nothing on the other side.
//
// What a variant is made of is two things:
//
//   - Paths, for whole files and directories. Listed here, because a file that
//     exists only to serve one part of the template has nothing to mark.
//   - A tag, for the places inside files that stay. Those are marked at the
//     coupling itself; see marker.go.
type variant struct {
	// Tag is what its markers say, and is the flag name without `--no-`.
	Tag string

	// Brief is one line, for `--help`.
	Brief string

	// Note is printed after the tool has run, and says what somebody is now
	// holding. A variant that removes the sample leaves questions that the
	// README cannot answer generically.
	Note string

	// Paths go entirely. A directory takes what is in it.
	Paths []string
}

// variants is every part that can be left out, by flag name.
var variants = map[string]variant{
	"db": {
		Tag:   "db",
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
			"server/gate/gate.go",
			"server/gate/gate_test.go",
			"server/gate/roles/roles_test.go",
			"cmd/health_test.go",

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
	},
}

// selfTag marks what belongs to the template rather than to any app made from
// it: this tool, and the CI job that checks the variants still build.
//
// It goes whenever the tool removes itself, which is always unless somebody
// asked otherwise -- there is no app for which `go run ./tools/init` is a thing
// that exists, so a CI job calling it would fail on the first push.
const selfTag = "self"

// tagsOf is the set of tags to strip for the variants being left out.
func tagsOf(left []string, self bool) map[string]bool {
	tags := make(map[string]bool, len(left)+1)
	for _, name := range left {
		tags[variants[name].Tag] = true
	}
	if self {
		tags[selfTag] = true
	}

	return tags
}
