package goip

import (
	"fmt"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestLinkWriteAddrLen is iplink_parse's address-length check on a set
// (ip/iplink.c:1279): nl_get_ll_addr_len looks dev up, gets it by index, and
// a reply whose IFLA_ADDRESS length differs is refused, exit 1. No index, a
// refused get or no address skips the check. The fake get by index is
// listed as "get N", with its family when it isn't AF_UNSPEC. GW2-T01 in goip-write-keywords.md.
//
// go test ./internal/goip/ -run TestLinkWriteAddrLen
func TestLinkWriteAddrLen(t *testing.T) {
	addr := func(b ...byte) xtcpnl.LinkAttr { return xtcpnl.LinkAttrBytes(unix.IFLA_ADDRESS, b) }
	set := func(index int32, resolved []string, a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: index, Attrs: a,
		}, resolved: resolved}
	}
	invalid := func(msg string, resolved ...string) linkWriteExpected {
		return linkWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	d0 := []string{"d0", "get 2"}
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: a matching length is sent", verb: "set", args: []string{"dev", "d0", "address", "02:00:00:00:00:aa"},
			expected: set(2, d0, addr(2, 0, 0, 0, 0, 0xaa))},
		{description: "positive: a multicast address is sent; the kernel refuses it", verb: "set",
			args: []string{"dev", "d0", "address", "01:00:00:00:00:01"}, expected: set(2, d0, addr(1, 0, 0, 0, 0, 1))},
		{description: "positive: the -6 preference is the family of the get", verb: "set", family: unix.AF_INET6,
			args:     []string{"dev", "d0", "address", "02:00:00:00:00:aa"},
			expected: linkWithFamily(set(2, []string{"d0", "get 2 family 10"}, addr(2, 0, 0, 0, 0, 0xaa)), unix.AF_INET6)},
		{description: "negative: a short address", verb: "set", args: []string{"dev", "d0", "address", "02:00"},
			expected: invalid("Invalid address length 2 - must be 6 bytes", d0...)},
		{description: "negative: a long address", verb: "set", args: []string{"dev", "d0", "address", "02:00:00:00:00:00:01"},
			expected: invalid("Invalid address length 7 - must be 6 bytes", d0...)},
		{description: "negative: the get fails other than by the kernel", verb: "set",
			args:     []string{"dev", "v0", "address", "02:00"},
			expected: linkWriteExpected{err: errFakeResolve, errMsg: "fake", resolved: []string{"v0", "get 6"}}},
		{description: "boundary: a device with no address is unchecked", verb: "set", args: []string{"dev", "la1", "address", "02:00"},
			expected: set(3, []string{"la1", "get 3"}, addr(2, 0))},
		{description: "boundary: a refused get is unchecked", verb: "set", args: []string{"dev", "la2", "address", "02:00"},
			expected: set(5, []string{"la2", "get 5"}, addr(2, 0))},
		{description: "boundary: a one-byte address against six", verb: "set", args: []string{"dev", "d0", "address", "02"},
			expected: invalid("Invalid address length 1 - must be 6 bytes", d0...)},
		{description: "corner: add doesn't look", verb: "add", args: []string{"d9", "address", "02:00", "type", "dummy"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK, Kind: "dummy",
				Attrs: []xtcpnl.LinkAttr{addr(2, 0), xtcpnl.LinkAttrString(unix.IFLA_IFNAME, "d9")},
			}}},
		{description: "corner: no dev, no lookup", verb: "set", args: []string{"address", "02:00"},
			expected: invalid(`"dev" argument is required`)},
		{description: "corner: no such device is looked up twice, as ip's uncached miss", verb: "set",
			args: []string{"dev", "nosuch", "address", "02:00"}, expected: invalid(`Cannot find device "nosuch"`, "nosuch", "nosuch")},
		{description: "corner: no address, no get", verb: "set", args: []string{"dev", "d0", "mtu", "1400"},
			expected: set(2, []string{"d0"}, xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400))},
	})
}

// linkWithFamily is e with its LinkWrite's ifi_family set.
func linkWithFamily(e linkWriteExpected, family uint8) linkWriteExpected {
	e.write.Family = family
	return e
}

// linkWithFlags is e with its LinkWrite's ifi_flags and ifi_change set.
func linkWithFlags(e linkWriteExpected, flags, change uint32) linkWriteExpected {
	e.write.IfFlags, e.write.IfChange = flags, change
	return e
}

// TestLinkWriteGroup is iplink_parse's group arm (ip/iplink.c:1102):
// rtnl_group_a2n, default or strtol in base 0 into an int, refused when
// negative, and IFLA_GROUP u32; duparg on a second. With no dev it is a
// group-wide set (:1299), ifi_index 0. GW2-T02.
//
// go test ./internal/goip/ -run TestLinkWriteGroup
func TestLinkWriteGroup(t *testing.T) {
	group := func(g uint32) xtcpnl.LinkAttr { return xtcpnl.LinkAttrU32(unix.IFLA_GROUP, g) }
	set := func(index int32, resolved []string, a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: index, Attrs: a,
		}, resolved: resolved}
	}
	d0 := []string{"d0"}
	usage := func(msg string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage, errMsg: msg}
	}
	invalid := `Error: argument %q is wrong: Invalid "group" value`
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: a number", verb: "set", args: []string{"dev", "d0", "group", "5"}, expected: set(2, d0, group(5))},
		{description: "positive: the stock name", verb: "set", args: []string{"dev", "d0", "group", "default"},
			expected: set(2, d0, group(0))},
		{description: "positive: hex, base 0", verb: "set", args: []string{"dev", "d0", "group", "0x10"}, expected: set(2, d0, group(16))},
		{description: "positive: octal, base 0", verb: "set", args: []string{"dev", "d0", "group", "010"}, expected: set(2, d0, group(8))},
		{description: "positive: in word order", verb: "set", args: []string{"dev", "d0", "mtu", "1400", "group", "5"},
			expected: set(2, d0, xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400), group(5))},
		{description: "positive: a group-wide set has no dev and index 0", verb: "set", args: []string{"group", "5", "up"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, IfFlags: unix.IFF_UP, IfChange: unix.IFF_UP,
				Attrs: []xtcpnl.LinkAttr{group(5)},
			}}},
		{description: "negative: a word", verb: "set", args: []string{"dev", "d0", "group", "nosuch"},
			expected: usage(fmt.Sprintf(invalid, "nosuch"))},
		{description: "negative: negative", verb: "set", args: []string{"dev", "d0", "group", "-1"},
			expected: usage(fmt.Sprintf(invalid, "-1"))},
		{description: "negative: trailing garbage", verb: "set", args: []string{"dev", "d0", "group", "5x"},
			expected: usage(fmt.Sprintf(invalid, "5x"))},
		{description: "negative: a second group", verb: "set", args: []string{"dev", "d0", "group", "1", "group", "2"},
			expected: usage(`duplicate "group": "2" is the second value`)},
		{description: "negative: a group-wide create", verb: "add", args: []string{"group", "5", "type", "dummy"},
			expected: usage("group cannot be used when creating devices")},
		{description: "negative: kind words on a group-wide set", verb: "set", args: []string{"group", "5", "type", "bond", "mode", "1"},
			expected: usage(`Garbage instead of arguments "mode ..."`)},
		{description: "boundary: the largest", verb: "set", args: []string{"dev", "d0", "group", "2147483647"},
			expected: set(2, d0, group(0x7fffffff))},
		{description: "boundary: one past, which wraps negative", verb: "set", args: []string{"dev", "d0", "group", "2147483648"},
			expected: usage(fmt.Sprintf(invalid, "2147483648"))},
		{description: "boundary: zero", verb: "set", args: []string{"dev", "d0", "group", "0"}, expected: set(2, d0, group(0))},
		{description: "corner: 2^32 wraps to 0", verb: "set", args: []string{"dev", "d0", "group", "4294967296"},
			expected: set(2, d0, group(0))},
		{description: "corner: 2^32+5 wraps to 5", verb: "set", args: []string{"dev", "d0", "group", "4294967301"},
			expected: set(2, d0, group(5))},
		{description: "corner: past LONG_MAX clamps, and LONG_MAX's low int is -1", verb: "set",
			args: []string{"dev", "d0", "group", "99999999999999999999"}, expected: usage(fmt.Sprintf(invalid, "99999999999999999999"))},
		{description: "corner: group is strcmp, so grou is the dev", verb: "set", args: []string{"grou", "group", "5"},
			expected: linkWriteExpected{err: ErrInvalidArgument, errMsg: `Cannot find device "grou"`, resolved: []string{"grou"}}},
		{description: "corner: names are strcmp", verb: "set", args: []string{"dev", "d0", "group", "def"},
			expected: usage(fmt.Sprintf(invalid, "def"))},
		{description: "corner: a type with no words is dropped on a group-wide set", verb: "set",
			args: []string{"group", "5", "type", "dummy"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Attrs: []xtcpnl.LinkAttr{group(5)},
			}}},
	})
}

// TestLinkWriteGsoGro is iplink_parse's five size arms (ip/iplink.c:1204-1249):
// get_unsigned in base 0, gso_max_segs at most GSO_MAX_SEGS, each its own
// IFLA_* u32, with no duparg. GW2-T03.
//
// go test ./internal/goip/ -run TestLinkWriteGsoGro
func TestLinkWriteGsoGro(t *testing.T) {
	u32 := xtcpnl.LinkAttrU32
	set := func(a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a,
		}, resolved: []string{"d0"}}
	}
	d0 := func(w ...string) []string { return append([]string{"dev", "d0"}, w...) }
	invalid := func(key, v string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage, errMsg: fmt.Sprintf(`Error: argument %q is wrong: Invalid %q value`, v, key)}
	}
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: gso_max_size", verb: "set", args: d0("gso_max_size", "32768"),
			expected: set(u32(unix.IFLA_GSO_MAX_SIZE, 32768))},
		{description: "positive: gso_max_segs", verb: "set", args: d0("gso_max_segs", "100"),
			expected: set(u32(unix.IFLA_GSO_MAX_SEGS, 100))},
		{description: "positive: gro_max_size", verb: "set", args: d0("gro_max_size", "524280"),
			expected: set(u32(unix.IFLA_GRO_MAX_SIZE, 524280))},
		{description: "positive: gso_ipv4_max_size", verb: "set", args: d0("gso_ipv4_max_size", "32768"),
			expected: set(u32(unix.IFLA_GSO_IPV4_MAX_SIZE, 32768))},
		{description: "positive: gro_ipv4_max_size", verb: "set", args: d0("gro_ipv4_max_size", "32768"),
			expected: set(u32(unix.IFLA_GRO_IPV4_MAX_SIZE, 32768))},
		{description: "positive: in word order", verb: "set", args: d0("gso_max_segs", "1", "gro_max_size", "2"),
			expected: set(u32(unix.IFLA_GSO_MAX_SEGS, 1), u32(unix.IFLA_GRO_MAX_SIZE, 2))},
		{description: "positive: hex, base 0", verb: "set", args: d0("gso_max_size", "0x10"),
			expected: set(u32(unix.IFLA_GSO_MAX_SIZE, 16))},
		{description: "negative: a word", verb: "set", args: d0("gso_max_size", "x"), expected: invalid("gso_max_size", "x")},
		{description: "negative: negative", verb: "set", args: d0("gro_max_size", "-1"), expected: invalid("gro_max_size", "-1")},
		{description: "negative: no value", verb: "set", args: d0("gso_max_segs"),
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: "Command line is not complete"}},
		{description: "boundary: segs at its limit", verb: "set", args: d0("gso_max_segs", "65535"),
			expected: set(u32(unix.IFLA_GSO_MAX_SEGS, 65535))},
		{description: "boundary: segs one past", verb: "set", args: d0("gso_max_segs", "65536"),
			expected: invalid("gso_max_segs", "65536")},
		{description: "boundary: a size at UINT_MAX", verb: "set", args: d0("gso_max_size", "4294967295"),
			expected: set(u32(unix.IFLA_GSO_MAX_SIZE, 4294967295))},
		{description: "boundary: a size one past", verb: "set", args: d0("gso_ipv4_max_size", "4294967296"),
			expected: invalid("gso_ipv4_max_size", "4294967296")},
		{description: "boundary: zero", verb: "set", args: d0("gro_ipv4_max_size", "0"),
			expected: set(u32(unix.IFLA_GRO_IPV4_MAX_SIZE, 0))},
		{description: "corner: no prefix match, so gso_max is the dev", verb: "set", args: []string{"d0", "gso_max", "5"},
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: `either "dev" is duplicate, or "gso_max" is garbage`}},
		{description: "corner: repeated, with no duparg", verb: "set", args: d0("gso_max_size", "1", "gso_max_size", "2"),
			expected: set(u32(unix.IFLA_GSO_MAX_SIZE, 1), u32(unix.IFLA_GSO_MAX_SIZE, 2))},
	})
}

// TestLinkWriteAddrGenMode is iplink_parse's addrgenmode arm
// (ip/iplink.c:1144): get_addr_gen_mode's four names by strcasecmp, sent as
// IFLA_AF_SPEC{AF_INET6{IFLA_INET6_ADDR_GEN_MODE u8}} in argument order,
// with no duparg. GW2-T04.
//
// go test ./internal/goip/ -run TestLinkWriteAddrGenMode
func TestLinkWriteAddrGenMode(t *testing.T) {
	mode := func(m uint8) xtcpnl.LinkAttr {
		a, err := xtcpnl.LinkAttrAddrGenMode(m)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	set := func(a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a,
		}, resolved: []string{"d0"}}
	}
	d0 := func(w ...string) []string { return append([]string{"dev", "d0"}, w...) }
	invalid := func(v string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage,
			errMsg: fmt.Sprintf(`Error: argument %q is wrong: Invalid address generation mode`, v)}
	}
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: eui64", verb: "set", args: d0("addrgenmode", "eui64"),
			expected: set(mode(xtcpnl.In6AddrGenModeEUI64))},
		{description: "positive: none", verb: "set", args: d0("addrgenmode", "none"),
			expected: set(mode(xtcpnl.In6AddrGenModeNone))},
		{description: "positive: stable_secret", verb: "set", args: d0("addrgenmode", "stable_secret"),
			expected: set(mode(xtcpnl.In6AddrGenModeStablePrivacy))},
		{description: "positive: random", verb: "set", args: d0("addrgenmode", "random"),
			expected: set(mode(xtcpnl.In6AddrGenModeRandom))},
		{description: "positive: the nest's bytes", verb: "set", args: d0("addrgenmode", "none"),
			expected: set(xtcpnl.LinkAttrBytes(unix.IFLA_AF_SPEC, []byte{
				0x0c, 0, unix.AF_INET6, 0, 5, 0, unix.IFLA_INET6_ADDR_GEN_MODE, 0, 1, 0, 0, 0}))},
		{description: "positive: in word order", verb: "set", args: d0("mtu", "1400", "addrgenmode", "none", "txqueuelen", "5"),
			expected: set(xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400), mode(xtcpnl.In6AddrGenModeNone),
				xtcpnl.LinkAttrU32(unix.IFLA_TXQLEN, 5))},
		{description: "negative: an unknown mode", verb: "set", args: d0("addrgenmode", "stable"), expected: invalid("stable")},
		{description: "negative: a number", verb: "set", args: d0("addrgenmode", "1"), expected: invalid("1")},
		{description: "negative: no value", verb: "set", args: d0("addrgenmode"),
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: "Command line is not complete"}},
		{description: "boundary: upper case", verb: "set", args: d0("addrgenmode", "NONE"),
			expected: set(mode(xtcpnl.In6AddrGenModeNone))},
		{description: "boundary: mixed case", verb: "set", args: d0("addrgenmode", "Stable_Secret"),
			expected: set(mode(xtcpnl.In6AddrGenModeStablePrivacy))},
		{description: "boundary: a non-ASCII fold, which strcasecmp does not make", verb: "set",
			args: d0("addrgenmode", "ſtable_secret"), expected: invalid("ſtable_secret")},
		{description: "corner: the prefix", verb: "set", args: d0("addrgen", "none"),
			expected: set(mode(xtcpnl.In6AddrGenModeNone))},
		{description: "corner: the shortest prefix after address", verb: "set", args: d0("addrg", "random"),
			expected: set(mode(xtcpnl.In6AddrGenModeRandom))},
		{description: "corner: twice is two nests, no duparg", verb: "set", args: d0("addrgenmode", "none", "addrgenmode", "random"),
			expected: set(mode(xtcpnl.In6AddrGenModeNone), mode(xtcpnl.In6AddrGenModeRandom))},
		{description: "corner: on add, before the kind", verb: "add", args: []string{"d9", "addrgenmode", "none", "type", "dummy"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{
				MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK, Kind: "dummy",
				Attrs: []xtcpnl.LinkAttr{mode(xtcpnl.In6AddrGenModeNone), xtcpnl.LinkAttrString(unix.IFLA_IFNAME, "d9")},
			}}},
	})
}

// TestLinkWriteProtodown is iplink_parse's protodown and protodown_reason
// arms (ip/iplink.c:1173-1203): parse_on_off to IFLA_PROTO_DOWN u8, and a
// reason by protodown_reason_a2n's strtoul below 32, then on or off by
// matches(), as IFLA_PROTO_DOWN_REASON|NLA_F_NESTED{MASK, VALUE}. GW2-T05.
//
// go test ./internal/goip/ -run TestLinkWriteProtodown
func TestLinkWriteProtodown(t *testing.T) {
	down := func(v uint8) xtcpnl.LinkAttr { return xtcpnl.LinkAttrU8(unix.IFLA_PROTO_DOWN, v) }
	reason := func(mask, value uint32) xtcpnl.LinkAttr {
		a, err := xtcpnl.LinkAttrProtoDownReason(mask, value)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	set := func(a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a,
		}, resolved: []string{"d0"}}
	}
	d0 := func(w ...string) []string { return append([]string{"dev", "d0"}, w...) }
	badReason := func(v string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage,
			errMsg: fmt.Sprintf(`Error: argument %q is wrong: invalid protodown reason`, v)}
	}
	notOnOff := func(v string) linkWriteExpected {
		return linkWriteExpected{err: ErrInvalidArgument,
			errMsg: fmt.Sprintf(`Error: argument of "protodown_reason" must be "on" or "off", not %q`, v)}
	}
	incomplete := linkWriteExpected{err: ErrLinkUsage, errMsg: "Command line is not complete"}
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: protodown on", verb: "set", args: d0("protodown", "on"), expected: set(down(1))},
		{description: "positive: protodown off", verb: "set", args: d0("protodown", "off"), expected: set(down(0))},
		{description: "positive: a reason on", verb: "set", args: d0("protodown_reason", "0", "on"), expected: set(reason(1, 1))},
		{description: "positive: a reason off", verb: "set", args: d0("protodown_reason", "0", "off"), expected: set(reason(1, 0))},
		{description: "positive: the reason nest's bytes", verb: "set", args: d0("protodown_reason", "0", "on"),
			expected: set(xtcpnl.LinkAttrBytes(unix.IFLA_PROTO_DOWN_REASON|unix.NLA_F_NESTED,
				[]byte{8, 0, 1, 0, 1, 0, 0, 0, 8, 0, 2, 0, 1, 0, 0, 0}))},
		{description: "positive: in word order, then up", verb: "set", args: d0("protodown", "on", "protodown_reason", "3", "on", "up"),
			expected: linkWithFlags(set(down(1), reason(8, 8)), unix.IFF_UP, unix.IFF_UP)},
		{description: "positive: a hex reason, base 0", verb: "set", args: d0("protodown_reason", "0x1f", "on"),
			expected: set(reason(0x80000000, 0x80000000))},
		{description: "negative: a bad on/off", verb: "set", args: d0("protodown", "maybe"),
			expected: linkWriteExpected{err: ErrInvalidArgument,
				errMsg: `Error: argument of "protodown" must be one of "off", "on", not "maybe"`}},
		{description: "negative: protodown by prefix is a dev", verb: "set", args: []string{"d0", "protod", "on"},
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: `either "dev" is duplicate, or "protod" is garbage`}},
		{description: "negative: reason 32", verb: "set", args: d0("protodown_reason", "32", "on"), expected: badReason("32")},
		{description: "negative: a reason name, with no protodown_reasons.d", verb: "set",
			args: d0("protodown_reason", "lacp", "on"), expected: badReason("lacp")},
		{description: "negative: a negative reason is ULONG_MAX", verb: "set", args: d0("protodown_reason", "-1", "on"), expected: badReason("-1")},
		{description: "negative: trailing junk", verb: "set", args: d0("protodown_reason", "1x", "on"), expected: badReason("1x")},
		{description: "negative: no on/off word", verb: "set", args: d0("protodown_reason", "1", "up"), expected: notOnOff("up")},
		{description: "negative: no reason", verb: "set", args: d0("protodown_reason"), expected: incomplete},
		{description: "negative: no on/off after the reason", verb: "set", args: d0("protodown_reason", "1"), expected: incomplete},
		{description: "negative: a bad reason is refused before the missing word", verb: "set",
			args: d0("protodown_reason", "32"), expected: badReason("32")},
		{description: "boundary: reason 31", verb: "set", args: d0("protodown_reason", "31", "on"),
			expected: set(reason(0x80000000, 0x80000000))},
		{description: "boundary: octal 037 is 31", verb: "set", args: d0("protodown_reason", "037", "off"),
			expected: set(reason(0x80000000, 0))},
		{description: "boundary: an empty reason", verb: "set", args: d0("protodown_reason", "", "on"), expected: badReason("")},
		{description: "corner: o is on by prefix", verb: "set", args: d0("protodown_reason", "2", "o"), expected: set(reason(4, 4))},
		{description: "corner: of is off by prefix", verb: "set", args: d0("protodown_reason", "2", "of"), expected: set(reason(4, 0))},
		{description: "corner: an empty word matches on", verb: "set", args: d0("protodown_reason", "2", ""), expected: set(reason(4, 4))},
		{description: "corner: onn is neither", verb: "set", args: d0("protodown_reason", "2", "onn"), expected: notOnOff("onn")},
		{description: "corner: protodown takes no prefix of on", verb: "set", args: d0("protodown", "o"),
			expected: linkWriteExpected{err: ErrInvalidArgument,
				errMsg: `Error: argument of "protodown" must be one of "off", "on", not "o"`}},
		{description: "corner: -0 is reason 0", verb: "set", args: d0("protodown_reason", "-0", "on"), expected: set(reason(1, 1))},
		{description: "corner: twice is two nests, no duparg", verb: "set",
			args:     d0("protodown_reason", "0", "on", "protodown_reason", "1", "off"),
			expected: set(reason(1, 1), reason(2, 0))},
	})
}

// TestLinkWriteBrdCarrierModeState is iplink_parse's broadcast, carrier, mode
// and state arms (ip/iplink.c:921, 1027, 1109, 1117): ll_addr_a2n to
// IFLA_BROADCAST; parse_on_off to IFLA_CARRIER u8; and get_link_mode and
// get_operstate by strcasecmp to IFLA_LINKMODE and IFLA_OPERSTATE u8s.
// GW2-T06.
//
// go test ./internal/goip/ -run TestLinkWriteBrdCarrierModeState
func TestLinkWriteBrdCarrierModeState(t *testing.T) {
	u8 := xtcpnl.LinkAttrU8
	brd := func(b ...byte) xtcpnl.LinkAttr { return xtcpnl.LinkAttrBytes(unix.IFLA_BROADCAST, b) }
	set := func(a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a,
		}, resolved: []string{"d0"}}
	}
	d0 := func(w ...string) []string { return append([]string{"dev", "d0"}, w...) }
	invarg := func(v, msg string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage, errMsg: fmt.Sprintf(`Error: argument %q is wrong: %s`, v, msg)}
	}
	ff := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xfe}
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: broadcast", verb: "set", args: d0("broadcast", "ff:ff:ff:ff:ff:fe"), expected: set(brd(ff...))},
		{description: "positive: brd", verb: "set", args: d0("brd", "ff:ff:ff:ff:ff:fe"), expected: set(brd(ff...))},
		{description: "positive: carrier on", verb: "set", args: d0("carrier", "on"), expected: set(u8(unix.IFLA_CARRIER, 1))},
		{description: "positive: carrier off", verb: "set", args: d0("carrier", "off"), expected: set(u8(unix.IFLA_CARRIER, 0))},
		{description: "positive: mode dormant", verb: "set", args: d0("mode", "dormant"), expected: set(u8(unix.IFLA_LINKMODE, 1))},
		{description: "positive: mode default", verb: "set", args: d0("mode", "default"), expected: set(u8(unix.IFLA_LINKMODE, 0))},
		{description: "positive: state dormant", verb: "set", args: d0("state", "dormant"), expected: set(u8(unix.IFLA_OPERSTATE, 5))},
		{description: "positive: state up", verb: "set", args: d0("state", "up"), expected: set(u8(unix.IFLA_OPERSTATE, 6))},
		{description: "positive: in word order", verb: "set", args: d0("mode", "dormant", "carrier", "off", "state", "testing"),
			expected: set(u8(unix.IFLA_LINKMODE, 1), u8(unix.IFLA_CARRIER, 0), u8(unix.IFLA_OPERSTATE, 4))},
		{description: "negative: carrier maybe", verb: "set", args: d0("carrier", "maybe"),
			expected: linkWriteExpected{err: ErrInvalidArgument,
				errMsg: `Error: argument of "carrier" must be one of "off", "on", not "maybe"`}},
		{description: "negative: mode x", verb: "set", args: d0("mode", "x"), expected: invarg("x", "Invalid link mode")},
		{description: "negative: state x", verb: "set", args: d0("state", "x"), expected: invarg("x", "Invalid operstate")},
		{description: "negative: a bad broadcast", verb: "set", args: d0("broadcast", "zz"),
			expected: linkWriteExpected{err: ErrInvalidArgument, errMsg: `"zz" is invalid lladdr`}},
		{description: "negative: mode by prefix is a dev", verb: "set", args: []string{"d0", "mod", "dormant"},
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: `either "dev" is duplicate, or "mod" is garbage`}},
		{description: "negative: no value", verb: "set", args: d0("state"),
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: "Command line is not complete"}},
		{description: "boundary: mode DORMANT", verb: "set", args: d0("mode", "DORMANT"), expected: set(u8(unix.IFLA_LINKMODE, 1))},
		{description: "boundary: state UP", verb: "set", args: d0("state", "UP"), expected: set(u8(unix.IFLA_OPERSTATE, 6))},
		{description: "boundary: state unknown, the first", verb: "set", args: d0("state", "unknown"), expected: set(u8(unix.IFLA_OPERSTATE, 0))},
		{description: "boundary: state LowerLayerDown", verb: "set", args: d0("state", "LowerLayerDown"),
			expected: set(u8(unix.IFLA_OPERSTATE, 3))},
		{description: "boundary: a one-byte broadcast", verb: "set", args: d0("broadcast", "ff"), expected: set(brd(0xff))},
		{description: "corner: b is broadcast", verb: "set", args: d0("b", "ff:ff:ff:ff:ff:fe"), expected: set(brd(ff...))},
		{description: "corner: br is broadcast by prefix", verb: "set", args: d0("br", "ff"), expected: set(brd(0xff))},
		{description: "corner: brdx is no keyword, so a dev", verb: "set", args: []string{"d0", "brdx", "ff"},
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: `either "dev" is duplicate, or "brdx" is garbage`}},
		{description: "corner: broadcast's length isn't checked against the device", verb: "set", args: d0("broadcast", "ff:ff"),
			expected: set(brd(0xff, 0xff))},
		{description: "corner: carrier takes no prefix of on", verb: "set", args: d0("carrier", "o"),
			expected: linkWriteExpected{err: ErrInvalidArgument,
				errMsg: `Error: argument of "carrier" must be one of "off", "on", not "o"`}},
		{description: "corner: a state number is not a name", verb: "set", args: d0("state", "6"), expected: invarg("6", "Invalid operstate")},
		{description: "corner: twice is two attributes, no duparg", verb: "set", args: d0("state", "up", "state", "down"),
			expected: set(u8(unix.IFLA_OPERSTATE, 6), u8(unix.IFLA_OPERSTATE, 2))},
	})
}

// TestLinkWriteXdp is iplink_parse's xdp arms (ip/iplink.c:949) through
// xdp_parse: off or none as the last word is IFLA_XDP{FD -1, FLAGS}, with
// UPDATE_IF_NOEXIST (goip refuses -force) and the mode's bit; xdpoffload
// looks dev up first. Any other word is a program load, not implemented.
// GW2-T07.
//
// go test ./internal/goip/ -run TestLinkWriteXdp
func TestLinkWriteXdp(t *testing.T) {
	xdp := func(flags uint32) xtcpnl.LinkAttr {
		a, err := xtcpnl.LinkAttrXdp(-1, flags)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	set := func(a ...xtcpnl.LinkAttr) linkWriteExpected {
		return linkWriteExpected{write: xtcpnl.LinkWrite{
			MsgType: unix.RTM_NEWLINK, Flags: unix.NLM_F_ACK, Index: 2, Attrs: a,
		}, resolved: []string{"d0"}}
	}
	d0 := func(w ...string) []string { return append([]string{"dev", "d0"}, w...) }
	notImpl := func(key string) linkWriteExpected {
		return linkWriteExpected{err: ErrNotImplemented, errMsg: fmt.Sprintf("%q: an XDP program", key)}
	}
	incomplete := func(resolved ...string) linkWriteExpected {
		return linkWriteExpected{err: ErrLinkUsage, errMsg: "Command line is not complete", resolved: resolved}
	}
	const noexist = unix.XDP_FLAGS_UPDATE_IF_NOEXIST
	runLinkWriteRows(t, []linkWriteRow{
		{description: "positive: xdp off", verb: "set", args: d0("xdp", "off"), expected: set(xdp(noexist))},
		{description: "positive: xdp none", verb: "set", args: d0("xdp", "none"), expected: set(xdp(noexist))},
		{description: "positive: xdpgeneric none", verb: "set", args: d0("xdpgeneric", "none"),
			expected: set(xdp(noexist | unix.XDP_FLAGS_SKB_MODE))},
		{description: "positive: xdpdrv off", verb: "set", args: d0("xdpdrv", "off"),
			expected: set(xdp(noexist | unix.XDP_FLAGS_DRV_MODE))},
		{description: "positive: xdpoffload off looks d0 up once", verb: "set", args: d0("xdpoffload", "off"),
			expected: set(xdp(noexist | unix.XDP_FLAGS_HW_MODE))},
		{description: "positive: the nest's bytes", verb: "set", args: d0("xdp", "off"),
			expected: set(xtcpnl.LinkAttrBytes(unix.IFLA_XDP, []byte{8, 0, 1, 0, 0xff, 0xff, 0xff, 0xff, 8, 0, 3, 0, 1, 0, 0, 0}))},
		{description: "positive: after other words", verb: "set", args: d0("mtu", "1400", "xdp", "off"),
			expected: set(xtcpnl.LinkAttrU32(unix.IFLA_MTU, 1400), xdp(noexist))},
		{description: "negative: off not last is a program", verb: "set", args: d0("xdp", "off", "carrier", "on"), expected: notImpl("xdp")},
		{description: "negative: an object file", verb: "set", args: d0("xdpdrv", "obj", "prog.o"), expected: notImpl("xdpdrv")},
		{description: "negative: no value", verb: "set", args: d0("xdp"), expected: incomplete()},
		{description: "negative: xdpoffload with no dev yet", verb: "set", args: []string{"xdpoffload", "off"}, expected: incomplete()},
		{description: "negative: xdpoffload on a missing dev", verb: "set", args: []string{"dev", "nosuch", "xdpoffload", "off"},
			expected: incomplete("nosuch")},
		{description: "negative: xdpoffload looks up before the program", verb: "set", args: []string{"dev", "nosuch", "xdpoffload", "obj", "p.o"},
			expected: incomplete("nosuch")},
		{description: "boundary: off as the only word after dev", verb: "set", args: []string{"d0", "xdpgeneric", "off"},
			expected: set(xdp(noexist | unix.XDP_FLAGS_SKB_MODE))},
		{description: "corner: OFF is strcmp, so a program", verb: "set", args: d0("xdp", "OFF"), expected: notImpl("xdp")},
		{description: "corner: of is no prefix of off, so a program", verb: "set", args: d0("xdp", "of"), expected: notImpl("xdp")},
		{description: "corner: xd is no keyword, so a dev", verb: "set", args: []string{"d0", "xd", "off"},
			expected: linkWriteExpected{err: ErrLinkUsage, errMsg: `either "dev" is duplicate, or "xd" is garbage`}},
		{description: "corner: twice, the first isn't last", verb: "set", args: d0("xdp", "off", "xdp", "off"), expected: notImpl("xdp")},
		{description: "corner: xdpoffload by a bare dev name", verb: "set", args: []string{"d0", "xdpoffload", "none"},
			expected: set(xdp(noexist | unix.XDP_FLAGS_HW_MODE))},
	})
}
