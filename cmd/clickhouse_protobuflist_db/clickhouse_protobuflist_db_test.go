package main

import (
	"os"
	"path/filepath"
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
