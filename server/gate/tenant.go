package gate

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	go_app "github.com/lesomnus/go-app/go_app"
)

type TenantServiceServer struct {
	Server
	go_app.TenantServiceServer
}

func NewTenantServiceServer(s Server) TenantServiceServer {
	return TenantServiceServer{s, s.Next().Tenant()}
}

func (s Server) Tenant() go_app.TenantServiceServer {
	return NewTenantServiceServer(s)
}

// errDeployment is the answer to putting a Tenant up or taking one down.
//
// A Tenant is the wall every other rule is about, so neither of these is
// something that happens from inside one -- and from behind this layer, inside
// one is the only place there is. Both are refused to everybody, the way the
// trail refuses its own writes, and for the same reason: this is not about who
// is asking, and no credential changes it.
//
// What does them is a server this layer is not in front of. `cmd/serve.go`
// builds one -- it is what puts the first Tenant there before anything is
// served -- and a deployment that wants an operator's path serves that one
// somewhere only an operator can reach. The capability is then a server
// instance somebody was handed, which can be taken away, rather than a row
// somebody is, which cannot.
func errDeployment(what string) error {
	return status.Errorf(codes.Unimplemented,
		"a tenant is %s by whoever runs this deployment, which is not something asked for from inside one", what)
}

// Add is not served. A Tenant is put up by the deployment; see [errDeployment].
func (s TenantServiceServer) Add(ctx context.Context, req *go_app.TenantAddRequest) (*go_app.Tenant, error) {
	return nil, errDeployment("put up")
}

// Erase is not served either, and it would take everything in the Tenant with
// it; see [core.TenantServiceServer.Erase].
func (s TenantServiceServer) Erase(ctx context.Context, req *go_app.TenantRef) (*emptypb.Empty, error) {
	return nil, errDeployment("taken down")
}
