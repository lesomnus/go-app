package server

import (
	"github.com/lesomnus/z"

	go_app "github.com/lesomnus/go-app/go_app"
)

// Builder makes a server that sits in front of `next`. Building may fail since
// a server is free to open the resources it needs, or to reject the settings
// it was given, while it is made.
type Builder interface {
	Build(next go_app.Server) (go_app.Server, error)
}

type BuilderFunc func(next go_app.Server) (go_app.Server, error)

func (f BuilderFunc) Build(next go_app.Server) (go_app.Server, error) {
	return f(next)
}

// Build stacks the given middlewares on top of `sink`. Each builder wraps the
// result of the previous one, so the last builder given handles the request
// first:
//
//	server.Build(bare.NewServer(db), core.Build(), log.Build())
//	// log -> core -> bare
//
// It stops at the first builder that fails, reporting it by its type.
func Build(sink go_app.Server, mws ...Builder) (go_app.Server, error) {
	s := sink
	for _, mw := range mws {
		v, err := mw.Build(s)
		if err != nil {
			return nil, z.Err(err, "%T", mw)
		}

		s = v
	}

	return s, nil
}
