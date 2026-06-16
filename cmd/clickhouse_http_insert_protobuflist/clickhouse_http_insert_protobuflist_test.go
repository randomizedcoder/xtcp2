package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareBinary_noEnvelope(t *testing.T) {
	c := config{
		envelope: false,
		values:   []uint{42},
	}
	got := prepareBinary(context.Background(), c)
	if len(got) == 0 {
		t.Error("prepareBinary returned empty buffer")
	}
}

func TestPrepareBinary_envelope(t *testing.T) {
	c := config{
		envelope: true,
		values:   []uint{1, 2, 3},
	}
	got := prepareBinary(context.Background(), c)
	if len(got) == 0 {
		t.Error("prepareBinary returned empty buffer")
	}
}

func TestPrepareBinary_envelopeWithDump(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump")
	c := config{
		envelope:     true,
		values:       []uint{42},
		debugDump:    true,
		dumpFilename: dump,
	}
	prepareBinary(context.Background(), c)
	if _, err := os.Stat(dump + ".envelope"); err != nil {
		t.Errorf("debugDump should have written %s.envelope: %v", dump, err)
	}
}

func TestWriteDataToFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "out.bin")
	if err := writeDataToFile(context.Background(), target, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestWriteDataToFile_badPath(t *testing.T) {
	// Empty path → "open : no such file or directory"
	err := writeDataToFile(context.Background(), "/no/such/dir/out.bin", []byte("x"))
	if err == nil {
		t.Error("writing to non-existent directory should error")
	}
}

func TestRunMain_version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain(t.Context(), []string{"-v"}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "commit:") {
		t.Errorf("stdout = %q, want commit prefix", stdout.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_badValue(t *testing.T) {
	rc := runMain(t.Context(), []string{"-values", "abc"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}

func TestRunMain_fileMode(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	rc := runMain(t.Context(), []string{"-filename", out, "-db=false"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunMain_writeError(t *testing.T) {
	rc := runMain(t.Context(), []string{"-filename", "/no/such/dir/x", "-db=false"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}

func TestRunMain_dbBadConnect(t *testing.T) {
	// Default -db=true with a bogus connect string forces insertIntoCH to
	// fail. rc=1, stderr should mention Error.
	var stderr bytes.Buffer
	rc := runMain(t.Context(), []string{"-connect", "127.0.0.1:0"}, &bytes.Buffer{}, &stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1; stderr=%s", rc, stderr.String())
	}
}

func TestInsertIntoCH_thinWrapper(t *testing.T) {
	// Production-default wrapper picks baseURL from config.connectStr.
	// Use a bogus port so the dial fails fast and the wrapper returns err.
	c := config{connectStr: "127.0.0.1:0"}
	if err := insertIntoCH(t.Context(), c, []byte("p")); err == nil {
		t.Error("insertIntoCH against bogus connect should error")
	}
}

func TestInsertIntoCHAt_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	if err := insertIntoCHAt(t.Context(), srv.Client(), srv.URL, []byte("payload"), true); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestInsertIntoCHAt_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()
	err := insertIntoCHAt(t.Context(), srv.Client(), srv.URL, []byte("payload"), true)
	if err == nil {
		t.Error("500 should produce error")
	}
	if !errors.Is(err, ErrClickHouseHTTPPost) {
		t.Errorf("err should wrap ErrClickHouseHTTPPost, got %v", err)
	}
}

func TestInsertIntoCHAt_connRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if err := insertIntoCHAt(t.Context(), http.DefaultClient, url, []byte("payload"), true); err == nil {
		t.Error("conn-refused should produce error")
	}
}

func TestInsertIntoCHAt_badURL(t *testing.T) {
	// Malformed URL forces http.NewRequestWithContext to fail.
	err := insertIntoCHAt(t.Context(), http.DefaultClient, "://not a url", []byte("p"), true)
	if err == nil {
		t.Error("malformed URL should produce error")
	}
}

// Verify the URL contains FORMAT ProtobufList when useEnvelope is true,
// and FORMAT Protobuf when false — the bug under fix.
func TestInsertIntoCHAt_formatSelection(t *testing.T) {
	cases := []struct {
		useEnvelope bool
		wantFormat  string
	}{
		{true, "ProtobufList"},
		{false, "Protobuf&"},
	}
	for _, tc := range cases {
		var gotURL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotURL = r.URL.String()
			w.WriteHeader(http.StatusOK)
		}))
		if err := insertIntoCHAt(t.Context(), srv.Client(), srv.URL, []byte("p"), tc.useEnvelope); err != nil {
			t.Errorf("useEnvelope=%v: err = %v", tc.useEnvelope, err)
		}
		if !strings.Contains(gotURL, "FORMAT%20"+tc.wantFormat) {
			t.Errorf("useEnvelope=%v: URL = %s, want FORMAT %s", tc.useEnvelope, gotURL, tc.wantFormat)
		}
		srv.Close()
	}
}
