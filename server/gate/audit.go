package gate

import (
	go_app "github.com/lesomnus/go-app/go_app"
)

type AuditServiceServer struct {
	Server
	go_app.AuditServiceServer
}

func NewAuditServiceServer(s Server) AuditServiceServer {
	return AuditServiceServer{s, s.Next().Audit()}
}

// Audit says nothing, and that is the state of it: whose trail a row belongs to
// is [Wall], and writing one is refused by `server/audit` to everybody.
//
// It used to say two things. Get read the row, compared its Tenant to the
// caller's and answered NotFound -- which the wall now does in the query, and
// does better: the row never leaves the database. List pushed the caller's
// Tenant into the request so that the wall was part of the query rather than a
// filter over the answer, because a filter over a list that is cut off at a
// limit is one any Tenant can push another's trail out of by writing enough
// rows of its own. That reasoning was right and is now the wall's, which is
// where it holds for every read rather than for the one that remembered.
func (s Server) Audit() go_app.AuditServiceServer {
	return NewAuditServiceServer(s)
}
