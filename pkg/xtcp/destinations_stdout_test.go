package xtcp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

// TestWriterDestFraming verifies the reusable writerDest writes each payload
// followed by a single newline, returns the payload byte count, and can be
// driven entirely through an injected io.Writer — no os.Stdout needed.
func TestWriterDestFraming(t *testing.T) {
	x := newTestXTCP(t, schemeStdout)
	var buf bytes.Buffer
	d := &writerDest{x: x, w: &buf, label: "destStdout"}
	ctx := context.Background()

	payloads := [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`)}
	for _, p := range payloads {
		b := append([]byte(nil), p...) // copy: Send must not mutate the caller's buffer
		n, err := d.Send(ctx, &b)
		if err != nil {
			t.Fatalf("Send: %v", err)
		}
		if n != len(p) {
			t.Errorf("n = %d, want %d", n, len(p))
		}
		if !bytes.Equal(b, p) {
			t.Errorf("Send mutated the payload buffer: got %q want %q", b, p)
		}
	}

	want := "{\"a\":1}\n{\"b\":2}\n"
	if got := buf.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

var errBoom = errors.New("boom")

// failingWriter fails on the Nth Write (1-indexed); earlier writes succeed
// into buf. Used to exercise both error branches of writerDest.Send.
type failingWriter struct {
	failOn int
	calls  int
	buf    bytes.Buffer
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failOn {
		return 0, errBoom
	}
	return w.buf.Write(p)
}

func TestWriterDestPayloadWriteError(t *testing.T) {
	x := newTestXTCP(t, schemeStdout)
	d := &writerDest{x: x, w: &failingWriter{failOn: 1}, label: "destStdout"}
	b := []byte("x")
	if _, err := d.Send(context.Background(), &b); err == nil {
		t.Fatal("expected error when the payload write fails")
	}
}

func TestWriterDestNewlineWriteError(t *testing.T) {
	x := newTestXTCP(t, schemeStdout)
	d := &writerDest{x: x, w: &failingWriter{failOn: 2}, label: "destStdout"}
	b := []byte("x")
	if _, err := d.Send(context.Background(), &b); err == nil {
		t.Fatal("expected error when the newline write fails")
	}
}

// TestStdoutDestFactory confirms the "stdout" scheme is registered and that
// its factory defaults the writer to os.Stdout with the expected label.
func TestStdoutDestFactory(t *testing.T) {
	if _, status := lookupDestinationFactory(schemeStdout); status != destLookupFound {
		t.Fatalf("stdout scheme not registered: status %v", status)
	}

	x := newTestXTCP(t, schemeStdout)
	dest, err := newStdoutDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newStdoutDest: %v", err)
	}
	wd, ok := dest.(*writerDest)
	if !ok {
		t.Fatalf("newStdoutDest returned %T, want *writerDest", dest)
	}
	if wd.w != os.Stdout {
		t.Error("default writer should be os.Stdout")
	}
	if wd.label != "destStdout" {
		t.Errorf("label = %q, want destStdout", wd.label)
	}
}
