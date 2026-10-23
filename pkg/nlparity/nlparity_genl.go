package nlparity

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Generic netlink, compared by family name rather than by nlmsg_type.
//
// A NETLINK_GENERIC message's nlmsg_type is its family's id, and that id is
// allocated when the family registers: fou is 41 in one boot and something
// else after a different module load order. iproute2 never hard-codes one; it
// asks the controller for the family by name (genl_resolve_family,
// lib/libgenl.c:50-57) and every C2 capture opens with that exchange, a
// CTRL_CMD_GETFAMILY request and the CTRL_CMD_NEWFAMILY reply. So a capture
// carries its own map from id to name, and ParseCapture reads it: each genl
// message is labeled with its family's name (Msg.Genl), and the comparator
// treats the pair (family name, genlmsghdr.cmd) as the message's kind. Two
// captures of the same command whose families were given different ids then
// compare clean, and the volatile ids the controller reports, the family's
// own and its multicast groups', are normalized away like a timestamp.

const (
	// genlHdrLen is sizeof(struct genlmsghdr): cmd, version and a reserved
	// u16, the family header of every generic netlink message.
	genlHdrLen = unix.GENL_HDRLEN

	// genlCtrlName is the controller's own family name, which it answers to
	// at the fixed id GENL_ID_CTRL and so never reports in a capture.
	genlCtrlName = "nlctrl"

	// nlmsgerrEchoTypeOffCst is the offset of the echoed nlmsg_type inside
	// an NLMSG_ERROR body: the 4-byte errno, then the nlmsghdr's len.
	nlmsgerrEchoTypeOffCst = 8
	nlmsgerrEchoTypeLenCst = 2

	// genlFamilyIDLenCst is CTRL_ATTR_FAMILY_ID's u16 and
	// genlGrpIDLenCst CTRL_ATTR_MCAST_GRP_ID's u32.
	genlFamilyIDLenCst = 2
	genlGrpIDLenCst    = 4
)

// ErrGenlFamilyConflict indicates a capture whose controller replies gave one
// family id two names, or one name two ids. Within one boot neither can
// happen while both families are registered, so the capture spans a module
// unload, and labeling its messages by either name would be a guess.
var ErrGenlFamilyConflict = errors.New("nlparity: generic netlink family id named twice")

// IsGenl reports whether m is a generic netlink message carrying a family's
// command, as opposed to one of the NLMSG_* control messages below
// NLMSG_MIN_TYPE, which mean the same thing on every protocol.
func (m Msg) IsGenl() bool {
	return m.Proto == unix.NETLINK_GENERIC && m.Hdr.Type >= unix.NLMSG_MIN_TYPE
}

// resolveGenl labels every generic netlink message in c with its family's
// name, from the capture's own CTRL_CMD_NEWFAMILY replies.
//
// The map is read from the whole capture before any message is labeled, so a
// message ahead of the reply that names its family (a second process's, say)
// is labeled too. A reply that does not decode names nothing; its messages
// stay unlabeled and compare by id, which is the comparison this replaces and
// so a safe fallback rather than a guess.
func resolveGenl(c *Capture) error {
	byID := map[uint16]string{unix.GENL_ID_CTRL: genlCtrlName}
	byName := map[string]uint16{genlCtrlName: unix.GENL_ID_CTRL}

	for _, d := range c.Datagrams {
		for _, m := range d.Msgs {
			if m.Hdr.Type != unix.GENL_ID_CTRL || m.IsRequest() {
				continue
			}
			f, err := xtcpnl.ParseGenericNetlinkFamily(m.Body)
			if err != nil {
				continue
			}
			if n, ok := byID[f.ID]; ok && n != f.Name {
				return fmt.Errorf("%w: id %d is %q and %q", ErrGenlFamilyConflict, f.ID, n, f.Name)
			}
			if id, ok := byName[f.Name]; ok && id != f.ID {
				return fmt.Errorf("%w: %q is id %d and %d", ErrGenlFamilyConflict, f.Name, id, f.ID)
			}
			byID[f.ID], byName[f.Name] = f.Name, f.ID
		}
	}

	for i := range c.Datagrams {
		msgs := c.Datagrams[i].Msgs
		for j := range msgs {
			if msgs[j].IsGenl() {
				msgs[j].Genl = byID[msgs[j].Hdr.Type]
			}
		}
	}
	c.GenlFamilies = byID
	return nil
}

// genlMsgName names a generic netlink message for a locus and for its kind:
// GENL(fou).cmd(1), with the controller's two commands spelled out. A family
// the capture never resolved is named by its id, GENL(id=41), and a body too
// short for its genlmsghdr has no command to name.
func genlMsgName(m Msg) string {
	fam := m.Genl
	if fam == "" {
		fam = fmt.Sprintf("id=%d", m.Hdr.Type)
	}
	if len(m.Body) < genlHdrLen {
		return fmt.Sprintf("GENL(%s).short", fam)
	}

	cmd := m.Body[0]
	if m.Hdr.Type == unix.GENL_ID_CTRL {
		switch cmd {
		case unix.CTRL_CMD_NEWFAMILY:
			return "GENL(nlctrl).CTRL_CMD_NEWFAMILY"
		case unix.CTRL_CMD_GETFAMILY:
			return "GENL(nlctrl).CTRL_CMD_GETFAMILY"
		}
	}
	return fmt.Sprintf("GENL(%s).cmd(%d)", fam, cmd)
}

// normalizeGenl zeroes what a generic netlink exchange reports that the boot,
// not the tool, decided: an NLMSG_ERROR's echoed nlmsg_type, which is the
// acknowledged request's family id, and in a controller reply the family's id
// and its multicast groups' ids. The names beside them stay compared, so a
// reply about a different family is still a finding. A tcp_metrics entry's
// clocks and measurements are the connection's (normalizeTCPMetrics).
func normalizeGenl(m Msg, body []byte) {
	if m.Proto != unix.NETLINK_GENERIC {
		return
	}
	if m.Hdr.Type == unix.NLMSG_ERROR {
		zeroRange(body, nlmsgerrEchoTypeOffCst, nlmsgerrEchoTypeLenCst)
		return
	}
	if m.IsGenl() && m.Genl == xtcpnl.TCPMetricsGenlName && len(body) >= genlHdrLen {
		normalizeTCPMetrics(body[genlHdrLen:])
		return
	}
	if m.Hdr.Type != unix.GENL_ID_CTRL || len(body) < genlHdrLen ||
		body[0] != unix.CTRL_CMD_NEWFAMILY {
		return
	}

	attrs, _ := SubAttrs(body[genlHdrLen:])
	for _, a := range attrs {
		switch a.BareType() {
		case unix.CTRL_ATTR_FAMILY_ID:
			zeroRange(a.Val, 0, genlFamilyIDLenCst)
		case unix.CTRL_ATTR_MCAST_GROUPS:
			groups, _ := SubAttrs(a.Val)
			for _, g := range groups {
				fields, _ := SubAttrs(g.Val)
				for _, f := range fields {
					if f.BareType() == unix.CTRL_ATTR_MCAST_GRP_ID {
						zeroRange(f.Val, 0, genlGrpIDLenCst)
					}
				}
			}
		}
	}
}

// genlObjectKey is a generic netlink family's object key, where the family
// gives its messages one: tcp_metrics' entry is its destination and source
// (TCP_METRICS_ATTR_ADDR_* and SADDR_*, tcp_metrics_fill_info), on a dump
// reply and on the DEL that copies it alike. The entries come in the order
// of a hash the kernel mixes with a per-namespace random value (net_hash_mix),
// so two namespaces list the same entries in different orders, and only a
// key pairs them. A family without one keys on its ordinal (keyReplies).
func genlObjectKey(m Msg) (string, bool) {
	if !m.IsGenl() || m.Genl != xtcpnl.TCPMetricsGenlName || len(m.Body) < genlHdrLen {
		return "", false
	}
	attrs, _ := SubAttrs(m.Body[genlHdrLen:])
	var addr, saddr []byte
	for _, a := range attrs {
		switch a.BareType() {
		case xtcpnl.TCPMetricsAttrAddrIPv4, xtcpnl.TCPMetricsAttrAddrIPv6:
			addr = a.Val
		case xtcpnl.TCPMetricsAttrSaddrIPv4, xtcpnl.TCPMetricsAttrSaddrIPv6:
			saddr = a.Val
		}
	}
	return fmt.Sprintf("%s/addr=%x/saddr=%x", genlMsgName(m), addr, saddr), true
}

// hashOrdered reports whether replies are a dump whose order the kernel's
// hash decides: a family genlObjectKey keys, tcp_metrics, whose table mixes
// each namespace's random net_hash_mix into the bucket. Two namespaces list
// the same entries in different orders, so the order is not compared; which
// entries, and each entry's values, are.
func hashOrdered(replies []Msg) bool {
	for i := range replies {
		if _, ok := genlObjectKey(replies[i]); ok {
			return true
		}
	}
	return false
}

// tcpMetricsMeasured are the TCP_METRICS_ATTR_VALS members a connection
// measures, tcp_metric_index + 1: RTT, RTTVAR, RTT_US and RTTVAR_US.
var tcpMetricsMeasured = [...]uint16{
	xtcpnl.TCPMetricRTT + 1, xtcpnl.TCPMetricRTTVar + 1,
	xtcpnl.TCPMetricRTTUs + 1, xtcpnl.TCPMetricRTTVarUs + 1,
}

// normalizeTCPMetrics zeroes a tcp_metrics entry's clocks, its age and the
// last Fast Open SYN drop's stamp, and its measured RTTs. A write row's
// sides each open their own connections in their own namespace, so these
// differ by however long each side took and however fast its loopback was,
// and the two ip sides can agree by chance where goip's does not. The
// addresses, cwnd, ssthresh and the rest stay compared.
func normalizeTCPMetrics(attrBytes []byte) {
	attrs, _ := SubAttrs(attrBytes)
	for _, a := range attrs {
		switch a.BareType() {
		case xtcpnl.TCPMetricsAttrAge, xtcpnl.TCPMetricsAttrFopenSynDropTS:
			zeroRange(a.Val, 0, len(a.Val))
		case xtcpnl.TCPMetricsAttrVals:
			vals, _ := SubAttrs(a.Val)
			for _, v := range vals {
				for _, measured := range tcpMetricsMeasured {
					if v.BareType() == measured {
						zeroRange(v.Val, 0, len(v.Val))
					}
				}
			}
		}
	}
}
