package main

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
)

func TestHandleRecord_happy(t *testing.T) {
	encoded, err := proto.Marshal(&xtcp_flat_record.Envelope_XtcpFlatRecord{Hostname: "test-host"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &kgo.Record{Topic: "xtcp", Partition: 0, Offset: 1, Value: encoded}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}

	handleRecord(0, 1, 1, rec, dst)
	if dst.Hostname != "test-host" {
		t.Errorf("decoded hostname = %q, want test-host", dst.Hostname)
	}
}

func TestHandleRecord_badProto(t *testing.T) {
	rec := &kgo.Record{Topic: "xtcp", Value: []byte{0xFF, 0xFF, 0xFF, 0xFF}}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}

	// handleRecord swallows the decode error (logs and returns). We just
	// verify it doesn't panic on malformed input.
	handleRecord(0, 1, 1, rec, dst)
}

func TestHandleRecord_emptyValue(t *testing.T) {
	rec := &kgo.Record{Topic: "xtcp", Value: nil}
	dst := &xtcp_flat_record.Envelope_XtcpFlatRecord{}
	handleRecord(0, 1, 1, rec, dst)
	// Empty bytes are a valid empty proto message; nothing to assert beyond
	// no-panic.
}
