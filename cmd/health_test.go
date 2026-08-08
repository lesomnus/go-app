package cmd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// store is one that can be turned off.
//
// A fake rather than a real database, and that is the point rather than a
// shortcut: what is being tested is which probe moves when the store stops
// answering, and that is true of every store. Reaching for a real one here
// would tie this test to the answer this branch happens to have.
type store struct{ up atomic.Bool }

func newStore() *store {
	s := &store{}
	s.up.Store(true)

	return s
}

func (s *store) Ping(context.Context) error {
	if s.up.Load() {
		return nil
	}

	return errors.New("not answering")
}

var _ Store = (*store)(nil)

func TestStoreStatus(t *testing.T) {
	x := require.New(t)

	s := newStore()
	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, storeStatus(t.Context(), s))

	// The app is not serving anything if the store is gone.
	s.up.Store(false)
	x.Equal(grpc_health_v1.HealthCheckResponse_NOT_SERVING, storeStatus(t.Context(), s))
}

// status asks the health server the way a probe does.
func status(t *testing.T, h *health.Server, service string) grpc_health_v1.HealthCheckResponse_ServingStatus {
	t.Helper()

	v, err := h.Check(t.Context(), &grpc_health_v1.HealthCheckRequest{Service: service})
	require.NoError(t, err)

	return v.GetStatus()
}

// TestStoreMovesReadinessOnly is the whole reason the two names are told apart.
// A store that is gone must send traffic elsewhere, and must not have every
// process killed and restarted into the same store it was killed for.
func TestStoreMovesReadinessOnly(t *testing.T) {
	x := require.New(t)

	s := newStore()

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
	go watchStore(ctx, h, s, every)

	reads := func(v grpc_health_v1.HealthCheckResponse_ServingStatus) func() bool {
		return func() bool { return status(t, h, ServiceReadiness) == v }
	}

	x.Eventually(reads(grpc_health_v1.HealthCheckResponse_SERVING), time.Second, every)
	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, status(t, h, ServiceLiveness))

	s.up.Store(false)

	x.Eventually(reads(grpc_health_v1.HealthCheckResponse_NOT_SERVING), time.Second, every)
	x.Equal(grpc_health_v1.HealthCheckResponse_SERVING, status(t, h, ServiceLiveness),
		"the store is not a reason to kill the process")
}
