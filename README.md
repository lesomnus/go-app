# go-app

My flavor of "Hello, World!" for Go app.

## Quick Start

```sh
# Rename the template to your own: the module path, the proto package and its
# directories, the binary, the config file and the `GO_APP_` environment prefix.
# It then generates everything that is generated, again -- see below for why
# that is not optional.
$ ./scripts/init.sh github.com/your-name/your-app

# Build and test the app.
# Build results will be placed in the `/dist` directory.
$ docker buildx bake build test

# Load apps into the local Docker engine.
$ docker buildx bake app --load
$ docker run --rm ghcr.io/lesomnus/go-app:local greet
> |........| 19:08:03.037 ○ 000000 000000 use default config
> Hello, hypnos!
```

`init.sh` regenerates rather than leaving the generated files it just rewrote,
and that is load-bearing: a compiled protobuf descriptor is a length-prefixed
byte string with the proto package name inside it, so replacing `go_app` with a
name of a different length leaves one whose prefixes say the old lengths. It
compiles, and panics on the first init with a slice bounds error a long way from
anything anybody wrote. `--no-generate` skips it for somebody who has to run the
generation elsewhere, and says so.

One thing it cannot do for you: the app name becomes the alias of the message
package (`go_app` here), so a local variable of that name shadows it. Pick a
name and `go build ./...` will say if you picked one this repository uses.

## The UI

`ui/` is a Vite + React page that talks to the server over **grpc-web**, and it
is here to be the other end of the contract rather than a design: it lists
Roasters, watches Coffees, and shows what the server says when it refuses.

```sh
$ cd ui && npm install
$ ./scripts/gen-ui.sh                 # the same protos, as TypeScript
$ cd ui && npm run dev                # http://localhost:5173
```

It needs the second listener, since a browser cannot speak the transport gRPC
brings:

```sh
$ GO_APP_SERVER_HTTP_ADDR=:8080 \
  GO_APP_SERVER_HTTP_ALLOW_GRPC_WEB=true \
  GO_APP_SERVER_HTTP_ORIGINS='["http://localhost:5173"]' \
  GO_APP_SERVER_ALLOW_ANONYMOUS_READS=true \
  go run . serve
```

Nothing else has to be running: the bundled configuration is SQLite **in
memory**, so there is no database to bring up and no migration to apply.

Three things it exists to show:

- **One contract, two languages.** `scripts/gen-ui.sh` runs the same `proto/`
  through `protoc-gen-es`, so the page and the server are wrong together or not
  at all. There is no hand-written client and no second schema.
- **`Watch` is what the list *is*.** The page does not call `List` and then
  subscribe: it opens the stream, and the first message is everything that
  matches. What arrives is the row as it is now, so the client keeps the last
  thing it was told about each id and replaces it — `src/useCoffees.ts` is the
  whole of that, and it is short because the payload is state and not a delta.
  A removal is an item with **no value**.
- **Who is calling is a header.** The subject box at the top becomes
  `authorization: Plain <subject>`, which the server believes — it is the
  development handler and the server warns about it at startup. Empty is the
  anonymous caller, and the page then gets `Unauthenticated` on writes and
  answers on reads. A real deployment sends a token instead and nothing else
  about `src/client.ts` changes.

The Vite template's own files are gone — no logo, no counter, no CSS nobody
asked for. What is left is `index.html`, `main.tsx`, `App.tsx`, the client, the
hook, and the stylesheet.

## Configuration

The app reads `go-app.yaml`, or the file `--config` names. What it says can be
overruled, and the last word wins:

```
built-in defaults  <  the file  <  the environment  <  the flags
```

### From the environment

Every field answers to a variable named after the path to it: `format` of
`greet` is `GO_APP_GREET_FORMAT`, and so on for however deep the field sits.
`config` prints what the app ended up with, which is how to check.

```sh
$ GO_APP_GREET_FORMAT='Hi, %s.' go run . greet hypnos
> Hi, hypnos.
```

A name that starts with `GO_APP_` and answers to nothing is reported rather
than ignored in silence, since that is what a typo looks like:

```
$ GO_APP_GREET_FORMATT=x go run . greet hypnos
> ! no configuration is read from these, so they were ignored - env=[GO_APP_GREET_FORMATT]
```

The names are made from the configuration itself, so a field that is added
answers to one without anything being written here or anywhere else.

### From the environment, inside the file

A value in the file can name a variable instead of holding one, the way the
OpenTelemetry Collector spells it. This is for the parts of a value that must
not be written down, such as the password inside a connection string.

```yaml
greet:
  format: "${env:GREETING:-Hello}, %s!"
```

Without `:-default` the variable is required: one that is not set is an error
rather than an empty string, so a missing secret is noticed at startup instead
of at the first request. Write `$$` for a literal dollar sign.

This happens to the file, not to the configuration, so **a comment is expanded
too**: a commented-out `${env:TOKEN}` left there as an example still stops the
server from starting until `TOKEN` is set. Write the example without the
syntax, or give it a default.

## Code generation

Entities are declared once as [protobuf-orm](https://github.com/protobuf-orm/protobuf-orm)
messages in `proto/`, and everything else is generated from them: the service
contracts, the Go messages, the [ent](https://entgo.io) schema and runtime, and
the gRPC servers that run CRUD against the database.

Each entity gets `Add`, `Get`, `Patch`, `Apply` and `Erase`. The first three
carry the entity's own fields; `Apply` carries a
[patch document](https://github.com/lesomnus/protobuf-patch) instead, which is
how the edits a fixed request shape cannot express are said - change one map
entry rather than replace the map, or assert a value before writing it. That
document schema is not frozen, so `buf.build/patch/patch` is pinned by
`buf.lock` rather than tracked.

```sh
# 1. Service contracts:
#      proto/<pkg>/*.proto  ->  proto.svc/<pkg>/*_svc.g.proto
$ buf generate --template buf.gen.svc.yaml

# 2. Merge each generated contract with its hand written overlay (*.ext.proto):
#      proto.svc/<pkg>/*_svc.g.proto  ->  proto/<pkg>/*_svc.proto
$ ./scripts/gen-service.sh

# 3. Go messages, gRPC stubs, query helpers, ent schema and ent backed servers:
#      go_app/           messages, gRPC stubs, query helpers, store wiring,
#                        and the stack helpers every server implementation uses
#      internal/ent/     schema/, the proto <-> ent conversions, and orm.g.go
#      server/bare/      the service servers, backed by an ent client
$ ./scripts/gen-go.sh

# 4. Ent runtime for the generated schema:
#      internal/ent/**
$ ./scripts/gen-ent.sh
```

Step 3 generates into a staging directory (`.gen`) first because the plugins
emit paths relative to the `go_package` of each proto file, which is the module
root. `scripts/gen-go.sh` then moves the message package into `go_app/` and
rewrites the imports of the module root accordingly; override `PKG_DIR` and
`PKG_NAME` to place it somewhere else.

The ent package must remain below the message package, since that is how the
generated servers spell its import path (`ent.namer` in `buf.gen.yaml`).

### What a layer does when nobody asked

A request is not the only reason a server does something: rows have to be swept,
caches warmed, leases renewed. `server/spin` is where a layer says so.

```go
func (s Server) Spin(ctx context.Context) error {
	return spin.Every(time.Hour, func(ctx context.Context) error {
		// through this layer's own servers, so its rules apply
		return sweep(ctx, s.Next())
	})(ctx)
}
```

`spin.All(ctx, s)` walks the stack and starts whatever answers to
`spin.Spinner`, so **a layer with no background work writes not one line**. That
is the ordinary `Find` answer rather than the `enttx.Binder` exception: starting
a layer that has nothing to start is nothing, and skipping one loses nothing —
where a rebind that skipped a layer would leave it out of the stack.

A loop that returns an error is logged and started again after `spin.Retry`; one
that returns nil has finished. Neither takes the process down, which is the
conservative half of a real trade — a sweep that failed once because the
database blinked should not stop the server, and one that has been failing for
an hour is a thing nobody notices. A deployment that would rather fall over says
so by having its loop do it.

**Nothing in this app spins.** The obvious candidate — sweeping Coffees that
were erased long enough ago — is a retention policy, and picking a number is the
deployment's, not the template's.

## Reading further

| | |
| --- | --- |
| [docs/EXTENDING.md](docs/EXTENDING.md) | how to add an entity and how to add an RPC — start here |
| [docs/DESIGN.md](docs/DESIGN.md) | why the parts that look odd are that way, and what it would cost to change them |
| [docs/AUTH.md](docs/AUTH.md) | who is calling, and what they may do |

## Servers

A server is a `go_app.Server`, which is no more than a set of the service
servers generated from the protobuf definitions, and implementations are
stacked on top of each other:

- `server/bare` is generated by `protoc-gen-orm-ent` and runs the queries
  against the database. It is always the innermost one.
- `server/core` holds the rules that apply wherever the app runs. It validates
  and completes the requests it cares about and hands them over to the next
  server. It is also where a service that is not CRUD, and so is not generated,
  is written by hand; `CoffeeService.List` is the example. `Server.Db()` reaches
  the client of the generated server behind it, so a middleware does not have
  to carry a database of its own.
- `server/watch` is the other end of the same hook: it publishes what a call
  changed, once the call has answered. It is not a layer at all. See
  [Telling somebody what changed](#telling-somebody-what-changed).
- `server/gate` says what the caller of a request may do with it. It is the
  outermost one, and it overrides nothing: what it decides is decided once, in
  an interceptor, before the handler. See [docs/AUTH.md](docs/AUTH.md).

What they share is not written here: `go_app` is generated with `Overlay` to
implement only the services of interest, `Build` to stack them, and `Iter`,
`Find` and `SinkOf` to look into a stack that was built. All of it is expressed
in terms of the generated `go_app.Server`, so an app that wrote it by hand would
write the same file.

**A capability is found, not declared.** Whatever a layer can do besides
answering a service — hold the database, hold a connection, narrow a query — is
reached with `Find` rather than by adding a method to `go_app.Server`. That is
what keeps `Server` the generated set it is: extend it and every layer, every
`Overlay` and every helper above has to be rewritten to match.
`core.Server.Db()` and `core.Server.Scope()` are the examples, and `Find` takes
any type so that a one-method interface naming just what a caller needs is as
good a question as a layer's own type.

**Except one, and knowingly.** Every layer implements
[`enttx.Binder`](https://github.com/protobuf-orm/protoc-gen-orm-ent/tree/main/runtime/enttx)
— four lines that rebind what is behind it and remake itself — so that a caller
can put the whole stack on one transaction. `Find` is the wrong tool here and
the difference matters: it walks *past* a layer that does not answer, and that
layer would then be missing from the rebuilt stack, so requests inside the
transaction would go around it. Reading a stack skips; rewriting one may not.
A `var _ enttx.Binder[go_app.Server] = Server{}` in each layer turns forgetting
it into a compile error.

```go
// server/watch/server.go
type Server struct {
	go_app.Overlay
}

func (s Server) Coffee() go_app.CoffeeServiceServer { ... }

// The innermost server writes SQL of its own for Apply, so it asks the client
// which SQL that is; a dialect nothing was written for is refused here rather
// than at the first Apply. The client knows because protoc-gen-orm-ent puts a
// `Dialect()` on it (`internal/ent/orm.g.go`) -- ent keeps its driver to
// itself, and a caller made to repeat what it said when it opened the
// connection can say something else the second time.
sink, err := bare.NewServer(db, bare.WithRecorder(watch.New(events).Recorder()))

// Stack it in front of the others; the last one handles the request first.
// Building fails if a server cannot make itself out of what it was given.
s, err := go_app.Build(sink, core.Build(), wat.Build(), gate.Build())
```

A middleware that has something to say about `Patch` usually has the same thing
to say about `Apply`, and saying it once does not cover both: one reads a field
of the request, the other reads a document. `server/core` reads what a document
writes by compiling it against the schema (`server/core/patch.go`).

### The general write is not an API

`Patch` and `Apply` are how the servers write. They are **not** how a caller
asks, and a deployment does not serve them: `server.allow_general_writes` is off
unless it is written down, and they answer `Unimplemented` to everybody.

Between them they can write anything the schema holds — `Patch` takes a field
per property, `Apply` takes a document that can address one map entry or assert
a value before writing it. That is what makes them useful to a server and wrong
as an API. What a caller may change, and under what conditions, is not something
a general write can be told: there is no request field for "may this Coffee be
renamed right now", because there is no request — there is a bag of fields, or a
document.

So an app writes the RPC it means, and implements it with the general write:

```proto
// proto.svc/go_app/coffee_svc.ext.proto
service CoffeeService {
  rpc Rename(CoffeeRenameRequest) returns (Coffee);
}

message CoffeeRenameRequest {
  CoffeeRef ref  = 1;
  string    name = 2;
}
```

```go
// server/core/coffee.go
func (s CoffeeServiceServer) Rename(ctx context.Context, req *go_app.CoffeeRenameRequest) (*go_app.Coffee, error) {
    // What a rename will and will not take, said here because here is what
    // knows -- and whatever renaming means besides writing the column.
    name, err := ParseName(req.GetName())
    if err != nil {
        return nil, err
    }

    return s.CoffeeServiceServer.Apply(ctx, go_app.CoffeeApplyRequest_builder{
        Ref:   req.GetRef(),
        Patch: patch.MustNew("go_app.Coffee",
            patch.Target(patch.Name("name")).Assign(patch.Str(name)),
        ),
    }.Build())
}
```

Three things come out of that shape:

- **The validation has somewhere to live.** `Rename` is a function, so what it
  will and will not take is said in it, beside the rest of what renaming means.
  See [What a request must say](#what-a-request-must-say).
- **What is published says `Rename`.** The write reports itself as `Apply`, and
  the event carries the method gRPC dispatched — so a watcher is told `Rename`
  rather than the leg of it that wrote. See
  [Telling somebody what changed](#telling-somebody-what-changed).
- **`Rename` still works.** The closing is a transport rule
  (`internal/grpcx.Closed`) and not a layer of the stack, so everything behind
  it goes on calling `Apply` normally. Closing them in a server would have
  closed them to the servers.

The tests of this repository are the exception, and knowingly: `internal/ox`
serves the general writes, because they are what this repository has to
demonstrate. An app made from this template tests the RPCs it wrote instead.

### Erasing a Coffee, and erasing a Roaster

A **Coffee is erased softly**. `Erase` stamps `date_erased` and the row stays;
every read is narrowed by that column, so the Coffee is gone from `Get`, from
`List`, from `Patch` and `Apply`. There is no `Restore`, and that is the point:
this is not a recycle bin.

The reason is the **identifier**. A row that is gone for real leaves its
identifier free for something else one day, and anything that kept one — a link
somebody saved, a row in another system, an event that was published — would
then point at a coffee nobody meant. Stamping keeps it taken for good.

A **Roaster is erased for real**, and it takes its Coffees with it — including
the ones that were stamped, in one transaction. That is not a policy about
deletion; it is what erasing a Roaster already meant, and it is now something
`core` has to *say*, because **soft deletion does not cascade**: a stamped
Coffee keeps its row, and the row keeps a foreign key to its Roaster, so without
the cascade a Roaster that ever had a Coffee could never be erased at all.

Two things fall out of the schema rather than out of any code:

- **The name comes free again.** The unique index on the slug covers only the
  rows that are still there (`WHERE date_erased IS NULL`), so the alias an
  erased Coffee held can be used again — an identifier is forever and a name is
  not. `includes_erased` is how the other choice is made.
- **Either spelling of `unique` frees its value.** `Roaster.alias` is `unique`
  on the field and `Coffee`'s slug is an `indexes` entry; for a soft-erasing
  entity the generator promotes the field to an index of its own, so the two
  spellings mean the same thing.

**Partial indexes are SQLite and PostgreSQL only.** MySQL has none, and ent
writes the annotation out for it rather than refusing — so the index would come
up covering every row and a freed name would stay taken with nothing to say so.
The generated `NewServer` refuses a dialect this backend writes no SQL for, and
that set is exactly the two that have partial indexes.

### Reading a list

Nothing generates a `List`. What a list filters by is the app's, and there is no
general answer to it — `CoffeeService.List` filters by a `Ref` because that is
the plainest thing that works, and it **is meant to be rewritten**.

The paging is not like that. It looks the same for every entity and it is the
half that is easy to get wrong, so it is borrowed from
[`runtime/entpage`](https://github.com/protobuf-orm/protoc-gen-orm-ent/tree/main/runtime/entpage)
rather than written out:

```go
vs, _ := c.Coffee().List(ctx, go_app.CoffeeListRequest_builder{Size: z.Ptr(int32(20))}.Build())
for vs.GetNext() != "" {
    vs, _ = c.Coffee().List(ctx, go_app.CoffeeListRequest_builder{
        Size:  z.Ptr(int32(20)),
        After: z.Ptr(vs.GetNext()),
    }.Build())
}
```

- **A cursor, not an offset.** `after` names the last row of the page before, so
  a row added while a caller is reading does not shift the page under them and
  nothing is seen twice or missed. An offset counts rows from the start every
  time, which is both wrong under writes and slower the further in you read.
- **The order ends in the key.** Two rows equal in every column of the order are
  two rows a cursor cannot tell apart, and rows written by one request are
  stamped a moment apart at best. A key as the last column is what a tiebreaker
  means.
- **The size is capped.** Nothing said is `PageSize`; more than `PageLimit` is
  `PageLimit`, and not an error — a caller asking for more than there is meant
  no harm.
- **One row more than the page is read**, which is how "is there another page"
  is answered without a second query and without a count. A full last page
  answers with no cursor rather than sending the caller back for an empty one.

A cursor is opaque and it is not secret: a caller who takes one apart asks for
rows starting somewhere else, which is a question they could have asked anyway
and which the same read answers.

### What a request must say

**In Go, in the server, beside the rule it is part of.** There are no
constraints in the messages — no `buf.validate`, no validating interceptor. A
request is checked by the code that is about to act on it.

```go
// server/core/coffee.go
if len(fs) > FilterLimit {
    return nil, status.Errorf(codes.InvalidArgument,
        "filters: %d of them, and %d is the most one list carries", len(fs), FilterLimit)
}
```

The reason is that almost none of the interesting checks are declarable, and
mixing the two makes it harder to know where to look:

- **A rule usually has a reason, and the reason lives in the server.**
  `FilterLimit` is not "32 because 32". Each filter is a predicate in the same
  query, so the request is what says how much of the database to read — which
  is a sentence about `List`, next to `List`, where the `PageLimit` it argues
  against also lives. As `max_items: 32` on a field it is a number with the
  reason left somewhere else.
- **Refusing and clamping are both answers.** A page size past the cap is
  clamped, because a caller asking for more rows than there are meant no harm.
  A filter count past the cap is refused, because dropping half the filters
  would answer a question nobody asked. A declaration has only the one verb.
- **Most of it is already covered by the code that reads the value.**
  `CoffeePick` refuses a reference that names nothing and says which part was
  wrong; `uuid.FromBytes` refuses sixteen bytes that are not sixteen bytes.
  Declaring `required` and `len: 16` over the top would be a second copy of
  the same rule, drifting.
- **And the rest was never declarable.** Normalizing a value on the way in
  (`" Acme "` → `acme`), a rule about two fields at once, an alias being free —
  `server/core` is full of these, and it is where a reader already goes.

The one thing given up is that a check has to be written rather than inherited.
That is the same trade the [general write](#the-general-write-is-not-an-api)
makes and for the same reason: an app that wants a caller to be able to do
something writes the RPC that means it, and an RPC somebody wrote is one where
there is already a place to say what it will and will not take.

## Who is calling

Every request is from somebody, and **that includes the ones that are from
nobody**. A caller who presents no credential is served as `frame.Anonymous`,
which is a caller like any other rather than the absence of one, so there is no
such thing here as a request with no frame. The whole chain — from the header a
caller sent to what they may do with it — is written up in
**[docs/AUTH.md](docs/AUTH.md)**.

The short of it:

| | | |
| --- | --- | --- |
| **Who is this?** | `server/auth` | A `Handler` reads a credential and says who it is. `plain`, `mtls` and `bearer` differ in one thing: where the name comes from. Nothing is looked up in this app's database — **it has no users**, so who somebody is comes from outside. |
| **...and what did the credential allow?** | `frame.Grant` | A token may allow *less* than the subject it names — a set of methods. Never more. |
| **What may they do?** | `server/gate` | Decided once, in an interceptor, before the handler. |

**`auth.TokenStore` is the seam an issuer is injected at.** A header and a
certificate carry a name; a token carries nothing, so somebody has to be asked,
and that somebody is a JWT this app verifies, an introspection endpoint, or a
table somebody else owns. What is bundled here is a map, and it is a sample of
the shape.

`server/gate` holds **one rule of its own**:

> an anonymous caller may make the calls that were named, and no others.

`server.allow_anonymous_reads` names the reads this app generates — `Get`,
`List`, `Watch` — which is the catalogue shape: anybody may read it, and only a
caller who said who they are may change it. It is a list of what is *allowed*
and not a list of what is not, and the difference is the day somebody writes
`Rename`: that is a write, it is not spelled `Patch`, and the other way round
would have opened it to everybody with nothing anywhere to say so.

Everything finer is a deployment's, and `gate.Policy` is where it is injected —
unset by default, and `server/gate/roles` is a reference implementation that
nothing wires in. This app is a resource server: it reads credentials and
enforces what it is told, and it does not define roles or decide who holds them.

**There is no wall here.** Nothing narrows what a caller may *see* — every row
is everybody's to read. An app whose rows belong to somebody says so as a
predicate in the query rather than as a rule per RPC, which is what `bare.Scope`
is the hook for; this branch installs none. See `kind/server-x`, whose whole
subject is that.

## Telling somebody what changed

`server/watch` hangs off `bare.Recorder`, the hook the generated servers call
from **inside** the transaction that makes a write — and it wants the opposite of
what that moment offers. An event has to be published only if the write
**survived**, so nothing is published there at all: the recorder remembers, and
an interceptor publishes once the handler has answered without an error, by which
time every transaction the call opened is committed or gone with it.

```go
events := watch.Signal()
wat := watch.New(events)

sink, err := bare.NewServer(db, bare.WithRecorder(rec), bare.WithRecorder(wat.Recorder()))
opts = append(opts, wat.Interceptor()...)

c, stop := events.Subscribe(64)   // whoever is listening
```

One event per call that changed something, carrying who, the RPC they asked for,
the request, the response, and every write the call made — which is more than the
response says, since erasing a Roaster takes its Coffees with it. A read
publishes nothing.

It is a [hard signal](https://github.com/lesomnus/signals): a subscriber that is
not keeping up has its channel **closed**. That is the only one of the three
choices that is safe here — waiting on the slowest watcher turns a slow consumer
into a slow server, and skipping an event in silence leaves a watcher believing
it has seen everything. Being cut off is something a client can notice and act
on.

**It is not an outbox**, and the difference matters before anything is built on
it: an event is published in this process, to whoever is listening in this
process, and a crash between the commit and the dispatch loses it. Something that
must not be lost is written in the transaction, as a row somebody else picks up.

#### `Watch` — state, not deltas

What a client gets is **the row as it is now**, never a description of what
changed. That is what makes a stream that missed something still correct: the
next item about a Coffee carries the whole of it, so a client converges instead
of replaying. It is why nothing here keeps a version, a cursor or a backlog.

```
subscribe                    first, so nothing that happens while reading is lost
List(filters)                the snapshot, page by page
per event:  Get(id)          the row as it is now, and the filters tested on it
```

Three things fall out of that order:

- **A client does not List and then Watch and race the two.** Subscribing first
  means the only thing that can go wrong is a Coffee arriving twice — in the
  snapshot and again as a change — and a duplicate is harmless when the payload
  is state.
- **Whatever narrows a read stays in one place.** The row is read back *through
  the servers behind this one*, with the context of the caller who asked, so it
  is narrowed by the same predicate as every other read — today the soft delete,
  and whatever else an app installs in `bare.Scope`. Nothing in `server/watch`
  knows what any of it means. The filters are the caller's own and are tested in
  Go, which is a different kind of thing: nothing that must not be got wrong
  depends on them.
- **A removal is said by absence.** `CoffeeWatchItem.value` is unset when the
  Coffee is no longer one this caller may see, and there is deliberately no way
  to tell "erased" from "no longer visible" — a stream that distinguished them
  would be reporting rows that stopped being readable. It is only ever said
  about a Coffee the stream has already carried.

`action` is the RPC the caller of *that* change asked for, so an RPC written by
hand is here under its own name; what it means is that RPC's documentation to
say. `Change.By` — the write it actually became — is deliberately **not** on the
wire, since publishing it would tell callers about `Patch` and `Apply`, the two
RPCs this app does not serve them.

A stream that falls more than `watch.Backlog` behind is cut off with
`ResourceExhausted`. Asking again is the recovery: a fresh stream begins with
everything that matches now.

`RoasterService.Watch` is the same in every way.

## Testing

`internal/ox` gives a test a whole app: an empty SQLite database in memory, the
server stack of `server/core`, and a client connected over an in-memory
listener, so the test travels the same gRPC stack the app is served with
without opening a port or leaving anything behind.

```go
func TestRoasterAdd(t *testing.T) {
	t.Run("alias is normalized", ox.T(func(ctx context.Context, x *ox.X, c *ox.Client) {
		v, err := c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{Alias: " Beans "}.Build())
		x.NoError(err)
		x.Equal("beans", v.GetAlias())

		// `x` asserts like `require` does, and knows about gRPC.
		_, err = c.Roaster().Add(ctx, go_app.RoasterAddRequest_builder{Alias: "beans"}.Build())
		x.ErrCode(codes.AlreadyExists, err)
	}))
}
```

A test is served as somebody — `c.As(ctx, "anna")` is anybody in particular, and
`c.AsNobody(ctx)` is the anonymous caller, which is a caller like any other.
`c.AsBearer(ctx, token)` travels as a credential that allows less than its
subject does. All of them go through the same authentication the app is served
with, so what a test travels is what a caller travels.

`c.Bare()` is a second client, of the innermost server, that skips the rules of
the servers in front of it; it is how a test arranges a state the app itself
would refuse to create.

`c.Server.Gate` and `c.Server.Policy` are what `server/gate` decides with, and
`c.Server.Events` is what a watcher would be listening to. Set them before
making the client that should meet them; see `server/gate/gate_test.go`.

The test is served with the same options the app is (`internal/grpcx`), and
whatever the server logs is attached to the test that ran it, so a recovered
panic is shown with its stack when the test fails. It gets there through
`grpcx.Seed`, which is a stats handler and not an interceptor for a reason worth
knowing about: the record of a call is written by a stats handler too, and a
stats handler never sees what an interceptor put in the context.

## Serve

`serve` opens the database and registers every generated service on the gRPC
server.

It talks to SQLite through [go-sqlite3](https://github.com/ncruces/go-sqlite3)
by default, and to a database held **in memory** — so it runs without anything
else around and leaves nothing behind:

```sh
$ go run . serve
> |........| 19:08:03.037 ○ 000000 000000 serving grpc - addr=[::]:50051 tls=false auth=[plain]
> |........| 19:08:07.451 ○ 98de37 73b3a4 ›» 127.0.0.1......... 0008B go_app.RoasterService/Get
> |........| 19:08:07.455 ○ 98de37 73b3a4 «‹ .OK 001.18ms 0008B 0081B go_app.RoasterService/Get
```

`db.migrate` runs ent's auto migration on startup, and an in-memory database
has nowhere to have had migrations applied to it, so that is what puts the
schema there at all. A deployment that keeps its data says so in `db.dsn` — a
file, or PostgreSQL — and runs `migrate apply` instead; see
[Migration](#migration).

`db.max_open_conns` is 1 on SQLite, because SQLite takes one writer: a second
connection asking to write reports a busy database rather than waiting for the
first.

The compose file is the shape of a deployment: the database, then the
migrations, then the server. `migrate` runs from the image that is about to
serve and `app` waits for it to have finished, so a server never answers on a
database that is behind the code it ships.

```sh
$ docker buildx bake build && docker buildx bake app --load
$ docker compose up -d app
> ✔ Container workspace-db-1       Healthy
> ✔ Container workspace-migrate-1  Exited
> ✔ Container workspace-app-1      Started
```

The connection string is given to both by the environment rather than written
in `go-app.yaml`, since it holds a password; everything else stays in the file.

Every call goes through `internal/grpcx` before it reaches a service: it is
traced and measured with the providers the app was started with, it leaves a
record as it arrives and another as it is answered, a handler that panics is
reported as an internal error instead of taking the process down with it, and a
call that arrived without a deadline of its own is given one. A call that named
a deadline is left alone however far away it is — the caller said how long the
answer is worth waiting for — and what is capped is only the absence of that.
`grpcx.WithDeadline` says how long, from `server.timeout`; a negative one caps
nothing. Streams are not capped at all, since a stream is long-lived by design.

The records come from `otxgrpc` as a stats handler rather than from an
interceptor, and that is what puts them outside everything else: a call that
panicked, one that ran out of time and one that was refused before a handler was
reached are all recorded like any other, where an interceptor is only outside
the interceptors installed after it. The same handler puts the service and the
method on the logger the call is served with, so what a handler writes of its
own accord says which RPC it was under without being told. Health is left out
(`grpcx.Log`) — a readiness probe every few seconds, from every replica, is most
of what a log holds and none of what anybody reads.

### How much one caller may ask for

`server.limit.rate` is calls a second, and `server.limit.burst` is how many may
arrive at once before the rate is what is left. It is **off** unless a rate is
written down, which is a decision rather than an oversight: a deadline can be
defaulted because a wrong one costs a call, and a rate cannot — a number a
template picked is a number nobody measured, and what it refuses is real traffic
on the day the app is busiest.

**Counted against the subject** (`gate.BySubject`), and **every anonymous caller
against one bucket between them**. That last part is the whole of what this can
honestly do: an anonymous caller has nothing to be told apart by, since an
address is the load balancer's or a company's. So a limit here protects the app
from anonymous traffic *in total* and does nothing about one anonymous caller
among many — which is the layer in front's to do, where the addresses are real. The limit is
installed behind the authentication, since the key is about who is calling, and
in front of `gate.Interceptor`, since consulting a policy is work a caller past
their line should not be able to ask for.

A call over the line is `ResourceExhausted` and carries `RetryInfo` saying how
long to wait, because a refusal a client cannot time is a client that asks again
at once — which is the traffic the limit was for. It is not `PermissionDenied`:
nothing about what the caller may do has changed, and the same call a moment
later is served.

Three things it is worth knowing before choosing a number:

- **It counts in this process.** Three replicas are three buckets, so a rate of
  20 is 60 to whoever a load balancer spreads around, and which replica a call
  lands on is nobody's decision. That is fine for what a limit like this is
  usually for — keeping one caller from taking a whole process — and wrong for a
  quota somebody is billed against, which has to be counted somewhere every
  process can see. `grpcx.Limiter` is the seam for that, the way `gate.Policy`
  is the seam for authorization.
- **It is behind the authentication, so it does not protect it.** A flood of
  calls that never authenticate is refused by `server/auth`, after it has looked
  the credential up. Keying on an address instead would be either the load
  balancer's address, which is one bucket for everybody, or a NAT's, which is one
  bucket for a company — so that job belongs to the layer in front. What is here
  for connection-level abuse is `server.max_concurrent_streams` and the
  keepalive `min_time`, which hangs up on a client that pings too often.
- **A call is the unit.** A `List` of a thousand rows costs what a `Get` costs.
  Weighing them would mean a price a caller cannot see, on a cost that is only
  known after the call ran; what is here instead is a finer key — put the method
  in it and a `List` comes out of a bucket of its own.

### The second listener

`server.http.addr` starts a second listener, and nothing is served on it unless
that is written down. It is a port of its own rather than the same one, and that
is the whole design decision:

**gRPC keeps its own transport.** A `grpc.Server` can be served through
`net/http` — `ServeHTTP` exists, and one handler could route on the content type
and serve both protocols on one port. gRPC's own documentation says not to for
anything that matters: that road uses `net/http`'s HTTP/2 instead of the
transport gRPC brings, and it is slower and has less of what gRPC does. So the
fast path is untouched and everything that cannot speak it comes here.

| | |
| --- | --- |
| **grpc-web** | `server.http.allow_grpc_web` — gRPC reframed so a browser can send it: the trailers ride in the body. Translated and handed to the **same** `grpc.Server`, so a browser meets the same interceptors, the same authentication and the same rules. That translation does go the slow way, which is the right place for it: a browser is not the throughput. |
| `/healthz` | Out of the same `health.Server` the gRPC health service answers from, so the two probes can never disagree. `/healthz/liveness` is the other question. |
| **pprof** | `server.http.allow_pprof`, off unless asked. Note that importing `net/http/pprof` at all registers it on `http.DefaultServeMux` — which is why nothing in this app ever serves that mux, and why there is a test that says so. |
| your own | `httpx.Options.Mux` is where a deployment puts whatever else it serves. |

`server.http.origins` is who a browser may call from, and nothing written down is
nobody. It is **not** the wall and not authentication: what an origin check stops
is a page somebody else wrote making calls as whoever is reading it, and it stops
nothing else.

The translation is [improbable-eng/grpc-web](https://github.com/improbable-eng/grpc-web),
and it brings six modules with it — `rs/cors`, `cenkalti/backoff`,
`desertbit/timer`, `klauspost/compress` and `nhooyr.io/websocket`, the last of
which is archived. That is the price of not writing a wire protocol by hand, and
it is worth knowing: an app that has no browser deletes the grpc-web half of
`internal/httpx` and the dependency goes with it.

### Which health question is being asked

Health is answered under two names, because a liveness probe and a readiness
probe are not asking the same thing and the difference is what a wrong answer
costs.

| | asked by | says |
| --- | --- | --- |
| `""` | a load balancer, and anything that names nothing | the database is answering |
| `"liveness"` | a container runtime | the process is here |

Every deployment shares one database, so a database that blinks makes every
process answer the same way at the same moment. Answered as readiness that is a
few seconds of failed calls; answered as liveness it is every process killed at
once, restarted into a database that is still not there, and a restart loop
that outlives the outage that started it. So the database moves readiness and
nothing moves liveness, and both turn to `NOT_SERVING` as soon as the server
starts shutting down.

The empty name is the readiness one on purpose. It is what the health protocol
gives to the server as a whole and what a caller that was not told a name asks
about, so the answer it gets should be the one that is safe to get wrong.

```yaml
livenessProbe:  { grpc: { port: 50051, service: liveness } }
readinessProbe: { grpc: { port: 50051 } }
```

### Whose trace is it

A call that arrives with a trace context is taken to be part of that trace, so
a request that crosses several services reads as one thing. That context is the
caller's word, though, and a caller anyone can be is a caller that can:

- **name the trace.** Two unrelated requests can be made to look like one, and
  a trace of yours can be joined from outside.
- **decide that it is sampled**, if the sampler honours the parent. The bundled
  one does not: `always_on` records everything regardless. Reach for
  `parent_based` and the decision becomes the caller's, which is worth knowing
  before it is made at the edge.
- **carry baggage**, which is arbitrary text that travels with the request and
  may end up as attributes wherever the telemetry is kept.

None of this is a way into the app; it is a way into what the app records about
itself. Where it matters, the answer is at the boundary: a gateway or a mesh
that strips the trace headers of callers it does not know, and mutual TLS so
that "does not know" means something. Behind that boundary, believing the
caller is the whole point.

The database is chosen by the configuration file. SQLite **in memory** is what
the bundled configuration names, because it needs nothing around it and leaves
nothing behind — it is a demonstration and not a place to keep anything. The
file DSN is a comment beside it. **Production runs on PostgreSQL**, which is
what the migrations are written for.

```yaml
db:
  driver: pgx
  dsn: "postgres://postgres:pw@localhost:5432/postgres?sslmode=disable"
```

Both bundled drivers, SQLite and [pgx](https://github.com/jackc/pgx), are pure
Go, so the binary is still built with `CGO_ENABLED=0`. Another database is a
file next to `cmd/config/db-pgx.go` that blank imports the `database/sql` driver
and tells the configuration which dialect it speaks — as long as it is a dialect
the generated servers write SQL for, which today means SQLite or PostgreSQL. See
[Somewhere other than PostgreSQL](#somewhere-other-than-postgresql).

```go
package config

import (
	"entgo.io/ent/dialect"

	_ "github.com/go-sql-driver/mysql"
)

func init() {
	RegisterDriver("mysql", dialect.MySQL)
}
```

`db.driver` then names the driver to open the connection with. A driver that is
not registered can still be used by naming its dialect explicitly with
`db.dialect`.

```
$ go run . serve
> error: open database: unknown driver "mysql": it must be one of [pgx sqlite3], or db.dialect must be given
```

## Migration

The ent schema says what the database should look like; a migration is the SQL
that takes a database from what it is to that. They are kept in `migrations/`,
reviewed like any other code, and applied in order.

`serve --db-migrate` is the other way: ent looks at the database and changes it
until it matches. It is quick and it is what the tests use, but it never drops
anything, it cannot be reviewed, and it happens while the app is starting. Use
it while developing; deploy with `migrate apply`.

### Writing one

Planning needs a **dev database**: an empty database of the same kind as the
one the app runs on. The migrations that are already written are replayed onto
it to work out what the current state is, and it is emptied again afterwards,
so it must not be a database anyone cares about. `docker compose up -d db`
brings up one alongside the local database; the devcontainer runs an engine of
its own, so that works from inside it as well.

```sh
# 1. Change the schema, which means changing the proto and generating again.
$ ./scripts/gen-go.sh && ./scripts/gen-ent.sh

# 2. Write the difference as a migration. Flags come before the name.
$ go run . migrate plan --dev "postgres://postgres:postgres@localhost:5432/dev?sslmode=disable" add_coffee_email
> written: 20260801175803_add_coffee_email.sql
> read them before they are applied to anything.

# 3. Read what was written, then commit it with the schema change.
$ cat migrations/20260801175803_add_coffee_email.sql
```

`db.dev_dsn` in the configuration file says the same thing as `--dev`. Either
way it is opened with whichever registered driver speaks the dialect the
migrations are written in, so what `db.driver` says does not come into it;
planning is about the files, not about this deployment.

Destructive changes are planned, not skipped: a column that is gone from the
schema is a `DROP COLUMN` in the file. That is the point of reading it before
it runs anywhere.

`atlas.sum` records what each file looked like when it was written, and it is
checked every time the directory is opened. Never change a file that was
already applied somewhere; write another migration instead.

```
$ go run . migrate apply
> error: open migration directory: "migrations" does not match its atlas.sum: checksum mismatch
```

### Starting over

Before the first release the schema moves every day, and a history of twenty
files that describe a table nobody ever had is worth nothing. While no database
that matters has run them, throw them away and plan again:

```sh
$ rm -f migrations/*.sql migrations/atlas.sum
$ docker compose down -v db && docker compose up -d db
$ go run . migrate plan init
$ go run . migrate apply
```

The second line is the one that is forgotten. A database records which versions
it ran, so one that ran the migrations that were just deleted is left claiming a
history that no longer exists, and the next apply tries to create what is
already there:

```
$ go run . migrate apply
> error: apply: execute: ... ERROR: relation "roaster" already exists (SQLSTATE 42P07)
```

It records that failure as well, so the database has to be dropped anyway.
**Anything that cannot be dropped is the reason not to start over**: once a
deployment has run a migration, that file is history and history is not
rewritten. Write another migration instead.

A history that grows long is a different problem, answered with a checkpoint
rather than with a reset: one file holding the whole schema at a point, which a
new database starts from and an existing one skips. `migrate apply` already
honors one, but nothing here writes one yet; the signal to add it is planning
getting slower, since it replays every file that was ever written onto the dev
database.

### Applying

```sh
# What would run, and nothing else.
$ go run . migrate apply --dry-run
> pending: 20260801175803_add_coffee_email.sql

$ go run . migrate apply
> applied: 20260801175803_add_coffee_email.sql
```

Which migrations a database has run is recorded in the database itself, in the
`schema_revisions` table, so applying twice does nothing the second time.

The migrations travel inside the image, so a deployment runs them with the same
binary it is about to serve with, before the new one takes over:

```sh
$ docker run --rm ghcr.io/lesomnus/go-app:edge migrate apply
```

### Somewhere other than PostgreSQL

The app runs on SQLite or PostgreSQL, and the tests run on SQLite. Two things
are tied to one database rather than to ent:

- **`Apply`.** A patch document becomes JSON functions, and those are not
  portable, so `entpatch` writes SQL for the two dialects above and no others. A
  client on anything else is refused by `bare.NewServer` when the stack is built,
  rather than at the first `Apply`. Widening that set is a change to
  [protoc-gen-orm-ent](https://github.com/protobuf-orm/protoc-gen-orm-ent), not
  to this repository.
- **The migration files**, since SQL is not the same everywhere.

To move the migrations:

- `migrate.Dialect` in `internal/migrate/migrate.go` is what the files are
  written for, and both `plan` and `apply` refuse a database that speaks
  something else.
- `driver` in the same file maps a dialect to the atlas driver that reads and
  writes it; the drivers live under `ariga.io/atlas/sql`.
- The files that are already in `migrations/` are PostgreSQL. Replan them from
  an empty directory rather than translating them by hand.

Keeping more than one database in production means keeping a directory of
migrations per dialect, and planning each of them against a dev database of its
own. It is worth the trouble only if it is worth the trouble.

Nothing here needs a tool of its own: the planning and the applying are done by
the [atlas](https://atlasgo.io) packages ent already depends on, which are
Apache-2.0. The Atlas CLI, which is licensed separately and has paid tiers, is
not used.
