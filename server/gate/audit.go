package gate

import (
	"bytes"
	"context"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/frame"
)

type AuditServiceServer struct {
	Server
	go_app.AuditServiceServer
}

func NewAuditServiceServer(s Server) AuditServiceServer {
	return AuditServiceServer{s, s.Next().Audit()}
}

func (s Server) Audit() go_app.AuditServiceServer {
	return NewAuditServiceServer(s)
}

// Get answers with a row of the trail if it is the caller's to see.
//
// The trail says who did something, so a row belongs to the Tenant the caller
// was held by when they did it. What was done to is not the question: whoever
// administers the deployment may write into any Tenant, and the row that says
// so is theirs, not that Tenant's. It is the same wall either way -- what one
// Tenant did is not visible from another.
func (s AuditServiceServer) Get(ctx context.Context, req *go_app.AuditGetRequest) (*go_app.Audit, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}

	// Whose it is has to be read to decide, so it is asked for whether the
	// caller wanted it or not, and taken back out if they did not.
	//
	// A request that names no selection at all is asking for the whole row, and
	// there is nothing to add to that. Adding to it anyway is not a no-op: a
	// selection that names one column is a selection that names *only* it, so
	// the row a caller got back would be that one column and the key. Which is
	// why this asks whether there is a selection before it edits one.
	sel := req.GetSelect()
	asked := sel.GetAll() || sel.GetTenantId()
	if !unbounded(f) && sel != nil && !asked {
		req.WithSelect(func(sel *go_app.AuditSelect) {
			sel.SetTenantId(true)
		})
	}

	v, err := s.AuditServiceServer.Get(ctx, req)
	if err != nil {
		return nil, err
	}
	if unbounded(f) {
		return v, nil
	}

	if !by(f, v) {
		return nil, errNotFound("Audit")
	}
	if !asked {
		v.SetTenantId(nil)
	}

	return v, nil
}

// List answers with the trail, less whatever belongs to another Tenant.
//
// Asked for rather than answered with, which is where this differs from
// `Holder.List`: the request carries whose trail it is, so the wall is part of
// the query and is applied before the answer is cut short. A wall that removed
// rows from what came back would be one that any Tenant could push another's
// trail out of by writing enough of its own.
func (s AuditServiceServer) List(ctx context.Context, req *go_app.AuditListRequest) (*go_app.AuditListResponse, error) {
	f, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if !unbounded(f) {
		// Whatever the caller said, which is the whole of the rule.
		req.SetTenantId(f.Tenant().GetId())
	}

	res, err := s.AuditServiceServer.List(ctx, req)
	if err != nil {
		return nil, err
	}
	if unbounded(f) {
		return res, nil
	}

	// Said twice on purpose. The query above is what makes the answer whole;
	// this is what makes it right if that ever stops being true, and it costs a
	// comparison per row of a list that is already bounded.
	vs := make([]*go_app.Audit, 0, len(res.GetItems()))
	for _, v := range res.GetItems() {
		if by(f, v) {
			vs = append(vs, v)
		}
	}

	return go_app.AuditListResponse_builder{Items: vs}.Build(), nil
}

// by reports whether the given row of the trail is one the caller's own Tenant
// wrote.
func by(f *frame.Frame, v *go_app.Audit) bool {
	return bytes.Equal(v.GetTenantId(), f.Tenant().GetId())
}
