package xtcpnl

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// mptcpAddrRender shows a decoded endpoint as {field=value ...}.
func mptcpAddrRender(a *MPTCPAddr) string {
	var out []string
	if a.Family != nil {
		out = append(out, "family="+strconv.Itoa(int(*a.Family)))
	}
	if a.ID != nil {
		out = append(out, "id="+strconv.Itoa(int(*a.ID)))
	}
	if a.Addr.IsValid() {
		out = append(out, "addr="+a.Addr.String())
	}
	if a.Port != nil {
		out = append(out, "port="+strconv.Itoa(int(*a.Port)))
	}
	if a.Flags != nil {
		out = append(out, "flags=0x"+strconv.FormatUint(uint64(*a.Flags), 16))
	}
	if a.IfIndex != nil {
		out = append(out, "ifindex="+strconv.Itoa(int(*a.IfIndex)))
	}
	return "{" + strings.Join(out, " ") + "}"
}

// mptcpPMRender shows a decoded mptcp_pm body as its command, then each
// field present, or the error.
func mptcpPMRender(m MPTCPPM, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.Addr != nil {
		out = append(out, "addr"+mptcpAddrRender(m.Addr))
	}
	if m.RcvAddAddrs != nil {
		out = append(out, "rcv_add_addrs="+strconv.FormatUint(uint64(*m.RcvAddAddrs), 10))
	}
	if m.Subflows != nil {
		out = append(out, "subflows="+strconv.FormatUint(uint64(*m.Subflows), 10))
	}
	if m.Token != nil {
		out = append(out, "token="+strconv.FormatUint(uint64(*m.Token), 10))
	}
	if m.LocID != nil {
		out = append(out, "loc_id="+strconv.Itoa(int(*m.LocID)))
	}
	if m.AddrRemote != nil {
		out = append(out, "remote"+mptcpAddrRender(m.AddrRemote))
	}
	return strings.Join(out, " ")
}

// mptcpEventRender shows a decoded mptcp_pm event the same way.
func mptcpEventRender(m MPTCPEvent, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	u := func(name string, v *uint32) {
		if v != nil {
			out = append(out, name+"="+strconv.FormatUint(uint64(*v), 10))
		}
	}
	u16 := func(name string, v *uint16) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u8 := func(name string, v *uint8) {
		if v != nil {
			out = append(out, name+"="+strconv.Itoa(int(*v)))
		}
	}
	u("token", m.Token)
	u16("family", m.Family)
	u8("loc_id", m.LocID)
	u8("rem_id", m.RemID)
	if m.Saddr.IsValid() {
		out = append(out, "saddr="+m.Saddr.String())
	}
	if m.Daddr.IsValid() {
		out = append(out, "daddr="+m.Daddr.String())
	}
	u16("sport", m.Sport)
	u16("dport", m.Dport)
	u8("backup", m.Backup)
	u8("error", m.Error)
	u16("flags", m.Flags)
	u("timeout", m.Timeout)
	if m.IfIndex != nil {
		out = append(out, "ifindex="+strconv.Itoa(int(*m.IfIndex)))
	}
	u("reset_reason", m.ResetReason)
	u("reset_flags", m.ResetFlags)
	u8("server_side", m.ServerSide)
	return strings.Join(out, " ")
}

// TestParseMPTCPPM decodes mptcp_pm command and answer bodies. The
// positive rows are bodies transcribed from link-write/genl/mptcp_*; the
// rest are hand-built wire edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseMPTCPPM$'
func TestParseMPTCPPM(t *testing.T) {
	add := []byte{MPTCPPMCmdAddAddr, 1, 0, 0}
	// ep nests attrs under MPTCP_PM_ATTR_ADDR with NLA_F_NESTED, as ip does.
	ep := func(attrs ...[]byte) []byte { return rtattr(MPTCPPMAttrAddr|0x8000, cat(attrs...)) }
	id := rtattr(MPTCPAddrAttrID, []byte{1})
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's endpoint add, the id, flags, family and address nested (mptcp_endpoint_add)",
			body:        mustHex(t, "010100002400018005000200010000000800060002000000060001000200000008000300c0000201"),
			expected:    "cmd=1 ver=1 addr{family=2 id=1 addr=192.0.2.1 flags=0x2}",
		},
		{
			description: "positive: ip's endpoint add with a port, in host order (mptcp_endpoint_add_port)",
			body:        mustHex(t, "010100002400018008000600010000000600050074270000060001000200000008000300c0000202"),
			expected:    "cmd=1 ver=1 addr{family=2 addr=192.0.2.2 port=10100 flags=0x1}",
		},
		{
			description: "positive: ip's IPv6 endpoint add on a device (mptcp_endpoint_add_v6_dev)",
			body:        mustHex(t, "010100003000018008000600050000000800070003000000060001000a0000001400040020010db8000000000000000000000001"),
			expected:    "cmd=1 ver=1 addr{family=10 addr=2001:db8::1 flags=0x5 ifindex=3}",
		},
		{
			description: "positive: the kernel's GET_ADDR dump of that endpoint, with its id and a zero port (mptcp_endpoint_add_v6_dev)",
			body:        mustHex(t, "0301000040000180060001000a00000006000500000000000500020001000000080006000500000008000700030000001400040020010db8000000000000000000000001"),
			expected:    "cmd=3 ver=1 addr{family=10 id=1 addr=2001:db8::1 port=0 flags=0x5 ifindex=3}",
		},
		{
			description: "positive: ip's endpoint delete is the id alone (mptcp_endpoint_delete)",
			body:        mustHex(t, "020100000c0001800500020001000000"),
			expected:    "cmd=2 ver=1 addr{id=1}",
		},
		{
			description: "positive: ip's endpoint change sets the flags by id (mptcp_endpoint_change)",
			body:        mustHex(t, "070100001400018005000200010000000800060004000000"),
			expected:    "cmd=7 ver=1 addr{id=1 flags=0x4}",
		},
		{
			description: "positive: ip's limits set, both limits (mptcp_limits_set)",
			body:        mustHex(t, "0501000008000200030000000800030004000000"),
			expected:    "cmd=5 ver=1 rcv_add_addrs=3 subflows=4",
		},
		{
			description: "positive: the kernel's GET_LIMITS answer (mptcp_limits_set)",
			body:        mustHex(t, "0601000008000200030000000800030004000000"),
			expected:    "cmd=6 ver=1 rcv_add_addrs=3 subflows=4",
		},
		{
			description: "positive: ip's limits set of subflows alone, over the kernel's maximum (mptcp_limits_set_over)",
			body:        mustHex(t, "050100000800030009000000"),
			expected:    "cmd=5 ver=1 subflows=9",
		},
		{
			description: "corner: the kernel's CLOSED event, command 3, read as a GET_ADDR answer fails, its token no endpoint (mptcp_monitor_conn)",
			body:        mustHex(t, "0301000008000100915f9493"),
			expected:    genlErr + ": attribute length 24465",
		},
		{
			description: "boundary: ip's flush and limits set with nothing are the genl header alone (mptcp_endpoint_flush)",
			body:        mustHex(t, "04010000"),
			expected:    "cmd=4 ver=1",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        add[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "boundary: an empty endpoint nest is an endpoint with no fields",
			body:        cat(add, ep()),
			expected:    "cmd=1 ver=1 addr{}",
		},
		{
			description: "corner: the userspace path manager's token, local id and remote endpoint decode",
			body: cat([]byte{MPTCPPMCmdSubflowCreate, 1, 0, 0}, rtattr(MPTCPPMAttrToken, le32(7)), rtattr(MPTCPPMAttrLocID, []byte{2}),
				ep(id), rtattr(MPTCPPMAttrAddrRemote|0x8000, rtattr(MPTCPAddrAttrAddr4, net.ParseIP("192.0.2.9").To4()))),
			expected: "cmd=10 ver=1 addr{id=1} token=7 loc_id=2 remote{addr=192.0.2.9}",
		},
		{
			description: "corner: an endpoint nest without NLA_F_NESTED decodes the same",
			body:        cat(add, rtattr(MPTCPPMAttrAddr, id)),
			expected:    "cmd=1 ver=1 addr{id=1}",
		},
		{
			description: "corner: with both an IPv4 and an IPv6 address, the IPv4 one wins",
			body: cat(add, ep(rtattr(MPTCPAddrAttrAddr6, net.ParseIP("2001:db8::1")),
				rtattr(MPTCPAddrAttrAddr4, net.ParseIP("192.0.2.7").To4()))),
			expected: "cmd=1 ver=1 addr{addr=192.0.2.7}",
		},
		{
			description: "corner: a negative ifindex keeps its sign",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrIfIdx, le32(0xffffffff)))),
			expected:    "cmd=1 ver=1 addr{ifindex=-1}",
		},
		{
			description: "corner: an unknown attribute, inside the nest or out, and an unknown command are not errors",
			body:        cat([]byte{42, 1, 0, 0}, rtattr(42, le32(1)), ep(rtattr(42, le32(1)), id)),
			expected:    "cmd=42 ver=1 addr{id=1}",
		},
		{
			description: "negative: an address that is not 4 or 16 bytes is an error",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrAddr4, net.ParseIP("2001:db8::1")))),
			expected:    genlErr + ": attribute 3 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a family narrower than a u16 is an error, though ip reads it as a u8",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrFamily, []byte{2}))),
			expected:    genlErr + ": attribute 1 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: an endpoint id wider than a u8 is an error",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrID, le32(1)))),
			expected:    genlErr + ": attribute 2 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a port wider than a u16 is an error",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrPort, le32(10100)))),
			expected:    genlErr + ": attribute 5 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: endpoint flags narrower than a u32 is an error",
			body:        cat(add, ep(rtattr(MPTCPAddrAttrFlags, []byte{2, 0}))),
			expected:    genlErr + ": attribute 6 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a limit narrower than a u32 is an error",
			body:        cat(add, rtattr(MPTCPPMAttrRcvAddAddrs, []byte{3, 0})),
			expected:    genlErr + ": attribute 2 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a repeated endpoint is an error",
			body:        cat(add, ep(id), ep(id)),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: a repeated id inside the endpoint is an error",
			body:        cat(add, ep(id, id)),
			expected:    genlErr + ": duplicate attribute 2",
		},
		{
			description: "negative: a network-order endpoint nest is an error",
			body:        cat(add, rtattr(MPTCPPMAttrAddr|0x4000, id)),
			expected:    genlErr + ": network-order nested attribute",
		},
		{
			description: "negative: an endpoint nest whose attributes overrun it is an error",
			body:        cat(add, ep(rtattrOverrunning(MPTCPAddrAttrID, 40))),
			expected:    genlErr + ": attribute length 40",
		},
		{
			description: "negative: a remote endpoint's error is kept",
			body:        cat(add, rtattr(MPTCPPMAttrAddrRemote, rtattr(MPTCPAddrAttrID, le32(1)))),
			expected:    genlErr + ": attribute 2 needs an unflagged 1-byte scalar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := mptcpPMRender(ParseMPTCPPM(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseMPTCPEvent decodes mptcp_pm event bodies. The positive rows are
// the kernel's events from link-write/genl/mptcp_monitor_*; the rest are
// hand-built.
//
// go test ./pkg/xtcpnl/ -run 'TestParseMPTCPEvent$'
func TestParseMPTCPEvent(t *testing.T) {
	ev := []byte{MPTCPEventCreated, 1, 0, 0}
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: the kernel's LISTENER_CREATED for the endpoint's port, in network order (mptcp_monitor_listener)",
			body:        mustHex(t, "0f0100000600020002000000060009002774000008000500c0000201"),
			expected:    "cmd=15 ver=1 family=2 saddr=192.0.2.1 sport=10100",
		},
		{
			description: "positive: the kernel's CREATED, the token, both ends and ids (mptcp_monitor_conn)",
			body:        mustHex(t, "0101000008000100915f9493060002000200000008000500c000020108000700c0000201060009009e50000006000a00b09f000005000300000000000500040000000000"),
			expected:    "cmd=1 ver=1 token=2475974545 family=2 loc_id=0 rem_id=0 saddr=192.0.2.1 daddr=192.0.2.1 sport=40528 dport=45215",
		},
		{
			description: "positive: the server side's ESTABLISHED, with its flags and server_side (mptcp_monitor_conn)",
			body:        mustHex(t, "020100000800010056ad0a7a050012000100000006000d0002000000060002000200000008000500c000020108000700c000020106000900b09f000006000a009e50000005000300000000000500040000000000"),
			expected:    "cmd=2 ver=1 token=2047520086 family=2 loc_id=0 rem_id=0 saddr=192.0.2.1 daddr=192.0.2.1 sport=45215 dport=40528 flags=2 server_side=1",
		},
		{
			description: "positive: a SUB_CLOSED with its backup, interface and error, ECONNREFUSED (mptcp_monitor_conn)",
			body:        mustHex(t, "0b0100000800010056ad0a7a060002000200000008000500c000020208000700c000020106000900e141000006000a009e5000000500030001000000050004000000000005000b000000000008000f000300000005000c006f000000"),
			expected:    "cmd=11 ver=1 token=2047520086 family=2 loc_id=1 rem_id=0 saddr=192.0.2.2 daddr=192.0.2.1 sport=57665 dport=40528 backup=0 error=111 ifindex=3",
		},
		{
			description: "positive: a CLOSED is the token alone (mptcp_monitor_conn)",
			body:        mustHex(t, "0301000008000100915f9493"),
			expected:    "cmd=3 ver=1 token=2475974545",
		},
		{
			description: "boundary: a header-only event has no fields",
			body:        ev,
			expected:    "cmd=1 ver=1",
		},
		{
			description: "corner: IPv6 ends, a timeout, a negative ifindex and the reset reason and flags decode",
			body: cat(ev, rtattr(MPTCPAttrSaddr6, net.ParseIP("2001:db8::1")), rtattr(MPTCPAttrDaddr6, net.ParseIP("2001:db8::2")),
				rtattr(MPTCPAttrTimeout, le32(30)), rtattr(MPTCPAttrIfIdx, le32(0xffffffff)),
				rtattr(MPTCPAttrResetReason, le32(5)), rtattr(MPTCPAttrResetFlags, le32(1))),
			expected: "cmd=1 ver=1 saddr=2001:db8::1 daddr=2001:db8::2 timeout=30 ifindex=-1 reset_reason=5 reset_flags=1",
		},
		{
			description: "corner: a port's high byte comes first",
			body:        cat(ev, rtattr(MPTCPAttrDport, []byte{0x01, 0x02})),
			expected:    "cmd=1 ver=1 dport=258",
		},
		{
			description: "corner: an unknown attribute is not an error",
			body:        cat(ev, rtattr(42, le32(1)), rtattr(MPTCPAttrRemID, []byte{3})),
			expected:    "cmd=1 ver=1 rem_id=3",
		},
		{
			description: "negative: a port wider than a u16 is an error",
			body:        cat(ev, rtattr(MPTCPAttrSport, le32(1))),
			expected:    genlErr + ": attribute 9 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a source address that is not 4 or 16 bytes is an error",
			body:        cat(ev, rtattr(MPTCPAttrSaddr6, net.ParseIP("192.0.2.1").To4())),
			expected:    genlErr + ": attribute 6 needs an unflagged 16-byte scalar",
		},
		{
			description: "negative: event flags wider than a u16 is an error",
			body:        cat(ev, rtattr(MPTCPAttrFlags, le32(2))),
			expected:    genlErr + ": attribute 13 needs an unflagged 2-byte scalar",
		},
		{
			description: "negative: a server_side wider than a u8 is an error",
			body:        cat(ev, rtattr(MPTCPAttrServerSide, le32(1))),
			expected:    genlErr + ": attribute 18 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a repeated token is an error",
			body:        cat(ev, rtattr(MPTCPAttrToken, le32(1)), rtattr(MPTCPAttrToken, le32(2))),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: a body one byte short of the genl header is an error",
			body:        ev[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := mptcpEventRender(ParseMPTCPEvent(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseMPTCPPMCorpus decodes every mptcp_pm message in the committed
// corpus: ip's requests and the kernel's answers as commands, and a kernel
// message with nlmsg_pid 0 as an event.
//
// go test ./pkg/xtcpnl/ -run TestParseMPTCPPMCorpus
func TestParseMPTCPPMCorpus(t *testing.T) {
	const (
		request = iota
		answer
		event
	)
	// tally decodes every mptcp_pm message of command cmd and origin, and
	// counts the fields they carry: an endpoint with an address, a
	// subflows limit, a token and an event's source address.
	tally := func(cmd uint8, origin int) string {
		var n, errs, ep, subflows, token, saddr int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			o := answer
			if m.h.Flags&unix.NLM_F_REQUEST != 0 {
				o = request
			} else if m.h.Pid == 0 {
				o = event
			}
			if c.family != MPTCPPMGenlName || o != origin || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			if o == event {
				ev, err := ParseMPTCPEvent(m.body)
				if err != nil {
					errs++
					continue
				}
				count(ev.Token != nil, &token)
				count(ev.Saddr.IsValid(), &saddr)
				continue
			}
			pm, err := ParseMPTCPPM(m.body)
			if err != nil {
				errs++
				continue
			}
			count(pm.Addr != nil && pm.Addr.Addr.IsValid(), &ep)
			count(pm.Subflows != nil, &subflows)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(ep) + " endpoint, " +
			strconv.Itoa(subflows) + " subflows, " + strconv.Itoa(token) + " token, " + strconv.Itoa(saddr) + " saddr"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's ADD_ADDR requests decode, each with an address", got: tally(MPTCPPMCmdAddAddr, request), expected: "7 decoded, 0 errors, 7 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's DEL_ADDR requests decode, the id-0 delete with an address", got: tally(MPTCPPMCmdDelAddr, request), expected: "4 decoded, 0 errors, 1 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's GET_ADDR requests decode, its 12 dumps and 2 gets by id, none with an address", got: tally(MPTCPPMCmdGetAddr, request), expected: "14 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: every endpoint the kernel answered decodes with an address", got: tally(MPTCPPMCmdGetAddr, answer), expected: "12 decoded, 0 errors, 12 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's FLUSH_ADDRS request decodes", got: tally(MPTCPPMCmdFlushAddrs, request), expected: "1 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's SET_LIMITS requests decode, 3 with a subflows limit", got: tally(MPTCPPMCmdSetLimits, request), expected: "5 decoded, 0 errors, 0 endpoint, 3 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's GET_LIMITS requests decode", got: tally(MPTCPPMCmdGetLimits, request), expected: "5 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: every GET_LIMITS answer decodes with both limits", got: tally(MPTCPPMCmdGetLimits, answer), expected: "5 decoded, 0 errors, 0 endpoint, 5 subflows, 0 token, 0 saddr"},
		{description: "positive: ip's SET_FLAGS requests decode, one with an address", got: tally(MPTCPPMCmdSetFlags, request), expected: "4 decoded, 0 errors, 1 endpoint, 0 subflows, 0 token, 0 saddr"},
		{description: "positive: the CREATED events decode with a token and a source", got: tally(MPTCPEventCreated, event), expected: "2 decoded, 0 errors, 0 endpoint, 0 subflows, 2 token, 2 saddr"},
		{description: "positive: the ESTABLISHED events decode", got: tally(MPTCPEventEstablished, event), expected: "2 decoded, 0 errors, 0 endpoint, 0 subflows, 2 token, 2 saddr"},
		{description: "positive: the CLOSED events decode, a token alone", got: tally(MPTCPEventClosed, event), expected: "2 decoded, 0 errors, 0 endpoint, 0 subflows, 2 token, 0 saddr"},
		{description: "positive: the SUB_ESTABLISHED events decode", got: tally(MPTCPEventSubEstablished, event), expected: "2 decoded, 0 errors, 0 endpoint, 0 subflows, 2 token, 2 saddr"},
		{description: "positive: the SUB_CLOSED event decodes", got: tally(MPTCPEventSubClosed, event), expected: "1 decoded, 0 errors, 0 endpoint, 0 subflows, 1 token, 1 saddr"},
		{description: "positive: the LISTENER_CREATED events decode, with a source and no token", got: tally(MPTCPEventListenerCreated, event), expected: "4 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 4 saddr"},
		{description: "positive: the LISTENER_CLOSED events decode", got: tally(MPTCPEventListenerClosed, event), expected: "4 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 4 saddr"},
		{description: "negative: the kernel answers no ADD_ADDR, and sends no ANNOUNCED event", got: tally(MPTCPPMCmdAddAddr, answer) + "|" + tally(MPTCPEventAnnounced, event), expected: "0 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 0 saddr|0 decoded, 0 errors, 0 endpoint, 0 subflows, 0 token, 0 saddr"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
