package xtcpnl

import "golang.org/x/sys/unix"

// VethPeer is the `peer` half of `type veth peer …`: its own ifinfomsg and
// attributes, which veth_parse_opt writes inside VETH_INFO_PEER by running
// iplink_parse a second time on the same request.
type VethPeer struct {
	Index    int32
	IfFlags  uint32
	IfChange uint32
	Attrs    []LinkAttr
}

// putPeerNest writes the peer half veth, netkit and vxcan share: a nest of
// atype holding the peer's raw ifinfomsg and then its attributes.
func putPeerNest(ab *AttrBuilder, atype uint16, p VethPeer) error {
	m, err := ab.BeginNest(atype)
	if err != nil {
		return err
	}
	if err := putRaw(ab, EncodeIfInfomsg(unix.AF_UNSPEC, p.Index, p.IfFlags, p.IfChange)); err != nil {
		return err
	}
	for _, a := range p.Attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return err
		}
	}
	return ab.EndNest(m)
}
