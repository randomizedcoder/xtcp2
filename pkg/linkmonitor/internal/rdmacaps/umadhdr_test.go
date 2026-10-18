package rdmacaps

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

var abiConstants = map[string]uint64{
	"umadHdrLen":               umadHdrLen,
	"umadHdrOffID":             umadHdrOffID,
	"umadHdrOffStatus":         umadHdrOffStatus,
	"umadHdrOffTimeout":        umadHdrOffTimeout,
	"umadHdrOffRetries":        umadHdrOffRetries,
	"umadHdrOffLength":         umadHdrOffLength,
	"umadHdrOffQPN":            umadHdrOffQPN,
	"umadHdrOffQKey":           umadHdrOffQKey,
	"umadHdrOffLID":            umadHdrOffLID,
	"umadHdrOffSL":             umadHdrOffSL,
	"umadHdrOffPathBits":       umadHdrOffPathBits,
	"umadHdrOffGRHPresent":     umadHdrOffGRHPresent,
	"umadHdrOffGIDIndex":       umadHdrOffGIDIndex,
	"umadHdrOffHopLimit":       umadHdrOffHopLimit,
	"umadHdrOffTrafficClass":   umadHdrOffTrafficClass,
	"umadHdrOffGID":            umadHdrOffGID,
	"umadHdrOffFlowLabel":      umadHdrOffFlowLabel,
	"umadHdrOffPKeyIndex":      umadHdrOffPKeyIndex,
	"umadRegReq2Len":           umadRegReq2Len,
	"umadRegReq2OffID":         umadRegReq2OffID,
	"umadRegReq2OffQPN":        umadRegReq2OffQPN,
	"umadRegReq2OffMgmtClass":  umadRegReq2OffMgmtClass,
	"umadRegReq2OffVersion":    umadRegReq2OffVersion,
	"umadRegReq2OffRes":        umadRegReq2OffRes,
	"umadRegReq2OffFlags":      umadRegReq2OffFlags,
	"umadRegReq2OffMethodMask": umadRegReq2OffMethodMask,
	"umadRegReq2OffOUI":        umadRegReq2OffOUI,
	"umadRegReq2OffRMPP":       umadRegReq2OffRMPP,
	"umadRegisterAgent2":       umadRegisterAgent2,
	"umadUnregisterAgent":      umadUnregisterAgent,
	"umadEnablePKey":           umadEnablePKey,
	"umadABIVersion":           umadABIVersion,
}

// TestABILayout is the umad half of the assessment's ABI layout guard table.
func TestABILayout(t *testing.T) {
	testkit.CheckABILayout(t, []string{"umadhdr.go"}, abiConstants, "rdmacaps", []testkit.ABIRow{
		{Name: "umad_hdr", Category: "positive", Description: "ib_user_mad_hdr size and every offset", Expected: "64 bytes; offsets as in C3", Prefixes: []string{"umadHdr"}},
		{Name: "reg_req2", Category: "positive", Description: "ib_user_mad_reg_req2 size, offsets and the REGISTER_AGENT2 and UNREGISTER_AGENT numbers", Expected: "40 bytes, 0xC0281B04, 0x40041B02", Prefixes: []string{"umadRegReq2", "umadRegisterAgent2", "umadUnregisterAgent"}},
		{Name: "enable_pkey", Category: "boundary", Description: "IB_USER_MAD_ENABLE_PKEY and IB_USER_MAD_ABI_VERSION, which select the 64-byte header and the lowest accepted ABI", Expected: "0x1B03, 5", Prefixes: []string{"umadEnablePKey", "umadABIVersion"}},
		{Name: "reg_req_v1", Category: "negative", Description: "No Go file in the package declares or writes IB_USER_MAD_REGISTER_AGENT", Expected: "no use", Text: "forbidden"},
		{Name: "unmapped", Category: "corner", Description: "A constant in umadhdr.go missing from the comparison map", Expected: "none", Text: "comparison map"},
	})
}

// No capture holds MAD bytes: on rxe, ionic and mlx5 RoCE the tools'
// SMI registration fails with EPROTONOSUPPORT before any write (R0-T03), so
// the golden packets below follow ib_user_mad.h and libibumad's umad_send and
// umad_set_addr, on a little-endian host.

// goldenRequestHeader is encodeUMADRequest's header for agent 7 and a 250 ms
// timeout: id, timeout and the permissive LID; everything else zero.
const goldenRequestHeader = "07000000" + "00000000" + "fa000000" + "00000000" + "00000000" +
	"00000000" + "00000000" + "ffff" + "00" + "00" + "00" + "00" + "00" + "00" +
	"00000000000000000000000000000000" + "00000000" + "0000" + "000000000000"

// goldenReplyHeader is a GSI reply as the kernel queues it on RoCE: agent 3,
// status 0, length 320, from QP1 with the GSI Q_Key, a GRH from GID index 0,
// flow label 0x12345 and P_Key index 2.
const goldenReplyHeader = "03000000" + "00000000" + "00000000" + "00000000" + "40010000" +
	"00000001" + "80010000" + "0000" + "00" + "00" + "01" + "00" + "40" + "00" +
	"fe800000000000000000000000000001" + "00012345" + "0200" + "000000000000"

var goldenReply = umadHeader{Agent: 3, Length: 320, QPN: 1, QKey: 0x80010000, GRHPresent: 1, HopLimit: 64,
	GID: [16]byte{0xfe, 0x80, 15: 1}, FlowLabel: 0x12345, PKeyIndex: 2}

func littleEndian(tb testing.TB) {
	tb.Helper()
	if binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		tb.Skip("golden packets are little-endian")
	}
}

func unhex(tb testing.TB, s string) []byte {
	tb.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		tb.Fatal(err)
	}
	return b
}

// replyPacket is the golden GSI reply with status and, as its MAD, a
// PortInfo GetResp to request(1).
func replyPacket(tb testing.TB, status uint32) *umadPacket {
	tb.Helper()
	var p umadPacket
	copy(p[:], unhex(tb, goldenReplyHeader))
	binary.NativeEndian.PutUint32(p[umadHdrOffStatus:], status)
	mad := portReply(request(1))
	copy(p[umadHdrLen:], mad[:])
	return &p
}

func TestUMADRequestEncode(t *testing.T) {
	littleEndian(t)
	mad := request(1)
	for _, tc := range []struct {
		name, category, description, expected string
		agent, timeout                        uint32
		want                                  string
	}{
		{"encode_request", "positive", "Header and MAD for port 1, agent 7, 250 ms", "the golden header, then request(1) unchanged",
			7, 250, goldenRequestHeader},
		{"zero_timeout", "boundary", "Agent 0 and no timeout", "only the permissive LID is set",
			0, 0, strings.Replace(strings.Replace(goldenRequestHeader, "07", "00", 1), "fa", "00", 1)},
		{"max_fields", "corner", "Agent and timeout at their maximum", "host-order 0xffffffff in both; the address is unchanged",
			^uint32(0), ^uint32(0), "ffffffff" + goldenRequestHeader[8:16] + "ffffffff" + goldenRequestHeader[24:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var p umadPacket
			for i := range p {
				p[i] = 0xa5 // every byte must be written
			}
			encodeUMADRequest(&p, tc.agent, tc.timeout, &mad)
			if got := hex.EncodeToString(p[:umadHdrLen]); got != tc.want {
				t.Fatalf("%s:\n got %s\nwant %s", tc.expected, got, tc.want)
			}
			if !bytes.Equal(p[umadHdrLen:], mad[:]) {
				t.Fatalf("%s: MAD changed", tc.expected)
			}
		})
	}
}

func TestUMADReplyDecode(t *testing.T) {
	littleEndian(t)
	full := len(umadPacket{})
	for _, tc := range []struct {
		name, category, description, expected string
		status                                uint32
		mutate                                func(*umadPacket)
		n                                     int
		agent                                 uint32
		want                                  error
	}{
		{"decode_reply", "positive", "GSI reply for agent 3, status 0, 320 bytes", "MAD copied; header fields equal the golden values",
			0, nil, full, 3, nil},
		{"tid_high_half", "corner", "Reply whose TID high half the kernel rewrote", "accepted here and by validateReply",
			0, func(p *umadPacket) { p[umadHdrLen+8] ^= 0xff }, full, 3, nil},
		{"timeout_header", "negative", "status ETIMEDOUT with only the MAD's 24-byte common header", "DeadlineExceeded",
			uint32(syscall.ETIMEDOUT), nil, umadHdrLen + 24, 3, context.DeadlineExceeded},
		{"timeout_full", "corner", "status ETIMEDOUT on a full-length read", "DeadlineExceeded before the length",
			uint32(syscall.ETIMEDOUT), nil, full, 3, context.DeadlineExceeded},
		{"status_errno", "negative", "status EIO", "EIO",
			uint32(syscall.EIO), nil, full, 3, syscall.EIO},
		{"wrong_agent", "negative", "Reply for agent 3 read by agent 4", "EPROTO",
			0, nil, full, 4, syscall.EPROTO},
		{"short_mad", "boundary", "A 255-byte MAD", "EPROTO",
			0, nil, full - 1, 3, syscall.EPROTO},
		{"header_only", "boundary", "Exactly a header, status 0", "EPROTO",
			0, nil, umadHdrLen, 3, syscall.EPROTO},
		{"short_header", "boundary", "One byte short of a header", "EPROTO, the header not read",
			0, nil, umadHdrLen - 1, 3, syscall.EPROTO},
		{"empty", "boundary", "A zero-byte read", "EPROTO",
			0, nil, 0, 3, syscall.EPROTO},
		{"over_length", "corner", "n beyond the buffer", "EPROTO, no panic",
			0, nil, full + 1, 3, syscall.EPROTO},
		{"negative_n", "corner", "A failed read's -1", "EPROTO, no panic",
			0, nil, -1, 3, syscall.EPROTO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			p := replyPacket(t, tc.status)
			if tc.mutate != nil {
				tc.mutate(p)
			}
			var reply [256]byte
			err := decodeUMADReply(p, tc.n, tc.agent, &reply)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("%s: got %v", tc.expected, err)
			}
			if err != nil {
				if reply != [256]byte{} {
					t.Fatalf("%s: reply written on error", tc.expected)
				}
				return
			}
			if !bytes.Equal(reply[:], p[umadHdrLen:]) {
				t.Fatalf("%s: MAD not copied", tc.expected)
			}
			if err := validateReply(request(1), reply); err != nil {
				t.Fatalf("%s: validateReply: %v", tc.expected, err)
			}
			var h umadHeader
			h.decode((*[umadHdrLen]byte)(p[:umadHdrLen]))
			if h != goldenReply {
				t.Fatalf("%s: header %+v", tc.expected, h)
			}
		})
	}
}

// TestUMADHeaderRoundTrip holds encode and decode to each other over every
// field and to the reserved tail being zero.
func TestUMADHeaderRoundTrip(t *testing.T) {
	littleEndian(t)
	for _, tc := range []struct {
		name, category, description, expected string
		h                                     umadHeader
	}{
		{"golden", "positive", "The golden GSI reply header", "goldenReplyHeader's bytes", goldenReply},
		{"zero", "boundary", "The zero header", "64 zero bytes", umadHeader{}},
		{"all_ones", "corner", "Every field at its maximum", "every byte 0xff but the reserved tail", umadHeader{
			Agent: ^uint32(0), Status: ^uint32(0), TimeoutMs: ^uint32(0), Retries: ^uint32(0), Length: ^uint32(0),
			QPN: ^uint32(0), QKey: ^uint32(0), LID: 0xffff, SL: 0xff, PathBits: 0xff, GRHPresent: 0xff, GIDIndex: 0xff,
			HopLimit: 0xff, TrafficClass: 0xff, GID: [16]byte(bytes.Repeat([]byte{0xff}, 16)), FlowLabel: ^uint32(0), PKeyIndex: 0xffff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var b [umadHdrLen]byte
			for i := range b {
				b[i] = 0xa5
			}
			tc.h.encode(&b)
			if [6]byte(b[umadHdrOffPKeyIndex+2:]) != [6]byte{} {
				t.Fatalf("%s: reserved tail %x", tc.expected, b[umadHdrOffPKeyIndex+2:])
			}
			if tc.name == "golden" && hex.EncodeToString(b[:]) != goldenReplyHeader {
				t.Fatalf("%s: got %x", tc.expected, b)
			}
			var got umadHeader
			got.decode(&b)
			if got != tc.h {
				t.Fatalf("%s: got %+v", tc.expected, got)
			}
		})
	}
}

func TestRegisterAgent2Encode(t *testing.T) {
	littleEndian(t)
	for _, tc := range []struct {
		name, category, description, expected string
		class, version                        uint8
		qpn                                   uint32
	}{
		{"reg_req2_layout", "boundary", "Directed-route SMP class 0x81, version 1", "40 bytes; QP0; class at 8, version at 9; the rest zero", 0x81, 1, 0},
		{"lid_routed_smp", "positive", "LID-routed SMP class 0x01", "QP0", 0x01, 1, 0},
		{"perf_mgt", "positive", "GSI PerfMgt class 0x04", "QP1", 0x04, 1, 1},
		{"class_zero", "corner", "Class 0, version 0", "QP1 and zero class bytes, as umad_register2", 0, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var b [umadRegReq2Len]byte
			for i := range b {
				b[i] = 0xa5 // stale bytes from a reused request
			}
			encodeRegisterAgent2(&b, tc.class, tc.version)
			var want [umadRegReq2Len]byte
			want[umadRegReq2OffQPN] = byte(tc.qpn)
			want[umadRegReq2OffMgmtClass], want[umadRegReq2OffVersion] = tc.class, tc.version
			if b != want {
				t.Fatalf("%s: got %x", tc.expected, b)
			}
			binary.NativeEndian.PutUint32(b[umadRegReq2OffID:], 9)
			if got := registeredAgent(&b); got != 9 {
				t.Fatalf("%s: agent %d", tc.expected, got)
			}
		})
	}
}

func TestUMADCodecAllocs(t *testing.T) {
	mad := request(1)
	var p umadPacket
	var reply [256]byte
	var reg [umadRegReq2Len]byte
	ok := replyPacket(t, 0)
	timeout := replyPacket(t, uint32(syscall.ETIMEDOUT))
	for _, tc := range []struct {
		name, category, description, expected string
		f                                     func()
	}{
		{"encode_request", "positive", "encodeUMADRequest", "0 allocs", func() { encodeUMADRequest(&p, 7, 250, &mad) }},
		{"decode_reply", "positive", "decodeUMADReply of a good reply", "0 allocs", func() { _ = decodeUMADReply(ok, len(ok), 3, &reply) }},
		{"decode_timeout", "negative", "decodeUMADReply of a timed-out send", "0 allocs", func() { _ = decodeUMADReply(timeout, umadHdrLen+24, 3, &reply) }},
		{"decode_short", "boundary", "decodeUMADReply of a short read", "0 allocs", func() { _ = decodeUMADReply(ok, 1, 3, &reply) }},
		{"register", "positive", "encodeRegisterAgent2 and registeredAgent", "0 allocs", func() { encodeRegisterAgent2(&reg, 0x81, 1); _ = registeredAgent(&reg) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := testing.AllocsPerRun(100, tc.f); got != 0 {
				t.Fatalf("%s: %.0f allocs", tc.expected, got)
			}
		})
	}
}

// FuzzUMADHeader decodes arbitrary reads. A header round-trips to its bytes
// with the reserved tail zeroed, and a reply is accepted only at full length
// for its agent with status 0, with the MAD copied.
func FuzzUMADHeader(f *testing.F) {
	f.Add(unhex(f, goldenReplyHeader), uint32(3))
	f.Add(unhex(f, goldenRequestHeader), uint32(7))
	f.Add(replyPacket(f, 0)[:], uint32(3))
	f.Add(replyPacket(f, uint32(syscall.ETIMEDOUT))[:umadHdrLen+24], uint32(3))
	f.Add([]byte{}, uint32(0))
	f.Fuzz(func(t *testing.T, data []byte, agent uint32) {
		var p umadPacket
		copy(p[:], data)
		var reply [256]byte
		err := decodeUMADReply(&p, len(data), agent, &reply)
		if len(data) < umadHdrLen {
			if !errors.Is(err, syscall.EPROTO) {
				t.Fatalf("short read: %v", err)
			}
			return
		}
		var h umadHeader
		hdr := (*[umadHdrLen]byte)(p[:umadHdrLen])
		h.decode(hdr)
		var back [umadHdrLen]byte
		h.encode(&back)
		want := *hdr
		clear(want[umadHdrOffPKeyIndex+2:])
		if back != want {
			t.Fatalf("round trip: %x != %x", back, want)
		}
		if accepted := err == nil; accepted != (len(data) == len(p) && h.Agent == agent && h.Status == 0) {
			t.Fatalf("accepted=%v for n=%d agent=%d/%d status=%d", accepted, len(data), h.Agent, agent, h.Status)
		}
		if err == nil && !bytes.Equal(reply[:], data[umadHdrLen:]) {
			t.Fatal("MAD not copied")
		}
	})
}

func BenchmarkEncodeUMADRequest(b *testing.B) {
	mad := request(1)
	var p umadPacket
	b.ReportAllocs()
	for b.Loop() {
		encodeUMADRequest(&p, 7, 250, &mad)
	}
}

func BenchmarkDecodeUMADReply(b *testing.B) {
	p := replyPacket(b, 0)
	var reply [256]byte
	b.ReportAllocs()
	for b.Loop() {
		if err := decodeUMADReply(p, len(p), 3, &reply); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeRegisterAgent2(b *testing.B) {
	var reg [umadRegReq2Len]byte
	b.ReportAllocs()
	for b.Loop() {
		encodeRegisterAgent2(&reg, 0x81, 1)
	}
}
