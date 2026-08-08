# Who is calling, and what they may do

Two questions, kept apart because they change for different reasons and are
answered by different things.

```
  a request  --->  server/auth   who is this? and what did the credential allow?
                        |
                        v
                   server/gate   may they make this call?
                        |
                        v
                   the servers,  which ask neither again
```

**Every request has an answer to the first one**, including the ones from
nobody. A caller who presents no credential is `frame.Anonymous`, which is a
caller like any other rather than the absence of one.

That is worth saying twice, because it is where this sort of thing goes wrong. A
request with no frame is a question nothing has an answer to, and every answer
that suggests itself is bad: refuse it, and the app's own calls have to go around
the layer that refuses; serve it as nobody-in-particular, and the code deciding
what nobody-in-particular may do is somewhere else, unwritten, defaulting to
whatever a zero value means. Making the anonymous caller a caller means the
question is asked and answered in one place, for everybody.

## `server/auth` — who is this

A `Handler` reads whatever the transport carries and says who the caller is.
Three are written, and they differ in one thing: where the name comes from.

| | | |
| --- | --- | --- |
| `plain` | `authorization: Plain anna` | Believed. Nothing is checked, which is the point; it is for development and for tests, and the server says so out loud at startup. |
| `mtls` | the client certificate | Nothing is checked here either, and here that is right — the handshake already did it. Read out of the **verified chain**, never out of what the peer merely sent. |
| `bearer` | `authorization: Bearer …` | The only one that has to *ask* something, which is what makes it the interesting one. |

**There is no second step and no user table.** This app has no users — it has
Coffees — so who somebody is comes from outside: a token's subject, a
certificate's name, a header in development. `frame.Actor.Subject` is an opaque
string and nothing here compares it with anything but itself. A deployment with
more than one issuer makes the subject say which, since two issuers can call two
different people the same thing.

### `auth.TokenStore` is where an issuer is injected

A header and a certificate carry a name. A token carries nothing, so somebody
has to be asked — and being asked can fail in a way the caller did not cause.

```go
type TokenStore interface {
	Lookup(ctx context.Context, token string) (frame.Actor, frame.Grant, error)
}
```

What is bundled is a map (`auth.MemTokenStore`), and it is a sample of the
*shape*: tokens held as digests so the store cannot give away what it was told,
and a life of its own that is not its subject's. What a deployment has is a JWT
it verifies, an introspection endpoint, or a table somebody else owns. None of
that belongs in this app — it reads credentials and enforces them, and does not
mint them.

**Three answers, not two.** A credential that is absent falls through to the next
handler. One that is *wrong* stops the search — serving somebody as whatever the
next handler makes of them would be answering a question nobody asked. And one
the store could not check answers `Unavailable`, not `Unauthenticated`: told the
latter, a caller throws away a token that is perfectly good and goes to get
another one from the thing that is already down.

### `frame.Grant` — what the credential allowed

A token may allow **less** than its subject may, and never more. One axis: the
set of methods it is for.

```go
frame.Whole()                                  // a header, a certificate
frame.To("/go_app.CoffeeService/Get")          // a token that may only read
```

**The zero value allows nothing.** A store that answers with a Grant it forgot to
fill in hands out a credential that can do nothing, which somebody notices
immediately; the other way round it hands out one that can do everything, which
nobody notices at all.

One axis and not two, because a permission set that varied per resource would be
a policy, and a policy is not something a credential should be carrying around.
GitHub's fine-grained tokens do not do it either.

## `server/gate` — what they may do

One rule of this app's own:

> an anonymous caller may make the calls that were named, and no others.

`server.allow_anonymous_reads` names the reads this app generates — `Get`,
`List`, `Watch` — which is the catalogue shape: anybody may read it, and only a
caller who said who they are may change it. Unwritten, an anonymous caller may
do nothing.

**A list of what is allowed, not a list of what is not.** The other way round
reads the same today and is wrong the day somebody writes `Rename`: it is a
write, it is not spelled `Patch`, and it would have been open to everybody with
nothing anywhere to say so.

The refusal is `Unauthenticated` and not `PermissionDenied`, because the two say
different things to do about it — this one is fixed by saying who you are.

### `gate.Policy` — what a deployment says

Everything finer is a deployment's, and this is the seam. It is **unset by
default**, and that is not a placeholder: a deployment that injects nothing gets
exactly what is written above.

```go
type Policy interface {
	May(ctx context.Context, c Call) error
}

type Call struct {
	Actor  frame.Actor
	Action string          // "/go_app.CoffeeService/Patch"
}
```

**It is asked once per request, before the handler**, which is why there is no
field for the row: a request may name one by an alias, and resolving that is a
query in front of the query. Ask it about the *kind* of thing, which is what a
method name already says.

A rule about a *particular row* is a different shape of thing and does not belong
in an interceptor at all. It belongs in the query, as a predicate — `bare.Scope`
is the hook the generated servers put one into every read they build. **This
branch installs none**: nothing here narrows what a caller may see, and every row
is everybody's to read. An app whose rows belong to somebody installs one there
rather than asking per row; see the `kind/server` branch, whose whole subject is
that.

And the answer is a function of the actor and the method with nothing of the
request in it, so it can be **held as a snapshot and evaluated in process** —
which is what Kubernetes does with RBAC, and what makes an authorization service
that is briefly unreachable not an outage.

### `server/gate/roles` — what one looks like

A sample implementation, and nothing wires it in. Roles name actions, bindings
give a subject a role, and the whole table is swapped at once:

```go
p, _ := roles.New(roles.Table{
	Roles: map[string]roles.Role{
		"reader": {Actions: []string{"/go_app.CoffeeService/Get"}},
		"admin":  {Actions: []string{"/go_app.CoffeeService/*"}},
	},
	Bindings: []roles.Binding{{Subject: "anna", Role: "admin"}},
})
opts = append(opts, gate.Interceptor(gate.WithPolicy(p))...)
p.Store(next)   // whenever the engine that produces the table says so
```

`Store` swaps it and no request ever waits on anything, which is the property
that makes the seam usable: a request answered from the last table that arrived
is a request answered while the engine that produces them is down.

## Where the trust boundary is

`plain` believes whatever it is told. It must not be reachable by anyone who is
not already trusted to say the truth — behind a gateway that strips the header,
on a socket only the deployment can reach, or in development. The server logs a
warning at startup when it is on, and that warning is the whole of what stops
somebody shipping it.

The trace context a caller sends is the caller's word too; see the README,
"Whose trace is it".

## The summary table

| question | who answers | when | what it becomes |
| --- | --- | --- | --- |
| who is this | `server/auth` | interceptor | `frame.Actor`, or `frame.Anonymous` |
| what did the credential allow | `frame.Grant` | interceptor | a refusal, or nothing |
| may an anonymous caller do this | `gate.Anonymous` | interceptor | `Unauthenticated`, or nothing |
| may *this* caller do this | `gate.Policy` | interceptor | whatever it says |
| which rows | — | — | nothing here; `bare.Scope` is the hook |
