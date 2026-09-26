package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadyzHealthz(t *testing.T) {
	tests := []struct {
		name       string
		desc       string
		class      string
		path       string
		setReady   bool
		wantStatus int
	}{
		{"positive_healthz", "positive: healthz is 200 regardless of readiness", "positive",
			"/healthz", false, http.StatusOK},
		{"negative_readyz_before", "negative: readyz is 503 before any successful cycle", "negative",
			"/readyz", false, http.StatusServiceUnavailable},
		{"boundary_readyz_after", "boundary: readyz flips to 200 right after SetReady", "boundary",
			"/readyz", true, http.StatusOK},
		{"corner_unknown_path", "corner: an unregistered path 404s", "corner",
			"/nope", true, http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewServer(":0")
			if tc.setReady {
				s.SetReady()
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Errorf("%s: status = %d, want %d", tc.desc, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestHandle covers mounting an extra handler (the daemon's Prometheus
// /metrics) next to the probes: it is served, the probes keep working, an
// unregistered path still 404s, and a duplicate pattern panics like
// http.ServeMux does.
//
// go test ./internal/ipfeed/health/ -run TestHandle
func TestHandle(t *testing.T) {
	metricsBody := "xtcp_gauges{function=\"loadAsn\"} 3\n"
	metrics := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(metricsBody))
	})
	tests := []struct {
		description string
		register    []string // patterns to Handle with the metrics handler, in order
		path        string
		wantStatus  int
		wantBody    string // "" = not checked
		wantPanic   bool
	}{
		// positive
		{"registered /metrics is served with the handler's body", []string{"/metrics"}, "/metrics", http.StatusOK, metricsBody, false},
		{"probes keep working alongside /metrics", []string{"/metrics"}, "/healthz", http.StatusOK, "ok", false},
		// negative
		{"nothing registered: /metrics 404s", nil, "/metrics", http.StatusNotFound, "", false},
		{"an unrelated path still 404s", []string{"/metrics"}, "/nope", http.StatusNotFound, "", false},
		// corner
		{"registering the same pattern twice panics (ServeMux contract)", []string{"/metrics", "/metrics"}, "/metrics", 0, "", true},
		{"registering a probe path again panics rather than silently replacing it", []string{"/healthz"}, "/healthz", 0, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s := NewServer(":0")
			panicked := func() (p bool) {
				defer func() {
					if r := recover(); r != nil {
						p = true
					}
				}()
				for _, pat := range tc.register {
					s.Handle(pat, metrics)
				}
				return false
			}()
			if panicked != tc.wantPanic {
				t.Fatalf("panicked = %v, want %v", panicked, tc.wantPanic)
			}
			if tc.wantPanic {
				return
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.wantStatus {
				t.Errorf("GET %s status = %d, want %d", tc.path, rec.Code, tc.wantStatus)
			}
			if tc.wantBody != "" && rec.Body.String() != tc.wantBody {
				t.Errorf("GET %s body = %q, want %q", tc.path, rec.Body.String(), tc.wantBody)
			}
		})
	}
}

func TestSetReadyIdempotent(t *testing.T) {
	s := NewServer(":0")
	if s.Ready() {
		t.Fatal("should start not-ready")
	}
	s.SetReady()
	s.SetReady() // idempotent
	if !s.Ready() {
		t.Fatal("should be ready after SetReady")
	}
}
