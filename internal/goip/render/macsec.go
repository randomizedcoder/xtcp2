package render

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// macsecPrefixCst is the prefix process() hands print_tx_sc and
// print_rx_sc: each channel's line and each statistics block carry it
// once, each SA's line twice.
const macsecPrefixCst = "    "

// The statistics names print_stats prints, ip/ipmacsec.c's tables: name
// i is counter i + 1, "" for one it leaves out, as the decoded counters
// are indexed.
var (
	macsecTxscStatsNames = []string{"OutPktsProtected", "OutPktsEncrypted", "OutOctetsProtected",
		"OutOctetsEncrypted"}
	macsecSecYStatsNames = []string{"OutPktsUntagged", "InPktsUntagged", "OutPktsTooLong", "InPktsNoTag",
		"InPktsBadTag", "InPktsUnknownSCI", "InPktsNoSCI", "InPktsOverrun"}
	macsecRxscStatsNames = []string{"InOctetsValidated", "InOctetsDecrypted", "InPktsUnchecked", "InPktsDelayed",
		"InPktsOK", "InPktsInvalid", "InPktsLate", "InPktsNotValid", "InPktsNotUsingSA", "InPktsUnusedSA"}
	macsecRxsaStatsNames = []string{"InPktsOK", "InPktsInvalid", "InPktsNotValid", "InPktsNotUsingSA",
		"InPktsUnusedSA"}
	macsecTxsaStatsNames = []string{"", "", "", "", "", "OutPktsProtected", "OutPktsEncrypted"}
)

// MacsecIsXPN is ciphersuite_is_xpn: an XPN suite, whose SAs carry a
// 64-bit PN and an SSCI.
func MacsecIsXPN(cid uint64) bool {
	return cid == xtcpnl.MacsecCipherGCMAESXPN128 || cid == xtcpnl.MacsecCipherGCMAESXPN256
}

// MacsecSecY is process()'s lines and object for one SecY of the GET_TXSC
// dump (ip/ipmacsec.c), which validate_dump and validate_secy_dump have
// passed: the device, print_attrs' SecY, print_tx_sc's channel and its SAs,
// each receive channel and its SAs, and the offload type. stats is
// show_stats, which adds each statistics nest present.
//
// An attribute ip reads without testing, an SA's AN, PN, ACTIVE or KEYID,
// a channel's SCI, ACTIVE or SA_LIST, or the offload's type, which the
// kernel always sends, is read as zero or empty when absent.
func MacsecSecY(m *xtcpnl.Macsec, ifname string, stats bool) (string, []byte) {
	var o genlOut
	idx := macsecU32(m.Ifindex)
	o.uint("ifindex", strconv.FormatUint(uint64(idx), 10)+": ", uint64(idx))
	o.str("ifname", ifname+": ", ifname)
	s := m.SecY
	macsecAttrs(&o, s)
	xpn := s.CipherSuite != nil && MacsecIsXPN(*s.CipherSuite)

	sci := macsecU64(s.SCI)
	o.text.WriteString(macsecPrefixCst)
	o.hex0x("sci", fmt.Sprintf("TXSC: %016x", sci), sci)
	enc := macsecU8(s.EncodingSA)
	o.uint("encoding_sa", " on SA "+strconv.FormatUint(uint64(enc), 10)+"\n", uint64(enc))
	if stats {
		macsecStats(&o, macsecSecYStatsNames, m.SecYStats)
		macsecStats(&o, macsecTxscStatsNames, m.TxscStats)
	}
	o.raw("sa_list", macsecSAList(&o, m.TxSAs, xpn, stats, macsecTxsaStatsNames))

	if m.RxSCs != nil {
		arr := []byte{'['}
		for i := range m.RxSCs {
			if i > 0 {
				arr = append(arr, ',')
			}
			arr = append(arr, macsecRxSC(&o, &m.RxSCs[i], xpn, stats)...)
		}
		o.raw("rx_sc", append(arr, ']'))
	}
	if m.HasOffload {
		t := macsecOffloadName(macsecU8(m.OffloadType))
		o.str("offload", macsecPrefixCst+"offload: "+t+" ", t)
		o.text.WriteString("\n")
	}
	return o.text.String(), o.object()
}

// macsecAttrs is print_attrs: each flag present, the validation, the
// window when present, then on a second line the cipher suite and the ICV
// length. Its sc and sa flags are MACSEC_RXSC_ATTR_ACTIVE and
// MACSEC_SA_ATTR_ACTIVE, both 2, read from the SecY's attributes, where 2
// is ENCODING_SA: both print the encoding SA's number as a flag.
func macsecAttrs(o *genlOut, s *xtcpnl.MacsecSecY) {
	macsecFlag(o, "protect", s.Protect)
	if s.Validate != nil {
		v := macsecValidateName(*s.Validate)
		o.str("validate", "validate "+v+" ", v)
	}
	macsecFlag(o, "sc", s.EncodingSA)
	macsecFlag(o, "sa", s.EncodingSA)
	macsecFlag(o, "encrypt", s.Encrypt)
	macsecFlag(o, "send_sci", s.IncSCI)
	macsecFlag(o, "end_station", s.ES)
	macsecFlag(o, "scb", s.SCB)
	macsecFlag(o, "replay", s.Replay)
	if s.Window != nil {
		o.uint("window", "window "+strconv.FormatUint(uint64(*s.Window), 10)+" ", uint64(*s.Window))
	}
	if s.CipherSuite != nil {
		name := macsecCipherName(*s.CipherSuite)
		o.text.WriteString("\n")
		o.str("cipher_suite", macsecPrefixCst+"cipher suite: "+name+",", name)
	}
	if s.ICVLen != nil {
		o.uint("icv_length", " using ICV length "+strconv.FormatUint(uint64(*s.ICVLen), 10)+"\n",
			uint64(*s.ICVLen))
	}
}

// macsecFlag is print_flag: nothing when absent, else "desc on " or "desc
// off " in the text and a JSON bool.
func macsecFlag(o *genlOut, desc string, v *uint8) {
	if v == nil {
		return
	}
	o.jsonBool(desc, *v != 0)
	o.text.WriteString(desc + " " + macsecOnOff(*v != 0) + " ")
}

// macsecRxSC is print_rx_sc for one channel of print_rxsc_list: its line
// and statistics in o's text, its object returned. ACTIVE is read as a
// u32 and handed on as a u8, the attribute's own byte.
func macsecRxSC(o *genlOut, sc *xtcpnl.MacsecRxSC, xpn, stats bool) []byte {
	var co genlOut
	sci := macsecU64(sc.SCI)
	co.text.WriteString(macsecPrefixCst)
	co.hex0x("sci", fmt.Sprintf("RXSC: %016x,", sci), sci)
	active := macsecU8(sc.Active) != 0
	co.jsonBool("active", active)
	co.text.WriteString(" state " + macsecOnOff(active) + "\n")
	if stats {
		macsecStats(&co, macsecRxscStatsNames, sc.Stats)
	}
	co.raw("sa_list", macsecSAList(&co, sc.SAs, xpn, stats, macsecRxsaStatsNames))
	o.text.WriteString(co.text.String())
	return co.object()
}

// macsecSAList is a channel's SA loop, print_tx_sc's or print_rx_sc's:
// each SA's line in o's text, the array of their objects returned.
func macsecSAList(o *genlOut, sas []xtcpnl.MacsecSA, xpn, stats bool, names []string) []byte {
	arr := []byte{'['}
	for i := range sas {
		var so genlOut
		macsecSA(&so, &sas[i], xpn)
		if stats {
			macsecStats(&so, names, macsecWiden(sas[i].Stats))
		}
		o.text.WriteString(so.text.String())
		if i > 0 {
			arr = append(arr, ',')
		}
		arr = append(arr, so.object()...)
	}
	return append(arr, ']')
}

// macsecSA is one SA's line: the AN, the PN, a u32 or on an XPN SecY a
// u64 and the SSCI, with no space between them, the state and the key id.
func macsecSA(o *genlOut, sa *xtcpnl.MacsecSA, xpn bool) {
	an := macsecU8(sa.AN)
	o.text.WriteString(macsecPrefixCst + macsecPrefixCst)
	o.uint("an", strconv.FormatUint(uint64(an), 10)+":", uint64(an))
	pn := macsecU64(sa.PN)
	if xpn {
		o.uint("pn", " PN "+strconv.FormatUint(pn, 10)+",", pn)
		ssci := macsecU32(sa.SSCI)
		o.hex0x("ssci", fmt.Sprintf("SSCI %08x", ssci), uint64(ssci))
	} else {
		// rta_getattr_u32's read of the attribute.
		pn32 := uint64(uint32(pn))
		o.uint("pn", " PN "+strconv.FormatUint(pn32, 10)+",", pn32)
	}
	active := macsecU8(sa.Active) != 0
	o.jsonBool("active", active)
	o.text.WriteString(" state " + macsecOnOff(active) + ",")
	key := hex.EncodeToString(sa.KeyID)
	o.str("key", " key "+key+"\n", key)
}

// macsecStats is print_stats over a decoded nest, nothing when the nest
// is absent: under -j each counter present as a member; in the text
// print_fp_stats' two lines, the names, then each counter right-aligned
// under its name, "-" for one absent.
func macsecStats(o *genlOut, names []string, vals []*uint64) {
	if vals == nil {
		return
	}
	var head, row strings.Builder
	head.WriteString(macsecPrefixCst + "stats:")
	row.WriteString("\n" + macsecPrefixCst + "      ")
	for i, name := range names {
		if name == "" {
			continue
		}
		head.WriteString(" " + name)
		w := len(name) + 1
		if i >= len(vals) || vals[i] == nil {
			fmt.Fprintf(&row, "%*c", w, '-')
			continue
		}
		fmt.Fprintf(&row, "%*d", w, *vals[i])
		o.key(name)
		o.obj = strconv.AppendUint(o.obj, *vals[i], 10)
	}
	o.text.WriteString(head.String() + row.String() + "\n")
}

// macsecWiden is an SA's u32 counters as getattr_u64 reads them.
func macsecWiden(s []*uint32) []*uint64 {
	if s == nil {
		return nil
	}
	out := make([]*uint64, len(s))
	for i, v := range s {
		if v != nil {
			w := uint64(*v)
			out[i] = &w
		}
	}
	return out
}

// macsecCipherName is cs_id_to_name.
func macsecCipherName(cid uint64) string {
	switch cid {
	case xtcpnl.MacsecCipherDefault, xtcpnl.MacsecCipherGCMAES128:
		return "GCM-AES-128"
	case xtcpnl.MacsecCipherGCMAES256:
		return "GCM-AES-256"
	case xtcpnl.MacsecCipherGCMAESXPN128:
		return "GCM-AES-XPN-128"
	case xtcpnl.MacsecCipherGCMAESXPN256:
		return "GCM-AES-XPN-256"
	}
	return macsecUnknownCst
}

// macsecUnknownCst is the name ip/ipmacsec.c gives a value past its table,
// and macsecOffCst and macsecDisabledCst the names it gives zero.
const (
	macsecUnknownCst  = "(unknown)"
	macsecOffCst      = "off"
	macsecDisabledCst = "disabled"
)

// macsecValidateName is validate_to_str.
func macsecValidateName(v uint8) string {
	names := [...]string{macsecDisabledCst, "check", "strict"}
	if int(v) < len(names) {
		return names[v]
	}
	return macsecUnknownCst
}

// macsecOffloadName is offload_to_str.
func macsecOffloadName(v uint8) string {
	names := [...]string{macsecOffCst, "phy", "mac"}
	if int(v) < len(names) {
		return names[v]
	}
	return macsecUnknownCst
}

// macsecOnOff is the flags' and states' word.
func macsecOnOff(v bool) string {
	if v {
		return "on"
	}
	return macsecOffCst
}

func macsecU8(v *uint8) uint8 {
	if v == nil {
		return 0
	}
	return *v
}

func macsecU32(v *uint32) uint32 {
	if v == nil {
		return 0
	}
	return *v
}

func macsecU64(v *uint64) uint64 {
	if v == nil {
		return 0
	}
	return *v
}
