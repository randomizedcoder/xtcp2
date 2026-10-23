package render

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip mptcp`'s printers (ip/ipmptcp.c): print_mptcp_addrinfo for an
// endpoint, print_mptcp_limit for the limits and mptcp_monitor_msg for an
// event. Each print_* call is PRINT_ANY, PRINT_FP or PRINT_JSON, so one
// walk writes the line and the object; which one `ip` shows is the
// caller's -j. The limits are printed outside new_json_obj, so they are
// text even under -j, and have no object here.

// mptcpOut is one walk's two renders: the text line and the JSON object.
type mptcpOut struct {
	text strings.Builder
	obj  []byte
}

// key opens a JSON member.
func (o *mptcpOut) key(k string) {
	if len(o.obj) == 0 {
		o.obj = append(o.obj, '{')
	} else {
		o.obj = append(o.obj, ',')
	}
	o.obj = AppendJSONWString(o.obj, []byte(k))
	o.obj = append(o.obj, ':')
}

// str is print_string(PRINT_ANY, k, ...): the text as given, the value as
// a JSON string.
func (o *mptcpOut) str(k, text, v string) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = AppendJSONWString(o.obj, []byte(v))
}

// uint is print_uint(PRINT_ANY, k, ...).
func (o *mptcpOut) uint(k, text string, v uint64) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = strconv.AppendUint(o.obj, v, 10)
}

// boolean is a PRINT_FP text with print_bool(PRINT_JSON, k, NULL, true).
func (o *mptcpOut) boolean(k, text string) {
	o.text.WriteString(text)
	o.key(k)
	o.obj = append(o.obj, "true"...)
}

// done is print_nl and close_json_object: the line's newline, and the
// object's brace, "{}" for an object with no member.
func (o *mptcpOut) done() (string, []byte) {
	o.text.WriteString("\n")
	if len(o.obj) == 0 {
		return o.text.String(), []byte("{}")
	}
	return o.text.String(), append(o.obj, '}')
}

// mptcpAddrFlagNames is mptcp_addr_flag_names in its order; nobackup and
// nofullmesh are 0 there, so they never print.
var mptcpAddrFlagNames = []struct {
	name string
	mask uint32
}{
	{"signal", xtcpnl.MPTCPAddrFlagSignal},
	{"subflow", xtcpnl.MPTCPAddrFlagSubflow},
	{"backup", xtcpnl.MPTCPAddrFlagBackup},
	{"fullmesh", xtcpnl.MPTCPAddrFlagFullmesh},
	{"implicit", xtcpnl.MPTCPAddrFlagImplicit},
	{"laminar", xtcpnl.MPTCPAddrFlagLaminar},
}

// MptcpAddr is print_mptcp_addrinfo's line and object. The address prints
// from the attribute the family names, ADDR4 for AF_INET and ADDR6 for any
// other, as format_host_rta writes it, "???" outside inet and inet6; an
// absent family reads as AF_UNSPEC. A port prints when it is not 0, and
// the device, through dev, when its index is not 0.
func MptcpAddr(a *xtcpnl.MPTCPAddr, dev func(int32) string) (string, []byte) {
	var o mptcpOut
	var family uint8
	if a.Family != nil {
		// rta_getattr_u8 reads the u16's first byte, its low one.
		family = uint8(*a.Family)
	}
	switch {
	case family == unix.AF_INET && a.Addr.Is4():
		o.str("address", a.Addr.String()+" ", a.Addr.String())
	case family != unix.AF_INET && a.Addr.Is6():
		s := "???"
		if family == unix.AF_INET6 {
			s = inet6Ntop(a.Addr.As16())
		}
		o.str("address", s+" ", s)
	}
	if a.Port != nil && *a.Port != 0 {
		o.uint("port", "port "+strconv.FormatUint(uint64(*a.Port), 10)+" ", uint64(*a.Port))
	}
	if a.ID != nil {
		o.uint("id", "id "+strconv.FormatUint(uint64(*a.ID), 10)+" ", uint64(*a.ID))
	}
	if a.Flags != nil {
		mptcpAddrFlags(&o, *a.Flags)
	}
	if a.IfIndex != nil && *a.IfIndex != 0 {
		name := dev(*a.IfIndex)
		o.str("dev", "dev "+name+" ", name)
	}
	return o.done()
}

// mptcpAddrFlags is print_mptcp_addr_flags: each known flag's name, then
// what is left as rawflags, %02x.
func mptcpAddrFlags(o *mptcpOut, flags uint32) {
	for _, f := range mptcpAddrFlagNames {
		if flags&f.mask != 0 {
			o.boolean(f.name, f.name+" ")
		}
		flags &^= f.mask
	}
	if flags != 0 {
		raw := fmt.Sprintf("%02x", flags)
		o.str("rawflags", "rawflags "+raw+" ", raw)
	}
}

// MptcpLimitsText is print_mptcp_limit's line: each limit present, printed
// with %d as the C int it is passed as.
func MptcpLimitsText(m *xtcpnl.MPTCPPM) string {
	var b strings.Builder
	if m.RcvAddAddrs != nil {
		b.WriteString("add_addr_accepted " + strconv.FormatInt(int64(int32(*m.RcvAddAddrs)), 10) + " ")
	}
	if m.Subflows != nil {
		b.WriteString("subflows " + strconv.FormatInt(int64(int32(*m.Subflows)), 10) + " ")
	}
	b.WriteString("\n")
	return b.String()
}

// mptcpEventNames is event_to_str, by MPTCP_EVENT_* number.
var mptcpEventNames = map[uint8]string{
	xtcpnl.MPTCPEventCreated:         "CREATED",
	xtcpnl.MPTCPEventEstablished:     "ESTABLISHED",
	xtcpnl.MPTCPEventClosed:          "CLOSED",
	xtcpnl.MPTCPEventAnnounced:       "ANNOUNCED",
	xtcpnl.MPTCPEventRemoved:         "REMOVED",
	xtcpnl.MPTCPEventSubEstablished:  "SF_ESTABLISHED",
	xtcpnl.MPTCPEventSubClosed:       "SF_CLOSED",
	xtcpnl.MPTCPEventSubPriority:     "SF_PRIO",
	xtcpnl.MPTCPEventListenerCreated: "LISTENER_CREATED",
	xtcpnl.MPTCPEventListenerClosed:  "LISTENER_CLOSED",
}

// MptcpEventKnown reports whether event_to_str names an event.
func MptcpEventKnown(cmd uint8) bool {
	_, ok := mptcpEventNames[cmd]
	return ok
}

// MptcpStamp is print_timestamp's member: Key "timestamp" or
// "timestamp_short", Text the line's form and Value the object's. A zero
// MptcpStamp is no -t.
type MptcpStamp struct {
	Key, Text, Value string
}

// MptcpEvent is mptcp_monitor_msg's line and object: the timestamp under
// -t, then the event, "[%16s]", or "[UNKNOWN n]" alone for one event_to_str does
// not name, then each attribute present in its order. An address prints
// from SADDR4, DADDR4, SADDR6 then DADDR6; the decoder keeps one address
// a side, so each prints under the name its width gives it. dev names the
// interface of a nonzero index.
func MptcpEvent(ts MptcpStamp, e *xtcpnl.MPTCPEvent, dev func(int32) string) (string, []byte) {
	var o mptcpOut
	if ts.Key != "" {
		o.str(ts.Key, ts.Text, ts.Value)
	}
	name, ok := mptcpEventNames[e.Command]
	if !ok {
		s := "UNKNOWN " + strconv.FormatUint(uint64(e.Command), 10)
		o.str("event", "["+s+"]", s)
		return o.done()
	}
	o.str("event", fmt.Sprintf("[%16s]", name), name)
	if e.Token != nil {
		o.str("token", fmt.Sprintf(" token=%08x", *e.Token), hex0x(uint64(*e.Token)))
	}
	mptcpEventU8(&o, "remid", e.RemID)
	mptcpEventU8(&o, "locid", e.LocID)
	mptcpEventAddrs(&o, e.Saddr, e.Daddr)
	mptcpEventU16(&o, "sport", e.Sport)
	mptcpEventU16(&o, "dport", e.Dport)
	mptcpEventU8(&o, "backup", e.Backup)
	mptcpEventU8(&o, "error", e.Error)
	if e.Timeout != nil {
		o.uint("timeout", " timeout="+strconv.FormatUint(uint64(*e.Timeout), 10), uint64(*e.Timeout))
	}
	if e.IfIndex != nil {
		mptcpEventIface(&o, *e.IfIndex, dev)
	}
	if e.ResetReason != nil {
		o.uint("reset_reason", " reset_reason="+strconv.FormatUint(uint64(*e.ResetReason), 10), uint64(*e.ResetReason))
	}
	if e.ResetFlags != nil {
		v := hex0x(uint64(*e.ResetFlags))
		o.str("reset_flags", " reset_flags="+v, v)
	}
	mptcpEventFlags(&o, e)
	return o.done()
}

// mptcpEventU8 is print_uint of a u8 attribute, " k=%u".
func mptcpEventU8(o *mptcpOut, k string, v *uint8) {
	if v != nil {
		o.uint(k, " "+k+"="+strconv.FormatUint(uint64(*v), 10), uint64(*v))
	}
}

// mptcpEventU16 is print_uint of a port, already in host order.
func mptcpEventU16(o *mptcpOut, k string, v *uint16) {
	if v != nil {
		o.uint(k, " "+k+"="+strconv.FormatUint(uint64(*v), 10), uint64(*v))
	}
}

// mptcpEventAddrs is print_addr for the four address attributes, in their
// order.
func mptcpEventAddrs(o *mptcpOut, saddr, daddr netip.Addr) {
	for _, a := range []struct {
		k    string
		addr netip.Addr
		v4   bool
	}{{"saddr4", saddr, true}, {"daddr4", daddr, true}, {"saddr6", saddr, false}, {"daddr6", daddr, false}} {
		switch {
		case a.v4 && a.addr.Is4():
			o.str(a.k, " "+a.k+"="+a.addr.String(), a.addr.String())
		case !a.v4 && a.addr.Is6():
			s := inet6Ntop(a.addr.As16())
			o.str(a.k, " "+a.k+"="+s, s)
		}
	}
}

// mptcpEventIface is print_iface: the index, then the name of a nonzero
// one.
func mptcpEventIface(o *mptcpOut, idx int32, dev func(int32) string) {
	o.text.WriteString(" ifindex=" + strconv.FormatInt(int64(idx), 10))
	o.key("ifindex")
	o.obj = strconv.AppendInt(o.obj, int64(idx), 10)
	if idx != 0 {
		name := dev(idx)
		o.str("dev", " dev="+name, name)
	}
}

// mptcpEventFlags is the event's flags: server_side from the flag or a
// nonzero SERVER_SIDE, deny_join_id0, then what is left, %#x.
func mptcpEventFlags(o *mptcpOut, e *xtcpnl.MPTCPEvent) {
	var flags uint16
	if e.Flags != nil {
		flags = *e.Flags
	}
	if flags&xtcpnl.MPTCPEventFlagServerSide != 0 || (e.ServerSide != nil && *e.ServerSide != 0) {
		flags &^= xtcpnl.MPTCPEventFlagServerSide
		o.boolean("server_side", " server_side")
	}
	if flags&xtcpnl.MPTCPEventFlagDenyJoinID0 != 0 {
		flags &^= xtcpnl.MPTCPEventFlagDenyJoinID0
		o.boolean("deny_join_id0", " deny_join_id0")
	}
	if flags != 0 {
		v := hex0x(uint64(flags))
		o.str("flags", " flags="+v, v)
	}
}
