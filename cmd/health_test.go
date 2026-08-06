package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3/vfs/memdb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/lesomnus/go-app/cmd/config"
)

func TestDbStatus(t *testing.T) {
	x := require.New(t)

	c := config.DbConfig{Driver: "sqlite3", Dsn: memdb.TestDB(t)}
	db, err := c.Open(t.Context())
	x.NoError(err)

	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, dbStatus(t.Context(), db))

	// The app is not serving anything if the database is gone.
	x.NoError(db.Close())
	x.Equal(grpc_health_v1.HealthCheckResponse_NOT_SERVING, dbStatus(t.Context(), db))
}

// status asks the health server the way a probe does.
func status(t *testing.T, h *health.Server, service string) grpc_health_v1.HealthCheckResponse_ServingStatus {
	t.Helper()

	v, err := h.Check(t.Context(), &grpc_health_v1.HealthCheckRequest{Service: service})
	require.NoError(t, err)

	return v.GetStatus()
}

// TestDbMovesReadinessOnly is the whole reason the two names are told apart. A
// database that is gone must send traffic elsewhere, and must not have every
// process killed and restarted into the same database it was killed for.
func TestDbMovesReadinessOnly(t *testing.T) {
	x := require.New(t)

	c := config.DbConfig{Driver: "sqlite3", Dsn: memdb.TestDB(t)}
	db, err := c.Open(t.Context())
	x.NoError(err)

	h := health.NewServer()
	h.SetServingStatus(ServiceLiveness, grpc_health_v1.HealthCheckResponse_SERVING)

	// The health server is born with the empty name already SERVING, so it is
	// told to say something else first. Without this the wait below is
	// answered by the constructor and passes without the watch having run at
	// all -- a test that holds whatever the watch does, including nothing.
	h.SetServingStatus(ServiceReadiness, grpc_health_v1.HealthCheckResponse_UNKNOWN)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	const every = time.Millisecond
	go watchDb(ctx, h, db, every)

	reads := func(s grpc_health_v1.HealthCheckResponse_ServingStatus) func() bool {
		return func() bool { return status(t, h, ServiceReadiness) == s }
	}

	x.Eventually(reads(grpc_health_v1.HealthCheckResponse_SERVING), time.Second, every)
	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, status(t, h, ServiceLiveness))

	x.NoError(db.Close())

	x.Eventually(reads(grpc_health_v1.HealthCheckResponse_NOT_SERVING), time.Second, every)
	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, status(t, h, ServiceLiveness),
		"the database is not a reason to kill the process")
}
