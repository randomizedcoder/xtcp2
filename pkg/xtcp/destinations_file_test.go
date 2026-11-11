//go:build dest_file

package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestFileDest_send drives the file destination and asserts the marshaled
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

// TestFileSchemeRegistered confirms the dest_file tag registers the scheme.
func TestFileSchemeRegistered(t *testing.T) {
	if _, status := lookupDestinationFactory(schemeFile); status != destLookupFound {
		t.Errorf("scheme %q not registered (status %v)", schemeFile, status)
	}
}

// TestValidateInput_fileDest: with the tag, `-dest file:<path>` validates.
func TestValidateInput_fileDest(t *testing.T) {
	x := newTestXTCP(t, "file:/var/log/xtcp.csv")
	x.config.MarshalTo = MarshallerProtoJSON
	x.config.Topic = "xtcp"
	x.Marshallers.Store(MarshallerProtoJSON, struct{}{})
	if err := x.validateInput(); err != nil {
		t.Errorf("validateInput: %v", err)
	}
}
