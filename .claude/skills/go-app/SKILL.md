---
name: go-app
description: Work on this repository — add an entity or an RPC, regenerate after changing a proto, or change a server layer. Use when editing anything under proto/, proto.svc/, server/, cmd/, internal/, or ui/, and whenever generated Go or TypeScript looks out of date.
---

# Working in this repository

Most of what is here is **generated from `proto/`**. The failure this skill
exists to prevent is editing the output instead of the input, or running the
generators out of order and reading the resulting mess as a bug.

Read [docs/EXTENDING.md](../../../docs/EXTENDING.md) before adding anything. It
is the procedure; this is the short version plus the traps.

**`Roaster` and `Coffee` are a sample.** In an app made from this template they
are meant to have been replaced, so a task about "the entities" is probably
about replacing them and not about adding a third — ask which if it is not
clear. The procedure is
[Replacing the sample](../../../docs/EXTENDING.md#replacing-the-sample), and the
part of it that is silently forgotten is the migrations: the bundled SQLite
configuration never reads `migrations/`, so a stale directory breaks nothing
until a deployment runs on PostgreSQL.

## Never edit these — they are generated

```
go_app/                        internal/ent/
server/bare/                   ui/src/gen/
proto/go_app/*_svc.proto       proto.svc/go_app/*_svc.g.proto
migrations/*.sql               (written by `migrate plan`, then read and committed)
```

Write in `proto/go_app/<name>.proto`, in `proto.svc/go_app/<name>_svc.ext.proto`,
or in `server/`, `cmd/`, `internal/`, `ui/src/` (outside `gen/`).

If a build error names a file in the list above, the fix is upstream of it.

## Regenerating

Four commands, **in this order**, from the repository root:

```sh
buf generate --template buf.gen.svc.yaml   # entity -> service contract
./scripts/gen-service.sh                   # merge contract + *.ext.proto overlay
./scripts/gen-go.sh                         # Go: messages, stubs, ent schema, servers
./scripts/gen-ent.sh                        # the ent runtime for that schema
./scripts/gen-ui.sh                         # only if ui/ is in play
```

- The entity changed → all of them.
- Only an `*.ext.proto` changed → steps 2 and 3.
- `scripts/gen-go.sh` deletes and rewrites whole directories; do not stage
  hand-edits into them expecting to keep them.

Then always: `go build ./... && go vet ./... && go test ./...`

## Conventions that are easy to violate

These are decisions with reasons, written up in
[docs/DESIGN.md](../../../docs/DESIGN.md). Do not undo one without saying so.

- **Validation goes in Go, in the server, beside the rule.** No `buf.validate`
  in the messages, no validating interceptor.
- **`Patch` and `Apply` are not an API.** To let a caller change something, add
  the RPC that means it (`Rename`) in the `*.ext.proto` overlay and implement it
  with `Apply` in `server/core`. Do not open the general writes.
- **Anonymous is a caller, not the absence of one.** Every request has a frame.
  A new RPC is closed to anonymous callers until `gate.AnonymousReads` is
  widened, and that is the right way round.
- **`go_app.Server` is generated — do not add a method to it.** A capability a
  layer has is reached with `go_app.Find`. The one exception is `enttx.Binder`,
  and every layer must implement `WithDriver` (there is a `var _` assertion in
  each one that turns forgetting it into a compile error).
- **A hand-written read goes through `bare.<Entity>Narrow`**, not through the
  scope hook directly, or it will answer with softly-erased rows.
- **Filters are hand-written per entity, on purpose.** Do not try to generalise
  them.

## Running it

The bundled configuration is SQLite **in memory**, so nothing has to be brought
up:

```sh
go run . serve
```

With the UI and grpc-web:

```sh
GO_APP_SERVER_ALLOW_ANONYMOUS_READS=true \
GO_APP_SERVER_HTTP_ADDR=":8080" \
GO_APP_SERVER_HTTP_ALLOW_GRPC_WEB=true \
GO_APP_SERVER_HTTP_ORIGINS='["http://localhost:5173"]' \
go run . serve

cd ui && npm install && npm run dev
```

A migration needs PostgreSQL and a dev database it may empty; see
[docs/EXTENDING.md](../../../docs/EXTENDING.md).

## Branches

`main` is the base with no server. `kind/server` is this one — a service, with
no tenancy and nothing narrowing what a caller may see. `kind/server-x` is the
multi-tenant version, where rows belong to somebody and `bare.Scope` is
installed. Do not mix the two: if the task is about tenancy or a wall, say so and
ask which branch it belongs on.

An app made from the template has none of this — one branch, and `main` is it.
GitHub's "Use this template" copies the default branch only, so somebody who
pressed the button and finds no `server/` took `main` by accident. What they
want is `git reset --hard origin/kind/server` on the copy they were given, or a
`git clone --branch kind/server --single-branch` of the template; the README of
`main` says it in full.

## The tests

`internal/ox` gives a test a whole app on an in-memory database, served through
the same gRPC stack a real caller travels. Prefer it to unit-testing a layer in
isolation — the interceptors, the auth and the closing of the general writes are
part of what is being tested.

```go
t.Run("what it does", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
	beans := c.CreateRoaster(ctx, x, "beans")
	v := c.CreateCoffee(ctx, x, beans.Ref(), "ethiopia")
	...
}))
```

`c.As(ctx, "anna")` is somebody, `c.AsNobody(ctx)` is the anonymous caller, and
`c.Server.Gate`, `c.Server.Policy` and `c.Server.Events` are what the served
stack decides and publishes with — set them before making the client that should
meet them.
