package cmd

import (
	"context"
	"log/slog"
	"time"

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
// The store is the reason they are split here -- the database in this app, and
// whatever it is in one that replaced it. Every deployment shares it, so a store
// that blinks makes every process answer the same way at the same moment.
// Answered as readiness that is a few seconds of failed calls; answered as
// liveness it is every process killed at once, restarted into a store that is
// still not there, and a restart loop that outlives the outage that started it.
// So the store moves readiness and nothing moves liveness.
//
// None of that reasoning is about databases, which is why what follows is not
// either. An app whose store is a queue, an API or a directory keeps this file
// and answers [Store] with that.
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
	// storeCheckInterval is how often the store is asked whether it is still
	// there, and storeCheckTimeout is how long it is given to answer.
	//
	// A database answers down a connection that is already open, so this is
	// cheap; a store reached over HTTP is a round trip, and an app with one of
	// those should slow this down rather than ask a gateway five times a
	// second all day.
	storeCheckInterval = 5 * time.Second
	storeCheckTimeout  = 3 * time.Second
)

// Store is whatever this app cannot serve without.
//
// One method, because readiness is one question: is it answering. What it is --
// a database, a controller, an API somebody else runs -- is not this file's
// business, and keeping it that way is what lets the reasoning above survive an
// app that changes its mind about the store.
type Store interface {
	Ping(ctx context.Context) error
}

// watchStore reports the app as ready for as long as the store answers, asking
// it every `every`.
func watchStore(ctx context.Context, h *health.Server, s Store, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()

	for {
		// The status is not ours to set once the server is shutting down.
		if v := storeStatus(ctx, s); ctx.Err() == nil {
			h.SetServingStatus(ServiceReadiness, v)
		}

		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// storeStatus is SERVING for as long as the store answers.
func storeStatus(ctx context.Context, s Store) grpc_health_v1.HealthCheckResponse_ServingStatus {
	ctx, cancel := context.WithTimeout(ctx, storeCheckTimeout)
	defer cancel()

	if err := s.Ping(ctx); err != nil {
		log.From(ctx).WarnContext(ctx, "the store is not reachable", slog.String("error", err.Error()))
		return grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	return grpc_health_v1.HealthCheckResponse_SERVING
}
