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
