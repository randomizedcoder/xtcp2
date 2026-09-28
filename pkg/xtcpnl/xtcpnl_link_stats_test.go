package xtcpnl

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Where the positive cases come from.
//
// They come from committed captures, and that was not the expectation going
// in: `ParseNewLink`'s own doc comment used to say IFLA_STATS and
// IFLA_STATS64 "are absent from every reply in the committed fixtures because
// the request sets RTEXT_FILTER_SKIP_STATS", and the plan that added this file
// was written to capture `ip -s link show` in order to obtain them.
//
// They were already here. `ip -4 addr show` and `ip neigh show` issue their
// link dump through a path that carries no IFLA_EXT_MASK at all —
// rtnl_linkdump_req_filter_fn forwards filter_fn only for AF_UNSPEC and
// AF_PACKET (lib/libnetlink.c:595) — so no RTEXT_FILTER_SKIP_STATS reaches the
// kernel and it appends both attributes to every reply. That is the same
// mechanism pkg/nlparity's allowlist already describes as the cause of
// GOIP_PARITY_CONTROL_NOISY on exactly those two commands: the counters move
// between captures. The two facts had been recorded in the same repository
// without being put together.
//
// So `-s` is still worth doing — it is a distinct request shape, and it is the
// only way to get stats onto `link show` — but it was never what made this
// decode reachable.
const (
	tdStatsV4    = tdDumpGetAddrV4_7_1_4
	tdStatsNeigh = tdDumps_7_1_4 + "/netlink_route_getneigh.pcap"
)

// u32le and u64le build attribute payloads for the constructed rows. Positive
// cases read captured bytes; everything short, long or extreme is built here,
// because no capture produces a truncated attribute on purpose.
func u32le(vals ...uint32) []byte {
	b := make([]byte, 4*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	return b
}

func u64le(vals ...uint64) []byte {
	b := make([]byte, 8*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint64(b[i*8:], v)
	}
	return b
}

// fullStats64 is a 25-field payload whose Nth field holds N+1, so every field
// that is decoded into the wrong slot is caught by value and not only by
// length.
func fullStats64() []byte {
	v := make([]uint64, RtnlLinkStats64SizeCst/8)
	for i := range v {
		v[i] = uint64(i) + 1
	}
	return u64le(v...)
}

// fullStats is the same idea at 24 fields and 32 bits.
func fullStats() []byte {
	v := make([]uint32, RtnlLinkStatsSizeCst/4)
	for i := range v {
		v[i] = uint32(i) + 1
	}
	return u32le(v...)
}

// TestLinkStatsStructSizes pins the two wire sizes against the Go structs.
//
// unsafe.Sizeof rather than a restated field count: a count agrees with itself
// while the deserializer's field list has drifted, whereas a size disagrees
// the moment a field is added to the struct. The round-trip rows in
// TestDecodeLinkStats are the other half — they write a distinct value per
// field and read every one back, so a field added to the struct but forgotten
// in the list fails there.
//
// go test ./pkg/xtcpnl/ -run TestLinkStatsStructSizes
func TestLinkStatsStructSizes(t *testing.T) {
	tests := []struct {
		description string
		got         uintptr
		want        uintptr
	}{
		{
			description: "positive: rtnl_link_stats is 24 __u32 = 96 bytes",
			got:         unsafe.Sizeof(RtnlLinkStats{}),
			want:        RtnlLinkStatsSizeCst,
		},
		{
			description: "positive: rtnl_link_stats64 is 25 __u64 = 200 bytes, one field MORE than the 32-bit struct",
			got:         unsafe.Sizeof(RtnlLinkStats64{}),
			want:        RtnlLinkStats64SizeCst,
		},
		{
			description: "boundary: the 64-bit struct is not merely the 32-bit one at double width — 96*2 = 192, not 200",
			got:         unsafe.Sizeof(RtnlLinkStats64{}) - 2*unsafe.Sizeof(RtnlLinkStats{}),
			want:        8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %d, want %d", tt.got, tt.want)
			}
		})
	}
}

// TestDecodeLinkStats drives the selection and length rules of iproute2's
// get_rtnl_link_stats_rta (lib/utils.c:1549-1588).
//
// go test ./pkg/xtcpnl/ -run TestDecodeLinkStats
func TestDecodeLinkStats(t *testing.T) {
	tests := []struct {
		description string
		stats64     []byte
		stats       []byte
		wantErr     error
		// check runs only when wantErr is nil.
		check func(t *testing.T, s RtnlLinkStats64)
	}{
		{
			description: "positive: an exact 200-byte IFLA_STATS64 decodes every one of the 25 fields in order",
			stats64:     fullStats64(),
			check: func(t *testing.T, s RtnlLinkStats64) {
				want := RtnlLinkStats64{
					RxPackets: 1, TxPackets: 2, RxBytes: 3, TxBytes: 4,
					RxErrors: 5, TxErrors: 6, RxDropped: 7, TxDropped: 8,
					Multicast: 9, Collisions: 10,
					RxLengthErrors: 11, RxOverErrors: 12, RxCrcErrors: 13,
					RxFrameErrors: 14, RxFifoErrors: 15, RxMissedErrors: 16,
					TxAbortedErrors: 17, TxCarrierErrors: 18, TxFifoErrors: 19,
					TxHeartbeatErrors: 20, TxWindowErrors: 21,
					RxCompressed: 22, TxCompressed: 23, RxNohandler: 24,
					RxOtherhostDropped: 25,
				}
				if s != want {
					t.Errorf("got %+v, want %+v", s, want)
				}
			},
		},
		{
			description: "positive: an exact 96-byte IFLA_STATS is widened field by field, leaving the 25th zero",
			stats:       fullStats(),
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxPackets != 1 || s.RxNohandler != 24 {
					t.Errorf("first/last shared field = %d/%d, want 1/24",
						s.RxPackets, s.RxNohandler)
				}
				// The field the 32-bit struct does not have. Widening must
				// not invent it from whatever followed the payload.
				if s.RxOtherhostDropped != 0 {
					t.Errorf("RxOtherhostDropped = %d, want 0 — the 32-bit struct has no such field",
						s.RxOtherhostDropped)
				}
			},
		},
		{
			description: "positive: IFLA_STATS64 wins when both are present, which is the case on every real reply",
			stats64:     fullStats64(),
			stats:       u32le(999, 999, 999),
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxPackets != 1 {
					t.Errorf("RxPackets = %d, want 1 from IFLA_STATS64", s.RxPackets)
				}
			},
		},
		{
			description: "corner: IFLA_STATS64 wins by PRESENCE, not by length — an empty one beats a full IFLA_STATS",
			stats64:     []byte{},
			stats:       fullStats(),
			check: func(t *testing.T, s RtnlLinkStats64) {
				// Upstream tests tb[IFLA_STATS64] for non-NULL and never looks
				// at its length before committing to it, so a zero-length
				// attribute selects the 64-bit arm and then zero-fills the
				// whole struct. Preferring the longer attribute would be more
				// useful and would not be iproute2.
				if s != (RtnlLinkStats64{}) {
					t.Errorf("got %+v, want the zero struct", s)
				}
			},
		},
		{
			description: "boundary: a payload one field short leaves that field zero and decodes the rest",
			stats64:     fullStats64()[:RtnlLinkStats64SizeCst-8],
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxNohandler != 24 {
					t.Errorf("RxNohandler = %d, want 24 — the last field that IS present", s.RxNohandler)
				}
				if s.RxOtherhostDropped != 0 {
					t.Errorf("RxOtherhostDropped = %d, want 0 — the tail is zero-filled, not left undefined",
						s.RxOtherhostDropped)
				}
			},
		},
		{
			description: "boundary: a payload from a kernel with 24 of the 25 fields — the real short case, and why short is tolerated rather than rejected",
			stats64:     fullStats64()[:24*8],
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxPackets != 1 || s.RxNohandler != 24 || s.RxOtherhostDropped != 0 {
					t.Errorf("got rxp=%d nohandler=%d otherhost=%d, want 1/24/0",
						s.RxPackets, s.RxNohandler, s.RxOtherhostDropped)
				}
			},
		},
		{
			description: "boundary: a payload longer than the struct is truncated, not an error — a newer kernel with a 26th counter must still decode",
			stats64:     append(fullStats64(), u64le(26, 27)...),
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxOtherhostDropped != 25 {
					t.Errorf("RxOtherhostDropped = %d, want 25 — the trailing fields are ignored, not shifted in",
						s.RxOtherhostDropped)
				}
			},
		},
		{
			description: "boundary: a payload that stops mid-field does not decode a partial value",
			stats64:     fullStats64()[:12], // one whole field plus half of the next
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s.RxPackets != 1 {
					t.Errorf("RxPackets = %d, want 1", s.RxPackets)
				}
				if s.TxPackets != 0 {
					t.Errorf("TxPackets = %d, want 0 — four of its eight bytes were present and that is not a value",
						s.TxPackets)
				}
			},
		},
		{
			description: "boundary: a zero-length payload decodes to the zero struct without error",
			stats64:     []byte{},
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s != (RtnlLinkStats64{}) {
					t.Errorf("got %+v, want the zero struct", s)
				}
			},
		},
		{
			description: "corner: a 32-bit counter at 0xFFFFFFFF widens to 4294967295, not to 0xFFFFFFFFFFFFFFFF",
			stats:       u32le(0xFFFFFFFF),
			check: func(t *testing.T, s RtnlLinkStats64) {
				const want = uint64(4294967295)
				if s.RxPackets != want {
					t.Errorf("RxPackets = %#x, want %#x — an unsigned widening must zero-extend",
						s.RxPackets, want)
				}
			},
		},
		{
			description: "negative: neither attribute present is ErrLinkStatsNone, because absent and zero render differently",
			stats64:     nil,
			stats:       nil,
			wantErr:     ErrLinkStatsNone,
		},
		{
			description: "negative: an empty non-nil IFLA_STATS is still present — nil is the only absence",
			stats:       []byte{},
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s != (RtnlLinkStats64{}) {
					t.Errorf("got %+v, want the zero struct", s)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := DecodeLinkStats(tt.stats64, tt.stats)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				if got != (RtnlLinkStats64{}) {
					t.Errorf("got %+v alongside the error, want the zero struct", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tt.check(t, got)
		})
	}
}

// TestWidenRtnlLinkStats checks the widening on its own, away from the
// selection rule.
//
// go test ./pkg/xtcpnl/ -run TestWidenRtnlLinkStats
func TestWidenRtnlLinkStats(t *testing.T) {
	tests := []struct {
		description string
		in          RtnlLinkStats
		check       func(t *testing.T, s RtnlLinkStats64)
	}{
		{
			description: "positive: every one of the 24 shared fields carries across to its own slot",
			in: RtnlLinkStats{
				RxPackets: 1, TxPackets: 2, RxBytes: 3, TxBytes: 4,
				RxErrors: 5, TxErrors: 6, RxDropped: 7, TxDropped: 8,
				Multicast: 9, Collisions: 10,
				RxLengthErrors: 11, RxOverErrors: 12, RxCrcErrors: 13,
				RxFrameErrors: 14, RxFifoErrors: 15, RxMissedErrors: 16,
				TxAbortedErrors: 17, TxCarrierErrors: 18, TxFifoErrors: 19,
				TxHeartbeatErrors: 20, TxWindowErrors: 21,
				RxCompressed: 22, TxCompressed: 23, RxNohandler: 24,
			},
			check: func(t *testing.T, s RtnlLinkStats64) {
				want := RtnlLinkStats64{
					RxPackets: 1, TxPackets: 2, RxBytes: 3, TxBytes: 4,
					RxErrors: 5, TxErrors: 6, RxDropped: 7, TxDropped: 8,
					Multicast: 9, Collisions: 10,
					RxLengthErrors: 11, RxOverErrors: 12, RxCrcErrors: 13,
					RxFrameErrors: 14, RxFifoErrors: 15, RxMissedErrors: 16,
					TxAbortedErrors: 17, TxCarrierErrors: 18, TxFifoErrors: 19,
					TxHeartbeatErrors: 20, TxWindowErrors: 21,
					RxCompressed: 22, TxCompressed: 23, RxNohandler: 24,
				}
				if s != want {
					t.Errorf("got %+v, want %+v", s, want)
				}
			},
		},
		{
			description: "boundary: the zero struct widens to the zero struct",
			in:          RtnlLinkStats{},
			check: func(t *testing.T, s RtnlLinkStats64) {
				if s != (RtnlLinkStats64{}) {
					t.Errorf("got %+v, want the zero struct", s)
				}
			},
		},
		{
			description: "corner: every field at 0xFFFFFFFF stays under 2^32 after widening",
			in: RtnlLinkStats{
				RxPackets: 0xFFFFFFFF, TxPackets: 0xFFFFFFFF,
				RxBytes: 0xFFFFFFFF, RxNohandler: 0xFFFFFFFF,
			},
			check: func(t *testing.T, s RtnlLinkStats64) {
				for name, v := range map[string]uint64{
					"RxPackets": s.RxPackets, "TxPackets": s.TxPackets,
					"RxBytes": s.RxBytes, "RxNohandler": s.RxNohandler,
				} {
					if v != 4294967295 {
						t.Errorf("%s = %#x, want 0xffffffff", name, v)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			tt.check(t, WidenRtnlLinkStats(tt.in))
		})
	}
}

// TestLinkStatsRealFixtures is the positive case that reads captured bytes,
// and it asserts a cross-check rather than a transcribed number.
//
// Every link reply in these two dumps carries IFLA_STATS and IFLA_STATS64
// both, and the kernel fills them from the same per-device counters — so
// decoding each independently and comparing the 24 shared fields is a real
// assertion about this package's two decoders that no single golden value
// could make. It is also the only form of positive here that survives a
// re-capture: the counters move between runs (that is why these two commands
// report GOIP_PARITY_CONTROL_NOISY), so a transcribed `rx_packets = 85` would
// be stale the next time the fixtures are regenerated.
//
// The comparison is valid only while no counter has passed 2^32, which is
// asserted rather than assumed.
//
// go test ./pkg/xtcpnl/ -run TestLinkStatsRealFixtures
func TestLinkStatsRealFixtures(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		wantLinks   int
	}{
		{
			description: "positive: `ip -4 addr show`'s link dump carries stats on every link, because its request has no IFLA_EXT_MASK",
			filename:    tdStatsV4,
			wantLinks:   3,
		},
		{
			description: "positive: `ip neigh show`'s ll_init_map dump carries them for the same reason",
			filename:    tdStatsNeigh,
			wantLinks:   3,
		},
		{
			description: "positive: `ip -s link show` carries them because its request ASKED, which is the only fixture here that did",
			filename:    tdDumpGetLinkStats_7_1_4,
			wantLinks:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			links := 0
			for _, msg := range newLinkBodies(t, tt.filename) {
				raw32, raw64 := rawLinkStatsAttrs(t, msg)
				if raw32 == nil || raw64 == nil {
					t.Fatalf("link reply carries IFLA_STATS=%v IFLA_STATS64=%v, want both",
						raw32 != nil, raw64 != nil)
				}
				if len(raw32) != RtnlLinkStatsSizeCst {
					t.Errorf("IFLA_STATS payload = %d bytes, want %d",
						len(raw32), RtnlLinkStatsSizeCst)
				}
				if len(raw64) != RtnlLinkStats64SizeCst {
					t.Errorf("IFLA_STATS64 payload = %d bytes, want %d",
						len(raw64), RtnlLinkStats64SizeCst)
				}

				var s32 RtnlLinkStats
				if _, err := DeserializeRtnlLinkStats(raw32, &s32); err != nil {
					t.Fatalf("DeserializeRtnlLinkStats: %v", err)
				}
				var s64 RtnlLinkStats64
				if _, err := DeserializeRtnlLinkStats64(raw64, &s64); err != nil {
					t.Fatalf("DeserializeRtnlLinkStats64: %v", err)
				}

				// The cross-check. Widening the 32-bit decode must reproduce
				// the 64-bit one exactly, except for the field the 32-bit
				// struct does not have.
				widened := WidenRtnlLinkStats(s32)
				want := s64
				want.RxOtherhostDropped = 0
				if widened != want {
					t.Errorf("widened IFLA_STATS != IFLA_STATS64\n got %+v\nwant %+v",
						widened, want)
				}

				// And the premise that makes the cross-check meaningful.
				if s64.RxBytes >= 1<<32 || s64.TxBytes >= 1<<32 {
					t.Errorf("a counter has passed 2^32 (rx=%d tx=%d); the 32-bit "+
						"attribute has wrapped and this comparison no longer holds",
						s64.RxBytes, s64.TxBytes)
				}

				// DecodeLinkStats must pick the 64-bit one.
				got, err := DecodeLinkStats(raw64, raw32)
				if err != nil {
					t.Fatalf("DecodeLinkStats: %v", err)
				}
				if got != s64 {
					t.Errorf("DecodeLinkStats chose %+v, want the IFLA_STATS64 decode %+v",
						got, s64)
				}
				links++
			}
			if links != tt.wantLinks {
				t.Errorf("%d link replies with stats, want %d", links, tt.wantLinks)
			}
		})
	}
}

// TestParseNewLinkStats checks the wiring: LinkInfo.Stats is nil exactly when
// the reply carried neither attribute, and set otherwise.
//
// go test ./pkg/xtcpnl/ -run TestParseNewLinkStats
func TestParseNewLinkStats(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		wantStats   bool
	}{
		{
			description: "positive: a dump whose request carried no IFLA_EXT_MASK yields Stats on every link",
			filename:    tdStatsV4,
			wantStats:   true,
		},
		{
			description: "negative: `ip link show` sets RTEXT_FILTER_SKIP_STATS, so Stats is nil — absent, not a zero struct",
			filename:    tdDumpGetLink_7_1_4,
			wantStats:   false,
		},
		{
			description: "positive: `ip -s link show` is the same dump with that bit cleared, so every link has Stats",
			filename:    tdDumpGetLinkStats_7_1_4,
			wantStats:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			bodies := newLinkBodies(t, tt.filename)
			if len(bodies) == 0 {
				t.Fatalf("%s holds no RTM_NEWLINK replies", tt.filename)
			}
			for _, msg := range bodies {
				li, err := ParseNewLink(msg)
				if err != nil {
					t.Fatalf("ParseNewLink: %v", err)
				}
				if (li.Stats != nil) != tt.wantStats {
					t.Fatalf("link %q: Stats != nil is %v, want %v",
						li.Name, li.Stats != nil, tt.wantStats)
				}
			}
		})
	}
}

// TestLinkStatsRequestExtMask pins the one byte that separates the two link
// dumps, which is the entire wire-level content of `-s`.
//
// ip/ipaddress.c:2017-2026 builds IFLA_EXT_MASK from two independent bits:
//
//	if (filter.vfinfo)  filt_mask |= RTEXT_FILTER_VF;         /* 1, default on */
//	if (!show_stats)    filt_mask |= RTEXT_FILTER_SKIP_STATS; /* 8 */
//
// so `ip link show` sends 0x09 and `ip -s link show` sends 0x01. Everything
// else about the datagram — length, flags, family, sequence handling — is
// identical, which is why the two captures are the same size and why the
// assertion has to be on this attribute rather than on the request as a whole.
//
// This is the assertion that would catch goip sending a plain `link show`
// request while printing a stats block anyway: the reply-side tests above
// cannot, because they read whatever the capture contains.
//
// go test ./pkg/xtcpnl/ -run TestLinkStatsRequestExtMask
func TestLinkStatsRequestExtMask(t *testing.T) {
	tests := []struct {
		description string
		filename    string
		want        uint32
	}{
		{
			description: "positive: `ip link show` sends RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS",
			filename:    tdDumpGetLink_7_1_4,
			want:        0x09,
		},
		{
			description: "positive: `ip -s link show` clears SKIP_STATS and sends RTEXT_FILTER_VF alone",
			filename:    tdDumpGetLinkStats_7_1_4,
			want:        0x01,
		},
		{
			description: "boundary: the delta is exactly RTEXT_FILTER_SKIP_STATS and nothing else",
			filename:    "",
			want:        0x08,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.filename == "" {
				plain := getLinkRequestExtMask(t, tdDumpGetLink_7_1_4)
				stats := getLinkRequestExtMask(t, tdDumpGetLinkStats_7_1_4)
				if got := plain ^ stats; got != tt.want {
					t.Errorf("mask delta = %#x, want %#x", got, tt.want)
				}
				return
			}
			if got := getLinkRequestExtMask(t, tt.filename); got != tt.want {
				t.Errorf("IFLA_EXT_MASK = %#x, want %#x", got, tt.want)
			}
		})
	}
}

// getLinkRequestExtMask returns the IFLA_EXT_MASK of the first RTM_GETLINK
// request in a capture, failing the test if there is no such request or it
// carries no mask.
//
// "No mask" is a fatal rather than a zero, because zero is a mask a request
// can legitimately send and the two must not be confused — that distinction is
// the same one ErrLinkStatsNone draws on the reply side.
func getLinkRequestExtMask(t *testing.T, filename string) uint32 {
	t.Helper()
	bs, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filename, err)
	}
	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", filename, err)
	}
	for i, r := range records {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s record %d: NetlinkPayload: %v", filename, i, perr)
		}
		var h NlMsgHdr
		if _, herr := DeserializeNlMsgHdr(body, &h); herr != nil {
			continue
		}
		if h.Type != unix.RTM_GETLINK || int(h.Len) > len(body) {
			continue
		}
		msg := body[NlMsgHdrSizeCst:h.Len]
		if len(msg) < IfInfomsgSizeCst {
			continue
		}
		found := false
		var mask uint32
		if werr := WalkRTAttrs(msg[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
			if atype == uint16(unix.IFLA_EXT_MASK) && len(val) == 4 {
				mask = binary.LittleEndian.Uint32(val)
				found = true
			}
		}); werr != nil {
			t.Fatalf("%s record %d: WalkRTAttrs: %v", filename, i, werr)
		}
		if !found {
			t.Fatalf("%s: the RTM_GETLINK request carries no IFLA_EXT_MASK", filename)
		}
		return mask
	}
	t.Fatalf("%s holds no RTM_GETLINK request", filename)
	return 0
}

// newLinkBodies returns the body of every RTM_NEWLINK message in a capture,
// walking the multipart runs a dump packs into one pcap record.
func newLinkBodies(t *testing.T, filename string) [][]byte {
	t.Helper()
	bs, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filename, err)
	}
	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", filename, err)
	}

	var out [][]byte
	for i, r := range records {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s record %d: NetlinkPayload: %v", filename, i, perr)
		}
		for off := 0; off+NlMsgHdrSizeCst <= len(body); {
			var h NlMsgHdr
			if _, herr := DeserializeNlMsgHdr(body[off:], &h); herr != nil {
				break
			}
			if h.Len < NlMsgHdrSizeCst || off+int(h.Len) > len(body) {
				break
			}
			if h.Type == unix.RTM_NEWLINK {
				out = append(out, body[off+NlMsgHdrSizeCst:off+int(h.Len)])
			}
			off += int(h.Len+3) &^ 3
		}
	}
	return out
}

// rawLinkStatsAttrs pulls the two attributes out of a link message body
// without going through ParseNewLink, so the test can compare the decoders
// against each other rather than against the thing under test.
func rawLinkStatsAttrs(t *testing.T, msg []byte) (stats, stats64 []byte) {
	t.Helper()
	if len(msg) < IfInfomsgSizeCst {
		t.Fatalf("link message is %d bytes, shorter than an ifinfomsg", len(msg))
	}
	if err := WalkRTAttrs(msg[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFLA_STATS):
			stats = val
		case uint16(unix.IFLA_STATS64):
			stats64 = val
		}
	}); err != nil {
		t.Fatalf("WalkRTAttrs: %v", err)
	}
	return stats, stats64
}
