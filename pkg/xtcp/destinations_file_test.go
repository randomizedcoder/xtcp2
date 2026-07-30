package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestFileDest_send drives the file destination and asserts the marshalled
// payload is appended verbatim to the target file. newFileDest returns the
// shared writerDest, so Send reports the byte count.
func TestFileDest_send(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.bin")
	x := newTestXTCP(t, schemeFile+":"+path)
	d, err := newFileDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newFileDest: %v", err)
	}

	payloads := [][]byte{[]byte("first\n"), []byte("second\n")}
	var want []byte
	for _, p := range payloads {
		b := append([]byte(nil), p...)
		n, err := d.Send(context.Background(), &b)
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if n != len(p) {
			t.Errorf("Send n = %d, want %d (byte count)", n, len(p))
		}
		want = append(want, p...)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("file content = %q, want %q", got, want)
	}
}
