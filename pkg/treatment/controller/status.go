package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
)

// StatusHandler provides an operator-authenticated JSON dashboard feed and
// Prometheus metrics. Avoid destination/agent labels on fleet metrics.
func (s *Server) StatusHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
			http.Error(w, "mTLS required", http.StatusUnauthorized)
			return
		}
		id, e := identity.FromCertificate(r.TLS.PeerCertificates[0])
		if e != nil || id.Role != identity.RoleOperator {
			http.Error(w, "operator required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "GET required", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/status" && r.URL.Path != "/metrics" && r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if r.URL.Path == "/readyz" {
			if e := s.Store.Ping(ctx); e != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintln(w, "ready")
			return
		}
		values, e := s.Store.Repos().Metrics.Snapshot(ctx, s.now())
		if e != nil {
			http.Error(w, "status query failed", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/status" {
			b, merr := json.Marshal(values)
			if merr != nil {
				http.Error(w, "status encoding failed", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if _, werr := w.Write(append(b, '\n')); werr != nil {
				log.Printf("controller status: write: %v", werr)
			}
			return
		}
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		for _, k := range keys {
			fmt.Fprintf(w, "# TYPE xtcp_controller_%s gauge\nxtcp_controller_%s %g\n", k, k, values[k])
		}
	})
}
