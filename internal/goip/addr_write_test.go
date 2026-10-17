package goip

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// addrWriteCorpusDir holds the A0-T04 address write captures; a row that
// names a slug reads that slug's ip.err.
const addrWriteCorpusDir = "../../pkg/xtcpnl/testdata/7_1_4/link-write/addr"

// TestLookupAddrWriteVerb is do_ipaddr's chain up to the list verbs.
//
// go test ./internal/goip/ -run TestLookupAddrWriteVerb
func TestLookupAddrWriteVerb(t *testing.T) {
	type verb struct {
		msgType uint16
		flags   uint16
	}
	add := &verb{unix.RTM_NEWADDR, unix.NLM_F_CREATE | unix.NLM_F_EXCL}
	change := &verb{unix.RTM_NEWADDR, unix.NLM_F_REPLACE}
	replace := &verb{unix.RTM_NEWADDR, unix.NLM_F_CREATE | unix.NLM_F_REPLACE}
	del := &verb{unix.RTM_DELADDR, 0}
	tests := []struct {
		description string
		arg         string
		expected    *verb
	}{
		{description: "positive: add", arg: "add", expected: add},
		{description: "positive: change", arg: "change", expected: change},
		{description: "positive: replace", arg: "replace", expected: replace},
		{description: "positive: delete", arg: "delete", expected: del},
		{description: "positive: chg is change, by strcmp", arg: "chg", expected: change},
		{description: "corner: a is add", arg: "a", expected: add},
		{description: "corner: c is change", arg: "c", expected: change},
		{description: "corner: d is delete", arg: "d", expected: del},
		{description: "corner: del is delete", arg: "del", expected: del},
		{description: "corner: r is replace, not restore", arg: "r", expected: replace},
		{description: "corner: re is replace, not restore", arg: "re", expected: replace},
		{description: "corner: res is restore, not a write verb here", arg: "res"},
		{description: "corner: s stays a show", arg: "s"},
		{description: "corner: l stays a list", arg: "l"},
		{description: "corner: f is flush, not a write verb here", arg: "f"},
		{description: "corner: sa is save", arg: "sa"},
		{description: "negative: chgx is not chg, which does not abbreviate or extend", arg: "chgx"},
		{description: "boundary: the full word plus one is no verb", arg: "adds"},
		{description: "boundary: an empty verb matches nothing", arg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := lookupAddrWriteVerb(tc.arg)
			switch {
			case tc.expected == nil && v != nil:
				t.Fatalf("got verb %q, want none", v.name)
			case tc.expected == nil:
			case v == nil:
				t.Fatalf("got no verb, want %+v", *tc.expected)
			case v.msgType != tc.expected.msgType || v.flags != tc.expected.flags:
				t.Errorf("got type %d flags %#x, want %d %#x", v.msgType, v.flags, tc.expected.msgType, tc.expected.flags)
			}
		})
	}
}

// TestLookupAddrKeyword is the chain's order deciding what an abbreviation
// is: design "Attribute order", the three collisions. 0 is the local arm.
//
// go test ./internal/goip/ -run TestLookupAddrKeyword
func TestLookupAddrKeyword(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    addrArm
	}{
		{description: "corner: p, pr, pref and prefer are the metric", args: []string{"p", "pr", "pref", "prefer"},
			expected: addrArmMetric},
		{description: "corner: preferr, preferred and preferred_lft are the lifetime",
			args: []string{"preferr", "preferred", "preferred_lft"}, expected: addrArmPreferredLft},
		{description: "corner: b, bro and broadcast are broadcast", args: []string{"b", "bro", "broadcast"},
			expected: addrArmBroadcast},
		{description: "positive: brd is broadcast's exact alias", args: []string{"brd"}, expected: addrArmBroadcast},
		{description: "corner: m and me are the metric", args: []string{"m", "me"}, expected: addrArmMetric},
		{description: "corner: mngtmpaddr is a flag, not the metric", args: []string{"mngtmpaddr"}, expected: addrArmFlag},
		{description: "positive: every ifa_flag_data name is a flag",
			args: []string{"secondary", "temporary", "nodad", "optimistic", "dadfailed", "home", "deprecated",
				"tentative", "permanent", "noprefixroute", "autojoin", "stable-privacy"},
			expected: addrArmFlag},
		{description: "corner: v is valid_lft", args: []string{"v"}, expected: addrArmValidLft},
		{description: "positive: remote is peer", args: []string{"remote"}, expected: addrArmPeer},
		{description: "negative: strcmp keywords do not abbreviate",
			args: []string{"pe", "an", "sc", "de", "la", "pro", "nod", "rem"}},
		{description: "boundary: an exact keyword plus one character is the local",
			args: []string{"peers", "brdx", "devs", "protos", "nodads"}},
		{description: "negative: an address is the local", args: []string{"192.0.2.10/24"}},
		{description: "corner: local itself is the else-arm", args: []string{"local"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			for _, a := range tc.args {
				var got addrArm
				if kw := lookupAddrKeyword(a); kw != nil {
					got = kw.arm
				}
				if got != tc.expected {
					t.Errorf("lookupAddrKeyword(%q) = %d, want %d", a, got, tc.expected)
				}
			}
		})
	}
}

// addrWriteExpected is a row's outcome: the AddrWrite, the names resolved
// and the warnings, or an error class and a fragment of iproute2's message.
// A slug replaces the warnings (on success) or the fragment (on a refusal)
// with that slug's captured ip.err.
type addrWriteExpected struct {
	write    xtcpnl.AddrWrite
	resolved []string
	warning  string
	slug     string
	err      error
	errMsg   string
}

func addrIP(s string) []byte { return netip.MustParseAddr(s).AsSlice() }

func ifaLocal(s string) xtcpnl.AddrAttr { return xtcpnl.AddrAttrBytes(unix.IFA_LOCAL, addrIP(s)) }
func ifaAddress(s string) xtcpnl.AddrAttr {
	return xtcpnl.AddrAttrBytes(unix.IFA_ADDRESS, addrIP(s))
}
func ifaBroadcast(s string) xtcpnl.AddrAttr {
	return xtcpnl.AddrAttrBytes(unix.IFA_BROADCAST, addrIP(s))
}

func ifaAttrs(a ...xtcpnl.AddrAttr) []xtcpnl.AddrAttr { return a }

// newAddrWrite is an IPv4 RTM_NEWADDR on d0 (ifindex 3), scope 0.
func newAddrWrite(flags uint16, plen uint8, attrs, post []xtcpnl.AddrAttr) xtcpnl.AddrWrite {
	return xtcpnl.AddrWrite{
		MsgType: unix.RTM_NEWADDR, Flags: flags | unix.NLM_F_ACK,
		Family: unix.AF_INET, Prefixlen: plen, Index: 3,
		Attrs: attrs, PostAttrs: post,
	}
}

func addrWith(w xtcpnl.AddrWrite, f func(*xtcpnl.AddrWrite)) xtcpnl.AddrWrite {
	f(&w)
	return w
}

// TestParseAddrWriteArgs is argv after the verb, through ipaddr_modify, into
// the AddrWrite the encoder takes: addr-write-path.md table 3, with the
// design's row numbers in parentheses. d0 is ifindex 3 as in the corpus,
// boom a resolver failure, any other name a miss.
//
// go test ./internal/goip/ -run TestParseAddrWriteArgs
func TestParseAddrWriteArgs(t *testing.T) {
	const (
		create  = unix.NLM_F_CREATE
		excl    = unix.NLM_F_EXCL
		replace = unix.NLM_F_REPLACE
		p24     = "192.0.2.10/24"
	)
	d0 := []string{"d0"}
	ok := func(w xtcpnl.AddrWrite) addrWriteExpected { return addrWriteExpected{write: w, resolved: d0} }
	warned := func(w xtcpnl.AddrWrite, msg string) addrWriteExpected {
		return addrWriteExpected{write: w, resolved: d0, warning: msg}
	}
	invalid := func(msg string, resolved ...string) addrWriteExpected {
		return addrWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	refusedSlug := func(slug string, resolved ...string) addrWriteExpected {
		return addrWriteExpected{err: ErrInvalidArgument, slug: slug, resolved: resolved}
	}
	notImpl := func(msg string) addrWriteExpected {
		return addrWriteExpected{err: ErrNotImplemented, errMsg: msg}
	}
	// added is `add 192.0.2.10/24 … dev d0`, extra attributes after the local.
	added := func(extra ...xtcpnl.AddrAttr) xtcpnl.AddrWrite {
		return newAddrWrite(create|excl, 24, append(ifaAttrs(ifaLocal("192.0.2.10")), extra...),
			ifaAttrs(ifaAddress("192.0.2.10")))
	}
	withPost := func(w xtcpnl.AddrWrite, post ...xtcpnl.AddrAttr) xtcpnl.AddrWrite {
		return addrWith(w, func(w *xtcpnl.AddrWrite) { w.PostAttrs = append(w.PostAttrs, post...) })
	}
	flagged := func(w xtcpnl.AddrWrite, f uint32) xtcpnl.AddrWrite {
		return addrWith(w, func(w *xtcpnl.AddrWrite) { w.IfaFlags = f })
	}
	v6 := func(flags uint16, plen uint8, ifaFlags uint32) xtcpnl.AddrWrite {
		return addrWith(newAddrWrite(flags, plen, ifaAttrs(ifaLocal("2001:db8::5")), ifaAttrs(ifaAddress("2001:db8::5"))),
			func(w *xtcpnl.AddrWrite) { w.Family, w.IfaFlags = unix.AF_INET6, ifaFlags })
	}
	brdV4 := func(plen uint8, local string, brd ...string) xtcpnl.AddrWrite {
		w := newAddrWrite(create|excl, plen, ifaAttrs(ifaLocal(local)), ifaAttrs(ifaAddress(local)))
		for _, b := range brd {
			w.PostAttrs = append(w.PostAttrs, ifaBroadcast(b))
		}
		return w
	}
	deleted := func(plen uint8, local string, post ...xtcpnl.AddrAttr) xtcpnl.AddrWrite {
		return addrWith(newAddrWrite(0, plen, ifaAttrs(ifaLocal(local)), post),
			func(w *xtcpnl.AddrWrite) { w.MsgType = unix.RTM_DELADDR })
	}
	scoped := func(w xtcpnl.AddrWrite, s uint8) xtcpnl.AddrWrite {
		return addrWith(w, func(w *xtcpnl.AddrWrite) { w.Scope = s })
	}
	prio := func(v uint32) xtcpnl.AddrAttr { return xtcpnl.AddrAttrU32(unix.IFA_RT_PRIORITY, v) }
	cache := xtcpnl.AddrAttrCacheinfo
	const inf = 0xffffffff
	peered := newAddrWrite(create|excl, 32, ifaAttrs(ifaLocal("10.0.0.1"), ifaAddress("10.0.0.2")), nil)

	tests := []struct {
		description string
		verb        string
		family      uint8
		args        string
		expected    addrWriteExpected
	}{
		// The verbs.
		{description: "positive: add (1)", verb: "add", args: p24 + " dev d0", expected: ok(added())},
		{description: "positive: chg is change (2)", verb: "chg", args: p24 + " dev d0",
			expected: ok(addrWith(added(), func(w *xtcpnl.AddrWrite) { w.Flags = replace | unix.NLM_F_ACK }))},
		{description: "positive: replace (3)", verb: "replace", args: p24 + " dev d0",
			expected: ok(addrWith(added(), func(w *xtcpnl.AddrWrite) { w.Flags = create | replace | unix.NLM_F_ACK }))},
		{description: "positive: del is RTM_DELADDR, flags 0 (4)", verb: "del", args: p24 + " dev d0",
			expected: ok(deleted(24, "192.0.2.10", ifaAddress("192.0.2.10")))},

		// Addresses, peers and broadcasts.
		{description: "positive: peer is IFA_ADDRESS in argv order and sets the prefix length, no copy (5)",
			verb: "add", args: "10.0.0.1 peer 10.0.0.2/32 dev d0", expected: ok(peered)},
		{description: "corner: remote is peer", verb: "add", args: "10.0.0.1 remote 10.0.0.2/32 dev d0",
			expected: ok(peered)},
		{description: "boundary: a peer with no /len is 32", verb: "add", args: "10.0.0.1 peer 10.0.0.2 dev d0",
			expected: ok(peered)},
		{description: "positive: brd + is the computed broadcast, after the copy (6)", verb: "add",
			args: p24 + " brd + dev d0", expected: ok(brdV4(24, "192.0.2.10", "192.0.2.255"))},
		{description: "positive: brd - (6)", verb: "add", args: p24 + " brd - dev d0",
			expected: ok(brdV4(24, "192.0.2.10", "192.0.2.0"))},
		{description: "corner: b is broadcast", verb: "add", args: p24 + " b + dev d0",
			expected: ok(brdV4(24, "192.0.2.10", "192.0.2.255"))},
		{description: "corner: brd + is computed from the peer when there is one", verb: "add",
			args: "10.0.0.1 peer 10.0.0.0/24 brd + dev d0",
			expected: ok(newAddrWrite(create|excl, 24, ifaAttrs(ifaLocal("10.0.0.1"), ifaAddress("10.0.0.0")),
				ifaAttrs(ifaBroadcast("10.0.0.255"))))},
		{description: "positive: a broadcast address is in argv order", verb: "add",
			args: "brd 192.0.2.255 " + p24 + " dev d0",
			expected: ok(newAddrWrite(create|excl, 24, ifaAttrs(ifaBroadcast("192.0.2.255"), ifaLocal("192.0.2.10")),
				ifaAttrs(ifaAddress("192.0.2.10"))))},
		{description: "boundary: brd + on a /30 has a broadcast (10)", verb: "add", args: "192.0.2.1/30 brd + dev d0",
			expected: ok(brdV4(30, "192.0.2.1", "192.0.2.3"))},
		{description: "boundary: brd + on a /31 has none, and no error (10)", verb: "add",
			args: "192.0.2.10/31 brd + dev d0", expected: ok(brdV4(31, "192.0.2.10"))},
		{description: "boundary: brd + on a /32 has none, and no error (10)", verb: "add",
			args: "192.0.2.10/32 brd + dev d0", expected: ok(brdV4(32, "192.0.2.10"))},
		{description: "corner: a delete ignores brd +", verb: "del", args: p24 + " brd + dev d0",
			expected: ok(deleted(24, "192.0.2.10", ifaAddress("192.0.2.10")))},
		{description: "corner: the label goes before the computed broadcast (32)", verb: "add",
			args:     p24 + " brd + label d0:1 dev d0",
			expected: ok(withPost(added(xtcpnl.AddrAttrString(unix.IFA_LABEL, "d0:1")), ifaBroadcast("192.0.2.255")))},
		{description: "positive: anycast and label are in argv order", verb: "add",
			args: p24 + " anycast 192.0.2.77 label d0:1 dev d0",
			expected: ok(added(xtcpnl.AddrAttrBytes(unix.IFA_ANYCAST, addrIP("192.0.2.77")),
				xtcpnl.AddrAttrString(unix.IFA_LABEL, "d0:1")))},
		{description: "boundary: an IPv4 local with no /len is 32", verb: "add", args: "192.0.2.10 dev d0",
			expected: ok(addrWith(added(), func(w *xtcpnl.AddrWrite) { w.Prefixlen = 32 }))},
		{description: "corner: the local keyword is optional (31)", verb: "add", args: "local " + p24 + " dev d0",
			expected: ok(added())},
		{description: "corner: -6 sets the header family", verb: "add", family: unix.AF_INET6,
			args: "2001:db8::5/64 dev d0", expected: ok(v6(create|excl, 64, 0))},
		{description: "corner: no address at all is still built; the kernel refuses it", verb: "add", args: "dev d0",
			expected: ok(addrWith(newAddrWrite(create|excl, 0, nil, nil), func(w *xtcpnl.AddrWrite) { w.Family = 0 }))},

		// Metric, proto and lifetimes.
		{description: "positive: metric is IFA_RT_PRIORITY (7)", verb: "add", args: p24 + " metric 5 dev d0",
			expected: ok(added(prio(5)))},
		{description: "positive: priority is the metric (7)", verb: "add", args: p24 + " priority 5 dev d0",
			expected: ok(added(prio(5)))},
		{description: "positive: preference is the metric (7)", verb: "add", args: p24 + " preference 5 dev d0",
			expected: ok(added(prio(5)))},
		{description: "corner: pref is the metric (30)", verb: "add", args: p24 + " pref 7 dev d0",
			expected: ok(added(prio(7)))},
		{description: "corner: preferred is the lifetime (30)", verb: "add", args: p24 + " preferred 7 valid_lft 9 dev d0",
			expected: ok(withPost(added(), cache(7, 9)))},
		{description: "positive: proto by name (8)", verb: "add", args: p24 + " proto kernel_lo dev d0",
			expected: ok(added(xtcpnl.AddrAttrU8(xtcpnl.IfaProto, 1)))},
		{description: "positive: proto by number, base 0", verb: "add", args: p24 + " proto 0x2 dev d0",
			expected: ok(added(xtcpnl.AddrAttrU8(xtcpnl.IfaProto, 2)))},
		{description: "positive: forever lifetimes (9)", verb: "add", args: p24 + " valid_lft forever preferred_lft forever dev d0",
			expected: ok(withPost(added(), cache(inf, inf)))},
		{description: "boundary: equal lifetimes are built (13)", verb: "add", args: p24 + " valid_lft 60 preferred_lft 60 dev d0",
			expected: ok(withPost(added(), cache(60, 60)))},
		{description: "boundary: lifetimes are base 0 (14)", verb: "add", args: p24 + " valid_lft 0x10 preferred_lft 0x10 dev d0",
			expected: ok(withPost(added(), cache(16, 16)))},
		{description: "corner: valid_lft alone leaves preferred infinite, which is greater", verb: "add",
			args: p24 + " valid_lft 0x10 dev d0", expected: invalid("preferred_lft is greater than valid_lft", "d0")},

		// Flags and scope.
		{description: "boundary: nodad fits the header byte (11)", verb: "add", family: unix.AF_INET6,
			args: "2001:db8::5/64 nodad dev d0", expected: ok(v6(create|excl, 64, unix.IFA_F_NODAD))},
		{description: "boundary: nodad mngtmpaddr is 0x102 (11)", verb: "add", family: unix.AF_INET6,
			args:     "2001:db8::5/64 nodad mngtmpaddr dev d0",
			expected: ok(v6(create|excl, 64, unix.IFA_F_NODAD|unix.IFA_F_MANAGETEMPADDR))},
		{description: "positive: autojoin on a multicast address", verb: "add", args: "224.0.0.251/32 autojoin dev d0",
			expected: ok(flagged(newAddrWrite(create|excl, 32, ifaAttrs(ifaLocal("224.0.0.251")),
				ifaAttrs(ifaAddress("224.0.0.251"))), unix.IFA_F_MCAUTOJOIN))},
		{description: "corner: noprefixroute is not v6-only", verb: "add", args: p24 + " noprefixroute dev d0",
			expected: ok(flagged(added(), unix.IFA_F_NOPREFIXROUTE))},
		{description: "corner: a read-only flag warns and is not set (24)", verb: "add", args: p24 + " permanent dev d0",
			expected: warned(added(), "Warning: permanent option is not mutable from userspace\n")},
		{description: "corner: a v6-only flag under -4 warns and is not set (25)", verb: "add", family: unix.AF_INET,
			args: p24 + " nodad dev d0", expected: warned(added(), "Warning: nodad option can be set only for IPv6 addresses\n")},
		{description: "corner: a flag before the address, no family option, warns (26)", verb: "add",
			args:     "nodad 2001:db8::5/64 dev d0",
			expected: addrWriteExpected{write: v6(create|excl, 64, 0), resolved: d0, slug: "add_v6_nodad_first"}},
		{description: "corner: the same words after the address set it", verb: "add", args: "2001:db8::5/64 nodad dev d0",
			expected: ok(v6(create|excl, 64, unix.IFA_F_NODAD))},
		{description: "boundary: a 127/8 local defaults to scope host (12)", verb: "add", args: "127.0.0.2/8 dev d0",
			expected: ok(scoped(newAddrWrite(create|excl, 8, ifaAttrs(ifaLocal("127.0.0.2")), ifaAttrs(ifaAddress("127.0.0.2"))),
				unix.RT_SCOPE_HOST))},
		{description: "boundary: 128.0.0.1/8 defaults to scope global (12)", verb: "add", args: "128.0.0.1/8 dev d0",
			expected: ok(newAddrWrite(create|excl, 8, ifaAttrs(ifaLocal("128.0.0.1")), ifaAttrs(ifaAddress("128.0.0.1"))))},
		{description: "positive: scope by name", verb: "add", args: p24 + " scope link dev d0",
			expected: ok(scoped(added(), unix.RT_SCOPE_LINK))},

		// Deletes.
		{description: "corner: the IPv4 wildcard delete has no IFA_ADDRESS and warns (27)", verb: "del",
			args:     "192.0.2.1 dev d0",
			expected: addrWriteExpected{write: deleted(32, "192.0.2.1"), resolved: d0, slug: "del_v4_wildcard"}},
		{description: "corner: an IPv6 delete with no /len is not a wildcard", verb: "del", args: "2001:db8::5 dev d0",
			expected: ok(addrWith(deleted(128, "2001:db8::5", ifaAddress("2001:db8::5")),
				func(w *xtcpnl.AddrWrite) { w.Family = unix.AF_INET6 }))},
		{description: "corner: a delete keeps scope 0 (28)", verb: "del", args: "127.0.0.2/8 dev d0",
			expected: ok(deleted(8, "127.0.0.2", ifaAddress("127.0.0.2")))},
		{description: "corner: a delete keeps an explicit scope", verb: "del", args: p24 + " scope link dev d0",
			expected: ok(scoped(deleted(24, "192.0.2.10", ifaAddress("192.0.2.10")), unix.RT_SCOPE_LINK))},

		// dev.
		{description: "corner: a second dev replaces the first", verb: "add", args: p24 + " dev x dev d0",
			expected: ok(added())},

		// Refused in the loop: nothing is resolved.
		{description: "negative: a lifetime with units (17)", verb: "add", args: p24 + " valid_lft 3600sec dev d0",
			expected: invalid(`"3600sec" is wrong: valid_lft value`)},
		{description: "negative: a second peer quotes its value (20)", verb: "add",
			args: "10.0.0.1 peer 10.0.0.2 peer 10.0.0.3 dev d0", expected: invalid(`duplicate "peer": "10.0.0.3"`)},
		{description: "negative: a second local is duparg2 (20)", verb: "add", args: p24 + " 192.0.2.11/24 dev d0",
			expected: invalid(`either "local" is duplicate, or "192.0.2.11/24" is garbage`)},
		{description: "negative: a second brd quotes its value (20)", verb: "add", args: p24 + " brd + brd - dev d0",
			expected: invalid(`duplicate "broadcast": "-"`)},
		{description: "negative: a second anycast (20)", verb: "add", args: p24 + " anycast 192.0.2.1 anycast 192.0.2.2 dev d0",
			expected: invalid(`duplicate "anycast": "192.0.2.2"`)},
		{description: "corner: a second valid_lft quotes the keyword, not its value (20)", verb: "add",
			args: p24 + " valid_lft 60 valid_lft 70 dev d0", expected: invalid(`duplicate "valid_lft": "valid_lft"`)},
		{description: "corner: a second preferred_lft quotes the abbreviation as typed", verb: "add",
			args: p24 + " preferred 1 preferred 2 dev d0", expected: invalid(`duplicate "preferred_lft": "preferred"`)},
		{description: "negative: a bad scope (21)", verb: "add", args: p24 + " scope bogus dev d0",
			expected: invalid(`"bogus" is wrong: invalid scope value.`)},
		{description: "negative: proto over 255 (21)", verb: "add", args: p24 + " proto 300 dev d0",
			expected: invalid(`"300" is wrong: "proto" value is invalid`)},
		{description: "negative: a bad metric", verb: "add", args: p24 + " metric x dev d0",
			expected: invalid(`"x" is wrong: "metric" value is invalid`)},
		{description: "negative: an over-long prefix (23)", verb: "add", args: "192.0.2.1/33 dev d0",
			expected: invalid(`any valid prefix is expected rather than "192.0.2.1/33"`)},
		{description: "negative: an IPv4 local under -6", verb: "add", family: unix.AF_INET6, args: p24 + " dev d0",
			expected: invalid(`inet6 prefix is expected rather than "192.0.2.10/24"`)},
		{description: "negative: a keyword missing its value", verb: "add", args: p24 + " dev",
			expected: invalid("Command line is not complete")},
		{description: "negative: local missing its value", verb: "add", args: "local",
			expected: invalid("Command line is not complete")},
		{description: "corner: help is usage, which goip does not print (33)", verb: "add", args: "help",
			expected: notImpl("ip address help")},
		{description: "corner: an abbreviated help after local is usage too (33)", verb: "add", args: "local he",
			expected: notImpl("ip address help")},

		// Refused after the loop, before the lookup.
		{description: "negative: no dev (15)", verb: "add", args: p24, expected: refusedSlug("no_dev_keyword")},
		{description: "negative: brd + on IPv6 (16)", verb: "add", family: unix.AF_INET6,
			args: "2001:db8::5/64 brd + dev d0", expected: refusedSlug("brd_plus_v6")},
		{description: "negative: brd + with no peer or local to compute from", verb: "add", family: unix.AF_INET,
			args: "brd + dev d0", expected: notImpl("no peer or local address")},

		// Refused at or after the lookup.
		{description: "negative: an unknown device", verb: "add", args: p24 + " dev nosuch",
			expected: refusedSlug("nodev", "nosuch")},
		{description: "negative: a resolver failure is returned", verb: "add", args: p24 + " dev boom",
			expected: addrWriteExpected{err: errFakeResolve, resolved: []string{"boom"}}},
		{description: "negative: preferred longer than valid (18)", verb: "add",
			args:     p24 + " valid_lft 60 preferred_lft 120 dev d0",
			expected: invalid("preferred_lft is greater than valid_lft", "d0")},
		{description: "negative: a zero valid lifetime (19)", verb: "add", args: p24 + " valid_lft 0 dev d0",
			expected: refusedSlug("valid_zero", "d0")},
		{description: "negative: autojoin on a unicast address (22)", verb: "add", args: p24 + " autojoin dev d0",
			expected: refusedSlug("autojoin_unicast", "d0")},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			verb := lookupAddrWriteVerb(tc.verb)
			if verb == nil {
				t.Fatalf("no verb %q", tc.verb)
			}
			exp := tc.expected
			if exp.slug != "" {
				b, err := os.ReadFile(filepath.Join(addrWriteCorpusDir, exp.slug, "ip.err"))
				if err != nil {
					t.Fatal(err)
				}
				if exp.err != nil {
					exp.errMsg = strings.TrimSuffix(string(b), "\n")
				} else {
					exp.warning = string(b)
				}
			}
			links := &fakeLinks{idx: map[string]int32{"d0": 3}}
			var warn bytes.Buffer
			got, err := parseAddrWriteArgs(verb, tc.family, strings.Fields(tc.args), links.resolve, &warn)
			if !equalStrings(links.calls, exp.resolved) {
				t.Errorf("resolved %q, want %q", links.calls, exp.resolved)
			}
			if exp.err != nil {
				if !errors.Is(err, exp.err) || !strings.Contains(err.Error(), exp.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, exp.err, exp.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if warn.String() != exp.warning {
				t.Errorf("warnings %q, want %q", warn.String(), exp.warning)
			}
			if !equalAddrWrite(got, exp.write) {
				t.Errorf("got  %+v\nwant %+v", got, exp.write)
			}
		})
	}
}

// equalAddrWrite compares two AddrWrites field by field, so a nil and an
// empty attribute list are the same, as they are on the wire.
func equalAddrWrite(a, b xtcpnl.AddrWrite) bool {
	if a.MsgType != b.MsgType || a.Flags != b.Flags || a.Family != b.Family || a.Prefixlen != b.Prefixlen ||
		a.Scope != b.Scope || a.Index != b.Index || a.IfaFlags != b.IfaFlags {
		return false
	}
	return equalAddrAttrs(a.Attrs, b.Attrs) && equalAddrAttrs(a.PostAttrs, b.PostAttrs)
}

func equalAddrAttrs(a, b []xtcpnl.AddrAttr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Type != b[i].Type || !bytes.Equal(a[i].Payload, b[i].Payload) {
			return false
		}
	}
	return true
}
