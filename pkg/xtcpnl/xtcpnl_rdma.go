package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// RDMA nldev decoding: the replies iproute2's read-only `rdma` commands get
// on NETLINK_RDMA, and the MONITOR events on RDMA_NL_GROUP_NOTIFY. Requests
// are in xtcpnl_rdma_requests.go, constants in xtcpnl_rdma_consts.go.
//
// An nldev message has no family header: the attributes start right after
// the nlmsghdr, and the body every ParseRdma* function takes is that
// attribute stream.
//
// # Validation and duplicates follow rdma's rd_attr_cb
//
// rdma/utils.c parses every message and every nest with rd_attr_cb, which
// validates each attribute it knows against nldev_policy, fails the whole
// message on a mismatch, skips the ones it does not know, and stores
// tb[type] = attr, so a repeated attribute is last-wins. The decoders do the
// same, with the widths of drivers/infiniband/core/nldev.c's nldev_policy:
// u8, u32 and u64 attributes must be exactly that wide (ErrRdmaAttrLen); a
// name must be non-empty and NUL-terminated (ErrRdmaAttrString), except
// PARENT_NAME, which rdma's policy has as MNL_TYPE_STRING and so only
// requires to be non-empty; a nest is empty or holds at least one attribute
// header; SRC_ADDR and DST_ADDR are a whole sockaddr_storage
// (xtcpnl_rdma_res.go). An attribute this file does not decode is recorded in Unknown, in
// wire order, nested ones included, and the rest of the message still
// decodes.

var (
	// ErrRdmaAttrLen is a fixed-width nldev attribute of the wrong width.
	ErrRdmaAttrLen = errors.New("xtcpnl: RDMA nldev attribute has the wrong length for its type")
	// ErrRdmaAttrString is an nldev name that is empty or not NUL-terminated.
	ErrRdmaAttrString = errors.New("xtcpnl: RDMA nldev string attribute is empty or not NUL-terminated")
	// ErrRdmaMsgType is a message ParseRdmaMsg does not decode.
	ErrRdmaMsgType = errors.New("xtcpnl: not a decoded RDMA nldev message type")
)

type rdmaKind uint8

const (
	rdmaKindNone rdmaKind = iota
	rdmaKindU8
	rdmaKindU32
	rdmaKindU64
	rdmaKindNulString
	rdmaKindString
	rdmaKindNested
	// rdmaKindSockaddr is a whole struct __kernel_sockaddr_storage.
	rdmaKindSockaddr
)

// rdmaAttrSlotsCst bounds the attribute types the decoders index; every
// attribute they decode is below it, and anything at or above is Unknown.
const rdmaAttrSlotsCst = 128

// rdmaPolicy is nldev_policy for the attributes these decoders read.
var rdmaPolicy = [rdmaAttrSlotsCst]rdmaKind{
	RdmaNldevAttrDevIndex:                rdmaKindU32,
	RdmaNldevAttrDevName:                 rdmaKindNulString,
	RdmaNldevAttrPortIndex:               rdmaKindU32,
	RdmaNldevAttrCapFlags:                rdmaKindU64,
	RdmaNldevAttrFwVersion:               rdmaKindNulString,
	RdmaNldevAttrNodeGUID:                rdmaKindU64,
	RdmaNldevAttrSysImageGUID:            rdmaKindU64,
	RdmaNldevAttrSubnetPrefix:            rdmaKindU64,
	RdmaNldevAttrLID:                     rdmaKindU32,
	RdmaNldevAttrSMLID:                   rdmaKindU32,
	RdmaNldevAttrLMC:                     rdmaKindU8,
	RdmaNldevAttrPortState:               rdmaKindU8,
	RdmaNldevAttrPortPhysState:           rdmaKindU8,
	RdmaNldevAttrDevNodeType:             rdmaKindU8,
	RdmaNldevAttrResSummary:              rdmaKindNested,
	RdmaNldevAttrResSummaryEntry:         rdmaKindNested,
	RdmaNldevAttrResSummaryEntryName:     rdmaKindNulString,
	RdmaNldevAttrResSummaryEntryCurr:     rdmaKindU64,
	RdmaNldevAttrResQP:                   rdmaKindNested,
	RdmaNldevAttrResQPEntry:              rdmaKindNested,
	RdmaNldevAttrResLQPN:                 rdmaKindU32,
	RdmaNldevAttrResRQPN:                 rdmaKindU32,
	RdmaNldevAttrResRqPSN:                rdmaKindU32,
	RdmaNldevAttrResSqPSN:                rdmaKindU32,
	RdmaNldevAttrResPathMigState:         rdmaKindU8,
	RdmaNldevAttrResType:                 rdmaKindU8,
	RdmaNldevAttrResState:                rdmaKindU8,
	RdmaNldevAttrResPID:                  rdmaKindU32,
	RdmaNldevAttrResKernName:             rdmaKindNulString,
	RdmaNldevAttrResCMID:                 rdmaKindNested,
	RdmaNldevAttrResCMIDEntry:            rdmaKindNested,
	RdmaNldevAttrResPS:                   rdmaKindU32,
	RdmaNldevAttrResSrcAddr:              rdmaKindSockaddr,
	RdmaNldevAttrResDstAddr:              rdmaKindSockaddr,
	RdmaNldevAttrResCQ:                   rdmaKindNested,
	RdmaNldevAttrResCQEntry:              rdmaKindNested,
	RdmaNldevAttrResCQE:                  rdmaKindU32,
	RdmaNldevAttrResUsecnt:               rdmaKindU64,
	RdmaNldevAttrResPollCtx:              rdmaKindU8,
	RdmaNldevAttrResMR:                   rdmaKindNested,
	RdmaNldevAttrResMREntry:              rdmaKindNested,
	RdmaNldevAttrResRKey:                 rdmaKindU32,
	RdmaNldevAttrResLKey:                 rdmaKindU32,
	RdmaNldevAttrResIOVA:                 rdmaKindU64,
	RdmaNldevAttrResMrlen:                rdmaKindU64,
	RdmaNldevAttrResPD:                   rdmaKindNested,
	RdmaNldevAttrResPDEntry:              rdmaKindNested,
	RdmaNldevAttrResLocalDMALKey:         rdmaKindU32,
	RdmaNldevAttrResUnsafeGlobalRKey:     rdmaKindU32,
	RdmaNldevAttrNdevIndex:               rdmaKindU32,
	RdmaNldevAttrNdevName:                rdmaKindNulString,
	RdmaNldevAttrResPDN:                  rdmaKindU32,
	RdmaNldevAttrResCQN:                  rdmaKindU32,
	RdmaNldevAttrResMRN:                  rdmaKindU32,
	RdmaNldevAttrResCMIDN:                rdmaKindU32,
	RdmaNldevAttrResCtxn:                 rdmaKindU32,
	RdmaNldevSysAttrNetnsMode:            rdmaKindU8,
	RdmaNldevAttrDevProtocol:             rdmaKindNulString,
	RdmaNldevAttrStatHwcounters:          rdmaKindNested,
	RdmaNldevAttrStatHwcounterEntry:      rdmaKindNested,
	RdmaNldevAttrStatHwcounterEntryName:  rdmaKindNulString,
	RdmaNldevAttrStatHwcounterEntryValue: rdmaKindU64,
	RdmaNldevAttrDevDIM:                  rdmaKindU8,
	RdmaNldevAttrResCtx:                  rdmaKindNested,
	RdmaNldevAttrResCtxEntry:             rdmaKindNested,
	RdmaNldevAttrResSRQ:                  rdmaKindNested,
	RdmaNldevAttrResSRQEntry:             rdmaKindNested,
	RdmaNldevAttrResSRQN:                 rdmaKindU32,
	RdmaNldevAttrMinRange:                rdmaKindU32,
	RdmaNldevAttrMaxRange:                rdmaKindU32,
	RdmaNldevSysAttrCopyOnFork:           rdmaKindU8,
	RdmaNldevAttrStatHwcounterIndex:      rdmaKindU32,
	RdmaNldevAttrStatHwcounterDynamic:    rdmaKindU8,
	RdmaNldevSysAttrPrivilegedQKeyMode:   rdmaKindU8,
	RdmaNldevAttrResSubtype:              rdmaKindNulString,
	RdmaNldevAttrDevType:                 rdmaKindU8,
	RdmaNldevAttrParentName:              rdmaKindString,
	RdmaNldevAttrNameAssignType:          rdmaKindU8,
	RdmaNldevAttrEventType:               rdmaKindU8,
	RdmaNldevSysAttrMonitorMode:          rdmaKindU8,
}

// rdmaAttrs is rd_attr_cb's tb: the last value of each decoded attribute.
type rdmaAttrs struct {
	tb  [rdmaAttrSlotsCst][]byte
	has [rdmaAttrSlotsCst]bool
}

func rdmaKindOf(atype uint16) rdmaKind {
	if int(atype) >= len(rdmaPolicy) {
		return rdmaKindNone
	}
	return rdmaPolicy[atype]
}

func rdmaValidate(k rdmaKind, v []byte) error {
	switch k {
	case rdmaKindU8:
		if len(v) != 1 {
			return ErrRdmaAttrLen
		}
	case rdmaKindU32:
		if len(v) != 4 {
			return ErrRdmaAttrLen
		}
	case rdmaKindU64:
		if len(v) != 8 {
			return ErrRdmaAttrLen
		}
	case rdmaKindNulString:
		if len(v) == 0 || v[len(v)-1] != 0 {
			return ErrRdmaAttrString
		}
	case rdmaKindString:
		if len(v) == 0 {
			return ErrRdmaAttrString
		}
	case rdmaKindNested:
		if len(v) != 0 && len(v) < RTAttrSizeCst {
			return ErrRdmaAttrLen
		}
	case rdmaKindSockaddr:
		if len(v) != rdmaSockaddrLenCst {
			return ErrRdmaAttrLen
		}
	case rdmaKindNone:
	}
	return nil
}

// walkRdmaAttrs fills a tb from body. Types it does not decode are appended
// to *unknown.
func walkRdmaAttrs(body []byte, unknown *[]uint16) (*rdmaAttrs, error) {
	a := &rdmaAttrs{}
	var verr error
	err := WalkRTAttrs(body, func(atype uint16, val []byte) {
		if verr != nil {
			return
		}
		k := rdmaKindOf(atype)
		if k == rdmaKindNone {
			*unknown = append(*unknown, atype)
			return
		}
		if verr = rdmaValidate(k, val); verr != nil {
			return
		}
		a.tb[atype], a.has[atype] = val, true
	})
	if err != nil {
		return nil, err
	}
	if verr != nil {
		return nil, verr
	}
	return a, nil
}

func (a *rdmaAttrs) u8(t uint16) (uint8, bool) {
	if !a.has[t] {
		return 0, false
	}
	return a.tb[t][0], true
}

func (a *rdmaAttrs) u32(t uint16) (uint32, bool) {
	if !a.has[t] {
		return 0, false
	}
	return binary.LittleEndian.Uint32(a.tb[t]), true
}

func (a *rdmaAttrs) u64(t uint16) (uint64, bool) {
	if !a.has[t] {
		return 0, false
	}
	return binary.LittleEndian.Uint64(a.tb[t]), true
}

// str returns a name without its terminator. A PARENT_NAME without one, which
// rdma's policy allows, is returned whole.
func (a *rdmaAttrs) str(t uint16) (string, bool) {
	if !a.has[t] {
		return "", false
	}
	v := a.tb[t]
	if len(v) > 0 && v[len(v)-1] == 0 {
		v = v[:len(v)-1]
	}
	return string(v), true
}

// RdmaHandle is the device, and for port-scoped messages the port, a message
// is about: DEV_INDEX, DEV_NAME and PORT_INDEX, which nldev puts first in
// every reply (fill_nldev_handle and fill_port_info, nldev.c).
type RdmaHandle struct {
	DevIndex     uint32
	HasDevIndex  bool
	DevName      string
	HasDevName   bool
	PortIndex    uint32
	HasPortIndex bool
}

func (h *RdmaHandle) fill(a *rdmaAttrs) {
	h.DevIndex, h.HasDevIndex = a.u32(RdmaNldevAttrDevIndex)
	h.DevName, h.HasDevName = a.str(RdmaNldevAttrDevName)
	h.PortIndex, h.HasPortIndex = a.u32(RdmaNldevAttrPortIndex)
}

// RdmaNetdev is the netdev an RDMA port is bound to, NDEV_INDEX and
// NDEV_NAME. The kernel sends them only when the netdev is in the reader's
// network namespace (fill_port_info, nldev.c).
type RdmaNetdev struct {
	NdevIndex    uint32
	HasNdevIndex bool
	NdevName     string
	HasNdevName  bool
}

func (n *RdmaNetdev) fill(a *rdmaAttrs) {
	n.NdevIndex, n.HasNdevIndex = a.u32(RdmaNldevAttrNdevIndex)
	n.NdevName, n.HasNdevName = a.str(RdmaNldevAttrNdevName)
}

// RdmaDev is a device, the RDMA_NLDEV_CMD_GET reply (fill_dev_info,
// nldev.c). In a device reply PORT_INDEX is the number of ports, which is
// why it is NumPorts here.
type RdmaDev struct {
	DevIndex          uint32
	HasDevIndex       bool
	DevName           string
	HasDevName        bool
	NumPorts          uint32
	HasNumPorts       bool
	CapFlags          uint64
	HasCapFlags       bool
	FwVersion         string
	HasFwVersion      bool
	NodeGUID          uint64
	HasNodeGUID       bool
	SysImageGUID      uint64
	HasSysImageGUID   bool
	NodeType          uint8
	HasNodeType       bool
	DevProtocol       string
	HasDevProtocol    bool
	DIM               uint8
	HasDIM            bool
	NameAssignType    uint8
	HasNameAssignType bool
	DevType           uint8
	HasDevType        bool
	ParentName        string
	HasParentName     bool
	Unknown           []uint16
}

// ParseRdmaDev decodes a device reply body.
func ParseRdmaDev(body []byte) (RdmaDev, error) {
	var d RdmaDev
	a, err := walkRdmaAttrs(body, &d.Unknown)
	if err != nil {
		return RdmaDev{}, err
	}
	var h RdmaHandle
	h.fill(a)
	d.DevIndex, d.HasDevIndex = h.DevIndex, h.HasDevIndex
	d.DevName, d.HasDevName = h.DevName, h.HasDevName
	d.NumPorts, d.HasNumPorts = h.PortIndex, h.HasPortIndex
	d.CapFlags, d.HasCapFlags = a.u64(RdmaNldevAttrCapFlags)
	d.FwVersion, d.HasFwVersion = a.str(RdmaNldevAttrFwVersion)
	d.NodeGUID, d.HasNodeGUID = a.u64(RdmaNldevAttrNodeGUID)
	d.SysImageGUID, d.HasSysImageGUID = a.u64(RdmaNldevAttrSysImageGUID)
	d.NodeType, d.HasNodeType = a.u8(RdmaNldevAttrDevNodeType)
	d.DevProtocol, d.HasDevProtocol = a.str(RdmaNldevAttrDevProtocol)
	d.DIM, d.HasDIM = a.u8(RdmaNldevAttrDevDIM)
	d.NameAssignType, d.HasNameAssignType = a.u8(RdmaNldevAttrNameAssignType)
	d.DevType, d.HasDevType = a.u8(RdmaNldevAttrDevType)
	d.ParentName, d.HasParentName = a.str(RdmaNldevAttrParentName)
	return d, nil
}

// RdmaPort is a port, the RDMA_NLDEV_CMD_PORT_GET reply (fill_port_info,
// nldev.c). LID, SM_LID, LMC, SUBNET_PREFIX and CAP_FLAGS are InfiniBand
// only; a RoCE or iWARP port has none of them.
type RdmaPort struct {
	RdmaHandle
	RdmaNetdev
	CapFlags        uint64
	HasCapFlags     bool
	SubnetPrefix    uint64
	HasSubnetPrefix bool
	LID             uint32
	HasLID          bool
	SMLID           uint32
	HasSMLID        bool
	LMC             uint8
	HasLMC          bool
	State           uint8
	HasState        bool
	PhysState       uint8
	HasPhysState    bool
	Unknown         []uint16
}

// ParseRdmaPort decodes a port reply body.
func ParseRdmaPort(body []byte) (RdmaPort, error) {
	var p RdmaPort
	a, err := walkRdmaAttrs(body, &p.Unknown)
	if err != nil {
		return RdmaPort{}, err
	}
	p.RdmaHandle.fill(a)
	p.RdmaNetdev.fill(a)
	p.CapFlags, p.HasCapFlags = a.u64(RdmaNldevAttrCapFlags)
	p.SubnetPrefix, p.HasSubnetPrefix = a.u64(RdmaNldevAttrSubnetPrefix)
	p.LID, p.HasLID = a.u32(RdmaNldevAttrLID)
	p.SMLID, p.HasSMLID = a.u32(RdmaNldevAttrSMLID)
	p.LMC, p.HasLMC = a.u8(RdmaNldevAttrLMC)
	p.State, p.HasState = a.u8(RdmaNldevAttrPortState)
	p.PhysState, p.HasPhysState = a.u8(RdmaNldevAttrPortPhysState)
	return p, nil
}

// RdmaResEntry is one RES_SUMMARY_ENTRY: a resource type and how many the
// device holds.
type RdmaResEntry struct {
	Name    string
	Curr    uint64
	HasCurr bool
}

// RdmaRes is the RDMA_NLDEV_CMD_RES_GET reply, the resource summary
// (fill_res_info, nldev.c). Summary is in wire order, which is the kernel's
// names[] order.
type RdmaRes struct {
	RdmaHandle
	Summary []RdmaResEntry
	Unknown []uint16
}

// ParseRdmaRes decodes a resource summary reply body.
func ParseRdmaRes(body []byte) (RdmaRes, error) {
	var r RdmaRes
	a, err := walkRdmaAttrs(body, &r.Unknown)
	if err != nil {
		return RdmaRes{}, err
	}
	r.fill(a)
	if !a.has[RdmaNldevAttrResSummary] {
		return r, nil
	}
	err = rdmaEntries(a.tb[RdmaNldevAttrResSummary], RdmaNldevAttrResSummaryEntry, &r.Unknown,
		func(e *rdmaAttrs) {
			var re RdmaResEntry
			re.Name, _ = e.str(RdmaNldevAttrResSummaryEntryName)
			re.Curr, re.HasCurr = e.u64(RdmaNldevAttrResSummaryEntryCurr)
			r.Summary = append(r.Summary, re)
		})
	if err != nil {
		return RdmaRes{}, err
	}
	return r, nil
}

// rdmaEntries walks a list nest whose members are entry nests of type
// entry, each parsed as rdma parses it (mnl_attr_parse_nested with
// rd_attr_cb), and calls fn with each entry's tb in wire order. A member of
// any other type is Unknown.
func rdmaEntries(list []byte, entry uint16, unknown *[]uint16, fn func(*rdmaAttrs)) error {
	l, err := walkRdmaList(list, entry, unknown)
	if err != nil {
		return err
	}
	for _, v := range l {
		e, err := walkRdmaAttrs(v, unknown)
		if err != nil {
			return err
		}
		fn(e)
	}
	return nil
}

// walkRdmaList returns the entry members of a list nest. Repeated entries
// are the point of a list, so this is the one place a type is not last-wins.
func walkRdmaList(list []byte, entry uint16, unknown *[]uint16) ([][]byte, error) {
	var out [][]byte
	var verr error
	err := WalkRTAttrs(list, func(atype uint16, val []byte) {
		if verr != nil {
			return
		}
		if atype != entry {
			*unknown = append(*unknown, atype)
			return
		}
		if verr = rdmaValidate(rdmaKindNested, val); verr == nil {
			out = append(out, val)
		}
	})
	if err != nil {
		return nil, err
	}
	return out, verr
}

// RdmaHwCounter is one STAT_HWCOUNTER_ENTRY. Index and Dynamic are sent only
// by STAT_GET_STATUS (stat_get_doit_default_counter does not, nldev.c).
type RdmaHwCounter struct {
	Name       string
	Value      uint64
	HasValue   bool
	Index      uint32
	HasIndex   bool
	Dynamic    uint8
	HasDynamic bool
}

// RdmaStat is the RDMA_NLDEV_CMD_STAT_GET reply for a port's default
// hardware counters (stat_get_doit_default_counter, nldev.c).
type RdmaStat struct {
	RdmaHandle
	HwCounters []RdmaHwCounter
	Unknown    []uint16
}

// ParseRdmaStat decodes a hardware counter reply body.
func ParseRdmaStat(body []byte) (RdmaStat, error) {
	var s RdmaStat
	a, err := walkRdmaAttrs(body, &s.Unknown)
	if err != nil {
		return RdmaStat{}, err
	}
	s.fill(a)
	if !a.has[RdmaNldevAttrStatHwcounters] {
		return s, nil
	}
	err = rdmaEntries(a.tb[RdmaNldevAttrStatHwcounters], RdmaNldevAttrStatHwcounterEntry, &s.Unknown,
		func(e *rdmaAttrs) {
			var c RdmaHwCounter
			c.Name, _ = e.str(RdmaNldevAttrStatHwcounterEntryName)
			c.Value, c.HasValue = e.u64(RdmaNldevAttrStatHwcounterEntryValue)
			c.Index, c.HasIndex = e.u32(RdmaNldevAttrStatHwcounterIndex)
			c.Dynamic, c.HasDynamic = e.u8(RdmaNldevAttrStatHwcounterDynamic)
			s.HwCounters = append(s.HwCounters, c)
		})
	if err != nil {
		return RdmaStat{}, err
	}
	return s, nil
}

// RdmaSys is the RDMA_NLDEV_CMD_SYS_GET reply, the subsystem's modes
// (nldev_sys_get_doit, nldev.c).
type RdmaSys struct {
	NetnsMode             uint8
	HasNetnsMode          bool
	PrivilegedQKeyMode    uint8
	HasPrivilegedQKeyMode bool
	MonitorMode           uint8
	HasMonitorMode        bool
	CopyOnFork            uint8
	HasCopyOnFork         bool
	Unknown               []uint16
}

// ParseRdmaSys decodes a system reply body.
func ParseRdmaSys(body []byte) (RdmaSys, error) {
	var s RdmaSys
	a, err := walkRdmaAttrs(body, &s.Unknown)
	if err != nil {
		return RdmaSys{}, err
	}
	s.NetnsMode, s.HasNetnsMode = a.u8(RdmaNldevSysAttrNetnsMode)
	s.PrivilegedQKeyMode, s.HasPrivilegedQKeyMode = a.u8(RdmaNldevSysAttrPrivilegedQKeyMode)
	s.MonitorMode, s.HasMonitorMode = a.u8(RdmaNldevSysAttrMonitorMode)
	s.CopyOnFork, s.HasCopyOnFork = a.u8(RdmaNldevSysAttrCopyOnFork)
	return s, nil
}

// RdmaEvent is an RDMA_NLDEV_CMD_MONITOR event on RDMA_NL_GROUP_NOTIFY
// (rdma_nl_notify_event, nldev.c). The netdev events carry the port and the
// netdev; REGISTER, UNREGISTER and RENAME carry the device alone.
type RdmaEvent struct {
	RdmaHandle
	RdmaNetdev
	EventType    uint8
	HasEventType bool
	Unknown      []uint16
}

// ParseRdmaEvent decodes a monitor event body.
func ParseRdmaEvent(body []byte) (RdmaEvent, error) {
	var e RdmaEvent
	a, err := walkRdmaAttrs(body, &e.Unknown)
	if err != nil {
		return RdmaEvent{}, err
	}
	e.RdmaHandle.fill(a)
	e.RdmaNetdev.fill(a)
	e.EventType, e.HasEventType = a.u8(RdmaNldevAttrEventType)
	return e, nil
}

// ParseRdmaMsg decodes body by msgType and returns one of RdmaDev, RdmaPort,
// RdmaRes, RdmaStat, RdmaSys, RdmaEvent, or for the resource object GETs
// RdmaResQPs, RdmaResCMIDs, RdmaResCQs, RdmaResMRs, RdmaResPDs, RdmaResCtxs
// or RdmaResSRQs.
//
// A GET-typed message carrying PORT_STATE is a port: the PORT_GET doit
// answers with RDMA_NLDEV_CMD_GET (nldev_port_get_doit, nldev.c), and only
// the PORT_GET dump answers with PORT_GET. A device reply never carries
// PORT_STATE, so the attribute tells them apart.
func ParseRdmaMsg(msgType uint16, body []byte) (any, error) {
	if RdmaNlGetClient(msgType) != RdmaNlNldev {
		return nil, ErrRdmaMsgType
	}
	switch RdmaNlGetOp(msgType) {
	case RdmaNldevCmdGet:
		if rdmaHasAttr(body, RdmaNldevAttrPortState) {
			return ParseRdmaPort(body)
		}
		return ParseRdmaDev(body)
	case RdmaNldevCmdPortGet:
		return ParseRdmaPort(body)
	case RdmaNldevCmdResGet:
		return ParseRdmaRes(body)
	case RdmaNldevCmdResQPGet:
		return ParseRdmaResQP(body)
	case RdmaNldevCmdResCMIDGet:
		return ParseRdmaResCMID(body)
	case RdmaNldevCmdResCQGet:
		return ParseRdmaResCQ(body)
	case RdmaNldevCmdResMRGet:
		return ParseRdmaResMR(body)
	case RdmaNldevCmdResPDGet:
		return ParseRdmaResPD(body)
	case RdmaNldevCmdResCtxGet:
		return ParseRdmaResCtx(body)
	case RdmaNldevCmdResSRQGet:
		return ParseRdmaResSRQ(body)
	case RdmaNldevCmdStatGet:
		return ParseRdmaStat(body)
	case RdmaNldevCmdSysGet:
		return ParseRdmaSys(body)
	case RdmaNldevCmdMonitor:
		return ParseRdmaEvent(body)
	}
	return nil, ErrRdmaMsgType
}

// rdmaHasAttr reports whether body's top level holds an atype. A malformed
// body answers false and fails in the parser it is then handed to.
func rdmaHasAttr(body []byte, atype uint16) bool {
	found := false
	if err := WalkRTAttrs(body, func(t uint16, _ []byte) {
		found = found || t == atype
	}); err != nil {
		return false
	}
	return found
}
