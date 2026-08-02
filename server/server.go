// Package server holds the pieces every server implementation shares.
//
// A server is a [go_app.Server], which is nothing but a set of service servers
// generated from the protobuf definitions. Implementations are stacked on top
// of each other: `server/bare` runs the queries against the database and every
// other package wraps it to add a behavior of its own, so a request walks the
// stack from the outermost server down to the bare one.
package server

import (
	"iter"

	go_app "github.com/lesomnus/go-app/go_app"
)

// Middleware is a server that delegates to another server.
type Middleware interface {
	Next() go_app.Server
}

// Iter yields `s` and, as long as they are middlewares, the servers behind it,
// from the outermost one to the one that handles the request.
func Iter(s go_app.Server) iter.Seq[go_app.Server] {
	return func(yield func(go_app.Server) bool) {
		for s != nil {
			if !yield(s) {
				return
			}

			mw, ok := s.(Middleware)
			if !ok {
				return
			}

			s = mw.Next()
		}
	}
}

// Find returns the outermost server in the stack that is a `T`.
func Find[T go_app.Server](s go_app.Server) (T, bool) {
	for s := range Iter(s) {
		if v, ok := s.(T); ok {
			return v, true
		}
	}

	var zero T
	return zero, false
}

// SinkOf returns the server the stack ends at, which is the one the others
// were built in front of and the only one that answers out of a database
// rather than by asking somebody else.
//
// It is the same server [Build] was given, and it is named the same way. What
// is at the end is not always what a caller means, though: reach for [Find]
// when there is a particular server in mind, and keep this for when the stack
// itself is the subject.
func SinkOf(s go_app.Server) go_app.Server {
	for v := range Iter(s) {
		s = v
	}
	return s
}

// Overlay is a middleware that forwards every service it does not override to
// the next server. Embed it to implement only the services of interest:
//
//	type Server struct {
//		server.Overlay
//	}
//
//	func (s Server) Tenant() go_app.TenantServiceServer { ... }
type Overlay struct {
	go_app.Server
}

func NewOverlay(next go_app.Server) Overlay {
	return Overlay{next}
}

func (s Overlay) Next() go_app.Server {
	return s.Server
}
