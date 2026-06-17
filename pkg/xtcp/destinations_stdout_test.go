package xtcp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

// TestWriterDestVerbatim verifies the reusable writerDest writes each payload
// verbatim (framing is the marshaller's job, not the destination's), returns
// the payload byte count, and can be driven entirely through an injected
// io.Writer — no os.Stdout needed.
func TestWriterDestVerbatim(t *testing.T) {
	x := newTestXTCP(t, schemeStdout)
	var buf bytes.Buffer
	d := &writerDest{x: x, w: &buf, label: "destStdout"}
	ctx := context.Background()

	// Marshallers own the newline, so payloads arrive already framed.
	payloads := [][]byte{[]byte("{\"a\":1}\n"), []byte("{\"b\":2}\n")}
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

// errWriter always fails, exercising writerDest.Send's error branch.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errBoom }

func TestWriterDestWriteError(t *testing.T) {
	x := newTestXTCP(t, schemeStdout)
	d := &writerDest{x: x, w: errWriter{}, label: "destStdout"}
	b := []byte("x")
	if _, err := d.Send(context.Background(), &b); err == nil {
		t.Fatal("expected error when the write fails")
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
