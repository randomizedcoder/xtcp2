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
