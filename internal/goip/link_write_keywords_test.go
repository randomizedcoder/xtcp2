package goip

import (
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
