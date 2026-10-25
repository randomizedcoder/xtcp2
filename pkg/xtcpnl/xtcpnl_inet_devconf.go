package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file is the codec for IPv4 devconf in a link message: IFLA_AF_SPEC ->
// AF_INET -> IFLA_INET_CONF, which iproute2 7.2.0 writes for
// `ip link set dev X inet <param> <value>…` (iplink_parse_inet,
// ip/iplink.c:572-860).
//
// The two directions do not share a shape:
//   - A request's IFLA_INET_CONF carries NLA_F_NESTED and holds u32
//     attributes typed by devconf id, in argument order, repeats kept. The
//     kernel parses them against inet_devconf_policy (net/ipv4/devinet.c).
//   - A dump's IFLA_INET_CONF (inet_fill_link_af) is a raw array of
//     IPV4_DEVCONF_MAX u32s, no flag, where entry i is devconf id i+1.
//
// IFLA_AF_SPEC and AF_INET are plain addattr_nests in the request, with no
// NLA_F_NESTED, and IFLA_AF_SPEC's entries are keyed by address family, not
// by an IFLA_* type (see setAddrGenMode).

// InetDevconfMax is IPV4_DEVCONF_MAX on the 7.3.0 kernel
// (include/uapi/linux/ip.h): the highest devconf id, arp_evict_nocarrier.
const InetDevconfMax uint16 = 33

// InetDevconfParam is one devconf param `ip link set … inet` names: its
// IPV4_DEVCONF_* id, ip's keyword, and whether ip reads its value with
// parse_on_off (true) or get_integer with base 0 (false).
type InetDevconfParam struct {
	ID    uint16
	Name  string
	OnOff bool
}

// InetDevconfParams are the 32 params iplink_parse_inet writes, in its order.
// mc_forwarding (2) has no keyword; inet_devconf_policy makes it NLA_REJECT.
var InetDevconfParams = []InetDevconfParam{
	{ID: 1, Name: "forwarding", OnOff: true},
	{ID: 3, Name: "proxy_arp", OnOff: true},
	{ID: 4, Name: "accept_redirects", OnOff: true},
	{ID: 5, Name: "secure_redirects", OnOff: true},
	{ID: 6, Name: "send_redirects", OnOff: true},
	{ID: 7, Name: "shared_media", OnOff: true},
	{ID: 8, Name: "rp_filter"},
	{ID: 9, Name: "accept_source_route", OnOff: true},
	{ID: 10, Name: "bootp_relay", OnOff: true},
	{ID: 11, Name: "log_martians", OnOff: true},
	{ID: 12, Name: "tag"},
	{ID: 13, Name: "arp_filter", OnOff: true},
	{ID: 14, Name: "medium_id"},
	{ID: 15, Name: "disable_xfrm", OnOff: true},
	{ID: 16, Name: "disable_policy", OnOff: true},
	{ID: 17, Name: "force_igmp_version"},
	{ID: 18, Name: "arp_announce"},
	{ID: 19, Name: "arp_ignore"},
	{ID: 20, Name: "promote_secondaries", OnOff: true},
	{ID: 21, Name: "arp_accept"},
	{ID: 22, Name: "arp_notify", OnOff: true},
	{ID: 23, Name: "accept_local", OnOff: true},
	{ID: 24, Name: "src_vmark", OnOff: true},
	{ID: 25, Name: "proxy_arp_pvlan", OnOff: true},
	{ID: 26, Name: "route_localnet", OnOff: true},
	{ID: 32, Name: "bc_forwarding", OnOff: true},
	{ID: 27, Name: "igmpv2_unsolicited_report_interval"},
	{ID: 28, Name: "igmpv3_unsolicited_report_interval"},
	{ID: 29, Name: "ignore_routes_with_linkdown", OnOff: true},
	{ID: 30, Name: "drop_unicast_in_l2_multicast", OnOff: true},
	{ID: 31, Name: "drop_gratuitous_arp", OnOff: true},
	{ID: 33, Name: "arp_evict_nocarrier", OnOff: true},
}

// InetDevconfSetting is one devconf attribute of a request. A negative
// medium_id is its two's complement, as addattr32 sends it.
type InetDevconfSetting struct {
	ID    uint16
	Value uint32
}

var (
	// ErrInetDevconfEmpty is an inet nest with no setting. ip never sends
	// one: a bare `inet` is incomplete_command.
	ErrInetDevconfEmpty = errors.New("xtcpnl: inet devconf with no setting")
	// ErrInetDevconfID is a devconf id of 0 or above InetDevconfMax, which
	// inet_validate_link_af's nla_parse would not index.
	ErrInetDevconfID = errors.New("xtcpnl: inet devconf id out of range")
	// ErrInetDevconfShort is a devconf attribute or array whose length is
	// not whole u32s.
	ErrInetDevconfShort = errors.New("xtcpnl: inet devconf value is not a u32")
)

// inetDevconfNestsCst is the two nest headers inside IFLA_AF_SPEC's payload:
// AF_INET and IFLA_INET_CONF.
const inetDevconfNestsCst = 2 * RTAttrSizeCst

// inetDevconfAttrCst is one u32 setting with its header.
const inetDevconfAttrCst = RTAttrSizeCst + 4

// LinkAttrInetDevconf encodes the whole IFLA_AF_SPEC attribute of an `inet`
// write, so the caller places it among a LinkWrite's Attrs in argument order:
// `inet forwarding on mtu 1400` sends it before IFLA_MTU.
func LinkAttrInetDevconf(settings []InetDevconfSetting) (LinkAttr, error) {
	if len(settings) == 0 {
		return LinkAttr{}, ErrInetDevconfEmpty
	}
	ab := NewAttrBuilder(make([]byte, inetDevconfNestsCst+inetDevconfAttrCst*len(settings)))
	af, err := ab.BeginNest(unix.AF_INET)
	if err != nil {
		return LinkAttr{}, err
	}
	conf, err := ab.BeginNest(unix.IFLA_INET_CONF | unix.NLA_F_NESTED)
	if err != nil {
		return LinkAttr{}, err
	}
	for _, s := range settings {
		if s.ID == 0 || s.ID > InetDevconfMax {
			return LinkAttr{}, ErrInetDevconfID
		}
		if err := ab.PutU32(s.ID, s.Value); err != nil {
			return LinkAttr{}, err
		}
	}
	if err := ab.EndNest(conf); err != nil {
		return LinkAttr{}, err
	}
	if err := ab.EndNest(af); err != nil {
		return LinkAttr{}, err
	}
	return LinkAttr{Type: unix.IFLA_AF_SPEC, Payload: ab.Bytes()}, nil
}

// inetConfs calls fn with the payload of every IFLA_INET_CONF under an
// AF_INET entry of afSpec, IFLA_AF_SPEC's payload. Other families are
// skipped.
func inetConfs(afSpec []byte, fn func(conf []byte) error) error {
	var ferr error
	err := WalkRTAttrsNested(afSpec, func(family uint16, afVal []byte) {
		if ferr != nil || family != unix.AF_INET {
			return
		}
		if err := WalkRTAttrsNested(afVal, func(atype uint16, conf []byte) {
			if ferr == nil && atype == unix.IFLA_INET_CONF {
				ferr = fn(conf)
			}
		}); err != nil && ferr == nil {
			ferr = err
		}
	})
	if err != nil {
		return err
	}
	return ferr
}

// ParseInetDevconfRequest decodes a request's IFLA_AF_SPEC payload into its
// devconf settings, in wire order, repeats kept. A setting shorter than a
// u32 is ErrInetDevconfShort, as the kernel's NLA_U32 check refuses it.
func ParseInetDevconfRequest(afSpec []byte) ([]InetDevconfSetting, error) {
	var out []InetDevconfSetting
	err := inetConfs(afSpec, func(conf []byte) error {
		var serr error
		if err := WalkRTAttrsNested(conf, func(id uint16, v []byte) {
			if serr != nil {
				return
			}
			if len(v) < 4 {
				serr = ErrInetDevconfShort
				return
			}
			out = append(out, InetDevconfSetting{ID: id, Value: binary.LittleEndian.Uint32(v)})
		}); err != nil {
			return err
		}
		return serr
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ParseInetDevconfValues decodes a dump's IFLA_AF_SPEC payload into the
// devconf array: values[i] is devconf id i+1, as many as the kernel sent, so
// a kernel with more ids than InetDevconfMax keeps them. ok is false when
// there is no AF_INET entry with an IFLA_INET_CONF; the first one is read.
func ParseInetDevconfValues(afSpec []byte) (values []uint32, ok bool, err error) {
	err = inetConfs(afSpec, func(conf []byte) error {
		if ok {
			return nil
		}
		if len(conf)%4 != 0 {
			return ErrInetDevconfShort
		}
		values = make([]uint32, len(conf)/4)
		for i := range values {
			values[i] = binary.LittleEndian.Uint32(conf[4*i:])
		}
		ok = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return values, ok, nil
}
