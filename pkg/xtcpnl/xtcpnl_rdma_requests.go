package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// ErrRdmaCmd is returned for an nldev command BuildRdmaRequest does not
// admit, and for flags outside NLM_F_REQUEST|NLM_F_ACK|NLM_F_DUMP.
var ErrRdmaCmd = errors.New("xtcpnl: not an admitted read-only RDMA nldev command")

// rdmaRequestFlagsCst are the only flags an nldev read sets. iproute2's rdma
// sends REQUEST|ACK for a doit and adds DUMP for a dump (rd_prepare_msg's
// callers in rdma/*.c).
const rdmaRequestFlagsCst = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_DUMP

// IsRdmaGetCmd reports whether cmd is a read-only nldev command: the device,
// port, system, resource summary, counter and per-object resource GETs. The
// _RAW resource dumps are refused: they hand out raw driver contexts, and
// nldev_cb_table (drivers/infiniband/core/nldev.c) marks them
// RDMA_NL_ADMIN_PERM, as it does every write. Every SET, NEW, DEL and
// GET_CHARDEV is refused, the last because the kernel answers it by
// resolving a char device for the caller, which is not a read of state.
func IsRdmaGetCmd(cmd uint16) bool {
	switch cmd {
	case RdmaNldevCmdGet, RdmaNldevCmdPortGet, RdmaNldevCmdSysGet,
		RdmaNldevCmdResGet, RdmaNldevCmdResQPGet, RdmaNldevCmdResCMIDGet,
		RdmaNldevCmdResCQGet, RdmaNldevCmdResMRGet, RdmaNldevCmdResPDGet,
		RdmaNldevCmdResCtxGet, RdmaNldevCmdResSRQGet, RdmaNldevCmdStatGet,
		RdmaNldevCmdStatGetStatus:
		return true
	}
	return false
}

// BuildRdmaRequest lays out an nldev request: an nlmsghdr of type
// RDMA_NL_GET_TYPE(RDMA_NL_NLDEV, cmd) and then attrs. nldev has no family
// header, so the attributes start right after the nlmsghdr, which is why
// BuildRequest (whose type gate is rtnetlink's arithmetic) is not used.
//
// NLM_F_REQUEST is always set. flags may add only NLM_F_ACK and NLM_F_DUMP.
func BuildRdmaRequest(cmd, flags uint16, seq uint32, attrs []byte) ([]byte, error) {
	if !IsRdmaGetCmd(cmd) || flags&^rdmaRequestFlagsCst != 0 {
		return nil, ErrRdmaCmd
	}
	return layoutRequest(RdmaNlGetType(RdmaNlNldev, cmd), flags|unix.NLM_F_REQUEST, seq, nil, attrs), nil
}

// rdmaDoitFlagsCst and rdmaDumpFlagsCst are what rdma sends.
const (
	rdmaDoitFlagsCst  = unix.NLM_F_REQUEST | unix.NLM_F_ACK
	rdmaDumpFlagsCst  = rdmaDoitFlagsCst | unix.NLM_F_DUMP
	rdmaU32AttrLenCst = 8
	// rdmaU8AttrLenCst is a u8 attribute's rta_len; it occupies 8 aligned bytes.
	rdmaU8AttrLenCst = 5
)

// rdmaIndexAttrs encodes u32 index attributes, type then value, in the order
// given: rdma's mnl_attr_put_u32 calls, one aligned 8-byte attribute each.
func rdmaIndexAttrs(kv ...uint32) []byte {
	buf := make([]byte, len(kv)/2*rdmaU32AttrLenCst)
	for i := 0; i+1 < len(kv); i += 2 {
		o := i / 2 * rdmaU32AttrLenCst
		binary.LittleEndian.PutUint16(buf[o:o+2], rdmaU32AttrLenCst)
		binary.LittleEndian.PutUint16(buf[o+2:o+4], uint16(kv[i]))
		binary.LittleEndian.PutUint32(buf[o+4:o+8], kv[i+1])
	}
	return buf
}

// rdmaRequest is BuildRdmaRequest for this file's wrappers, whose commands
// and flags are admitted by construction.
func rdmaRequest(cmd, flags uint16, seq uint32, attrs []byte) []byte {
	return layoutRequest(RdmaNlGetType(RdmaNlNldev, cmd), flags, seq, nil, attrs)
}

// BuildRdmaDevDumpRequest is the GET dump rdma opens every command with, to
// map device names to indexes (rd_dev_init_cb, rdma/rdma.c).
func BuildRdmaDevDumpRequest(seq uint32) []byte {
	return rdmaRequest(RdmaNldevCmdGet, rdmaDumpFlagsCst, seq, nil)
}

// BuildRdmaDevGetRequest is `rdma dev show <dev>`'s doit (dev_no_args,
// rdma/dev.c).
func BuildRdmaDevGetRequest(dev, seq uint32) []byte {
	return rdmaRequest(RdmaNldevCmdGet, rdmaDoitFlagsCst, seq,
		rdmaIndexAttrs(uint32(RdmaNldevAttrDevIndex), dev))
}

// BuildRdmaPortGetRequest is `rdma link show <dev>/<port>`'s doit
// (link_no_args, rdma/link.c). The kernel answers it with a GET-typed
// message, not a PORT_GET one; see ParseRdmaMsg.
func BuildRdmaPortGetRequest(dev, port, seq uint32) []byte {
	return rdmaRequest(RdmaNldevCmdPortGet, rdmaDoitFlagsCst, seq,
		rdmaIndexAttrs(uint32(RdmaNldevAttrDevIndex), dev, uint32(RdmaNldevAttrPortIndex), port))
}

// BuildRdmaSysGetRequest is `rdma system show` (sys_show_no_args,
// rdma/sys.c).
func BuildRdmaSysGetRequest(seq uint32) []byte {
	return rdmaRequest(RdmaNldevCmdSysGet, rdmaDoitFlagsCst, seq, nil)
}

// BuildRdmaResGetRequest is `rdma res show`'s per-device doit
// (_res_send_msg, rdma/res.c): RES_GET, with DRIVER_DETAILS 0 ahead of the
// device index, because rdma puts it on every resource request. port 0 sends
// no PORT_INDEX, as rdma does when no port was named.
func BuildRdmaResGetRequest(dev, port, seq uint32) []byte {
	// DRIVER_DETAILS: rta_len 5, the u8 0, three bytes of padding.
	attrs := binary.LittleEndian.AppendUint16(nil, rdmaU8AttrLenCst)
	attrs = binary.LittleEndian.AppendUint16(attrs, RdmaNldevAttrDriverDetails)
	attrs = append(attrs, 0, 0, 0, 0)
	attrs = append(attrs, rdmaIndexAttrs(uint32(RdmaNldevAttrDevIndex), dev)...)
	if port != 0 {
		attrs = append(attrs, rdmaIndexAttrs(uint32(RdmaNldevAttrPortIndex), port)...)
	}
	return rdmaRequest(RdmaNldevCmdResGet, rdmaDoitFlagsCst, seq, attrs)
}

// BuildRdmaStatGetRequest is `rdma statistic show link <dev>/<port>`
// (stat_show_one_link, rdma/stat.c): STAT_GET with no STAT_RES, which the
// kernel answers with the port's hardware counters.
func BuildRdmaStatGetRequest(dev, port, seq uint32) []byte {
	return rdmaRequest(RdmaNldevCmdStatGet, rdmaDoitFlagsCst, seq,
		rdmaIndexAttrs(uint32(RdmaNldevAttrDevIndex), dev, uint32(RdmaNldevAttrPortIndex), port))
}
