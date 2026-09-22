// Package health serves liveness and readiness endpoints for daemon mode.
// /healthz reports process liveness (always 200 once serving); /readyz reports
// 200 only after at least one successful collection cycle, so orchestrators
// can wait for the first dataset before routing/alerting.
package health

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Server wraps an http.Server plus a readiness flag.
type Server struct {
	ready atomic.Bool
	mux   *http.ServeMux
	http  *http.Server
}

// NewServer builds a health server bound to addr (e.g. ":8080"). It does not
// start listening until Start is called.
func NewServer(addr string) *Server {
	s := &Server{mux: http.NewServeMux()}
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/readyz", s.handleReadyz)
	s.http = &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Handle registers an extra handler on the health server's mux (e.g. a
// Prometheus /metrics endpoint), so the daemon exposes one port for
// orchestration probes and scraping. Call before Start; the mux panics on a
// duplicate pattern, exactly like http.ServeMux.
func (s *Server) Handle(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
}

// SetReady marks the service ready (idempotent). Called after a successful cycle.
func (s *Server) SetReady() { s.ready.Store(true) }

// Ready reports the current readiness state.
func (s *Server) Ready() bool { return s.ready.Load() }

// writeBody writes the response body. A write error means the client
// disconnected before reading the response — nothing the handler can do — so it
// is checked here once and dropped rather than ignored blank at each call site.
func writeBody(w http.ResponseWriter, s string) {
	if _, err := w.Write([]byte(s)); err != nil {
		return
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	writeBody(w, "ok")
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	if s.ready.Load() {
		w.WriteHeader(http.StatusOK)
		writeBody(w, "ready")
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	writeBody(w, "not ready")
}

// Start begins serving in a background goroutine. It returns once the listener
// is bound (so a bind error surfaces synchronously) or an error if binding
// failed. A non-graceful Serve error is logged (the daemon keeps running; a
// dead health endpoint must not take down collection).
func (s *Server) Start(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", s.http.Addr)
	if err != nil {
		return err
	}
	go func() {
		if serr := s.http.Serve(ln); serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			slog.Error("health server stopped", "err", serr)
		}
	}()
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.http.Shutdown(ctx)
}

// Handler exposes the mux for testing without binding a socket.
func (s *Server) Handler() http.Handler { return s.http.Handler }
