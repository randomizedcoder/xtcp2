package xtcp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// recordingDest captures every Send() payload. Used by the corner-case
// tests so they can assert on exactly which records the parser emitted —
// not just on Deserialize's return value (which only counts loop
// iterations, including ones that hit `continue`).
type recordingDest struct {
	x       *XTCP
	mu      sync.Mutex
	records [][]byte
}

func newRecordingDest(x *XTCP) *recordingDest { return &recordingDest{x: x} }

func (d *recordingDest) Send(_ context.Context, b *[]byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// The pooled buffer may be reused by the caller, so copy.
	cp := append([]byte(nil), (*b)...)
	d.records = append(d.records, cp)
	return len(cp), nil
}

func (d *recordingDest) Close() error { return nil }

func (d *recordingDest) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.records)
}

// mkNlMsg builds a synthetic netlink message whose on-wire `Len` field is
// `length` and whose buffer occupies `bufSize` bytes. Setting `length` to
// a value that does NOT match `bufSize` is exactly the adversarial shape
// the bounds-check tests need: the parser must trust nothing about
// nlh.Len.
func mkNlMsg(typ uint16, length uint32, bufSize int) []byte {
	if bufSize < xtcpnl.NlMsgHdrSizeCst {
		bufSize = xtcpnl.NlMsgHdrSizeCst
	}
	b := make([]byte, bufSize)
	binary.LittleEndian.PutUint32(b[0:4], length)
	binary.LittleEndian.PutUint16(b[4:6], typ)
	return b
}

// loadRealMultipart returns the netlink payload (pcap headers stripped)
// of the canonical 10-socket inet_diag dump used by the existing
// TestDeserialize. The buffer ends with an NLMSG_DONE.
func loadRealMultipart(tb testing.TB) []byte {
	tb.Helper()
	const path = "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet2.pcap"
	f, err := os.Open(path)
	if err != nil {
		tb.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	bs, err := io.ReadAll(f)
	if err != nil {
		tb.Fatalf("read %s: %v", path, err)
	}
	return bs[xtcpnl.PcapNetlinkOffsetCst:]
}

// signalNetlinkerDone: non-blocking send (default cap=1) covers the happy
// arm; with the channel pre-filled the default branch executes (counter
// increment) and the subsequent blocking send completes once we drain.
//
// A short sleep before draining ensures the goroutine reaches the select
// (and takes the `default` arm + the blocking send) BEFORE the main
// goroutine drains — otherwise there's a race where the drain wins and
// the non-blocking arm fires instead, missing the default branch.
func TestSignalNetlinkerDone_blockingPath(t *testing.T) {
	x := newTestXTCP(t, "null")
	x.netlinkerDoneCh = make(chan netlinkerDone, 1)
	// Pre-fill the channel so the non-blocking send hits `default`.
	x.netlinkerDoneCh <- netlinkerDone{fd: -1}

	args := DeserializeArgs{fd: 42, pC: x.pC}
	done := make(chan struct{})
	go func() {
		x.signalNetlinkerDone(args)
		close(done)
	}()
	// Sleep before draining so the goroutine has time to hit the
	// default arm + reach the blocking send.
	time.Sleep(50 * time.Millisecond)
	<-x.netlinkerDoneCh // unblocks the goroutine's blocking send
	<-x.netlinkerDoneCh // and drains its sent value
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("signalNetlinkerDone did not unblock after drain")
	}
}

// runDeserialize is the common wrapper. It installs a recording
// destination on the test XTCP, runs Deserialize with a bounded timeout
// (any hang fails the test), and returns the recording dest plus the
// (n, err) tuple so the table-driven cases can assert.
func runDeserialize(t *testing.T, x *XTCP, buf []byte) (rec *recordingDest, n uint64, err error) {
	t.Helper()
	rec = newRecordingDest(x)
	x.dest = rec

	nsName := "corner-case-ns"
	args := DeserializeArgs{
		ns:             &nsName,
		fd:             0,
		NLPacket:       &buf,
		xtcpRecordPool: &x.xtcpRecordPool,
		nlhPool:        &x.nlhPool,
		rtaPool:        &x.rtaPool,
		pC:             x.pC,
		pH:             x.pH,
		id:             0,
	}

	// Bound the call: a hang or infinite loop here should fail visibly,
	// not block CI for ten minutes. The panic-recover branch records the
	// failure on the main test goroutine.
	type result struct {
		n   uint64
		err error
		pnc any
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			if p := recover(); p != nil {
				r.pnc = p
			}
			done <- r
		}()
		r.n, r.err = x.Deserialize(context.Background(), args)
	}()

	select {
	case r := <-done:
		if r.pnc != nil {
			t.Fatalf("Deserialize panicked: %v", r.pnc)
		}
		return rec, r.n, r.err
	case <-time.After(3 * time.Second):
		t.Fatalf("Deserialize hung on input of %d bytes", len(buf))
		return nil, 0, fmt.Errorf("unreachable")
	}
}

// TestDeserializeSkipsUnknownNlMsgTypes is a regression test for the bug
// where the parser aborted the multipart parse on the first non-InetDiag
// netlink message — silently dropping every InetDiag record that
// followed in the same response. The fix advances past unknown messages
// and continues; these cases assert both that records still flow and
// that the skipUnknownType counter ticks exactly once per skipped
// message.
//
// Standard linux/netlink.h types tested: NLMSG_NOOP=1, NLMSG_ERROR=2,
// NLMSG_OVERRUN=4, plus a high vendor-ish type to ensure the dispatch
// has no upper bound surprises.
func TestDeserializeSkipsUnknownNlMsgTypes(t *testing.T) {
	const (
		nlNoop    uint16 = 1
		nlError   uint16 = 2
		nlOverrun uint16 = 4
		nlVendor  uint16 = 0xff00
	)

	realPacket := loadRealMultipart(t)

	cases := []struct {
		name     string
		buildBuf func() []byte
		wantMinN uint64
		wantErr  error
		wantSkip float64
	}{
		{
			name:     "real_packet_baseline_no_skips",
			buildBuf: func() []byte { return append([]byte(nil), realPacket...) },
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 0,
		},
		{
			name: "noop_prefix_then_real",
			buildBuf: func() []byte {
				return append(mkNlMsg(nlNoop, 16, 16), realPacket...)
			},
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 1,
		},
		{
			name: "error_prefix_then_real",
			buildBuf: func() []byte {
				// NLMSG_ERROR carries a struct nlmsgerr after the header
				// (≥4 bytes of errno + the original header echoed back).
				// 32 bytes is plenty and exercises a non-zero-body skip.
				return append(mkNlMsg(nlError, 32, 32), realPacket...)
			},
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 1,
		},
		{
			name: "overrun_prefix_then_real",
			buildBuf: func() []byte {
				return append(mkNlMsg(nlOverrun, 24, 24), realPacket...)
			},
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 1,
		},
		{
			name: "vendor_high_type_prefix_then_real",
			buildBuf: func() []byte {
				return append(mkNlMsg(nlVendor, 16, 16), realPacket...)
			},
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 1,
		},
		{
			name: "two_unknown_in_a_row_then_real",
			buildBuf: func() []byte {
				buf := mkNlMsg(nlNoop, 16, 16)
				buf = append(buf, mkNlMsg(nlError, 32, 32)...)
				return append(buf, realPacket...)
			},
			wantMinN: 1,
			wantErr:  nil,
			wantSkip: 2,
		},
		{
			name: "only_unknown_then_done",
			buildBuf: func() []byte {
				return append(
					mkNlMsg(nlNoop, 16, 16),
					mkNlMsg(xtcpnl.NlMsgHdrTypeDoneCst, 16, 16)...,
				)
			},
			wantMinN: 0,
			wantErr:  nil,
			wantSkip: 1,
		},
		{
			name: "only_unknown_no_done",
			buildBuf: func() []byte {
				return mkNlMsg(nlNoop, 16, 16)
			},
			wantMinN: 0,
			wantErr:  nil,
			wantSkip: 1,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			x := newTestDeserializeXTCP(t)
			_, n, err := runDeserialize(t, x, tc.buildBuf())

			if err != tc.wantErr {
				t.Errorf("err = %v want %v (parsed n=%d)", err, tc.wantErr, n)
			}
			if n < tc.wantMinN {
				t.Errorf("n = %d want >= %d", n, tc.wantMinN)
			}

			gotSkip := testutil.ToFloat64(
				x.pC.WithLabelValues("Deserialize", "skipUnknownType", "count"))
			if gotSkip != tc.wantSkip {
				t.Errorf("skipUnknownType counter = %v want %v", gotSkip, tc.wantSkip)
			}
		})
	}
}

// TestDeserializeBaselineProducesRecords is a sanity check that the
// recording-destination plumbing matches the loop counter — i.e. every
// `n` iteration that processes an InetDiag message also produces exactly
// one record on the destination. Catches regressions where the loop
// counter advances but the Send call is skipped (a class of bugs the
// silent-drop change could otherwise mask).
func TestDeserializeBaselineProducesRecords(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	rec, n, err := runDeserialize(t, x, append([]byte(nil), loadRealMultipart(t)...))
	if err != nil {
		t.Fatalf("err = %v want nil", err)
	}
	if n == 0 {
		t.Fatal("n = 0; testdata should contain records")
	}
	// The recording dest receives one Send per real (non-skipped) record.
	// The DONE iteration increments n but doesn't Send.
	if rec.Count() == 0 {
		t.Fatalf("recording dest captured 0 records, n=%d", n)
	}
	if rec.Count() > int(n) {
		t.Errorf("recording dest captured %d records but n=%d", rec.Count(), n)
	}
}

// TestDeserializeEarlyDone covers the multipart shape where NLMSG_DONE
// is the very first message. The parser must return cleanly with n=0
// and err=nil, not bail with a parse error.
func TestDeserializeEarlyDone(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	buf := mkNlMsg(xtcpnl.NlMsgHdrTypeDoneCst, 16, 16)
	rec, n, err := runDeserialize(t, x, buf)
	if err != nil {
		t.Fatalf("err = %v want nil", err)
	}
	if n != 0 {
		t.Errorf("n = %d want 0", n)
	}
	if rec.Count() != 0 {
		t.Errorf("recording dest got %d records, want 0", rec.Count())
	}
}

// TestDeserializeEmptyBuffer: a zero-length buffer must short-circuit
// the loop and return (0, nil). Catches regressions where the
// truncated-header guard fires on a buffer that has nothing wrong with
// it — it just contains nothing.
func TestDeserializeEmptyBuffer(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	_, n, err := runDeserialize(t, x, []byte{})
	if err != nil {
		t.Errorf("err = %v want nil", err)
	}
	if n != 0 {
		t.Errorf("n = %d want 0", n)
	}
}

// TestDeserializeAdversarialNlh exercises malformed nlh.Len values to
// confirm the parser refuses to infinite-loop, panic on a slice bounds
// violation, or read out of bounds. The exact (n, err) shape isn't the
// point — what matters is that the call returns in bounded time without
// panicking.
func TestDeserializeAdversarialNlh(t *testing.T) {
	cases := []struct {
		name     string
		buildBuf func() []byte
	}{
		{
			name: "unknown_type_len_zero",
			buildBuf: func() []byte {
				// bodyLen = 0 - 16 = -16, fix's "bodyLen < 0" guard fires.
				return mkNlMsg(0x42, 0, 16)
			},
		},
		{
			name: "unknown_type_len_below_header_size",
			buildBuf: func() []byte {
				// bodyLen = 8 - 16 = -8, same guard.
				return mkNlMsg(0x42, 8, 16)
			},
		},
		{
			name: "unknown_type_len_equals_header_size",
			buildBuf: func() []byte {
				// bodyLen = 0, valid; parser advances 0 then the outer loop
				// terminates because offset == end after the header read.
				return mkNlMsg(0x42, 16, 16)
			},
		},
		{
			name: "unknown_type_len_beyond_buffer_end",
			buildBuf: func() []byte {
				// nlh.Len lies about message length: claims 1024 in a 32-byte
				// buffer. The bounds check `offset+bodyLen > end` must fire
				// before any OOB slice.
				return mkNlMsg(0x42, 1024, 32)
			},
		},
		{
			name: "unknown_type_len_uint32_max",
			buildBuf: func() []byte {
				return mkNlMsg(0x42, ^uint32(0), 32)
			},
		},
		{
			name: "truncated_below_header_size",
			buildBuf: func() []byte {
				// Buffer shorter than the 16-byte header. The new bounds
				// guard at loop entry must turn this into a clean error
				// return, not a "slice bounds out of range" panic.
				return []byte{0xff, 0x00, 0x00}
			},
		},
		{
			name: "truncated_just_below_header_size",
			buildBuf: func() []byte {
				// 15 bytes — one short of a valid header.
				return make([]byte, xtcpnl.NlMsgHdrSizeCst-1)
			},
		},
		{
			name: "many_unknown_zero_body",
			buildBuf: func() []byte {
				// 200 sequential 16-byte unknown headers. Must walk in O(n)
				// and return, not loop forever or balloon allocations.
				buf := make([]byte, 0, 200*xtcpnl.NlMsgHdrSizeCst)
				for i := 0; i < 200; i++ {
					buf = append(buf, mkNlMsg(0x42, 16, 16)...)
				}
				return buf
			},
		},
		{
			name: "many_unknown_then_done",
			buildBuf: func() []byte {
				// Same as above but DONE-terminated.
				buf := make([]byte, 0, 201*xtcpnl.NlMsgHdrSizeCst)
				for i := 0; i < 200; i++ {
					buf = append(buf, mkNlMsg(0x42, 16, 16)...)
				}
				return append(buf, mkNlMsg(xtcpnl.NlMsgHdrTypeDoneCst, 16, 16)...)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			x := newTestDeserializeXTCP(t)
			// We only assert no-panic / no-hang. Any (n, err) is acceptable.
			_, _, _ = runDeserialize(t, x, tc.buildBuf())
		})
	}
}

// TestDeserializeInetDiagAdversarialAttrs builds full inet-diag messages
// (header + body) whose attribute bodies (the RTAttr/NLA sequence after
// InetDiagMsgSizeCst) contain adversarial sizes — bogus rta.Len smaller
// than RTAttrSizeCst, larger than the buffer, etc. DeserializeAttributes
// must not panic on these.
func TestDeserializeInetDiagAdversarialAttrs(t *testing.T) {
	const (
		hdrSize = xtcpnl.NlMsgHdrSizeCst        // 16
		idmSize = xtcpnl.InetDiagMsgSizeCst     // 72
		rtaSize = xtcpnl.RTAttrSizeCst          // 4
	)

	// buildInetDiagWithAttrBody returns a netlink message of type
	// NlMsgHdrTypeInetDiagCst, nlh.Len set so the attributes section
	// is exactly len(attrBody) bytes, and the body filled with attrBody.
	buildInetDiagWithAttrBody := func(attrBody []byte) []byte {
		bufSize := hdrSize + idmSize + len(attrBody)
		buf := mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst,
			uint32(bufSize), bufSize)
		copy(buf[hdrSize+idmSize:], attrBody)
		return buf
	}

	cases := []struct {
		name     string
		attrBody []byte
	}{
		{
			// Only 2 bytes of attribute body — less than RTAttrSizeCst.
			// DeserializeRTAttr slice would panic on [offset:offset+4].
			name:     "attr_body_shorter_than_rta_header",
			attrBody: []byte{0x00, 0x00},
		},
		{
			// rta.Len = 0 — negative attribute length after subtraction.
			name: "rta_len_zero",
			attrBody: func() []byte {
				b := make([]byte, 32)
				binary.LittleEndian.PutUint16(b[0:2], 0) // rta.Len
				binary.LittleEndian.PutUint16(b[2:4], 1) // rta.Type
				return b
			}(),
		},
		{
			// rta.Len = 2 — smaller than RTAttrSizeCst (4). Negative.
			name: "rta_len_below_header_size",
			attrBody: func() []byte {
				b := make([]byte, 32)
				binary.LittleEndian.PutUint16(b[0:2], 2)
				return b
			}(),
		},
		{
			// rta.Len lies about a huge attribute (claims 1024 bytes
			// in an 8-byte body). Slice would extend past the buffer.
			name: "rta_len_beyond_buffer",
			attrBody: func() []byte {
				b := make([]byte, 8)
				binary.LittleEndian.PutUint16(b[0:2], 1024)
				binary.LittleEndian.PutUint16(b[2:4], 1)
				return b
			}(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("DeserializeAttributes panicked on adversarial input %q: %v", tc.name, r)
				}
			}()
			x := newTestDeserializeXTCP(t)
			_, _, _ = runDeserialize(t, x, buildInetDiagWithAttrBody(tc.attrBody))
		})
	}
}

// TestDeserializeInetDiagAdversarialNlh: same idea as the unknown-type
// adversarial cases, but with nlh.Type = NlMsgHdrTypeInetDiagCst so the
// parser routes into processInetDiagRecord — which slices
// (*d.NLPacket)[offset : offset+InetDiagMsgSizeCst] and computes a
// body-length from nlh.Len that can go negative. The parser must reject
// the truncated/lying input cleanly instead of panicking with a slice
// bounds violation.
func TestDeserializeInetDiagAdversarialNlh(t *testing.T) {
	cases := []struct {
		name     string
		buildBuf func() []byte
	}{
		{
			// nlh.Len == header size, no body. offset+InetDiagMsgSizeCst
			// would slice past end-of-buffer.
			name: "inet_diag_len_equals_header_no_body",
			buildBuf: func() []byte {
				return mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst, 16, 16)
			},
		},
		{
			// nlh.Len == header + half an InetDiagMsg. The 72-byte
			// inet-diag slice would extend past the buffer's end.
			name: "inet_diag_len_partial_body",
			buildBuf: func() []byte {
				return mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst,
					uint32(xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst/2),
					xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst/2)
			},
		},
		{
			// nlh.Len < InetDiagMsgSizeCst+NlMsgHdrSizeCst → attributes
			// length goes negative in processInetDiagRecord.
			name: "inet_diag_len_below_msg_size",
			buildBuf: func() []byte {
				return mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst, 20,
					xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst)
			},
		},
		{
			// nlh.Len lies about a huge message in a small buffer.
			name: "inet_diag_len_beyond_buffer",
			buildBuf: func() []byte {
				return mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst, 4096,
					xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst)
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Deserialize panicked on adversarial input %q: %v", tc.name, r)
				}
			}()
			x := newTestDeserializeXTCP(t)
			_, _, _ = runDeserialize(t, x, tc.buildBuf())
		})
	}
}
