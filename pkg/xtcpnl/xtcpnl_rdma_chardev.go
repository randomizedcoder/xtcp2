package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// RDMA_NLDEV_CMD_GET_CHARDEV: which character device serves a device (and,
// for umad, a port). IsRdmaGetCmd keeps refusing the command, because the
// kernel answers it by resolving a char device for the caller rather than by
// reading state; this file is the one dedicated path that builds it.
//
// The request is libibverbs' (find_uverbs_nl, libibverbs/ibdev_nl.c):
// NLM_F_REQUEST alone, DEV_INDEX, then CHARDEV_TYPE. The reply is
// nldev_get_chardev's (drivers/infiniband/core/nldev.c): the client's
// get_nl_info attributes (UVERBS_DRIVER_ID for uverbs), CHARDEV as
// huge_encode_dev, CHARDEV_ABI, then CHARDEV_NAME.

var (
	// ErrRdmaChardevKind is a ChardevKind BuildRdmaGetChardev does not know,
	// or a port that does not fit the kind: uverbs is per device, so port must
	// be 0; umad is per port, so it must not be.
	ErrRdmaChardevKind = errors.New("xtcpnl: not a GET_CHARDEV kind and port this package builds")
	// ErrRdmaChardev is a GET_CHARDEV reply missing CHARDEV, CHARDEV_ABI or
	// CHARDEV_NAME, or whose name is not the kind's prefix and a number.
	ErrRdmaChardev = errors.New("xtcpnl: GET_CHARDEV reply is incomplete or names an unexpected device")
)

// ChardevKind is the RDMA_NLDEV_ATTR_CHARDEV_TYPE a request names: the
// ib_client whose char device is wanted.
type ChardevKind uint8

const (
	// ChardevUverbs is ib_uverbs' /dev/infiniband/uverbsN, one per device.
	ChardevUverbs ChardevKind = iota + 1
	// ChardevUmad is ib_umad's /dev/infiniband/umadN, one per port.
	ChardevUmad
)

// chardevType is the CHARDEV_TYPE string, with its terminator, which
// nldev_policy's NLA_NUL_STRING requires.
func (k ChardevKind) chardevType() string {
	switch k {
	case ChardevUverbs:
		return "uverbs\x00"
	case ChardevUmad:
		return "umad\x00"
	}
	return ""
}

// prefix is the char device name without its number.
func (k ChardevKind) prefix() string {
	t := k.chardevType()
	if t == "" {
		return ""
	}
	return t[:len(t)-1]
}

// RdmaChardevNameMaxCst bounds CHARDEV_NAME, terminator excluded. The names are
// "uverbs" or "umad" and a minor-derived number (IB_UVERBS_MAX_DEVICES and
// IB_UMAD_MAX_PORTS are both far below 10^9), so 15 leaves room while keeping
// RdmaChardev fixed-size.
const RdmaChardevNameMaxCst = 15

// RdmaChardev is a GET_CHARDEV reply. Name is a fixed array so parsing never
// allocates; Name[:NameLen] is the device name under /dev/infiniband.
type RdmaChardev struct {
	// Dev is RDMA_NLDEV_ATTR_CHARDEV, huge_encode_dev(devt).
	Dev uint64
	// ABI is RDMA_NLDEV_ATTR_CHARDEV_ABI. For uverbs it is the driver's
	// uverbs_abi_ver (mlx5 1, rxe 2), not the global uverbs ABI, which
	// libibverbs fixes at 6; for umad it is IB_USER_MAD_ABI_VERSION.
	ABI         uint64
	Name        [RdmaChardevNameMaxCst + 1]byte
	NameLen     uint8
	DriverID    uint32
	HasDriverID bool
}

// Major is Dev's major number, decoded as libibverbs does.
func (c *RdmaChardev) Major() uint32 { return uint32((c.Dev & 0xfff00) >> 8) }

// Minor is Dev's minor number, decoded as libibverbs does.
func (c *RdmaChardev) Minor() uint32 { return uint32((c.Dev & 0xff) | ((c.Dev >> 12) & 0xfff00)) }

// NameBytes is the device name, aliasing c.
func (c *RdmaChardev) NameBytes() []byte { return c.Name[:min(c.NameLen, RdmaChardevNameMaxCst)] }

// rdmaChardevFlagsCst is libibverbs' request flags: a doit with no ACK, which
// the unicast reply completes.
const rdmaChardevFlagsCst = unix.NLM_F_REQUEST

// BuildRdmaGetChardev appends one GET_CHARDEV request to buf and returns the
// extended slice: DEV_INDEX, PORT_INDEX for umad only, then CHARDEV_TYPE. With
// enough capacity in buf it does not allocate.
func BuildRdmaGetChardev(buf []byte, seq, dev, port uint32, kind ChardevKind) ([]byte, error) {
	t := kind.chardevType()
	if t == "" || (kind == ChardevUverbs) != (port == 0) {
		return buf, ErrRdmaChardevKind
	}
	n := NlMsgHdrSizeCst + rdmaU32AttrLenCst + RTAttrSizeCst + len(t) + FourByteAlignPadding(len(t))
	if port != 0 {
		n += rdmaU32AttrLenCst
	}
	buf = binary.LittleEndian.AppendUint32(buf, uint32(n))
	buf = binary.LittleEndian.AppendUint16(buf, RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGetChardev))
	buf = binary.LittleEndian.AppendUint16(buf, rdmaChardevFlagsCst)
	buf = binary.LittleEndian.AppendUint32(buf, seq)
	buf = binary.LittleEndian.AppendUint32(buf, 0) // nlmsg_pid, the kernel's
	buf = appendRdmaU32(buf, RdmaNldevAttrDevIndex, dev)
	if port != 0 {
		buf = appendRdmaU32(buf, RdmaNldevAttrPortIndex, port)
	}
	buf = binary.LittleEndian.AppendUint16(buf, uint16(RTAttrSizeCst+len(t)))
	buf = binary.LittleEndian.AppendUint16(buf, RdmaNldevAttrChardevType)
	buf = append(buf, t...)
	for range FourByteAlignPadding(len(t)) {
		buf = append(buf, 0)
	}
	return buf, nil
}

func appendRdmaU32(buf []byte, atype uint16, v uint32) []byte {
	buf = binary.LittleEndian.AppendUint16(buf, rdmaU32AttrLenCst)
	buf = binary.LittleEndian.AppendUint16(buf, atype)
	return binary.LittleEndian.AppendUint32(buf, v)
}

// ParseRdmaChardev decodes a GET_CHARDEV reply body into out, which it
// overwrites, and checks it against the kind the request named: CHARDEV,
// CHARDEV_ABI and CHARDEV_NAME are required, as libibverbs requires them; the
// name must be the kind's prefix and a decimal number, so it is a safe path
// component; UVERBS_DRIVER_ID is admitted only for uverbs, whose
// get_nl_info is the only one that sends it. Widths follow nldev_policy and a
// repeated attribute is last-wins, as everywhere in this package; other
// attributes, PAD included, are ignored. It does not allocate.
func ParseRdmaChardev(body []byte, kind ChardevKind, out *RdmaChardev) error {
	*out = RdmaChardev{}
	prefix := kind.prefix()
	if prefix == "" {
		return ErrRdmaChardevKind
	}
	var name []byte
	var hasDev, hasABI, hasName bool
	var verr error
	err := WalkRTAttrs(body, func(atype uint16, val []byte) {
		if verr != nil {
			return
		}
		switch atype {
		case RdmaNldevAttrChardev:
			if verr = rdmaValidate(rdmaKindU64, val); verr == nil {
				_ = val[7]
				out.Dev, hasDev = binary.LittleEndian.Uint64(val), true
			}
		case RdmaNldevAttrChardevABI:
			if verr = rdmaValidate(rdmaKindU64, val); verr == nil {
				_ = val[7]
				out.ABI, hasABI = binary.LittleEndian.Uint64(val), true
			}
		case RdmaNldevAttrChardevName:
			if verr = rdmaValidate(rdmaKindNulString, val); verr == nil {
				_ = val[len(val)-1]
				name, hasName = val[:len(val)-1], true
			}
		case RdmaNldevAttrUverbsDriverID:
			if verr = rdmaValidate(rdmaKindU32, val); verr == nil {
				_ = val[3]
				out.DriverID, out.HasDriverID = binary.LittleEndian.Uint32(val), true
			}
		}
	})
	if err == nil {
		err = verr
	}
	if err != nil {
		*out = RdmaChardev{}
		return err
	}
	if !hasDev || !hasABI || !hasName || !chardevName(name, prefix) ||
		(out.HasDriverID && kind != ChardevUverbs) {
		*out = RdmaChardev{}
		return ErrRdmaChardev
	}
	out.NameLen = uint8(copy(out.Name[:], name))
	return nil
}

// chardevName reports whether name is prefix and a decimal number with no
// leading zero, at most RdmaChardevNameMaxCst bytes: dev_set_name's
// "uverbs%d" and "umad%d".
func chardevName(name []byte, prefix string) bool {
	if len(name) > RdmaChardevNameMaxCst || len(name) <= len(prefix) {
		return false
	}
	for i, c := range name {
		switch {
		case i < len(prefix):
			if c != prefix[i] {
				return false
			}
		case c < '0' || c > '9':
			return false
		case c == '0' && i == len(prefix) && i+1 < len(name):
			return false
		}
	}
	return true
}
