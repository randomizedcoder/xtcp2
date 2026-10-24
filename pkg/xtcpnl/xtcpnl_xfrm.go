package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"
)

// This file holds what the NETLINK_XFRM parsers share: the message types,
// the XFRMA_* attributes and the structs more than one message carries
// (include/uapi/linux/xfrm.h). x/sys/unix defines none of them. Every size
// and offset is x86_64's; a 32-bit kernel lays several of the structs out
// differently, and the kernel's compat layer (net/xfrm/xfrm_compat.c)
// translates for a 32-bit ip on a 64-bit kernel, so the wire is always the
// kernel's own layout.

// XFRM_MSG_* message types.
const (
	XfrmMsgNewSA        uint16 = 16
	XfrmMsgDelSA        uint16 = 17
	XfrmMsgGetSA        uint16 = 18
	XfrmMsgNewPolicy    uint16 = 19
	XfrmMsgDelPolicy    uint16 = 20
	XfrmMsgGetPolicy    uint16 = 21
	XfrmMsgAllocSPI     uint16 = 22
	XfrmMsgAcquire      uint16 = 23
	XfrmMsgExpire       uint16 = 24
	XfrmMsgUpdPolicy    uint16 = 25
	XfrmMsgUpdSA        uint16 = 26
	XfrmMsgPolExpire    uint16 = 27
	XfrmMsgFlushSA      uint16 = 28
	XfrmMsgFlushPolicy  uint16 = 29
	XfrmMsgNewAE        uint16 = 30
	XfrmMsgGetAE        uint16 = 31
	XfrmMsgReport       uint16 = 32
	XfrmMsgMigrate      uint16 = 33
	XfrmMsgNewSADInfo   uint16 = 34
	XfrmMsgGetSADInfo   uint16 = 35
	XfrmMsgNewSPDInfo   uint16 = 36
	XfrmMsgGetSPDInfo   uint16 = 37
	XfrmMsgMapping      uint16 = 38
	XfrmMsgSetDefault   uint16 = 39
	XfrmMsgGetDefault   uint16 = 40
	XfrmMsgMigrateState uint16 = 41
)

// XFRMA_* attributes (enum xfrm_attr_type_t).
const (
	XfrmAttrAlgAuth              uint16 = 1  // struct xfrm_algo
	XfrmAttrAlgCrypt             uint16 = 2  // struct xfrm_algo
	XfrmAttrAlgComp              uint16 = 3  // struct xfrm_algo
	XfrmAttrEncap                uint16 = 4  // struct xfrm_encap_tmpl
	XfrmAttrTmpl                 uint16 = 5  // struct xfrm_user_tmpl, one or more
	XfrmAttrSA                   uint16 = 6  // struct xfrm_usersa_info
	XfrmAttrPolicy               uint16 = 7  // struct xfrm_userpolicy_info
	XfrmAttrSecCtx               uint16 = 8  // struct xfrm_user_sec_ctx and the context
	XfrmAttrLtimeVal             uint16 = 9  // struct xfrm_lifetime_cur
	XfrmAttrReplayVal            uint16 = 10 // struct xfrm_replay_state
	XfrmAttrReplayThresh         uint16 = 11 // u32
	XfrmAttrEtimerThresh         uint16 = 12 // u32
	XfrmAttrSrcAddr              uint16 = 13 // xfrm_address_t
	XfrmAttrCoAddr               uint16 = 14 // xfrm_address_t
	XfrmAttrLastUsed             uint16 = 15 // u64
	XfrmAttrPolicyType           uint16 = 16 // struct xfrm_userpolicy_type
	XfrmAttrMigrate              uint16 = 17
	XfrmAttrAlgAEAD              uint16 = 18 // struct xfrm_algo_aead
	XfrmAttrKMAddress            uint16 = 19
	XfrmAttrAlgAuthTrunc         uint16 = 20 // struct xfrm_algo_auth
	XfrmAttrMark                 uint16 = 21 // struct xfrm_mark
	XfrmAttrTFCPad               uint16 = 22 // u32
	XfrmAttrReplayESNVal         uint16 = 23 // struct xfrm_replay_state_esn
	XfrmAttrSAExtraFlags         uint16 = 24 // u32
	XfrmAttrProto                uint16 = 25 // u8
	XfrmAttrAddressFilter        uint16 = 26 // struct xfrm_address_filter
	XfrmAttrPad                  uint16 = 27
	XfrmAttrOffloadDev           uint16 = 28 // struct xfrm_user_offload
	XfrmAttrSetMark              uint16 = 29 // u32
	XfrmAttrSetMarkMask          uint16 = 30 // u32
	XfrmAttrIfID                 uint16 = 31 // u32
	XfrmAttrMtimerThresh         uint16 = 32 // u32
	XfrmAttrSADir                uint16 = 33 // u8
	XfrmAttrNATKeepaliveInterval uint16 = 34 // u32
	XfrmAttrSAPcpu               uint16 = 35 // u32
	XfrmAttrIPTFSDropTime        uint16 = 36 // u32
	XfrmAttrIPTFSReorderWindow   uint16 = 37 // u16
	XfrmAttrIPTFSDontFrag        uint16 = 38 // flag
	XfrmAttrIPTFSInitDelay       uint16 = 39 // u32
	XfrmAttrIPTFSMaxQSize        uint16 = 40 // u32
	XfrmAttrIPTFSPktSize         uint16 = 41 // u32
)

// Sizes of the XFRM structs on x86_64.
const (
	XfrmAddressSizeCst       = 16  // xfrm_address_t
	XfrmSelectorSizeCst      = 56  // struct xfrm_selector
	XfrmLifetimeCfgSizeCst   = 64  // struct xfrm_lifetime_cfg
	XfrmLifetimeCurSizeCst   = 32  // struct xfrm_lifetime_cur
	XfrmAlgoSizeCst          = 68  // struct xfrm_algo, before its key
	XfrmAlgoICVSizeCst       = 72  // struct xfrm_algo_aead and xfrm_algo_auth, before their key
	XfrmEncapTmplSizeCst     = 24  // struct xfrm_encap_tmpl
	XfrmReplayStateSizeCst   = 12  // struct xfrm_replay_state
	XfrmReplayESNSizeCst     = 24  // struct xfrm_replay_state_esn, before its bitmap
	XfrmMarkSizeCst          = 8   // struct xfrm_mark
	XfrmAddressFilterSizeCst = 36  // struct xfrm_address_filter
	XfrmUserOffloadSizeCst   = 8   // struct xfrm_user_offload
	XfrmUserSecCtxSizeCst    = 8   // struct xfrm_user_sec_ctx, before its context
	XfrmUsersaInfoSizeCst    = 224 // struct xfrm_usersa_info
	XfrmUsersaIDSizeCst      = 24  // struct xfrm_usersa_id
	XfrmUsersaFlushSizeCst   = 1   // struct xfrm_usersa_flush
	XfrmAeventIDSizeCst      = 48  // struct xfrm_aevent_id
	XfrmUserspiInfoSizeCst   = 232 // struct xfrm_userspi_info

	xfrmAlgoNameLen = 64
)

var (
	// ErrXfrmSmall is an XFRM body shorter than its type's struct.
	ErrXfrmSmall = errors.New("xtcpnl: xfrm body too small for its type")
	// ErrXfrmType is a message type the XFRM parser was not written for.
	ErrXfrmType = errors.New("xtcpnl: xfrm message type not handled")
	// ErrXfrmAttr is an XFRMA_* attribute too short for its struct, or a
	// scalar not exactly its width.
	ErrXfrmAttr = errors.New("xtcpnl: malformed xfrm attribute")
)

// XfrmAddress is an xfrm_address_t: 16 bytes holding an IPv4 address in
// its first 4 or an IPv6 address, in network order. Which one is the
// family's, which the struct carrying it names.
type XfrmAddress [XfrmAddressSizeCst]byte

// Addr is the address as family has it: the first 4 bytes for AF_INET, all
// 16 for AF_INET6, and the zero netip.Addr for any other family.
func (a XfrmAddress) Addr(family uint16) netip.Addr {
	switch family {
	case unix.AF_INET:
		return netip.AddrFrom4([4]byte(a[:4]))
	case unix.AF_INET6:
		return netip.AddrFrom16(a)
	}
	return netip.Addr{}
}

func xfrmAddress(b []byte) XfrmAddress {
	if len(b) < XfrmAddressSizeCst {
		return XfrmAddress{}
	}
	return XfrmAddress(b[:XfrmAddressSizeCst])
}

// XfrmSelector is a struct xfrm_selector: the traffic an SA or a policy
// applies to. Ports and masks are in host order here, converted from the
// network order they are sent in.
type XfrmSelector struct {
	Daddr, Saddr     XfrmAddress
	Dport, DportMask uint16
	Sport, SportMask uint16
	Family           uint16
	PrefixlenD       uint8
	PrefixlenS       uint8
	Proto            uint8
	Ifindex          int32
	User             uint32 // the kernel's uid_t
}

func xfrmSelector(b []byte) XfrmSelector {
	if len(b) < XfrmSelectorSizeCst {
		return XfrmSelector{}
	}
	return XfrmSelector{
		Daddr:      xfrmAddress(b[0:]),
		Saddr:      xfrmAddress(b[16:]),
		Dport:      binary.BigEndian.Uint16(b[32:]),
		DportMask:  binary.BigEndian.Uint16(b[34:]),
		Sport:      binary.BigEndian.Uint16(b[36:]),
		SportMask:  binary.BigEndian.Uint16(b[38:]),
		Family:     binary.LittleEndian.Uint16(b[40:]),
		PrefixlenD: b[42],
		PrefixlenS: b[43],
		Proto:      b[44],
		Ifindex:    int32(binary.LittleEndian.Uint32(b[48:])),
		User:       binary.LittleEndian.Uint32(b[52:]),
	}
}

// XfrmLifetimeCfg is a struct xfrm_lifetime_cfg: soft and hard limits on
// bytes, packets, seconds since add and seconds since first use. The
// kernel's infinity is ^uint64(0).
type XfrmLifetimeCfg struct {
	SoftByteLimit, HardByteLimit                 uint64
	SoftPacketLimit, HardPacketLimit             uint64
	SoftAddExpiresSeconds, HardAddExpiresSeconds uint64
	SoftUseExpiresSeconds, HardUseExpiresSeconds uint64
}

func xfrmLifetimeCfg(b []byte) XfrmLifetimeCfg {
	if len(b) < XfrmLifetimeCfgSizeCst {
		return XfrmLifetimeCfg{}
	}
	u := func(i int) uint64 { return binary.LittleEndian.Uint64(b[8*i:]) }
	return XfrmLifetimeCfg{u(0), u(1), u(2), u(3), u(4), u(5), u(6), u(7)}
}

// XfrmLifetimeCur is a struct xfrm_lifetime_cur: the bytes and packets an
// SA has carried and when it was added and first used, in seconds since
// the epoch.
type XfrmLifetimeCur struct {
	Bytes, Packets   uint64
	AddTime, UseTime uint64
}

func xfrmLifetimeCur(b []byte) XfrmLifetimeCur {
	if len(b) < XfrmLifetimeCurSizeCst {
		return XfrmLifetimeCur{}
	}
	u := func(i int) uint64 { return binary.LittleEndian.Uint64(b[8*i:]) }
	return XfrmLifetimeCur{u(0), u(1), u(2), u(3)}
}

// XfrmAlgo is an algorithm attribute: XFRMA_ALG_AUTH, _CRYPT and _COMP
// (struct xfrm_algo), XFRMA_ALG_AEAD (struct xfrm_algo_aead) and
// XFRMA_ALG_AUTH_TRUNC (struct xfrm_algo_auth). The last two carry a third
// word, an AEAD's ICV length or a hash's truncation length in bits, in
// ICVTruncBits; it is 0 for the first three.
type XfrmAlgo struct {
	Name         string
	KeyBits      uint32
	ICVTruncBits uint32
	Key          []byte
}

// xfrmAlgo decodes an algorithm attribute whose key starts at hdr, which
// must hold (KeyBits+7)/8 bytes of key, as the kernel's verify_one_alg
// requires. Bytes after the key are left out.
func xfrmAlgo(id uint16, d []byte, hdr int) (*XfrmAlgo, error) {
	if len(d) < hdr {
		return nil, fmt.Errorf("%w: %d is %d bytes, under %d", ErrXfrmAttr, id, len(d), hdr)
	}
	name := d[:xfrmAlgoNameLen]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	a := &XfrmAlgo{Name: string(name), KeyBits: binary.LittleEndian.Uint32(d[xfrmAlgoNameLen:])}
	if hdr == XfrmAlgoICVSizeCst {
		a.ICVTruncBits = binary.LittleEndian.Uint32(d[xfrmAlgoNameLen+4:])
	}
	n := (int(a.KeyBits) + 7) / 8
	if n > len(d)-hdr {
		return nil, fmt.Errorf("%w: %d holds %d key bytes of %d", ErrXfrmAttr, id, len(d)-hdr, n)
	}
	a.Key = CopyBytes(d[hdr : hdr+n])
	return a, nil
}

// XfrmEncapTmpl is a struct xfrm_encap_tmpl: an SA's UDP encapsulation,
// its ports in host order, and the original address NAT-T saw.
type XfrmEncapTmpl struct {
	Type         uint16 // UDP_ENCAP_ESPINUDP (2) and the like
	Sport, Dport uint16
	OA           XfrmAddress
}

// XfrmReplayState is a struct xfrm_replay_state: the outbound and inbound
// sequence numbers and the replay bitmap.
type XfrmReplayState struct {
	OSeq, Seq, Bitmap uint32
}

// XfrmReplayStateESN is a struct xfrm_replay_state_esn: the extended
// sequence number state, then its bitmap of bmp_len u32s.
type XfrmReplayStateESN struct {
	OSeq, Seq, OSeqHi, SeqHi uint32
	ReplayWindow             uint32
	Bmp                      []uint32
}

// XfrmMark is a struct xfrm_mark: a value and its mask.
type XfrmMark struct {
	V, M uint32
}

// XfrmAddressFilter is a struct xfrm_address_filter, which a GETSA dump
// sends to have the kernel return only the SAs whose addresses match.
type XfrmAddressFilter struct {
	Saddr, Daddr XfrmAddress
	Family       uint16
	SPLen, DPLen uint8
}

// XfrmUserOffload is a struct xfrm_user_offload: the device an SA is
// offloaded to and the XFRM_OFFLOAD_* flags.
type XfrmUserOffload struct {
	Ifindex int32
	Flags   uint8
}

// XfrmSecCtx is a struct xfrm_user_sec_ctx and the security context after
// it.
type XfrmSecCtx struct {
	ExtType uint16
	Alg     uint8
	DOI     uint8
	Ctx     []byte
}

// XfrmAttrs is the XFRMA_* attributes an SA or policy message carries,
// each nil or false when absent. A repeated attribute keeps the last, as the kernel's
// nla_parse does. An attribute this does not know is skipped.
type XfrmAttrs struct {
	AlgAuth      *XfrmAlgo
	AlgCrypt     *XfrmAlgo
	AlgComp      *XfrmAlgo
	AlgAEAD      *XfrmAlgo
	AlgAuthTrunc *XfrmAlgo
	Encap        *XfrmEncapTmpl
	SA           *XfrmUsersaInfo // the SA a DELSA event nests
	Tmpl         []XfrmUserTmpl
	Policy       *XfrmUserpolicyInfo // the policy a DELPOLICY event nests
	PolicyType   *uint8              // XFRM_POLICY_TYPE_MAIN (0) or _SUB (1)
	SecCtx       *XfrmSecCtx
	LtimeVal     *XfrmLifetimeCur
	ReplayVal    *XfrmReplayState
	ReplayESNVal *XfrmReplayStateESN
	SrcAddr      *XfrmAddress
	CoAddr       *XfrmAddress
	LastUsed     *uint64
	Mark         *XfrmMark
	Proto        *uint8
	SADir        *uint8
	Filter       *XfrmAddressFilter
	OffloadDev   *XfrmUserOffload

	IPTFSReorderWindow *uint16
	IPTFSDontFrag      bool

	ReplayThresh         *uint32
	EtimerThresh         *uint32
	TFCPad               *uint32
	ExtraFlags           *uint32
	SetMark              *uint32
	SetMarkMask          *uint32
	IfID                 *uint32
	MtimerThresh         *uint32
	NATKeepaliveInterval *uint32
	SAPcpu               *uint32
	IPTFSDropTime        *uint32
	IPTFSInitDelay       *uint32
	IPTFSMaxQSize        *uint32
	IPTFSPktSize         *uint32
}

// u32Field is where the u32 attribute id goes, nil if id is not one.
func (a *XfrmAttrs) u32Field(id uint16) **uint32 {
	switch id {
	case XfrmAttrReplayThresh:
		return &a.ReplayThresh
	case XfrmAttrEtimerThresh:
		return &a.EtimerThresh
	case XfrmAttrTFCPad:
		return &a.TFCPad
	case XfrmAttrSAExtraFlags:
		return &a.ExtraFlags
	case XfrmAttrSetMark:
		return &a.SetMark
	case XfrmAttrSetMarkMask:
		return &a.SetMarkMask
	case XfrmAttrIfID:
		return &a.IfID
	case XfrmAttrMtimerThresh:
		return &a.MtimerThresh
	case XfrmAttrNATKeepaliveInterval:
		return &a.NATKeepaliveInterval
	case XfrmAttrSAPcpu:
		return &a.SAPcpu
	case XfrmAttrIPTFSDropTime:
		return &a.IPTFSDropTime
	case XfrmAttrIPTFSInitDelay:
		return &a.IPTFSInitDelay
	case XfrmAttrIPTFSMaxQSize:
		return &a.IPTFSMaxQSize
	case XfrmAttrIPTFSPktSize:
		return &a.IPTFSPktSize
	}
	return nil
}

// xfrmAttrMin is the least length of each struct attribute, as the
// kernel's xfrma_policy has it; the u8, u16, u32, u64 and flag
// attributes must be exactly their width.
var xfrmAttrMin = map[uint16]int{
	XfrmAttrAlgAuth: XfrmAlgoSizeCst, XfrmAttrAlgCrypt: XfrmAlgoSizeCst, XfrmAttrAlgComp: XfrmAlgoSizeCst,
	XfrmAttrAlgAEAD: XfrmAlgoICVSizeCst, XfrmAttrAlgAuthTrunc: XfrmAlgoICVSizeCst,
	XfrmAttrEncap: XfrmEncapTmplSizeCst, XfrmAttrSA: XfrmUsersaInfoSizeCst, XfrmAttrSecCtx: XfrmUserSecCtxSizeCst,
	XfrmAttrTmpl: XfrmUserTmplSizeCst, XfrmAttrPolicy: XfrmUserpolicyInfoSizeCst,
	XfrmAttrPolicyType: XfrmUserpolicyTypeSizeCst,
	XfrmAttrLtimeVal:   XfrmLifetimeCurSizeCst, XfrmAttrReplayVal: XfrmReplayStateSizeCst,
	XfrmAttrReplayESNVal: XfrmReplayESNSizeCst, XfrmAttrSrcAddr: XfrmAddressSizeCst,
	XfrmAttrCoAddr: XfrmAddressSizeCst, XfrmAttrMark: XfrmMarkSizeCst,
	XfrmAttrAddressFilter: XfrmAddressFilterSizeCst, XfrmAttrOffloadDev: XfrmUserOffloadSizeCst,
}

// xfrmAttrWidth is the exact length of each scalar attribute but the u32s.
var xfrmAttrWidth = map[uint16]int{
	XfrmAttrLastUsed: 8, XfrmAttrProto: 1, XfrmAttrSADir: 1,
	XfrmAttrIPTFSReorderWindow: 2, XfrmAttrIPTFSDontFrag: 0,
}

// xfrmAttrLen checks the length of a struct or scalar attribute other
// than a u32 against xfrmAttrMin and xfrmAttrWidth.
func xfrmAttrLen(id uint16, d []byte) error {
	if w, ok := xfrmAttrWidth[id]; ok && len(d) != w {
		return fmt.Errorf("%w: %d needs %d bytes, has %d", ErrXfrmAttr, id, w, len(d))
	}
	if n, ok := xfrmAttrMin[id]; ok && len(d) < n {
		return fmt.Errorf("%w: %d is %d bytes, under %d", ErrXfrmAttr, id, len(d), n)
	}
	return nil
}

// set decodes one attribute into a, after checking its length.
func (a *XfrmAttrs) set(id uint16, d []byte) error {
	if p := a.u32Field(id); p != nil {
		if len(d) != 4 {
			return fmt.Errorf("%w: %d needs 4 bytes, has %d", ErrXfrmAttr, id, len(d))
		}
		v := binary.LittleEndian.Uint32(d)
		*p = &v
		return nil
	}
	if err := xfrmAttrLen(id, d); err != nil {
		return err
	}
	var err error
	switch id {
	case XfrmAttrAlgAuth:
		a.AlgAuth, err = xfrmAlgo(id, d, XfrmAlgoSizeCst)
	case XfrmAttrAlgCrypt:
		a.AlgCrypt, err = xfrmAlgo(id, d, XfrmAlgoSizeCst)
	case XfrmAttrAlgComp:
		a.AlgComp, err = xfrmAlgo(id, d, XfrmAlgoSizeCst)
	case XfrmAttrAlgAEAD:
		a.AlgAEAD, err = xfrmAlgo(id, d, XfrmAlgoICVSizeCst)
	case XfrmAttrAlgAuthTrunc:
		a.AlgAuthTrunc, err = xfrmAlgo(id, d, XfrmAlgoICVSizeCst)
	case XfrmAttrEncap:
		a.Encap = &XfrmEncapTmpl{binary.LittleEndian.Uint16(d), binary.BigEndian.Uint16(d[2:]),
			binary.BigEndian.Uint16(d[4:]), xfrmAddress(d[8:])}
	case XfrmAttrSA:
		sa := xfrmUsersaInfo(d)
		a.SA = &sa
	case XfrmAttrTmpl:
		a.Tmpl = xfrmUserTmpls(d)
	case XfrmAttrPolicy:
		pol := xfrmUserpolicyInfo(d)
		a.Policy = &pol
	case XfrmAttrPolicyType:
		v := d[0]
		a.PolicyType = &v
	case XfrmAttrSecCtx:
		a.SecCtx, err = xfrmSecCtx(d)
	case XfrmAttrLtimeVal:
		c := xfrmLifetimeCur(d)
		a.LtimeVal = &c
	case XfrmAttrReplayVal:
		a.ReplayVal = &XfrmReplayState{binary.LittleEndian.Uint32(d), binary.LittleEndian.Uint32(d[4:]),
			binary.LittleEndian.Uint32(d[8:])}
	case XfrmAttrReplayESNVal:
		a.ReplayESNVal, err = xfrmReplayESN(d)
	case XfrmAttrSrcAddr:
		v := xfrmAddress(d)
		a.SrcAddr = &v
	case XfrmAttrCoAddr:
		v := xfrmAddress(d)
		a.CoAddr = &v
	case XfrmAttrLastUsed:
		v := binary.LittleEndian.Uint64(d)
		a.LastUsed = &v
	case XfrmAttrMark:
		a.Mark = &XfrmMark{binary.LittleEndian.Uint32(d), binary.LittleEndian.Uint32(d[4:])}
	case XfrmAttrProto:
		v := d[0]
		a.Proto = &v
	case XfrmAttrSADir:
		v := d[0]
		a.SADir = &v
	case XfrmAttrAddressFilter:
		a.Filter = &XfrmAddressFilter{xfrmAddress(d), xfrmAddress(d[16:]), binary.LittleEndian.Uint16(d[32:]), d[34], d[35]}
	case XfrmAttrOffloadDev:
		a.OffloadDev = &XfrmUserOffload{int32(binary.LittleEndian.Uint32(d)), d[4]}
	case XfrmAttrIPTFSReorderWindow:
		v := binary.LittleEndian.Uint16(d)
		a.IPTFSReorderWindow = &v
	case XfrmAttrIPTFSDontFrag:
		a.IPTFSDontFrag = true
	}
	return err
}

// xfrmSecCtx decodes an XFRMA_SEC_CTX: its header, then ctx_len bytes of
// context.
func xfrmSecCtx(d []byte) (*XfrmSecCtx, error) {
	n := int(binary.LittleEndian.Uint16(d[6:]))
	if n > len(d)-XfrmUserSecCtxSizeCst {
		return nil, fmt.Errorf("%w: %d holds %d context bytes of %d", ErrXfrmAttr, XfrmAttrSecCtx,
			len(d)-XfrmUserSecCtxSizeCst, n)
	}
	return &XfrmSecCtx{binary.LittleEndian.Uint16(d[2:]), d[4], d[5],
		CopyBytes(d[XfrmUserSecCtxSizeCst : XfrmUserSecCtxSizeCst+n])}, nil
}

// xfrmReplayESN decodes an XFRMA_REPLAY_ESN_VAL: its header, then bmp_len
// u32s of bitmap.
func xfrmReplayESN(d []byte) (*XfrmReplayStateESN, error) {
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(d[4*i:]) }
	n := int(u(0))
	if n > (len(d)-XfrmReplayESNSizeCst)/4 {
		return nil, fmt.Errorf("%w: %d holds %d bytes of bitmap, under %d words", ErrXfrmAttr, XfrmAttrReplayESNVal,
			len(d)-XfrmReplayESNSizeCst, n)
	}
	r := &XfrmReplayStateESN{OSeq: u(1), Seq: u(2), OSeqHi: u(3), SeqHi: u(4), ReplayWindow: u(5),
		Bmp: make([]uint32, n)}
	for i := range r.Bmp {
		r.Bmp[i] = u(6 + i)
	}
	return r, nil
}

// parseXfrmAttrs decodes the XFRMA_* attributes in b, which starts at the
// NLMSG_ALIGN of the end of its message's struct, as nlmsg_parse reads
// them. It stops at the first malformed attribute, keeping those before.
func parseXfrmAttrs(b []byte, a *XfrmAttrs) error {
	var err error
	walkErr := WalkRTAttrs(b, func(id uint16, d []byte) {
		if err == nil {
			err = a.set(id, d)
		}
	})
	if walkErr != nil {
		return walkErr
	}
	return err
}
