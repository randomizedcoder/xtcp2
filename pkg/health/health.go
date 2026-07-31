// Package health exposes liveness/readiness for containerised deployment
// (Docker healthcheck, Kubernetes httpGet probes). Readiness is a single
// process-wide flag — there is one xtcp2 daemon per process — that the daemon
// flips true once it has initialised its destination and netlinkers and started
// polling, and false on shutdown. The gRPC health service (see grpc_server.go)
// is driven from the same flag.
package health

import (
	"net/http"
	"sync/atomic"
)

var ready atomic.Bool

// SetReady sets the process readiness state reported by Readyz.
func SetReady(r bool) { ready.Store(r) }

// Ready reports the current readiness state.
func Ready() bool { return ready.Load() }

// writeBody writes the response body. A write error means the client
// disconnected before reading the health/readiness response — there is nothing
// the probe handler can do about it, so the error is checked here (once) and
// dropped rather than ignored blank at every call site.
func writeBody(w http.ResponseWriter, s string) {
	if _, err := w.Write([]byte(s)); err != nil {
		return
	}
}

// Healthz is a liveness handler: 200 as soon as the HTTP server is serving. It
// says nothing about whether the daemon is polling yet — that is Readyz.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	writeBody(w, "ok\n")
}

// Readyz is a readiness handler: 200 once the daemon has initialised its
// destination + netlinkers and started polling, else 503 (and again on
// shutdown) — so an orchestrator holds traffic/rollout until xtcp2 is live.
func Readyz(w http.ResponseWriter, _ *http.Request) {
	if !ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeBody(w, "not ready\n")
		return
	}
	w.WriteHeader(http.StatusOK)
	writeBody(w, "ready\n")
}
