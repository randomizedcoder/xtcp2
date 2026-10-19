package xtcpnl

import (
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// capturedNlMsg is one netlink message out of a pcap, header and body.
type capturedNlMsg struct {
	h    NlMsgHdr
	body []byte
}

// pcapNlMsgs returns every netlink message in a DLT 253 pcap, in capture
// order, across every datagram.
func pcapNlMsgs(t *testing.T, path string) []capturedNlMsg {
	t.Helper()
	bs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, recs, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var out []capturedNlMsg
	for _, r := range recs {
		_, p, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s: %v", path, perr)
		}
		for len(p) >= NlMsgHdrSizeCst {
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(p, &h); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			mlen := int(h.Len)
			if mlen < NlMsgHdrSizeCst || mlen > len(p) {
				t.Fatalf("%s: nlmsg_len %d in a %d-byte datagram", path, mlen, len(p))
			}
			out = append(out, capturedNlMsg{h: h, body: p[NlMsgHdrSizeCst:mlen]})
			if adv := nlmsgAlign(mlen); adv < len(p) {
				p = p[adv:]
			} else {
				p = nil
			}
		}
	}
	return out
}

// nsidOf decodes NETNSA_NSID from an RTM_NEWNSID or RTM_DELNSID body: a
// 4-byte rtgenmsg, then attributes. There is no ParseNsid yet (P1), so the
// test reads the one attribute it needs.
func nsidOf(t *testing.T, body []byte) uint32 {
	t.Helper()
	const rtgenmsgSize = 4
	if len(body) < rtgenmsgSize {
		t.Fatalf("nsid body is %d bytes", len(body))
	}
	attrs, err := ParseNetlinkAttributes(body[rtgenmsgSize:])
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range attrs {
		if a.ID() == unix.NETNSA_NSID && len(a.Data) == 4 {
			return binary.LittleEndian.Uint32(a.Data)
		}
	}
	t.Fatal("no NETNSA_NSID")
	return 0
}

// TestRtnlWriteCorpus checks the C1-T01 rows (testdata/7_1_4/link-write/
// rtnl/, scripts/rtnl-write-corpus.exp; docs/netlink/goip-parity-gaps.md):
// each row's ip.pcap holds the message type it was written for, that
// message decodes with the type's existing parser to the value the row
// sent, the kernel's answer is the one expected, and ip.rc agrees.
//
// key is the decoded field that names the row's object: a rule's priority,
// a label, a nexthop id, a table name, an ifindex, an nsid or an anycast
// address. A request is matched by NLM_F_REQUEST; an event row's message is
// the notification, which carries neither REQUEST nor MULTI.
func TestRtnlWriteCorpus(t *testing.T) {
	type result struct {
		msgType uint16
		key     string
		errno   syscall.Errno
		rc      string
	}
	decode := map[uint16]func(t *testing.T, body []byte) string{
		unix.RTM_DELRULE: func(t *testing.T, b []byte) string {
			ri, err := ParseRule(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(ri.Priority)
		},
		unix.RTM_DELADDRLABEL: func(t *testing.T, b []byte) string {
			li, err := ParseNewAddrLabel(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(li.Label)
		},
		unix.RTM_DELNEXTHOP: func(t *testing.T, b []byte) string {
			ni, err := ParseNewNexthop(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(ni.ID)
		},
		unix.RTM_SETNEIGHTBL: func(t *testing.T, b []byte) string {
			ti, err := ParseNewNeighTbl(b)
			if err != nil {
				t.Fatal(err)
			}
			return ti.Name
		},
		unix.RTM_SETSTATS: func(t *testing.T, b []byte) string {
			si, err := ParseNewStats(b)
			if err != nil {
				t.Fatal(err)
			}
			return u32s(si.Ifindex)
		},
		unix.RTM_NEWNSID: func(t *testing.T, b []byte) string { return u32s(nsidOf(t, b)) },
		unix.RTM_DELNSID: func(t *testing.T, b []byte) string { return u32s(nsidOf(t, b)) },
		unix.RTM_DELANYCAST: func(t *testing.T, b []byte) string {
			ai, err := ParseNewAddr(b)
			if err != nil {
				t.Fatal(err)
			}
			return net.IP(ai.Anycast).String()
		},
	}
	tests := []struct {
		description string
		slug        string
		event       bool
		expected    result
	}{
		{description: "positive: rule del sends DELRULE for pref 100, ACKed", slug: "rule_del",
			expected: result{msgType: unix.RTM_DELRULE, key: "100", rc: "0"}},
		{description: "negative: deleting a rule that does not exist is ENOENT", slug: "rule_del_missing",
			expected: result{msgType: unix.RTM_DELRULE, key: "4242", errno: unix.ENOENT, rc: "2"}},
		{description: "positive: addrlabel del sends DELADDRLABEL for label 99", slug: "addrlabel_del",
			expected: result{msgType: unix.RTM_DELADDRLABEL, key: "99", rc: "0"}},
		{description: "positive: nexthop del sends DELNEXTHOP for id 1", slug: "nexthop_del",
			expected: result{msgType: unix.RTM_DELNEXTHOP, key: "1", rc: "0"}},
		{description: "positive: ntable change sends SETNEIGHTBL for arp_cache", slug: "ntable_change",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "arp_cache", rc: "0"}},
		{description: "corner: ntable change on a table that does not exist is the kernel's ENOENT",
			slug:     "ntable_change_missing",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "nosuch_cache", errno: unix.ENOENT, rc: "2"}},
		// The kernel's neightbl_set refuses the table-wide thresholds
		// (NDTA_THRESH1-3, NDTA_GC_INTERVAL) outside init_net, so the same
		// request that init_net takes is ENOENT in the capture's namespace.
		{description: "corner: thresh1 outside the initial namespace is ENOENT",
			slug:     "ntable_change_thresh_netns",
			expected: result{msgType: unix.RTM_SETNEIGHTBL, key: "arp_cache", errno: unix.ENOENT, rc: "2"}},
		{description: "positive: stats set sends SETSTATS for d0, ifindex 3", slug: "stats_set_l3",
			expected: result{msgType: unix.RTM_SETSTATS, key: "3", rc: "0"}},
		{description: "positive: netns set sends NEWNSID with nsid 7", slug: "netns_set",
			expected: result{msgType: unix.RTM_NEWNSID, key: "7", rc: "0"}},
		{description: "positive: the peer's deletion notifies DELNSID for nsid 7", slug: "nsid_del_event",
			event: true, expected: result{msgType: unix.RTM_DELNSID, key: "7", rc: "0"}},
		{description: "positive: deleting the address notifies DELANYCAST for 2001:db8:1::",
			slug: "anycast_del_event", event: true,
			expected: result{msgType: unix.RTM_DELANYCAST, key: "2001:db8:1::", rc: "0"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := tdRtnlWrite_7_1_4 + "/" + tc.slug
			msgs := pcapNlMsgs(t, dir+"/ip.pcap")
			var got result
			var found *capturedNlMsg
			for i := range msgs {
				m := &msgs[i]
				if m.h.Type != tc.expected.msgType {
					continue
				}
				isReq := m.h.Flags&unix.NLM_F_REQUEST != 0
				isNotif := m.h.Flags&(unix.NLM_F_REQUEST|unix.NLM_F_MULTI) == 0
				if (tc.event && isNotif) || (!tc.event && isReq) {
					found = m
					break
				}
			}
			if found == nil {
				t.Fatalf("no %d message (event=%v) in %s", tc.expected.msgType, tc.event, dir)
			}
			got.msgType = found.h.Type
			got.key = decode[found.h.Type](t, found.body)
			if !tc.event {
				got.errno = answerTo(t, msgs, found.h)
			}
			rc, err := os.ReadFile(dir + "/ip.rc")
			if err != nil {
				t.Fatal(err)
			}
			got.rc = strings.TrimSpace(string(rc))
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRtnlWriteCorpusFirstRequest pins what `ip` sends first in each row:
// a write naming a device resolves it with an RTM_GETLINK by name, and `ip
// ntable` takes a link dump (ll_init_map) whether or not it names one, so
// the write is never the first request there; a write naming no device is.
func TestRtnlWriteCorpusFirstRequest(t *testing.T) {
	tests := []struct {
		description string
		slug        string
		expected    uint16
	}{
		{description: "positive: stats set resolves d0 before SETSTATS", slug: "stats_set_l3",
			expected: unix.RTM_GETLINK},
		{description: "positive: ntable change dumps the links before SETNEIGHTBL", slug: "ntable_change",
			expected: unix.RTM_GETLINK},
		{description: "corner: and does so even when it names no device", slug: "ntable_change_missing",
			expected: unix.RTM_GETLINK},
		{description: "boundary: rule del names no device, so DELRULE is first", slug: "rule_del",
			expected: unix.RTM_DELRULE},
		{description: "boundary: netns set is first, ahead of its list-id dump", slug: "netns_set",
			expected: unix.RTM_NEWNSID},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got uint16
			for _, m := range pcapNlMsgs(t, tdRtnlWrite_7_1_4+"/"+tc.slug+"/ip.pcap") {
				if m.h.Flags&unix.NLM_F_REQUEST != 0 {
					got = m.h.Type
					break
				}
			}
			if got != tc.expected {
				t.Errorf("first request is type %d, want %d", got, tc.expected)
			}
		})
	}
}

// answerTo finds the NLMSG_ERROR that answers req (same seq, echoing its
// type) and returns its errno, 0 for an ACK.
func answerTo(t *testing.T, msgs []capturedNlMsg, req NlMsgHdr) syscall.Errno {
	t.Helper()
	for _, m := range msgs {
		if m.h.Type != unix.NLMSG_ERROR || m.h.Seq != req.Seq {
			continue
		}
		e, err := ParseNlMsgErr(m.h.Flags, m.body)
		if err != nil {
			t.Fatal(err)
		}
		if e.RequestType != req.Type {
			continue
		}
		if e.Errno == 0 {
			return 0
		}
		return syscall.Errno(-e.Errno)
	}
	t.Fatalf("no NLMSG_ERROR answers seq %d type %d", req.Seq, req.Type)
	return 0
}

func u32s(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
