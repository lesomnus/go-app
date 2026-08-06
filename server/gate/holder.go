package gate

import (
	"context"

	go_app "github.com/lesomnus/go-app/go_app"
)

type HolderServiceServer struct {
	Server
	go_app.HolderServiceServer
}

func NewHolderServiceServer(s Server) HolderServiceServer {
	return HolderServiceServer{s, s.Next().Holder()}
}

func (s Server) Holder() go_app.HolderServiceServer {
	return NewHolderServiceServer(s)
}

// Add is the one thing about a Holder this layer still says, and it is here
// because it is the one that is not a predicate: the row does not exist yet, so
// there is nothing to narrow. Reading one, changing one and erasing one are all
// [Wall].
func (s HolderServiceServer) Add(ctx context.Context, req *go_app.HolderAddRequest) (*go_app.Holder, error) {
	if _, err := actor(ctx); err != nil {
		return nil, err
	}

	t, err := Scope(ctx)
	if err != nil {
		return nil, err
	}
	if !t.Picks(req.GetTenant()) {
		return nil, errForbidden("add a holder to")
	}

	return s.HolderServiceServer.Add(ctx, req)
}
