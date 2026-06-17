package xtcp

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
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

// TestFileDest writes payloads and reads them back from the file.
func TestFileDest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.jsonl")
	x := newTestXTCP(t, "file:"+path)
	dest, err := newFileDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newFileDest: %v", err)
	}
	payload := []byte("hello\n")
	if _, err := dest.Send(context.Background(), &payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := dest.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Errorf("file content = %q, want %q", got, "hello\n")
	}
}

func TestFileDest_emptyPath(t *testing.T) {
	x := newTestXTCP(t, "file:")
	if _, err := newFileDest(context.Background(), x); err == nil {
		t.Fatal("expected error for empty file path")
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

func TestTCPDest_dialError(t *testing.T) {
	// Port 1 on localhost should refuse; dial must fail.
	x := newTestXTCP(t, "tcp:127.0.0.1:1")
	if _, err := newTCPDest(context.Background(), x); err == nil {
		t.Fatal("expected dial error")
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
	for _, s := range []string{schemeStderr, schemeFile, schemeTCP, schemeHTTP, schemeHTTPS} {
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
