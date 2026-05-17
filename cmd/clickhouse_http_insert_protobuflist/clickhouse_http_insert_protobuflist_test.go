package main

import (
	"context"
	"os"
	"path/filepath"
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
