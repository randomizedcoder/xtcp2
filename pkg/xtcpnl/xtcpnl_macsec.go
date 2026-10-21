package xtcpnl

// This file decodes the macsec generic netlink family behind `ip macsec`:
// ip's SA, receive channel and offload requests and the kernel's GET_TXSC
// dump, one SecY per message (drivers/net/macsec.c, ip/ipmacsec.c). The
// family id is assigned at boot, so the caller resolves "macsec" through
// the controller before handing a body here.

const (
	// MacsecGenlName is the family's name in the controller.
	MacsecGenlName = "macsec"

	// MACSEC_CMD_* (include/uapi/linux/if_macsec.h). GET_TXSC is 0.
	MacsecCmdGetTxsc    uint8 = 0
	MacsecCmdAddRxsc    uint8 = 1
	MacsecCmdDelRxsc    uint8 = 2
	MacsecCmdUpdRxsc    uint8 = 3
	MacsecCmdAddTxsa    uint8 = 4
	MacsecCmdDelTxsa    uint8 = 5
	MacsecCmdUpdTxsa    uint8 = 6
	MacsecCmdAddRxsa    uint8 = 7
	MacsecCmdDelRxsa    uint8 = 8
	MacsecCmdUpdRxsa    uint8 = 9
	MacsecCmdUpdOffload uint8 = 10

	// MACSEC_ATTR_*, the top level.
	MacsecAttrIfindex    uint16 = 1 // u32
	MacsecAttrRxscConfig uint16 = 2 // nested MACSEC_RXSC_ATTR_*, a request's channel
	MacsecAttrSAConfig   uint16 = 3 // nested MACSEC_SA_ATTR_*, a request's SA
	MacsecAttrSecY       uint16 = 4 // nested MACSEC_SECY_ATTR_*
	MacsecAttrTxsaList   uint16 = 5 // a nest of SA nests
	MacsecAttrRxscList   uint16 = 6 // a nest of receive channel nests
	MacsecAttrTxscStats  uint16 = 7 // nested u64 counters
	MacsecAttrSecYStats  uint16 = 8 // nested u64 counters
	MacsecAttrOffload    uint16 = 9 // nested MACSEC_OFFLOAD_ATTR_*

	// MACSEC_SECY_ATTR_*. Type 14 is the pad before a u64; it is skipped.
	MacsecSecYAttrSCI         uint16 = 1  // u64, network order
	MacsecSecYAttrEncodingSA  uint16 = 2  // u8
	MacsecSecYAttrWindow      uint16 = 3  // u32
	MacsecSecYAttrCipherSuite uint16 = 4  // u64
	MacsecSecYAttrICVLen      uint16 = 5  // u8
	MacsecSecYAttrProtect     uint16 = 6  // u8
	MacsecSecYAttrReplay      uint16 = 7  // u8
	MacsecSecYAttrOper        uint16 = 8  // u8
	MacsecSecYAttrValidate    uint16 = 9  // u8
	MacsecSecYAttrEncrypt     uint16 = 10 // u8
	MacsecSecYAttrIncSCI      uint16 = 11 // u8
	MacsecSecYAttrES          uint16 = 12 // u8
	MacsecSecYAttrSCB         uint16 = 13 // u8

	// MACSEC_RXSC_ATTR_*. Type 5 is the pad; it is skipped.
	MacsecRxscAttrSCI    uint16 = 1 // u64, network order
	MacsecRxscAttrActive uint16 = 2 // u8
	MacsecRxscAttrSAList uint16 = 3 // a nest of SA nests
	MacsecRxscAttrStats  uint16 = 4 // nested u64 counters

	// MACSEC_SA_ATTR_*. Type 7 is the pad; it is skipped.
	MacsecSAAttrAN     uint16 = 1 // u8
	MacsecSAAttrActive uint16 = 2 // u8
	MacsecSAAttrPN     uint16 = 3 // u32, or u64 on an XPN SecY
	MacsecSAAttrKey    uint16 = 4 // binary; ip sends it, the kernel never
	MacsecSAAttrKeyID  uint16 = 5 // binary, 16 bytes
	MacsecSAAttrStats  uint16 = 6 // nested u32 counters
	MacsecSAAttrSSCI   uint16 = 8 // u32, network order; XPN only
	MacsecSAAttrSalt   uint16 = 9 // binary, 12 bytes; XPN only, never sent back

	// MACSEC_OFFLOAD_ATTR_TYPE, a u8. Type 2 is the pad.
	MacsecOffloadAttrType uint16 = 1

	// The counters in each statistics nest, numbered 1 to N in their
	// enum: macsec_txsc_stats_attr, macsec_secy_stats_attr and
	// macsec_rxsc_stats_attr (u64 each, the pad at N + 1), and
	// macsec_sa_stats_attr (u32 each, no pad).
	MacsecTxscStatsCount = 4
	MacsecSecYStatsCount = 8
	MacsecRxscStatsCount = 10
	MacsecSAStatsCount   = 7
)

// MacsecSecY is a decoded MACSEC_ATTR_SECY nest. Each field is nil when
// absent.
type MacsecSecY struct {
	SCI         *uint64 // as ip prints it, the MAC address then the port
	EncodingSA  *uint8
	Window      *uint32
	CipherSuite *uint64
	ICVLen      *uint8
	Protect     *uint8
	Replay      *uint8
	Oper        *uint8
	Validate    *uint8
	Encrypt     *uint8
	IncSCI      *uint8
	ES          *uint8
	SCB         *uint8
}

// MacsecSA is a decoded SA nest, a request's SA_CONFIG or an entry of a
// TXSA_LIST or SA_LIST. The scalar fields are nil and the binary ones nil
// when absent. Stats is nil when absent, and otherwise holds counter i + 1
// at index i.
type MacsecSA struct {
	AN     *uint8
	Active *uint8
	PN     *uint64
	XPN    bool   // PN was 8 bytes, as on an XPN SecY
	Key    []byte // owned
	KeyID  []byte // owned
	SSCI   *uint32
	Salt   []byte // owned
	Stats  []*uint32
}

// MacsecRxSC is a decoded receive channel nest, a request's RXSC_CONFIG or
// an entry of an RXSC_LIST. SAs is nil when absent and non-nil when the
// kernel sent an empty list.
type MacsecRxSC struct {
	SCI    *uint64 // as ip prints it
	Active *uint8
	SAs    []MacsecSA
	Stats  []*uint64
}

// Macsec is a decoded macsec message: a request naming a SecY by ifindex,
// or one SecY of the kernel's GET_TXSC dump. The nests are nil when
// absent; TxSAs and RxSCs are non-nil when the kernel sent an empty list.
type Macsec struct {
	GenericNetlinkMessage
	Ifindex     *uint32
	RxSC        *MacsecRxSC // RXSC_CONFIG
	SA          *MacsecSA   // SA_CONFIG
	SecY        *MacsecSecY
	TxSAs       []MacsecSA
	RxSCs       []MacsecRxSC
	TxscStats   []*uint64
	SecYStats   []*uint64
	OffloadType *uint8
	HasOffload  bool
}

// ParseMacsec decodes a macsec body AFTER the caller has resolved its
// family as MacsecGenlName. It builds on ParseGenericNetlink, so framing is
// checked as for every genl family, a repeated attribute is an error at
// every level, each scalar must be its exact width (a PN 4 or 8 bytes), a
// nest must not be network-order, and each statistics nest's counters
// must be their width. A key's, key id's or salt's length is not checked.
// An unknown command or attribute is not an error.
func ParseMacsec(body []byte) (Macsec, error) {
	var m Macsec
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.u32(MacsecAttrIfindex, &m.Ifindex)
	if nd, ok := d.nested(MacsecAttrRxscConfig); ok {
		m.RxSC = nd.macsecRxSC()
		d.err = nd.err
	}
	if nd, ok := d.nested(MacsecAttrSAConfig); ok {
		m.SA = nd.macsecSA()
		d.err = nd.err
	}
	if nd, ok := d.nested(MacsecAttrSecY); ok {
		m.SecY = nd.macsecSecY()
		d.err = nd.err
	}
	if items := d.list(MacsecAttrTxsaList); items != nil {
		m.TxSAs = make([]MacsecSA, 0, len(items))
		for i := 0; i < len(items) && d.err == nil; i++ {
			m.TxSAs = append(m.TxSAs, *items[i].macsecSA())
			d.err = items[i].err
		}
	}
	if items := d.list(MacsecAttrRxscList); items != nil {
		m.RxSCs = make([]MacsecRxSC, 0, len(items))
		for i := 0; i < len(items) && d.err == nil; i++ {
			m.RxSCs = append(m.RxSCs, *items[i].macsecRxSC())
			d.err = items[i].err
		}
	}
	m.TxscStats = d.counters64(MacsecAttrTxscStats, MacsecTxscStatsCount)
	m.SecYStats = d.counters64(MacsecAttrSecYStats, MacsecSecYStatsCount)
	if nd, ok := d.nested(MacsecAttrOffload); ok {
		m.HasOffload = true
		nd.u8(MacsecOffloadAttrType, &m.OffloadType)
		d.err = nd.err
	}
	return m, d.err
}

// macsecSecY reads a SecY nest's attributes.
func (d *genlDecoder) macsecSecY() *MacsecSecY {
	var s MacsecSecY
	d.be64(MacsecSecYAttrSCI, &s.SCI)
	d.u8(MacsecSecYAttrEncodingSA, &s.EncodingSA)
	d.u32(MacsecSecYAttrWindow, &s.Window)
	d.u64(MacsecSecYAttrCipherSuite, &s.CipherSuite)
	for _, f := range []struct {
		id  uint16
		dst **uint8
	}{
		{MacsecSecYAttrICVLen, &s.ICVLen}, {MacsecSecYAttrProtect, &s.Protect}, {MacsecSecYAttrReplay, &s.Replay},
		{MacsecSecYAttrOper, &s.Oper}, {MacsecSecYAttrValidate, &s.Validate}, {MacsecSecYAttrEncrypt, &s.Encrypt},
		{MacsecSecYAttrIncSCI, &s.IncSCI}, {MacsecSecYAttrES, &s.ES}, {MacsecSecYAttrSCB, &s.SCB},
	} {
		d.u8(f.id, f.dst)
	}
	return &s
}

// macsecSA reads an SA nest's attributes.
func (d *genlDecoder) macsecSA() *MacsecSA {
	var s MacsecSA
	d.u8(MacsecSAAttrAN, &s.AN)
	d.u8(MacsecSAAttrActive, &s.Active)
	s.XPN = d.nlaUint(MacsecSAAttrPN, &s.PN)
	s.Key = d.bytes(MacsecSAAttrKey)
	s.KeyID = d.bytes(MacsecSAAttrKeyID)
	d.be32(MacsecSAAttrSSCI, &s.SSCI)
	s.Salt = d.bytes(MacsecSAAttrSalt)
	if nd, ok := d.nested(MacsecSAAttrStats); ok {
		s.Stats = make([]*uint32, MacsecSAStatsCount)
		for i := range s.Stats {
			nd.u32(uint16(i+1), &s.Stats[i])
		}
		d.err = nd.err
	}
	return &s
}

// macsecRxSC reads a receive channel nest's attributes, its SAs included.
func (d *genlDecoder) macsecRxSC() *MacsecRxSC {
	var c MacsecRxSC
	d.be64(MacsecRxscAttrSCI, &c.SCI)
	d.u8(MacsecRxscAttrActive, &c.Active)
	if items := d.list(MacsecRxscAttrSAList); items != nil {
		c.SAs = make([]MacsecSA, 0, len(items))
		for i := 0; i < len(items) && d.err == nil; i++ {
			c.SAs = append(c.SAs, *items[i].macsecSA())
			d.err = items[i].err
		}
	}
	c.Stats = d.counters64(MacsecRxscAttrStats, MacsecRxscStatsCount)
	return &c
}

// counters64 reads a nest of u64 counters numbered 1 to n, counter i + 1
// at index i: nil when absent.
func (d *genlDecoder) counters64(id uint16, n int) []*uint64 {
	nd, ok := d.nested(id)
	if !ok {
		return nil
	}
	s := make([]*uint64, n)
	for i := range s {
		nd.u64(uint16(i+1), &s[i])
	}
	d.err = nd.err
	return s
}
