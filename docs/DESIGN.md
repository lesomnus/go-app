# The shape of this app, and why

What an app made from this template gets, and the reasoning behind the parts
that are not obvious. Read it once before changing something that looks odd —
most of what looks odd here is load-bearing.

The README says *what* everything does. This says *why it is that way*, and what
it would cost to do it differently.

## A service, not a platform

One kind of thing (`Coffee`), somebody it belongs to (`Roaster`), and the
plumbing an app of that shape needs. There is no tenancy, no audit trail, and
nothing that narrows what a caller may *see*: every row is everybody's to read,
and what a caller may **do** is decided in front of the handler.

That is a deliberate line, and the other side of it is `kind/server-x`: the same
template with the wall — a Tenant every row belongs to, a predicate in every
query saying which of them a caller may read, and an audit trail. Reach for that
one when rows belong to somebody. Reach for this one otherwise, and it is most of
the time.

Both branch from `main`, which is the base with no server in it at all.

## Two entities, and why not one

An entity on its own demonstrates nothing about references. `Roaster` exists so
that `Coffee` can belong to something, which is what makes these real rather than
described:

- an **edge**, and an immutable one, which is what most edges are
- a **slug**: a name unique *within* something else, which is the shape almost
  every name has once there is more than one table
- **key resolution** — pointing an edge at a row named by a reference
- a **composite unique index**, and a partial one at that

Delete `Roaster` and rename `Coffee` if your app really has one flat table. Keep
both if it does not, and rename them to what your app is about.

## Anonymous is a caller

A request with no credential is served as `frame.Anonymous` rather than refused,
so **there is no such thing here as a request with no frame**.

That is worth a paragraph, because the alternative is where this sort of thing
goes wrong. A request with no frame is a question nothing has an answer to, and
every answer that suggests itself is bad: refuse it, and the app's own calls have
to go around the layer that refuses; serve it as nobody-in-particular, and the
code deciding what nobody-in-particular may do is somewhere else, unwritten,
defaulting to whatever a zero value means. Making the anonymous caller a caller
means the question is asked and answered in one place, for everybody.

What one may do is one rule in `server/gate`, and it is a **list of what is
allowed**. `server.allow_anonymous_reads` names the reads this app generates —
`Get`, `List`, `Watch`. The other way round reads the same today and is wrong the
day somebody writes `Rename`: it is a write, it is not spelled `Patch`, and it
would have been open to everybody with nothing anywhere to say so.

See [AUTH.md](AUTH.md) for the whole chain.

## The actor is not a row of this app

There is no user table, so who somebody is comes from **outside** — a token's
subject, a certificate's name, a header in development. `frame.Actor.Subject` is
an opaque string and nothing here compares it with anything but itself.

That is what makes `auth.TokenStore` the seam an issuer is injected at, and it is
why there is no resolver step: the only credential that has to be exchanged for a
name is the token, and exchanging it *is* the store. A header and a certificate
already carry a name.

## Soft delete is about the identifier

A `Coffee` is stamped rather than deleted so that its identifier stays taken for
good. A row that is gone for real leaves its identifier free for something else
one day, and anything that kept one — a link somebody saved, a row in another
system, an event that was published — would then point at a coffee nobody meant.

The **name** comes free again, through a unique index covering only the rows
still there. An identifier is forever and a name is not. `includes_erased` is how
the other choice is made.

There is no `Restore`, and that is the point: this is not a recycle bin.

**A `Roaster` is erased for real, and takes its Coffees with it.** That is not a
policy about deletion, it is what erasing a Roaster already meant — and it is
something `server/core` has to *say*, because **soft deletion does not cascade**
and a foreign key does not care that a row is "gone". A stamped Coffee keeps its
row, and the row keeps a key to its Roaster, so without the cascade a Roaster that
ever had a Coffee could never be erased at all.

**Partial indexes are SQLite and PostgreSQL only.** MySQL has none and ent writes
the annotation out for it rather than refusing, so the index would come up
covering every row and a freed name would stay taken with nothing to say so. The
generated `NewServer` refuses a dialect this backend writes no SQL for, and that
set is exactly the two that have partial indexes.

## A watch sends state, never a delta

So a client converges rather than replays, a stream that missed something is
still correct, and there is no version, cursor or backlog to keep anywhere.

The order matters: **subscribe, then snapshot, then read the row back per
event.** Reading first loses whatever changed in between, with nothing to say it
did. Subscribing first means the only thing that can go wrong is a row arriving
twice, which is harmless when the payload is state.

A removal is said by **absence** — an item with no value — and nothing
distinguishes "erased" from "no longer visible", because nothing needs to.

It is **not an outbox**: an event lives in this process, and a crash between the
commit and the dispatch loses it. That is enough for a watch and is not enough
for anything that has to act on every change exactly once.

## `Patch` and `Apply` are not an API

They are how the servers write. Between them they can change anything the schema
holds, which is what makes them useful to a server and wrong as a public surface:
what a caller may change, and under what conditions, is not something a general
write can be told.

A caller gets the RPC somebody wrote — `Rename`, `Publish`, whatever the app
means — which can be validated and can mean something. The general ones are
closed **at the transport** (`server.allow_general_writes`), not in a layer, so
an RPC written by hand goes on being implemented with them.

See [EXTENDING.md](EXTENDING.md) for how to write one.

## What a request must say, in Go

There are no constraints in the messages — no `buf.validate`, no validating
interceptor. A rule lives beside the thing it is a rule about, in the server,
written out.

That was tried the other way first. Two things came of it: the service generator
does not copy field options into the request messages it generates, so a
constraint on the entity never reaches the request; and the constraints that were
left turned out to be either redundant with the code that already reads the value
or better said in Go, where the *reason* for the number survives next to it.

## The seams that are empty on purpose

Three, and each is off by default:

| | |
| --- | --- |
| `gate.Policy` | what a deployment says about a caller. `server/gate/roles` is a reference implementation and nothing wires it in |
| `watch`'s subscribers | nothing in this binary listens; `cmd/serve.go` shows where one goes |
| `spin`'s loops | nothing in this app spins |

A deployment that says nothing gets exactly the app it was served before, and
that is what makes them seams rather than features.

## What `bare.Scope` is doing here

Nothing, and it is worth saying why it is not deleted.

It is the **read-side convergence point**: the place the generated servers put a
predicate into every query they build, so that narrowing a read is one statement
rather than an override of `Get`, `Patch`, `Apply` and `Erase`, once per entity,
forever. This branch has nothing to narrow by, so nothing installs one — except
the generator itself, which puts the soft-delete predicate there unconditionally.
That is why `CoffeeNarrow` exists, and why the hand-written `List` goes through it
rather than reaching for the hook.

An app that grows an owner, a tenant or a visibility installs a scope there and
writes nothing per entity. See `kind/server-x`.

`bare.Unscoped` is what keeps an entity added later from being a compile error in
every app that narrows anything — and it is also the trap: "no opinion" means
every row. An app that installs a scope should have a test that asks each method
what it answers, the way `kind/server-x`'s `wall_test.go` does.

## Later

Not now, and not blocked by anything here.

- **An outbox**, for anything that has to act on every change exactly once.
- **Retention.** Sweeping Coffees erased long enough ago is the obvious thing for
  `server/spin` to do, and the number is a deployment's rather than a template's.
- **A quota**, as opposed to the rate limit that is done: a budget over a window
  somebody is billed against. It has to be counted somewhere every process can
  see, which is a store this template would be imposing. `grpcx.Limiter` is the
  seam.
