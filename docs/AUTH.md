# Who is calling, and what they may do

Two questions, kept apart because they change for different reasons, and
answered by four things that hand their answers to each other.

```
   a request arrives
          │
          ▼
  server/auth ─────────── who is this?
          │               a Handler reads a claim, a Resolver looks it up.
          │               The answer is a Holder read from the database.
          │
          │               ...and what did the credential itself allow?
          │               frame.Grant, at most what that Holder allows.
          ▼
  server/gate ─────────── what may they see?
   (interceptor)          gate.Policy if a deployment injected one,
          │               the Tenant wall if not. Met with the Grant.
          ▼
  server/frame ────────── Actor · Grant · Scope
          │               Worked out once. Everything after reads it.
          ▼
  server/bare ─────────── ...and it is a WHERE clause.
   (the queries)          gate.Wall() put a predicate in every one of them.
```

Everything below is that picture, slowly.

## `server/frame` — what is known about a request

```go
type Frame struct {
	Actor *go_app.Holder  // who
	Grant Grant           // what their credential allows
	Scope Tenants         // which Tenants they may see
}
```

A frame is put into the context by whatever worked each of those out, and read
from it by everything that has to decide anything. **Each is worked out once.**
That is not an optimisation, it is what keeps the answers consistent: a scope
computed twice during one request is a request that can be told two things.

The zero value of `Grant` allows nothing and the zero value of `Tenants` sees
nothing. Both are that way round on purpose. A frame built by hand that forgets
one is a frame that can do nothing, which somebody notices at once; the other
way round it is a frame that can do everything, which nobody notices at all.

A request that reaches a walled server **with no frame is refused**. There is no
scope that means "everything, because nobody asked".

## `server/auth` — who is this

A `Handler` reads a claim out of the transport, a `Resolver` looks it up. What
comes back is a Holder read from the database rather than one the caller
described, so what it says about itself can be relied on.

Three handlers are written, and they differ in one thing: **where the name comes
from.**

| | | |
| --- | --- | --- |
| `plain` | the caller writes it, and is believed | `authorization: Plain acme/admin` |
| `mtls` | the client certificate carries it | a URI SAN, or the common name |
| `bearer` | the token carries nothing, and is exchanged for one | `authorization: Bearer <token>` |

`plain` checks nothing, which is the point — a test or a call by hand says who
it is and gets on with it — and the server says so out loud when it starts:

```
> ! callers are believed when they say who they are - auth=plain
```

`mtls` checks nothing either, and there it is right: the handshake already did
it. It reads only a chain the server **verified**, never what the peer merely
sent, so it says nothing at all under a TLS configuration that asks for a
certificate without checking one.

`bearer` is the only one that has to ask something, and that is what makes it
the interesting one. `server/auth/token.go` has a sample store — a map, with the
tokens held as digests and each carrying its own expiry, because a token having
a life of its own is exactly what a header and a certificate do not have. A real
store is a table or an issuer; the shape is the same.

### Falling back

The configuration names them in the order they are tried:

```yaml
auth:
  methods: [bearer, mtls]   # a token, and the certificate for whoever has none
```

The first one that finds a credential answers. One that finds a **bad** one, or
that **cannot tell** whether it is good, refuses the call rather than letting
the next one have a go — and that is the whole of what makes a fallback safe. A
token that expired must not quietly become whoever the certificate says, and a
token store that is down must not either.

That third answer is why `ErrUnavailable` exists. Told `Unauthenticated`, a
caller throws away a token that was never wrong and goes to get another one,
from the issuer that is already having a bad day; told `Unavailable`, it waits.

For a caller who has only a token to get as far as the token, the handshake has
to let them in without a certificate — `server.tls.client_cert_optional`. Leave
it off where the certificate is the floor and everything else is said on top.

A new way of asking is a `Handler` and nothing else.

## `frame.Grant` — what the credential itself allows

Every handler answers who. What that caller may then do is `server/gate`'s, and
a credential has nothing to say about it — **except that it should be able to do
less**. A token meaning "john, but only for reading" needs somewhere to put the
"only":

```yaml
acme/ci:
  token: "a-secret"
  tenants: ["018f2c...."]                 # or omitted: wherever the Holder may act
  actions: [/go_app.HolderService/Get]    # or omitted: whatever the Holder may call
```

Two axes, each narrowed or not, which is the shape of a GitHub fine-grained
token: a set of resources and a set of things that may be done to them.
Deliberately **not a map of one to the other** — "write here, read there" —
because a permission set that varies per resource is a policy, and a policy is
not something a credential should be carrying around. GitHub does not do that
either.

- **It only ever takes away.** Whatever decided the scope runs first and this is
  met with the answer. A token naming every Tenant, held by somebody who may see
  one, still sees one.
- **Only `bearer` can carry one**, because only a token has anywhere to put it.
  `plain` and `mtls` name somebody and stop, so they answer `frame.Whole()` and
  say so.
- **The action is checked once**, in `auth`'s interceptor, before the handler.
  It is not a rule about the caller — it is the credential saying it was not
  made for this, which is a question about the request rather than about the row
  it was going to touch.

What issues such a token, and who decided what to narrow it to, is not here and
should not be. **This app is a resource server, not an authorization server**:
it reads credentials and enforces them; it does not mint them.

## `gate.Wall()` — the Tenant wall, as a predicate

`server/gate` holds one rule: a Tenant is a wall.

- Nothing of another Tenant is visible. Not `PermissionDenied` but `NotFound`,
  since that it exists is itself something not to say.
- A Tenant is put up and taken down by the deployment, which is not something
  that happens from inside one — so both are `Unimplemented` here, to everybody.
- **There is no caller the wall is not about.** Nothing compares an identifier
  against a well-known one and answers "everything".

The first of those is **stated** in `server/gate` and **enforced** somewhere
else — on the innermost server, as a predicate in every query it builds:

```go
sink,   err := bare.NewServer(db, bare.WithRecorder(audit.NewRecorder()))
walled, err := bare.NewServer(db, bare.WithRecorder(audit.NewRecorder()), bare.WithScope(gate.Wall()))
s,      err := go_app.Build(walled, core.Build(), audit.Build(), gate.Build())
```

Narrowing what a caller may see is a predicate, and a predicate belongs in the
`WHERE`. Done from in front it is an override of `Get`, `Patch`, `Apply` and
`Erase`, once per entity and once more for every entity added afterwards. This
app had thirteen of them across three entities, and they had already started to
drift: one carried a bug that was fixed in one copy and left in the next.

`gate.Wall()` answers with a `bare.Scope` — one method per entity, all of them
the same shape: everything, or the rows that hang off the Tenants in scope. It
embeds `bare.Unscoped`, which is the generated "no opinion" for every entity, so
an entity added to the schema is not a compile error here. In this app that is
the wrong way round — everything is inside a Tenant — so `wall_test.go` asks
each method what it answers and fails when one of them says nothing.

Three things fall out of it rather than being written:

- **`NotFound`, not `PermissionDenied`.** A row out of the wall is a row the
  query does not match. Nothing has to remember to answer carefully.
- **A selection is the caller's alone.** The old wall read the Tenant of a row
  to decide, so it had to add that column to a selection that did not ask for it
  and take it back out afterwards — which meant the same request answered
  different rows depending on who sent it. Nothing reads the answer now.
- **A list is walled before it is cut short.** A limit taken across every Tenant
  and filtered afterwards is one that any Tenant can push the others out of by
  making a hundred rows of its own — and the victim sees an empty list, which
  reads like "nothing happened" rather than like an error. `List` is written by
  hand, so it borrows the same predicate (`bare.<Entity>Narrow`); that is the
  one read the generated servers do not make.

### Going around it is a server, not an identity

There used to be a superuser: whoever held a Tenant whose identifier this app
kept as a constant, told apart by a comparison in the middle of the wall. There
is not one now. A privilege granted by **being a particular row** cannot be
revoked, cannot be narrowed, does not appear anywhere it is used, and belongs to
whoever finds the row.

What the deployment has to do for itself, it does through a server the wall was
never installed on:

```go
sink,   err := bare.NewServer(db, ...)                              // no wall
walled, err := bare.NewServer(db, ..., bare.WithScope(gate.Wall()))

ungated := go_app.Build(sink,   core.Build(), audit.Build())                 // no gate either
served  := go_app.Build(walled, core.Build(), audit.Build(), gate.Build())
```

`cmd/serve.go` serves the second and uses the first for the two things that
cannot go through a wall: working out **who is calling**, which happens before
there is anybody to be walled by, and `EnsureRoot`, which puts the first Tenant
there before anybody exists at all. A deployment that wants an operator's path
serves the ungated stack somewhere only an operator can reach — a second port, a
unix socket, a separate binary.

The difference is the whole point: that capability is **a server instance
somebody was handed**, which can be withheld and taken away, rather than **a row
somebody is**, which cannot. `internal/ox` exposes it as `c.Ungated()`, and that
a test has to reach for it is the same fact from the other side.

The first Tenant is called `root` and is not privileged. It exists because a
deployment with no Tenant has nobody who can authenticate at all.

One consequence is worth knowing: erasing something out of the wall **succeeds
and erases nothing**, because erasing what is not there succeeds and out of the
wall is not there. It reads odd and it is the honest answer — an erase is
idempotent, and answering `NotFound` for a row that exists but is not yours
would tell a caller apart from the case where it never existed.

### What is left in `server/gate`

What is genuinely not a predicate — decisions about a row that does not exist
yet, where there is nothing to narrow:

| | |
| --- | --- |
| `Tenant.Add`, `Tenant.Erase` | `Unimplemented`, to everybody — the deployment does these |
| `Holder.Add` | the Tenant must be one the caller can read |

`Holder.Add` reads the Tenant **through the wall** rather than comparing a
reference against the scope, and answers `NotFound`. A reference names a Tenant
by identifier or by alias, and answering "is this one of mine" without a query
means holding every Tenant in scope in full — fine while that is the caller's
own, wrong as soon as it is a list a credential or a policy narrowed to.

## `gate.Policy` — what a deployment says about a caller

Roles, and who is bound to them, are dynamic, deployment-specific, and edited by
something that is not this app. `gate.Policy` is the seam, and it is
deliberately not implemented here:

```go
type Policy interface {
	May(ctx context.Context, c Call) error                     // a point
	Where(ctx context.Context, c Call) (frame.Tenants, error)  // a set
}

type Call struct {
	Actor  *go_app.Holder
	Action string          // "/go_app.HolderService/Patch"
}
```

**Two questions because there are two.** `May` answers whether a call may happen
at all, and is asked before the handler, so it must not need the row — a request
may name one by an alias, and resolving that is a query in front of the query.
Ask it about the *kind* of thing, which is what a method name already says.
`Where` answers which Tenants a caller may act in, and it is a set because **a
list is not a boolean**: asked for a boolean per row instead, a list has to fetch
rows it may not answer with and drop them, which cannot be paged and which any
Tenant can use to push another's rows out of an answer.

Both take the same `Call`, because both are asked at the same moment about the
same call and neither knows anything the other does not.

**There is no field for the row**, and that is not an omission. Anything about a
particular row is `Where`'s, and becomes a predicate.

### It is injected where the server is built

```go
opts = append(opts, auth_opts...)
opts = append(opts, gate.Interceptor(nil)...)   // or whatever a deployment consults
```

**Unset is not a placeholder.** A deployment that injects nothing behaves exactly
as this app always has: everybody sees their own Tenant, and nobody sees more.
Nothing here takes a dependency on a running service; the interface is the seam,
and an integration with a real engine belongs in a branch of its own — this
repository already keeps a branch per kind of app.

**It is asked once per request**, which is why it is an interceptor rather than
something the wall consults. The hooks `Wall()` installs run per *query*, and a
request makes several — a `Get` that also asks for the Tenant runs two. The
answer goes on the frame and everything after reads it, the same way `auth`
carries who the caller is.

And it is a function of the actor and the method with nothing of the request in
it, so it can be **held as a snapshot and evaluated in process** — which is what
Kubernetes does with RBAC, and what makes an authorization service that is
briefly unreachable not an outage. An implementation that does call out owes the
caller the same distinction `auth` makes: `Unavailable` when it could not find
out, which is not the caller's fault, rather than a refusal that reads as theirs.

The credential is met with the answer **afterwards**, because it can only take
away: a policy that grants a Tenant and a token that does not name it is a call
that does not reach it.

## Where each thing is decided

| question | decided by | when | carried as |
| --- | --- | --- | --- |
| who is this | `auth.Handler` + `auth.Resolver` | interceptor | `frame.Actor` |
| what did the credential allow | `auth.Handler` | interceptor | `frame.Grant` |
| may this call happen at all | `gate.Policy.May` | interceptor | — refused there |
| which Tenants may they see | `gate.Policy.Where`, or the wall | interceptor | `frame.Scope` |
| which rows, exactly | `gate.Wall()` | every query | a `WHERE` clause |
| may they create this | `server/gate` | the RPC | — refused there |

## What this is not

**Not an authorization server.** It does not issue credentials and it does not
define roles. Both belong to something a deployment already runs.

**Not Zanzibar, and the line is worth knowing.** A relationship-graph service
answers *point* questions — may this actor do this to this object — and answers
them by traversal. That is the right tool for permissions **derived over a deep
graph**: nested teams, folder inheritance, "viewer of the parent of the parent".
It is the wrong tool for a list, because a list needs a predicate and a
traversal cannot become one.

The dividing line is not "instance-level or not". It is:

> **Can the grant be a predicate in the query?**

A row in this database can — a collaborator table is instance-level and lists
fine. A graph in another service cannot, and every list built on it must either
fetch rows it may not show and discard them, or ask that service to enumerate
every object first. GitHub's fine-grained tokens are not Zanzibar either: they
are attenuation, which is [`frame.Grant`](#framegrant-what-the-credential-itself-allows).

So if a Zanzibar-like engine is added, it belongs **above** the wall and not
instead of it, and `gate.Tenants` being a set is where its answer would land:
"which Tenants may this caller see" is small and bounded, unlike "which rows".
If you find yourself reaching for `LookupResources` to build a list, that is the
signal that the wrong thing went into the graph.
