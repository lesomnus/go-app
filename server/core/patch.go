package core

import (
	"github.com/lesomnus/protobuf-patch/patchpb"
	"github.com/protobuf-orm/protobuf-orm/graph"
	"github.com/protobuf-orm/protobuf-orm/ormpatch"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
)

// The entities the rules below are about.
//
// `Apply` carries a patch document instead of a field per prop, so the only way
// to know which column it writes is to compile it against the schema, which is
// what these describe. It is the same thing the generated server does, and it
// is done twice: compiling is cheap and reading a plan is the only way a rule
// in front can be about what the document actually does.
var (
	roasterEntity = ormpatch.MustEntityOf(go_app.File_go_app_roaster_proto, "Roaster")
	coffeeEntity  = ormpatch.MustEntityOf(go_app.File_go_app_coffee_proto, "Coffee")
)

// checkAlias refuses a patch document that writes an alias in a spelling other
// than the one it would be stored under.
//
// `Patch` normalizes instead of refusing, and the difference is on purpose. A
// request field is a value the caller wants stored, so trimming and lowercasing
// it is a courtesy. A document is not that: it states operations, and it can
// assert what it is about with a `test`, so a caller may well read a value
// back, test it, and write it. A server that quietly stored something else
// would make the next such test fail on a row nobody else had touched.
//
// So the stricter rule is the honest one here -- say it the way it will be
// stored, or do not say it -- and normalizing is left to the caller, which is
// where a document is written.
func checkAlias(e graph.Entity, doc *patchpb.Patch) error {
	if doc == nil {
		return nil
	}

	fd := e.Descriptor().Fields().ByName("alias")
	if fd == nil {
		return nil
	}

	// A document that does not compile is refused by the server behind this
	// one, which says why in terms of the document. Nothing here can do better,
	// so it is let through rather than reported twice, differently.
	plan, err := ormpatch.Compile(e, doc)
	if err != nil {
		return nil
	}

	w, ok := plan.WriteTo(fd.Number())
	if !ok {
		return nil
	}

	// The alias is a plain, non-nullable string column, so an assignment is the
	// only write it can take; compiling refuses the rest. Anything else that
	// arrives here is not this rule's to judge.
	op, ok := w.Op.(ormpatch.SetColumn)
	if !ok {
		return nil
	}
	was, ok := op.Value.Interface().(string)
	if !ok {
		return nil
	}

	v, err := ParseAlias(was)
	if err != nil {
		return err
	}
	if v != was {
		return status.Errorf(codes.InvalidArgument, "alias: %q is stored as %q, so say it that way", was, v)
	}

	return nil
}
