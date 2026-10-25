package xtcpnl

// The vxcan IFLA_INFO_DATA encoder, as ip/iplink_vxcan.c at v7.2.0 writes it
// (docs/netlink/goip-link-kinds.md, "KP5-T03 The vrf, vrf_slave and vxcan
// parsers"). The kernel has no fill_info for vxcan, so nothing decodes.

// VxcanInfoPeer is VXCAN_INFO_PEER, include/uapi/linux/can/vxcan.h. x/sys/unix
// does not name it.
const VxcanInfoPeer uint16 = 1

// EncodeVxcanInfoData encodes the inside of IFLA_INFO_DATA for vxcan as
// iplink_vxcan.c's vxcan_parse_opt does: the first word must be peer, and
// everything after it is the peer's, so the nest is always one
// VXCAN_INFO_PEER holding the peer's ifinfomsg and attributes, peer alone
// included. A nil peer, no option words, encodes to nothing.
func EncodeVxcanInfoData(peer *VethPeer) ([]byte, error) {
	if peer == nil {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := putPeerNest(&ab, VxcanInfoPeer, *peer); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
