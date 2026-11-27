package xtcp

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestStderrDestFactory: the stderr scheme is registered and writes to stderr.
func TestStderrDestFactory(t *testing.T) {
	if _, status := lookupDestinationFactory(schemeStderr); status != destLookupFound {
		t.Fatalf("stderr not registered: %v", status)
	}
	x := newTestXTCP(t, schemeStderr)
	dest, err := newStderrDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newStderrDest: %v", err)
	}
	wd, ok := dest.(*writerDest)
	if !ok || wd.w != os.Stderr {
		t.Fatalf("stderr dest not wired to os.Stderr: %T", dest)
	}
	if err := dest.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestTCPDest dials a listener, sends, and reads the bytes back verbatim.
func TestTCPDest(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	got := make(chan string, 1)
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			got <- ""
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		got <- line
	}()

	x := newTestXTCP(t, "tcp:"+ln.Addr().String())
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest: %v", err)
	}
	payload := []byte("tcp-line\n")
	if _, err := dest.Send(context.Background(), &payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if line := <-got; line != "tcp-line\n" {
		t.Errorf("received %q, want %q", line, "tcp-line\n")
	}
	if err := dest.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestTCPDest_sinkDownAtStart(t *testing.T) {
	// Port 1 on localhost refuses. Pre-#339 the factory dialed here and
	// aborted startup; now the dial is the worker's job, so the daemon
	// starts with the sink down, Send never blocks, and the queued envelope
	// is dropped (counted) once the attempt budget runs out.
	x := newTestXTCP(t, "tcp:127.0.0.1:1")
	dest, err := newTCPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newTCPDest with the sink down: %v", err)
	}
	td, ok := dest.(*tcpDest)
	if !ok {
		t.Fatalf("dest is %T, want *tcpDest", dest)
	}
	td.attempts = 1 // one refused dial per envelope, no backoff
	td.jitterDur = func(time.Duration) time.Duration { return 0 }

	b := []byte("queued-while-down\n")
	n, err := dest.Send(context.Background(), &b)
	if err != nil {
		t.Fatalf("Send with the sink down: %v", err)
	}
	if n != 1 {
		t.Errorf("Send n = %d, want 1", n)
	}
	if err := dest.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// The attempt budget dropped the envelope: one dial error, one drop.
	if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "Dial", "error")); got < 1 {
		t.Errorf("destTCP dial-error counter = %v, want >= 1", got)
	}
	if got := testutil.ToFloat64(x.pC.WithLabelValues("destTCP", "dropped", "error")); got < 1 {
		t.Errorf("destTCP dropped counter = %v, want >= 1", got)
	}
}

// TestHTTPDest posts to a test server and checks body + Content-Type.
func TestHTTPDest(t *testing.T) {
	type capture struct {
		body        string
		contentType string
	}
	capCh := make(chan capture, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf, _ := io.ReadAll(r.Body)
		capCh <- capture{body: string(buf), contentType: r.Header.Get("Content-Type")}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	x := newTestXTCP(t, srv.URL)
	x.config.MarshalTo = MarshallerJSONL
	dest, err := newHTTPDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newHTTPDest: %v", err)
	}
	payload := []byte("{\"a\":1}\n")
	if _, err := dest.Send(context.Background(), &payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	c := <-capCh
	if c.body != "{\"a\":1}\n" {
		t.Errorf("posted body = %q", c.body)
	}
	if c.contentType != "application/x-ndjson" {
		t.Errorf("content-type = %q, want application/x-ndjson", c.contentType)
	}
	if err := dest.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestHTTPDest_non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	x := newTestXTCP(t, srv.URL)
	dest, err := newHTTPDest(context.Background(), x)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("x")
	if _, err := dest.Send(context.Background(), &payload); err == nil {
		t.Fatal("expected error on 500 response")
	}
}

// TestNewStreamSchemesRegistered confirms all new schemes resolve.
func TestNewStreamSchemesRegistered(t *testing.T) {
	for _, s := range []string{schemeStderr, schemeTCP, schemeHTTP, schemeHTTPS} {
		if _, status := lookupDestinationFactory(s); status != destLookupFound {
			t.Errorf("scheme %q not registered (status %v)", s, status)
		}
	}
}

// Ensure the package's InitEnvelopeMarshallers + new dests don't deadlock when
// initialized together (smoke).
func TestInitWithNewFormats(t *testing.T) {
	x, _ := newMarshalFixture(t)
	x.config.MarshalTo = MarshallerCSV
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitEnvelopeMarshallers(&wg)
	wg.Wait()
	if x.EnvelopeMarshaller == nil {
		t.Fatal("csv EnvelopeMarshaller nil")
	}
}
