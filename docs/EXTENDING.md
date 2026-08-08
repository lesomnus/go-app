# Adding things

How to replace the sample, how to add an entity, and how to add an RPC — with
what is generated for you and what is not.

All of it starts in `proto/`. **Nothing in `go_app/`, `internal/ent/` or
`server/bare/` is written by hand** — those are generated from the protos, and an
edit there is lost the next time anything is generated.

## The pipeline

Four commands, in this order. They are separate because each reads what the one
before wrote.

```sh
$ buf generate --template buf.gen.svc.yaml   # 1. entity  ->  service contract
$ ./scripts/gen-service.sh                   # 2. merge the contract with the overlay
$ ./scripts/gen-go.sh                        # 3. Go: messages, stubs, ent schema, servers
$ ./scripts/gen-ent.sh                       # 4. the ent runtime for that schema
```

`./scripts/gen-ui.sh` is a fifth, for the TypeScript half; it is only needed if
`ui/` is being used.

Run all four when the schema changed. Steps 2–3 are enough when only an
`*.ext.proto` changed, since the entity itself did not.

## Replacing the sample

`Roaster` and `Coffee` are a demonstration, and every one of them is meant to
go. This is the first thing a new app does, and it is a different job from
adding — most of the work is deciding what to keep.

Which way round depends on whether your app has the shape they demonstrate: **a
thing, and something it belongs to.** If it does, rename them and keep the
plumbing; if it does not, delete them and write one.

### If the shape fits: rename

Say the app is about `Post`s that belong to an `Author`.

```sh
# 1. The hand-written files whose *names* carry an entity.
$ git mv proto/go_app/roaster.proto             proto/go_app/author.proto
$ git mv proto/go_app/coffee.proto              proto/go_app/post.proto
$ git mv proto.svc/go_app/roaster_svc.ext.proto proto.svc/go_app/author_svc.ext.proto
$ git mv proto.svc/go_app/coffee_svc.ext.proto  proto.svc/go_app/post_svc.ext.proto
$ git mv server/core/roaster.go                 server/core/author.go
$ git mv server/core/coffee.go                  server/core/post.go
$ git mv server/watch/roaster.go                server/watch/author.go
$ git mv server/watch/coffee.go                 server/watch/post.go
$ git mv server/watch/roaster_test.go           server/watch/author_test.go
$ git mv server/watch/coffee_test.go            server/watch/post_test.go
$ git mv ui/src/useCoffees.ts                   ui/src/usePosts.ts

# 2. And the *generated* ones whose names carry an entity. These are the only
#    generated files you ever delete by hand: the pipeline rewrites a file per
#    entity but does not remove one for an entity that stopped existing, and a
#    left-behind `coffee_svc.proto` declares a `PostService` that is now
#    declared twice.
$ rm proto/go_app/{roaster,coffee}_svc.proto \
     proto.svc/go_app/{roaster,coffee}_svc.g.proto

# 3. The words, wherever they are written by hand.
$ grep -rl -iE 'roaster|coffee' proto proto.svc server internal cmd ui/src go-app.yaml \
  | grep -vE '^(go_app|internal/ent|server/bare|ui/src/gen)/' \
  | xargs sed -i \
      -e 's/Roaster/Author/g' -e 's/roaster/author/g' -e 's/ROASTER/AUTHOR/g' \
      -e 's/Coffee/Post/g'    -e 's/coffee/post/g'    -e 's/COFFEE/POST/g'

# 4. Generate. The rest of the generated tree cleans up after itself --
#    `gen-go.sh` deletes and rewrites whole directories.
$ buf generate --template buf.gen.svc.yaml && ./scripts/gen-service.sh
$ ./scripts/gen-go.sh && ./scripts/gen-ent.sh
$ go build ./... && go vet ./... && go test ./...

# 5. The TypeScript half, if you are keeping it.
$ ./scripts/gen-ui.sh && (cd ui && npm run build)
```

The plurals come out right on their own — `Roasters` is `Roaster` with an `s`
on it — so an entity whose plural is not its name plus an `s` wants a pass of
its own before step 3.

**The rename gives you the right names and the sample's meaning**, which is the
half that is left:

| where | what is still the sample's |
| --- | --- |
| `proto/go_app/*.proto` | the fields. `alias`, `name` and the soft-erasing `date_erased` are a catalogue's; yours are yours |
| `server/core/*.go` | the rules — the normalizing, what an `Add` completes, what `Erase` cascades to |
| `proto.svc/go_app/*_svc.ext.proto` | what `List` and `Watch` filter by |
| `server/watch/*.go` | the same filter again, in Go, for the stream |
| `internal/ox/client.go` | the `Create…` helpers a test arranges state with |
| `server/core/core_test.go` | tests about aliases and cascades |
| `ui/src/` | the page |

Everything else the `sed` touched is one line of a comment or one method name in
a test — `cmd/config/auth.go`, `go-app.yaml`, `server/frame`, `server/gate`,
`server/auth`, `internal/grpcx`. There is nothing to think about in those.

### If it does not fit: delete and write one

Keep one of the two as a model while you write the first of your own, and delete
it after — `server/core/coffee.go` and `server/watch/coffee.go` are the only
written-out examples of a hand-written `List` and a hand-written `Watch` there
are.

```sh
$ git rm proto/go_app/{roaster,coffee}.proto \
         proto/go_app/{roaster,coffee}_svc.proto \
         proto.svc/go_app/{roaster,coffee}_svc.{ext,g}.proto \
         server/core/{roaster,coffee}.go server/core/core_test.go \
         server/watch/{roaster,coffee}.go server/watch/{roaster,coffee}_test.go \
         ui/src/useCoffees.ts
```

Then write yours as in [Adding an entity](#adding-an-entity), run the pipeline,
and let the compiler find the rest. It is a short list, and it is the same list
every time:

| | |
| --- | --- |
| `server/core/patch.go` | **the one that is not a test.** It names the file descriptors it compiles patch documents against (`go_app.File_go_app_coffee_proto`), so it has to name yours |
| `internal/ox/client.go` | the `CreateRoaster` / `CreateCoffee` helpers a test arranges state with. Write the equivalent for yours |
| `server/gate/gate_test.go`, `server/watch/watch_test.go`, `server/gate/roles/roles_test.go` | these test the gate and the watch machinery and only *use* an entity as a fixture. Rewrite them onto yours rather than deleting them — what they cover is not the sample |

What the compiler will not find is a method name inside a string —
`"/go_app.CoffeeService/Get"` in `internal/grpcx/limit_test.go`, in
`server/gate/policy.go`'s documentation, in `go-app.yaml`'s commented example.
`grep -ri coffee` is the second pass, and there is nothing to think about in
what it turns up.

### Either way: replan the migrations

`migrations/` describes `roaster` and `coffee` tables that no database of yours
will ever have. **Nothing will tell you.** The bundled configuration is SQLite in
memory with `db.migrate` on, which puts the ent schema straight into the database
and never reads `migrations/` at all — so the app runs, the tests pass, and the
directory is wrong until the day something deploys on PostgreSQL.

While no database that matters has run them, throw them away and plan again:

```sh
$ rm -f migrations/*.sql migrations/atlas.sum
$ docker compose up -d db
$ go run . migrate plan \
    --dev "postgres://postgres:postgres@localhost:5432/dev?sslmode=disable" init
> written: 20260808085214_init.sql
```

Read it, then commit it. After the first deployment this stops being an option
and every change is another migration; see the README, "Starting over".

### What else you may not need

Each of these is self-contained, and deleting one costs nothing anywhere else:

| | delete it if |
| --- | --- |
| `ui/` | there is no browser. Take the `ui` job out of `.github/workflows/ci.yaml`, `ui/**` out of its `paths:`, `buf.gen.ui.yaml` and `scripts/gen-ui.sh` with them |
| `internal/httpx`'s grpc-web half | same, and six modules go with it — see the README, "The second listener" |
| `server/gate/roles` | your deployment gets its policy from somewhere else. Nothing wires it in |
| `server/watch` | nobody subscribes. It is a recorder and an interceptor, not a layer, so it comes out at `cmd/serve.go` |
| `cmd/greet.go` | it is there to prove the binary runs |

## Adding an entity

Write `proto/go_app/<name>.proto`, run the four commands, and it works. That is
the whole of the required part — a new entity compiles and serves CRUD with
**no hand-written Go at all**.

```proto
edition = "2023";

package go_app;

import "go_app/roaster.proto";
import "google/protobuf/timestamp.proto";
import "orm.proto";

option features.field_presence = IMPLICIT;
option go_package = "github.com/lesomnus/go-app";

message Grinder {
  bytes id = 1 [(orm.field) = {
    type: TYPE_UUID
    key: true
    default: ""
  }];

  // An edge. Immutable, which is what most edges are.
  Roaster roaster = 2 [(orm.edge) = {immutable: true}];

  // Unique within its Roaster, which is the `slug` index below.
  string alias = 3;
  string name = 4;

  // Erase softly, so the identifier stays taken. Leave it out to erase for
  // real; see DESIGN.md.
  google.protobuf.Timestamp date_erased = 14 [(orm.field) = {erased: {}}];

  google.protobuf.Timestamp date_created = 15 [(orm.field) = {
    immutable: true
    default: ""
  }];

  option (orm.message) = {
    rpc: {crud: true}
    indexes: [{
      name: "slug"
      refs: [
        {name: "alias" number: 3},
        {name: "roaster" number: 2}
      ]
      unique: true
    }]
  };
}
```

### What you get without writing anything

- `Add`, `Get`, `Patch`, `Apply`, `Erase`, on `go_app.GrinderService`
- `GrinderRef`, `GrinderSelect`, `GrinderPick`, `GrinderById`, `GrinderBySlug`,
  and the key resolution an edge to it needs
- the ent schema, the runtime, and the proto ↔ ent conversions
- a server on `server/bare`, on `go_app.Server`, registered by `RegisterServer`
- the soft-delete predicate in every read (`GrinderNarrow`)
- **watch events**, since the recorder is on the innermost server: a write shows
  up as a `bare.Change` whose `By` is `/go_app.GrinderService/Add`

### What you write by hand, if you want it

| | where | why it is not generated |
| --- | --- | --- |
| rules — validation, normalization, what an `Add` completes | `server/core/<name>.go` | it is the domain |
| `List` | `proto.svc/go_app/<name>_svc.ext.proto` + `server/core` | filtering is the domain and there is no general answer; the paging is borrowed from `runtime/entpage` |
| `Watch` | the same ext.proto + `server/watch/<name>.go` | the filter is per entity, and so is the message |
| a migration | `migrate plan <name>` | see below |

A layer that has nothing to say about an entity says nothing: `go_app.Overlay`
forwards it. Only write `server/core/<name>.go` when there is a rule.

### The migration

The schema changed, so the database has to. This needs PostgreSQL — it is what
the migrations are written for — and a **dev database it is allowed to empty**:

```sh
$ docker compose up -d db
$ GO_APP_DB_DRIVER=pgx \
  GO_APP_DB_DSN="postgres://postgres:postgres@127.0.0.1:5432/go-app?sslmode=disable" \
  GO_APP_DB_DEV_DSN="postgres://postgres:postgres@127.0.0.1:5432/dev?sslmode=disable" \
  go run . migrate plan add_grinder
```

**Read what it wrote** before committing it, and commit it with the schema
change. The bundled configuration is SQLite in memory and applies the ent schema
directly (`db.migrate`), so a demo needs none of this — a deployment that keeps
its data does.

### The UI

```sh
$ ./scripts/gen-ui.sh
```

The same protos, as TypeScript. There is no hand-written client and no second
schema.

## Adding an RPC

The one to write is the RPC that means something — `Rename`, `Publish`,
`Deactivate` — and to implement it with `Apply`. **That is what `Apply` is for**;
see DESIGN.md on why the general write is not an API.

### 1. Say it, in the overlay

`proto.svc/go_app/coffee_svc.ext.proto`. The generated contract
(`*_svc.g.proto`) is not the place — it is regenerated.

```proto
service CoffeeService {
  rpc Rename(CoffeeRenameRequest) returns (Coffee);
}

message CoffeeRenameRequest {
  CoffeeRef ref  = 1;
  string    name = 2;
}
```

```sh
$ ./scripts/gen-service.sh && ./scripts/gen-go.sh
```

### 2. Implement it, in `server/core`

```go
// server/core/coffee.go
func (s CoffeeServiceServer) Rename(ctx context.Context, req *go_app.CoffeeRenameRequest) (*go_app.Coffee, error) {
	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return nil, status.Error(codes.InvalidArgument, "name: must not be empty")
	}

	return s.CoffeeServiceServer.Apply(ctx, go_app.CoffeeApplyRequest_builder{
		Ref: req.GetRef(),
		Patch: patch.MustNew("go_app.Coffee",
			patch.Target(patch.Name("name")).Assign(patch.Str(name)),
		),
	}.Build())
}
```

Four things follow, and they are the reason to do it this way:

- **The validation has somewhere to live.** `Rename` is a function, so what it
  will and will not take is said in it, beside the rest of what renaming means.
- **What is published says `Rename`.** The write reports itself as `Apply`, and
  the event carries the method gRPC dispatched, so a watcher is told the thing
  the caller asked for rather than the leg of it that wrote.
- **`Apply` still works from in here.** The closing is a transport rule
  (`internal/grpcx.Closed`) and not a layer, so everything behind it goes on
  calling the general writes normally.
- **It is closed to anonymous callers**, because `gate.AnonymousReads` names
  `Get`, `List` and `Watch` and nothing else. That is the right way round: a
  write you just wrote is not open until you open it.

### 3. If anonymous callers should reach it

`gate.AnonymousReads` is a `func(method string) bool`. Write your own and pass it
where `cmd/serve.go` builds the gate:

```go
opts = append(opts, gate.Interceptor(gate.WithAnonymous(func(m string) bool {
	return gate.AnonymousReads(m) || m == go_app.CoffeeService_Search_FullMethodName
}))...)
```

## Adding a `List`

`List` is not CRUD, so nothing generates it. What is worth copying from
`server/core/coffee.go` is not the filtering — that is the domain and is meant to
be rewritten — but the two thirds that are not:

- **the narrowing**, through `bare.CoffeeNarrow` rather than by reaching for the
  scope hook: a read of a soft-erasing entity means the scope *and* leaving out
  the erased ones, and a list that asked the hook alone would quietly answer with
  the erased ones
- **the paging**, from `runtime/entpage`: a keyset cursor, an order that ends in
  the key, a capped size, and one row more than the page read so that a full last
  page answers with no cursor

## Adding a `Watch`

Copy `server/watch/coffee.go`. The shared half — subscribe, snapshot, coalesce,
the keys already sent — is `stream()`, and what is per entity is the filter
matching and the message.

The order is the part that must not drift, and it is why `stream()` exists:
**subscribe before reading anything.** A stream that read first would lose
whatever changed in between with nothing to say it did.

Note that the filter ends up written twice — as SQL for the snapshot and in Go
for the stream — which is inherent and is why there is a test that puts the same
filter through both roads.

## Things that will bite

- **Do not edit generated files.** `go_app/`, `internal/ent/`, `server/bare/`,
  `proto/go_app/*_svc.proto`, `proto.svc/go_app/*_svc.g.proto`, `ui/src/gen/`.
  Write in `proto/go_app/<name>.proto`, in `*_svc.ext.proto`, or in `server/`.
- **`go_app.Server` is generated**, so do not add a method to it. A capability a
  layer has is reached with `go_app.Find`; see the README. The one exception is
  `enttx.Binder`, and it is one because a rebind that *skips* a layer leaves it
  out of the stack.
- **Every layer must implement `WithDriver`.** Four lines, and a
  `var _ enttx.Binder[go_app.Server] = Server{}` in each layer turns forgetting
  it into a compile error rather than a surprise at the first transaction.
- **`init.sh` regenerates for a reason.** A compiled protobuf descriptor is a
  length-prefixed byte string with the package name inside it, so renaming
  without regenerating leaves one that compiles and panics on the first init.
- **Adding an entity adds a method to `bare.Scope`.** Nothing in this branch
  implements it, so nothing breaks — but an app that installs a scope gets
  `bare.Unscoped`'s "no opinion", which means every row. Test it.
