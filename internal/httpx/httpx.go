// Package httpx is the second listener: what this app serves to something that
// cannot speak gRPC.
//
// # Why it is a second listener and not the same one
//
// A gRPC server can be served through `net/http` -- `grpc.Server.ServeHTTP`
// exists, and one handler could route on the content type and serve both
// protocols on one port. gRPC's own documentation says not to for anything that
// matters: that road goes through `net/http`'s HTTP/2 rather than the
// transport gRPC brings, and it is slower and has less of what gRPC does.
//
// So the fast path keeps its own listener and its own transport, and everything
// that has to arrive over ordinary HTTP arrives here. It is not a compromise
// either way round: a browser cannot speak the fast one, and a service that can
// has no reason to come here.
//
// # What arrives here
//
//   - **grpc-web**, which is gRPC reframed so a browser can send it: the
//     trailers ride in the body and the text variant is base64. It is
//     translated and handed to the same [grpc.Server], so a browser reaches the
//     same handlers, the same interceptors and the same wall as anything else.
//     That translation *does* go the slow way, and that is the right place for
//     it -- a browser is not the throughput.
//   - **Plain HTTP**, on a mux a deployment fills in. `/healthz` is the one
//     this app writes, and it answers out of the same health server the gRPC
//     one does, so the two can never disagree.
//   - **pprof**, if it is asked for. Off unless it is written down, and it
//     should stay off anywhere a stranger can reach: it is the heap, the
//     goroutines and the ability to make the process spend thirty seconds
//     profiling itself.
package httpx

import (
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Options is what the second listener serves.
type Options struct {
	// Grpc is the server grpc-web is translated into, and nothing is translated
	// if it is nil.
	Grpc *grpc.Server

	// Health answers `/healthz`, and there is no such path if it is nil. It is
	// the same server the gRPC health service is registered with, on purpose:
	// two probes of one process that can disagree are worse than one probe.
	Health *health.Server

	// Origins reports whether a browser at this origin may make a grpc-web
	// call. Nil is **none** -- a browser is refused rather than served to
	// whoever asks.
	//
	// It is not the wall and it is not authentication. A browser sends its
	// cookies and its Origin; a program sends whatever it likes. What this
	// stops is a page somebody else wrote making calls as whoever is reading
	// it, and it stops nothing else.
	Origins func(origin string) bool

	// Pprof serves the profiles under `/debug/pprof/`.
	Pprof bool

	// Mux is what a deployment has of its own, and is where the paths above are
	// registered when it is nil.
	Mux *http.ServeMux
}

// Handler answers with what the second listener serves.
func Handler(o Options) http.Handler {
	mux := o.Mux
	if mux == nil {
		mux = http.NewServeMux()
	}

	if o.Health != nil {
		mux.Handle("GET /healthz", Health(o.Health))
		mux.Handle("GET /healthz/{service}", Health(o.Health))
	}
	if o.Pprof {
		// Registered here by name, and note what importing `net/http/pprof`
		// has *already* done: its init puts these on `http.DefaultServeMux`,
		// whether or not this block runs. So nothing in this app ever serves
		// that mux, and anything that started an HTTP server on it would be
		// serving the heap and the goroutines to whoever asked.
		mux.HandleFunc("GET /debug/pprof/", pprof.Index)
		mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	}

	if o.Grpc == nil {
		return mux
	}

	origins := o.Origins
	if origins == nil {
		origins = func(string) bool { return false }
	}

	w := grpcweb.WrapServer(o.Grpc,
		grpcweb.WithOriginFunc(origins),
		// A browser that opened a stream and went away leaves the server
		// holding it, since HTTP/1.1 has no way to say "I am done sending".
		// This is grpc-web's answer: a message on the wire that means it.
		grpcweb.WithWebsockets(false),
	)

	// grpc-web first, and only for what is grpc-web. Everything else is the
	// mux's, so a deployment's own paths are not shadowed by a protocol.
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		if w.IsGrpcWebRequest(req) || w.IsAcceptableGrpcCorsRequest(req) {
			w.ServeHTTP(res, req)
			return
		}

		mux.ServeHTTP(res, req)
	})
}

// Health answers what the gRPC health service answers, over HTTP, for a probe
// that cannot speak gRPC.
//
// `/healthz` is readiness and `/healthz/liveness` is the process; see
// cmd/health.go for why those are not the same question. The service is named
// by the path so that the two probes read the same two answers the gRPC ones
// do, out of the same server.
func Health(s *health.Server) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		v, err := s.Check(req.Context(), &grpc_health_v1.HealthCheckRequest{
			Service: strings.TrimPrefix(req.PathValue("service"), "/"),
		})
		if err != nil {
			// NotFound for a name nothing was registered under, which is what
			// the gRPC one answers too.
			http.Error(res, err.Error(), http.StatusNotFound)
			return
		}

		if v.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
			// 503 and not 500: nothing is broken here, this process is saying
			// it is not the one to send the traffic to.
			http.Error(res, v.GetStatus().String(), http.StatusServiceUnavailable)
			return
		}

		res.Header().Set("content-type", "text/plain; charset=utf-8")
		res.WriteHeader(http.StatusOK)
		_, _ = res.Write([]byte(v.GetStatus().String()))
	})
}
