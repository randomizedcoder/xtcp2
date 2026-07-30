package xtcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHTTPDest_send drives the http destination against a real httptest server
// and asserts the marshalled payload arrives verbatim as the POST body. The
// same factory backs the https scheme.
func TestHTTPDest_send(t *testing.T) {
	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		bodyCh <- b
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// httpDest reads x.config.Dest as the full URL (scheme included).
	x := newTestXTCP(t, srv.URL)
	d, err := newHTTPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newHTTPDest: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	payload := []byte(`{"hello":"http"}`)
	b := append([]byte(nil), payload...)
	n, err := d.Send(context.Background(), &b)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n != 1 {
		t.Errorf("Send n = %d, want 1 (one record)", n)
	}

	select {
	case got := <-bodyCh:
		if !bytes.Equal(got, payload) {
			t.Errorf("POST body = %q, want %q", got, payload)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("timed out waiting for the http POST")
	}
}
