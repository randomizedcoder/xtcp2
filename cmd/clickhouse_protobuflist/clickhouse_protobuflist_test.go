package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"google.golang.org/protobuf/proto"
)

func TestEncodeLengthDelimitedProtobufList(t *testing.T) {
	r := &clickhouse_protolist.Envelope_Record{MyUint32: 7}
	got, err := encodeLengthDelimitedProtobufList(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Error("encodeLengthDelimitedProtobufList returned empty bytes")
	}
}

func TestEncodeLengthDelimitedEnvelope(t *testing.T) {
	got, err := encodeLengthDelimitedEnvelope([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	// First byte is the varint length (5), then the payload.
	if len(got) < 6 {
		t.Errorf("len = %d, want ≥ 6", len(got))
	}
	if string(got[len(got)-5:]) != "hello" {
		t.Errorf("payload tail = %q, want hello", got[len(got)-5:])
	}
}

func TestWriteDataToFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.bin")
	if err := writeDataToFile(p, []byte("xyz")); err != nil {
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
	if err := writeDataToFile("/no/such/dir/out.bin", []byte("x")); err == nil {
		t.Error("missing dir should produce error")
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

func TestRunMain_noEnvelope(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	rc := runMain([]string{"-filename", out, "-value", "42"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output file not written: %v", err)
	}
}

func TestRunMain_envelope(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	rc := runMain([]string{"-filename", out, "-value", "7", "-envelope"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunMain_writeError(t *testing.T) {
	rc := runMain([]string{"-filename", "/no/such/dir/out.bin"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}

func TestRunMain_writeEnvelopeError(t *testing.T) {
	rc := runMain([]string{"-filename", "/no/such/dir/out.bin", "-envelope"}, &bytes.Buffer{}, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("rc = %d, want 1", rc)
	}
}

// TestEncodeLengthDelimitedProtobufList_marshalErr swaps the
// marshalFn seam for a failing fake to cover the proto-marshal
// error-return branch (unreachable from production where proto.Marshal
// can't fail on this struct).
func TestEncodeLengthDelimitedProtobufList_marshalErr(t *testing.T) {
	orig := marshalFn
	marshalFn = func(_ proto.Message) ([]byte, error) {
		return nil, fmt.Errorf("synthetic marshal err")
	}
	defer func() { marshalFn = orig }()

	_, err := encodeLengthDelimitedProtobufList(&clickhouse_protolist.Envelope_Record{MyUint32: 1})
	if err == nil {
		t.Fatal("expected err from failing marshaller")
	}
	if !strings.Contains(err.Error(), "error marshaling Record") {
		t.Errorf("err = %q, want substring 'error marshaling Record'", err)
	}
}

// TestRunMain_encodeError exercises runMain's error-handling branch
// when encodeLengthDelimitedProtobufList fails. rc=1 means the
// encode-error branch fired.
func TestRunMain_encodeError(t *testing.T) {
	orig := marshalFn
	marshalFn = func(_ proto.Message) ([]byte, error) {
		return nil, fmt.Errorf("synthetic")
	}
	defer func() { marshalFn = orig }()

	dir := t.TempDir()
	out := filepath.Join(dir, "out.bin")
	var stderr bytes.Buffer
	rc := runMain([]string{"-filename", out}, &bytes.Buffer{}, &stderr)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 (encode error)", rc)
	}
	if !strings.Contains(stderr.String(), "Error encoding") {
		t.Errorf("stderr = %q, want substring 'Error encoding'", stderr.String())
	}
}
