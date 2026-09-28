package req

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is **Tier A** of the parity harness, and it is the primary gate.
//
// Tier A asserts that goip's request builders, called live, produce the bytes
// the pinned `ip` was recorded emitting. Tier B asserts the comparator still
// finds recorded ip↔goip pcap pairs clean, which stays green even if today's
// goip is broken — it is a regression gate on the fixture, not on the code.
// Only Tier A can fail because goip changed, and it can do so under plain
// `go test`, with no socket, no root and no capture VM, because every function
// under test is pure.
//
// # The rule about where expectations come from
//
// Positive rows read real captured bytes. Not one expectation in this file is
// transcribed from iproute2's C source, because the C source is what the
// implementation was written from — asserting one against the other would only
// prove the transcription was self-consistent. The captures are the
// independent witness.

const (
	// The bulk nlmon captures are the only fixtures in the repo that hold
	// iproute2's REQUEST bytes; the extracted *_dump.pcap files keep the
	// replies and discard the request. Reached across packages rather than
	// copied, for the reason pkg/nlparity/testdata_test.go gives.
	tdBulkGetLink  = "../../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getlink.pcap"
	tdBulkGetRoute = "../../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getroute.pcap"
	tdGetNeigh     = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh.pcap"

	// The gated-topology route captures, one command each, taken in a microVM
	// with no other netlink traffic on the host. That is what makes "exactly
	// one RTM_GETROUTE request" an assertion the route rows can make and the
	// bulk desktop captures above cannot.
	tdGatedGetRoute    = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute.pcap"
	tdGatedGetRoute6   = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute6.pcap"
	tdGatedGetRouteAll = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_table_all.pcap"
)

// canonicalRequests returns every request in a capture with nlmsg_seq and
// nlmsg_pid zeroed, as whole datagrams' worth of message bytes.
//
// Those two fields are the only ones that legitimately differ between two runs
// of the same command: iproute2 seeds seq from time(NULL) (lib/libnetlink.c:249)
// and pid is the socket's port id. Everything else in a request is
// tool-controlled and byte-deterministic, which is what makes full equality
// the right assertion here rather than a field-by-field comparison.
func canonicalRequests(t *testing.T, path string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, m := range mustCapture(t, path).Msgs() {
		if !m.IsRequest() {
			continue
		}
		out = append(out, canonicalize(m))
	}
	return out
}

func TestTierANeighShowRequests(t *testing.T) {
	reqs := canonicalRequests(t, tdGetNeigh)
	if len(reqs) != 2 {
		t.Fatalf("requests in neighbor capture = %d, want link dump then neighbor dump", len(reqs))
	}
	link, err := NeighShowLinkDump(123)
	if err != nil {
		t.Fatal(err)
	}
	neigh := NeighShowDump(unix.AF_UNSPEC, 124)
	if !bytes.Equal(zeroSeqPid(link), reqs[0]) {
		t.Fatalf("link-map request differs\n got %x\nwant %x", zeroSeqPid(link), reqs[0])
	}
	if !bytes.Equal(zeroSeqPid(neigh), reqs[1]) {
		t.Fatalf("neighbor request differs\n got %x\nwant %x", zeroSeqPid(neigh), reqs[1])
	}
}

// mustCapture parses a committed pcap or fails the test.
func mustCapture(t *testing.T, path string) nlparity.Capture {
	t.Helper()
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		t.Fatalf("parse capture %s: %v", path, err)
	}
	return c
}

// canonicalize rebuilds one captured message with nlmsg_seq and nlmsg_pid
// zeroed. Split out of canonicalRequests so a caller that needs its own
// selection — the addr capture holds four of our requests among eighteen —
// can reuse the canonicalisation without reimplementing it.
func canonicalize(m nlparity.Msg) []byte {
	full := make([]byte, 0, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	var hdr [xtcpnl.NlMsgHdrSizeCst]byte
	binary.LittleEndian.PutUint32(hdr[0:4], m.Hdr.Len)
	binary.LittleEndian.PutUint16(hdr[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(hdr[6:8], m.Hdr.Flags)
	// seq and pid stay zero: that is the canonicalisation.
	full = append(full, hdr[:]...)
	full = append(full, m.Body...)
	return full
}

// zeroSeqPid returns a copy of a built request with nlmsg_seq and nlmsg_pid
// zeroed, so it can be compared against canonicalRequests' output.
func zeroSeqPid(b []byte) []byte {
	out := xtcpnl.CopyBytes(b)
	if len(out) >= 16 {
		for i := 8; i < 16; i++ {
			out[i] = 0
		}
	}
	return out
}

// TestTierALinkShowDump is the single highest-value assertion in the project
// so far: the bytes `ip link show` put on the wire, reproduced by goip.
//
// The capture holds exactly one request, which is itself worth asserting —
// nlmon records the whole host, and a polluted capture would make every
// expectation below describe something other than one command.
//
// go test ./internal/goip/req/ -run TestTierALinkShowDump
func TestTierALinkShowDump(t *testing.T) {
	reqs := canonicalRequests(t, tdBulkGetLink)
	if len(reqs) != 1 {
		t.Fatalf("requests in %s = %d, want exactly 1; the capture is polluted "+
			"and the expectation no longer describes a single command",
			tdBulkGetLink, len(reqs))
	}
	captured := reqs[0]

	got, err := LinkShowDump(12345)
	if err != nil {
		t.Fatalf("LinkShowDump: %v", err)
	}
	got = zeroSeqPid(got)

	// Length is checked before the field table, and the message names the
	// likely cause, because the most valuable way for this test to fail is the
	// one the plan calls for: drop IFLA_EXT_MASK from the builder and confirm
	// Tier A goes red *naming it*. Indexing got[34:36] straight away turns
	// that into a slice-bounds panic with a stack trace and no diagnosis —
	// which is a gate that fails, but not a gate that tells you anything. The
	// first version of this test did exactly that.
	if len(got) != len(captured) {
		t.Fatalf("built %d bytes, captured %d.\n"+
			"  built    %x\n  captured %x\n"+
			"A 32-byte build means IFLA_EXT_MASK was not emitted: without it the "+
			"kernel appends IFLA_STATS and IFLA_STATS64 to every reply, so the "+
			"reply set diverges by the most volatile attribute in the protocol.",
			len(got), len(captured), got, captured)
	}

	tests := []struct {
		description string
		got         any
		want        any
	}{
		{
			// The assertion the whole tier exists for. Everything below is a
			// decomposition of this one, kept so a failure names which byte
			// group moved instead of printing two hex blobs.
			description: "positive: the built request is byte-identical to the captured `ip link show` request",
			got:         fmt.Sprintf("%x", got),
			want:        fmt.Sprintf("%x", captured),
		},
		{
			// 16-byte nlmsghdr + 16-byte ifinfomsg + one 8-byte attribute.
			// This is also the one command with no oversend: iproute2 sends
			// nlmsg_len rather than sizeof(req), so the datagram is 40 too.
			description: "positive: length is 40",
			got:         len(got),
			want:        40,
		},
		{
			description: "positive: nlmsg_type is RTM_GETLINK",
			got:         binary.LittleEndian.Uint16(got[4:6]),
			want:        uint16(unix.RTM_GETLINK),
		},
		{
			// 0x0301. NLM_F_DUMP is ROOT|MATCH, and note that those two bits
			// are numerically identical to REPLACE|EXCL — which is why the
			// read-only invariant has to be a message-TYPE rule and cannot be
			// a flags rule.
			description: "positive: nlmsg_flags is REQUEST|DUMP",
			got:         binary.LittleEndian.Uint16(got[6:8]),
			want:        uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP),
		},
		{
			// AF_PACKET, and `ipaddr_list_link` forces it even under -4/-6.
			description: "positive: ifi_family is AF_PACKET (17)",
			got:         got[16],
			want:        byte(unix.AF_PACKET),
		},
		{
			description: "positive: the one attribute is IFLA_EXT_MASK",
			got:         binary.LittleEndian.Uint16(got[34:36]),
			want:        uint16(unix.IFLA_EXT_MASK),
		},
		{
			// RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS. SKIP_STATS is why
			// IFLA_STATS and IFLA_STATS64 are absent from every reply in the
			// committed dump, so this value changes the reply set and not just
			// the request.
			description: "positive: the ext-mask value is 0x09",
			got:         binary.LittleEndian.Uint32(got[36:40]),
			want:        uint32(0x09),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if fmt.Sprint(tc.got) != fmt.Sprint(tc.want) {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

// TestTierALinkShowByIndexAllCaptured is the derived table: it does not
// hand-write its rows.
//
// `ip route show table all` issues ten RTM_GETLINK single-gets as ll_link_get
// fills the index cache for route rendering, and all ten are in the capture.
// (The plan's prose says eleven; the capture says ten, and
// pkg/xtcpnl's TestBuildGetLinkByIndexRequestAllCaptured already asserts ten.)
// Rather than transcribing ten indices, this walks the capture,
// keeps every non-dump RTM_GETLINK request, reads ifi_index and the ext-mask
// *out of the captured bytes*, rebuilds each one, and compares.
//
// A hand-written table can be wrong in the same direction as the code. A table
// derived from the fixture cannot: moving ifi_index from offset 4 to offset 8
// fails every row at once, and the count assertion catches a capture that has
// silently lost requests — it is also what caught the plan's "eleven".
//
// go test ./internal/goip/req/ -run TestTierALinkShowByIndexAllCaptured
func TestTierALinkShowByIndexAllCaptured(t *testing.T) {
	const wantCount = 10

	var singles [][]byte
	for _, r := range canonicalRequests(t, tdBulkGetRoute) {
		if len(r) != 40 {
			continue
		}
		if binary.LittleEndian.Uint16(r[4:6]) != uint16(unix.RTM_GETLINK) {
			continue
		}
		if binary.LittleEndian.Uint16(r[6:8]) != uint16(unix.NLM_F_REQUEST) {
			continue
		}
		singles = append(singles, r)
	}
	if len(singles) != wantCount {
		t.Fatalf("40-byte non-dump RTM_GETLINK requests = %d, want %d; either the "+
			"capture changed or the filter above no longer selects ll_link_get's gets",
			len(singles), wantCount)
	}

	for _, captured := range singles {
		index := int32(binary.LittleEndian.Uint32(captured[20:24]))
		mask := binary.LittleEndian.Uint32(captured[36:40])

		description := fmt.Sprintf("positive: ll_link_get single-get for ifi_index %d, mask %#x", index, mask)
		t.Run(description, func(t *testing.T) {
			if mask != ExtMaskShow {
				t.Fatalf("captured ext-mask %#x != ExtMaskShow %#x; a released `ip` "+
					"sends 0x09 here, and 0x109 would mean the capture came from a "+
					"post-de91e928 build, which needs a version-skew allowlist entry",
					mask, ExtMaskShow)
			}
			got, err := LinkShowByIndex(index, 999)
			if err != nil {
				t.Fatalf("LinkShowByIndex(%d): %v", index, err)
			}
			if !bytes.Equal(zeroSeqPid(got), captured) {
				t.Errorf("built  %x\ncaptured %x", zeroSeqPid(got), captured)
			}
		})
	}
}

// TestLinkShowByName covers the by-name single-get, whose positive fixture
// does not exist yet.
//
// `ip link show dev lo` is not in the corpus — the plan's Item 7 adds it — so
// there is deliberately no positive-from-capture row here. What can be
// asserted without inventing an expectation is the structure the captured
// by-index form already proves (same type, same flags, same ext-mask first)
// plus every way the name can be wrong.
//
// go test ./internal/goip/req/ -run TestLinkShowByName
func TestLinkShowByName(t *testing.T) {
	tests := []struct {
		description string
		name        string
		wantLen     int
		wantErr     error
	}{
		{
			// 16 + 16 + 8 (ext-mask) + 4+3 padded to 8 (IFLA_IFNAME "lo\0").
			description: "positive: a short name yields ext-mask then IFLA_IFNAME",
			name:        "lo",
			wantLen:     48,
		},
		{
			// 15 characters plus the NUL is exactly IFNAMSIZ, so the attribute
			// payload is 16 and nothing is padded. This is the longest name the
			// kernel can return in IFLA_IFNAME, and the committed dump has one:
			// "ve-nordlayepDd-", the truncated form of a 16-character name.
			description: "boundary: a 15-character name is the longest accepted",
			name:        "ve-nordlayepDd-",
			wantLen:     60,
		},
		{
			// One over. IFNAMSIZ is 16 *including* the NUL, so 16 characters do
			// not fit and iproute2 would be unable to name the interface either.
			description: "negative: a 16-character name is rejected",
			name:        "ve-nordlayer-vpn",
			wantErr:     xtcpnl.ErrBadIfName,
		},
		{
			description: "negative: an empty name is rejected",
			name:        "",
			wantErr:     xtcpnl.ErrBadIfName,
		},
		{
			// A name with an embedded NUL would truncate the attribute and
			// address a different interface than the caller asked for — the
			// kind of confusion that has to be an error, not a silent trim.
			description: "corner: a name containing a NUL is rejected",
			name:        "lo\x00extra",
			wantErr:     xtcpnl.ErrBadIfName,
		},
		{
			// A slash cannot appear in an interface name, and accepting one
			// would let a caller smuggle a path into a request.
			description: "corner: a name containing a slash is rejected",
			name:        "eth/0",
			wantErr:     xtcpnl.ErrBadIfName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := LinkShowByName(tc.name, 7)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if mt := binary.LittleEndian.Uint16(got[4:6]); mt != uint16(unix.RTM_GETLINK) {
				t.Errorf("nlmsg_type = %d, want RTM_GETLINK", mt)
			}
			if f := binary.LittleEndian.Uint16(got[6:8]); f != uint16(unix.NLM_F_REQUEST) {
				t.Errorf("nlmsg_flags = %#x, want NLM_F_REQUEST alone", f)
			}
			// Attribute order is load-bearing: ll_link_get emits the ext-mask
			// first and the name second (lib/ll_map.c:289-293), and the parity
			// comparator holds requests to full byte equality.
			if at := binary.LittleEndian.Uint16(got[34:36]); at != uint16(unix.IFLA_EXT_MASK) {
				t.Errorf("first attribute = %d, want IFLA_EXT_MASK", at)
			}
			if at := binary.LittleEndian.Uint16(got[42:44]); at != uint16(unix.IFLA_IFNAME) {
				t.Errorf("second attribute = %d, want IFLA_IFNAME", at)
			}
		})
	}
}

// TestExtMaskShow pins the constant itself.
//
// go test ./internal/goip/req/ -run TestExtMaskShow
func TestExtMaskShow(t *testing.T) {
	tests := []struct {
		description string
		got         uint32
		want        uint32
	}{
		{
			description: "positive: ExtMaskShow is RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS",
			got:         ExtMaskShow,
			want:        0x09,
		},
		{
			// The bit that is NOT set, and the reason this row exists.
			// RTEXT_FILTER_NAME_ONLY = (1 << 8) was added by iproute2 commit
			// de91e928 (2026-05-20, in no tag) and makes the mask 0x109 on the
			// two ll_map paths. Released `ip` sends 0x09. If this row ever has
			// to change, the parity allowlist needs a version-skew entry with
			// an ip_version, not a quiet edit.
			description: "boundary: RTEXT_FILTER_NAME_ONLY (1<<8) is not set",
			got:         ExtMaskShow & (1 << 8),
			want:        0,
		},
		{
			// RTEXT_FILTER_BRVLAN and friends are not set either; the mask is
			// exactly two bits wide, and an accidental third would change the
			// reply set.
			description: "boundary: exactly two bits are set",
			got:         uint32(popcount(ExtMaskShow)),
			want:        2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %#x, want %#x", tc.got, tc.want)
			}
		})
	}
}

func popcount(v uint32) int {
	n := 0
	for ; v != 0; v >>= 1 {
		n += int(v & 1)
	}
	return n
}

// tdBulkGetAddr is the `ip -4 addr show; ip -6 addr show` capture, and it is
// the only polluted fixture in the corpus.
const tdBulkGetAddr = "../../../pkg/xtcpnl/testdata/7_1_8/netlink_route_getaddr.pcap"

// iproute2AddrRequests picks the four requests the two `ip` runs sent out of
// the eighteen the capture holds.
//
// # The discriminator, clause by clause, and why nothing simpler works
//
// The capture is the plan's fact 5 in its worst form: 251 messages, six
// portids. Among the requests alone there are, besides our four:
//
//		type=20 RTM_NEWADDR len=92 flags=0x0505  a DHCPv6 client adding an address
//		type=22 RTM_GETADDR len=20 flags=0x0305  x2, built on rtgenmsg not ifaddrmsg
//		type=30 RTM_GETNEIGH len=28              an unrelated neighbor dump
//		type=18 RTM_GETLINK len=32 flags=0x0021  x9, single-gets by index
//		type=18 RTM_GETLINK len=32 flags=0x0301  an AF_UNSPEC dump with no ext mask
//
//	  - **nlmsg_pid == 0** is iproute2's signature. libnetlink never sets it on
//	    a request, and the kernel fills the socket's portid in on the *reply*.
//	    Ten of the eighteen requests here carry a non-zero nlmsg_pid, which is
//	    how they identify themselves as somebody else's library.
//	  - **flags == NLM_F_REQUEST|NLM_F_DUMP exactly.** This is what separates
//	    our two RTM_GETADDR dumps from the two rtgenmsg ones, which set
//	    NLM_F_ACK as well (0x0305). Testing for "dump" with a mask instead of
//	    equality would let them through, and they are 20 bytes where iproute2
//	    sends 24 — a length mismatch on a row that should never have matched.
//
// What deliberately is *not* used: nlmsg_seq. The two `ip` runs started within
// the same second and `rtnl_open` seeds seq from time(NULL), so they share
// both of their sequence numbers exactly — 1789012353 for the link dump and
// 1789012354 for the address dump, twice over. That is the plan's fact 6 with
// no ambiguity left in it, and it is why the parity comparator has to bind a
// transaction by reply portid rather than by seq.
func iproute2AddrRequests(t *testing.T) [][]byte {
	t.Helper()
	const wantDump = uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)

	var out [][]byte
	for _, m := range mustCapture(t, tdBulkGetAddr).Msgs() {
		if !m.IsRequest() || m.Hdr.Pid != 0 || m.Hdr.Flags != wantDump {
			continue
		}
		out = append(out, canonicalize(m))
	}
	return out
}

// TestTierAAddrShowRequests is Tier A for `addr show`: §8.2's second, third
// and fourth positive rows, byte-for-byte against the capture.
//
// The four requests are asserted **in capture order**, which makes this the
// L1 ordering assertion as well as the L2 byte assertion: `ip` sends the link
// dump before the address dump, twice, and a goip that reversed them would
// pass every individual byte comparison and fail here.
//
// go test ./internal/goip/req/ -run TestTierAAddrShowRequests
func TestTierAAddrShowRequests(t *testing.T) {
	reqs := iproute2AddrRequests(t)
	if len(reqs) != 4 {
		t.Fatalf("found %d iproute2 dump requests in %s, want 4 (a link dump and an "+
			"address dump for each of the -4 and -6 runs); the capture or the "+
			"discriminator has changed", len(reqs), tdBulkGetAddr)
	}

	tests := []struct {
		description string
		captured    []byte
		build       func() ([]byte, error)
		wantLen     int
		wantType    uint16
		wantFamily  uint8
		wantAttrs   int
	}{
		{
			// **32 bytes, not 40.** The link dump `addr show` sends is the
			// bare __rtnl_linkdump_req form, because
			// rtnl_linkdump_req_filter_fn only calls its filter_fn — the one
			// thing that appends IFLA_EXT_MASK — for AF_UNSPEC and AF_PACKET
			// (lib/libnetlink.c:591-618). So `ip -4 addr show` asks the kernel
			// for links with no ext mask at all, which is why its replies
			// carry IFLA_STATS64 and `ip link show`'s do not.
			description: "positive: `ip -4 addr show` link dump is 32 bytes, ifi_family AF_INET, no attributes",
			captured:    reqs[0],
			build:       func() ([]byte, error) { return AddrShowLinkDump(unix.AF_INET, 1) },
			wantLen:     32,
			wantType:    unix.RTM_GETLINK,
			wantFamily:  unix.AF_INET,
			wantAttrs:   0,
		},
		{
			description: "positive: `ip -4 addr show` address dump is 24 bytes, ifa_family AF_INET",
			captured:    reqs[1],
			build:       func() ([]byte, error) { return AddrShowDump(unix.AF_INET, 2), nil },
			wantLen:     24,
			wantType:    unix.RTM_GETADDR,
			wantFamily:  unix.AF_INET,
			wantAttrs:   0,
		},
		{
			// Identical to the -4 link dump but for the family byte, and that
			// byte is the one that makes the kernel answer from
			// inet6_dump_ifinfo instead of rtnl_dump_ifinfo. A one-byte
			// request difference with a forty-attribute reply difference
			// behind it.
			description: "positive: `ip -6 addr show` link dump differs from the -4 one in exactly one byte",
			captured:    reqs[2],
			build:       func() ([]byte, error) { return AddrShowLinkDump(unix.AF_INET6, 3) },
			wantLen:     32,
			wantType:    unix.RTM_GETLINK,
			wantFamily:  unix.AF_INET6,
			wantAttrs:   0,
		},
		{
			description: "positive: `ip -6 addr show` address dump is 24 bytes, ifa_family AF_INET6",
			captured:    reqs[3],
			build:       func() ([]byte, error) { return AddrShowDump(unix.AF_INET6, 4), nil },
			wantLen:     24,
			wantType:    unix.RTM_GETADDR,
			wantFamily:  unix.AF_INET6,
			wantAttrs:   0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if len(tc.captured) != tc.wantLen {
				t.Fatalf("captured request is %d bytes, want %d", len(tc.captured), tc.wantLen)
			}
			if got := binary.LittleEndian.Uint16(tc.captured[4:6]); got != tc.wantType {
				t.Fatalf("captured nlmsg_type = %d, want %d", got, tc.wantType)
			}
			if got := tc.captured[16]; got != tc.wantFamily {
				t.Fatalf("captured family byte = %d, want %d", got, tc.wantFamily)
			}
			// wantAttrs is asserted, not merely recorded. Every row here is a
			// zero-attribute request and the descriptions say so in words, so
			// a field nobody read would let a row keep claiming "no
			// attributes" while the capture had grown an IFLA_EXT_MASK. What
			// stood here before was a second copy of the length check above,
			// which could not fail without that one having already fataled.
			//
			// The attribute region starts after the netlink header and the
			// message type's fixed family struct: 16 bytes of ifinfomsg for
			// RTM_GETLINK, 8 of ifaddrmsg for RTM_GETADDR.
			var famLen int
			switch tc.wantType {
			case unix.RTM_GETLINK:
				famLen = xtcpnl.IfInfomsgSizeCst
			case unix.RTM_GETADDR:
				famLen = xtcpnl.IfAddrmsgSizeCst
			default:
				t.Fatalf("no family header size known for nlmsg_type %d", tc.wantType)
			}
			gotAttrs := 0
			if err := xtcpnl.WalkRTAttrs(
				tc.captured[xtcpnl.NlMsgHdrSizeCst+famLen:],
				func(uint16, []byte) { gotAttrs++ },
			); err != nil {
				t.Fatalf("walking the captured attribute region: %v", err)
			}
			if gotAttrs != tc.wantAttrs {
				t.Errorf("captured request carries %d attributes, want %d",
					gotAttrs, tc.wantAttrs)
			}

			got, err := tc.build()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if !bytes.Equal(zeroSeqPid(got), tc.captured) {
				t.Errorf("built    %x\ncaptured %x", zeroSeqPid(got), tc.captured)
			}
		})
	}

	t.Run("boundary: the two link dumps differ in exactly one byte, the family", func(t *testing.T) {
		a, b := reqs[0], reqs[2]
		if len(a) != len(b) {
			t.Fatalf("link dumps are %d and %d bytes", len(a), len(b))
		}
		diff := 0
		for i := range a {
			if a[i] != b[i] {
				diff++
				if i != 16 {
					t.Errorf("link dumps differ at offset %d, which is not ifi_family", i)
				}
			}
		}
		if diff != 1 {
			t.Errorf("link dumps differ in %d bytes, want exactly 1", diff)
		}
	})

	t.Run("corner: both runs reused the same two nlmsg_seq values, so seq cannot attribute", func(t *testing.T) {
		// Read from the capture rather than from canonicalRequests, which
		// zeroes seq by design. This is the one row that needs the raw value.
		var seqs []uint32
		const wantDump = uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)
		for _, m := range mustCapture(t, tdBulkGetAddr).Msgs() {
			if !m.IsRequest() || m.Hdr.Pid != 0 || m.Hdr.Flags != wantDump {
				continue
			}
			seqs = append(seqs, m.Hdr.Seq)
		}
		if len(seqs) != 4 {
			t.Fatalf("got %d seqs, want 4", len(seqs))
		}
		if seqs[0] != seqs[2] || seqs[1] != seqs[3] {
			t.Errorf("seqs = %v; the two runs were expected to reuse the same pair, "+
				"which is what makes seq-based attribution impossible here", seqs)
		}
		if seqs[0] == seqs[1] {
			t.Errorf("both requests of one run share seq %d; `ip` increments per request", seqs[0])
		}
	})
}

// TestAddrShowLinkDumpFamilies covers the family branch in
// AddrShowLinkDump, including the two families the capture does not hold.
//
// go test ./internal/goip/req/ -run TestAddrShowLinkDumpFamilies
func TestAddrShowLinkDumpFamilies(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		wantLen     int
		wantAttrs   bool
	}{
		{
			// The plain `ip addr show` form, which has no capture: AF_UNSPEC
			// reaches the filter_fn path, so it is the 40-byte
			// ext-mask-carrying request. Listed as boundary rather than
			// positive because the expectation comes from the source and from
			// the AF_PACKET row's captured twin, not from a recorded
			// AF_UNSPEC addr show — that fixture is on the plan's Item 7 list.
			description: "boundary: AF_UNSPEC takes the filter_fn path and carries the ext mask",
			family:      unix.AF_UNSPEC,
			wantLen:     40,
			wantAttrs:   true,
		},
		{
			// AF_PACKET is unreachable from goip's argv — there is no `-0`
			// option — but it is the other family the filter_fn path accepts,
			// and `ip link show` proves the 40-byte shape is right for it.
			description: "boundary: AF_PACKET also takes the filter_fn path",
			family:      unix.AF_PACKET,
			wantLen:     40,
			wantAttrs:   true,
		},
		{
			description: "positive: AF_INET falls through to the bare 32-byte form",
			family:      unix.AF_INET,
			wantLen:     32,
			wantAttrs:   false,
		},
		{
			description: "positive: AF_INET6 falls through to the bare 32-byte form",
			family:      unix.AF_INET6,
			wantLen:     32,
			wantAttrs:   false,
		},
		{
			// An arbitrary family is not AF_UNSPEC or AF_PACKET, so it takes
			// the bare path. The kernel would answer AF_MPLS RTM_GETLINK from
			// PF_UNSPEC's dumpit, since net/mpls registers no RTM_GETLINK
			// handler — but that is the kernel's business, and the request
			// shape is decided entirely by the two-family test in
			// rtnl_linkdump_req_filter_fn.
			description: "corner: an unrelated family such as AF_MPLS still takes the bare path",
			family:      unix.AF_MPLS,
			wantLen:     32,
			wantAttrs:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := AddrShowLinkDump(tc.family, 7)
			if err != nil {
				t.Fatalf("AddrShowLinkDump(%d): %v", tc.family, err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("got %d bytes, want %d: %x", len(got), tc.wantLen, got)
			}
			if got[16] != tc.family {
				t.Errorf("ifi_family = %d, want %d", got[16], tc.family)
			}
			if binary.LittleEndian.Uint32(got[0:4]) != uint32(tc.wantLen) {
				t.Errorf("nlmsg_len = %d, want %d", binary.LittleEndian.Uint32(got[0:4]), tc.wantLen)
			}
			if tc.wantAttrs {
				if atype := binary.LittleEndian.Uint16(got[34:36]); atype != uint16(unix.IFLA_EXT_MASK) {
					t.Errorf("attribute type = %d, want IFLA_EXT_MASK (%d)", atype, unix.IFLA_EXT_MASK)
				}
				if v := binary.LittleEndian.Uint32(got[36:40]); v != ExtMaskShow {
					t.Errorf("ext mask = %#x, want %#x", v, ExtMaskShow)
				}
			}
		})
	}
}

// TestAddrShowDumpShape covers the address dump, whose whole content is a
// family byte.
//
// go test ./internal/goip/req/ -run TestAddrShowDumpShape
func TestAddrShowDumpShape(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
	}{
		{description: "positive: AF_INET, as `ip -4 addr show` sends", family: unix.AF_INET},
		{description: "positive: AF_INET6, as `ip -6 addr show` sends", family: unix.AF_INET6},
		{
			// The plain form. Unlike the link dump, the address dump's shape
			// does not change with the family at all — rtnl_addrdump_req has
			// no two-family test — so AF_UNSPEC is the same 24 bytes.
			description: "boundary: AF_UNSPEC is the same 24 bytes, because rtnl_addrdump_req has no family branch",
			family:      unix.AF_UNSPEC,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := AddrShowDump(tc.family, 5)
			if len(got) != 24 {
				t.Fatalf("got %d bytes, want 24: %x", len(got), got)
			}
			if binary.LittleEndian.Uint16(got[4:6]) != uint16(unix.RTM_GETADDR) {
				t.Errorf("nlmsg_type = %d, want RTM_GETADDR", binary.LittleEndian.Uint16(got[4:6]))
			}
			if want := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP); binary.LittleEndian.Uint16(got[6:8]) != want {
				t.Errorf("nlmsg_flags = %#x, want %#x", binary.LittleEndian.Uint16(got[6:8]), want)
			}
			if got[16] != tc.family {
				t.Errorf("ifa_family = %d, want %d", got[16], tc.family)
			}
			// The remaining seven ifaddrmsg bytes — prefixlen, flags, scope
			// and index — are all zero for an unfiltered show.
			// ipaddr_dump_filter writes filter.ifindex into ifa_index, which
			// is 0 here, so a non-zero byte would mean goip had invented a
			// filter `ip` did not send.
			for i := 17; i < 24; i++ {
				if got[i] != 0 {
					t.Errorf("ifaddrmsg byte %d = %#x, want 0", i-16, got[i])
				}
			}
		})
	}
}

// TestTierARouteShowRequests is the route half of Tier A: the three
// `ip route show` forms, byte-for-byte against the requests the pinned `ip`
// was recorded emitting.
//
// Each of the three captures below holds exactly one RTM_GETROUTE request, and
// the row asserts that too. These are the gated-topology captures, taken one
// command at a time in a microVM with nothing else on the host, which is what
// makes a count of one an assertion rather than a hope — the bulk 7_1_8
// capture this file's link rows use was taken on a live desktop.
//
// go test ./internal/goip/req/ -run TestTierARouteShowRequests
func TestTierARouteShowRequests(t *testing.T) {
	tests := []struct {
		description string
		capture     string
		family      uint8
		table       uint32
	}{
		{
			// `ip route show`. AF_INET rather than AF_UNSPEC because
			// iproute_list_flush_or_save promotes the family whenever a table
			// filter is set (ip/iproute.c:2000), and filter.tb defaults to
			// RT_TABLE_MAIN.
			description: "positive: `route show` is AF_INET with RTA_TABLE=254",
			capture:     tdGatedGetRoute,
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
		},
		{
			description: "positive: `-6 route show` is AF_INET6 with RTA_TABLE=254",
			capture:     tdGatedGetRoute6,
			family:      unix.AF_INET6,
			table:       unix.RT_TABLE_MAIN,
		},
		{
			// The one form with no attribute at all: `table all` clears
			// filter.tb, which both drops RTA_TABLE and leaves the family
			// unpromoted.
			description: "positive: `route show table all` is AF_UNSPEC with no RTA_TABLE",
			capture:     tdGatedGetRouteAll,
			family:      unix.AF_UNSPEC,
			table:       unix.RT_TABLE_UNSPEC,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var captured [][]byte
			for _, r := range canonicalRequests(t, tc.capture) {
				if binary.LittleEndian.Uint16(r[4:6]) == uint16(unix.RTM_GETROUTE) {
					captured = append(captured, r)
				}
			}
			if len(captured) != 1 {
				t.Fatalf("RTM_GETROUTE requests in %s = %d, want exactly 1; the "+
					"capture is polluted and the expectation no longer describes "+
					"a single command", tc.capture, len(captured))
			}
			got, err := RouteShowDump(tc.family, tc.table, 42)
			if err != nil {
				t.Fatalf("RouteShowDump(%d, %d): %v", tc.family, tc.table, err)
			}
			if !bytes.Equal(zeroSeqPid(got), captured[0]) {
				t.Errorf("built    %x\ncaptured %x", zeroSeqPid(got), captured[0])
			}
		})
	}
}

// TestRouteShowDumpShape covers the table ids that have no capture: the
// spellings of "no filter", the edges of the byte the header field can hold,
// and the values only RTA_TABLE can express.
//
// The structural invariant every row checks is that rtm_table in the HEADER
// stays 0 no matter what the attribute says. iproute2 never writes it for a
// dump (iproute_dump_filter only adds the attribute, ip/iproute.c:1726), and
// filling it in would be a plausible-looking change that makes every request
// differ from the capture by one byte.
//
// go test ./internal/goip/req/ -run TestRouteShowDumpShape
func TestRouteShowDumpShape(t *testing.T) {
	const (
		hdrAndRtmsg = 28 // 16-byte nlmsghdr + 12-byte rtmsg
		withTable   = 36 // ... plus one 8-byte u32 attribute
	)

	tests := []struct {
		description string
		family      uint8
		table       uint32
		wantLen     int
		wantTable   uint32 // RTA_TABLE's value, checked only when wantLen is withTable
	}{
		{
			description: "positive: RT_TABLE_MAIN carries RTA_TABLE=254",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
			wantLen:     withTable,
			wantTable:   254,
		},
		{
			// `table all` and `table 0` are different spellings that both land
			// on filter.tb = 0, so they must build the same bytes. The "0"
			// spelling parses as a table id; "all" does not and is caught by
			// iproute2's fallback (ip/iproute.c:1849).
			description: "boundary: RT_TABLE_UNSPEC omits the attribute entirely",
			family:      unix.AF_UNSPEC,
			table:       unix.RT_TABLE_UNSPEC,
			wantLen:     hdrAndRtmsg,
		},
		{
			// The largest id rtm_table could have held, which is exactly why
			// it is worth a row: the attribute must carry it and the header
			// byte must still be 0.
			description: "boundary: table 255 is RT_TABLE_LOCAL and fits a byte, but still goes in the attribute",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_LOCAL,
			wantLen:     withTable,
			wantTable:   255,
		},
		{
			// One past the byte. A host with `ip route add ... table 256` has
			// a table no rtm_table can name, which is the whole reason
			// RTA_TABLE exists.
			description: "corner: table 256 is unrepresentable in rtm_table and must survive in the attribute",
			family:      unix.AF_INET,
			table:       256,
			wantLen:     withTable,
			wantTable:   256,
		},
		{
			// RT_TABLE_MAX. rtnl_rttable_a2n accepts it, so goip must build it
			// rather than silently truncating to 0xFF.
			description: "corner: table 4294967295 is RT_TABLE_MAX and round-trips whole",
			family:      unix.AF_INET,
			table:       0xFFFFFFFF,
			wantLen:     withTable,
			wantTable:   0xFFFFFFFF,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := RouteShowDump(tc.family, tc.table, 9)
			if err != nil {
				t.Fatalf("RouteShowDump: %v", err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("len = %d, want %d: %x", len(got), tc.wantLen, got)
			}
			if mt := binary.LittleEndian.Uint16(got[4:6]); mt != uint16(unix.RTM_GETROUTE) {
				t.Errorf("nlmsg_type = %d, want RTM_GETROUTE", mt)
			}
			if want := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP); binary.LittleEndian.Uint16(got[6:8]) != want {
				t.Errorf("nlmsg_flags = %#x, want %#x", binary.LittleEndian.Uint16(got[6:8]), want)
			}
			if got[16] != tc.family {
				t.Errorf("rtm_family = %d, want %d", got[16], tc.family)
			}
			// rtm_table is byte 4 of the rtmsg, so offset 20.
			if got[20] != 0 {
				t.Errorf("rtm_table = %d, want 0; the header field is never written for a dump", got[20])
			}
			// Every other rtmsg byte is zero for an unfiltered show: a
			// non-zero dst_len or scope would be a filter `ip` did not send.
			for i := 17; i < 28; i++ {
				if got[i] != 0 {
					t.Errorf("rtmsg byte %d = %#x, want 0", i-16, got[i])
				}
			}
			if tc.wantLen != withTable {
				return
			}
			if at := binary.LittleEndian.Uint16(got[30:32]); at != uint16(unix.RTA_TABLE) {
				t.Errorf("attribute type = %d, want RTA_TABLE", at)
			}
			if v := binary.LittleEndian.Uint32(got[32:36]); v != tc.wantTable {
				t.Errorf("RTA_TABLE = %d, want %d", v, tc.wantTable)
			}
		})
	}
}
