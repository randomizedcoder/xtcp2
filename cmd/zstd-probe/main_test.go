package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func writeZstd(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.txt.zst")
	var b bytes.Buffer
	enc, err := zstd.NewWriter(&b)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err := enc.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestDecompressFileDiscard(t *testing.T) {
	payload := bytes.Repeat([]byte("xtcp2 zstd probe\n"), 1024)
	path := writeZstd(t, payload)

	got, err := decompressFile(path, "")
	if err != nil {
		t.Fatalf("decompressFile: %v", err)
	}
	if got.compressedBytes <= 0 {
		t.Fatalf("compressedBytes = %d, want > 0", got.compressedBytes)
	}
	if got.decompressedBytes != int64(len(payload)) {
		t.Fatalf("decompressedBytes = %d, want %d", got.decompressedBytes, len(payload))
	}
	if got.duration <= 0 {
		t.Fatalf("duration = %s, want > 0", got.duration)
	}
}

func TestDecompressFileOutput(t *testing.T) {
	payload := []byte("hello from zstd-probe\n")
	path := writeZstd(t, payload)
	outDir := t.TempDir()

	if _, err := decompressFile(path, outDir); err != nil {
		t.Fatalf("decompressFile: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "payload.txt"))
	if err != nil {
		t.Fatalf("ReadFile output: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("output = %q, want %q", got, payload)
	}
}
