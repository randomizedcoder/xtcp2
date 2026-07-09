package health

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_alwaysOK(t *testing.T) {
	rr := httptest.NewRecorder()
	Healthz(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("Healthz = %d, want 200", rr.Code)
	}
}

func TestReadyz_reflectsState(t *testing.T) {
	t.Cleanup(func() { SetReady(false) })

	SetReady(false)
	rr := httptest.NewRecorder()
	Readyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("Readyz(not ready) = %d, want 503", rr.Code)
	}

	SetReady(true)
	if !Ready() {
		t.Fatal("Ready() = false after SetReady(true)")
	}
	rr = httptest.NewRecorder()
	Readyz(rr, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("Readyz(ready) = %d, want 200", rr.Code)
	}
}
