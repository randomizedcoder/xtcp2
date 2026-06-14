package xtcp

import (
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/proto"

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
	want := []string{MarshallerProtobufSingle, "protoJson", "protoText", "msgpack"}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("validMarshallers() = %q, missing %q", got, w)
		}
	}
}

func TestProtobufSingleMarshal_roundtrip(t *testing.T) {
	x, rec := newMarshalFixture(t)
	buf := x.protobufSingleMarshal(rec)
	if buf == nil || len(*buf) == 0 {
		t.Fatalf("protobufSingleMarshal returned empty buf")
	}
	var got xtcp_flat_record.XtcpFlatRecord
	if uerr := proto.Unmarshal(*buf, &got); uerr != nil {
		t.Fatalf("Unmarshal of marshaled output failed: %v", uerr)
	}
	if got.Hostname != rec.Hostname || got.SocketFd != rec.SocketFd {
		t.Errorf("roundtrip lost fields: in.Hostname=%q out.Hostname=%q in.SocketFd=%d out.SocketFd=%d",
			rec.Hostname, got.Hostname, rec.SocketFd, got.SocketFd)
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

// InitMarshallers registers four marshallers into x.Marshallers and
// resolves x.Marshaller to the one named by config.MarshalTo. Verify
// every valid name dispatches.
func TestInitMarshallers_validNames(t *testing.T) {
	for _, name := range []string{MarshallerProtobufSingle, "protoJson", "protoText", "msgpack"} {
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
