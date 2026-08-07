package watch

import (
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	go_app "github.com/lesomnus/go-app/go_app"
	"github.com/lesomnus/go-app/server/audit"
)

var auditService = strings.TrimSuffix(go_app.AuditService_Get_FullMethodName, "Get")

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

// Watch answers with the trail as it is written.
//
// It is the one watch in this app that is not about state, and every difference
// from the other two comes from that. A trail row is written once, never
// changed and never erased, so:
//
//   - **There is no first message.** There is nothing to converge on, and the
//     whole history is not something to send to whoever opens a stream. What
//     happened before is what `List` is for.
//   - **There is nothing to say by absence.** A row does not stop being a row,
//     so [seen] is never read and the answer is rows rather than the wrapper
//     the other two use.
//   - **Reading it back is still what walls it.** The row is fetched through
//     the servers behind this one with this caller's context, so a row of a
//     Tenant they may not see comes back NotFound and is never sent -- which
//     for a trail means a row whose *actor* was elsewhere; see `Audit.tenant_id`.
func (s AuditServiceServer) Watch(req *go_app.AuditWatchRequest, out grpc.ServerStreamingServer[go_app.AuditWatchResponse]) error {
	ctx := out.Context()

	if n := len(req.GetFilters()); n > audit.FilterLimit {
		return status.Errorf(codes.InvalidArgument,
			"filters: %d of them, and %d is the most one watch carries", n, audit.FilterLimit)
	}

	return stream(ctx, s.w, auditService, nil,
		func(ks map[uuid.UUID]string, _ seen) error {
			items := make([]*go_app.Audit, 0, len(ks))
			for k := range ks {
				v, err := s.AuditServiceServer.Get(ctx, go_app.AuditGetById(k[:]))
				if err != nil {
					if status.Code(err) == codes.NotFound {
						// Not this caller's to read.
						continue
					}

					return err
				}
				if !matchesAudit(req, v) {
					continue
				}

				items = append(items, v)
			}
			if len(items) == 0 {
				return nil
			}

			return out.Send(go_app.AuditWatchResponse_builder{Items: items}.Build())
		})
}

// matchesAudit is the filtering of `audit.List` written again in Go; see
// [matchesHolder] for why that is said out loud rather than hidden.
func matchesAudit(req *go_app.AuditWatchRequest, v *go_app.Audit) bool {
	// The Tenant, which narrows before the filters do -- the same way it does
	// in the list. Nothing said is every Tenant this caller may see, which the
	// read has already settled.
	if k := req.GetTenantId(); len(k) > 0 && !equalId(k, v.GetTenantId()) {
		return false
	}

	fs := req.GetFilters()
	if len(fs) == 0 {
		return true
	}

	for _, f := range fs {
		if equalId(f.GetObjectId(), v.GetObjectId()) {
			return true
		}
	}

	return false
}

func equalId(a, b []byte) bool {
	return len(a) == len(b) && string(a) == string(b)
}
