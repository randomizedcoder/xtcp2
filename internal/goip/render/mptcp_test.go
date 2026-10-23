package render

import (
	"net/netip"
	"strconv"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ptr is a pointer to v.
func ptr[T any](v T) *T { return &v }

// devNames names an index "dev" plus the index, the stand-in for
// ll_index_to_name.
func devNames(idx int32) string { return "dev" + strconv.FormatInt(int64(idx), 10) }

// TestMptcpAddr is print_mptcp_addrinfo's line and object.
//
// go test ./internal/goip/render/ -run TestMptcpAddr
func TestMptcpAddr(t *testing.T) {
	v4, v6 := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("2001:db8::1")
	tests := []struct {
		description string
		addr        xtcpnl.MPTCPAddr
		text        string
		obj         string
	}{
		{description: "positive: an IPv4 endpoint, its id and a flag",
			addr: xtcpnl.MPTCPAddr{Family: ptr[uint16](unix.AF_INET), Addr: v4, ID: ptr[uint8](1), Flags: ptr[uint32](2)},
			text: "192.0.2.1 id 1 subflow \n", obj: `{"address":"192.0.2.1","id":1,"subflow":true}`},
		{description: "positive: IPv6 with a port, every flag in table order and a device",
			addr: xtcpnl.MPTCPAddr{Family: ptr[uint16](unix.AF_INET6), Addr: v6, Port: ptr[uint16](10100), ID: ptr[uint8](255),
				Flags: ptr[uint32](0x3f), IfIndex: ptr[int32](3)},
			text: "2001:db8::1 port 10100 id 255 signal subflow backup fullmesh implicit laminar dev dev3 \n",
			obj: `{"address":"2001:db8::1","port":10100,"id":255,"signal":true,"subflow":true,"backup":true,` +
				`"fullmesh":true,"implicit":true,"laminar":true,"dev":"dev3"}`},
		{description: "corner: bits past laminar are rawflags, %02x",
			addr: xtcpnl.MPTCPAddr{Flags: ptr[uint32](0x1c1)}, text: "signal rawflags 1c0 \n", obj: `{"signal":true,"rawflags":"1c0"}`},
		{description: "boundary: port 0 and index 0 do not print",
			addr: xtcpnl.MPTCPAddr{Port: ptr[uint16](0), IfIndex: ptr[int32](0)}, text: "\n", obj: "{}"},
		{description: "boundary: nothing at all is a newline and an empty object", text: "\n", obj: "{}"},
		{description: "corner: no family reads as AF_UNSPEC, whose ADDR6 format_host writes as ???",
			addr: xtcpnl.MPTCPAddr{Addr: v6}, text: "??? \n", obj: `{"address":"???"}`},
		{description: "corner: AF_INET with only an IPv6 address prints none",
			addr: xtcpnl.MPTCPAddr{Family: ptr[uint16](unix.AF_INET), Addr: v6}, text: "\n", obj: "{}"},
		{description: "corner: an IPv4 address under AF_INET6 prints none",
			addr: xtcpnl.MPTCPAddr{Family: ptr[uint16](unix.AF_INET6), Addr: v4}, text: "\n", obj: "{}"},
		{description: "corner: the family is read as its low byte",
			addr: xtcpnl.MPTCPAddr{Family: ptr[uint16](0x100 | unix.AF_INET), Addr: v4}, text: "192.0.2.1 \n",
			obj: `{"address":"192.0.2.1"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := MptcpAddr(&tc.addr, devNames)
			if text != tc.text {
				t.Errorf("text %q, want %q", text, tc.text)
			}
			if string(obj) != tc.obj {
				t.Errorf("obj %s, want %s", obj, tc.obj)
			}
		})
	}
}

// TestMptcpLimitsText is print_mptcp_limit's line.
//
// go test ./internal/goip/render/ -run TestMptcpLimitsText
func TestMptcpLimitsText(t *testing.T) {
	tests := []struct {
		description string
		m           xtcpnl.MPTCPPM
		expected    string
	}{
		{description: "positive: both, add_addr_accepted first", m: xtcpnl.MPTCPPM{RcvAddAddrs: ptr[uint32](3), Subflows: ptr[uint32](4)},
			expected: "add_addr_accepted 3 subflows 4 \n"},
		{description: "positive: subflows alone", m: xtcpnl.MPTCPPM{Subflows: ptr[uint32](0)}, expected: "subflows 0 \n"},
		{description: "boundary: neither, a newline", expected: "\n"},
		{description: "corner: past INT_MAX, negative as %d prints it", m: xtcpnl.MPTCPPM{RcvAddAddrs: ptr[uint32](1 << 31)},
			expected: "add_addr_accepted -2147483648 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MptcpLimitsText(&tc.m); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestMptcpEvent is mptcp_monitor_msg's line and object.
//
// go test ./internal/goip/render/ -run TestMptcpEvent
func TestMptcpEvent(t *testing.T) {
	ev := func(cmd uint8) xtcpnl.MPTCPEvent {
		var e xtcpnl.MPTCPEvent
		e.Command = cmd
		return e
	}
	full := ev(xtcpnl.MPTCPEventSubClosed)
	full.Token, full.RemID, full.LocID = ptr[uint32](0x93945f91), ptr[uint8](0), ptr[uint8](1)
	full.Saddr, full.Daddr = netip.MustParseAddr("192.0.2.2"), netip.MustParseAddr("192.0.2.1")
	full.Sport, full.Dport, full.Backup, full.Error = ptr[uint16](57665), ptr[uint16](40528), ptr[uint8](0), ptr[uint8](111)
	full.Timeout, full.IfIndex = ptr[uint32](30), ptr[int32](3)
	full.ResetReason, full.ResetFlags = ptr[uint32](1), ptr[uint32](0x10)
	v6 := ev(xtcpnl.MPTCPEventAnnounced)
	v6.Saddr, v6.Daddr = netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("::1.2.3.4")
	zero := ev(xtcpnl.MPTCPEventCreated)
	zero.Token, zero.ResetFlags, zero.IfIndex = ptr[uint32](0), ptr[uint32](0), ptr[int32](0)
	server := ev(xtcpnl.MPTCPEventEstablished)
	server.ServerSide = ptr[uint8](1)
	flags := ev(xtcpnl.MPTCPEventEstablished)
	flags.Flags = ptr[uint16](0x7)
	serverOff := ev(xtcpnl.MPTCPEventEstablished)
	serverOff.ServerSide = ptr[uint8](0)
	tests := []struct {
		description string
		e           xtcpnl.MPTCPEvent
		stamp       MptcpStamp
		text        string
		obj         string
	}{
		{description: "positive: every scalar, in mptcp_monitor_msg's order", e: full,
			text: "[       SF_CLOSED] token=93945f91 remid=0 locid=1 saddr4=192.0.2.2 daddr4=192.0.2.1 sport=57665 dport=40528" +
				" backup=0 error=111 timeout=30 ifindex=3 dev=dev3 reset_reason=1 reset_flags=0x10\n",
			obj: `{"event":"SF_CLOSED","token":"0x93945f91","remid":0,"locid":1,"saddr4":"192.0.2.2","daddr4":"192.0.2.1",` +
				`"sport":57665,"dport":40528,"backup":0,"error":111,"timeout":30,"ifindex":3,"dev":"dev3","reset_reason":1,` +
				`"reset_flags":"0x10"}`},
		{description: "positive: IPv6 addresses as inet_ntop writes them",
			e: v6, text: "[       ANNOUNCED] saddr6=2001:db8::1 daddr6=::1.2.3.4\n",
			obj: `{"event":"ANNOUNCED","saddr6":"2001:db8::1","daddr6":"::1.2.3.4"}`},
		{description: "boundary: a zero token pads to eight, its JSON and reset_flags' are 0, index 0 has no name",
			e: zero, text: "[         CREATED] token=00000000 ifindex=0 reset_flags=0\n",
			obj: `{"event":"CREATED","token":"0","ifindex":0,"reset_flags":"0"}`},
		{description: "positive: SERVER_SIDE set is server_side", e: server,
			text: "[     ESTABLISHED] server_side\n", obj: `{"event":"ESTABLISHED","server_side":true}`},
		{description: "negative: SERVER_SIDE 0 is not", e: serverOff, text: "[     ESTABLISHED]\n", obj: `{"event":"ESTABLISHED"}`},
		{description: "corner: the flags' server_side and deny_join_id0, then the rest, %#x", e: flags,
			text: "[     ESTABLISHED] server_side deny_join_id0 flags=0x4\n",
			obj:  `{"event":"ESTABLISHED","server_side":true,"deny_join_id0":true,"flags":"0x4"}`},
		{description: "corner: an event with no name prints its number, no padding", e: ev(12),
			text: "[UNKNOWN 12]\n", obj: `{"event":"UNKNOWN 12"}`},
		{description: "boundary: the longest name fills the sixteen", e: ev(xtcpnl.MPTCPEventListenerCreated),
			text: "[LISTENER_CREATED]\n", obj: `{"event":"LISTENER_CREATED"}`},
		{description: "positive: a timestamp leads both", e: ev(xtcpnl.MPTCPEventClosed),
			stamp: MptcpStamp{Key: "timestamp_short", Text: "[T] ", Value: "T"},
			text:  "[T] [          CLOSED]\n", obj: `{"timestamp_short":"T","event":"CLOSED"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := MptcpEvent(tc.stamp, &tc.e, devNames)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}

// TestMptcpEventKnown is event_to_str's coverage: its ten names, and the
// gaps and the end of its table.
//
// go test ./internal/goip/render/ -run TestMptcpEventKnown
func TestMptcpEventKnown(t *testing.T) {
	tests := []struct {
		description string
		cmd         uint8
		expected    bool
	}{
		{description: "positive: CREATED, the first", cmd: xtcpnl.MPTCPEventCreated, expected: true},
		{description: "positive: LISTENER_CLOSED, the last", cmd: xtcpnl.MPTCPEventListenerClosed, expected: true},
		{description: "negative: 4, a gap", cmd: 4},
		{description: "negative: 12, the gap after SF_CLOSED", cmd: 12},
		{description: "boundary: 0, UNSPEC", cmd: 0},
		{description: "boundary: 17, past the table", cmd: 17},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MptcpEventKnown(tc.cmd); got != tc.expected {
				t.Errorf("MptcpEventKnown(%d) = %v, want %v", tc.cmd, got, tc.expected)
			}
		})
	}
}
