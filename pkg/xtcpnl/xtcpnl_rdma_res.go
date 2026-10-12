package xtcpnl

import (
	"encoding/binary"
	"net/netip"
)

// RDMA nldev resource objects: the RES_QP_GET, RES_CM_ID_GET, RES_CQ_GET,
// RES_MR_GET, RES_PD_GET, RES_CTX_GET and RES_SRQ_GET replies, which `rdma
// res show <object>` prints (rdma/res-*.c).
//
// # Dump and doit replies
//
// A dump reply (res_get_common_dumpit, nldev.c) carries the device handle
// and then one table nest, RES_QP for instance, holding an entry nest,
// RES_QP_ENTRY, per object. A device with no objects sends no message at
// all, only NLMSG_DONE; a device whose objects are all on another port
// sends the handle and an empty table. A doit reply, the answer to an id
// filter such as `lqpn 1` (res_get_common_doit), is flat: the one object's
// attributes follow the handle at top level, with no table and no entry
// nest. RdmaResObjects tells the two apart with Flat.
//
// # Who owns an object
//
// fill_res_name_pid sends KERN_NAME for an object the kernel owns, the MAD
// layer's GSI QP, CQ and PD for instance, and PID for one a process owns.
// A user object whose process has left this pid namespace carries neither.

// rdmaSockaddrLenCst is the width of SRC_ADDR and DST_ADDR: the kernel puts
// a whole struct __kernel_sockaddr_storage (fill_res_cm_id_entry, nldev.c).
// rdma's policy has them as MNL_TYPE_UNSPEC and reads whatever arrives;
// this decoder holds them to the one width the kernel sends.
const rdmaSockaddrLenCst = 128

// The address families parseRdmaSockaddr decodes, and the offsets of
// struct sockaddr_in and struct sockaddr_in6 inside a sockaddr_storage.
const (
	rdmaAfInetCst      = 2
	rdmaAfInet6Cst     = 10
	rdmaSinPortOffCst  = 2
	rdmaSinAddrOffCst  = 4
	rdmaSin6AddrOffCst = 8
)

// RdmaResOwner is who holds a resource object: KERN_NAME for a kernel
// object, PID for a user one.
type RdmaResOwner struct {
	KernName    string
	HasKernName bool
	PID         uint32
	HasPID      bool
}

func (o *RdmaResOwner) fill(a *rdmaAttrs) {
	o.KernName, o.HasKernName = a.str(RdmaNldevAttrResKernName)
	o.PID, o.HasPID = a.u32(RdmaNldevAttrResPID)
}

// RdmaSockaddr is a CM ID's SRC_ADDR or DST_ADDR. Family is sa_family, in
// host byte order; Port and Addr are decoded for AF_INET and AF_INET6 only,
// the port from network byte order. An AF_INET6 address is kept as sent,
// so a v4-mapped one stays v4-mapped, as rdma's inet_ntop prints it.
type RdmaSockaddr struct {
	Family uint16
	Port   uint16
	Addr   netip.Addr
}

// parseRdmaSockaddr decodes a sockaddr_storage. The policy has already
// checked b is rdmaSockaddrLenCst bytes; anything shorter is the zero value.
func parseRdmaSockaddr(b []byte) RdmaSockaddr {
	if len(b) < rdmaSockaddrLenCst {
		return RdmaSockaddr{}
	}
	s := RdmaSockaddr{Family: binary.LittleEndian.Uint16(b[0:2])}
	switch s.Family {
	case rdmaAfInetCst:
		s.Port = binary.BigEndian.Uint16(b[rdmaSinPortOffCst:])
		s.Addr = netip.AddrFrom4([4]byte(b[rdmaSinAddrOffCst : rdmaSinAddrOffCst+4]))
	case rdmaAfInet6Cst:
		s.Port = binary.BigEndian.Uint16(b[rdmaSinPortOffCst:])
		s.Addr = netip.AddrFrom16([16]byte(b[rdmaSin6AddrOffCst : rdmaSin6AddrOffCst+16]))
	}
	return s
}

func (a *rdmaAttrs) sockaddr(t uint16) (RdmaSockaddr, bool) {
	if !a.has[t] {
		return RdmaSockaddr{}, false
	}
	return parseRdmaSockaddr(a.tb[t]), true
}

// RdmaResQP is a queue pair (fill_res_qp_entry, nldev.c). RQPN and RQ_PSN
// are sent for RC and UC QPs only, PATH_MIG_STATE for RC, UC and XRC, and
// PDN for user QPs only. PortIndex is absent for a QP not yet bound to a
// port.
type RdmaResQP struct {
	PortIndex       uint32
	HasPortIndex    bool
	LQPN            uint32
	HasLQPN         bool
	PDN             uint32
	HasPDN          bool
	RQPN            uint32
	HasRQPN         bool
	RQPSN           uint32
	HasRQPSN        bool
	SQPSN           uint32
	HasSQPSN        bool
	PathMigState    uint8
	HasPathMigState bool
	Type            uint8
	HasType         bool
	State           uint8
	HasState        bool
	Subtype         string
	HasSubtype      bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResQP(a *rdmaAttrs, unknown []uint16) (RdmaResQP, error) {
	q := RdmaResQP{Unknown: unknown}
	q.PortIndex, q.HasPortIndex = a.u32(RdmaNldevAttrPortIndex)
	q.LQPN, q.HasLQPN = a.u32(RdmaNldevAttrResLQPN)
	q.PDN, q.HasPDN = a.u32(RdmaNldevAttrResPDN)
	q.RQPN, q.HasRQPN = a.u32(RdmaNldevAttrResRQPN)
	q.RQPSN, q.HasRQPSN = a.u32(RdmaNldevAttrResRqPSN)
	q.SQPSN, q.HasSQPSN = a.u32(RdmaNldevAttrResSqPSN)
	q.PathMigState, q.HasPathMigState = a.u8(RdmaNldevAttrResPathMigState)
	q.Type, q.HasType = a.u8(RdmaNldevAttrResType)
	q.State, q.HasState = a.u8(RdmaNldevAttrResState)
	q.Subtype, q.HasSubtype = a.str(RdmaNldevAttrResSubtype)
	q.fill(a)
	return q, nil
}

// RdmaResCMID is an RDMA CM ID (fill_res_cm_id_entry, nldev.c). LQPN and
// Type are sent only once the ID has a QP; Src and Dst only when their
// family is set.
type RdmaResCMID struct {
	PortIndex    uint32
	HasPortIndex bool
	LQPN         uint32
	HasLQPN      bool
	Type         uint8
	HasType      bool
	PS           uint32
	HasPS        bool
	State        uint8
	HasState     bool
	Src          RdmaSockaddr
	HasSrc       bool
	Dst          RdmaSockaddr
	HasDst       bool
	CMIDN        uint32
	HasCMIDN     bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResCMID(a *rdmaAttrs, unknown []uint16) (RdmaResCMID, error) {
	c := RdmaResCMID{Unknown: unknown}
	c.PortIndex, c.HasPortIndex = a.u32(RdmaNldevAttrPortIndex)
	c.LQPN, c.HasLQPN = a.u32(RdmaNldevAttrResLQPN)
	c.Type, c.HasType = a.u8(RdmaNldevAttrResType)
	c.PS, c.HasPS = a.u32(RdmaNldevAttrResPS)
	c.State, c.HasState = a.u8(RdmaNldevAttrResState)
	c.Src, c.HasSrc = a.sockaddr(RdmaNldevAttrResSrcAddr)
	c.Dst, c.HasDst = a.sockaddr(RdmaNldevAttrResDstAddr)
	c.CMIDN, c.HasCMIDN = a.u32(RdmaNldevAttrResCMIDN)
	c.fill(a)
	return c, nil
}

// RdmaResCQ is a completion queue (fill_res_cq_entry, nldev.c). PollCtx is
// sent for kernel CQs only, CTXN for user ones only.
type RdmaResCQ struct {
	CQE        uint32
	HasCQE     bool
	UseCnt     uint64
	HasUseCnt  bool
	PollCtx    uint8
	HasPollCtx bool
	DIM        uint8
	HasDIM     bool
	CQN        uint32
	HasCQN     bool
	CTXN       uint32
	HasCTXN    bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResCQ(a *rdmaAttrs, unknown []uint16) (RdmaResCQ, error) {
	c := RdmaResCQ{Unknown: unknown}
	c.CQE, c.HasCQE = a.u32(RdmaNldevAttrResCQE)
	c.UseCnt, c.HasUseCnt = a.u64(RdmaNldevAttrResUsecnt)
	c.PollCtx, c.HasPollCtx = a.u8(RdmaNldevAttrResPollCtx)
	c.DIM, c.HasDIM = a.u8(RdmaNldevAttrDevDIM)
	c.CQN, c.HasCQN = a.u32(RdmaNldevAttrResCQN)
	c.CTXN, c.HasCTXN = a.u32(RdmaNldevAttrResCtxn)
	c.fill(a)
	return c, nil
}

// RdmaResMR is a memory region (fill_res_mr_entry, nldev.c). RKey and LKey
// are sent only to a CAP_NET_ADMIN reader, PDN for user MRs only.
type RdmaResMR struct {
	RKey     uint32
	HasRKey  bool
	LKey     uint32
	HasLKey  bool
	IOVA     uint64
	HasIOVA  bool
	MRLen    uint64
	HasMRLen bool
	MRN      uint32
	HasMRN   bool
	PDN      uint32
	HasPDN   bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResMR(a *rdmaAttrs, unknown []uint16) (RdmaResMR, error) {
	m := RdmaResMR{Unknown: unknown}
	m.RKey, m.HasRKey = a.u32(RdmaNldevAttrResRKey)
	m.LKey, m.HasLKey = a.u32(RdmaNldevAttrResLKey)
	m.IOVA, m.HasIOVA = a.u64(RdmaNldevAttrResIOVA)
	m.MRLen, m.HasMRLen = a.u64(RdmaNldevAttrResMrlen)
	m.MRN, m.HasMRN = a.u32(RdmaNldevAttrResMRN)
	m.PDN, m.HasPDN = a.u32(RdmaNldevAttrResPDN)
	m.fill(a)
	return m, nil
}

// RdmaResPD is a protection domain (fill_res_pd_entry, nldev.c).
// LocalDMALKey, and UnsafeGlobalRKey when the PD has one, are sent only to
// a CAP_NET_ADMIN reader; CTXN for user PDs only.
type RdmaResPD struct {
	LocalDMALKey        uint32
	HasLocalDMALKey     bool
	UnsafeGlobalRKey    uint32
	HasUnsafeGlobalRKey bool
	UseCnt              uint64
	HasUseCnt           bool
	PDN                 uint32
	HasPDN              bool
	CTXN                uint32
	HasCTXN             bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResPD(a *rdmaAttrs, unknown []uint16) (RdmaResPD, error) {
	p := RdmaResPD{Unknown: unknown}
	p.LocalDMALKey, p.HasLocalDMALKey = a.u32(RdmaNldevAttrResLocalDMALKey)
	p.UnsafeGlobalRKey, p.HasUnsafeGlobalRKey = a.u32(RdmaNldevAttrResUnsafeGlobalRKey)
	p.UseCnt, p.HasUseCnt = a.u64(RdmaNldevAttrResUsecnt)
	p.PDN, p.HasPDN = a.u32(RdmaNldevAttrResPDN)
	p.CTXN, p.HasCTXN = a.u32(RdmaNldevAttrResCtxn)
	p.fill(a)
	return p, nil
}

// RdmaResCtx is a user verbs context (fill_res_ctx_entry, nldev.c); the
// kernel lists no kernel-owned ones.
type RdmaResCtx struct {
	CTXN    uint32
	HasCTXN bool
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResCtx(a *rdmaAttrs, unknown []uint16) (RdmaResCtx, error) {
	c := RdmaResCtx{Unknown: unknown}
	c.CTXN, c.HasCTXN = a.u32(RdmaNldevAttrResCtxn)
	c.fill(a)
	return c, nil
}

// RdmaQPRange is a run of QP numbers attached to an SRQ, Min to Max
// inclusive. The kernel sends a run of one as a lone LQPN, which is Min ==
// Max here (fill_res_range_qp_entry, nldev.c).
type RdmaQPRange struct {
	Min uint32
	Max uint32
}

// RdmaResSRQ is a shared receive queue (fill_res_srq_entry, nldev.c). CQN
// is sent only for the SRQ types that have a CQ; QPs is the inner RES_QP
// list, in wire order, which is ascending.
type RdmaResSRQ struct {
	SRQN    uint32
	HasSRQN bool
	Type    uint8
	HasType bool
	PDN     uint32
	HasPDN  bool
	CQN     uint32
	HasCQN  bool
	QPs     []RdmaQPRange
	RdmaResOwner
	Unknown []uint16
}

func fillRdmaResSRQ(a *rdmaAttrs, unknown []uint16) (RdmaResSRQ, error) {
	s := RdmaResSRQ{Unknown: unknown}
	s.SRQN, s.HasSRQN = a.u32(RdmaNldevAttrResSRQN)
	s.Type, s.HasType = a.u8(RdmaNldevAttrResType)
	s.PDN, s.HasPDN = a.u32(RdmaNldevAttrResPDN)
	s.CQN, s.HasCQN = a.u32(RdmaNldevAttrResCQN)
	s.fill(a)
	if !a.has[RdmaNldevAttrResQP] {
		return s, nil
	}
	err := rdmaEntries(a.tb[RdmaNldevAttrResQP], RdmaNldevAttrResQPEntry, &s.Unknown,
		func(e *rdmaAttrs) {
			if n, ok := e.u32(RdmaNldevAttrResLQPN); ok {
				s.QPs = append(s.QPs, RdmaQPRange{Min: n, Max: n})
				return
			}
			lo, _ := e.u32(RdmaNldevAttrMinRange)
			hi, _ := e.u32(RdmaNldevAttrMaxRange)
			s.QPs = append(s.QPs, RdmaQPRange{Min: lo, Max: hi})
		})
	if err != nil {
		return RdmaResSRQ{}, err
	}
	return s, nil
}

// RdmaResObjects is one resource object reply: the device handle and the
// objects it lists. Flat is a doit reply, whose one object is the top level
// itself; an empty Flat body lists no object. Unknown is the top level's
// and the table's; each entry has its own.
type RdmaResObjects[T any] struct {
	RdmaHandle
	Entries []T
	Flat    bool
	Unknown []uint16
}

// RdmaResQPs and the rest name the instantiations ParseRdmaMsg returns.
type (
	RdmaResQPs   = RdmaResObjects[RdmaResQP]
	RdmaResCMIDs = RdmaResObjects[RdmaResCMID]
	RdmaResCQs   = RdmaResObjects[RdmaResCQ]
	RdmaResMRs   = RdmaResObjects[RdmaResMR]
	RdmaResPDs   = RdmaResObjects[RdmaResPD]
	RdmaResCtxs  = RdmaResObjects[RdmaResCtx]
	RdmaResSRQs  = RdmaResObjects[RdmaResSRQ]
)

// parseRdmaResObjects decodes a resource object reply whose table nest is
// table and whose entries are entry, filling each object with fill.
func parseRdmaResObjects[T any](body []byte, table, entry uint16,
	fill func(*rdmaAttrs, []uint16) (T, error)) (RdmaResObjects[T], error) {
	var r RdmaResObjects[T]
	a, err := walkRdmaAttrs(body, &r.Unknown)
	if err != nil {
		return RdmaResObjects[T]{}, err
	}
	r.fill(a)
	if !a.has[table] {
		r.Flat = true
		if len(body) == 0 {
			return r, nil
		}
		o, err := fill(a, nil)
		if err != nil {
			return RdmaResObjects[T]{}, err
		}
		r.Entries = append(r.Entries, o)
		return r, nil
	}
	l, err := walkRdmaList(a.tb[table], entry, &r.Unknown)
	if err != nil {
		return RdmaResObjects[T]{}, err
	}
	r.Entries = make([]T, 0, len(l))
	for _, v := range l {
		var unknown []uint16
		e, err := walkRdmaAttrs(v, &unknown)
		if err != nil {
			return RdmaResObjects[T]{}, err
		}
		o, err := fill(e, unknown)
		if err != nil {
			return RdmaResObjects[T]{}, err
		}
		r.Entries = append(r.Entries, o)
	}
	return r, nil
}

// ParseRdmaResQP decodes a RES_QP_GET reply body.
func ParseRdmaResQP(body []byte) (RdmaResQPs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResQP, RdmaNldevAttrResQPEntry, fillRdmaResQP)
}

// ParseRdmaResCMID decodes a RES_CM_ID_GET reply body.
func ParseRdmaResCMID(body []byte) (RdmaResCMIDs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResCMID, RdmaNldevAttrResCMIDEntry, fillRdmaResCMID)
}

// ParseRdmaResCQ decodes a RES_CQ_GET reply body.
func ParseRdmaResCQ(body []byte) (RdmaResCQs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResCQ, RdmaNldevAttrResCQEntry, fillRdmaResCQ)
}

// ParseRdmaResMR decodes a RES_MR_GET reply body.
func ParseRdmaResMR(body []byte) (RdmaResMRs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResMR, RdmaNldevAttrResMREntry, fillRdmaResMR)
}

// ParseRdmaResPD decodes a RES_PD_GET reply body.
func ParseRdmaResPD(body []byte) (RdmaResPDs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResPD, RdmaNldevAttrResPDEntry, fillRdmaResPD)
}

// ParseRdmaResCtx decodes a RES_CTX_GET reply body.
func ParseRdmaResCtx(body []byte) (RdmaResCtxs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResCtx, RdmaNldevAttrResCtxEntry, fillRdmaResCtx)
}

// ParseRdmaResSRQ decodes a RES_SRQ_GET reply body.
func ParseRdmaResSRQ(body []byte) (RdmaResSRQs, error) {
	return parseRdmaResObjects(body, RdmaNldevAttrResSRQ, RdmaNldevAttrResSRQEntry, fillRdmaResSRQ)
}
