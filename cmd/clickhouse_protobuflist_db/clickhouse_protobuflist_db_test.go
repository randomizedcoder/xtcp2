package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareBinary_noEnvelope(t *testing.T) {
	c := config{envelope: false, values: []uint{42}}
	got := prepareBinary(c)
	if len(got) == 0 {
		t.Error("empty buffer")
	}
}

func TestPrepareBinary_envelope(t *testing.T) {
	c := config{envelope: true, values: []uint{1, 2, 3}}
	got := prepareBinary(c)
	if len(got) == 0 {
		t.Error("empty buffer")
	}
}

func TestPrepareBinary_envelopeDump(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump")
	c := config{
		envelope: true, values: []uint{1},
		debugDump: true, dumpFilename: dump,
	}
	prepareBinary(c)
	if _, err := os.Stat(dump + ".envelope"); err != nil {
		t.Errorf("debugDump should have written sidecar: %v", err)
	}
}

func TestWriteDataToFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.bin")
	if err := writeDataToFile(p, []byte("xyz")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p) //nolint:errcheck // test plumbing
	if string(b) != "xyz" {
		t.Errorf("got %q, want xyz", b)
	}
}

func TestWriteDataToFile_badPath(t *testing.T) {
	if err := writeDataToFile("/no/such/dir/x", []byte{1}); err == nil {
		t.Error("missing dir should error")
	}
}

func TestRunMain_version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain([]string{"-v"}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "commit:") {
		t.Errorf("stdout = %q, want commit prefix", stdout.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain([]string{"-not-a-flag"}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_badValue(t *testing.T) {
	var stderr bytes.Buffer
	if rc := runMain([]string{"-values", "not-a-uint"}, &bytes.Buffer{}, &stderr); rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}

func TestRunMain_happy(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	rc := runMain([]string{"-filename", out, "-values", "1,2,3"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunMain_dbNotImplemented(t *testing.T) {
	var stderr bytes.Buffer
	rc := runMain([]string{"-db"}, &bytes.Buffer{}, &stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
	if !strings.Contains(stderr.String(), "not implemented") {
		t.Errorf("stderr = %q, want 'not implemented'", stderr.String())
	}
}

func TestRunMain_writeError(t *testing.T) {
	rc := runMain([]string{"-filename", "/no/such/dir/x"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}
