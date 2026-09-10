package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// TestClientGetConcurrent shares one *Client across many goroutines hitting a
// single server, so `go test -race` verifies the reused http.Client and the
// retry/backoff seams carry no data race. Each goroutine uses a distinct URL
// key; the server fails that key's first request (500) then serves 200, so
// every goroutine deterministically exercises exactly one retry — the backoff
// window/jitter/sleep seams run concurrently, not just the happy path.
func TestClientGetConcurrent(t *testing.T) {
	var seen sync.Map // key -> struct{}: has this key been hit before?
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.RawQuery
		if _, hit := seen.LoadOrStore(key, struct{}{}); !hit {
			w.WriteHeader(http.StatusInternalServerError) // first hit for this key
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := testClient(5, nil) // no-op sleep, zero jitter -> deterministic, fast

	const goroutines = 50
	var wg sync.WaitGroup
	errs := make([]error, goroutines)
	for i := range goroutines {
		wg.Go(func() {
			url := srv.URL + "?g=" + strconv.Itoa(i)
			_, err := client.Get(context.Background(), url, Conditional{})
			errs[i] = err
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, err)
		}
	}
}
