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

	// `ip link show dev goip0`, taken in the same quiet microVM. Two requests
	// in the whole file, which is the assertion as much as their contents are.
	tdGatedGetLinkDev = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getlink_dev.pcap"

	// The `ip addr show dev goip0` capture. Taken on the same topology by the
	// same script as the rest of this corpus, but on a LATER run of it — see
	// TestTierAAddrShowDevRequests for why that is sound here and what it
	// would not be sound for.
	tdGatedGetAddrDev = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getaddr_dev.pcap"

	// The gated-topology route captures, one command each, taken in a microVM
	// with no other netlink traffic on the host. That is what makes "exactly
	// one RTM_GETROUTE request" an assertion the route rows can make and the
	// bulk desktop captures above cannot.
	tdGatedGetRoute    = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute.pcap"
	tdGatedGetRoute6   = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute6.pcap"
	tdGatedGetRouteAll = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_table_all.pcap"

	// `ip route show dev NAME`, on both topologies. The MESH one is here for
	// one reason: on that namespace the named device owns no routes, so the
	// dump answers with nothing but NLMSG_DONE. Two captures of the same
	// command whose answers differ by every route are what let the transaction
	// count be asserted as a constant rather than as a coincidence of the
	// clean topology's size.
	tdGatedGetRouteDev     = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getroute_dev.pcap"
	tdGatedGetRouteDevMesh = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getroute_dev.pcap"

	// `ip neigh show dev NAME`, on both topologies. Its whole difference from
	// tdGetNeigh is 8 bytes on the second request, which is precisely why it
	// needs its own file: nothing in the replies or the output records it.
	tdGatedGetNeighDev     = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh_dev.pcap"
	tdGatedGetNeighDevMesh = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getneigh_dev.pcap"

	// `ip rule show` and `ip -6 rule show`. Two datagrams each — one request
	// and one multipart reply — the smallest captures in the corpus, and for a
	// structural reason rather than an incidental one:
	// iprule_list_flush_or_save calls no ll_init_map, because FRA_IIFNAME and
	// FRA_OIFNAME arrive as strings and there is no index to resolve.
	//
	// There is deliberately no `-4` file. `ip rule show` and `ip -4 rule show`
	// build the same bytes (ip/iprule.c:748-752), so a second capture would
	// assert nothing; the identity is claimed by a parity row instead.
	tdGatedGetRule  = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getrule.pcap"
	tdGatedGetRule6 = "../../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getrule6.pcap"
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
	// ifindex 0: the bare command sets no filter, so the request must come
	// back byte-identical to the pre-filter builder's output.
	neigh, err := NeighShowDump(unix.AF_UNSPEC, 0, 0, 124)
	if err != nil {
		t.Fatal(err)
	}
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

	got, err := LinkShowDump(ExtMaskShow, 12345)
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

// TestLinkShowDumpStatsMask asserts what `-s` costs on the wire, which is one
// byte.
//
// Deliberately written as a DIFFERENCE rather than as a second golden request.
// A golden would pass while both forms drifted together; comparing the two
// builds against each other can only pass if exactly the intended byte moved.
// There is also no captured `ip -s link show` request to compare against yet,
// and the difference is checkable without one — TestTierALinkShowDump above
// already pins the plain form byte-for-byte against the capture, so anchoring
// to it inherits that.
//
// go test ./internal/goip/req/ -run TestLinkShowDumpStatsMask
func TestLinkShowDumpStatsMask(t *testing.T) {
	plain, err := LinkShowDump(ExtMaskShow, 12345)
	if err != nil {
		t.Fatalf("LinkShowDump(ExtMaskShow): %v", err)
	}
	stats, err := LinkShowDump(ExtMaskStats, 12345)
	if err != nil {
		t.Fatalf("LinkShowDump(ExtMaskStats): %v", err)
	}

	// The differing offsets, computed rather than transcribed, so the table
	// below can state the expectation as a set.
	var diff []int
	if len(plain) == len(stats) {
		for i := range plain {
			if plain[i] != stats[i] {
				diff = append(diff, i)
			}
		}
	}

	tests := []struct {
		description string
		got         any
		want        any
	}{
		{
			description: "positive: `-s` changes the length of the request not at all — the same 40-byte datagram",
			got:         []int{len(plain), len(stats)},
			want:        []int{40, 40},
		},
		{
			// The whole claim, in one row. Offset 36 is the first byte of the
			// IFLA_EXT_MASK attribute's u32 value: 16 nlmsghdr + 16 ifinfomsg
			// + 4 rtattr header.
			description: "positive: exactly one byte differs, at offset 36, the low byte of the ext-mask value",
			got:         diff,
			want:        []int{36},
		},
		{
			description: "positive: the plain mask is RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS = 0x09",
			got:         binary.LittleEndian.Uint32(plain[36:40]),
			want:        uint32(0x09),
		},
		{
			// Clearing SKIP_STATS is what makes the kernel attach IFLA_STATS
			// and IFLA_STATS64 to every reply, so this byte changes the reply
			// set far more than it changes the request.
			description: "positive: the -s mask is RTEXT_FILTER_VF alone = 0x01",
			got:         binary.LittleEndian.Uint32(stats[36:40]),
			want:        uint32(0x01),
		},
		{
			// Named separately because it is the bit that has to STAY: `-s`
			// is not `novf`, and clearing RTEXT_FILTER_VF as well would be a
			// different command (ip/ipaddress.c:2239).
			description: "boundary: RTEXT_FILTER_VF survives -s; only RTEXT_FILTER_SKIP_STATS is cleared",
			got: []uint32{
				binary.LittleEndian.Uint32(stats[36:40]) & 0x01,
				binary.LittleEndian.Uint32(stats[36:40]) & 0x08,
			},
			want: []uint32{0x01, 0x00},
		},
		{
			description: "positive: the attribute is still IFLA_EXT_MASK under -s — the value moved and the type did not",
			got:         binary.LittleEndian.Uint16(stats[34:36]),
			want:        uint16(unix.IFLA_EXT_MASK),
		},
		{
			description: "positive: ifi_family is AF_PACKET under -s too",
			got:         stats[16],
			want:        byte(unix.AF_PACKET),
		},
		{
			description: "negative: -s does not turn the dump into a get — flags stay REQUEST|DUMP",
			got:         binary.LittleEndian.Uint16(stats[6:8]),
			want:        uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if fmt.Sprint(tc.got) != fmt.Sprint(tc.want) {
				t.Errorf("got %v, want %v\n plain %x\n stats %x",
					tc.got, tc.want, plain, stats)
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

// TestLinkShowByName covers the by-name single-get over names the capture
// does not contain.
//
// The positive-from-capture assertion moved to
// TestTierALinkShowDevRequests once `ip link show dev goip0` entered the
// corpus. What is left here is the part a capture of one interface cannot
// reach: the lengths at other name lengths, and every way a name can be
// wrong.
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

// TestTierALinkShowDevRequests is the Tier A positive for `link show dev`:
// both of the requests the pinned `ip` sent, reproduced by the two builders
// goip calls, in order and byte for byte.
//
// It is table-driven over a pair rather than two separate assertions because
// the ORDER is part of what is under test. Each builder producing correct
// bytes is not enough — they have to be sent in iproute2's order, since a
// capture compared position by position would reject the swap even though
// both requests appear in it.
//
// Exactly two requests in the file is asserted first, for the reason
// TestTierALinkShowDump gives: nlmon records the whole host, and a polluted
// capture would make every expectation below describe something else.
//
// go test ./internal/goip/req/ -run TestTierALinkShowDevRequests
func TestTierALinkShowDevRequests(t *testing.T) {
	const devCst = "goip0"

	reqs := canonicalRequests(t, tdGatedGetLinkDev)
	if len(reqs) != 2 {
		t.Fatalf("requests in the dev capture = %d, want ll_link_get then iplink_get", len(reqs))
	}

	tests := []struct {
		description string
		build       func() ([]byte, error)
		captured    []byte
	}{
		{
			// ll_name_to_index's cache miss (ip/ipaddress.c:2254 into
			// lib/ll_map.c:264). Its reply is discarded except for the index.
			description: "positive: the first request is ll_link_get, AF_UNSPEC with the ext-mask first",
			build:       func() ([]byte, error) { return LinkShowByName(devCst, 41) },
			captured:    reqs[0],
		},
		{
			// iplink_get (ip/ipaddress.c:2293 into ip/iplink.c:1497). This
			// reply is the one print_linkinfo renders.
			description: "positive: the second request is iplink_get, AF_PACKET with the name first",
			build:       func() ([]byte, error) { return LinkShowDev(devCst, ExtMaskShow, 42) },
			captured:    reqs[1],
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(zeroSeqPid(got), tc.captured) {
				t.Fatalf("request differs\n got %x\nwant %x", zeroSeqPid(got), tc.captured)
			}
		})
	}

	// And the negative the two rows above cannot state on their own: the
	// builders are not interchangeable, so neither captured request can be
	// served by the other builder. Without this, a LinkShowDev that simply
	// called LinkShowByName would fail the second row with a hex dump rather
	// than with a reason.
	swapped, err := LinkShowDev(devCst, ExtMaskShow, 41)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bytes.Equal(zeroSeqPid(swapped), reqs[0]) {
		t.Error("iplink_get reproduced ll_link_get's captured request; the two builders have collapsed into one")
	}
}

// TestTierAAddrShowDevRequests is the Tier A positive for `addr show dev`:
// all THREE requests the pinned `ip` sent, in order, byte for byte.
//
// # What the three are, and why the middle one needed a capture
//
// Requests one and three were predictable from the source. Request two was
// the one worth capturing: ipaddr_link_get sets `.i.ifi_family =
// filter.family` (ip/ipaddress.c:2058), and for a plain `addr show dev` that
// is AF_UNSPEC — which is the same byte ll_link_get's designated initializer
// leaves behind for a completely different reason. Two requests agreeing on a
// field by coincidence is exactly the situation where reading the source and
// writing down what you expect goes wrong, and the capture is what makes the
// agreement a measurement.
//
// Request three is the payoff: `18000000 16000103 00000000 00000000 00000000
// 03000000` — an RTM_GETADDR dump whose ifaddrmsg carries ifa_index 3. It is
// the only request in the whole corpus with a non-zero value in that field,
// and it is what makes BuildDumpAddrRequestIndex a builder with a caller
// rather than a builder with a doc comment.
//
// # This capture is from a later run than the rest of the corpus
//
// The capture script rewrites every fixture it takes, and the guest's dummy
// gets a fresh random MAC on each boot, so installing a whole re-capture
// would have churned the sidecars, the portids and the neighbor dump's hash
// order across the corpus for one new command. Only this pcap and its
// ip_addr_dev sidecar were taken from the new run, and they are a matched
// pair with each other, which is all any test here needs.
//
// What it would NOT be sound for is a test that cross-referenced this pcap
// against another fixture's MAC or portid. Nothing does, and the topology is
// otherwise identical — same namespace script, same indexes, same addresses.
//
// go test ./internal/goip/req/ -run TestTierAAddrShowDevRequests
func TestTierAAddrShowDevRequests(t *testing.T) {
	const (
		devCst   = "goip0"
		indexCst = 3
	)

	reqs := canonicalRequests(t, tdGatedGetAddrDev)
	if len(reqs) != 3 {
		t.Fatalf("requests in the addr dev capture = %d, want ll_link_get, ipaddr_link_get, ip_addr_list", len(reqs))
	}

	tests := []struct {
		description string
		build       func() ([]byte, error)
		captured    []byte
	}{
		{
			// The same function `link show dev` calls, so the same bytes.
			// Asserted here as well as there, because "the two commands
			// share their first request" is a claim about this capture and
			// not only about the builder.
			description: "positive: the first request is ll_link_get, by name, AF_UNSPEC, ext-mask first",
			build:       func() ([]byte, error) { return LinkShowByName(devCst, 51) },
			captured:    reqs[0],
		},
		{
			description: "positive: the second request is ipaddr_link_get, by index, carrying filter.family",
			build:       func() ([]byte, error) { return AddrShowLinkGet(unix.AF_UNSPEC, indexCst, ExtMaskShow, 52) },
			captured:    reqs[1],
		},
		{
			description: "positive: the third request is the address dump with the resolved index in ifa_index",
			build:       func() ([]byte, error) { return AddrShowDump(unix.AF_UNSPEC, indexCst, 53), nil },
			captured:    reqs[2],
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(zeroSeqPid(got), tc.captured) {
				t.Fatalf("request differs\n got %x\nwant %x", zeroSeqPid(got), tc.captured)
			}
		})
	}

	// The negatives the rows above cannot state, because each of them only
	// says "this builder reproduces this capture".
	t.Run("negative: the unfiltered address dump does NOT reproduce the captured third request", func(t *testing.T) {
		// Without this, a goip that ignored the selector entirely would fail
		// the third row with a hex dump and no reason. It is also the
		// assertion that the index reaches the wire at all: the two requests
		// differ in exactly four bytes.
		unfiltered := AddrShowDump(unix.AF_UNSPEC, 0, 53)
		if bytes.Equal(zeroSeqPid(unfiltered), reqs[2]) {
			t.Error("the unfiltered dump equals the captured `dev` dump; ifa_index is not reaching the request")
		}
	})

	t.Run("negative: iplink_get does not reproduce the captured second request", func(t *testing.T) {
		// `link show dev`'s second request, for the same interface. It is by
		// name, AF_PACKET, with the attributes in the other order — three
		// differences, none of them visible in any output either tool prints.
		iplink, err := LinkShowDev(devCst, ExtMaskShow, 52)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bytes.Equal(zeroSeqPid(iplink), reqs[1]) {
			t.Error("iplink_get reproduced ipaddr_link_get's captured request; " +
				"`link show dev` and `addr show dev` have collapsed into one command")
		}
	})

	t.Run("corner: the first request is byte-identical to `link show dev`'s first", func(t *testing.T) {
		// Stated as an equality between two CAPTURES rather than between two
		// builder calls, because the claim is about iproute2 — that
		// ll_name_to_index is reached identically from both commands — and a
		// claim about iproute2 that is checked against goip's own output
		// proves nothing.
		linkDevReqs := canonicalRequests(t, tdGatedGetLinkDev)
		if len(linkDevReqs) != 2 {
			t.Fatalf("requests in the link dev capture = %d, want 2", len(linkDevReqs))
		}
		if !bytes.Equal(reqs[0], linkDevReqs[0]) {
			t.Errorf("the two commands' first requests differ, and ll_name_to_index is the same call in both\n"+
				"addr: %x\nlink: %x", reqs[0], linkDevReqs[0])
		}
	})
}

// TestTierARouteShowDevRequests is the Tier A positive for `route show dev`:
// both requests the pinned `ip` sent, in order, byte for byte.
//
// # The selector both adds a transaction and removes several
//
// It ADDS ll_name_to_index's throwaway ll_link_get at the front
// (ip/iproute.c:2008), which is why this capture holds two requests where the
// bare `route show` capture holds one.
//
// It REMOVES every lazy by-index side-get at the back. print_route emits its
// `dev NAME` token only under `filter.oifmask != -1` (:900-901), and that
// token is the sole caller of ll_index_to_name for RTA_OIF, so naming a
// device means no name ever has to be resolved. A bare `route show` costs one
// get per distinct output interface; this form costs exactly two transactions
// however large the answer is.
//
// That last claim is the reason the mesh capture is read here as well. Its
// namespace gives the named device no routes at all, so the dump comes back
// as nothing but NLMSG_DONE — and the request count is still two. One capture
// could not tell "the transaction count is a constant" apart from "the clean
// topology happens to need no lookups", and the difference is the whole
// behavior of the selector.
//
// # The dump is the only two-attribute RTM_GETROUTE in the corpus
//
// iproute_dump_filter writes RTA_TABLE first (:1726) then RTA_OIF (:1731),
// each behind its own presence test. Order is part of the byte-equality
// contract and no other captured route request exercises it, because every
// other form carries at most one of the two.
//
// go test ./internal/goip/req/ -run TestTierARouteShowDevRequests
func TestTierARouteShowDevRequests(t *testing.T) {
	const (
		devCst  = "goip0"
		idxCst  = 3
		meshDev = "veth0"
		meshIdx = 5
	)

	reqs := canonicalRequests(t, tdGatedGetRouteDev)
	if len(reqs) != 2 {
		t.Fatalf("requests in the route dev capture = %d, want ll_link_get then the filtered dump", len(reqs))
	}

	tests := []struct {
		description string
		build       func() ([]byte, error)
		captured    []byte
	}{
		{
			// The same function `link show dev` and `addr show dev` call, so
			// the same bytes. Asserted again here because "all three `dev`
			// commands share their first request" is a claim about these three
			// captures, not about the builder.
			description: "positive: the first request is ll_link_get, by name, AF_UNSPEC, ext-mask first",
			build:       func() ([]byte, error) { return LinkShowByName(devCst, 61) },
			captured:    reqs[0],
		},
		{
			// AF_INET, not AF_UNSPEC: the promotion at ip/iproute.c:1998 keys
			// on filter.tb alone and knows nothing about `dev`, so a plain
			// `route show dev X` is promoted exactly as a plain `route show`
			// is. Then RTA_TABLE=254 and RTA_OIF=3, in that order.
			description: "positive: the second request is the dump carrying RTA_TABLE then RTA_OIF",
			build:       func() ([]byte, error) { return RouteShowDump(unix.AF_INET, unix.RT_TABLE_MAIN, idxCst, 62) },
			captured:    reqs[1],
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(zeroSeqPid(got), tc.captured) {
				t.Fatalf("request differs\n got %x\nwant %x", zeroSeqPid(got), tc.captured)
			}
		})
	}

	// The negatives and corners the rows above cannot state, because each of
	// them only says "this builder reproduces this capture".

	t.Run("negative: the unfiltered route dump does NOT reproduce the captured second request", func(t *testing.T) {
		// Without this a goip that dropped the selector on the floor would
		// fail the second row with a hex dump and no reason. It is also the
		// assertion that the index reaches the wire at all: the two requests
		// differ by one whole 8-byte attribute.
		unfiltered, err := RouteShowDump(unix.AF_INET, unix.RT_TABLE_MAIN, 0, 62)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bytes.Equal(zeroSeqPid(unfiltered), reqs[1]) {
			t.Error("the unfiltered dump equals the captured `dev` dump; RTA_OIF is not reaching the request")
		}
		if len(unfiltered)+8 != len(reqs[1]) {
			t.Errorf("the `dev` dump is %d bytes and the bare one %d; want exactly one 8-byte attribute between them",
				len(reqs[1]), len(unfiltered))
		}
	})

	t.Run("negative: the two attributes in the other order do NOT reproduce the capture", func(t *testing.T) {
		// The one property a length check and a field-by-field check both
		// miss. Built by hand rather than by a second builder, because the
		// point is that no builder in the package can produce these bytes.
		swapped := xtcpnl.CopyBytes(reqs[1])
		if len(swapped) != 44 {
			t.Fatalf("captured dump is %d bytes, want 28 + two 8-byte attributes", len(swapped))
		}
		copy(swapped[28:36], reqs[1][36:44])
		copy(swapped[36:44], reqs[1][28:36])
		if bytes.Equal(swapped, reqs[1]) {
			t.Fatal("swapping the two attributes changed nothing; they are not distinguishable and this test proves nothing")
		}
		got, err := RouteShowDump(unix.AF_INET, unix.RT_TABLE_MAIN, idxCst, 62)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bytes.Equal(zeroSeqPid(got), swapped) {
			t.Error("the builder emitted RTA_OIF before RTA_TABLE; iproute_dump_filter writes the table first (ip/iproute.c:1726)")
		}
	})

	t.Run("corner: the first request is byte-identical to `link show dev`'s and `addr show dev`'s", func(t *testing.T) {
		// Stated as an equality between CAPTURES rather than between builder
		// calls, because the claim is about iproute2 — that ll_name_to_index
		// is reached identically from all three objects — and a claim about
		// iproute2 checked against goip's own output proves nothing.
		for _, other := range []struct {
			name string
			path string
		}{
			{"link show dev", tdGatedGetLinkDev},
			{"addr show dev", tdGatedGetAddrDev},
		} {
			otherReqs := canonicalRequests(t, other.path)
			if len(otherReqs) == 0 {
				t.Fatalf("no requests in %s", other.path)
			}
			if !bytes.Equal(reqs[0], otherReqs[0]) {
				t.Errorf("`route show dev` and `%s` differ in their first request, and ll_name_to_index is the same call in both\n"+
					"route: %x\n%5s: %x", other.name, reqs[0], "other", otherReqs[0])
			}
		}
	})

	t.Run("boundary: an answerless topology sends the same two requests, with its own index", func(t *testing.T) {
		// The mesh namespace's named device owns no routes, so this capture's
		// dump is answered by NLMSG_DONE alone. Everything on the REQUEST side
		// is unchanged, which is the assertion: the cost of this command does
		// not depend on the size of its answer.
		meshReqs := canonicalRequests(t, tdGatedGetRouteDevMesh)
		if len(meshReqs) != 2 {
			t.Fatalf("requests in the mesh route dev capture = %d, want the same two as the clean topology", len(meshReqs))
		}
		name, err := LinkShowByName(meshDev, 61)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(zeroSeqPid(name), meshReqs[0]) {
			t.Errorf("mesh ll_link_get differs\n got %x\nwant %x", zeroSeqPid(name), meshReqs[0])
		}
		dump, err := RouteShowDump(unix.AF_INET, unix.RT_TABLE_MAIN, meshIdx, 62)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(zeroSeqPid(dump), meshReqs[1]) {
			t.Errorf("mesh dump differs\n got %x\nwant %x", zeroSeqPid(dump), meshReqs[1])
		}
		// And the only difference between the two topologies' dumps is the
		// index, which is what makes the row above a statement about the
		// command rather than about either namespace.
		if len(meshReqs[1]) != len(reqs[1]) {
			t.Errorf("mesh dump is %d bytes, clean one %d; the request shape must not depend on the topology",
				len(meshReqs[1]), len(reqs[1]))
		}
	})
}

// TestTierANeighShowDevRequests is the Tier A positive for `neigh show dev`,
// and the one whose interesting property is what the capture does NOT contain.
//
// # Two requests, the same two as the bare command
//
// Every other `dev NAME` form in goip pays a throwaway ll_link_get for the
// name: `link show dev` and `addr show dev` add one at the front,
// `route show dev` adds one there and deletes the lazy ones at the back. This
// capture holds no such request. do_show_or_flush calls ll_init_map(&rth) at
// ip/ipneigh.c:597 — unconditionally, for the bare command as much as for this
// one — and only then resolves the name at :600, out of the cache that dump
// just filled, so ll_get_by_name hits and ll_link_get is never reached
// (lib/ll_map.c:354-359).
//
// So the first request here is byte-identical to the bare capture's first, and
// the whole difference between the two commands is 8 bytes on the second. That
// makes this the narrowest delta in the corpus, and the reason a pcap is
// needed rather than a sidecar: the request delta produces no output, and the
// output delta — print_neigh's `dev` token, suppressed at :415 — produces no
// request.
//
// # And the index is an attribute, not the field of that name
//
// ipneigh_dump_filter writes `addattr32(nlh, reqlen, NDA_IFINDEX,
// filter.index)` (:493), leaving the `ndm_ifindex` member at offset 4 of the
// ndmsg zero — the opposite of the addr equivalent, where the index IS the
// header field. The negative below is what holds goip to it.
//
// go test ./internal/goip/req/ -run TestTierANeighShowDevRequests
func TestTierANeighShowDevRequests(t *testing.T) {
	// No device NAME constant, unlike the route test: neither request carries
	// one. ll_init_map's dump is unfiltered and the neighbor dump names the
	// interface by index, so `goip0` appears nowhere on the wire — which is
	// itself the shape of the command.
	const (
		idxCst  = 3
		meshIdx = 5
	)

	reqs := canonicalRequests(t, tdGatedGetNeighDev)
	if len(reqs) != 2 {
		t.Fatalf("requests in the neigh dev capture = %d, want ll_init_map's link dump then the filtered neighbor dump", len(reqs))
	}

	tests := []struct {
		description string
		build       func() ([]byte, error)
		captured    []byte
	}{
		{
			// RTEXT_FILTER_VF alone, 0x01, not the 0x09 every `show` dump
			// carries: at the pinned iproute2 7.1.0 ll_init_map passes only
			// that flag. Commit 7bd7f335 adds SKIP_STATS and de91e928 adds
			// NAME_ONLY, and neither is in the pin — the version-skew fault
			// line the allowlist already carries an entry for.
			description: "positive: the first request is ll_init_map's link dump, AF_UNSPEC, EXT_MASK 0x01",
			build:       func() ([]byte, error) { return NeighShowLinkDump(71) },
			captured:    reqs[0],
		},
		{
			description: "positive: the second request is the neighbor dump carrying NDA_IFINDEX",
			build:       func() ([]byte, error) { return NeighShowDump(unix.AF_UNSPEC, 0, idxCst, 72) },
			captured:    reqs[1],
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(zeroSeqPid(got), tc.captured) {
				t.Fatalf("request differs\n got %x\nwant %x", zeroSeqPid(got), tc.captured)
			}
		})
	}

	t.Run("negative: the unfiltered neighbor dump does NOT reproduce the captured second request", func(t *testing.T) {
		// Without this, a goip that dropped the selector would fail the row
		// above with a hex dump and no reason. The length check is the
		// positive half: the two differ by exactly one 8-byte attribute.
		unfiltered, err := NeighShowDump(unix.AF_UNSPEC, 0, 0, 72)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bytes.Equal(zeroSeqPid(unfiltered), reqs[1]) {
			t.Error("the unfiltered dump equals the captured `dev` dump; NDA_IFINDEX is not reaching the request")
		}
		if len(unfiltered)+8 != len(reqs[1]) {
			t.Errorf("the `dev` dump is %d bytes and the bare one %d; want exactly one 8-byte attribute between them",
				len(reqs[1]), len(unfiltered))
		}
	})

	t.Run("negative: the index in ndm_ifindex instead of NDA_IFINDEX does NOT reproduce the capture", func(t *testing.T) {
		// The trap, checked against the capture rather than against a second
		// builder. `struct ndmsg` has an ndm_ifindex at offset 4 of the body,
		// so a request that filled it would be well formed, the same 28 bytes
		// as the bare dump, and filtered by nothing — plausible enough that
		// only iproute2's own bytes settle it.
		const bodyAt = 16 // nlmsghdr
		if len(reqs[1]) < bodyAt+12 {
			t.Fatalf("captured dump is %d bytes, too short to hold an ndmsg", len(reqs[1]))
		}
		if got := binary.LittleEndian.Uint32(reqs[1][bodyAt+4 : bodyAt+8]); got != 0 {
			t.Errorf("captured ndm_ifindex = %d, want 0; iproute2 sends the index as NDA_IFINDEX (ip/ipneigh.c:493)", got)
		}
	})

	t.Run("corner: the first request is byte-identical to the bare `neigh show`'s", func(t *testing.T) {
		// Stated as an equality between CAPTURES, because the claim is about
		// iproute2 — that ll_init_map runs the same way whether or not a
		// device was named — and checking it against goip's own output would
		// prove only that goip is self-consistent.
		bare := canonicalRequests(t, tdGetNeigh)
		if len(bare) != 2 {
			t.Fatalf("requests in the bare neigh capture = %d, want two", len(bare))
		}
		if !bytes.Equal(reqs[0], bare[0]) {
			t.Errorf("`neigh show dev` and `neigh show` differ in their first request, and ll_init_map is the same call in both\n"+
				" dev: %x\nbare: %x", reqs[0], bare[0])
		}
		if bytes.Equal(reqs[1], bare[1]) {
			t.Error("the two second requests are equal; the whole difference between the commands lives there")
		}
	})

	t.Run("corner: `neigh show dev` sends no single-get, unlike every other dev form", func(t *testing.T) {
		// The absence, asserted directly. Both requests in this capture are
		// dumps; the three other `dev` captures each open with a
		// non-dump RTM_GETLINK. Reading the flags rather than counting
		// requests, so a capture that gained a third dump would still be
		// reported honestly by the length check above.
		// nlmsg_flags is at offset 6 of the nlmsghdr, after nlmsg_len and
		// nlmsg_type.
		const flagsAt = 6
		for i, r := range reqs {
			flags := binary.LittleEndian.Uint16(r[flagsAt : flagsAt+2])
			if flags&unix.NLM_F_DUMP == 0 {
				t.Errorf("request %d has flags %#x, a single-get; ll_init_map should have made every lookup a cache hit", i, flags)
			}
		}
		// And the contrast, from the capture that does pay for one.
		routeDev := canonicalRequests(t, tdGatedGetRouteDev)
		if len(routeDev) == 0 {
			t.Fatalf("no requests in %s", tdGatedGetRouteDev)
		}
		if flags := binary.LittleEndian.Uint16(routeDev[0][flagsAt : flagsAt+2]); flags&unix.NLM_F_DUMP != 0 {
			t.Errorf("`route show dev`'s first request has flags %#x, a dump; it should be ll_link_get's single-get, "+
				"and if it is not then the contrast this test draws is meaningless", flags)
		}
	})

	t.Run("boundary: the mesh topology sends the same two requests, with its own index", func(t *testing.T) {
		meshReqs := canonicalRequests(t, tdGatedGetNeighDevMesh)
		if len(meshReqs) != 2 {
			t.Fatalf("requests in the mesh neigh dev capture = %d, want the same two as the clean topology", len(meshReqs))
		}
		link, err := NeighShowLinkDump(71)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(zeroSeqPid(link), meshReqs[0]) {
			t.Errorf("mesh ll_init_map dump differs\n got %x\nwant %x", zeroSeqPid(link), meshReqs[0])
		}
		dump, err := NeighShowDump(unix.AF_UNSPEC, 0, meshIdx, 72)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(zeroSeqPid(dump), meshReqs[1]) {
			t.Errorf("mesh neighbor dump differs\n got %x\nwant %x", zeroSeqPid(dump), meshReqs[1])
		}
		if len(meshReqs[1]) != len(reqs[1]) {
			t.Errorf("mesh dump is %d bytes, clean one %d; the request shape must not depend on the topology",
				len(meshReqs[1]), len(reqs[1]))
		}
	})
}

// TestLinkShowDev covers the SECOND by-name single-get, iplink_get, over
// names the capture does not contain.
//
// The table is deliberately the mirror of TestLinkShowByName above: the same
// names, the same lengths, the same error rows — and the opposite attribute
// order with a different ifi_family. Keeping them parallel is the point.
// These two requests are the most confusable pair in the whole corpus, since
// one command sends both about the same interface within microseconds, and
// the way to keep them straight is to state their differences at the same
// offsets in two tables that otherwise read alike.
//
// go test ./internal/goip/req/ -run TestLinkShowDev
func TestLinkShowDev(t *testing.T) {
	tests := []struct {
		description string
		name        string
		wantLen     int
		wantErr     error
	}{
		{
			// Same 48 bytes as LinkShowByName: the two attributes are the
			// same size, so length cannot tell the requests apart — only the
			// family byte and the attribute order can.
			description: "positive: a short name yields IFLA_IFNAME then ext-mask",
			name:        "lo",
			wantLen:     48,
		},
		{
			description: "boundary: a 15-character name is the longest accepted",
			name:        "ve-nordlayepDd-",
			wantLen:     60,
		},
		{
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
			description: "corner: a name containing a NUL is rejected",
			name:        "lo\x00extra",
			wantErr:     xtcpnl.ErrBadIfName,
		},
		{
			description: "corner: a name containing a slash is rejected",
			name:        "eth/0",
			wantErr:     xtcpnl.ErrBadIfName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := LinkShowDev(tc.name, ExtMaskShow, 7)
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
			// ifi_family is preferred_family (ip/iplink.c:1502), which
			// ipaddr_list_link forced to AF_PACKET at ip/ipaddress.c:2416
			// before parsing any argument. ll_link_get's zero-initialized
			// header leaves AF_UNSPEC here, so this one byte is the cheapest
			// way to tell the two requests apart on a wire trace.
			if fam := got[16]; fam != unix.AF_PACKET {
				t.Errorf("ifi_family = %d, want AF_PACKET", fam)
			}
			// And the attribute order, reversed from ll_link_get's:
			// IFLA_IFNAME at ip/iplink.c:1513, IFLA_EXT_MASK at :1514.
			if at := binary.LittleEndian.Uint16(got[34:36]); at != uint16(unix.IFLA_IFNAME) {
				t.Errorf("first attribute = %d, want IFLA_IFNAME", at)
			}
			second := 36 + len(tc.name) + 1
			second += (4 - second%4) % 4 // rta_align past the name payload
			if at := binary.LittleEndian.Uint16(got[second+2 : second+4]); at != uint16(unix.IFLA_EXT_MASK) {
				t.Errorf("second attribute = %d, want IFLA_EXT_MASK", at)
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
			build:       func() ([]byte, error) { return AddrShowDump(unix.AF_INET, 0, 2), nil },
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
			build:       func() ([]byte, error) { return AddrShowDump(unix.AF_INET6, 0, 4), nil },
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
// family byte and — under `dev NAME` — an interface index.
//
// The ifindex column is the `dev NAME` half. It is asserted here rather than
// only through obj_addr, because ipaddr_dump_filter writes filter.ifindex into
// the ifaddrmsg HEADER and not into an attribute (ip/ipaddress.c:1954-1958),
// so getting it wrong produces a 24-byte datagram of exactly the right length
// with four bytes in the wrong place. Length and message type would both still
// pass; only a positional byte check catches it.
//
// go test ./internal/goip/req/ -run TestAddrShowDumpShape
func TestAddrShowDumpShape(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		ifindex     uint32
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
		{
			// The `dev NAME` form. Same length, same type, same flags: the
			// entire difference between `ip addr show` and
			// `ip addr show dev X` on this request is four bytes at offset 4
			// of the ifaddrmsg.
			description: "positive: `addr show dev NAME` puts the resolved index in ifa_index and changes nothing else",
			family:      unix.AF_UNSPEC,
			ifindex:     2,
		},
		{
			description: "positive: the index survives alongside a family, which is what `-4 addr show dev X` sends",
			family:      unix.AF_INET,
			ifindex:     2,
		},
		{
			// Not a plausible ifindex, and that is the point: the field is a
			// u32 written little-endian, so a builder that truncated it to a
			// byte or wrote it big-endian passes every other row here.
			description: "boundary: a high index is written as a full little-endian u32, not truncated",
			family:      unix.AF_UNSPEC,
			ifindex:     0x01020304,
		},
		{
			// Documented in the builder: ifindex 0 IS the unfiltered form,
			// because ipaddr_dump_filter assigns unconditionally and
			// filter.ifindex is 0 when no `dev` was given. This row pins that
			// the two spellings cannot drift apart.
			description: "corner: index 0 is the unfiltered request, not a filter on interface zero",
			family:      unix.AF_INET6,
			ifindex:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := AddrShowDump(tc.family, tc.ifindex, 5)
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
			// ifa_prefixlen, ifa_flags and ifa_scope stay zero on every form:
			// ipaddr_dump_filter touches ifa_index alone, so a non-zero byte
			// here would mean goip had invented a filter `ip` did not send.
			for i := 17; i < 20; i++ {
				if got[i] != 0 {
					t.Errorf("ifaddrmsg byte %d = %#x, want 0", i-16, got[i])
				}
			}
			if idx := binary.LittleEndian.Uint32(got[20:24]); idx != tc.ifindex {
				t.Errorf("ifa_index = %d, want %d (bytes %x)", idx, tc.ifindex, got[20:24])
			}
		})
	}

	t.Run("corner: the index-0 form is byte-identical to the unfiltered builder", func(t *testing.T) {
		// Stated as an equality rather than as two separate shape checks,
		// because the claim in AddrShowDump's doc is that ip has ONE request
		// here. xtcpnl.BuildDumpAddrRequest is the older builder that predates
		// the `dev` selector; if the two ever diverge, every `addr show`
		// fixture in the corpus is describing a request goip no longer sends.
		with := AddrShowDump(unix.AF_UNSPEC, 0, 7)
		without := xtcpnl.BuildDumpAddrRequest(unix.AF_UNSPEC, 7)
		if !bytes.Equal(with, without) {
			t.Errorf("AddrShowDump(_, 0, _) = %x, BuildDumpAddrRequest = %x", with, without)
		}
	})
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
			got, err := RouteShowDump(tc.family, tc.table, 0, 42)
			if err != nil {
				t.Fatalf("RouteShowDump(%d, %d): %v", tc.family, tc.table, err)
			}
			if !bytes.Equal(zeroSeqPid(got), captured[0]) {
				t.Errorf("built    %x\ncaptured %x", zeroSeqPid(got), captured[0])
			}
		})
	}
}

// routeAttr is one expected u32 attribute of an RTM_GETROUTE dump request, in
// emission order. Both of iproute_dump_filter's attributes are u32, so one
// shape covers the whole surface.
type routeAttr struct {
	typ   uint16
	value uint32
}

// TestRouteShowDumpShape covers the selector values that have no capture: the
// spellings of "no filter", the edges of the byte the header field can hold,
// the values only RTA_TABLE can express, and the interaction between the two
// attributes.
//
// Two structural invariants, checked on every row.
//
// rtm_table in the HEADER stays 0 no matter what the attribute says. iproute2
// never writes it for a dump (iproute_dump_filter only adds the attribute,
// ip/iproute.c:1726), and filling it in would be a plausible-looking change
// that makes every request differ from the capture by one byte.
//
// The attributes come out in iproute_dump_filter's order — RTA_TABLE at :1726
// then RTA_OIF at :1731 — with no gaps and nothing else. The expectation is a
// LIST rather than a set of independent field checks, because attribute order
// is part of the byte-equality contract and per-field assertions would pass on
// the reversed pair.
//
// go test ./internal/goip/req/ -run TestRouteShowDumpShape
func TestRouteShowDumpShape(t *testing.T) {
	const (
		hdrAndRtmsg = 28 // 16-byte nlmsghdr + 12-byte rtmsg
		attrLen     = 8  // one u32 attribute: 4-byte rtattr + 4-byte payload
	)

	tests := []struct {
		description string
		family      uint8
		table       uint32
		oif         uint32
		wantAttrs   []routeAttr
	}{
		{
			description: "positive: RT_TABLE_MAIN carries RTA_TABLE=254",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 254}},
		},
		{
			// `table all` and `table 0` are different spellings that both land
			// on filter.tb = 0, so they must build the same bytes. The "0"
			// spelling parses as a table id; "all" does not and is caught by
			// iproute2's fallback (ip/iproute.c:1849).
			description: "boundary: RT_TABLE_UNSPEC omits the attribute entirely",
			family:      unix.AF_UNSPEC,
			table:       unix.RT_TABLE_UNSPEC,
		},
		{
			// The largest id rtm_table could have held, which is exactly why
			// it is worth a row: the attribute must carry it and the header
			// byte must still be 0.
			description: "boundary: table 255 is RT_TABLE_LOCAL and fits a byte, but still goes in the attribute",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_LOCAL,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 255}},
		},
		{
			// One past the byte. A host with `ip route add ... table 256` has
			// a table no rtm_table can name, which is the whole reason
			// RTA_TABLE exists.
			description: "corner: table 256 is unrepresentable in rtm_table and must survive in the attribute",
			family:      unix.AF_INET,
			table:       256,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 256}},
		},
		{
			// RT_TABLE_MAX. rtnl_rttable_a2n accepts it, so goip must build it
			// rather than silently truncating to 0xFF.
			description: "corner: table 4294967295 is RT_TABLE_MAX and round-trips whole",
			family:      unix.AF_INET,
			table:       0xFFFFFFFF,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 0xFFFFFFFF}},
		},
		{
			// `ip route show dev goip0` on the clean topology. The ORDER is
			// the assertion: RTA_TABLE comes first even though `dev` was the
			// only thing on the command line, because filter.tb's default is
			// what puts the table attribute there.
			description: "positive: `route show dev NAME` is RTA_TABLE then RTA_OIF",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
			oif:         3,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 254}, {unix.RTA_OIF, 3}},
		},
		{
			// `ip route show table all dev goip0`. The two presence tests are
			// independent, so the device filter survives the table filter
			// going away — and the family stays AF_UNSPEC, because the
			// promotion at ip/iproute.c:1998 keys on the table alone and
			// knows nothing about `dev`.
			description: "boundary: `table all dev NAME` carries RTA_OIF alone and stays AF_UNSPEC",
			family:      unix.AF_UNSPEC,
			table:       unix.RT_TABLE_UNSPEC,
			oif:         3,
			wantAttrs:   []routeAttr{{unix.RTA_OIF, 3}},
		},
		{
			// No interface has index 0, so `if (filter.oif)` (:1731) uses the
			// value as its own presence flag. This row is what keeps the bare
			// `route show` bytes from acquiring a zero-valued RTA_OIF now
			// that the parameter exists.
			description: "corner: oif 0 is not a filter and appends nothing",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
			oif:         0,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 254}},
		},
		{
			// Neither selector: the 28-byte form, reachable from the CLI as
			// `route show table all`, here with an explicit zero device as
			// well to show the two zeros compose rather than interfering.
			description: "boundary: no table and no device is the bare 28-byte rtmsg",
			family:      unix.AF_UNSPEC,
			table:       unix.RT_TABLE_UNSPEC,
			oif:         0,
		},
		{
			// An ifindex is signed in the kernel and filter.oif is an `int`,
			// but RTA_OIF is a u32 on the wire. A builder that narrowed it
			// anywhere would show up here and in no capture, because no real
			// host reaches these indexes.
			description: "corner: a top-bit-set ifindex round-trips whole in RTA_OIF",
			family:      unix.AF_INET,
			table:       unix.RT_TABLE_MAIN,
			oif:         0xFFFFFFFF,
			wantAttrs:   []routeAttr{{unix.RTA_TABLE, 254}, {unix.RTA_OIF, 0xFFFFFFFF}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := RouteShowDump(tc.family, tc.table, tc.oif, 9)
			if err != nil {
				t.Fatalf("RouteShowDump: %v", err)
			}
			wantLen := hdrAndRtmsg + attrLen*len(tc.wantAttrs)
			if len(got) != wantLen {
				t.Fatalf("len = %d, want %d: %x", len(got), wantLen, got)
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
			// Every other rtmsg byte is zero for a show with no address
			// selector: a non-zero dst_len or scope would be a filter `ip`
			// did not send.
			for i := 17; i < 28; i++ {
				if got[i] != 0 {
					t.Errorf("rtmsg byte %d = %#x, want 0", i-16, got[i])
				}
			}
			for i, want := range tc.wantAttrs {
				off := hdrAndRtmsg + i*attrLen
				if l := binary.LittleEndian.Uint16(got[off : off+2]); l != attrLen {
					t.Errorf("attribute %d rta_len = %d, want %d", i, l, attrLen)
				}
				if typ := binary.LittleEndian.Uint16(got[off+2 : off+4]); typ != want.typ {
					t.Errorf("attribute %d type = %d, want %d", i, typ, want.typ)
				}
				if v := binary.LittleEndian.Uint32(got[off+4 : off+8]); v != want.value {
					t.Errorf("attribute %d value = %d, want %d", i, v, want.value)
				}
			}
		})
	}
}

// TestTierARuleShowRequests compares the built rule dump request against the
// bytes the pinned `ip` emitted, for both families.
//
// # The one command in goip whose request is fully determined
//
// Every other Tier A test above has at least one degree of freedom to get
// wrong — a family, a mask, a table, an index. This one has a single byte:
// rtnl_ruledump_req (lib/libnetlink.c:407-421) writes a 12-byte fib_rule_hdr
// with only frh_family set, and the kernel's strict-mode validator rejects a
// nonzero dst_len, src_len, tos, table, res1, res2, action or flags
// (net/core/fib_rules.c:1271-1276) and refuses any attribute at all
// (:1278-1281).
//
// So the assertion is narrow and total: 28 bytes, of which 27 are fixed. The
// value in checking it against a capture rather than against the C is that the
// one free byte is precisely the one the kernel does NOT validate — a wrong
// family is answered rather than refused, with a different family's rules.
//
// go test ./internal/goip/req/ -run TestTierARuleShowRequests
func TestTierARuleShowRequests(t *testing.T) {
	tests := []struct {
		description string
		capture     string
		family      uint8
	}{
		{
			// AF_INET and not AF_UNSPEC, because iprule_list_flush_or_save
			// substitutes it before building anything (ip/iprule.c:748-752).
			// That substitution is in obj_rule.go, so this row is also what
			// proves it is applied: a goip that left it out would build the
			// same 28 bytes with a 0 in the first position of the body.
			description: "positive: `rule show` is AF_INET with no attributes",
			capture:     tdGatedGetRule,
			family:      unix.AF_INET,
		},
		{
			description: "positive: `-6 rule show` differs in exactly the family byte",
			capture:     tdGatedGetRule6,
			family:      unix.AF_INET6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var captured [][]byte
			for _, r := range canonicalRequests(t, tc.capture) {
				if binary.LittleEndian.Uint16(r[4:6]) == uint16(unix.RTM_GETRULE) {
					captured = append(captured, r)
				}
			}
			if len(captured) != 1 {
				t.Fatalf("RTM_GETRULE requests in %s = %d, want exactly 1; the "+
					"capture is polluted and the expectation no longer describes "+
					"a single command", tc.capture, len(captured))
			}
			got, err := RuleShowDump(tc.family, 42)
			if err != nil {
				t.Fatalf("RuleShowDump(%d): %v", tc.family, err)
			}
			if !bytes.Equal(zeroSeqPid(got), captured[0]) {
				t.Errorf("built    %x\ncaptured %x", zeroSeqPid(got), captured[0])
			}
		})
	}
}

// TestRuleShowDumpShape asserts the invariants the two captured families cannot
// distinguish between them: the length, the flags, and that all eleven bytes
// after the family are zero.
//
// It is the negative half of the test above. Byte equality against a capture
// proves AF_INET and AF_INET6 are right; it says nothing about AF_UNSPEC or
// AF_PACKET, and those are exactly the two a goip bug would produce — the
// first by skipping the substitution, the second by honoring `-0`. Both are
// legal requests the kernel answers, so nothing downstream would complain.
//
// go test ./internal/goip/req/ -run TestRuleShowDumpShape
func TestRuleShowDumpShape(t *testing.T) {
	// 16-byte nlmsghdr + 12-byte fib_rule_hdr, and nothing else is possible.
	const hdrAndFibRule = 28

	tests := []struct {
		description string
		family      uint8
	}{
		{
			description: "positive: AF_INET, the family a bare `rule show` resolves to",
			family:      unix.AF_INET,
		},
		{
			description: "positive: AF_INET6, the only other family `ip rule` produces",
			family:      unix.AF_INET6,
		},
		{
			// The substitution's input, which must never reach the wire from
			// goip — the kernel would answer it with every family's rules at
			// once. The builder still has to encode it faithfully, because the
			// substitution's home is the object layer; this row pins the
			// builder's half of that split.
			description: "boundary: AF_UNSPEC encodes as zero, so a missing substitution is visible in the bytes",
			family:      unix.AF_UNSPEC,
		},
		{
			// `-0`. preferred_family survives the substitution, reaches the
			// kernel, finds no AF_PACKET rules_ops, and the dump comes back
			// empty rather than erroring.
			description: "corner: AF_PACKET passes through unchanged, which is how `-0 rule show` comes back empty",
			family:      unix.AF_PACKET,
		},
		{
			description: "corner: a family byte the kernel has no rules_ops for is still encoded, not rejected here",
			family:      0xFF,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := RuleShowDump(tc.family, 9)
			if err != nil {
				t.Fatalf("RuleShowDump: %v", err)
			}
			if len(got) != hdrAndFibRule {
				t.Fatalf("len = %d, want %d: %x", len(got), hdrAndFibRule, got)
			}
			if mt := binary.LittleEndian.Uint16(got[4:6]); mt != uint16(unix.RTM_GETRULE) {
				t.Errorf("nlmsg_type = %d, want RTM_GETRULE", mt)
			}
			if want := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP); binary.LittleEndian.Uint16(got[6:8]) != want {
				t.Errorf("nlmsg_flags = %#x, want %#x", binary.LittleEndian.Uint16(got[6:8]), want)
			}
			if got[16] != tc.family {
				t.Errorf("frh_family = %d, want %d", got[16], tc.family)
			}
			// Bytes 17..27 are dst_len, src_len, tos, table, res1, res2,
			// action and the four flag bytes. The validator refuses the dump
			// if any is nonzero, so this is not style — it is the difference
			// between a dump and an EINVAL.
			for i := 17; i < hdrAndFibRule; i++ {
				if got[i] != 0 {
					t.Errorf("fib_rule_hdr byte %d = %#x, want 0; "+
						"fib_valid_dumprule_req refuses a nonzero one", i-16, got[i])
				}
			}
		})
	}
}
