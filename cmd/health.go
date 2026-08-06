package cmd

import (
	"context"
	"log/slog"
	"time"

	"github.com/lesomnus/go-app/cmd/config"
	"github.com/lesomnus/otx/log"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// The two questions a health check is asked, which are not the same question
// and must not be answered together.
//
// A liveness probe asks whether the process is worth keeping, and a container
// runtime kills what says no. A readiness probe asks whether to send it work,
// and a load balancer routes around what says no. The difference is what the
// answer costs when it is wrong.
//
// The database is the reason they are split here. Every deployment shares one,
// so a database that blinks makes every process answer the same way at the
// same moment. Answered as readiness that is a few seconds of failed calls;
// answered as liveness it is every process killed at once, restarted into a
// database that is still not there, and a restart loop that outlives the
// outage that started it. So the database moves readiness and nothing moves
// liveness.
const (
	// ServiceReadiness is the empty name, which the health protocol gives to
	// the server as a whole and which is what a caller that names nothing
	// asks about. It stays the readiness one: a load balancer that was not
	// told which name to ask should route around a process that cannot serve,
	// and a liveness probe is the one that has to be configured on purpose.
	ServiceReadiness = ""

	// ServiceLiveness is answered for as long as the process is serving. It is
	// not a gRPC service name -- there is no service behind it -- and it does
	// not need to be, since the name is only how a probe says which question
	// it is asking.
	//
	//	livenessProbe:  { grpc: { port: 50051, service: liveness } }
	//	readinessProbe: { grpc: { port: 50051 } }
	ServiceLiveness = "liveness"
)

const (
	// dbCheckInterval is how often the database is asked whether it is still
	// there, and dbCheckTimeout is how long it is given to answer.
	dbCheckInterval = 5 * time.Second
	dbCheckTimeout  = 3 * time.Second
)

// watchDb reports the app as ready for as long as the database answers, asking
// it every `every`.
func watchDb(ctx context.Context, h *health.Server, db *config.Db, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		// The status is not ours to set once the server is shutting down.
		if s := dbStatus(ctx, db); ctx.Err() == nil {
			h.SetServingStatus(ServiceReadiness, s)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// dbStatus is SERVING for as long as the database answers.
func dbStatus(ctx context.Context, db *config.Db) grpc_health_v1.HealthCheckResponse_ServingStatus {
	ctx, cancel := context.WithTimeout(ctx, dbCheckTimeout)
	defer cancel()

	if err := db.Ping(ctx); err != nil {
		log.From(ctx).WarnContext(ctx, "database is not reachable", slog.String("error", err.Error()))
		return grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	return grpc_health_v1.HealthCheckResponse_SERVING
}
