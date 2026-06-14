package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
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
