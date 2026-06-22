package recordfmt

import (
	"bytes"
	"net"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/encoding/protodelim"
)

// vtEnvelope is a populated multi-row envelope for the protobufList tests.
func vtEnvelope(rows int) *xtcp_flat_record.Envelope {
	e := &xtcp_flat_record.Envelope{}
	for i := range rows {
		e.Row = append(e.Row, &xtcp_flat_record.XtcpFlatRecord{
			Hostname:                    "bench-host-01",
			Netns:                       "/run/netns/xtcp2host",
			SocketFd:                    uint64(i),
			RecordCounter:               uint64(i),
			InetDiagMsgFamily:           afInet,
			InetDiagMsgState:            1,
			InetDiagMsgSocketSource:     []byte(net.ParseIP("10.0.0.5").To4()),
			InetDiagMsgSocketSourcePort: 443,
			CongestionAlgorithmEnum:     xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC,
			TcpInfoRtt:                  uint32(18000 + i),
			TcpInfoMinRtt:               12011,
			TcpInfoSndCwnd:              64,
			TcpInfoBytesAcked:           104857600,
			TcpInfoDeliveryRate:         1250000000,
		})
	}
	return e
}

// TestAppendEnvelopeProtobufList_byteIdentity proves the vtprotobuf-based
// length-delimited encoding is byte-for-byte identical to the protobuf
// runtime's protodelim output — the ClickHouse ProtobufList wire contract.
func TestAppendEnvelopeProtobufList_byteIdentity(t *testing.T) {
	for _, rows := range []int{0, 1, 3, 64} {
		e := vtEnvelope(rows)

		got, err := AppendEnvelopeProtobufList(nil, e)
		if err != nil {
			t.Fatalf("rows=%d: AppendEnvelopeProtobufList: %v", rows, err)
		}

		var want bytes.Buffer
		if _, err := protodelim.MarshalTo(&want, e); err != nil {
			t.Fatalf("rows=%d: protodelim.MarshalTo: %v", rows, err)
		}

		if !bytes.Equal(got, want.Bytes()) {
			t.Errorf("rows=%d: vtproto encoding differs from protodelim\n got=%x\nwant=%x", rows, got, want.Bytes())
		}
	}
}

// TestAppendEnvelopeProtobufList_roundtrip parses the vtproto output back with
// the runtime parser (what ClickHouse uses) and asserts the rows survive.
func TestAppendEnvelopeProtobufList_roundtrip(t *testing.T) {
	e := vtEnvelope(3)
	buf, err := AppendEnvelopeProtobufList(nil, e)
	if err != nil {
		t.Fatal(err)
	}
	var got xtcp_flat_record.Envelope
	if err := protodelim.UnmarshalFrom(bytes.NewReader(buf), &got); err != nil {
		t.Fatalf("protodelim.UnmarshalFrom: %v", err)
	}
	if len(got.Row) != 3 {
		t.Fatalf("len(got.Row)=%d want 3", len(got.Row))
	}
	for i, r := range got.Row {
		if r.SocketFd != uint64(i) || r.Hostname != "bench-host-01" {
			t.Errorf("row[%d] = {fd:%d host:%q}", i, r.SocketFd, r.Hostname)
		}
	}
}

// appendEnvelopeProtobufListProtodelim is the pre-vtproto reference encoder,
// kept here only so the benchmark can compare it against the vtproto path.
func appendEnvelopeProtobufListProtodelim(dst []byte, e *xtcp_flat_record.Envelope) ([]byte, error) {
	w := &benchByteSliceWriter{buf: &dst}
	_, err := protodelim.MarshalTo(w, e)
	return dst, err
}

type benchByteSliceWriter struct{ buf *[]byte }

func (w *benchByteSliceWriter) Write(b []byte) (int, error) {
	*w.buf = append(*w.buf, b...)
	return len(b), nil
}

func BenchmarkProtobufListProtodelim(b *testing.B) {
	e := vtEnvelope(64)
	dst := make([]byte, 0, 1<<16)
	b.ReportAllocs()
	for b.Loop() {
		out, err := appendEnvelopeProtobufListProtodelim(dst[:0], e)
		if err != nil {
			b.Fatal(err)
		}
		dst = out[:0]
	}
}

func BenchmarkProtobufListVT(b *testing.B) {
	e := vtEnvelope(64)
	dst := make([]byte, 0, 1<<16)
	b.ReportAllocs()
	for b.Loop() {
		out, err := AppendEnvelopeProtobufList(dst[:0], e)
		if err != nil {
			b.Fatal(err)
		}
		dst = out[:0]
	}
}
