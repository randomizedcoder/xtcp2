package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"net/http"
	"sort"
	"time"
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
			if e := s.Store.DB.PingContext(ctx); e != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintln(w, "ready")
			return
		}
		queries := map[string]string{
			"agents":               "SELECT count(*) FROM xtcp_control_agents",
			"agents_connected":     "SELECT count(*) FROM xtcp_control_agents WHERE last_seen>now()-interval '2 minutes'",
			"agents_suspended":     "SELECT count(*) FROM xtcp_control_agents WHERE desired->>'suspended'='true' OR observed->>'locallySuspended'='true'",
			"assignments":          "SELECT coalesce(sum(jsonb_array_length(coalesce(desired->'assignments','[]'))),0) FROM xtcp_control_agents",
			"routes_verified":      "SELECT count(*) FROM xtcp_control_agents a CROSS JOIN LATERAL jsonb_array_elements(coalesce(a.observed->'records','[]')) r WHERE r->>'status'='APPLY_STATUS_VERIFIED'",
			"routes_failed":        "SELECT count(*) FROM xtcp_control_agents a CROSS JOIN LATERAL jsonb_array_elements(coalesce(a.observed->'records','[]')) r WHERE r->>'status'='APPLY_STATUS_FAILED'",
			"routes_expired":       "SELECT count(*) FROM xtcp_control_agents a CROSS JOIN LATERAL jsonb_array_elements(coalesce(a.observed->'records','[]')) r WHERE r->>'status'='APPLY_STATUS_EXPIRED'",
			"experiments_pending":  "SELECT count(*) FROM xtcp_control_experiments WHERE state->>'status'='EXPERIMENT_STATUS_PROPOSED'",
			"experiments_running":  "SELECT count(*) FROM xtcp_control_experiments WHERE state->>'status'='EXPERIMENT_STATUS_RUNNING'",
			"experiments_complete": "SELECT count(*) FROM xtcp_control_experiments WHERE state->>'status'='EXPERIMENT_STATUS_COMPLETE'",
			"audit_events":         "SELECT count(*) FROM xtcp_control_events",
		}
		values := map[string]float64{}
		keys := make([]string, 0, len(queries))
		for k, q := range queries {
			var v float64
			if e := s.Store.DB.QueryRowContext(ctx, q).Scan(&v); e != nil {
				http.Error(w, "status query failed", http.StatusServiceUnavailable)
				return
			}
			values[k] = v
			keys = append(keys, k)
		}
		if r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(values)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "# TYPE xtcp_controller_%s gauge\nxtcp_controller_%s %g\n", k, k, values[k])
		}
	})
}
