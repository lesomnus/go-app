package main

// The `--no-db` case of TestVariantsBuild, in the same file-per-branch shape as
// the variant it is about: a branch without the database half has neither this
// file nor `variant_db.go`, and nothing has to be deleted from a shared one.
func init() {
	variantCases = append(variantCases, variantCase{
		name: "no-db",
		args: []string{"--no-generate", "--no-db", "github.com/acme/svc"},
		gone: []string{
			"internal/ent", "server/bare", "migrations", "internal/migrate",
			"server/watch", "server/spin", "proto.svc", "svc", "cmd/migrate.go",
			"cmd/config/db.go", "buf.gen.svc.yaml", "internal/ox/server.go",
			"server/core/coffee.go", "server/gate/gate.go", "tools/init",
		},
		kept: []string{
			"server/auth", "server/gate", "internal/grpcx", "internal/httpx",
			"internal/ox/stack.go", "cmd/serve.go", "svc.yaml", "ui/src/App.tsx",
			// Written by the variant in place of what it removed.
			"ui/src/client.ts",
		},
	})
}
