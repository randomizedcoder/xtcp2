package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareBinary_noEnvelope(t *testing.T) {
	c := config{envelope: false, values: []uint{42}}
	got := prepareBinary(context.Background(), c, 7)
	if len(got) == 0 {
		t.Error("prepareBinary returned empty buffer")
	}
	// First byte is magic 0x00, then 4 bytes big-endian schemaID.
	if got[0] != 0 {
		t.Errorf("magic byte = %x, want 0", got[0])
	}
}

func TestPrepareBinary_envelope(t *testing.T) {
	c := config{envelope: true, values: []uint{1, 2, 3}}
	got := prepareBinary(context.Background(), c, 11)
	if len(got) == 0 {
		t.Error("prepareBinary returned empty buffer")
	}
}

func TestPrepareBinary_envelopeDump(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump")
	c := config{
		envelope: true, values: []uint{1},
		debugDump: true, dumpFilename: dump,
	}
	prepareBinary(context.Background(), c, 1)
	if _, err := os.Stat(dump + ".envelope"); err != nil {
		t.Errorf("debugDump should have written sidecar: %v", err)
	}
}

func TestIncrementSlice(t *testing.T) {
	c := config{debugLevel: 0}
	vals := []uint{1, 2, 3}
	incrementSlice(c, &vals, 10)
	for i, v := range []uint{11, 12, 13} {
		if vals[i] != v {
			t.Errorf("vals[%d] = %d, want %d", i, vals[i], v)
		}
	}
}

func TestWriteDataToFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.bin")
	if err := writeDataToFile(context.Background(), p, []byte("xyz")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "xyz" {
		t.Errorf("got %q, want xyz", b)
	}
}

func TestWriteDataToFile_badPath(t *testing.T) {
	if err := writeDataToFile(context.Background(), "/no/such/dir/x", []byte{1}); err == nil {
		t.Error("missing dir should error")
	}
}

func TestGetLatestSchemaIDAt_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":99}`)) //nolint:errcheck // test plumbing
	}))
	defer srv.Close()
	got, err := getLatestSchemaIDAt(context.Background(), srv.Client(), srv.URL, "subj")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != 99 {
		t.Errorf("id = %d, want 99", got)
	}
}

func TestGetLatestSchemaIDAt_badJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json")) //nolint:errcheck // test plumbing
	}))
	defer srv.Close()
	if _, err := getLatestSchemaIDAt(context.Background(), srv.Client(), srv.URL, "subj"); err == nil {
		t.Error("bad JSON should produce error")
	}
}

func TestGetLatestSchemaIDAt_connRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if _, err := getLatestSchemaIDAt(context.Background(), http.DefaultClient, url, "subj"); err == nil {
		t.Error("conn-refused should produce error")
	}
}

func TestGetLatestSchemaIDAt_ctxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"id":1}`)) //nolint:errcheck // test plumbing
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := getLatestSchemaIDAt(ctx, srv.Client(), srv.URL, "subj"); err == nil {
		t.Error("cancelled ctx should produce error")
	}
}
