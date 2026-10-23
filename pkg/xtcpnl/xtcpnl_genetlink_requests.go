package xtcpnl

import (
	"encoding/binary"
	"strings"

	"golang.org/x/sys/unix"
)

// BuildGetFamilyRequest looks up a named generic-netlink family. The controller
// ID is fixed by UAPI; the returned family and multicast IDs are dynamic.
// No ACK is requested: transaction completion requires the family reply.
func BuildGetFamilyRequest(name string, seq uint32) ([]byte, error) {
	return BuildGetFamilyRequestVersion(name, 2, seq)
}

// BuildGetFamilyRequestVersion is BuildGetFamilyRequest with the genlmsghdr
// version given. iproute2's genl_resolve_family sends version 0
// (GENL_REQUEST's _ver, lib/libgenl.c:52), and the controller does not read it,
// so a parity caller passes 0 and everything else keeps 2.
func BuildGetFamilyRequestVersion(name string, version uint8, seq uint32) ([]byte, error) {
	// GENL_NAMSIZ includes the terminator.
	if name == "" || len(name) >= 16 || strings.ContainsRune(name, 0) {
		return nil, ErrGenericNetlink
	}
	var raw [20]byte
	a := NewAttrBuilder(raw[:])
	if err := a.PutString(2, name); err != nil {
		return nil, err
	}
	return buildGenericGet(GenlControllerID, 3, version, seq, a.Bytes()), nil
}

// BuildGetFamilyByIDRequest builds genl_add_mcast_grp's CTRL_CMD_GETFAMILY
// (lib/libgenl.c): version 0, the family by CTRL_ATTR_FAMILY_ID, whose
// answer names the family's multicast groups. id must be a family's, above
// the controller's.
func BuildGetFamilyByIDRequest(id uint16, seq uint32) ([]byte, error) {
	if id <= GenlControllerID {
		return nil, ErrGenericNetlink
	}
	var raw [8]byte
	a := NewAttrBuilder(raw[:])
	if err := a.PutU16(1, id); err != nil {
		return nil, err
	}
	return buildGenericGet(GenlControllerID, 3, 0, seq, a.Bytes()), nil
}

// BuildGetEthtoolRequest builds a read-only, single-device GET. family must be
// resolved as ethtool by the caller. Compact bitsets are requested; mutation,
// omit-reply and arbitrary command flags are not accepted by this API.
func BuildGetEthtoolRequest(family uint16, kind EthtoolKind, ifindex, seq uint32) ([]byte, error) {
	if family <= GenlControllerID || ifindex == 0 || ifindex > 0x7fffffff {
		return nil, ErrGenericNetlink
	}
	command, err := ethtoolGetCommand(kind)
	if err != nil {
		return nil, err
	}
	// HEADER (nested), DEV_INDEX (u32), FLAGS (u32). Exact, aligned lengths.
	var attrs [20]byte
	binary.LittleEndian.PutUint16(attrs[0:2], 20)
	binary.LittleEndian.PutUint16(attrs[2:4], 1|unix.NLA_F_NESTED)
	binary.LittleEndian.PutUint16(attrs[4:6], 8)
	binary.LittleEndian.PutUint16(attrs[6:8], 1)
	binary.LittleEndian.PutUint32(attrs[8:12], ifindex)
	binary.LittleEndian.PutUint16(attrs[12:14], 8)
	binary.LittleEndian.PutUint16(attrs[14:16], 3)
	binary.LittleEndian.PutUint32(attrs[16:20], 1) // ETHTOOL_FLAG_COMPACT_BITSETS
	return buildGenericGet(family, command, 1, seq, attrs[:]), nil
}

func ethtoolGetCommand(kind EthtoolKind) (uint8, error) {
	switch kind {
	case EthtoolLinkInfoKind:
		return 2, nil
	case EthtoolLinkModesKind:
		return 4, nil
	case EthtoolLinkStateKind:
		return 6, nil
	case EthtoolRingsKind:
		return 15, nil
	case EthtoolChannelsKind:
		return 17, nil
	case EthtoolPauseKind:
		return 21, nil
	case EthtoolFECKind:
		return 29, nil
	default:
		return 0, ErrUnsupportedEthtool
	}
}

// Private: callers above bound sizes and select only UAPI GET commands.
func buildGenericGet(family uint16, command, version uint8, seq uint32, attrs []byte) []byte {
	out := make([]byte, NlMsgHdrSizeCst+4+len(attrs))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint16(out[4:6], family)
	binary.LittleEndian.PutUint16(out[6:8], unix.NLM_F_REQUEST)
	binary.LittleEndian.PutUint32(out[8:12], seq)
	out[16], out[17] = command, version
	copy(out[20:], attrs)
	return out
}
