package audit

import (
	"context"
	"testing"

	"github.com/lesomnus/protobuf-patch/patch"
	"github.com/stretchr/testify/require"
)

// The two columns that may hold nothing hold no bytes rather than no value.
//
// This is here rather than in a test that writes a row because the difference
// does not show through one: the driver the tests run on writes a nil slice as
// an empty string, and the engine a deployment runs on is handed a NULL and
// refuses the row -- so the first thing that would have found it is the first
// startup, on the write the deployment makes to itself.
func TestBytesAreEmptyAndNotAbsent(t *testing.T) {
	x := require.New(t)

	t.Run("a write that was not traced", func(t *testing.T) {
		v := traceId(context.Background())
		x.NotNil(v)
		x.Empty(v)
	})

	t.Run("a write that was not a patch", func(t *testing.T) {
		v, err := document(nil)
		x.NoError(err)
		x.NotNil(v)
		x.Empty(v)
	})

	t.Run("and a document is kept whole", func(t *testing.T) {
		doc, err := patch.New("go_app.Holder",
			patch.Target(patch.Name("name")).Assign(patch.Str("Johnny")),
		)
		x.NoError(err)

		v, err := document(doc)
		x.NoError(err)
		x.NotEmpty(v)
	})
}
