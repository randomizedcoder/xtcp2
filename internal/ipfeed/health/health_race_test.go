package health

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestReadinessConcurrent drives SetReady() from many goroutines while others
// serve /readyz, so `go test -race` verifies the readiness atomic.Bool has no
// data race between writers and readers. After all goroutines finish, readiness
// must be latched true (SetReady is monotonic/idempotent).
func TestReadinessConcurrent(t *testing.T) {
	s := NewServer(":0")
	h := s.Handler()

	const writers, readers = 16, 16
	var wg sync.WaitGroup

	for range writers {
		wg.Go(func() {
			for range 100 {
				s.SetReady()
			}
		})
	}
	for range readers {
		wg.Go(func() {
			for range 100 {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
				h.ServeHTTP(rec, req)
				// Status races with the writers (200 or 503), so we only assert
				// it is one of the two valid outcomes — never a panic or 0.
				if rec.Code != http.StatusOK && rec.Code != http.StatusServiceUnavailable {
					t.Errorf("unexpected /readyz status %d", rec.Code)
				}
			}
		})
	}
	wg.Wait()

	if !s.Ready() {
		t.Fatal("readiness should be latched true after concurrent SetReady")
	}
}
