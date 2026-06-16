package xtcp

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protodelim"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// newMarshalFixture returns an XTCP with the prom counters registered
// and a fixed XtcpFlatRecord we can golden-compare against.
func newMarshalFixture(t *testing.T) (*XTCP, *xtcp_flat_record.XtcpFlatRecord) {
	t.Helper()
	x := &XTCP{config: &xtcp_config.XtcpConfig{}}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_marshal_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	r := &xtcp_flat_record.XtcpFlatRecord{
		Hostname:     "test-host",
		Netns:        "/run/netns/default",
		TimestampNs:  1.23,
		SocketFd:     7,
		NetlinkerId:  3,
		TcpInfoState: 1,
	}
	return x, r
}

func TestValidMarshallers(t *testing.T) {
	got := validMarshallers()
	want := []string{MarshallerProtobufList, "protoJson", "protoText", "msgpack"}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("validMarshallers() = %q, missing %q", got, w)
		}
	}
}

func TestProtoJsonMarshal_containsFields(t *testing.T) {
	x, rec := newMarshalFixture(t)
	buf := x.protoJsonMarshal(rec)
	if buf == nil {
		t.Fatal("protoJsonMarshal returned nil")
	}
	s := string(*buf)
	if !strings.Contains(s, "test-host") {
		t.Errorf("JSON output missing hostname: %s", s)
	}
}

func TestProtoTextMarshal_containsFields(t *testing.T) {
	x, rec := newMarshalFixture(t)
	buf := x.protoTextMarshal(rec)
	if buf == nil {
		t.Fatal("protoTextMarshal returned nil")
	}
	s := string(*buf)
	if !strings.Contains(s, "test-host") {
		t.Errorf("Text output missing hostname: %s", s)
	}
}

func TestProtoMsgPackMarshal_roundtrip(t *testing.T) {
	x, rec := newMarshalFixture(t)
	buf := x.protoMsgPackMarshal(rec)
	if buf == nil || len(*buf) == 0 {
		t.Fatalf("protoMsgPackMarshal returned empty buf")
	}
	var got xtcp_flat_record.XtcpFlatRecord
	if uerr := msgpack.Unmarshal(*buf, &got); uerr != nil {
		t.Fatalf("msgpack.Unmarshal failed: %v", uerr)
	}
	if got.Hostname != rec.Hostname {
		t.Errorf("msgpack roundtrip lost hostname: %q", got.Hostname)
	}
}

// ByteSliceWriter appends raw bytes onto its target.
func TestByteSliceWriter_Write(t *testing.T) {
	buf := []byte{}
	w := &ByteSliceWriter{Buf: &buf}
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned n=%d, want 5", n)
	}
	if string(buf) != "hello" {
		t.Errorf("buf = %q, want hello", buf)
	}
	// Subsequent writes append.
	if _, err := w.Write([]byte(" world")); err != nil {
		t.Fatalf("Write append err: %v", err)
	}
	if string(buf) != "hello world" {
		t.Errorf("buf = %q, want hello world", buf)
	}
}

// InitMarshallers with an invalid MarshalTo: fatalf fires once (the
// early-return path); the function exits without populating x.Marshaller.
func TestInitMarshallers_invalidName(t *testing.T) {
	x, _ := newMarshalFixture(t)
	x.config.MarshalTo = "not-a-marshaller"
	called := 0
	x.fatalf = func(string, ...any) { called++ }
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitMarshallers(&wg)
	wg.Wait()
	if called != 1 {
		t.Errorf("fatalf called %d times, want 1", called)
	}
	if x.Marshaller != nil {
		t.Error("Marshaller should remain nil on invalid name")
	}
}

// InitMarshallers registers four marshallers into x.Marshallers and
// resolves x.Marshaller to the one named by config.MarshalTo. Verify
// every valid name dispatches. protobufList is NOT in this set —
// it's resolved via InitEnvelopeMarshallers and tested separately.
func TestInitMarshallers_validNames(t *testing.T) {
	for _, name := range []string{MarshallerProtoJSON, "protoText", "msgpack"} {
		t.Run(name, func(t *testing.T) {
			x, rec := newMarshalFixture(t)
			x.config.MarshalTo = name
			var wg sync.WaitGroup
			wg.Add(1)
			x.InitMarshallers(&wg)
			wg.Wait()
			if x.Marshaller == nil {
				t.Fatalf("Marshaller pointer nil after Init for %q", name)
			}
			buf := x.Marshaller(rec)
			if buf == nil {
				t.Fatalf("Marshaller(%q) returned nil buf", name)
			}
			if len(*buf) == 0 && name != "protoText" {
				// protoText can be empty for an empty record; we have a
				// populated rec, so this should never trigger.
				t.Errorf("Marshaller(%q) produced empty buf", name)
			}
		})
	}
}

// TestProtobufListMarshal_roundtrip builds an Envelope with three rows,
// marshals it via protobufListMarshal (length-delimited; no Confluent
// header), parses the result with protodelim, and asserts every row's
// fields survived. This is the wire-format contract with ClickHouse's
// kafka_format='ProtobufList' — the parser ClickHouse uses on the other
// end expects exactly this byte layout: varint(envelope_size) || envelope.
func TestProtobufListMarshal_roundtrip(t *testing.T) {
	x, _ := newMarshalFixture(t)
	x.destBytesPool.Init(func() *[]byte { b := make([]byte, 0, 1024); return &b })

	env := &xtcp_flat_record.Envelope{
		Row: []*xtcp_flat_record.XtcpFlatRecord{
			{Hostname: "host-a", Netns: "/run/netns/ns-1", SocketFd: 11, RecordCounter: 1},
			{Hostname: "host-b", Netns: "/run/netns/ns-2", SocketFd: 22, RecordCounter: 2},
			{Hostname: "host-c", Netns: "/run/netns/ns-3", SocketFd: 33, RecordCounter: 3},
		},
	}
	buf := x.protobufListMarshal(env)
	if buf == nil || len(*buf) == 0 {
		t.Fatalf("protobufListMarshal returned empty buf")
	}

	// Parse back via protodelim. The bytes start with a varint length
	// prefix followed by the encoded Envelope.
	var got xtcp_flat_record.Envelope
	r := bytes.NewReader(*buf)
	if err := protodelim.UnmarshalFrom(r, &got); err != nil {
		t.Fatalf("protodelim.UnmarshalFrom failed: %v", err)
	}
	if len(got.Row) != 3 {
		t.Fatalf("len(got.Row) = %d, want 3", len(got.Row))
	}
	for i, row := range got.Row {
		want := env.Row[i]
		if row.Hostname != want.Hostname {
			t.Errorf("row[%d].Hostname = %q, want %q", i, row.Hostname, want.Hostname)
		}
		if row.Netns != want.Netns {
			t.Errorf("row[%d].Netns = %q, want %q", i, row.Netns, want.Netns)
		}
		if row.SocketFd != want.SocketFd {
			t.Errorf("row[%d].SocketFd = %d, want %d", i, row.SocketFd, want.SocketFd)
		}
		if row.RecordCounter != want.RecordCounter {
			t.Errorf("row[%d].RecordCounter = %d, want %d", i, row.RecordCounter, want.RecordCounter)
		}
	}
}

// TestInitEnvelopeMarshallers_kafkaPair verifies the envelope marshaller
// registry resolves protobufList when paired with a kafka destination.
func TestInitEnvelopeMarshallers_kafkaPair(t *testing.T) {
	x, _ := newMarshalFixture(t)
	x.config.MarshalTo = MarshallerProtobufList
	x.config.Dest = "kafka:redpanda-0:9092"
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitEnvelopeMarshallers(&wg)
	wg.Wait()
	if x.EnvelopeMarshaller == nil {
		t.Fatal("EnvelopeMarshaller nil after Init for protobufList+kafka")
	}
}

// TestInitEnvelopeMarshallers_anyDest verifies the envelope marshaller
// registry resolves protobufList regardless of destination — the
// destination's Send takes bytes, the marshaller doesn't care what
// happens to them.
func TestInitEnvelopeMarshallers_anyDest(t *testing.T) {
	for _, dest := range []string{"kafka:redpanda-0:9092", "null", "udp:127.0.0.1:1234"} {
		t.Run(dest, func(t *testing.T) {
			x, _ := newMarshalFixture(t)
			x.config.MarshalTo = MarshallerProtobufList
			x.config.Dest = dest
			var wg sync.WaitGroup
			wg.Add(1)
			x.InitEnvelopeMarshallers(&wg)
			wg.Wait()
			if x.EnvelopeMarshaller == nil {
				t.Errorf("EnvelopeMarshaller nil for dest=%q", dest)
			}
		})
	}
}
