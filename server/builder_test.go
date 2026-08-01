package server_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server"
)

// fake is a middleware that does nothing but tell where it is in the stack.
type fake struct {
	server.Overlay
	name string
}

func build(name string, err error) server.Builder {
	return server.BuilderFunc(func(next go_app.Server) (go_app.Server, error) {
		if err != nil {
			return nil, err
		}

		return fake{server.NewOverlay(next), name}, nil
	})
}

func names(s go_app.Server) []string {
	vs := []string{}
	for s := range server.Iter(s) {
		if v, ok := s.(fake); ok {
			vs = append(vs, v.name)
		}
	}

	return vs
}

func TestBuild(t *testing.T) {
	t.Run("the last builder handles the request first", func(t *testing.T) {
		x := require.New(t)

		sink := go_app.UnimplementedServer{}
		s, err := server.Build(sink, build("core", nil), build("log", nil))
		x.NoError(err)
		x.Equal([]string{"log", "core"}, names(s))
		x.Equal(sink, server.TerminalOf(s))
	})
	t.Run("stops at the first failure", func(t *testing.T) {
		x := require.New(t)

		expected := errors.New("cannot be built")
		built := 0
		count := server.BuilderFunc(func(next go_app.Server) (go_app.Server, error) {
			built++
			return next, nil
		})

		_, err := server.Build(go_app.UnimplementedServer{}, count, build("log", expected), count)
		x.ErrorIs(err, expected)
		x.Equal(1, built)
	})
}
