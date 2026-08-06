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
  The hook is the app's, so the app decides, but this repo's hook answers
  *unscoped* when there is no frame — the same call the trail already makes
  when it falls back to `Change.Method`. It is the one dangerous default here
  and it is written down where it is made.
- **How it is installed.** Predicates are typed per entity, so one option
  cannot carry them all.

### Phase 2 — what goes through it

- **2.1 The Tenant wall.** `frame` carries a scope — the set of Tenants this
  caller may see — instead of one Tenant. Root is the whole set, an ordinary
  caller is their own, and a later sharing or transfer feature is a larger set
  rather than a special case; `unbounded` stops being a concept. `gate` keeps
  only the rules that are rules: a Tenant is not created from inside one, and a
  Holder is created inside a named one. "Not found rather than denied" comes
  for free — a walled query returns nothing, and nothing is `NotFound`.
- **2.2 Soft delete.** Declared in proto, emitted by the generator: the column,
  the read predicate (Phase 1's slot), and `Erase` stamping instead of
  deleting. It cannot live in `core`, because it is a predicate on every *read*
  and the reads are all generated. The real work is uniqueness — a soft-deleted
  `acme` still occupies the unique index, so the same alias cannot be used
  again until the index is told about the stamp. A hard-delete guide comes with
  it, and the interesting half of that guide is what happens to the trail.

### Phase 3 — `object_tenant_id`

A column on `Audit`, stamped at write time, and the wall on the trail reads it
instead of the actor's Tenant. This is what makes "history stays where it
happened" true rather than accidental, and it closes the limitation the README
records today: a Tenant cannot see changes made to its own resources from
outside it.

### Phase 4 — validation, and what the general write is for

- **protovalidate.** Field constraints declared where every other declaration
  is, checked by one interceptor. `core` keeps what is genuinely domain —
  normalization, cross-field rules, and anything that has to ask the database.
- **The doctrine, written down.** A README section on why `Patch` and `Apply`
  are not an API, and what to write instead.
- **The surface closed.** `gate` currently forwards `Patch` and `Apply` for
  Holder and Tenant, which contradicts the above. They close the way the trail
  already closes its writes: by building from the unimplemented server, so an
  RPC added later is refused rather than forwarded.

### Phase 5 — paging

A cursor helper in the runtime, and both hand-written `List`s rewritten to use
it. The example stops being the simplest thing that works and becomes the
simplest thing that is *right*, because the paging is what a reader is there to
copy.

### Later

Not now, and not blocked by anything here.

- Zanzibar-style permissions, above the wall rather than instead of it.
- An outbox and a `Watch`, when there is something to consume them. Phase 0's
  second recorder is the room they need.
- grpc-web, and the HTTP listener that brings `pprof` with it.
- Rate limiting and per-Tenant quotas, which want Phase 2.1's scope to exist
  first.

## Progress

| Phase | | |
| --- | --- | --- |
| 0.1 `buf breaking` | **done** | against the base of a pull request, or the commit a push moved from; `main` here carries no definitions to compare against |
| 0.2 default deadline | **done** | `grpcx.Deadline`, `server.timeout`, unary only |
| 0.3 readiness / liveness | **done** | `""` follows the database, `"liveness"` follows the process |
| 0.4 more than one recorder | not started | |
| 1 the `Scope` hook | not started | |
| 2.1 the Tenant wall | not started | |
| 2.2 soft delete | not started | |
| 3 `object_tenant_id` | not started | |
| 4 validation and the doctrine | not started | |
| 5 paging | not started | |
