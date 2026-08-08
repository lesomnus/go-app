# Plan

What this branch is, what it decided, and what it does not do.

This file is about the work, not about the app. An app made from this template
can delete it.

## What this branch is

**A service, not a platform.** One kind of thing (`Coffee`), somebody it belongs
to (`Roaster`), and the plumbing an app of that shape needs. There is no tenancy,
no audit trail, and nothing that narrows what a caller may *see*.

That is a deliberate line, and the other side of it is `kind/server`: the same
template with the wall — a Tenant every row belongs to, a predicate in every
query saying which of them a caller may read, an audit trail, and a
superuser-shaped hole that was closed on purpose. Reach for that one when rows
belong to somebody. Reach for this one otherwise, and it is most of the time.

Both share `main`, which is the base with no server in it at all.

## Two entities, and why not one

An entity on its own demonstrates nothing about references. `Roaster` is here so
that `Coffee` can belong to something, which is what makes the following real
rather than described:

- an **edge**, and an immutable one, which is what most edges are
- a **slug**: a name unique *within* something else, which is the shape almost
  every name has once there is more than one table
- **key resolution** — pointing an edge at a row named by a reference
- a **composite unique index**, and a partial one at that

## What was decided

**Anonymous is a caller.** A request with no credential is served as
`frame.Anonymous` rather than refused, so there is no such thing here as a
request with no frame — the case that has no good answer. What an anonymous
caller may do is one rule in `server/gate`, and it is a list of what is
*allowed*: `Rename` is a write, it is not spelled `Patch`, and a rule that named
the writes instead would have opened it to everybody.

**The actor is not a row of this app.** There is no user table, so who somebody
is comes from outside — a token's subject, a certificate's name. That is what
turns `auth.TokenStore` into the seam an issuer is injected at, and it is why
there is no `Resolver` step any more: the only credential that has to be
exchanged for a name is the token, and exchanging it *is* the store.

**Soft delete is about the identifier.** A `Coffee` is stamped rather than
deleted so that its identifier stays taken for good — anything that kept one
would otherwise point at a coffee nobody meant one day. The *name* comes free
again, through a unique index covering only the rows still there. An identifier
is forever and a name is not.

**A `Roaster` is erased for real, and takes its Coffees with it.** That is not a
policy about deletion, it is what erasing a Roaster already meant — and it is
something `core` has to *say*, because soft deletion does not cascade and a
foreign key does not care that a row is "gone".

**A watch sends state, never a delta.** So a client converges rather than
replays, a stream that missed something is still correct, and there is no
version, cursor or backlog to keep. Subscribe, then snapshot, then read the row
back per event — in that order, since reading first loses whatever changed in
between with nothing to say it did.

**`Patch` and `Apply` are not an API.** They are how the servers write. A caller
gets the RPC somebody wrote, which can be validated and can mean something; the
general ones are closed at the transport (`server.allow_general_writes`), so an
RPC written by hand goes on being implemented with them.

## What is here

| | |
| --- | --- |
| `server/bare` | generated: CRUD against the database, the `Recorder` hook, the `Scope` hook |
| `server/core` | the rules that hold wherever it runs, and the hand-written reads |
| `server/gate` | what a caller may do, decided once in front. `Policy` is the seam and is unset |
| `server/watch` | what a call changed, published once it has, and served as `Watch` |
| `server/auth` | who is calling. `TokenStore` is where an issuer goes |
| `server/spin` | what a layer does when nobody asked |
| `internal/grpcx` | what every call goes through: trace, log, recover, deadline, limit, closed |
| `internal/httpx` | the second listener: grpc-web, `/healthz`, pprof |

Three of those are **seams that are empty on purpose** — `gate.Policy`, `watch`'s
subscribers, and `spin`'s loops. A deployment that says nothing gets exactly the
app it was served before, and that is what makes them seams rather than features.

## What `bare.Scope` is doing here

Nothing, and it is worth saying why it is not deleted.

It is the **read-side convergence point**: the place the generated servers put a
predicate into every query they build, so that narrowing a read is one statement
rather than an override of `Get`, `Patch`, `Apply` and `Erase`, once per entity,
forever. This branch has nothing to narrow by, so nothing installs one — except
the generator itself, which puts the soft-delete predicate there unconditionally.
That is why `CoffeeNarrow` exists, and why the hand-written `List` goes through
it rather than reaching for the hook.

An app that grows an owner, a tenant or a visibility installs a scope there and
writes nothing per entity. See `kind/server`.

## Later

Not now, and not blocked by anything here.

- **An outbox.** `Watch` is served, but what it publishes lives in this process,
  so an event lost in a crash is lost. That is enough for a watch, because what a
  watch sends is state; it is not enough for anything that has to act on every
  change exactly once.
- **Retention.** Sweeping Coffees erased long enough ago is the obvious thing for
  `server/spin` to do, and the number is a deployment's rather than a template's.
- **A quota**, as opposed to the rate limit that is done: a budget over a window
  somebody is billed against. It has to be counted somewhere every process can
  see, which is a store this template would be imposing. `grpcx.Limiter` is the
  seam.
