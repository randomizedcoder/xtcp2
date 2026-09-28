package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"encoding/binary"
	"errors"
	"reflect"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- byte builders -----------------------------------------------------------

// ifinfomsgHdrChange is ifinfomsgHdr plus ifi_change, which the plain builder
// leaves zero. A dump reply really does carry change == 0; only a notification
// sets it, so the event tests need a builder that can.
func ifinfomsgHdrChange(family uint8, itype uint16, index int32, flags, change uint32) []byte {
	b := ifinfomsgHdr(family, itype, index, flags)
	binary.LittleEndian.PutUint32(b[12:16], change)
	return b
}

// ---- type predicate ----------------------------------------------------------

// TestIsRtnetlinkEventType checks the filter that says whether
// ParseRtnetlinkEvent can decode a given nlmsg_type.
//
// go test ./pkg/xtcpnl/ -run TestIsRtnetlinkEventType
func TestIsRtnetlinkEventType(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		want        bool
	}{
		{"positive: RTM_NEWLINK", uint16(unix.RTM_NEWLINK), true},
		{"positive: RTM_DELLINK", uint16(unix.RTM_DELLINK), true},
		{"positive: RTM_NEWADDR", uint16(unix.RTM_NEWADDR), true},
		{"positive: RTM_DELADDR", uint16(unix.RTM_DELADDR), true},
		{"positive: RTM_NEWROUTE", uint16(unix.RTM_NEWROUTE), true},
		{"positive: RTM_DELROUTE", uint16(unix.RTM_DELROUTE), true},
		{"positive: RTM_NEWNEIGH", uint16(unix.RTM_NEWNEIGH), true},
		{"positive: RTM_DELNEIGH", uint16(unix.RTM_DELNEIGH), true},

		{"negative: RTM_GETLINK is a request, not an event", uint16(unix.RTM_GETLINK), false},
		{"negative: RTM_GETROUTE is a request, not an event", uint16(unix.RTM_GETROUTE), false},
		{"negative: RTM_NEWRULE is out of scope", uint16(unix.RTM_NEWRULE), false},
		{"negative: RTM_NEWQDISC is out of scope", uint16(unix.RTM_NEWQDISC), false},
		{"negative: NLMSG_ERROR is a control message", uint16(unix.NLMSG_ERROR), false},
		{"negative: NLMSG_DONE is a control message", uint16(unix.NLMSG_DONE), false},
		{"negative: NLMSG_NOOP is a control message", uint16(unix.NLMSG_NOOP), false},

		{"boundary: type 0 (below NLMSG_MIN_TYPE)", 0, false},
		{"boundary: RTM_NEWLINK-1 is the reserved boundary below the RTM range", uint16(unix.RTM_NEWLINK) - 1, false},
		{"boundary: max uint16", 0xffff, false},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsRtnetlinkEventType(tc.msgType); got != tc.want {
				t.Errorf("IsRtnetlinkEventType(%d) = %v, want %v", tc.msgType, got, tc.want)
			}
		})
	}
}

// TestIsRtnetlinkNotification covers the request / dump-reply / notification
// discriminator, including the pid+seq cases that make the obvious
// implementation wrong. The header values in the "corner" rows are taken from
// the real 7_1_4 capture.
//
// go test ./pkg/xtcpnl/ -run TestIsRtnetlinkNotification
func TestIsRtnetlinkNotification(t *testing.T) {
	tests := []struct {
		description string
		hdr         NlMsgHdr
		want        bool
	}{
		{
			description: "positive: a kernel-internal notification — no flags, pid and seq zero",
			hdr:         NlMsgHdr{Type: uint16(unix.RTM_NEWLINK)},
			want:        true,
		},
		{
			// ip_monitor_all / the capture: `ip route add 198.51.100.0/24`
			// produced pid=725 seq=1790381641 flags=0x600 (CREATE|EXCL echoed,
			// REQUEST clear). A pid/seq filter drops this — it is the single
			// most important row in this table.
			description: "corner: a notification for a userspace-initiated change echoes the origin pid and seq",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_NEWROUTE),
				Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL,
				Seq:   1790381641,
				Pid:   725,
			},
			want: true,
		},
		{
			// The matching request from the same exchange: flags 0x605, and
			// note pid=0 because `ip` sends on an unbound socket. A pid filter
			// would ADMIT this.
			description: "negative: a request with pid 0 is still a request (NLM_F_REQUEST set)",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_NEWROUTE),
				Flags: unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL,
				Seq:   1790381641,
			},
			want: false,
		},
		{
			description: "negative: a bare NLM_F_REQUEST is a request",
			hdr:         NlMsgHdr{Type: uint16(unix.RTM_NEWLINK), Flags: unix.NLM_F_REQUEST},
			want:        false,
		},
		{
			description: "negative: a dump request (REQUEST|DUMP) is not a notification",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_NEWADDR),
				Flags: unix.NLM_F_REQUEST | unix.NLM_F_DUMP,
			},
			want: false,
		},
		{
			description: "negative: a multipart dump reply (NLM_F_MULTI) is not a notification",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_NEWLINK),
				Flags: unix.NLM_F_MULTI,
				Seq:   7,
				Pid:   1234,
			},
			want: false,
		},
		{
			description: "corner: MULTI and REQUEST together is still not a notification",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_NEWROUTE),
				Flags: unix.NLM_F_REQUEST | unix.NLM_F_MULTI,
			},
			want: false,
		},
		{
			description: "negative: a non-event nlmsg_type is rejected even with notification flags",
			hdr:         NlMsgHdr{Type: uint16(unix.RTM_GETLINK)},
			want:        false,
		},
		{
			description: "negative: NLMSG_ERROR is never a notification",
			hdr:         NlMsgHdr{Type: uint16(unix.NLMSG_ERROR)},
			want:        false,
		},
		{
			description: "negative: NLMSG_DONE is never a notification",
			hdr:         NlMsgHdr{Type: uint16(unix.NLMSG_DONE), Flags: unix.NLM_F_MULTI},
			want:        false,
		},
		{
			description: "boundary: the zero header is not a notification (type 0 is not an event)",
			hdr:         NlMsgHdr{},
			want:        false,
		},
		{
			description: "boundary: an event type with every unrelated flag bit set is still a notification",
			hdr: NlMsgHdr{
				Type:  uint16(unix.RTM_DELNEIGH),
				Flags: ^uint16(unix.NLM_F_REQUEST | unix.NLM_F_MULTI),
			},
			want: true,
		},
		{
			description: "corner: all flag bits set — REQUEST and MULTI are both among them",
			hdr:         NlMsgHdr{Type: uint16(unix.RTM_DELNEIGH), Flags: 0xffff},
			want:        false,
		},
		{
			description: "positive: RTM_DELLINK with no flags is a notification",
			hdr:         NlMsgHdr{Type: uint16(unix.RTM_DELLINK)},
			want:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsRtnetlinkNotification(tc.hdr); got != tc.want {
				t.Errorf("IsRtnetlinkNotification(type=%d flags=%#x pid=%d seq=%d) = %v, want %v",
					tc.hdr.Type, tc.hdr.Flags, tc.hdr.Pid, tc.hdr.Seq, got, tc.want)
			}
		})
	}
}

// ---- EventAction -------------------------------------------------------------

// TestEventActionString checks the add/del/unknown renderer, including a value
// outside the declared set.
//
// go test ./pkg/xtcpnl/ -run TestEventActionString
func TestEventActionString(t *testing.T) {
	tests := []struct {
		description string
		action      EventAction
		want        string
	}{
		{"positive: add", EventActionAdd, "add"},
		{"positive: del", EventActionDel, "del"},
		{"boundary: the zero value is unknown", EventActionUnknown, "unknown"},
		{"corner: an out-of-range value renders its number, not a wrong name", EventAction(9), "EventAction(9)"},
		{"corner: max uint8", EventAction(255), "EventAction(255)"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.action.String(); got != tc.want {
				t.Errorf("EventAction(%d).String() = %q, want %q", uint8(tc.action), got, tc.want)
			}
		})
	}
}

// ---- dispatch ----------------------------------------------------------------

// TestParseRtnetlinkEvent covers the nlmsg_type dispatch across all four
// families in both senses, the ErrNotAnEvent rejection, and the propagation of
// each family decoder's own errors.
//
// The bodies here are synthesized rather than captured so the boundary and
// corner rows can be expressed exactly; the real-capture counterpart is
// xtcpnl_rtnetlink_events_realfixtures_test.go.
//
// go test ./pkg/xtcpnl/ -run TestParseRtnetlinkEvent
func TestParseRtnetlinkEvent(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		body        []byte
		want        any
		wantErr     error
	}{
		// ---- link ------------------------------------------------------------
		{
			description: "positive: RTM_NEWLINK, nlcap0 comes up (ifi_change IFF_UP)",
			msgType:     uint16(unix.RTM_NEWLINK),
			body: concat(
				ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 10,
					unix.IFF_UP|unix.IFF_RUNNING|unix.IFF_BROADCAST, unix.IFF_UP),
				rtattr(unix.IFLA_IFNAME, append([]byte("nlcap0"), 0)),
				rtattr(unix.IFLA_MTU, le32(1500)),
				rtattr(unix.IFLA_OPERSTATE, []byte{IfOperUp}),
				rtattr(unix.IFLA_CARRIER, []byte{1}),
			),
			want: LinkEvent{
				Action: EventActionAdd,
				Link: LinkInfo{
					Index: 10, Flags: unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_BROADCAST,
					Name: "nlcap0", Change: unix.IFF_UP, Type: unix.ARPHRD_ETHER,
					OperState: IfOperUp, HasOperState: true,
					Carrier: 1, HasCarrier: true,
					MTU: 1500, HasMTU: true,
				},
			},
		},
		{
			description: "positive: RTM_NEWLINK carrier loss — IFF_UP still set, IFF_RUNNING cleared",
			msgType:     uint16(unix.RTM_NEWLINK),
			body: concat(
				ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 10, unix.IFF_UP|unix.IFF_BROADCAST, 0),
				rtattr(unix.IFLA_IFNAME, append([]byte("nlcap0"), 0)),
				rtattr(unix.IFLA_OPERSTATE, []byte{IfOperLowerLayerDown}),
				rtattr(unix.IFLA_CARRIER, []byte{0}),
			),
			want: LinkEvent{
				Action: EventActionAdd,
				Link: LinkInfo{
					Index: 10, Flags: unix.IFF_UP | unix.IFF_BROADCAST,
					Name: "nlcap0", Type: unix.ARPHRD_ETHER,
					OperState: IfOperLowerLayerDown, HasOperState: true,
					// The reason HasCarrier exists, in one row: this
					// notification carries IFLA_CARRIER = 0, and "carrier
					// went away" is a different event from "the kernel said
					// nothing about carrier". Without the flag the two decode
					// identically, and a listener cannot tell them apart.
					Carrier: 0, HasCarrier: true,
				},
			},
		},
		{
			description: "positive: RTM_DELLINK reuses the ifinfomsg layout, only the action differs",
			msgType:     uint16(unix.RTM_DELLINK),
			body: concat(
				ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 10, unix.IFF_BROADCAST, 0xFFFFFFFF),
				rtattr(unix.IFLA_IFNAME, append([]byte("nlcap0"), 0)),
			),
			want: LinkEvent{
				Action: EventActionDel,
				Link: LinkInfo{
					Index: 10, Flags: unix.IFF_BROADCAST, Name: "nlcap0",
					Change: 0xFFFFFFFF, Type: unix.ARPHRD_ETHER,
				},
			},
		},
		{
			description: "corner: ifi_change 0xFFFFFFFF (kernel's \"everything\" mask) is kept verbatim",
			msgType:     uint16(unix.RTM_NEWLINK),
			body:        ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_LOOPBACK, 1, unix.IFF_UP, 0xFFFFFFFF),
			want: LinkEvent{
				Action: EventActionAdd,
				Link:   LinkInfo{Index: 1, Flags: unix.IFF_UP, Change: 0xFFFFFFFF, Type: unix.ARPHRD_LOOPBACK},
			},
		},
		{
			description: "boundary: RTM_NEWLINK body exactly IfInfomsgSizeCst with no attributes",
			msgType:     uint16(unix.RTM_NEWLINK),
			body:        ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 2, unix.IFF_UP, 0),
			want: LinkEvent{
				Action: EventActionAdd,
				Link:   LinkInfo{Index: 2, Flags: unix.IFF_UP, Type: unix.ARPHRD_ETHER},
			},
		},
		{
			description: "corner: RTM_NEWLINK one byte short of ifinfomsg -> ErrIfInfomsgSmall",
			msgType:     uint16(unix.RTM_NEWLINK),
			body:        make([]byte, IfInfomsgSizeCst-1),
			wantErr:     ErrIfInfomsgSmall,
		},
		{
			description: "corner: RTM_DELLINK with an attribute running past the buffer -> ErrRTAttrSmall",
			msgType:     uint16(unix.RTM_DELLINK),
			body: concat(
				ifinfomsgHdrChange(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 2, 0, 0),
				[]byte{0xff, 0x00, byte(unix.IFLA_IFNAME), 0x00, 0x00, 0x00, 0x00, 0x00},
			),
			wantErr: ErrRTAttrSmall,
		},

		// ---- addr ------------------------------------------------------------
		{
			description: "positive: RTM_NEWADDR IPv4 /24 with IFA_ADDRESS, IFA_LOCAL and IFA_LABEL",
			msgType:     uint16(unix.RTM_NEWADDR),
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 10),
				rtattr(unix.IFA_ADDRESS, v4b(192, 0, 2, 1)),
				rtattr(unix.IFA_LOCAL, v4b(192, 0, 2, 1)),
				rtattr(unix.IFA_LABEL, append([]byte("nlcap0"), 0)),
			),
			want: AddrEvent{
				Action: EventActionAdd,
				Addr: AddrInfo{
					Family: unix.AF_INET, Prefixlen: 24, Scope: unix.RT_SCOPE_UNIVERSE, Index: 10,
					Address: v4b(192, 0, 2, 1), Local: v4b(192, 0, 2, 1), Label: "nlcap0",
				},
			},
		},
		{
			// The kernel sends no IFA_LOCAL for IPv6, and ParseNewAddr now
			// aliases IFA_ADDRESS into Local exactly as
			// ip/ipaddress.c:1531-1534 does — so Local is populated here even
			// though the wire carries one attribute.
			description: "positive: RTM_DELADDR IPv6 /64 (no IFA_LOCAL, as the kernel sends for v6)",
			msgType:     uint16(unix.RTM_DELADDR),
			body: concat(
				ifaddrmsgHdr(unix.AF_INET6, 64, 0, unix.RT_SCOPE_UNIVERSE, 10),
				rtattr(unix.IFA_ADDRESS, mustV6(t, "2001:db8::1")),
			),
			want: AddrEvent{
				Action: EventActionDel,
				Addr: AddrInfo{
					Family: unix.AF_INET6, Prefixlen: 64, Scope: unix.RT_SCOPE_UNIVERSE, Index: 10,
					Address: mustV6(t, "2001:db8::1"), Local: mustV6(t, "2001:db8::1"),
				},
			},
		},
		{
			description: "boundary: RTM_NEWADDR body exactly IfAddrmsgSizeCst with no attributes",
			msgType:     uint16(unix.RTM_NEWADDR),
			body:        ifaddrmsgHdr(unix.AF_INET, 32, 0, unix.RT_SCOPE_HOST, 1),
			want: AddrEvent{
				Action: EventActionAdd,
				Addr:   AddrInfo{Family: unix.AF_INET, Prefixlen: 32, Scope: unix.RT_SCOPE_HOST, Index: 1},
			},
		},
		{
			description: "corner: RTM_DELADDR one byte short of ifaddrmsg -> ErrIfAddrmsgSmall",
			msgType:     uint16(unix.RTM_DELADDR),
			body:        make([]byte, IfAddrmsgSizeCst-1),
			wantErr:     ErrIfAddrmsgSmall,
		},

		// ---- route -----------------------------------------------------------
		{
			description: "positive: RTM_NEWROUTE IPv4 default via a gateway",
			msgType:     uint16(unix.RTM_NEWROUTE),
			body: concat(
				rtmsgHdr(unix.AF_INET, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT,
					unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 254)),
				rtattr(unix.RTA_OIF, le32(10)),
			),
			want: RouteEvent{
				Action: EventActionAdd,
				Route: RouteInfo{
					Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN,
					Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
					Gateway: v4b(192, 0, 2, 254), Oif: 10,
				},
			},
		},
		{
			description: "positive: RTM_DELROUTE IPv6 /64 on-link",
			msgType:     uint16(unix.RTM_DELROUTE),
			body: concat(
				rtmsgHdr(unix.AF_INET6, 64, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL,
					unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, mustV6(t, "2001:db8::")),
				rtattr(unix.RTA_OIF, le32(10)),
			),
			want: RouteEvent{
				Action: EventActionDel,
				Route: RouteInfo{
					Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
					Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
					Dst: mustV6(t, "2001:db8::"), Oif: 10,
				},
			},
		},
		{
			description: "positive: RTM_NEWROUTE blackhole carries rtm_type RTN_BLACKHOLE and no oif",
			msgType:     uint16(unix.RTM_NEWROUTE),
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC,
					unix.RT_SCOPE_UNIVERSE, unix.RTN_BLACKHOLE, 0),
				rtattr(unix.RTA_DST, v4b(198, 51, 100, 0)),
			),
			want: RouteEvent{
				Action: EventActionAdd,
				Route: RouteInfo{
					Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
					Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_BLACKHOLE, Protocol: unix.RTPROT_STATIC,
					Dst: v4b(198, 51, 100, 0),
				},
			},
		},
		{
			description: "corner: RTA_TABLE upgrades the 8-bit rtm_table for a table id above 255",
			msgType:     uint16(unix.RTM_NEWROUTE),
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_UNSPEC, unix.RTPROT_STATIC,
					unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(unix.RTA_DST, v4b(203, 0, 113, 0)),
				rtattr(unix.RTA_TABLE, le32(1000)),
			),
			want: RouteEvent{
				Action: EventActionAdd,
				Route: RouteInfo{
					Family: unix.AF_INET, DstLen: 24, Table: 1000,
					Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_STATIC,
					Dst: v4b(203, 0, 113, 0),
				},
			},
		},
		{
			description: "boundary: RTM_NEWROUTE body exactly RtMsgSizeCst with no attributes",
			msgType:     uint16(unix.RTM_NEWROUTE),
			body: rtmsgHdr(unix.AF_INET, 0, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL,
				unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
			want: RouteEvent{
				Action: EventActionAdd,
				Route: RouteInfo{
					Family: unix.AF_INET, Table: unix.RT_TABLE_MAIN,
					Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				},
			},
		},
		{
			description: "corner: RTM_DELROUTE one byte short of rtmsg -> ErrRtMsgSmall",
			msgType:     uint16(unix.RTM_DELROUTE),
			body:        make([]byte, RtMsgSizeCst-1),
			wantErr:     ErrRtMsgSmall,
		},

		// ---- neigh -----------------------------------------------------------
		{
			description: "positive: RTM_NEWNEIGH permanent IPv4 ARP entry",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			body: concat(
				ndmsgHdr(unix.AF_INET, 10, unix.NUD_PERMANENT, 0, unix.RTN_UNICAST),
				rtattr(unix.NDA_DST, v4b(192, 0, 2, 50)),
				rtattr(unix.NDA_LLADDR, macb(0x02, 0x00, 0x00, 0x00, 0x00, 0x01)),
			),
			want: NeighEvent{
				Action: EventActionAdd,
				Neigh: NeighInfo{
					Family: unix.AF_INET, Ifindex: 10, State: unix.NUD_PERMANENT, Type: unix.RTN_UNICAST,
					Dst: v4b(192, 0, 2, 50), LLAddr: macb(0x02, 0x00, 0x00, 0x00, 0x00, 0x01),
				},
			},
		},
		{
			description: "positive: RTM_DELNEIGH arrives NUD_FAILED, so the delete says nothing about reachability",
			msgType:     uint16(unix.RTM_DELNEIGH),
			body: concat(
				ndmsgHdr(unix.AF_INET, 10, unix.NUD_FAILED, 0, unix.RTN_UNICAST),
				rtattr(unix.NDA_DST, v4b(192, 0, 2, 50)),
			),
			want: NeighEvent{
				Action: EventActionDel,
				Neigh: NeighInfo{
					Family: unix.AF_INET, Ifindex: 10, State: unix.NUD_FAILED, Type: unix.RTN_UNICAST,
					Dst: v4b(192, 0, 2, 50),
				},
			},
		},
		{
			description: "boundary: RTM_NEWNEIGH body exactly NdMsgSizeCst with no attributes",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			body:        ndmsgHdr(unix.AF_INET6, 10, unix.NUD_NOARP, 0, 0),
			want: NeighEvent{
				Action: EventActionAdd,
				Neigh:  NeighInfo{Family: unix.AF_INET6, Ifindex: 10, State: unix.NUD_NOARP},
			},
		},
		{
			description: "corner: RTM_NEWNEIGH one byte short of ndmsg -> ErrNdMsgSmall",
			msgType:     uint16(unix.RTM_NEWNEIGH),
			body:        make([]byte, NdMsgSizeCst-1),
			wantErr:     ErrNdMsgSmall,
		},

		// ---- not an event ----------------------------------------------------
		{
			description: "negative: RTM_GETLINK is a request type -> ErrNotAnEvent",
			msgType:     uint16(unix.RTM_GETLINK),
			body:        ifinfomsgHdrChange(unix.AF_UNSPEC, 0, 0, 0, 0),
			wantErr:     ErrNotAnEvent,
		},
		{
			description: "negative: NLMSG_ERROR body is not dispatched here -> ErrNotAnEvent",
			msgType:     uint16(unix.NLMSG_ERROR),
			body:        negErrno(syscall.ESRCH),
			wantErr:     ErrNotAnEvent,
		},
		{
			description: "negative: NLMSG_DONE -> ErrNotAnEvent",
			msgType:     uint16(unix.NLMSG_DONE),
			body:        nil,
			wantErr:     ErrNotAnEvent,
		},
		{
			description: "negative: RTM_NEWRULE is out of scope -> ErrNotAnEvent",
			msgType:     uint16(unix.RTM_NEWRULE),
			body:        make([]byte, RtMsgSizeCst),
			wantErr:     ErrNotAnEvent,
		},
		{
			description: "boundary: type 0 with an empty body -> ErrNotAnEvent, not a panic",
			msgType:     0,
			body:        nil,
			wantErr:     ErrNotAnEvent,
		},
		{
			description: "boundary: max uint16 type -> ErrNotAnEvent",
			msgType:     0xffff,
			body:        nil,
			wantErr:     ErrNotAnEvent,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRtnetlinkEvent(tc.msgType, tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != nil {
					t.Errorf("got = %+v on error, want nil", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseRtnetlinkEvent = %#v, want %#v", got, tc.want)
			}
			// IsRtnetlinkEventType must agree with the parser on every type it
			// accepts — a disagreement would make the pre-filter drop real events.
			if !IsRtnetlinkEventType(tc.msgType) {
				t.Errorf("IsRtnetlinkEventType(%d) = false but the parser accepted it", tc.msgType)
			}
		})
	}
}

// ---- link state helpers ------------------------------------------------------

// TestLinkInfoStateHelpers covers the three flag predicates that separate an
// administrative down from a carrier loss — the distinction link events exist
// for. The rows are exhaustive over the IFF_UP × IFF_RUNNING square plus the
// cases where other flags must not interfere.
//
// go test ./pkg/xtcpnl/ -run TestLinkInfoStateHelpers
func TestLinkInfoStateHelpers(t *testing.T) {
	tests := []struct {
		description     string
		flags           uint32
		wantUp          bool
		wantAdminDown   bool
		wantCarrierDown bool
	}{
		{
			description: "positive: IFF_UP|IFF_RUNNING is up, and neither kind of down",
			flags:       unix.IFF_UP | unix.IFF_RUNNING,
			wantUp:      true,
		},
		{
			description:     "positive: IFF_UP without IFF_RUNNING is a carrier loss, not an admin down",
			flags:           unix.IFF_UP,
			wantCarrierDown: true,
		},
		{
			description:   "positive: neither flag set is an admin down",
			flags:         0,
			wantAdminDown: true,
		},
		{
			description:   "corner: IFF_RUNNING without IFF_UP still counts as an admin down",
			flags:         unix.IFF_RUNNING,
			wantAdminDown: true,
		},
		{
			description: "corner: unrelated flags (BROADCAST|MULTICAST|LOWER_UP) do not change the verdict",
			flags: unix.IFF_UP | unix.IFF_RUNNING | unix.IFF_BROADCAST |
				unix.IFF_MULTICAST | unix.IFF_LOWER_UP,
			wantUp: true,
		},
		{
			description:   "corner: a loopback that is down is still an admin down",
			flags:         unix.IFF_LOOPBACK,
			wantAdminDown: true,
		},
		{
			description: "boundary: all flag bits set reads as up",
			flags:       0xFFFFFFFF,
			wantUp:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li := LinkInfo{Flags: tc.flags}
			if got := li.IsUp(); got != tc.wantUp {
				t.Errorf("IsUp(%#x) = %v, want %v", tc.flags, got, tc.wantUp)
			}
			if got := li.IsAdminDown(); got != tc.wantAdminDown {
				t.Errorf("IsAdminDown(%#x) = %v, want %v", tc.flags, got, tc.wantAdminDown)
			}
			if got := li.IsCarrierDown(); got != tc.wantCarrierDown {
				t.Errorf("IsCarrierDown(%#x) = %v, want %v", tc.flags, got, tc.wantCarrierDown)
			}
			// The three are mutually exclusive by construction; assert it so a
			// future edit to one predicate cannot silently overlap another.
			n := 0
			for _, b := range []bool{tc.wantUp, tc.wantAdminDown, tc.wantCarrierDown} {
				if b {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("test table row is self-inconsistent: %d predicates expected true, want exactly 1", n)
			}
		})
	}
}

// ---- attribute flag masking --------------------------------------------------

// TestWalkRTAttrsFlagMasking covers NlaTypeMaskCst: the kernel ORs NLA_F_NESTED
// and NLA_F_NET_BYTEORDER into nla_type, and an unmasked comparison against a
// bare IFLA_*/RTA_*/NDA_* constant silently misses those attributes.
//
// go test ./pkg/xtcpnl/ -run TestWalkRTAttrsFlagMasking
func TestWalkRTAttrsFlagMasking(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantTypes   []uint16
	}{
		{
			description: "positive: an unflagged type is unchanged",
			data:        rtattr(unix.IFLA_IFNAME, []byte("x\x00")),
			wantTypes:   []uint16{unix.IFLA_IFNAME},
		},
		{
			description: "positive: NLA_F_NESTED is masked off",
			data:        rtattr(unix.IFLA_LINKINFO|unix.NLA_F_NESTED, nil),
			wantTypes:   []uint16{unix.IFLA_LINKINFO},
		},
		{
			description: "positive: NLA_F_NET_BYTEORDER is masked off",
			data:        rtattr(unix.RTA_DST|unix.NLA_F_NET_BYTEORDER, v4b(10, 0, 0, 1)),
			wantTypes:   []uint16{unix.RTA_DST},
		},
		{
			description: "corner: both flags together are masked off",
			data:        rtattr(unix.NDA_DST|unix.NLA_F_NESTED|unix.NLA_F_NET_BYTEORDER, v4b(10, 0, 0, 2)),
			wantTypes:   []uint16{unix.NDA_DST},
		},
		{
			description: "boundary: type 0 with both flags set masks back to 0",
			data:        rtattr(unix.NLA_F_NESTED|unix.NLA_F_NET_BYTEORDER, nil),
			wantTypes:   []uint16{0},
		},
		{
			description: "boundary: type 0x3FFF, the largest value the mask leaves intact",
			data:        rtattr(0x3FFF, nil),
			wantTypes:   []uint16{0x3FFF},
		},
		{
			description: "corner: a flagged and an unflagged attribute in one stream",
			data: concat(
				rtattr(unix.IFLA_MTU, le32(1500)),
				rtattr(unix.IFLA_LINKINFO|unix.NLA_F_NESTED, nil),
			),
			wantTypes: []uint16{unix.IFLA_MTU, unix.IFLA_LINKINFO},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []uint16
			if err := WalkRTAttrs(tc.data, func(atype uint16, _ []byte) {
				got = append(got, atype)
			}); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantTypes) {
				t.Errorf("types = %v, want %v", got, tc.wantTypes)
			}
		})
	}
}

// TestWalkRTAttrsNested checks that the descent helper decodes a nested
// attribute's payload as an ordinary TLV stream.
//
// go test ./pkg/xtcpnl/ -run TestWalkRTAttrsNested
func TestWalkRTAttrsNested(t *testing.T) {
	tests := []struct {
		description string
		val         []byte
		wantTypes   []uint16
		wantErr     error
	}{
		{
			description: "positive: two inner attributes walked in order",
			val:         concat(rtattr(1, []byte("veth\x00")), rtattr(2, nil)),
			wantTypes:   []uint16{1, 2},
		},
		{
			description: "boundary: an empty nested payload yields no callbacks",
			val:         nil,
			wantTypes:   nil,
		},
		{
			description: "corner: an inner length beyond the payload -> ErrRTAttrSmall",
			val:         []byte{0xff, 0x00, 0x01, 0x00},
			wantErr:     ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []uint16
			err := WalkRTAttrsNested(tc.val, func(atype uint16, _ []byte) {
				got = append(got, atype)
			})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.wantTypes) {
				t.Errorf("types = %v, want %v", got, tc.wantTypes)
			}
		})
	}
}

// FuzzParseRtnetlinkEvent checks the dispatcher never panics on arbitrary
// type/body pairs.
//
// go test ./pkg/xtcpnl/ -run FuzzParseRtnetlinkEvent -fuzz FuzzParseRtnetlinkEvent
func FuzzParseRtnetlinkEvent(f *testing.F) {
	f.Add(uint16(unix.RTM_NEWLINK), ifinfomsgHdrChange(unix.AF_UNSPEC, 1, 2, unix.IFF_UP, unix.IFF_UP))
	f.Add(uint16(unix.RTM_DELROUTE), rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, 0, 0, unix.RTN_UNICAST, 0))
	f.Add(uint16(unix.RTM_NEWNEIGH), ndmsgHdr(unix.AF_INET, 2, unix.NUD_REACHABLE, 0, 0))
	f.Add(uint16(unix.NLMSG_ERROR), []byte(nil))
	f.Fuzz(func(_ *testing.T, msgType uint16, body []byte) {
		_, _ = ParseRtnetlinkEvent(msgType, body)
	})
}
