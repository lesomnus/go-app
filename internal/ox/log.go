package ox

import (
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// logger returns a logger that writes to the log of the test, so that whatever
// the server says while a test runs is attached to that test and is shown only
// if it fails. It is where a recovered panic and its stack end up.
func logger(tb testing.TB) *slog.Logger {
	w := &tbWriter{tb: tb}
	tb.Cleanup(w.close)

	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// tbWriter writes to the log of a test until that test is over, since logging
// to a test that has finished is a panic.
type tbWriter struct {
	mu   sync.Mutex
	tb   testing.TB
	done bool
}

func (w *tbWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.done {
		w.tb.Log(strings.TrimRight(string(p), "\n"))
	}

	return len(p), nil
}

func (w *tbWriter) close() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.done = true
}
