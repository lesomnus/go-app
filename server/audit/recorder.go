package audit

import (
	"context"

	"github.com/google/uuid"
	"github.com/lesomnus/protobuf-patch/patchpb"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/bare"
	"github.com/lesomnus/go-app/server/frame"
)

var _ bare.Recorder = Recorder{}

// Recorder writes one row of the trail for every write the generated servers
// make.
//
// It is not a layer of the stack and it does not override an RPC. The servers
// call it from inside the transaction that makes the write, so every RPC that
// changes anything is on the trail without anybody having listed them, and one
// added later is on it without anybody remembering to.
type Recorder struct{}

func NewRecorder() Recorder {
	return Recorder{}
}

// Record writes what happened. It runs inside the write's transaction, so an
// error here takes the write with it -- which is the answer we want: a write
// that could not be accounted for did not happen.
//
// With one caveat that is not this package's to fix. When the server that
// called this had joined a transaction somebody else began, rather than opened
// one of its own, undoing it is not its to do: the refusal is reported and
// whoever began the transaction decides whether the whole of it still holds. A
// caller that puts several writes on one transaction and carries on past a
// failed one commits a change that nothing recorded. See [bare.Recorder].
func (Recorder) Record(ctx context.Context, s bare.Server, c bare.Change) error {
	object, err := identifier(c.Key)
	if err != nil {
		return err
	}

	doc, err := document(c.Patch)
	if err != nil {
		return status.Errorf(codes.Internal, "hold the patch of %s: %s", c.By, err)
	}

	// The zero identifier for a write nobody asked for, which is what the
	// deployment does to itself before it serves anything; see core.EnsureRoot.
	var actor, tenant uuid.UUID
	if f, ok := frame.From(ctx); ok {
		actor = identifierOf(f.Actor.GetId())
		tenant = identifierOf(f.Tenant().GetId())
	}

	// Through the server it was handed, which runs on the transaction of the
	// write it is about and does not record: see [bare.Recorder].
	_, err = s.Audit().Add(ctx, go_app.AuditAddRequest_builder{
		TenantId: tenant[:],
		ActorId:  actor[:],
		TraceId:  traceId(ctx),

		// What the caller asked for, which is what a trail is read for. The
		// write also says which of these servers made it (`c.By`), and that
		// is not what goes here: one request writes through several of them
		// -- adding a Tenant writes the admin Holder that comes with it --
		// and an RPC written by hand ends in a Patch nobody called by that
		// name. "Who renamed this" has to answer with Rename.
		Action:   c.Method,
		ObjectId: object,
		Patch:    doc,
	}.Build())
	if err != nil {
		return err
	}

	return nil
}

// document is the patch as the trail holds it, byte for byte. A trail is
// written once and read by somebody asking what happened, so it keeps what was
// said rather than a reading of it; whoever asks unmarshals it. There is none
// for an Add or an Erase, which say all they did in the action and the object.
//
// No bytes, then, and never no value. The column holds bytes and says it always
// has some, and a nil slice reaches the database as NULL through one driver and
// as an empty string through another -- a difference that does not show up
// until the engine that is not the one the tests run on refuses the row.
func document(v *patchpb.Patch) ([]byte, error) {
	if v == nil {
		return []byte{}, nil
	}

	return proto.Marshal(v)
}

// identifier is the key of the row that changed, as the trail holds one.
//
// Every entity of this app is keyed by an identifier, and the column says so.
// A schema that grows one keyed otherwise is a decision about what the trail
// should hold for it, so it is refused here rather than written as something
// that is not an identifier.
func identifier(v any) ([]byte, error) {
	k, ok := v.(uuid.UUID)
	if !ok {
		return nil, status.Errorf(codes.Internal, "a key of %T is not an identifier the trail can hold", v)
	}

	return k[:], nil
}

// identifierOf reads an identifier out of what a message carries it as, and
// answers with the zero one for anything that is not one.
//
// Nothing here is worth failing a write over. The trail records who did it, and
// "nobody it could name" is a truthful answer to that -- refusing the write
// instead would take down the thing being recorded because of the record.
func identifierOf(v []byte) uuid.UUID {
	k, err := uuid.FromBytes(v)
	if err != nil {
		return uuid.Nil
	}

	return k
}

// traceId is the trace the request belonged to, and no bytes at all for a write
// that was not traced -- which is every write made before the server is serving.
//
// Empty rather than nil, for the reason the document above is.
func traceId(ctx context.Context) []byte {
	v := trace.SpanContextFromContext(ctx)
	if !v.IsValid() {
		return []byte{}
	}

	id := v.TraceID()
	return id[:]
}
