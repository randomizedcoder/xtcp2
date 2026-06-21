package recordfmt

import (
	"net"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// benchRecord is a realistically-populated record: an ESTABLISHED IPv4 socket
// with a full complement of tcp_info fields, so the marshal benchmarks exercise
// the field-encoding hot path rather than a near-empty message.
func benchRecord() *xtcp_flat_record.XtcpFlatRecord {
	return &xtcp_flat_record.XtcpFlatRecord{
		Hostname:                         "bench-host-01",
		Netns:                            "/run/netns/xtcp2host",
		TimestampNs:                      1.7e18,
		SocketFd:                         42,
		NetlinkerId:                      3,
		InetDiagMsgFamily:                afInet,
		InetDiagMsgState:                 1, // ESTABLISHED
		InetDiagMsgSocketSource:          []byte(net.ParseIP("10.0.0.5").To4()),
		InetDiagMsgSocketSourcePort:      443,
		InetDiagMsgSocketDestination:     []byte(net.ParseIP("10.0.12.99").To4()),
		InetDiagMsgSocketDestinationPort: 51514,
		TcpInfoState:                     1,
		CongestionAlgorithmEnum:          xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC,
		TcpInfoRtt:                       18234,
		TcpInfoRttVar:                    4096,
		TcpInfoMinRtt:                    12011,
		TcpInfoSndCwnd:                   64,
		TcpInfoSndMss:                    1448,
		TcpInfoRcvMss:                    536,
		TcpInfoAdvMss:                    1460,
		TcpInfoBytesAcked:                104857600,
		TcpInfoBytesReceived:             52428800,
		TcpInfoBytesSent:                 104900000,
		TcpInfoBytesRetrans:              8192,
		TcpInfoDelivered:                 72000,
		TcpInfoDeliveryRate:              1250000000,
		TcpInfoSegsOut:                   72100,
		TcpInfoSegsIn:                    36050,
		TcpInfoDataSegsOut:               72000,
		TcpInfoDataSegsIn:                36000,
		TcpInfoPacingRate:                2500000000,
		TcpInfoMaxPacingRate:             ^uint64(0),
		TcpInfoLost:                      3,
		TcpInfoRetrans:                   2,
	}
}

// benchEnvelope returns an n-row envelope, the unit the destination pipeline
// actually marshals (one flush = one envelope).
func benchEnvelope(n int) *xtcp_flat_record.Envelope {
	rows := make([]*xtcp_flat_record.XtcpFlatRecord, n)
	for i := range rows {
		rows[i] = benchRecord()
	}
	return &xtcp_flat_record.Envelope{Row: rows}
}

// --- per-record marshallers (the gRPC / per-record client paths) ---

func BenchmarkMarshalJSON(b *testing.B) {
	r := benchRecord()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalJSON(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalHumanizedJSON(b *testing.B) {
	r := benchRecord()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalHumanizedJSON(r); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalText(b *testing.B) {
	r := benchRecord()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalText(r); err != nil {
			b.Fatal(err)
		}
	}
}

// --- envelope marshallers (the destination flush path; 64-row batch) ---

const benchEnvRows = 64

func BenchmarkMarshalEnvelopeProtobufList(b *testing.B) {
	e := benchEnvelope(benchEnvRows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalEnvelopeProtobufList(e); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAppendEnvelopeProtobufList exercises the pooled, append-to-buffer
// variant the daemon uses on the flush path (no per-call allocation of dst).
func BenchmarkAppendEnvelopeProtobufList(b *testing.B) {
	e := benchEnvelope(benchEnvRows)
	dst := make([]byte, 0, 1<<16)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := AppendEnvelopeProtobufList(dst[:0], e)
		if err != nil {
			b.Fatal(err)
		}
		dst = out[:0]
	}
}

func BenchmarkMarshalEnvelopeJSONL(b *testing.B) {
	e := benchEnvelope(benchEnvRows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalEnvelopeJSONL(e); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalEnvelopeHumanizedJSONL(b *testing.B) {
	e := benchEnvelope(benchEnvRows)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalEnvelopeHumanizedJSONL(e); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMarshalEnvelopeTableCSV exercises the reflection-based column path.
func BenchmarkMarshalEnvelopeTableCSV(b *testing.B) {
	e := benchEnvelope(benchEnvRows)
	cols := AllColumns()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalEnvelopeTable(e, cols, ',', true); err != nil {
			b.Fatal(err)
		}
	}
}
