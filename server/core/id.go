package core

import (
	"bytes"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NobodyId is the identifier that names nobody, and so is one nothing may hold.
//
// A request may say what identifier it wants, which is what makes this worth a
// rule at all: the zero identifier is what a field nobody filled in looks like,
// and a row holding it would be the row every such field accidentally points
// at.
var NobodyId = uuid.Nil

// CheckId refuses an identifier a row may not be stored under. An empty one is
// no identifier at all, which is a request asking for whatever the database
// settles on, and that is allowed.
func CheckId(v []byte) error {
	if len(v) == 0 {
		return nil
	}
	if bytes.Equal(v, NobodyId[:]) {
		return status.Error(codes.InvalidArgument, "id: that one names nobody, so nothing may hold it")
	}

	return nil
}
