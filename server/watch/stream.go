package watch

import (
	"context"

	"github.com/google/uuid"
)

// seen is which rows a stream has carried, so that one leaving the answer can
// be said and one that was never in it is not news.
//
// It grows with what a caller is shown rather than with what happens, which is
// what keeps it bounded by the filters they asked with.
type seen map[uuid.UUID]bool

// stream is the shape every Watch has, and the reason it is written once is the
// order rather than the length. Subscribing has to come before the first read
// or a change that lands between them is lost with nothing to say it was; that
// is easy to write correctly and easier to write correctly three times and then
// wrongly the fourth.
//
//   - `service` is the prefix of the RPCs whose writes this stream is about.
//   - `snapshot` sends what matches now, and is nil for an entity that has
//     nothing to snapshot; see `AuditServiceServer.Watch`.
//   - `send` reads the rows the keys name and sends what it should.
//
// It returns when the caller hangs up, when a send fails, or when this
// subscriber has fallen too far behind ([errBehind]).
func stream(
	ctx context.Context,
	w *Watch,
	service string,
	snapshot func(sent seen) error,
	send func(ks map[uuid.UUID]string, sent seen) error,
) error {
	// First, and before anything is read. See above.
	events, stop := w.subscribe()
	defer stop()

	sent := seen{}
	if snapshot != nil {
		if err := snapshot(sent); err != nil {
			return err
		}
	}

	for {
		ks, err := next(ctx, events, service)
		if err != nil {
			return err
		}

		if err := send(ks, sent); err != nil {
			return err
		}
	}
}
