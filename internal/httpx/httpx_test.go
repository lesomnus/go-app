package httpx_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/lesomnus/go-app/internal/httpx"
)

// get asks the handler and answers with the status and the body.
func get(t *testing.T, h http.Handler, path string) (int, string) {
	t.Helper()

	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))

	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	return res.Code, string(body)
}

func TestHealth(t *testing.T) {
	t.Run("answers what the grpc one answers", func(t *testing.T) {
		x := require.New(t)

		s := health.NewServer()
		s.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
		s.SetServingStatus("liveness", grpc_health_v1.HealthCheckResponse_SERVING)

		h := httpx.Handler(httpx.Options{Health: s})

		// Out of the same server, which is the point: two probes of one process
		// that can disagree are worse than one probe.
		code, body := get(t, h, "/healthz")
		x.Equal(http.StatusOK, code)
		x.Contains(body, "SERVING")

		code, _ = get(t, h, "/healthz/liveness")
		x.Equal(http.StatusOK, code)
	})

	t.Run("not serving is 503 and not 500", func(t *testing.T) {
		x := require.New(t)

		s := health.NewServer()
		s.SetServingStatus("", grpc_health_v1.HealthCheckResponse_NOT_SERVING)

		// Nothing is broken here. This process is saying it is not the one to
		// send the traffic to, which is what a load balancer reads.
		code, _ := get(t, httpx.Handler(httpx.Options{Health: s}), "/healthz")
		x.Equal(http.StatusServiceUnavailable, code)
	})

	t.Run("a name nothing was registered under is not found", func(t *testing.T) {
		x := require.New(t)

		s := health.NewServer()
		s.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

		code, _ := get(t, httpx.Handler(httpx.Options{Health: s}), "/healthz/nothing")
		x.Equal(http.StatusNotFound, code)
	})

	t.Run("there is no such path without a health server", func(t *testing.T) {
		x := require.New(t)

		code, _ := get(t, httpx.Handler(httpx.Options{}), "/healthz")
		x.Equal(http.StatusNotFound, code)
	})
}

func TestPprof(t *testing.T) {
	t.Run("is not served unless it is asked for", func(t *testing.T) {
		x := require.New(t)

		// The heap, the goroutines, and the ability to make the process spend
		// thirty seconds profiling itself.
		code, _ := get(t, httpx.Handler(httpx.Options{}), "/debug/pprof/")
		x.Equal(http.StatusNotFound, code)

		code, _ = get(t, httpx.Handler(httpx.Options{Pprof: true}), "/debug/pprof/")
		x.Equal(http.StatusOK, code)
	})

	t.Run("the default mux has it whatever this says, which is why nothing serves that", func(t *testing.T) {
		x := require.New(t)

		// `net/http/pprof` registers itself on `http.DefaultServeMux` from its
		// init, so importing it is enough -- the switch above decides only
		// whether it is on *our* mux. This is what makes "never serve the
		// default mux" a rule rather than a preference, and it is here so that
		// the day it stops being true, this says so.
		res := httptest.NewRecorder()
		http.DefaultServeMux.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
		x.Equal(http.StatusOK, res.Code)

		// And what this app serves is its own mux, which does not have it.
		code, _ := get(t, httpx.Handler(httpx.Options{}), "/debug/pprof/")
		x.Equal(http.StatusNotFound, code)
	})
}

func TestMux(t *testing.T) {
	t.Run("a deployment's own paths are served", func(t *testing.T) {
		x := require.New(t)

		mux := http.NewServeMux()
		mux.HandleFunc("GET /version", func(res http.ResponseWriter, _ *http.Request) {
			_, _ = res.Write([]byte("v1"))
		})

		s := health.NewServer()
		s.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

		h := httpx.Handler(httpx.Options{Health: s, Mux: mux})

		code, body := get(t, h, "/version")
		x.Equal(http.StatusOK, code)
		x.Equal("v1", body)

		// And what this app writes is on it too.
		code, _ = get(t, h, "/healthz")
		x.Equal(http.StatusOK, code)
	})
}
