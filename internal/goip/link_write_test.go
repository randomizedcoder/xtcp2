package goip

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestLookupLinkWriteVerb is do_iplink's chain: matches() in the order
// add, set, change, replace, delete, then the show verbs.
//
// go test ./internal/goip/ -run TestLookupLinkWriteVerb
func TestLookupLinkWriteVerb(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    string // the verb, "" for not a write verb
	}{
		{description: "positive: add", arg: "add", expected: "add"},
		{description: "positive: a is add", arg: "a", expected: "add"},
		{description: "positive: set", arg: "set", expected: "set"},
		{description: "corner: s is set, because set is tried before show", arg: "s", expected: "set"},
		{description: "positive: change", arg: "change", expected: "change"},
		{description: "positive: c is change", arg: "c", expected: "change"},
		{description: "positive: r is replace", arg: "r", expected: "replace"},
		{description: "positive: d is delete", arg: "d", expected: "delete"},
		{description: "positive: del is delete", arg: "del", expected: "delete"},
		{description: "negative: sh is show, not a write", arg: "sh"},
		{description: "negative: l is list", arg: "l"},
		{description: "negative: x is xstats", arg: "x"},
		{description: "boundary: a longer word than the verb is not it", arg: "adds"},
		{description: "corner: case-sensitive", arg: "ADD"},
		{description: "corner: empty matches nothing here, where ip's matches() would take add", arg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got string
			if v := lookupLinkWriteVerb(tc.arg); v != nil {
				got = v.name
			}
			if got != tc.expected {
				t.Errorf("lookupLinkWriteVerb(%q) = %q, want %q", tc.arg, got, tc.expected)
			}
		})
	}
}

// fakeLinks answers ll_name_to_index for the parser tests and records the
// order names were asked for, which is part of parity: `ip` resolves
// master before dev, and kind options after both.
type fakeLinks struct {
	idx   map[string]int32
	calls []string
}

var errFakeResolve = errors.New("fake resolver failure")

func newFakeLinks() *fakeLinks {
	return &fakeLinks{idx: map[string]int32{
		"d0": 2, "la1": 3, "bond0": 4, "la2": 5, "v0": 6,
		strings.Repeat("x", altIfNameSizeCst-1): 9,
	}}
}

func (f *fakeLinks) resolve(name string) (int32, error) {
	f.calls = append(f.calls, name)
	if name == "boom" {
		return 0, errFakeResolve
	}
	return f.idx[name], nil
}

// linkWriteExpected is a row's outcome: the LinkWrite and the names
// resolved, or an error class and a fragment of iproute2's message.
type linkWriteExpected struct {
	write    xtcpnl.LinkWrite
	resolved []string
	err      error
	errMsg   string
}

func ifinfomsg(index int32, flags, change uint32) []byte {
	return xtcpnl.EncodeIfInfomsg(unix.AF_UNSPEC, index, flags, change)
}

func strz(s string) []byte { return append([]byte(s), 0) }

// TestBuildLinkWrite is argv after the verb, through iplink_parse and
// iplink_modify, into the LinkWrite the encoder takes. It carries
// link-write-path.md table 2's argv rows (8, 9, 14, 15) and table 4 row 5,
// which Phase 5 left for the parser.
//
// go test ./internal/goip/ -run TestBuildLinkWrite
func TestBuildLinkWrite(t *testing.T) {
	const (
		newlink = uint16(unix.RTM_NEWLINK)
		dellink = uint16(unix.RTM_DELLINK)
		ack     = uint16(unix.NLM_F_ACK)
		addF    = uint16(unix.NLM_F_CREATE|unix.NLM_F_EXCL) | ack
		replF   = uint16(unix.NLM_F_CREATE|unix.NLM_F_REPLACE) | ack
	)
	ifname := func(s string) xtcpnl.LinkAttr { return xtcpnl.LinkAttrString(unix.IFLA_IFNAME, s) }
	u32 := xtcpnl.LinkAttrU32
	attrs := func(a ...xtcpnl.LinkAttr) []xtcpnl.LinkAttr { return a }
	add := func(a []xtcpnl.LinkAttr, kind string) xtcpnl.LinkWrite {
		return xtcpnl.LinkWrite{MsgType: newlink, Flags: addF, Attrs: a, Kind: kind}
	}
	set := func(index int32, a []xtcpnl.LinkAttr) xtcpnl.LinkWrite {
		return xtcpnl.LinkWrite{MsgType: newlink, Flags: ack, Index: index, Attrs: a}
	}
	withFlags := func(w xtcpnl.LinkWrite, flags, change uint32) xtcpnl.LinkWrite {
		w.IfFlags, w.IfChange = flags, change
		return w
	}
	withData := func(w xtcpnl.LinkWrite, kind string, data []byte) xtcpnl.LinkWrite {
		w.Kind, w.Data, w.HasData = kind, data, true
		return w
	}
	invalid := func(msg string, resolved ...string) linkWriteExpected {
		return linkWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	notImpl := func(msg string) linkWriteExpected {
		return linkWriteExpected{err: ErrNotImplemented, errMsg: msg}
	}
	peer := func(index int32, flags, change uint32, a ...[]byte) []byte {
		return nla(xtcpnl.VethInfoPeer, append([][]byte{ifinfomsg(index, flags, change)}, a...)...)
	}
	alias255 := strings.Repeat("a", ifAliasSizeCst-1)
	alias256 := strings.Repeat("a", ifAliasSizeCst)
	dev127 := strings.Repeat("x", altIfNameSizeCst-1)
	dev128 := strings.Repeat("x", altIfNameSizeCst)

	tests := []struct {
		description string
		verb        string
		family      uint8
		args        []string
		expected    linkWriteExpected
	}{
		// Addressing and the header.
		{description: "positive: add addresses by name, index 0", verb: "add",
			args:     []string{"d0", "type", "dummy"},
			expected: linkWriteExpected{write: add(attrs(ifname("d0")), "dummy")}},
		{description: "positive: set addresses by index and sends no name", verb: "set",
			args:     []string{"d0", "up"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_UP, unix.IFF_UP), resolved: []string{"d0"}}},
		{description: "positive: down clears the flag and still sets the change bit", verb: "set",
			args:     []string{"d0", "down"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), 0, unix.IFF_UP), resolved: []string{"d0"}}},
		{description: "corner: up then down leaves flags 0, change 1", verb: "set",
			args:     []string{"d0", "up", "down"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), 0, unix.IFF_UP), resolved: []string{"d0"}}},
		{description: "positive: change is set", verb: "change",
			args:     []string{"d0", "up"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_UP, unix.IFF_UP), resolved: []string{"d0"}}},
		{description: "positive: replace is CREATE|REPLACE and addresses by name", verb: "replace",
			args:     []string{"bond0", "type", "bond"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{MsgType: newlink, Flags: replF, Attrs: attrs(ifname("bond0")), Kind: "bond"}}},
		{description: "positive: delete is RTM_DELLINK by index", verb: "delete",
			args:     []string{"bond0"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{MsgType: dellink, Flags: ack, Index: 4}, resolved: []string{"bond0"}}},
		{description: "corner: delete with a type carries the kind and no data", verb: "delete",
			args:     []string{"bond0", "type", "bond"},
			expected: linkWriteExpected{write: xtcpnl.LinkWrite{MsgType: dellink, Flags: ack, Index: 4, Kind: "bond"}, resolved: []string{"bond0"}}},
		{description: "positive: -4 puts AF_INET in ifi_family", verb: "set", family: unix.AF_INET,
			args: []string{"d0", "up"},
			expected: linkWriteExpected{write: func() xtcpnl.LinkWrite {
				w := withFlags(set(2, nil), unix.IFF_UP, unix.IFF_UP)
				w.Family = unix.AF_INET
				return w
			}(), resolved: []string{"d0"}}},

		// Names, dev and their reconciliation.
		{description: "boundary: a 15-character name is the limit", verb: "add",
			args:     []string{"name", "abcdefghijklmno", "type", "dummy"},
			expected: linkWriteExpected{write: add(attrs(ifname("abcdefghijklmno")), "dummy")}},
		{description: "negative: a 16-character name is refused before the wire", verb: "add",
			args:     []string{"name", "abcdefghijklmnop", "type", "dummy"},
			expected: invalid(`"name" not a valid ifname`)},
		{description: "corner: a bare 16-character dev passes check_altifname and is sent; the kernel refuses it", verb: "add",
			args:     []string{"abcdefghijklmnop", "type", "dummy"},
			expected: linkWriteExpected{write: add(attrs(ifname("abcdefghijklmnop")), "dummy")}},
		{description: "boundary: a 127-byte dev is the altname limit", verb: "set",
			args:     []string{dev127, "up"},
			expected: linkWriteExpected{write: withFlags(set(9, nil), unix.IFF_UP, unix.IFF_UP), resolved: []string{dev127}}},
		{description: "negative: a 128-byte dev is not", verb: "set",
			args:     []string{dev128, "up"},
			expected: invalid(`"dev" not a valid ifname`)},
		{description: "negative: a name with a slash", verb: "set", args: []string{"d0", "name", "a/b"},
			expected: invalid(`"name" not a valid ifname`)},
		{description: "negative: a name with a space", verb: "set", args: []string{"d0", "name", "a b"},
			expected: invalid(`"name" not a valid ifname`)},
		{description: "negative: the name ..", verb: "set", args: []string{"d0", "name", ".."},
			expected: invalid(`"name" not a valid ifname`)},
		{description: "corner: rename sends IFLA_IFNAME on a set, after the other attributes", verb: "set",
			args: []string{"d0", "name", "d1", "mtu", "9000", "alias", "uplink"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_MTU, 9000),
				xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte("uplink")), ifname("d1"))), resolved: []string{"d0"}}},
		{description: "corner: renaming to the same name sends no IFLA_IFNAME", verb: "set",
			args:     []string{"d0", "name", "d0"},
			expected: linkWriteExpected{write: set(2, nil), resolved: []string{"d0"}}},
		{description: "corner: name before the device is still a rename of that device", verb: "set",
			args:     []string{"name", "d1", "d0"},
			expected: linkWriteExpected{write: set(2, attrs(ifname("d1"))), resolved: []string{"d0"}}},
		{description: "positive: dev keyword", verb: "set",
			args:     []string{"dev", "d0", "name", "d1"},
			expected: linkWriteExpected{write: set(2, attrs(ifname("d1"))), resolved: []string{"d0"}}},
		{description: "negative: two devices is duparg2", verb: "set", args: []string{"d0", "d1"},
			expected: invalid(`either "dev" is duplicate, or "d1" is garbage`)},
		{description: "negative: name twice is duparg", verb: "set", args: []string{"d0", "name", "d1", "name", "d2"},
			expected: invalid(`duplicate "name": "d2" is the second value`)},
		{description: "negative: name and a different dev on create", verb: "add",
			args:     []string{"name", "d0", "dev", "d1", "type", "dummy"},
			expected: invalid(`both "name" and "dev" cannot be used when creating devices`)},
		{description: "positive: add with name and no dev", verb: "add",
			args:     []string{"name", "d0", "type", "dummy"},
			expected: linkWriteExpected{write: add(attrs(ifname("d0")), "dummy")}},
		{description: "corner: add with no name at all sends no IFLA_IFNAME; the kernel picks one", verb: "add",
			args:     []string{"type", "dummy"},
			expected: linkWriteExpected{write: add(nil, "dummy")}},
		{description: "negative: add without type", verb: "add", args: []string{"d0"},
			expected: invalid(`Not enough information: "type" argument is required`)},
		{description: "negative: set without a device", verb: "set", args: []string{"up"},
			expected: invalid(`Not enough information: "dev" argument is required`)},
		{description: "negative: an unknown device is nodev, after its lookup", verb: "set",
			args: []string{"nosuch", "up"}, expected: invalid(`Cannot find device "nosuch"`, "nosuch")},
		{description: "negative: a resolver failure is returned as it is", verb: "set",
			args: []string{"boom", "up"}, expected: linkWriteExpected{err: errFakeResolve, errMsg: "fake", resolved: []string{"boom"}}},
		{description: "negative: dev with no value", verb: "set", args: []string{"dev"},
			expected: invalid(`Command line is not complete`)},
		{description: "negative: help is not implemented", verb: "set", args: []string{"d0", "help"},
			expected: notImpl("ip link help")},
		{description: "corner: h is help, because the help test comes before a bare device", verb: "set",
			args: []string{"h"}, expected: notImpl("ip link help")},

		// Generic attributes.
		{description: "boundary: IFLA_IFNAME follows the parse-order attributes on add", verb: "add",
			args:     []string{"bond0", "mtu", "9000", "type", "bond"},
			expected: linkWriteExpected{write: add(attrs(u32(unix.IFLA_MTU, 9000), ifname("bond0")), "bond")}},
		{description: "positive: master is the master's ifindex, resolved before the device", verb: "set",
			args:     []string{"la1", "master", "bond0"},
			expected: linkWriteExpected{write: set(3, attrs(u32(unix.IFLA_MASTER, 4))), resolved: []string{"bond0", "la1"}}},
		{description: "positive: nomaster is master 0 and resolves only the device", verb: "set",
			args:     []string{"la1", "nomaster"},
			expected: linkWriteExpected{write: set(3, attrs(u32(unix.IFLA_MASTER, 0))), resolved: []string{"la1"}}},
		{description: "negative: an unknown master", verb: "set", args: []string{"la1", "master", "nosuch"},
			expected: invalid(`Device does not exist`, "nosuch")},
		{description: "corner: mtu is base 0", verb: "set", args: []string{"d0", "mtu", "0x10"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_MTU, 16))), resolved: []string{"d0"}}},
		{description: "corner: mtu -1 is accepted and is not counted, so a second mtu is no duplicate", verb: "set",
			args: []string{"d0", "mtu", "-1", "mtu", "5"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_MTU, 0xffffffff), u32(unix.IFLA_MTU, 5))),
				resolved: []string{"d0"}}},
		{description: "negative: mtu twice", verb: "set", args: []string{"d0", "mtu", "1", "mtu", "2"},
			expected: invalid(`duplicate "mtu": "2" is the second value`)},
		{description: "negative: mtu garbage", verb: "set", args: []string{"d0", "mtu", "9k"},
			expected: invalid(`Invalid "mtu" value`)},
		{description: "boundary: mtu past INT_MAX", verb: "set", args: []string{"d0", "mtu", "2147483648"},
			expected: invalid(`Invalid "mtu" value`)},
		{description: "negative: mtu with no value", verb: "set", args: []string{"d0", "mtu"},
			expected: invalid(`Command line is not complete`)},
		{description: "corner: mtu is strcmp, so mt is a second device", verb: "set", args: []string{"d0", "mt", "1"},
			expected: invalid(`either "dev" is duplicate, or "mt" is garbage`)},
		{description: "corner: t is txqueuelen, tried before type", verb: "set", args: []string{"d0", "t", "100"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_TXQLEN, 100))), resolved: []string{"d0"}}},
		{description: "positive: qlen is exact", verb: "set", args: []string{"d0", "qlen", "100"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_TXQLEN, 100))), resolved: []string{"d0"}}},
		{description: "corner: txql is the txqlen spelling", verb: "set", args: []string{"d0", "txql", "7"},
			expected: linkWriteExpected{write: set(2, attrs(u32(unix.IFLA_TXQLEN, 7))), resolved: []string{"d0"}}},
		{description: "negative: qlen and txqueuelen are one keyword, so both is a duplicate", verb: "set",
			args: []string{"d0", "qlen", "1", "txqueuelen", "2"}, expected: invalid(`duplicate "txqueuelen"`)},
		{description: "corner: alias is the one string with no NUL", verb: "set", args: []string{"d0", "alias", "uplink"},
			expected: linkWriteExpected{write: set(2, attrs(xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte("uplink")))),
				resolved: []string{"d0"}}},
		{description: "boundary: a 255-byte alias", verb: "set", args: []string{"d0", "alias", alias255},
			expected: linkWriteExpected{write: set(2, attrs(xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte(alias255)))),
				resolved: []string{"d0"}}},
		{description: "boundary: a 256-byte alias is too long", verb: "set", args: []string{"d0", "alias", alias256},
			expected: invalid(`alias too long`)},
		{description: "corner: an empty alias is an empty attribute, which clears it", verb: "set", args: []string{"d0", "alias", ""},
			expected: linkWriteExpected{write: set(2, attrs(xtcpnl.LinkAttrBytes(unix.IFLA_IFALIAS, []byte{}))),
				resolved: []string{"d0"}}},
		{description: "positive: address on add, before IFLA_IFNAME", verb: "add",
			args: []string{"d0", "address", "02:00:00:00:00:01", "type", "dummy"},
			expected: linkWriteExpected{write: add(attrs(xtcpnl.LinkAttrBytes(unix.IFLA_ADDRESS, []byte{2, 0, 0, 0, 0, 1}),
				ifname("d0")), "dummy")}},
		{description: "negative: an unparseable address", verb: "add", args: []string{"d0", "address", "zz", "type", "dummy"},
			expected: invalid(`"zz" is invalid lladdr`)},
		{description: "negative: address on set needs a length lookup goip does not do", verb: "set",
			args: []string{"d0", "address", "02:00:00:00:00:01"}, expected: notImpl("address-length")},

		// Flag keywords.
		{description: "positive: promisc on", verb: "set", args: []string{"d0", "promisc", "on"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_PROMISC, unix.IFF_PROMISC), resolved: []string{"d0"}}},
		{description: "positive: multicast off clears it", verb: "set", args: []string{"d0", "multicast", "off"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), 0, unix.IFF_MULTICAST), resolved: []string{"d0"}}},
		{description: "corner: arp off sets IFF_NOARP", verb: "set", args: []string{"d0", "arp", "off"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_NOARP, unix.IFF_NOARP), resolved: []string{"d0"}}},
		{description: "corner: trailers off sets IFF_NOTRAILERS", verb: "set", args: []string{"d0", "trailers", "off"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_NOTRAILERS, unix.IFF_NOTRAILERS), resolved: []string{"d0"}}},
		{description: "corner: d is dynamic, a prefix keyword", verb: "set", args: []string{"d0", "d", "on"},
			expected: linkWriteExpected{write: withFlags(set(2, nil), unix.IFF_DYNAMIC, unix.IFF_DYNAMIC), resolved: []string{"d0"}}},
		{description: "negative: on/off is strcmp", verb: "set", args: []string{"d0", "promisc", "On"},
			expected: invalid(`argument of "promisc" must be "on" or "off", not "On"`)},

		// Keywords goip does not implement are refused, never read as a device.
		{description: "negative: l is link, not a device", verb: "set", args: []string{"d0", "l", "x"},
			expected: notImpl(`"link"`)},
		{description: "negative: index", verb: "add", args: []string{"d0", "index", "5", "type", "dummy"},
			expected: notImpl(`"index"`)},
		{description: "negative: group", verb: "set", args: []string{"d0", "group", "1"},
			expected: notImpl(`"group"`)},

		// Kinds.
		{description: "positive: a kind with no arguments has no data nest, implemented or not", verb: "add",
			args:     []string{"br0", "type", "bridge"},
			expected: linkWriteExpected{write: add(attrs(ifname("br0")), "bridge")}},
		{description: "corner: ty is type", verb: "add", args: []string{"d0", "ty", "dummy"},
			expected: linkWriteExpected{write: add(attrs(ifname("d0")), "dummy")}},
		{description: "negative: type with no kind", verb: "add", args: []string{"d0", "type"},
			expected: invalid(`Command line is not complete`)},
		{description: "negative: an empty kind is not expressible", verb: "add", args: []string{"d0", "type", ""},
			expected: notImpl(`type ""`)},
		{description: "negative: dummy has no options", verb: "set", args: []string{"d0", "type", "dummy", "foo"},
			expected: invalid(`Garbage instead of arguments "foo ...".`, "d0")},
		{description: "negative: options of a kind goip does not implement", verb: "add",
			args: []string{"br0", "type", "bridge", "stp_state", "1"}, expected: notImpl(`"bridge"`)},
		{description: "positive: bond options", verb: "add",
			args: []string{"bond0", "type", "bond", "mode", "802.3ad"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("bond0")), ""), "bond",
				nla(unix.IFLA_BOND_MODE, []byte{4}))}},
		{description: "positive: bond names resolve after the device", verb: "set",
			args: []string{"bond0", "type", "bond", "active_slave", "la1"},
			expected: linkWriteExpected{write: withData(set(4, nil), "bond", nla(unix.IFLA_BOND_ACTIVE_SLAVE, le32(3))),
				resolved: []string{"bond0", "la1"}}},
		{description: "positive: master, device, then bond_slave options", verb: "set",
			args: []string{"la1", "master", "bond0", "type", "bond_slave", "queue_id", "1"},
			expected: linkWriteExpected{write: withData(set(3, attrs(u32(unix.IFLA_MASTER, 4))), "bond_slave",
				nla(unix.IFLA_BOND_SLAVE_QUEUE_ID, le16(1))), resolved: []string{"bond0", "la1"}}},
		{description: "negative: lacp_strict is newer than the pinned iproute2", verb: "add",
			args: []string{"bond0", "type", "bond", "lacp_strict", "on"}, expected: invalid(`bond: unknown command "lacp_strict"`)},

		// veth.
		{description: "positive: veth peer carries its own ifinfomsg", verb: "add",
			args: []string{"v0", "type", "veth", "peer", "name", "v1"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("v0")), ""), "veth",
				peer(0, 0, 0, nla(unix.IFLA_IFNAME, strz("v1"))))}},
		{description: "corner: outer up stays outer; the peer header is zero", verb: "add",
			args: []string{"v0", "up", "type", "veth", "peer", "name", "v1"},
			expected: linkWriteExpected{write: withData(withFlags(add(attrs(ifname("v0")), ""), unix.IFF_UP, unix.IFF_UP), "veth",
				peer(0, 0, 0, nla(unix.IFLA_IFNAME, strz("v1"))))}},
		{description: "corner: peer up goes in the peer header only", verb: "add",
			args: []string{"v0", "type", "veth", "peer", "name", "v1", "up"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("v0")), ""), "veth",
				peer(0, unix.IFF_UP, unix.IFF_UP, nla(unix.IFLA_IFNAME, strz("v1"))))}},
		{description: "positive: a bare peer device is its name", verb: "add",
			args: []string{"v0", "type", "veth", "peer", "v1"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("v0")), ""), "veth",
				peer(0, 0, 0, nla(unix.IFLA_IFNAME, strz("v1"))))}},
		{description: "boundary: peer with nothing after it is a bare peer header", verb: "add",
			args:     []string{"v0", "type", "veth", "peer"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("v0")), ""), "veth", peer(0, 0, 0))}},
		{description: "positive: a peer master resolves inside the peer parse", verb: "add",
			args: []string{"v0", "type", "veth", "peer", "name", "v1", "master", "bond0"},
			expected: linkWriteExpected{write: withData(add(attrs(ifname("v0")), ""), "veth",
				peer(0, 0, 0, nla(unix.IFLA_MASTER, le32(4)), nla(unix.IFLA_IFNAME, strz("v1")))),
				resolved: []string{"bond0"}}},
		{description: "negative: veth options start with peer, by strcmp", verb: "add",
			args: []string{"v0", "type", "veth", "pe", "name", "v1"}, expected: invalid(`expected "peer"`)},
		{description: "negative: a peer type is a duplicate type, naming the peer's kind", verb: "add",
			args:     []string{"v0", "type", "veth", "peer", "name", "v1", "type", "dummy"},
			expected: invalid(`duplicate "type": "dummy" is the second value`)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			verb := lookupLinkWriteVerb(tc.verb)
			if verb == nil {
				t.Fatalf("no verb %q", tc.verb)
			}
			links := newFakeLinks()
			got, err := buildLinkWrite(verb, tc.family, tc.args, links.resolve)
			if !slices.Equal(links.calls, tc.expected.resolved) {
				t.Errorf("resolved %q, want %q", links.calls, tc.expected.resolved)
			}
			if tc.expected.err != nil {
				if !errors.Is(err, tc.expected.err) || !strings.Contains(fmt.Sprint(err), tc.expected.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, tc.expected.err, tc.expected.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected.write) {
				t.Fatalf("LinkWrite mismatch\n got  %+v\n want %+v", got, tc.expected.write)
			}
			if _, err := xtcpnl.BuildLinkWriteRequest(got, 1); err != nil {
				t.Fatalf("BuildLinkWriteRequest: %v", err)
			}
		})
	}
}

// TestLinkParseKeywordsAreUnique guards the chain's shape: a keyword listed
// twice would make its second row dead, and the linkArmFlag rows need a bit.
//
// go test ./internal/goip/ -run TestLinkParseKeywordsAreUnique
func TestLinkParseKeywordsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range linkParseKeywords {
		if seen[k.keyword] {
			t.Errorf("keyword %q listed twice", k.keyword)
		}
		seen[k.keyword] = true
		if (k.arm == linkArmFlag) != (k.flag != 0) {
			t.Errorf("keyword %q: arm %d with flag %#x", k.keyword, k.arm, k.flag)
		}
		if got := lookupLinkKeyword(k.keyword); got == nil || got.keyword != k.keyword {
			t.Errorf("keyword %q spelled in full finds %+v, an earlier prefix row", k.keyword, got)
		}
	}
}

// talkLinks is a Source whose single-gets are answered from a table, for
// the resolver: each name maps to an RTM_NEWLINK body or an error.
type talkLinks struct {
	replies map[string][]byte
	errs    map[string]error
	talks   []string
}

func (s *talkLinks) Dump([]byte, uint16) ([][]byte, error) {
	return nil, errors.New("talkLinks: no dumps")
}

func (s *talkLinks) Talk(request []byte, _ uint16) ([]byte, error) {
	name := selectorOf(request).name
	s.talks = append(s.talks, name)
	if err, ok := s.errs[name]; ok {
		return nil, err
	}
	if b, ok := s.replies[name]; ok {
		return b, nil
	}
	return nil, fmt.Errorf("xtcpnl: rtnetlink error: %w", syscall.ENODEV)
}

// newLinkBody is an RTM_NEWLINK body: the ifinfomsg, IFLA_IFNAME and the
// altnames in IFLA_PROP_LIST.
func newLinkBody(index int32, name string, alts ...string) []byte {
	b := catBytes(ifinfomsg(index, 0, 0), nla(unix.IFLA_IFNAME, strz(name)))
	if len(alts) > 0 {
		list := make([][]byte, 0, len(alts))
		for _, a := range alts {
			list = append(list, nla(unix.IFLA_ALT_IFNAME, strz(a)))
		}
		b = append(b, nla(unix.IFLA_PROP_LIST, list...)...)
	}
	return b
}

// TestNewLinkNameResolver is ll_name_to_index: the cache, ll_link_get, and
// the "ifN" fallback, with the gets it sends.
//
// go test ./internal/goip/ -run TestNewLinkNameResolver
func TestNewLinkNameResolver(t *testing.T) {
	type result struct {
		indexes []int32
		talks   []string
		err     error
	}
	tests := []struct {
		description string
		lookups     []string
		expected    result
	}{
		{description: "positive: one get per name", lookups: []string{"bond0"},
			expected: result{indexes: []int32{4}, talks: []string{"bond0"}}},
		{description: "positive: a second lookup is answered from the cache", lookups: []string{"bond0", "la1", "bond0"},
			expected: result{indexes: []int32{4, 3, 4}, talks: []string{"bond0", "la1"}}},
		{description: "corner: the reply's altnames are cached too", lookups: []string{"la1", "uplink1"},
			expected: result{indexes: []int32{3, 3}, talks: []string{"la1"}}},
		{description: "negative: ENODEV is no device", lookups: []string{"nosuch"},
			expected: result{indexes: []int32{0}, talks: []string{"nosuch"}}},
		{description: "corner: a refused get falls back to the ifN spelling", lookups: []string{"if7"},
			expected: result{indexes: []int32{7}, talks: []string{"if7"}}},
		{description: "corner: sscanf ignores what follows the digits", lookups: []string{"if7x"},
			expected: result{indexes: []int32{7}, talks: []string{"if7x"}}},
		{description: "corner: if with no digits is no device", lookups: []string{"ifx"},
			expected: result{indexes: []int32{0}, talks: []string{"ifx"}}},
		{description: "negative: a misses are not cached, so asking again asks the kernel again", lookups: []string{"nosuch", "nosuch"},
			expected: result{indexes: []int32{0, 0}, talks: []string{"nosuch", "nosuch"}}},
		{description: "negative: a failure that is not an errno is returned", lookups: []string{"broken"},
			expected: result{talks: []string{"broken"}, err: ErrNoReplay}},
		{description: "negative: a name that needs IFLA_ALT_IFNAME sends nothing", lookups: []string{strings.Repeat("y", 16)},
			expected: result{err: ErrNotImplemented}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &talkLinks{
				replies: map[string][]byte{
					"bond0": newLinkBody(4, "bond0"),
					"la1":   newLinkBody(3, "la1", "uplink1"),
				},
				errs: map[string]error{"broken": ErrNoReplay},
			}
			var seq uint32
			resolve := newLinkNameResolver(service.New(src, func() uint32 { seq++; return seq }))
			var got result
			for _, name := range tc.lookups {
				idx, err := resolve(name)
				if err != nil {
					got.err = err
					break
				}
				got.indexes = append(got.indexes, idx)
			}
			got.talks = src.talks
			if tc.expected.err != nil {
				if !errors.Is(got.err, tc.expected.err) {
					t.Fatalf("err = %v, want %v", got.err, tc.expected.err)
				}
			} else if got.err != nil {
				t.Fatalf("unexpected error: %v", got.err)
			}
			if !slices.Equal(got.indexes, tc.expected.indexes) || !slices.Equal(got.talks, tc.expected.talks) {
				t.Fatalf("got indexes %v talks %q, want %v %q", got.indexes, got.talks, tc.expected.indexes, tc.expected.talks)
			}
		})
	}
}

// TestRunLinkWrite is the CLI end to end: the GOIP_WRITE_OUT hook, the
// refusal without it, and the exit codes. `add` resolves no names, so the
// replay source is never asked for anything.
//
// go test ./internal/goip/ -run TestRunLinkWrite
func TestRunLinkWrite(t *testing.T) {
	wantAdd, err := xtcpnl.BuildLinkWriteRequest(xtcpnl.LinkWrite{
		MsgType: unix.RTM_NEWLINK,
		Flags:   unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK,
		Attrs:   []xtcpnl.LinkAttr{xtcpnl.LinkAttrString(unix.IFLA_IFNAME, "d0")},
		Kind:    "dummy",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		code      int
		stderr    string // a fragment; "" for empty
		writeFile []byte // nil when no file is expected
	}
	tests := []struct {
		description string
		args        []string
		writeOut    bool
		expected    result
	}{
		{description: "positive: the request goes to GOIP_WRITE_OUT and nothing is printed",
			args: []string{"link", "add", "d0", "type", "dummy"}, writeOut: true,
			expected: result{code: ExitOK, writeFile: wantAdd}},
		{description: "corner: l a is link add",
			args: []string{"l", "a", "d0", "type", "dummy"}, writeOut: true,
			expected: result{code: ExitOK, writeFile: wantAdd}},
		{description: "negative: without GOIP_WRITE_OUT the write is refused",
			args:     []string{"link", "add", "d0", "type", "dummy"},
			expected: result{code: ExitUsage, stderr: "never applies a write"}},
		{description: "negative: an argv error wins over the missing hook",
			args:     []string{"link", "add", "d0"},
			expected: result{code: ExitUsage, stderr: `"type" argument is required`}},
		{description: "negative: an argv error writes no file",
			args: []string{"link", "add", "d0"}, writeOut: true,
			expected: result{code: ExitUsage, stderr: `"type" argument is required`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", linkDumpPcap)
			out := filepath.Join(t.TempDir(), "write.bin")
			if tc.writeOut {
				t.Setenv(writeOutEnvCst, out)
			} else {
				t.Setenv(writeOutEnvCst, "")
			}
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.expected.code {
				t.Errorf("code = %d, want %d; stderr %q", code, tc.expected.code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.expected.stderr) || (tc.expected.stderr == "") != (stderr.Len() == 0) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.expected.stderr)
			}
			got, rerr := os.ReadFile(out)
			switch {
			case tc.expected.writeFile == nil && rerr == nil:
				t.Errorf("wrote %x, want no file", got)
			case tc.expected.writeFile != nil && !bytes.Equal(got, tc.expected.writeFile):
				t.Errorf("wrote %x (%v)\nwant  %x", got, rerr, tc.expected.writeFile)
			}
		})
	}
}
