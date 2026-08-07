// Package watch publishes what a call changed, once its transaction has
// committed.
//
// It is the other end of `server/audit`. Both are told about a write by the
// same hook -- `bare.Recorder`, called from inside the transaction that makes
// it -- and they want opposite things from that moment. A trail row has to hold
// or fall *with* the write, so it is written there and then. An event has to be
// published only if the write survived, so nothing is published there at all:
// what the recorder does is remember, and the interceptor publishes once the
// handler has answered without an error.
//
// # What "the transaction committed" means here
//
// The handler returning is the signal. Every transaction a call opens is opened
// and committed below the interceptor -- the generated servers open one per
// write, `core.TenantServiceServer.Erase` opens one across several -- so by the
// time the handler answers, whatever it wrote is committed or gone with the
// error.
//
// A layer that held a transaction open past its own handler would break that,
// and nothing here does. If one is written, it is the thing to reread this for.
//
// # Why the events are dropped rather than waited on
//
// The signal is a [github.com/lesomnus/signals.Hard] one: a subscriber that is
// not keeping up has its channel closed and stops being a subscriber. That is
// the only one of the three that is safe here. `Sure` would block the request
// path until the slowest watcher caught up, which turns a slow consumer into a
// slow server; `Soft` would silently skip an event and leave a watcher believing
// it has seen everything, which is worse than being cut off. Being disconnected
// is a thing a client can notice and act on -- it reconnects and reads what it
// missed from somewhere durable.
//
// Which is the caveat worth saying plainly: **this is not an outbox.** An event
// is published in this process, to whoever is listening in this process, and a
// crash between the commit and the dispatch loses it. Something that must not
// be lost is written in the transaction, as a row somebody else picks up.
package watch

import (
	"context"
	"sync"

	"github.com/lesomnus/signals"
	"github.com/lesomnus/z"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/frame"
)

// use carries the buffer one call fills; see [changes].
var use = z.NewUse[*changes]()

// Event is one call that changed something.
//
// Every message in it is shared with every subscriber and with the call that
// produced it. **Read it; do not write to it.** A subscriber that needs to keep
// one keeps a copy.
type Event struct {
	// Actor is who made the call, and is nil for a call nobody vouched for --
	// the deployment writing to itself before it serves anything.
	Actor *go_app.Holder

	// Method is the RPC gRPC dispatched, which is what the caller asked for.
	// What each write actually did is in [Event.Changes].
	Method string

	// Request and Response are what the call carried. The response of an Add,
	// a Patch or an Apply is the row as it was written, which is the whole
	// content of the thing the request was about.
	Request  proto.Message
	Response proto.Message

	// Changes is every write the call made, in the order it made them, as the
	// servers that made them saw them. A call writes more than one row often
	// enough to be the normal case -- adding a Tenant writes the admin Holder
	// that comes with it -- and only the first of them is the response.
	//
	// The row itself is not read back. It is a query per write, inside the
	// transaction that is trying to commit, for something the caller usually
	// has already: whoever wants the full content of a row that was not the
	// response reads it by the key here.
	Changes []bare.Change
}

// Watch is the two halves of publishing an event, which are installed in two
// different places and are useless apart.
//
// [Watch.Recorder] goes on the innermost server, where writes happen.
// [Watch.Interceptor] goes on the gRPC server, where calls end. Install the
// interceptor alone and every event has an empty [Event.Changes]; install the
// recorder alone and nothing is published at all, since there is nowhere for it
// to remember into. Neither is an error and both are wiring somebody can read;
// see `cmd/serve.go`.
type Watch struct {
	to signals.Dispatcher[Event]
}

// New answers with the two halves, publishing into `to`.
func New(to signals.Dispatcher[Event]) *Watch {
	return &Watch{to: to}
}

// Signal answers with a signal shaped the way this package needs one, for a
// deployment that has no opinion of its own. See the package comment for why it
// is the hard one, and [signals.Subscriber.Subscribe] for how far behind a
// subscriber may fall before it finds out.
func Signal() signals.Signal[Event] {
	return signals.Hard[Event]()
}

var _ bare.Recorder = Recorder{}

// Recorder remembers what a call wrote, so that the interceptor can publish it
// once the call has succeeded.
//
// It never refuses. Every `bare.Recorder` is required by default -- the write
// fails with it -- and that is right for a trail and wrong for this: an event
// nobody could publish is not a reason to undo the thing it was about. So this
// is the best-effort kind, and says so by answering nil.
type Recorder struct{}

func (w *Watch) Recorder() Recorder { return Recorder{} }

func (Recorder) Record(ctx context.Context, _ bare.Server, c bare.Change) error {
	if v, ok := use.From(ctx); ok {
		v.add(c)
	}

	return nil
}

// Interceptor publishes one event per call that changed something.
//
// Unary only, and for the reason [grpcx.Deadline] is: a stream has no single
// response to publish and no single moment to publish it at. Every RPC this app
// generates is unary, and a stream that wants to say what it did says it
// itself.
func (w *Watch) Interceptor() []grpc.ServerOption {
	return []grpc.ServerOption{grpc.ChainUnaryInterceptor(w.Unary())}
}

func (w *Watch) Unary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		vs := &changes{}
		ctx = use.Into(ctx, vs)

		res, err := handler(ctx, req)
		if err != nil {
			// Nothing is published for a call that failed. What it wrote was
			// undone with it -- or, for a call that joined a transaction
			// somebody else began, is theirs to undo; see [bare.Recorder].
			return res, err
		}

		w.publish(ctx, req, res, info.FullMethod, vs.taken())
		return res, nil
	}
}

// publish sends the event, and sends none for a call that changed nothing. A
// read is most of what a server does and is not news.
func (w *Watch) publish(ctx context.Context, req, res any, method string, cs []bare.Change) {
	if w.to == nil || len(cs) == 0 {
		return
	}

	v := Event{
		Method:   method,
		Request:  message(req),
		Response: message(res),
		Changes:  cs,
	}
	if f, ok := frame.From(ctx); ok {
		v.Actor = f.Actor
	}

	// Detached from cancellation. The write already happened, and a caller who
	// hung up is not a reason for it to go unannounced -- which is the same
	// reason the record of a call is written with a context that outlives it.
	//
	// What it answers with is deliberately dropped. The hard signal cuts off a
	// subscriber that cannot keep up and says so by answering the count it
	// reached; there is nobody here for whom that is news, and it is not a
	// reason to fail a call that has already succeeded.
	_, _ = w.to.Dispatch(context.WithoutCancel(ctx), v)
}

// message is `v` as something a subscriber can read, and nothing for a value
// that is not a message. Every request and response of this app is one; the
// signature is `any` because that is what an interceptor is handed.
func message(v any) proto.Message {
	m, _ := v.(proto.Message)
	return m
}

// changes is what one call wrote, gathered as it wrote it.
//
// Guarded, because nothing promises a handler writes from one goroutine. It is
// held by pointer in the context so that a recorder running several layers down
// adds to what the interceptor will read.
type changes struct {
	mu sync.Mutex
	vs []bare.Change
}

func (c *changes) add(v bare.Change) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.vs = append(c.vs, v)
}

// taken answers with what was written and leaves nothing behind, so that the
// slice handed to every subscriber is one nothing else appends to.
func (c *changes) taken() []bare.Change {
	c.mu.Lock()
	defer c.mu.Unlock()

	vs := c.vs
	c.vs = nil

	return vs
}
