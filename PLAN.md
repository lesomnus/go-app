# Plan

What is still missing from this boilerplate, in the order it is being filled
in, and the reasons for the shape each piece takes.

This file is about the work, not about the app. An app made from this template
can delete it.

## The one idea

Four things turn out to be the same missing feature:

> A way to say, for an entity, **what predicate every read of it carries** and
> **what every write of it reports**, without writing it out once per entity.

| | |
| --- | --- |
| the audit trail | every write reports itself |
| the Tenant wall | every read carries `tenant_id ∈ scope` |
| soft delete | every read carries `deleted_at IS NULL` |
| field constraints | every write checks what it writes |

The trail is done: `protoc-gen-orm-ent` emits a `Recorder` into the generated
servers and they call it from inside the transaction that makes the write. That
is the **write-side convergence point**, and finding it is what made the trail
complete rather than a list of overrides.

What is missing is the **read-side** one: a place the generated servers put a
predicate into every query they build. `server/gate` is what it costs not to
have it — thirteen overrides across three entities, five more for every entity
added, and a bug that was fixed in one of them and copied into the next.

So the plan is: open the read-side convergence point, then put the Tenant wall
and soft delete through it.

## What is already settled

Decisions taken before the work started, kept here so the reasons do not have
to be found again.

**History stays where it happened.** A trail row is walled by the Tenant that
owned the object *when the write happened*, not by whoever owns it now. So a
resource that moves from one Tenant to another leaves its past behind. This is
the conservative reading — receiving a resource does not grant the right to
read what the previous owner did inside their own walls — and it is also the
cheap one: it needs a column and no join.

The other policy, where history follows the resource, needs to reach the
object's *current* owner, which means a join, which means knowing which table
the object is in. The trail deliberately does not record what kind of thing a
row is about — an identifier is unique across every table, so it names the
thing on its own. **Wanting history to transfer is what would force the kind
back into the row**, and that is the price to weigh when it comes up.

It can be reconsidered later, at a cost: the kind can be backfilled for objects
that still exist by probing the tables, and not at all for ones that were
erased. `object_tenant_id` cannot be reconstructed later at all, which is why
it is added now (Phase 3) even though nothing reads it differently yet.

**`Patch` and `Apply` are for layer implementors, not for callers.** They are
the general write, and a general write cannot be validated in general: an
`Apply` that writes `alias` is not an `alias` field on a request, so nothing
that checks requests sees it. The answer is not to check documents harder. It
is that a caller does not get to say a general write at all — an app that needs
to rename something defines `Rename`, validates *that*, and implements it with
`Apply`. So the public surface closes them by default (Phase 4), and the
trail's `action` says `Rename` because it is the method gRPC dispatched.

**The Tenant wall stays a SQL predicate.** Fine-grained permissions may later
be answered by a Zanzibar-style relation service, and that is a good fit for
point questions — may this actor do this to this object. It is a bad fit for
`List`, which needs a predicate and not a yes-or-no; answering it means reverse
expansion, which does not push into SQL and does not survive a large table. So
the two stay separate on purpose: the wall is a predicate in the query, and
anything finer is asked above it.

**`List` filters stay hand-written.** Filtering is the domain and there is no
general answer. Paging is not the domain: a cursor, a stable order, and a limit
look the same for every entity, and they are the half people get wrong. So the
paging is borrowed from the runtime and the filter is written out (Phase 5).

## Phases

Each phase is a commit or two. The generator and this repo take turns, since
the generated code cannot use a runtime that has not been released yet.

### Phase 0 — the ops floor

Independent of everything else and of each other.

- **`buf breaking` in CI.** The contract is what every generated thing is made
  from, and CI checks that the generated things match it, but nothing checks
  that the contract itself did not break.
- **A default deadline.** A call that arrives without one can hold a database
  connection for as long as it likes. The interceptor caps a call that named no
  deadline and leaves alone one that did.
- **Readiness and liveness are different questions.** `watchDb` sets a single
  status under the empty service name, so a database that blinks reads as a
  process that should be killed. Liveness is the process; readiness follows the
  database.
- **More than one recorder.** `WithRecorder` replaces rather than accumulates,
  so there is no room for a second thing that wants to hear about writes. The
  interesting part is the failure semantics: a recorder is *required* by
  default — the write fails with it — and something best-effort swallows its
  own errors, which is a thing to say out loud rather than to leave to whoever
  writes the second one.

### Phase 1 — the read-side convergence point

A `Scope` hook next to `Rec` on each generated service server: a function from
a context to a predicate, applied to every query that server builds.

The generator must not learn what a Tenant is. It emits the hook and calls it;
what the predicate means is the app's to say. That is the same discipline the
`Recorder` keeps — `bare` knows a write happened and nothing about audit.

Three things to get right:

- **The trail must not be walled.** A recorder writes through a server that has
  no recorder of its own, so it cannot audit itself into a loop; it needs the
  same exemption from the scope, or the trail write walls itself out.
- **A request with no frame.** `EnsureRoot` runs before there is anybody to be.
  The plan was for the hook to answer *unscoped* there, and that was wrong: it
  is the same answer for a request whose frame went missing by mistake, and the
  authentication interceptor is the only thing standing between that and every
  row in the database. What is done instead is to refuse a frameless request
  and hand the two callers that must go around the wall a server it was never
  installed on. The bypass is then a wiring decision somebody can read.
- **How it is installed.** Predicates are typed per entity, so one option
  cannot carry them all.

### Phase 2 — what goes through it

- **2.1 The Tenant wall.** `frame` carries a scope — the set of Tenants this
  caller may see — instead of one Tenant. An ordinary caller is their own, and a
  later sharing or transfer feature is a larger set rather than a special case;
  `unbounded` stops being a concept. (At the time root was the whole set; that
  privilege was removed afterwards — see phase 7.) `gate` keeps only the rules
  that are rules: a Tenant is not put up from inside one, and a Holder is
  created inside a named one. "Not found rather than denied" comes
  for free — a walled query returns nothing, and nothing is `NotFound`.
- **2.2 Soft delete.** **Done**, once `protobuf-orm` could say it. Declared in
  proto, emitted by the generator: the column, the read predicate (Phase 1's
  slot), `Erase` stamping instead of deleting, and the partial unique index that
  lets an erased row give up its name. `Holder` uses it; `Tenant` does not, and
  the reason is the interesting part.

#### What 2.2 was blocked on, and what came of it

Two of its three parts needed a **fourth** repository, `protobuf-orm`, whose
schema this app consumes from a remote registry (`buf.build/orm/orm`, pinned by
digest in `buf.lock`). A local edit there cannot be consumed without publishing
to that registry -- so the work stopped here until that was done, and then went
on. Both parts are now in `protobuf-orm` and published.

- **Declaring it** — now `(orm.field) = {erased: {}}`, the way `version: {}`
  already marks the version field.
- **Uniqueness, which is the part that matters** — now `orm.Index` defaults a
  unique index of a soft-erasing entity to covering only the rows that are
  still there, with `includes_erased` to opt back out. Shipping soft delete
  without that would have been worse than not shipping it: an app would find
  the hole the first time somebody re-created a deleted thing.

What is *not* blocked is the half this plan said was the hard one. **Reads are
already free**: soft delete is a predicate, Phase 1 is where predicates go, and
an app can install `holder.DateErasedIsNil()` next to the Tenant wall today.
That is the thesis of this plan holding up — the wall and the soft delete are
the same feature — and it is worth saying that the earlier reasoning here
("it cannot live in `core`, because the reads are all generated") stopped being
true the moment Phase 1 landed.

What the adoption turned up, which no amount of planning would have:

**Soft deletion does not cascade, and a foreign key does not care that a row is
"gone".** Erasing a Holder softly leaves the row, and the row keeps its key to
its Tenant -- so `Tenant.Erase` began failing on that constraint, for ever,
however many Holders had been erased first. It is a real capability regression
introduced by the feature, and the fix is a sentence the entity that owns the
others has to say: `core.TenantServiceServer.Erase` takes its Holders with it,
in one transaction, which is what erasing a Tenant already meant.

**And a field-level `unique` could not be made partial**, at first. Only an
`indexes` entry can carry a predicate, so `Tenant.alias` would have kept its
name across a soft erasure while `Holder.alias` gave it up -- an asymmetry that
does not bite here, since a Tenant is not erased softly, and would have bitten
somebody. It was closed rather than written down: for a soft-erasing entity the
generator promotes a unique field to an index of its own. The `Ref` keeps the
bare-scalar shape a unique field always had, because that is `graph`'s to decide
and it reads the field, not the schema.

### Phase 3 — `object_tenant_id` — **not done, and the reason is a correction**

The plan was a column on `Audit`, stamped at write time, so that the wall on the
trail reads the object's Tenant rather than the actor's. Two things came out of
building it that change the answer.

**The premise was wrong.** "History stays where it happened" was given as the
thing this column buys, and it is already true without it: `tenant_id` is the
Tenant *the actor was held by*, stamped when the write happened, and nothing
moves it afterwards. A Holder that is later transferred to another Tenant leaves
every row of its trail behind, because those rows were never about where the
Holder lives — they are about who did something.

So the column is not "history stays put". It is a **different policy**: let a
Tenant see what was done *to its own rows*, including from outside it. That is
the limitation the README records, it is a real one, and it is a widening of the
wall rather than a tightening — acme would start seeing rows whose actor is
somebody outside it. It was not asked for, and it has a disclosure trade-off of its own.

**And it cannot be recorded cheaply.** The recorder runs after the write, inside
its transaction. For `Add` and `Apply` the row is still there and its Tenant is
one query away. For an `Erase` that deletes -- which at the time was every
`Erase` -- it is gone: the generated server reads the row's key first, deletes,
and *then* records. Making the object's Tenant
available there means one of

- handing the recorder the row itself (`Change.Row`), which for a Holder means
  the generated `Erase` eagerly loading every edge of every entity, in a
  general-purpose generator, to serve one app's column; or
- recording before the delete, which gives up the property that only what
  actually happened is recorded.

A column that is right for three RPCs and absent for the fourth is worse than no
column, and `Erase` is the case somebody would most want it for.

Soft delete (2.2) landed afterwards and changed the fact without changing the
answer. An erased `Holder` is still a row, so the recorder could read it -- but a
`Tenant` is erased for real, so the column would be right for the entities that
erase softly and empty for the ones that do not, which is the same wrong shape
one step over. It would read as "no Tenant" where it means "not recorded here".

The trade-off is written down in `proto/go_app/audit.proto` and in the README
instead, which is what this phase was really for: a reader should know that the
trail is the actor's and not the object's, and why.

### Phase 4 — validation, and what the general write is for

- **Validation, in Go rather than in the message.** protovalidate was built and
  then taken back out, which was the right end to arrive at from the wrong
  direction, so the reasoning is kept.

  It was tried first because the constraints would be declared where every
  other declaration is. Two things came out of trying it. `protoc-gen-orm-service`
  does not copy field options into the request messages it generates, so a
  constraint on `Holder.alias` never reaches `HolderAddRequest.alias` — which
  under the doctrine below does not matter, since every RPC a caller uses is one
  somebody wrote. And then the constraints that were left, on the two
  hand-written lists, turned out to be either redundant with the code that
  already reads the value (`HolderPick` refuses a reference that names nothing;
  `uuid.FromBytes` refuses sixteen bytes that are not) or better said in Go: the
  filter bound is refused where the page size is clamped, and the reason for it
  is a sentence about `List` that belongs next to `List`.

  What is left is a rule written beside the thing it is a rule about, and no
  `cel-go` in the dependency tree.
- **The doctrine, written down.** A README section on why `Patch` and `Apply`
  are not an API, and what to write instead.
- **The surface closed**, but at the *transport* rather than in a layer, which
  is the whole point: what is closed is what a caller may ask for, and an RPC
  written by hand goes on being implemented with `Apply`. Closing it in a
  server would close it to the servers. `server.allow_general_writes` is off by
  default; `internal/ox` serves them, knowingly, because they are what this
  repository has to demonstrate.

### Phase 5 — paging

A cursor helper in the runtime (`runtime/entpage`), and both hand-written
`List`s rewritten to use it. The example stops being the simplest thing that
works and becomes the simplest thing that is *right*, because the paging is what
a reader is there to copy.

Keyset and not offset; the order ends in the key, since a cursor cannot tell
apart two rows equal in every column of it; the size is capped; and one row more
than the page is read, so a full last page answers with no cursor rather than
sending the caller back for an empty one. The filtering stays hand-written and
stays marked as the part to rewrite.

### Phase 8 — how much one caller may ask for

A token bucket per caller, in front of the layers that decide what a caller may
see. `grpcx.Limit` counts, `gate.ByTenant` says what is counted, and
`server.limit.rate` says how much — off unless it is written down.

Three things that had to be decided rather than looked up:

- **What the key is.** The Tenant, and not the Holder or the credential: a
  Tenant makes as many of either as it likes, so counting one of those is a
  limit anybody can raise by asking for another one. The other direction — one
  runaway client inside a Tenant starving the rest of it — is a second key, not
  a different one.
- **What the refusal says.** `ResourceExhausted` with `RetryInfo`, since a
  refusal a client cannot time is a client that asks again immediately, which is
  the traffic the limit was for. Not `PermissionDenied`: nothing about what the
  caller may do changed.
- **What keeps the map bounded.** A full bucket answers exactly the way a bucket
  that was never made does, so a key that has gone quiet can be forgotten
  without changing an answer. That is what makes it a map of the keys that are
  *behind* rather than of every key ever seen — and it is only enough because
  the keys here are the Tenants. A key a caller can invent needs something that
  forgets by size.

### Later

Not now, and not blocked by anything here.

- **Roles**, above the wall rather than instead of it. `gate.Policy` is the
  seam and is unset: this app is a resource server, not an authorization
  server. A real engine is a dependency an app takes, not one a template
  imposes, so an integration belongs in a branch of its own.
  `server/gate/roles` is a reference implementation — a table of roles and
  bindings, held as a snapshot and swapped whole — so that the seam has
  something to read and something to test against. Nothing wires it in.
- Zanzibar-style relations, which is a different thing again and is only worth
  it for permissions *derived* over a deep graph — nested teams, folder
  inheritance. The dividing line is whether the grant can be a predicate in the
  query: a row in this database can, a graph in another service cannot, and a
  list depending on the second one breaks.
- An outbox, and a `Watch` RPC to serve what `server/watch` publishes. The
  publishing half is done: Phase 0's second recorder turned out to be exactly
  the room it needed, and what was left was deciding *when* — which is not
  inside the transaction, where the trail is written, but after the handler has
  answered. What is not done is durability (an event lost in a crash is lost)
  and a way for a client to ask for the stream.
- grpc-web, and the HTTP listener that brings `pprof` with it.
- **A quota**, as opposed to the rate limit that is done (Phase 8): a budget
  over a window that somebody is billed against. It is not a smaller version of
  the same thing — it has to be counted somewhere every process can see, which
  is a store this template would be imposing. `grpcx.Limiter` is the seam, the
  same way `gate.Policy` is.

## Progress

| Phase | | |
| --- | --- | --- |
| 0.1 `buf breaking` | **done** | against the base of a pull request, or the commit a push moved from; `main` here carries no definitions to compare against |
| 0.2 default deadline | **done** | `grpcx.Deadline`, `server.timeout`, unary only |
| 0.3 readiness / liveness | **done** | `""` follows the database, `"liveness"` follows the process |
| 0.4 more than one recorder | **done** | `WithRecorder` accumulates; every recorder is required |
| 1 the `Scope` hook | **done** | `bare.Scope`, a method per entity, into every query the generated servers build |
| 2.1 the Tenant wall | **done** | `gate.Wall()`; thirteen overrides became three rules and a predicate |
| 2.2 soft delete | **done** | `erased: {}` in `protobuf-orm`, stamped by the generated `Erase`, partial unique index; `Holder` uses it |
| 3 `object_tenant_id` | **dropped** | the premise was wrong and the cost is a generator-wide one; written down instead |
| 4 validation and the doctrine | **done** | checks in Go beside the rule they belong to; `Patch`/`Apply` closed at the transport, off by default |
| 5 paging | **done** | `runtime/entpage`; both hand-written lists page by cursor |
| 6 attenuation | **done** | `frame.Grant`, met with the wall; `gate.Policy` defined and unset |
| 7 no superuser | **done** | the root comparison is gone; going around the wall is a server instance, not an identity |
| 8 rate limit | **done** | `grpcx.Limit` and `grpcx.MemLimiter`, keyed by `gate.ByTenant`; off unless `server.limit.rate` says otherwise |

Checked against a real PostgreSQL and not only the SQLite the tests run on:
migrations applied to an empty database, the root Tenant put there before
anything was served, writes leaving trail rows, a list read across three pages,
`Patch` refused, a bad identifier refused before the server saw it, the wall
answering `NotFound` from inside a Tenant, and `Tenant.Add`/`Tenant.Erase`
answering `Unimplemented` to the first Tenant's admin — who is the caller a
superuser would have been.

### What the plan got wrong

Worth keeping, since the errors were in the reasoning rather than in the code.

- **Soft delete "cannot live in `core`, because the reads are all generated"**
  stopped being true the moment the `Scope` hook landed — and then turned out
  not to belong in `core` anyway. It is not a scope: a scope says what *this
  caller* may see, and this says what there is to see at all, so it is
  unconditional in the generated `<Entity>Narrow`. What `core` does have to say
  is the part nobody predicted: that erasing a Tenant takes its Holders with
  it, because soft deletion does not cascade and a foreign key does not care
  that a row is "gone".
- **`object_tenant_id` makes "history stays where it happened" true.** It was
  already true. The column is a different policy, and a widening one.
- **The scope should see everything when a request has no frame.** That is also
  the answer for a frame that went missing by mistake. Refusing, and handing a
  server with no wall to the two callers that must go around it, makes the
  bypass something a reader can find.
- **A superuser is the shape of "the deployment can do more".** It is not, and
  writing one was the plan's own doing: an identifier kept as a constant, a
  comparison against it in the middle of the wall, and a `Tenant.Add` served to
  whoever matched. A privilege granted by *being a particular row* cannot be
  revoked, cannot be narrowed, does not appear anywhere it is used, and belongs
  to whoever finds the row. What the deployment needs is a **server the wall was
  never installed on** — which already existed, because the trail and
  `EnsureRoot` need one. Removing it made the boundary something a reader can
  see: a capability somebody was handed rather than one they satisfy, and a test
  that wants it has to reach for `c.Ungated()` and say so.
- **A rate limit wants the scope to exist first.** It does not want it at all.
  The scope is a *set* — what this caller may see — and a limit needs one thing
  to count against. Counting against the scope would give a credential that
  narrows itself a bucket of its own, which is a limit anybody could raise by
  issuing a narrower token. What it wanted was the Tenant on the frame, which
  Phase 2.1 did not add and `server/auth` had put there all along.
- **Validation belongs on the request, declared.** On the request, yes — but
  written in Go, next to the rule it is part of. Declared, the number survives
  and the reason for it does not, and there is only the one verb where refusing
  and clamping are both answers.
