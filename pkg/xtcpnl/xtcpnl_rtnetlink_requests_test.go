package xtcpnl

// Tests for the per-family request builders (xtcpnl_rtnetlink_requests.go) and
// for TalkRtnetlink, the single-get transport.
//
// The builder positives are the highest-value assertions in the netlink work,
// and they cost nothing to run: no root, no nlmon, no VM. They compare our
// bytes against the bytes iproute2 7.1.0 actually put on the wire, lifted from
// the committed bulk captures and compared byte-for-byte after zeroing
// nlmsg_seq and nlmsg_pid.
//
// Where a form has no capture, the row is labeled boundary or corner and says
// which iproute2 function it mirrors and why no recording covers it. That
// labeling is the point: a positive row means "iproute2 sent exactly these
// bytes", and nothing else is allowed to claim it.
//
// The list of uncaptured forms has shrunk as the gated-topology corpus grew —
// the neigh dump, the default `route show` and both by-name single gets have
// positives now — so what is left structural is genuinely uncapturable rather
// than merely not yet captured: masks and families no `ip` invocation
// produces, and names chosen to move a padded attribute's offset.

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Record indices of the request datagrams in the three bulk captures, found by
// walking every record and keeping those with NLM_F_REQUEST set and
// nlmsg_pid == 0. Recorded as constants so a regenerated fixture that shifts
// them fails in capturedRequest's assertions rather than silently comparing
// against the wrong record.
const (
	// `ip link show`: RTM_GETLINK dump, AF_PACKET, IFLA_EXT_MASK=0x09.
	recGetLinkShowCst = 0

	// `ip -4 addr show` then `ip -6 addr show`, each a link dump followed by an
	// addr dump. The link dumps carry no attribute because iproute2 skips its
	// filter callback for AF_INET/AF_INET6 (lib/libnetlink.c:595).
	recAddrV4LinkDumpCst = 62
	recAddrV4AddrDumpCst = 67
	recAddrV6LinkDumpCst = 70
	recAddrV6AddrDumpCst = 74

	// `ip route show table all`: RTM_GETROUTE dump, all-zero rtmsg, no attrs.
	recRouteShowAllCst = 0

	// The first of ten ll_link_get single-gets in the route capture.
	recRouteSingleGetCst = 4

	// `ip neigh show` in the gated-topology corpus
	// (7_1_4/dumps/netlink_route_getneigh.pcap): ll_init_map's AF_UNSPEC link
	// dump, then the neighbor dump itself. Two requests in the whole file,
	// because that capture was taken in a microVM with nothing else on
	// netlink.
	recNeighLinkDumpCst = 0
	recNeighDumpCst     = 4

	// The gated-topology route captures, one command per file. Record 0 is
	// the RTM_GETROUTE dump in all three; the RTM_GETLINK single-gets that
	// follow are ll_index_to_name resolving the device lazily while printing.
	recGatedRouteDumpCst  = 0
	recGatedRoute6DumpCst = 0

	// `ip link show dev goip0` (7_1_4/dumps/netlink_route_getlink_dev.pcap):
	// the two by-name single-gets the command sends, and nothing else. They
	// are records 0 and 2 because each one's reply sits between them.
	//
	// This capture is why neither by-name builder needs a structural row for
	// its main form. It holds the pair — same command, same interface, the
	// same two attributes in opposite orders — which is the one thing no
	// hand-built expectation could establish on its own.
	recLinkDevNameGetCst  = 0 // ll_link_get, AF_UNSPEC, ext-mask first
	recLinkDevPrintGetCst = 2 // iplink_get, AF_PACKET, name first
)

// Measured datagram sizes, which are nlmsg_len plus iproute2's oversend. These
// are asserted so the numbers pkg/nlparity reports informationally have a
// source in the tree rather than in a commit message: `char buf[128]` for addr
// and route, none for the link dump and the single get.
const (
	dgramGetLinkShowCst  = 40  // rtnl_linkdump_req_filter_fn sends nlmsg_len
	dgramAddrDumpCst     = 152 // 24 + char buf[128]
	dgramRouteShowAllCst = 156 // 28 + char buf[128]
	dgramSingleGetCst    = 40  // rtnl_talk sends nlmsg_len
	dgramByNameGetCst    = 52  // likewise; 12 more than by-index for IFLA_IFNAME
)

// capturedRequests returns every request datagram in a bulk capture, indexed by
// pcap record number, along with each one's full datagram length.
//
// A "request" is NLM_F_REQUEST set and nlmsg_pid == 0. Direction cannot come
// from sll_pkttype — it is PACKET_OUTGOING for requests and replies alike,
// because AF_PACKET's dev_queue_xmit_nit overwrites what
// __netlink_deliver_tap_skb set.
func capturedRequests(t *testing.T, path string) (msgs map[int][]byte, dgramLens map[int]int) {
	t.Helper()

	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", path, err)
	}

	msgs = make(map[int][]byte)
	dgramLens = make(map[int]int)
	for i, r := range recs {
		_, body, perr := r.NetlinkPayload()
		if perr != nil || len(body) < NlMsgHdrSizeCst {
			continue
		}
		flags := binary.LittleEndian.Uint16(body[6:8])
		pid := binary.LittleEndian.Uint32(body[12:16])
		if flags&uint16(unix.NLM_F_REQUEST) == 0 || pid != 0 {
			continue
		}
		msgLen := int(binary.LittleEndian.Uint32(body[0:4]))
		if msgLen < NlMsgHdrSizeCst || msgLen > len(body) {
			continue
		}
		msgs[i] = zeroSeqPid(body[:msgLen])
		dgramLens[i] = len(body)
	}
	if len(msgs) == 0 {
		t.Fatalf("%s: no request datagrams found", path)
	}
	return msgs, dgramLens
}

// capturedRequest fetches one request by record index, failing if the record is
// absent or is not the expected message type.
func capturedRequest(t *testing.T, msgs map[int][]byte, rec int, wantType uint16) []byte {
	t.Helper()
	msg, ok := msgs[rec]
	if !ok {
		t.Fatalf("record %d is not a request in this capture (fixture regenerated?)", rec)
	}
	if got := binary.LittleEndian.Uint16(msg[4:6]); got != wantType {
		t.Fatalf("record %d nlmsg_type = %d, want %d", rec, got, wantType)
	}
	return msg
}

// mustBuildReq unwraps a fallible builder inside a table literal.
//
// It panics rather than taking a *testing.T because Go allows a multi-value
// call only as the SOLE argument to a function, so threading t would force
// every call site into a closure. A panic in a test is a failure with a stack,
// which is what is wanted for a builder that cannot fail on these inputs —
// the fallible paths have their own table, TestRequestBuilderErrors.
func mustBuildReq(b []byte, err error) []byte {
	if err != nil {
		panic("xtcpnl test: request builder failed: " + err.Error())
	}
	return b
}

// ---- the request builders ----------------------------------------------------

// TestRequestBuilders is §8.2 of the goip plan: every request shape goip needs,
// compared byte-for-byte.
//
// go test ./pkg/xtcpnl/ -run TestRequestBuilders$
func TestRequestBuilders(t *testing.T) {
	const extMask = RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS // 0x09

	linkMsgs, linkDgrams := capturedRequests(t, tdRouteBulkGetLink_7_1_8)
	addrMsgs, addrDgrams := capturedRequests(t, tdRouteBulkGetAddr_7_1_8)
	routeMsgs, routeDgrams := capturedRequests(t, tdRouteBulkGetRoute_7_1_8)
	neighMsgs, _ := capturedRequests(t, tdDumpGetNeigh_7_1_4)
	gatedRouteMsgs, _ := capturedRequests(t, tdDumpGetRoute_7_1_4)
	gatedRoute6Msgs, _ := capturedRequests(t, tdDumpGetRoute6_7_1_4)
	linkDevMsgs, linkDevDgrams := capturedRequests(t, tdDumpGetLinkDev_7_1_4)

	const wantDumpFlags = uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)
	const wantGetFlags = uint16(unix.NLM_F_REQUEST)

	tests := []struct {
		description string
		got         []byte
		want        []byte
	}{
		// positive — the expectation is iproute2's own captured bytes
		{
			description: "positive: ip link show link dump (AF_PACKET, EXT_MASK 0x09)",
			got:         mustBuildReq(BuildDumpLinkRequestExt(unix.AF_PACKET, extMask, 0)),
			want:        capturedRequest(t, linkMsgs, recGetLinkShowCst, uint16(unix.RTM_GETLINK)),
		},
		{
			description: "positive: ip -4 addr show link dump (AF_INET, no attrs)",
			got:         mustBuildReq(BuildDumpLinkRequestExt(unix.AF_INET, 0, 0)),
			want:        capturedRequest(t, addrMsgs, recAddrV4LinkDumpCst, uint16(unix.RTM_GETLINK)),
		},
		{
			description: "positive: ip -6 addr show link dump (AF_INET6, no attrs)",
			got:         mustBuildReq(BuildDumpLinkRequestExt(unix.AF_INET6, 0, 0)),
			want:        capturedRequest(t, addrMsgs, recAddrV6LinkDumpCst, uint16(unix.RTM_GETLINK)),
		},
		{
			description: "positive: ip -4 addr show addr dump (ifaddrmsg, AF_INET)",
			got:         BuildDumpAddrRequest(unix.AF_INET, 0),
			want:        capturedRequest(t, addrMsgs, recAddrV4AddrDumpCst, uint16(unix.RTM_GETADDR)),
		},
		{
			description: "positive: ip -6 addr show addr dump (ifaddrmsg, AF_INET6)",
			got:         BuildDumpAddrRequest(unix.AF_INET6, 0),
			want:        capturedRequest(t, addrMsgs, recAddrV6AddrDumpCst, uint16(unix.RTM_GETADDR)),
		},
		{
			// filter.tb = 0 for `table all`, so iproute_dump_filter adds no
			// RTA_TABLE, and dump_family is left AF_UNSPEC (ip/iproute.c:1998
			// promotes it to AF_INET only when a table filter is set).
			description: "positive: ip route show table all (all-zero rtmsg, no attrs)",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_UNSPEC, unix.RT_TABLE_UNSPEC, 0, 0)),
			want:        capturedRequest(t, routeMsgs, recRouteShowAllCst, uint16(unix.RTM_GETROUTE)),
		},
		{
			description: "positive: ll_link_get single-get by index (AF_UNSPEC, ifi_index 2, EXT_MASK 0x09)",
			got:         mustBuildReq(BuildGetLinkByIndexRequest(unix.AF_UNSPEC, 2, extMask, 0)),
			want:        capturedRequest(t, routeMsgs, recRouteSingleGetCst, uint16(unix.RTM_GETLINK)),
		},

		{
			// lib/libnetlink.c rtnl_neighdump_req. This row used to be a
			// structural boundary case, because no RTM_GETNEIGH capture was
			// committed; 7_1_4/dumps/netlink_route_getneigh.pcap is, so it is
			// a positive against iproute2's own bytes now.
			description: "positive: ip neigh show dump (ndmsg, AF_UNSPEC)",
			got:         BuildDumpNeighRequest(unix.AF_UNSPEC, 0),
			want:        capturedRequest(t, neighMsgs, recNeighDumpCst, uint16(unix.RTM_GETNEIGH)),
		},
		{
			// lib/ll_map.c ll_init_map, the dump `ip neigh show` issues before
			// its own, via rtnl_linkdump_req_filter — whose attribute path is
			// AF_UNSPEC|AF_BRIDGE rather than the _fn variant's
			// AF_UNSPEC|AF_PACKET. The same 40 bytes as `ip link show` but
			// ifi_family 0, which is the whole reason it needs its own row.
			//
			// The mask is RTEXT_FILTER_VF ALONE, 0x01, not the 0x09 every
			// other `show` dump carries. The structural row this replaced
			// asserted 0x09 and was wrong: at the pinned iproute2 7.1.0,
			// ll_init_map passes only RTEXT_FILTER_VF. Commit 7bd7f335
			// ("ll_map: add RTEXT_FILTER_SKIP_STATS to ll_init_map()", 28 Apr
			// 2026) is what makes it 0x09, and it is not in 7.1.0 — so this
			// locus is a second version-skew fault line alongside de91e928's
			// RTEXT_FILTER_NAME_ONLY, and a nixpkgs bump moves it.
			description: "positive: ll_init_map link dump (AF_UNSPEC, EXT_MASK 0x01)",
			got:         mustBuildReq(BuildDumpLinkRequestExt(unix.AF_UNSPEC, RTEXT_FILTER_VF, 0)),
			want:        capturedRequest(t, neighMsgs, recNeighLinkDumpCst, uint16(unix.RTM_GETLINK)),
		},

		{
			// ip/iproute.c:1836 (filter.tb = RT_TABLE_MAIN) + :1998 (AF_UNSPEC
			// promoted to AF_INET when a table filter is set). Differs from the
			// `table all` row above in family AND in carrying the attribute.
			// Also formerly structural: the gated-topology capture supplies the
			// bytes now.
			description: "positive: default ip route show (AF_INET + RTA_TABLE 254)",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET, unix.RT_TABLE_MAIN, 0, 0)),
			want:        capturedRequest(t, gatedRouteMsgs, recGatedRouteDumpCst, uint16(unix.RTM_GETROUTE)),
		},
		{
			// The same request with rtm_family AF_INET6, which is the one byte
			// `-6` changes. Asserted separately because a builder that ignored
			// its family argument would still pass the row above.
			description: "positive: ip -6 route show (AF_INET6 + RTA_TABLE 254)",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET6, unix.RT_TABLE_MAIN, 0, 0)),
			want:        capturedRequest(t, gatedRoute6Msgs, recGatedRoute6DumpCst, uint16(unix.RTM_GETROUTE)),
		},
		{
			// The FIRST of the two requests `ip link show dev goip0` sends:
			// ll_name_to_index's cache miss, resolving the name to an index it
			// then throws the rest of the reply away for (ip/ipaddress.c:2254,
			// lib/ll_map.c:264-305).
			description: "positive: ip link show dev, first get — ll_link_get (AF_UNSPEC, ext-mask first)",
			got:         mustBuildReq(BuildGetLinkByNameRequest(unix.AF_UNSPEC, "goip0", extMask, 0)),
			want:        capturedRequest(t, linkDevMsgs, recLinkDevNameGetCst, uint16(unix.RTM_GETLINK)),
		},
		{
			// The SECOND, and the one whose reply print_linkinfo renders. Same
			// command, same interface, microseconds later — and different
			// bytes, which is the whole reason BuildIplinkGetRequest exists
			// rather than a second call to the builder above.
			description: "positive: ip link show dev, second get — iplink_get (AF_PACKET, name first)",
			got:         mustBuildReq(BuildIplinkGetRequest(unix.AF_PACKET, "goip0", extMask, 0)),
			want:        capturedRequest(t, linkDevMsgs, recLinkDevPrintGetCst, uint16(unix.RTM_GETLINK)),
		},

		// boundary — structural, no capture yet; each names its source
		{
			// The unfiltered form must be bit-identical to BuildDumpAddrRequest,
			// so the two builders cannot disagree about the same request.
			description: "boundary: BuildDumpAddrRequestIndex with ifindex 0 equals BuildDumpAddrRequest",
			got:         BuildDumpAddrRequestIndex(unix.AF_INET, 0, testSeq),
			want:        BuildDumpAddrRequest(unix.AF_INET, testSeq),
		},
		{
			// Same contract on the neighbor side: `ip neigh show` and
			// `ip neigh show dev NAME` share a builder, so the unfiltered
			// form must be indistinguishable from the pre-filter one.
			description: "boundary: BuildDumpNeighRequestFilter with ifindex 0 equals BuildDumpNeighRequest",
			got:         mustBuildReq(BuildDumpNeighRequestFilter(unix.AF_UNSPEC, 0, testSeq)),
			want:        BuildDumpNeighRequest(unix.AF_UNSPEC, testSeq),
		},
		{
			// The trap, asserted rather than described. ndmsgHdr's third
			// argument is ndm_ifindex and it stays 0 here while the index
			// travels as NDA_IFINDEX — the opposite of the ifaddrmsg row
			// above, where the index IS the header field. ipneigh_dump_filter
			// (ip/ipneigh.c:493) is the reason.
			description: "boundary: BuildDumpNeighRequestFilter sends the index as NDA_IFINDEX, leaving ndm_ifindex zero",
			got:         mustBuildReq(BuildDumpNeighRequestFilter(unix.AF_UNSPEC, 3, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETNEIGH), wantDumpFlags, testSeq,
				concat(ndmsgHdr(unix.AF_UNSPEC, 0, 0, 0, 0),
					rtattr(uint16(unix.NDA_IFINDEX), le32(3)))),
		},
		{
			// The family byte is the one thing `-4`/`-6` change on this
			// request, and a builder that ignored its family argument would
			// still pass the row above.
			description: "corner: BuildDumpNeighRequestFilter keeps ndm_family alongside the filter",
			got:         mustBuildReq(BuildDumpNeighRequestFilter(unix.AF_INET6, 3, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETNEIGH), wantDumpFlags, testSeq,
				concat(ndmsgHdr(unix.AF_INET6, 0, 0, 0, 0),
					rtattr(uint16(unix.NDA_IFINDEX), le32(3)))),
		},
		{
			// ifa_index lives in the HEADER, at offset 4 of the ifaddrmsg. There
			// is no attribute for it, and the kernel honors it only on a socket
			// with NETLINK_GET_STRICT_CHK set.
			description: "boundary: BuildDumpAddrRequestIndex writes ifa_index into the request header",
			got:         BuildDumpAddrRequestIndex(unix.AF_INET, 2, testSeq),
			want: nlmsg(uint16(unix.RTM_GETADDR), wantDumpFlags, testSeq,
				ifaddrmsgHdr(unix.AF_INET, 0, 0, 0, 2)),
		},
		{
			description: "boundary: a 15-character name is the longest IFNAMSIZ allows",
			got:         mustBuildReq(BuildGetLinkByNameRequest(unix.AF_UNSPEC, "abcdefghijklmno", 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_UNSPEC, 0, 0, 0),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("abcdefghijklmno\x00")))),
		},
		{
			// Structural: the captured `ip link show dev goip0` covers the
			// normal mask, but no capture exists of `ip -s novf link show dev
			// lo`, the only invocation that reaches iplink_get with a zero one.
			// The row is here because a zero mask is exactly where the two
			// by-name builders part company.
			//
			// ll_link_get's addattr32 sits behind `if (filt_mask)`
			// (lib/ll_map.c:289), so BuildGetLinkByNameRequest omits the attribute;
			// iplink_get calls addattr32 unconditionally (ip/iplink.c:1514), so a
			// four-byte zero attribute goes on the wire. "No attribute" and "an
			// attribute whose value is zero" are different bytes, and pkg/nlparity
			// compares bytes.
			description: "boundary: iplink_get with a zero mask still emits IFLA_EXT_MASK, unlike ll_link_get",
			got:         mustBuildReq(BuildIplinkGetRequest(unix.AF_PACKET, "lo", 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_PACKET, 0, 0, 0),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")),
					rtattr(uint16(unix.IFLA_EXT_MASK), le32(0)))),
		},

		// corner
		{
			// ll_link_get adds IFLA_EXT_MASK first and IFLA_IFNAME second
			// (lib/ll_map.c:289-293). pkg/nlparity compares requests for full byte
			// equality, so the order is a divergence, not a detail.
			description: "corner: by-name single get emits IFLA_EXT_MASK before IFLA_IFNAME",
			got:         mustBuildReq(BuildGetLinkByNameRequest(unix.AF_UNSPEC, "lo", extMask, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_UNSPEC, 0, 0, 0),
					rtattr(uint16(unix.IFLA_EXT_MASK), le32(extMask)),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")))),
		},
		{
			description: "corner: by-name single get with extMask 0 emits IFLA_IFNAME alone",
			got:         mustBuildReq(BuildGetLinkByNameRequest(unix.AF_UNSPEC, "lo", 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_UNSPEC, 0, 0, 0),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")))),
		},
		{
			// The positive rows above prove this ordering against the capture,
			// for "goip0". This row restates it for "lo", whose 3-byte padded
			// payload puts the second attribute at a different offset — so a
			// builder that hard-coded where the mask goes would pass there and
			// fail here. iplink_get adds IFLA_IFNAME at ip/iplink.c:1513 and
			// IFLA_EXT_MASK at :1514; ll_link_get adds them the other way
			// round, and the row two above says so for the same name.
			description: "corner: iplink_get emits IFLA_IFNAME before IFLA_EXT_MASK, reversing ll_link_get",
			got:         mustBuildReq(BuildIplinkGetRequest(unix.AF_PACKET, "lo", extMask, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_PACKET, 0, 0, 0),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")),
					rtattr(uint16(unix.IFLA_EXT_MASK), le32(extMask)))),
		},
		{
			// ifi_family is `preferred_family`, not a constant (ip/iplink.c:1502),
			// so the builder must store what it is handed. `ip link show dev lo`
			// always reaches it as AF_PACKET — ipaddr_list_link assigns that at
			// ip/ipaddress.c:2416, before argument parsing, which is also why
			// `ip -4 link show dev lo` sends AF_PACKET and not AF_INET. This row
			// passes AF_INET precisely because no real invocation does: a builder
			// that hard-coded AF_PACKET would satisfy every other row here.
			description: "corner: iplink_get stores the family it is given, not a hard-coded AF_PACKET",
			got:         mustBuildReq(BuildIplinkGetRequest(unix.AF_INET, "lo", extMask, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_INET, 0, 0, 0),
					rtattr(uint16(unix.IFLA_IFNAME), []byte("lo\x00")),
					rtattr(uint16(unix.IFLA_EXT_MASK), le32(extMask)))),
		},
		{
			// rtm_table is one byte and cannot hold this; RTA_TABLE is the u32
			// that supersedes it, which is the whole reason the attribute exists.
			description: "corner: a table id above 255 fits RTA_TABLE but not rtm_table",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET, 0xdeadbeef, 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_TABLE), le32(0xdeadbeef)))),
		},
		{
			// `ip route show dev NAME`. iproute_dump_filter writes RTA_TABLE
			// at ip/iproute.c:1726 and RTA_OIF at :1731, in that order and
			// each behind its own presence test, so this row asserts the
			// ORDER as much as the contents — nlparity compares requests for
			// full byte equality and the reversed pair is a divergence.
			description: "positive: ip route show dev NAME (RTA_TABLE then RTA_OIF)",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET, unix.RT_TABLE_MAIN, 3, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_TABLE), le32(unix.RT_TABLE_MAIN)),
					rtattr(uint16(unix.RTA_OIF), le32(3)))),
		},
		{
			// `ip route show table all dev NAME`. The two presence tests are
			// independent, so dropping the table does not drop the device —
			// and the family stays AF_UNSPEC because ip/iproute.c:1998
			// promotes on the TABLE alone and knows nothing about `dev`.
			description: "boundary: table all with a device filter carries RTA_OIF alone",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_UNSPEC, unix.RT_TABLE_UNSPEC, 3, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_UNSPEC, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_OIF), le32(3)))),
		},
		{
			// No interface has index 0, so `if (filter.oif)` (:1731) can use
			// the value as its own presence flag — and so can this builder.
			// Spelled out by hand rather than compared against the captured
			// `ip route show` row above, because what needs asserting is that
			// oif 0 appends NOTHING: an empty RTA_OIF, or a zero-valued one,
			// would still satisfy a length check and would still be a
			// divergence.
			description: "corner: oif 0 appends no attribute at all",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET, unix.RT_TABLE_MAIN, 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_TABLE), le32(unix.RT_TABLE_MAIN)))),
		},
		{
			// filter.oif is an `int` in C and an ifindex is signed in the
			// kernel, so the top bit is reachable only by a host that has
			// churned through two billion interfaces — but the attribute is a
			// u32 on the wire either way, and a builder that narrowed it to
			// int16 somewhere would show up here and nowhere else.
			description: "boundary: a 32-bit ifindex round-trips little-endian in RTA_OIF",
			got:         mustBuildReq(BuildDumpRouteRequestFilter(unix.AF_INET, unix.RT_TABLE_UNSPEC, 0xfeedface, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_OIF), le32(0xfeedface)))),
		},
		{
			// A negative index is how the kernel spells "unset" in some replies;
			// the builder stores whatever int32 it is given, two's complement, so
			// a caller cannot get a truncated index without noticing.
			description: "corner: ifi_index -1 round-trips as two's complement",
			got:         mustBuildReq(BuildGetLinkByIndexRequest(unix.AF_UNSPEC, -1, 0, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantGetFlags, testSeq,
				ifinfomsgHdr(unix.AF_UNSPEC, 0, -1, 0)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if !bytes.Equal(tc.got, tc.want) {
				t.Fatalf("request = % x\n     want = % x", tc.got, tc.want)
			}
			if n := binary.LittleEndian.Uint32(tc.got[0:4]); int(n) != len(tc.got) {
				t.Errorf("nlmsg_len = %d, want %d", n, len(tc.got))
			}
		})
	}

	// Oversend, asserted separately because it is a property of the DATAGRAM and
	// not of the message the builders emit. pkg/nlparity reports these
	// informationally; they are never gated, but they do have to be right.
	dgrams := []struct {
		description string
		got         int
		want        int
	}{
		{"positive: ip link show sends nlmsg_len, no tail", linkDgrams[recGetLinkShowCst], dgramGetLinkShowCst},
		{"positive: the v4 addr dump has a 128-byte zeroed tail", addrDgrams[recAddrV4AddrDumpCst], dgramAddrDumpCst},
		{"positive: the v6 addr dump has a 128-byte zeroed tail", addrDgrams[recAddrV6AddrDumpCst], dgramAddrDumpCst},
		{"positive: the route dump has a 128-byte zeroed tail", routeDgrams[recRouteShowAllCst], dgramRouteShowAllCst},
		{"positive: the single get sends nlmsg_len, no tail", routeDgrams[recRouteSingleGetCst], dgramSingleGetCst},
		// Both by-name gets go through rtnl_talk, so neither oversends — and
		// both are the same 52 bytes, which is the point. Length is the one
		// thing about these two requests that is identical, so a harness that
		// compared sizes would call them the same request.
		{"positive: the first by-name get sends nlmsg_len, no tail", linkDevDgrams[recLinkDevNameGetCst], dgramByNameGetCst},
		{"positive: the second by-name get is the same size as the first", linkDevDgrams[recLinkDevPrintGetCst], dgramByNameGetCst},
	}
	for _, tc := range dgrams {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("datagram length = %d, want %d", tc.got, tc.want)
			}
		})
	}
}

// TestBuildGetLinkByIndexRequestAllCaptured reproduces every ll_link_get
// single-get in the route capture — ten of them, one per interface the route
// dump had to name.
//
// The index is read out of the capture rather than hardcoded, so this also
// proves the builder writes ifi_index at the right offset: a builder that put
// it anywhere else would reproduce at most one record, the one with index 0.
//
// go test ./pkg/xtcpnl/ -run TestBuildGetLinkByIndexRequestAllCaptured
func TestBuildGetLinkByIndexRequestAllCaptured(t *testing.T) {
	msgs, _ := capturedRequests(t, tdRouteBulkGetRoute_7_1_8)

	type row struct {
		description string
		captured    []byte
		family      uint8
		ifindex     int32
		extMask     uint32
	}

	var tests []row
	for rec := 0; rec < 64; rec++ {
		msg, ok := msgs[rec]
		if !ok {
			continue
		}
		if binary.LittleEndian.Uint16(msg[4:6]) != uint16(unix.RTM_GETLINK) {
			continue
		}
		if binary.LittleEndian.Uint16(msg[6:8]) != uint16(unix.NLM_F_REQUEST) {
			continue // a dump, not a single get
		}
		hdr := msg[NlMsgHdrSizeCst:]
		idx := int32(binary.LittleEndian.Uint32(hdr[4:8]))
		var mask uint32
		if err := WalkRTAttrs(hdr[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
			if atype == uint16(unix.IFLA_EXT_MASK) && len(val) >= 4 {
				mask = binary.LittleEndian.Uint32(val)
			}
		}); err != nil {
			t.Fatalf("record %d: WalkRTAttrs: %v", rec, err)
		}
		tests = append(tests, row{
			description: "positive: captured single get, record " + strconv.Itoa(rec),
			captured:    msg,
			family:      hdr[0],
			ifindex:     idx,
			extMask:     mask,
		})
	}

	// The corpus has exactly ten; fewer means the fixture changed and the
	// assertion below has quietly stopped covering what it claims to.
	if len(tests) != 10 {
		t.Fatalf("found %d single-get requests, want 10 (fixture regenerated?)", len(tests))
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildGetLinkByIndexRequest(tc.family, tc.ifindex, tc.extMask, 0)
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if !bytes.Equal(got, tc.captured) {
				t.Fatalf("ifindex %d: request = % x\n              want = % x", tc.ifindex, got, tc.captured)
			}
		})
	}
}

// TestByNameGetsAreNotInterchangeable pins the differences between the two
// by-name single-gets `ip link show dev NAME` sends, field by field.
//
// The rows above assert each builder against its own expected bytes, which is
// necessary but does not say the interesting thing: that one builder cannot
// stand in for the other. The tempting simplification here is real — the
// command asks the kernel about the same interface twice in a row, with the
// same two attributes and the same mask — and it is wrong, in three places.
// Each gets a row, so a future collapse into one builder fails with a name for
// what it broke rather than a wall of hex.
//
// ll_link_get is lib/ll_map.c:264-305; iplink_get is ip/iplink.c:1497-1515.
//
// go test ./pkg/xtcpnl/ -run TestByNameGetsAreNotInterchangeable
func TestByNameGetsAreNotInterchangeable(t *testing.T) {
	const (
		devName = "lo"
		extMask = RTEXT_FILTER_VF | RTEXT_FILTER_SKIP_STATS // 0x09 at 7.1.0
		seq     = 7
	)

	// Field offsets into the request. The nlmsghdr is 16 bytes and the
	// ifinfomsg 16 more, so the first attribute starts at 32; both attributes
	// here are 8 bytes once padded (4 + 4 for the u32 mask, 4 + 3 + 1 for
	// "lo\0"), which is what lets the swap row below compare one request's
	// tail against the other's head.
	const (
		offFamilyCst   = NlMsgHdrSizeCst                    // ifi_family
		offPadCst      = offFamilyCst + 1                   // __ifi_pad onward
		offAttrsCst    = NlMsgHdrSizeCst + IfInfomsgSizeCst // first attribute
		offAttrTypeCst = offAttrsCst + 2                    // its nla_type
		attrLenCst     = 8                                  // both, once padded
		offAttr2Cst    = offAttrsCst + attrLenCst           // second attribute
		offAttr2EndCst = offAttr2Cst + attrLenCst           // end of request
	)

	llLinkGet := mustBuildReq(BuildGetLinkByNameRequest(unix.AF_UNSPEC, devName, extMask, seq))
	iplinkGet := mustBuildReq(BuildIplinkGetRequest(unix.AF_PACKET, devName, extMask, seq))

	tests := []struct {
		description string
		// field slices the region under test out of ll_link_get's request.
		field func([]byte) []byte
		// against slices the region compared against it out of iplink_get's
		// request. Nil means the same region, which is what every row but the
		// swap wants.
		against   func([]byte) []byte
		wantEqual bool
	}{
		{
			// The headline: the command sends two requests because two requests
			// are needed, not because it forgot it had already asked.
			description: "negative: the whole request differs, so neither can be sent twice",
			field:       func(r []byte) []byte { return r },
			wantEqual:   false,
		},
		{
			description: "positive: both are RTM_GETLINK with NLM_F_REQUEST and no NLM_F_DUMP",
			field:       func(r []byte) []byte { return r[4:8] },
			wantEqual:   true,
		},
		{
			// Same attributes, same sizes — so a length check is exactly the
			// kind of assertion that would call these two requests identical.
			description: "boundary: nlmsg_len is the same, so length alone cannot tell them apart",
			field:       func(r []byte) []byte { return r[0:4] },
			wantEqual:   true,
		},
		{
			// ll_link_get zero-initializes its ifinfomsg; iplink_get assigns
			// preferred_family, which ipaddr_list_link forced to AF_PACKET at
			// ip/ipaddress.c:2416.
			description: "negative: ifi_family is AF_UNSPEC in one and AF_PACKET in the other",
			field:       func(r []byte) []byte { return r[offFamilyCst : offFamilyCst+1] },
			wantEqual:   false,
		},
		{
			// Everything else in the header is zero in both: the name is the
			// selector, so ifi_index stays 0 and no flag is set.
			description: "positive: the rest of the ifinfomsg is zero in both — the name is the selector",
			field:       func(r []byte) []byte { return r[offPadCst:offAttrsCst] },
			wantEqual:   true,
		},
		{
			description: "negative: the leading attribute is IFLA_EXT_MASK in one and IFLA_IFNAME in the other",
			field:       func(r []byte) []byte { return r[offAttrTypeCst : offAttrTypeCst+2] },
			wantEqual:   false,
		},
		{
			// The swap, stated as an equality: ll_link_get's second attribute is
			// byte-for-byte iplink_get's first. This is what "the same two
			// attributes in the opposite order" means, and it is the row that
			// would catch a builder that reordered the pair by dropping one
			// attribute and duplicating the other.
			description: "corner: ll_link_get's trailing attribute is iplink_get's leading one",
			field:       func(r []byte) []byte { return r[offAttr2Cst:offAttr2EndCst] },
			against:     func(r []byte) []byte { return r[offAttrsCst:offAttr2Cst] },
			wantEqual:   true,
		},
		{
			description: "corner: and ll_link_get's leading attribute is iplink_get's trailing one",
			field:       func(r []byte) []byte { return r[offAttrsCst:offAttr2Cst] },
			against:     func(r []byte) []byte { return r[offAttr2Cst:offAttr2EndCst] },
			wantEqual:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			against := tc.against
			if against == nil {
				against = tc.field
			}
			got, want := tc.field(llLinkGet), against(iplinkGet)
			if bytes.Equal(got, want) != tc.wantEqual {
				verb := "differ from"
				if tc.wantEqual {
					verb = "equal"
				}
				t.Fatalf("ll_link_get % x should %s iplink_get % x", got, verb, want)
			}
		})
	}
}

// TestRequestBuilderErrors covers the fallible paths, which are all name
// validation plus the read-only allowlist inherited from BuildRequest.
//
// go test ./pkg/xtcpnl/ -run TestRequestBuilderErrors
func TestRequestBuilderErrors(t *testing.T) {
	tests := []struct {
		description string
		build       func() ([]byte, error)
		wantErr     error
	}{
		{
			description: "positive: a normal name builds",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "eth0", 0, 1) },
		},
		{
			description: "negative: an empty name is ErrBadIfName",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			// 16 characters plus the NUL does not fit IFNAMSIZ.
			description: "negative: a 16-character name is ErrBadIfName",
			build: func() ([]byte, error) {
				return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "abcdefghijklmnop", 0, 1)
			},
			wantErr: ErrBadIfName,
		},
		{
			description: "negative: a name containing '/' is ErrBadIfName (dev_valid_name)",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "a/b", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "negative: a name containing a space is ErrBadIfName (dev_valid_name)",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "eth 0", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "negative: a name containing a newline is ErrBadIfName",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "eth\n0", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "boundary: \".\" is rejected, as the kernel rejects it",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, ".", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "boundary: \"..\" is rejected, as the kernel rejects it",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "..", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "boundary: a 15-character name is accepted",
			build: func() ([]byte, error) {
				return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "abcdefghijklmno", 0, 1)
			},
		},
		{
			description: "corner: a name of only a dot-prefixed word is fine",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, ".hidden", 0, 1) },
		},
		{
			// dev_valid_name rejects ':' alongside '/' (net/core/dev.c:1335).
			// It is excluded because `ip` uses "dev:label" syntax for address
			// labels, so a colon in a device name makes an argument ambiguous.
			description: "negative: a name containing ':' is ErrBadIfName (dev_valid_name rejects it too)",
			build:       func() ([]byte, error) { return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "eth0:1", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			// **The one rule the kernel cannot express.** dev_valid_name's
			// validation loop is `while (*name)`, so it stops at the first NUL
			// and never sees anything after it — a C string has no way to carry
			// one. A Go string does, and the consequence is not cosmetic:
			// PutString appends its own terminator, so "lo\x00extra" would go on
			// the wire as `lo\0extra\0` and the kernel would read the name as
			// "lo". The request would succeed and answer about a DIFFERENT
			// interface than the caller named, which is strictly worse than an
			// error. iproute2 never meets this case because its names come from
			// argv and execve cannot pass an embedded NUL.
			description: "corner: a name containing an embedded NUL is ErrBadIfName, not silently truncated to \"lo\"",
			build: func() ([]byte, error) {
				return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "lo\x00extra", 0, 1)
			},
			wantErr: ErrBadIfName,
		},
		{
			// A trailing NUL is the same hazard with a friendlier-looking
			// input: the caller has already terminated the string, PutString
			// would add a second terminator, and the attribute payload would be
			// one byte longer than `ip`'s for the same interface — a byte-level
			// request divergence that no output comparison could see.
			description: "corner: a name the caller already NUL-terminated is ErrBadIfName",
			build: func() ([]byte, error) {
				return BuildGetLinkByNameRequest(unix.AF_UNSPEC, "lo\x00", 0, 1)
			},
			wantErr: ErrBadIfName,
		},

		// The second by-name builder validates independently. These rows exist
		// because the two share no code — BuildIplinkGetRequest calls validIfName
		// itself — so a rewrite that dropped the call from one of them would keep
		// every row above green while putting an unvalidated name on the wire.
		{
			description: "positive: iplink_get accepts a normal name",
			build:       func() ([]byte, error) { return BuildIplinkGetRequest(unix.AF_PACKET, "eth0", 0, 1) },
		},
		{
			description: "negative: iplink_get rejects an empty name with ErrBadIfName",
			build:       func() ([]byte, error) { return BuildIplinkGetRequest(unix.AF_PACKET, "", 0, 1) },
			wantErr:     ErrBadIfName,
		},
		{
			description: "boundary: iplink_get accepts a 15-character name",
			build: func() ([]byte, error) {
				return BuildIplinkGetRequest(unix.AF_PACKET, "abcdefghijklmno", 0, 1)
			},
			wantErr: nil,
		},
		{
			description: "negative: iplink_get rejects a 16-character name with ErrBadIfName",
			build: func() ([]byte, error) {
				return BuildIplinkGetRequest(unix.AF_PACKET, "abcdefghijklmnop", 0, 1)
			},
			wantErr: ErrBadIfName,
		},
		{
			description: "corner: iplink_get rejects an embedded NUL rather than asking about \"lo\"",
			build: func() ([]byte, error) {
				return BuildIplinkGetRequest(unix.AF_PACKET, "lo\x00extra", 0, 1)
			},
			wantErr: ErrBadIfName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := tc.build()

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Errorf("request = % x, want nil on rejection", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if len(got) < NlMsgHdrSizeCst+IfInfomsgSizeCst {
				t.Fatalf("request is %d bytes, too short to hold an ifinfomsg", len(got))
			}
		})
	}
}

// ---- TalkRtnetlink -----------------------------------------------------------

// seqpacketPairTimeout is seqpacketPair with a caller-chosen receive timeout,
// so the rows that deliberately wait for a timeout cost 300 ms rather than the
// shared harness's 2 s.
func seqpacketPairTimeout(t *testing.T, tv unix.Timeval) [2]int {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("Socketpair: %v", err)
	}
	if err := unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		t.Fatalf("SO_RCVTIMEO: %v", err)
	}
	t.Cleanup(func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	})
	return [2]int{fds[0], fds[1]}
}

// Single-get reply fixtures: no NLM_F_MULTI, and no NLMSG_DONE follows.
var (
	singleLink  = nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, []byte("single-get-reply"))
	bareLink    = nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, nil)
	otherLink   = nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, []byte("second-reply"))
	staleSingle = nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq+7, []byte("someone-elses"))
	enodevMsg   = nlmsg(uint16(unix.NLMSG_ERROR), 0, testSeq, errnoBody(syscall.ENODEV))
)

// TestTalkRtnetlink is §8.3 of the goip plan, driven over the AF_UNIX
// SOCK_SEQPACKET harness (each Write is one datagram, exactly like a netlink
// recv). It works because TalkRtnetlink accepts sa == nil and fromKernel
// tolerates a non-netlink peer.
//
// The first row is the case DumpRtnetlink cannot handle;
// TestDumpRtnetlinkCannotDriveASingleGet proves that claim rather than
// asserting it in a comment.
//
// go test ./pkg/xtcpnl/ -run TestTalkRtnetlink$
func TestTalkRtnetlink(t *testing.T) {
	request := mustBuildReq(BuildGetLinkByIndexRequest(unix.AF_UNSPEC, 2,
		RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS, testSeq))

	tests := []struct {
		description string
		request     []byte
		replies     [][]byte // datagrams the fake kernel writes after reading the request
		closeAfter  bool     // fake kernel closes its end after writing (EOF)
		wantSent    bool
		wantType    uint16
		wantBody    string
		wantErr     error
		wantQueued  string // a reply expected to be left unread on the socket
	}{
		// positive
		{
			description: "positive: one RTM_NEWLINK with no MULTI and no DONE returns the reply",
			request:     request, replies: [][]byte{singleLink}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "single-get-reply",
		},
		{
			description: "positive: NLMSG_ERROR with errno 0 is an ACK, not an error",
			request:     request, replies: [][]byte{ackMsg}, wantSent: true,
		},
		{
			description: "positive: a stale-seq datagram is skipped, the next one answers",
			request:     request, replies: [][]byte{staleSingle, singleLink}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "single-get-reply",
		},

		// negative
		{
			description: "negative: NLMSG_ERROR with ENODEV surfaces as a wrapped errno",
			request:     request, replies: [][]byte{enodevMsg}, wantSent: true,
			wantErr: syscall.ENODEV,
		},
		{
			description: "negative: a reply carrying someone else's seq times out rather than being returned",
			request:     request, replies: [][]byte{staleSingle}, wantSent: true,
			wantErr: unix.EAGAIN,
		},
		{
			description: "negative: no reply at all times out",
			request:     request, replies: nil, wantSent: true,
			wantErr: unix.EAGAIN,
		},
		{
			description: "negative: the peer closing without replying surfaces as ErrShortRecv",
			request:     request, replies: nil, closeAfter: true, wantSent: true,
			wantErr: ErrShortRecv,
		},

		// boundary
		{
			description: "boundary: a reply of exactly one nlmsghdr yields an empty body and no error",
			request:     request, replies: [][]byte{bareLink}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "",
		},
		{
			description: "boundary: a request shorter than an nlmsghdr is ErrShortRequest, nothing sent",
			request:     request[:NlMsgHdrSizeCst-1], wantErr: ErrShortRequest,
		},
		{
			// BuildDumpRequest returns nil for a message type outside the
			// read-only allowlist; this is where that nil is caught.
			description: "boundary: a nil request (a rejected write type) is ErrShortRequest",
			request:     BuildDumpRequest(uint16(unix.RTM_NEWLINK), testSeq, make([]byte, IfInfomsgSizeCst)),
			wantErr:     ErrShortRequest,
		},

		// corner
		{
			description: "corner: NLMSG_DONE arriving first ends the exchange with no body and no error",
			request:     request, replies: [][]byte{doneMsg}, wantSent: true,
		},
		{
			description: "corner: NLMSG_NOOP is skipped and the reply behind it is returned",
			request:     request, replies: [][]byte{stream(noopMsg, singleLink)}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "single-get-reply",
		},
		{
			// Two datagrams, one consumed: the second is still on the socket. This
			// is the documented behavior, and it is why TalkRtnetlink must not be
			// pointed at a dump.
			description: "corner: a second reply datagram is left queued on the socket",
			request:     request, replies: [][]byte{singleLink, otherLink}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "single-get-reply",
			wantQueued: "second-reply",
		},
		{
			// Two messages in ONE datagram: the first is returned and the rest of
			// the datagram is discarded, not queued — there is nothing left to
			// recv.
			description: "corner: a second message in the same datagram is discarded, not queued",
			request:     request, replies: [][]byte{stream(singleLink, otherLink)}, wantSent: true,
			wantType: uint16(unix.RTM_NEWLINK), wantBody: "single-get-reply",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fds := seqpacketPairTimeout(t, unix.Timeval{Usec: 300_000})

			sentCh := make(chan []byte, 1)
			go func() {
				defer func() {
					if tc.closeAfter {
						_ = unix.Close(fds[1])
					}
				}()
				if !tc.wantSent {
					return
				}
				rb := make([]byte, 512)
				n, _, err := unix.Recvfrom(fds[1], rb, 0)
				if err != nil {
					sentCh <- nil
					return
				}
				sentCh <- rb[:n]
				for _, d := range tc.replies {
					if _, err := unix.Write(fds[1], d); err != nil {
						return
					}
				}
			}()

			msgType, body, err := TalkRtnetlink(fds[0], tc.request, nil)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want errors.Is(%v)", err, tc.wantErr)
				}
				if msgType != 0 || body != nil {
					t.Errorf("got (%d, %q) alongside the error, want (0, nil)", msgType, body)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if msgType != tc.wantType {
				t.Errorf("msgType = %d, want %d", msgType, tc.wantType)
			}
			if string(body) != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}

			if tc.wantSent {
				select {
				case sent := <-sentCh:
					if !bytes.Equal(sent, tc.request) {
						t.Errorf("peer received % x, want the request % x", sent, tc.request)
					}
				case <-time.After(2 * time.Second):
					t.Error("peer never received the request")
				}
			}

			if tc.wantQueued != "" {
				rb := make([]byte, 512)
				n, _, rerr := unix.Recvfrom(fds[0], rb, 0)
				if rerr != nil {
					t.Fatalf("expected a queued datagram, recv failed: %v", rerr)
				}
				if n < NlMsgHdrSizeCst {
					t.Fatalf("queued datagram is %d bytes, too short", n)
				}
				if got := string(rb[NlMsgHdrSizeCst:n]); got != tc.wantQueued {
					t.Errorf("queued body = %q, want %q", got, tc.wantQueued)
				}
			}
		})
	}
}

// TestDumpRtnetlinkCannotDriveASingleGet is the justification for
// TalkRtnetlink existing, asserted rather than asserted-in-a-comment.
//
// The kernel's answer to a non-dump RTM_GETLINK is one message with no
// NLM_F_MULTI and no NLMSG_DONE. DumpRtnetlink's loop terminates only on DONE
// or NLMSG_ERROR, so it delivers the reply and then recvs until SO_RCVTIMEO.
// TalkRtnetlink returns the same reply immediately.
//
// go test ./pkg/xtcpnl/ -run TestDumpRtnetlinkCannotDriveASingleGet
func TestDumpRtnetlinkCannotDriveASingleGet(t *testing.T) {
	request := mustBuildReq(BuildGetLinkByIndexRequest(unix.AF_UNSPEC, 2, 0, testSeq))

	tests := []struct {
		description string
		talk        bool
		wantErr     error
		wantBody    string
	}{
		{
			description: "negative: DumpRtnetlink waits for a DONE that never comes and times out",
			talk:        false,
			wantErr:     unix.EAGAIN,
			wantBody:    "single-get-reply", // it did deliver the reply first
		},
		{
			description: "positive: TalkRtnetlink returns the same reply immediately",
			talk:        true,
			wantBody:    "single-get-reply",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fds := seqpacketPairTimeout(t, unix.Timeval{Usec: 300_000})

			go func() {
				rb := make([]byte, 512)
				if _, _, err := unix.Recvfrom(fds[1], rb, 0); err != nil {
					return
				}
				_, _ = unix.Write(fds[1], singleLink)
			}()

			var gotBody string
			var err error
			if tc.talk {
				var body []byte
				_, body, err = TalkRtnetlink(fds[0], request, nil)
				gotBody = string(body)
			} else {
				var got []delivered
				err = DumpRtnetlink(fds[0], request, nil, collector(&got, nil))
				if len(got) == 1 {
					gotBody = got[0].body
				}
			}

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want errors.Is(%v)", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if gotBody != tc.wantBody {
				t.Errorf("body = %q, want %q", gotBody, tc.wantBody)
			}
		})
	}
}
