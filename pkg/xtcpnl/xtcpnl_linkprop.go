package xtcpnl

import (
	"bytes"

	"golang.org/x/sys/unix"
)

// This file decodes RTM_NEWLINKPROP and RTM_DELLINKPROP, the requests
// `ip link property add` and `ip link property del` send to add or remove a
// link's alternative names (ip/iplink.c). Only ip sends them: the kernel
// answers with an ack and announces the change as an RTM_NEWLINK, whose
// IFLA_PROP_LIST ParseNewLink decodes.

// LinkPropInfo is a decoded RTM_*LINKPROP body: the ifinfomsg header, the
// selectors the kernel's rtnl_linkprop (net/core/rtnetlink.c) finds the link
// by, and the alternative names in IFLA_PROP_LIST.
//
// The kernel uses Index when it is positive, and otherwise IfName or
// AltIfName. ip resolves the device itself and sends its index.
type LinkPropInfo struct {
	Family uint8
	Type   uint16
	Index  int32
	Flags  uint32
	Change uint32

	IfName    string // IFLA_IFNAME, a selector
	AltIfName string // IFLA_ALT_IFNAME outside the list, a selector

	HasPropList bool
	AltNames    []string // the IFLA_ALT_IFNAME entries in IFLA_PROP_LIST, in wire order
}

// ParseLinkProp decodes an RTM_NEWLINKPROP or RTM_DELLINKPROP body: the
// ifinfomsg header then the IFLA_* attributes. A repeated top-level
// attribute keeps the last, as the kernel's nla_parse, which reads this
// request, does. Every IFLA_ALT_IFNAME inside IFLA_PROP_LIST is kept, since
// rtnl_linkprop acts on each; the nest is read by linkAltNames, as in
// ParseNewLink, so an empty name is skipped and a malformed entry ends the
// list.
func ParseLinkProp(body []byte) (LinkPropInfo, error) {
	var lp LinkPropInfo
	var h IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &h); err != nil {
		return lp, err
	}
	lp.Family = h.Family
	lp.Type = h.Type
	lp.Index = h.Index
	lp.Flags = h.Flags
	lp.Change = h.Change

	err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFLA_IFNAME):
			lp.IfName = string(bytes.TrimRight(val, "\x00"))
		case uint16(unix.IFLA_ALT_IFNAME):
			lp.AltIfName = string(bytes.TrimRight(val, "\x00"))
		case uint16(unix.IFLA_PROP_LIST):
			lp.HasPropList = true
			lp.AltNames = linkAltNames(val)
		}
	})
	if err != nil {
		return lp, err
	}
	return lp, nil
}
