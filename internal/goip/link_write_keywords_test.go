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
