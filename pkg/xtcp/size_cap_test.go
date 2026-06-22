package xtcp

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// sizeCapRecord builds a record whose serialized size varies with i, so the
// per-row length prefix exercises multiple varint widths (small records → 1
// byte, large bytes field → 2-byte length prefix).
func sizeCapRecord(i int) *xtcp_flat_record.XtcpFlatRecord {
	return &xtcp_flat_record.XtcpFlatRecord{
		Hostname:                "host-" + strings.Repeat("x", i%200),
		Netns:                   "/run/netns/ns",
		SocketFd:                uint64(i),
		RecordCounter:           uint64(i),
		TcpInfoRtt:              uint32(1000 + i),
		InetDiagMsgSocketSource: []byte(strings.Repeat("b", i%300)),
	}
}

// TestEnvelopeRowBytes_exact proves the incremental accumulator is exact:
// summing envelopeRowBytes over every row equals proto.Size(envelope). This is
// the invariant the byte-cap relies on — Envelope has only the `row` field, so
// the running total reproduces proto.Size(Envelope) with no per-check walk.
func TestEnvelopeRowBytes_exact(t *testing.T) {
	for _, n := range []int{0, 1, 5, 64, 65, 500} {
		env := &xtcp_flat_record.Envelope{}
		sum := 0
		for i := range n {
			r := sizeCapRecord(i)
			env.Row = append(env.Row, r)
			sum += envelopeRowBytes(r)
		}
		if got := proto.Size(env); got != sum {
			t.Errorf("n=%d: sum(envelopeRowBytes)=%d, proto.Size(envelope)=%d", n, sum, got)
		}
	}
}

// TestEnvelopeRowBytes_byteCapParity confirms the accumulator trips the byte
// cap at the same row at which the old proto.Size(envelope) > threshold check
// would have, for a representative threshold.
func TestEnvelopeRowBytes_byteCapParity(t *testing.T) {
	const threshold = 4096
	env := &xtcp_flat_record.Envelope{}
	acc := 0
	accTrip, oldTrip := -1, -1
	for i := range 1000 {
		r := sizeCapRecord(i)
		env.Row = append(env.Row, r)
		acc += envelopeRowBytes(r)
		if accTrip == -1 && acc > threshold {
			accTrip = i
		}
		if oldTrip == -1 && proto.Size(env) > threshold {
			oldTrip = i
		}
	}
	if accTrip != oldTrip {
		t.Errorf("byte-cap trip row mismatch: accumulator=%d, proto.Size=%d", accTrip, oldTrip)
	}
	if accTrip == -1 {
		t.Fatal("threshold never tripped — pick a smaller threshold")
	}
}

// BenchmarkEnvelopeSizeCapProtoSize is the OLD path: proto.Size over the whole
// growing envelope every envelopeRowFieldNumber-independent 64 appends.
func BenchmarkEnvelopeSizeCapProtoSize(b *testing.B) {
	const rows = 10000
	const modulus = 64
	const threshold = 768 * 1024
	recs := make([]*xtcp_flat_record.XtcpFlatRecord, rows)
	for i := range recs {
		recs[i] = sizeCapRecord(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		env := &xtcp_flat_record.Envelope{Row: make([]*xtcp_flat_record.XtcpFlatRecord, 0, rows)}
		for i, r := range recs {
			env.Row = append(env.Row, r)
			if (i+1)%modulus == 0 {
				if proto.Size(env) > threshold {
					_ = env
				}
			}
		}
	}
}

// BenchmarkEnvelopeSizeCapAccumulator is the NEW path: an exact running byte
// total updated once per append, checked every append.
func BenchmarkEnvelopeSizeCapAccumulator(b *testing.B) {
	const rows = 10000
	const threshold = 768 * 1024
	recs := make([]*xtcp_flat_record.XtcpFlatRecord, rows)
	for i := range recs {
		recs[i] = sizeCapRecord(i)
	}
	b.ReportAllocs()
	for b.Loop() {
		env := &xtcp_flat_record.Envelope{Row: make([]*xtcp_flat_record.XtcpFlatRecord, 0, rows)}
		acc := 0
		for _, r := range recs {
			env.Row = append(env.Row, r)
			acc += envelopeRowBytes(r)
			if acc > threshold {
				_ = env
			}
		}
	}
}
