package xtcpnl

import (
	"fmt"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDecodeAddrWriteCorpus runs the read path's decoder over the address
// writes `ip` 7.1.0 sent in the captured corpus
// (testdata/7_1_4/link-write/addr/), so the encoder's wire format and the
// decoder's agree on bytes neither of them produced, and checks the kernel's
// answer with kernelAnswer.
//
// ParseNewAddr fills a missing IFA_ADDRESS from IFA_LOCAL, as `ip`'s printer
// does, so noAddress is read from the attributes themselves. prio, proto and
// cache are "" when the attribute is absent.
//
// go test ./pkg/xtcpnl/ -run TestDecodeAddrWriteCorpus
func TestDecodeAddrWriteCorpus(t *testing.T) {
	type result struct {
		msgType   uint16
		prefixlen uint8
		scope     uint8
		flags     uint32 // the header's, or IFA_FLAGS when present
		noAddress bool   // no IFA_ADDRESS on the wire
		brd       string
		label     string
		prio      string
		proto     string
		cache     string // "preferred/valid"
		answer    string
	}
	const newAddr, delAddr = unix.RTM_NEWADDR, unix.RTM_DELADDR
	refused := func(errno, msg string) string { return fmt.Sprintf("%s %q off=0", errno, msg) }
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{description: "positive: add, the plain IPv4 case", slug: "add_v4",
			expected: result{msgType: newAddr, prefixlen: 24, answer: "ACK"}},
		{description: "positive: peer is IFA_ADDRESS, the local is not copied", slug: "add_v4_peer",
			expected: result{msgType: newAddr, prefixlen: 32, answer: "ACK"}},
		{description: "positive: brd + is the last address of the prefix", slug: "add_v4_brd_plus",
			expected: result{msgType: newAddr, prefixlen: 24, brd: "192.0.2.255", answer: "ACK"}},
		{description: "positive: brd - is the first", slug: "add_v4_brd_minus",
			expected: result{msgType: newAddr, prefixlen: 24, brd: "192.0.2.0", answer: "ACK"}},
		{description: "positive: the label and the computed broadcast", slug: "add_v4_label",
			expected: result{msgType: newAddr, prefixlen: 24, brd: "192.0.2.255", label: "d0:1", answer: "ACK"}},
		{description: "positive: metric is IFA_RT_PRIORITY", slug: "add_v4_metric",
			expected: result{msgType: newAddr, prefixlen: 24, prio: "50", answer: "ACK"}},
		{description: "corner: pref is metric, not preferred_lft", slug: "add_v4_pref_is_metric",
			expected: result{msgType: newAddr, prefixlen: 24, prio: "7", answer: "ACK"}},
		{description: "positive: proto kernel_lo is IFA_PROTO 1", slug: "add_v4_proto_name",
			expected: result{msgType: newAddr, prefixlen: 24, proto: "1", answer: "ACK"}},
		{description: "positive: the lifetimes are IFA_CACHEINFO", slug: "add_v4_lifetimes",
			expected: result{msgType: newAddr, prefixlen: 24, cache: "1800/3600", answer: "ACK"}},
		{description: "positive: a secondary is the same request", slug: "add_v4_secondary",
			expected: result{msgType: newAddr, prefixlen: 24, answer: "ACK"}},
		{description: "positive: autojoin is IFA_F_MCAUTOJOIN", slug: "add_v4_autojoin",
			expected: result{msgType: newAddr, prefixlen: 32, flags: unix.IFA_F_MCAUTOJOIN, answer: "ACK"}},
		{description: "positive: nodad is IFA_F_NODAD", slug: "add_v6_nodad",
			expected: result{msgType: newAddr, prefixlen: 64, flags: unix.IFA_F_NODAD, answer: "ACK"}},
		// The kernel on this capture accepts the pair; the design table
		// expected a refusal, and the capture wins.
		{description: "positive: nodad optimistic is both flags, and accepted", slug: "add_v6_nodad_optimistic",
			expected: result{msgType: newAddr, prefixlen: 64, flags: unix.IFA_F_NODAD | unix.IFA_F_OPTIMISTIC, answer: "ACK"}},
		{description: "positive: change of a missing address creates it", slug: "change_missing_creates",
			expected: result{msgType: newAddr, prefixlen: 24, answer: "ACK"}},
		{description: "positive: replace with a metric", slug: "replace_existing",
			expected: result{msgType: newAddr, prefixlen: 24, prio: "9", answer: "ACK"}},
		{description: "positive: delete is RTM_DELADDR with scope 0", slug: "del_v4",
			expected: result{msgType: delAddr, prefixlen: 24, answer: "ACK"}},
		{description: "boundary: /31 gets no broadcast", slug: "add_v4_brd_31",
			expected: result{msgType: newAddr, prefixlen: 31, answer: "ACK"}},
		{description: "boundary: the wide flags are IFA_FLAGS", slug: "add_v6_flags_wide",
			expected: result{msgType: newAddr, prefixlen: 64, flags: unix.IFA_F_NODAD | unix.IFA_F_NOPREFIXROUTE, answer: "ACK"}},
		{description: "boundary: 127/8 gets host scope", slug: "add_v4_loopback_scope",
			expected: result{msgType: newAddr, prefixlen: 8, scope: unix.RT_SCOPE_HOST, answer: "ACK"}},
		{description: "corner: the wildcard delete has no IFA_ADDRESS and prefixlen 32", slug: "del_v4_wildcard",
			expected: result{msgType: delAddr, prefixlen: 32, noAddress: true, answer: "ACK"}},
		{description: "corner: nodad before the address is dropped", slug: "add_v6_nodad_first",
			expected: result{msgType: newAddr, prefixlen: 64, answer: "ACK"}},
		{description: "negative: an existing address is EEXIST", slug: "add_existing",
			expected: result{msgType: newAddr, prefixlen: 24,
				answer: refused("EEXIST", "ipv4: Address already assigned")}},
		{description: "negative: deleting a missing address is EADDRNOTAVAIL", slug: "del_missing",
			expected: result{msgType: delAddr, prefixlen: 24,
				answer: refused("EADDRNOTAVAIL", "ipv4: Address not found")}},
		{description: "negative: a secondary with another scope is EINVAL", slug: "scope_conflict",
			expected: result{msgType: newAddr, prefixlen: 24, scope: unix.RT_SCOPE_LINK,
				answer: refused("EINVAL", "ipv4: Invalid scope value")}},
		{description: "negative: IPv6 multicast without autojoin is EADDRNOTAVAIL", slug: "v6_mcast_noautojoin",
			expected: result{msgType: newAddr, prefixlen: 128,
				answer: refused("EADDRNOTAVAIL", `ipv6: Cannot assign multicast address without "IFA_F_MCAUTOJOIN" flag`)}},
		{description: "negative: mngtmpaddr on /48 is EINVAL", slug: "add_v6_mngtmpaddr_48",
			expected: result{msgType: newAddr, prefixlen: 48, flags: unix.IFA_F_MANAGETEMPADDR,
				answer: refused("EINVAL", `ipv6: address with "mngtmpaddr" flag must have a prefix length of 64`)}},
		{description: "negative: a 16-byte label is ERANGE at IFA_LABEL", slug: "label_16",
			expected: result{msgType: newAddr, prefixlen: 24, label: "d0:0123456789abc",
				answer: fmt.Sprintf("ERANGE %q off=%d", "Attribute failed policy validation", unix.IFA_LABEL)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, _, request := lastNlMsgErr(t, addrFixture("addr", tc.slug, "ip.pcap"))
			body := request[NlMsgHdrSizeCst:]
			ai, err := ParseNewAddr(body)
			if err != nil {
				t.Fatal(err)
			}
			attrs, err := ParseNetlinkAttributes(body[IfAddrmsgSizeCst:])
			if err != nil {
				t.Fatal(err)
			}
			address, err := attribute(attrs, unix.IFA_ADDRESS)
			if err != nil {
				t.Fatal(err)
			}
			got := result{
				msgType:   uint16(request[4]) | uint16(request[5])<<8,
				prefixlen: ai.Prefixlen,
				scope:     ai.Scope,
				flags:     ai.Flags,
				noAddress: address == nil,
				label:     ai.Label,
				answer:    kernelAnswer(t, "addr", tc.slug),
			}
			if ai.Broadcast != nil {
				got.brd = net.IP(ai.Broadcast).String()
			}
			if ai.HasRtPriority {
				got.prio = fmt.Sprint(ai.RtPriority)
			}
			if ai.HasProto {
				got.proto = fmt.Sprint(ai.Proto)
			}
			if ai.HasCacheInfo {
				got.cache = fmt.Sprintf("%d/%d", ai.CacheInfo.Preferred, ai.CacheInfo.Valid)
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}
