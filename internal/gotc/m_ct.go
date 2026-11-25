package gotc

import (
	"bytes"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/m_ct.c print_ct and its helpers (iproute2 v7.2.0), with
// tc/tc_util.c print_masked_type (newline false) and lib/inet_proto.c
// inet_proto_n2a.

// ctHelperNameMaxCst is ct_print_helper's `char helper[32]`: snprintf
// keeps 31 bytes and the NUL.
const ctHelperNameMaxCst = 31

// ctHelperProtos are the protocols a conntrack helper registers for
// (nf_conntrack_{ftp,irc,sip,tftp,h323,pptp,sane,snmp,...}), named as
// getprotobynumber does on the workbench (NixOS's iana-etc). Any other
// is refused: tc would read the host's /etc/protocols.
var ctHelperProtos = map[int32]string{unix.IPPROTO_TCP: wordTCP, unix.IPPROTO_UDP: wordUDP}

// inetProtoN2A is inet_proto_n2a: the name, or "ipproto-%d" under -n.
func (r *runner) inetProtoN2A(p int32) (string, error) {
	if r.numeric > 0 {
		return "ipproto-" + i32s(p), nil
	}
	if s, ok := ctHelperProtos[p]; ok {
		return s, nil
	}
	return "", fmt.Errorf("%w: ct helper protocol %d (tc names it from /etc/protocols)", errNotImplemented, p)
}

// ctMasked is print_masked_type with newline false: " ", then "name %u",
// then "/0x%x" (a JSON "%x" string under "<name>_mask") unless the mask is
// all ones.
func (r *runner) ctMasked(name string, v, m, all uint32) {
	r.o.fp(" ")
	r.printUintNameValue(name, v)
	if m != all {
		r.o.hex(printAny, name+"_mask", "/0x"+u32hex(m), m)
	}
}

// ctSprintIPAddr is ct_sprint_ip_addr: only a 4- or 16-byte address.
func ctSprintIPAddr(prefix string, b []byte) string {
	if len(b) != 4 && len(b) != 16 {
		return ""
	}
	return prefix + rtAddrN2A(b)
}

// ctSprintPort is ct_sprint_port: "%d" of the be16.
func ctSprintPort(prefix string, b []byte) string {
	if b == nil {
		return ""
	}
	return prefix + u32s(uint32(rtaBE16(b)))
}

// ctNat is ct_print_nat's text: a nat with neither src nor dst prints
// `"nat":""` (AC57). The caller has refused a NAT tc would memcmp through
// an absent or short bound (AC54).
func ctNat(act uint16, tb xtcpnl.AttrTable) (string, bool) {
	if act&xtcpnl.TcaCtActNat == 0 {
		return "", false
	}
	var out string
	switch {
	case act&xtcpnl.TcaCtActNatSrc != 0:
		out = wordSrc
	case act&xtcpnl.TcaCtActNatDst != 0:
		out = wordDst
	default:
		return "", true
	}
	out += ctSprintIPAddr(" addr ", tb.Bytes(xtcpnl.TcaCtNatIPv4Min))
	out += ctSprintIPAddr(" addr ", tb.Bytes(xtcpnl.TcaCtNatIPv6Min))
	differ := func(minT, maxT uint16, n int) bool {
		return tb.Has(maxT) && !bytes.Equal(tb.Bytes(minT)[:n], tb.Bytes(maxT)[:n])
	}
	if differ(xtcpnl.TcaCtNatIPv4Min, xtcpnl.TcaCtNatIPv4Max, 4) {
		out += ctSprintIPAddr("-", tb.Bytes(xtcpnl.TcaCtNatIPv4Max))
	} else if differ(xtcpnl.TcaCtNatIPv6Min, xtcpnl.TcaCtNatIPv6Max, 16) {
		out += ctSprintIPAddr("-", tb.Bytes(xtcpnl.TcaCtNatIPv6Max))
	}
	out += ctSprintPort(" port ", tb.Bytes(xtcpnl.TcaCtNatPortMin))
	if differ(xtcpnl.TcaCtNatPortMin, xtcpnl.TcaCtNatPortMax, 2) {
		out += ctSprintPort("-", tb.Bytes(xtcpnl.TcaCtNatPortMax))
	}
	return out, true
}

// ctLabels is ct_print_labels' text: the label in hex, then '/' and the
// mask for the label's length unless that is 16 bytes of 0xff. The caller
// has refused a mask that is absent or short, or text past out[256] (AC55).
func ctLabels(l, m []byte) string {
	s := hexstringN2A(l)
	printMask := len(l) != 16
	for _, c := range m[:len(l)] {
		printMask = printMask || c != 0xff
	}
	if printMask {
		s += "/" + hexstringN2A(m[:len(l)])
	}
	return s
}

// ctChecks is each attribute print_ct reads unchecked, and its width.
var ctChecks = []struct {
	t    uint16
	what string
	n    int
}{
	{xtcpnl.TcaCtAction, wordAction, 2},
	{xtcpnl.TcaCtMark, wordMark, 4},
	{xtcpnl.TcaCtMarkMask, "mark mask", 4},
	{xtcpnl.TcaCtZone, "zone", 2},
}

// printCt is print_ct: the kind prints "%s" with no space; a missing PARMS
// prints "[NULL ct parameters]" to stdout with no newline (AC58). The
// helper's family and protocol are u8 attributes read as ints, and any
// family but AF_INET prints ipv6 (AC56); the kernel dumps the ftp helper
// with family 0 (K13), so it reads back as ipv6.
func (r *runner) printCt(opt []byte, has bool) (int, error) {
	o := r.o
	const kind = xtcpnl.TcActKindCt
	o.str(printAny, colorNone, "kind", kind, kind)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaCtMaxKnown)
	c, err := xtcpnl.ParseTcCt(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !c.HasParms {
		o.fp("[NULL ct parameters]")
		return -1, nil
	}
	if c.ParmsShort(xtcpnl.TcCtSizeCst) {
		return 0, actShort(kind, c.ParmsLen)
	}
	tb := c.Tb
	for _, k := range ctChecks {
		if err := attrNeed(kind, k.what, tb.Bytes(k.t), k.n); err != nil {
			return 0, err
		}
	}
	if c.NatUnchecked {
		return 0, actUnchecked(kind, "nat bounds")
	}
	if c.LabelsUnchecked {
		return 0, actUnchecked(kind, "label mask")
	}
	helper, err := r.ctHelper(c)
	if err != nil {
		return 0, err
	}
	switch {
	case c.CtAction&xtcpnl.TcaCtActCommit != 0:
		s := "commit"
		if c.CtAction&xtcpnl.TcaCtActForce != 0 {
			s = "commit force"
		}
		o.str(printAny, colorNone, wordAction, " "+s, s)
	case c.CtAction&xtcpnl.TcaCtActClear != 0:
		o.str(printAny, colorNone, wordAction, " clear", "clear")
	}
	if b := tb.Bytes(xtcpnl.TcaCtMark); b != nil {
		m := uint32(0xFFFFFFFF)
		if mb := tb.Bytes(xtcpnl.TcaCtMarkMask); mb != nil {
			m = rtaU32(mb)
		}
		r.ctMasked(wordMark, rtaU32(b), m, 0xFFFFFFFF)
	}
	if b := tb.Bytes(xtcpnl.TcaCtZone); b != nil {
		r.ctMasked("zone", uint32(rtaU16(b)), 0xFFFF, 0xFFFF)
	}
	if l := tb.Bytes(xtcpnl.TcaCtLabels); l != nil {
		s := ctLabels(l, tb.Bytes(xtcpnl.TcaCtLabelsMask))
		o.str(printAny, colorNone, "label", " label "+s, s)
	}
	if helper != "" {
		o.str(printAny, colorNone, "helper", " helper "+helper, helper)
	}
	if s, ok := ctNat(c.CtAction, tb); ok {
		if s != "" {
			o.str(printAny, colorNone, "nat", " nat "+s, s)
		} else {
			o.str(printAny, colorNone, "nat", " nat", "")
		}
	}
	r.printActionControl(" ", c.P.Action, "")
	o.nl()
	r.actTail(c.P, "\t index ", c.TcActCommon)
	o.nl()
	return 0, nil
}

// ctHelper is ct_print_helper's "%s-%s-%s" in its 32-byte buffer, or ""
// when family, proto or name is absent.
func (r *runner) ctHelper(c xtcpnl.TcCtOpt) (string, error) {
	nb := c.Tb.Bytes(xtcpnl.TcaCtHelperName)
	if !c.HelperFamily.Has || !c.HelperProto.Has || nb == nil {
		return "", nil
	}
	if c.HelperFamily.Past || c.HelperProto.Past {
		return "", actUnchecked(xtcpnl.TcActKindCt, "helper family or protocol at the end of the options")
	}
	name, ok := xtcpnl.TcCString(nb)
	if !ok {
		return "", actUnchecked(xtcpnl.TcActKindCt, "helper name without its NUL")
	}
	fam := wordIPv6
	if c.HelperFamily.V == unix.AF_INET {
		fam = wordIPv4
	}
	proto, err := r.inetProtoN2A(c.HelperProto.V)
	if err != nil {
		return "", err
	}
	s := fam + "-" + proto + "-" + name
	if len(s) > ctHelperNameMaxCst {
		s = s[:ctHelperNameMaxCst]
	}
	return s, nil
}
