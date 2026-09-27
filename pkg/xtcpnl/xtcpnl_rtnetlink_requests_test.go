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
// Where a form has no capture yet — neigh dump, default `route show`, the
// by-name single get — the row is labeled boundary or corner, says which
// iproute2 function it mirrors, and names the capture Item 7 of the goip plan
// must add. That labeling is the point: a positive row means "iproute2 sent
// exactly these bytes", and nothing else is allowed to claim it.

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
			got:         mustBuildReq(BuildDumpRouteRequestTable(unix.AF_UNSPEC, unix.RT_TABLE_UNSPEC, 0)),
			want:        capturedRequest(t, routeMsgs, recRouteShowAllCst, uint16(unix.RTM_GETROUTE)),
		},
		{
			description: "positive: ll_link_get single-get by index (AF_UNSPEC, ifi_index 2, EXT_MASK 0x09)",
			got:         mustBuildReq(BuildGetLinkByIndexRequest(unix.AF_UNSPEC, 2, extMask, 0)),
			want:        capturedRequest(t, routeMsgs, recRouteSingleGetCst, uint16(unix.RTM_GETLINK)),
		},

		// boundary — structural, no capture yet; each names its source
		{
			// lib/libnetlink.c rtnl_neighdump_req. Blocked on Item 7's
			// `ip neigh show` capture; asserted structurally meanwhile.
			description: "boundary: ip neigh show dump, no fixture yet (lib/libnetlink.c rtnl_neighdump_req)",
			got:         BuildDumpNeighRequest(unix.AF_UNSPEC, testSeq),
			want:        nlmsg(uint16(unix.RTM_GETNEIGH), wantDumpFlags, testSeq, make([]byte, NdMsgSizeCst)),
		},
		{
			// ip/iproute.c:1836 (filter.tb = RT_TABLE_MAIN) + :1998 (AF_UNSPEC
			// promoted to AF_INET when a table filter is set). Differs from the
			// `table all` row above in family AND in carrying the attribute.
			description: "boundary: default ip route show, no fixture yet (AF_INET + RTA_TABLE 254)",
			got:         mustBuildReq(BuildDumpRouteRequestTable(unix.AF_INET, unix.RT_TABLE_MAIN, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_TABLE), le32(unix.RT_TABLE_MAIN)))),
		},
		{
			// lib/ll_map.c ll_init_map: AF_UNSPEC with the same 0x09 mask, via
			// rtnl_linkdump_req_filter, whose attribute path is AF_UNSPEC|AF_BRIDGE
			// rather than the _fn variant's AF_UNSPEC|AF_PACKET. Same 40 bytes as
			// `ip link show` but ifi_family 0. This is the dump `ip neigh show`
			// issues before its own, so it arrives with Item 7's neigh capture.
			description: "boundary: ll_init_map link dump, no fixture yet (AF_UNSPEC, EXT_MASK 0x09)",
			got:         mustBuildReq(BuildDumpLinkRequestExt(unix.AF_UNSPEC, extMask, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETLINK), wantDumpFlags, testSeq,
				concat(ifinfomsgHdr(unix.AF_UNSPEC, 0, 0, 0),
					rtattr(uint16(unix.IFLA_EXT_MASK), le32(extMask)))),
		},
		{
			// The unfiltered form must be bit-identical to BuildDumpAddrRequest,
			// so the two builders cannot disagree about the same request.
			description: "boundary: BuildDumpAddrRequestIndex with ifindex 0 equals BuildDumpAddrRequest",
			got:         BuildDumpAddrRequestIndex(unix.AF_INET, 0, testSeq),
			want:        BuildDumpAddrRequest(unix.AF_INET, testSeq),
		},
		{
			// ifa_index lives in the HEADER, at offset 4 of the ifaddrmsg. There
			// is no attribute for it, and the kernel honours it only on a socket
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
			// rtm_table is one byte and cannot hold this; RTA_TABLE is the u32
			// that supersedes it, which is the whole reason the attribute exists.
			description: "corner: a table id above 255 fits RTA_TABLE but not rtm_table",
			got:         mustBuildReq(BuildDumpRouteRequestTable(unix.AF_INET, 0xdeadbeef, testSeq)),
			want: nlmsg(uint16(unix.RTM_GETROUTE), wantDumpFlags, testSeq,
				concat(rtmsgHdr(unix.AF_INET, 0, 0, 0, 0, 0, 0, 0),
					rtattr(uint16(unix.RTA_TABLE), le32(0xdeadbeef)))),
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
			// is the documented behaviour, and it is why TalkRtnetlink must not be
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
