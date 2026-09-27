package nlparity

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// RTEXT_FILTER_VF and RTEXT_FILTER_SKIP_STATS are absent from
// golang.org/x/sys/unix v0.47.0 (verified), while IFLA_EXT_MASK is present.
// Declared here with a kernel citation, as pkg/xtcpnl does for RtaNhID.
//
// linux/include/uapi/linux/rtnetlink.h:835,838
const (
	rtextFilterVFCst        = 1 << 0
	rtextFilterSkipStatsCst = 1 << 3
)

// TestGoldenLinkShowRequest pins the bytes iproute2 puts on the wire for
// `ip link show`, read out of a real nlmon capture.
//
// This is the whole point of the package stated as one test. Until goip exists
// there is nothing to compare against, but the expectation itself is the
// deliverable: it is the first executable statement in this repo of what a
// working `ip link show` request looks like, and every request builder added to
// pkg/xtcpnl later has to reproduce it.
//
// Two of the assertions below are load-bearing in ways that are easy to miss:
//
//   - IFLA_EXT_MASK changes the REPLY, not just the request. RTEXT_FILTER_SKIP_STATS
//     is why IFLA_STATS (7) and IFLA_STATS64 (23) are absent from every reply in
//     the committed dump fixture. A goip that omits the mask gets both appended
//     by the kernel and diverges on the most volatile attribute in the protocol,
//     so the encoder is not deferrable to "later".
//   - nlmsg_pid is 0 on the request. The kernel fills the peer port id only on
//     the way back, so pid is how a request is told from a reply — not the
//     capture direction, which nlmon reports as PACKET_OUTGOING for both because
//     AF_PACKET's dev_queue_xmit_nit overwrites it.
//
// There is no ext-mask version skew to worry about: filter.vfinfo = 1 is
// unconditional at ip/ipaddress.c:2153, and iplink_filter_req is
// `if (filter.vfinfo) |= VF; if (!show_stats) |= SKIP_STATS`, so 0x09 is what
// both iproute2 7.1.0 and 7.2.0 emit for a bare show.
//
// go test ./pkg/nlparity/ -run TestGoldenLinkShowRequest
func TestGoldenLinkShowRequest(t *testing.T) {
	c, err := ParseRouteCaptureFile(tdBulkGetLink)
	if err != nil {
		t.Fatalf("parse capture: %v", err)
	}

	reqs := c.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d, want exactly 1; the capture is polluted and the "+
			"expectations below no longer describe a single command", len(reqs))
	}
	req := reqs[0].Canonical()

	// --- the header --------------------------------------------------------

	wantFlags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ROOT | unix.NLM_F_MATCH)
	hdrChecks := []struct {
		description string
		got         uint64
		want        uint64
	}{
		{"nlmsg_len is 40: a 16-byte header, a 16-byte ifinfomsg, one 8-byte attribute",
			uint64(req.Hdr.Len), 40},
		{"nlmsg_type is RTM_GETLINK", uint64(req.Hdr.Type), uint64(unix.RTM_GETLINK)},
		{"nlmsg_flags is REQUEST|ROOT|MATCH, which is REQUEST|DUMP spelled out",
			uint64(req.Hdr.Flags), uint64(wantFlags)},
		{"nlmsg_seq is zeroed by Canonical, because iproute2 seeds it from time(NULL)",
			uint64(req.Hdr.Seq), 0},
		{"nlmsg_pid is 0 on a request, which is how direction is determined",
			uint64(req.Hdr.Pid), 0},
	}
	for _, hc := range hdrChecks {
		t.Run(hc.description, func(t *testing.T) {
			if hc.got != hc.want {
				t.Errorf("got %d (0x%04x), want %d (0x%04x)", hc.got, hc.got, hc.want, hc.want)
			}
		})
	}

	// NLM_F_DUMP is the composite iproute2 does not use by name. Assert the
	// equivalence rather than assuming it, so a x/sys/unix change cannot make
	// the row above pass for the wrong reason.
	if wantFlags != uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP) {
		t.Errorf("REQUEST|ROOT|MATCH = 0x%04x, want the same bits as REQUEST|DUMP 0x%04x",
			wantFlags, uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP))
	}

	// --- the family header and attributes ----------------------------------

	familyHdr, attrs, remainder := DecodeAttrs(req.Hdr.Type, req.Body)

	t.Run("the family header is a 16-byte ifinfomsg", func(t *testing.T) {
		if len(familyHdr) != xtcpnl.IfInfomsgSizeCst {
			t.Fatalf("family header = %d bytes, want %d", len(familyHdr), xtcpnl.IfInfomsgSizeCst)
		}
		if familyHdr[0] != unix.AF_PACKET {
			t.Errorf("ifi_family = %d, want AF_PACKET (%d): `ip link show` asks by "+
				"link layer, not by address family", familyHdr[0], unix.AF_PACKET)
		}
		// Everything past ifi_family is zero for a bare show: no ifi_type, no
		// ifi_index, no flags filter.
		if !bytes.Equal(familyHdr[1:], make([]byte, xtcpnl.IfInfomsgSizeCst-1)) {
			t.Errorf("ifinfomsg past ifi_family = %x, want all zero", familyHdr[1:])
		}
	})

	t.Run("the only attribute is IFLA_EXT_MASK = VF|SKIP_STATS", func(t *testing.T) {
		if len(attrs) != 1 {
			t.Fatalf("attributes = %d, want exactly 1", len(attrs))
		}
		a := attrs[0]
		if a.Type != uint16(unix.IFLA_EXT_MASK) {
			t.Errorf("rta_type = %d, want IFLA_EXT_MASK (%d)", a.Type, unix.IFLA_EXT_MASK)
		}
		if a.Nested() || a.NetByteOrder() {
			t.Errorf("rta_type 0x%04x has a flag bit set; a u32 selector has none", a.Type)
		}
		if len(a.Val) != 4 {
			t.Fatalf("value = %d bytes, want 4", len(a.Val))
		}
		got := binary.LittleEndian.Uint32(a.Val)
		want := uint32(rtextFilterVFCst | rtextFilterSkipStatsCst)
		if got != want {
			t.Errorf("IFLA_EXT_MASK = 0x%02x, want 0x%02x (RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS)",
				got, want)
		}
	})

	t.Run("nothing is left over", func(t *testing.T) {
		if len(remainder) != 0 {
			t.Errorf("remainder = %d bytes (%x), want 0", len(remainder), remainder)
		}
	})

	// --- the datagram ------------------------------------------------------

	t.Run("the datagram is exactly nlmsg_len, with no oversend", func(t *testing.T) {
		d := c.Datagrams[0]
		if d.Len != 40 || d.TailBytes != 0 {
			t.Errorf("datagram = %d bytes with a %d-byte tail, want 40 and 0: "+
				"rtnl_linkdump_req_filter_fn sends n.nlmsg_len, unlike the addr, "+
				"route and neigh dump helpers", d.Len, d.TailBytes)
		}
	})
}

// TestGoldenLinkShowReplyHasNoStats is the reply-side half of the fact above,
// and the reason the request encoder cannot be deferred.
//
// RTEXT_FILTER_SKIP_STATS suppresses IFLA_STATS and IFLA_STATS64. Every
// attribute in a link reply is stable across runs except those two, which are
// packet and byte counters that change while the dump is being taken. Sending
// the mask is what makes reply comparison tractable at all; this test fails the
// moment that stops being true.
//
// go test ./pkg/nlparity/ -run TestGoldenLinkShowReplyHasNoStats
func TestGoldenLinkShowReplyHasNoStats(t *testing.T) {
	c, err := ParseRouteCaptureFile(tdGetLinkDump)
	if err != nil {
		t.Fatalf("parse capture: %v", err)
	}

	links := 0
	for _, m := range c.Msgs() {
		if m.Hdr.Type != uint16(unix.RTM_NEWLINK) {
			continue
		}
		links++

		_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
		for _, a := range attrs {
			switch a.BareType() {
			case uint16(unix.IFLA_STATS):
				t.Errorf("link reply %d carries IFLA_STATS; RTEXT_FILTER_SKIP_STATS "+
					"should have suppressed it", links)
			case uint16(unix.IFLA_STATS64):
				t.Errorf("link reply %d carries IFLA_STATS64; RTEXT_FILTER_SKIP_STATS "+
					"should have suppressed it", links)
			}
		}
	}

	if links == 0 {
		t.Fatalf("no RTM_NEWLINK replies in %s; the assertion above vacuously passed",
			tdGetLinkDump)
	}
}
