package gotc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/f_flower.c flower_print_opt and its helpers (iproute2 v7.2.0), line
// for line, with tc's quirks kept (docs/netlink/upstream/iproute2-tc-flower.md,
// FL1-FL20). Where C reads an attribute it never tested (a NULL rtattr, or
// malloc'd memory it never wrote) the process's output is not reproducible;
// gotc refuses there, naming the attribute.

// Ethertypes and IP protocols flower_print_opt names (if_ether.h, in.h).
const (
	ethPARP  = 0x0806
	ethPRARP = 0x8035

	ipprotoICMP   = 1
	ipprotoTCP    = 6
	ipprotoUDP    = 17
	ipprotoESP    = 50
	ipprotoAH     = 51
	ipprotoICMPv6 = 58
	ipprotoSCTP   = 132
	ipprotoL2TP   = 115

	arpopRequest = 1
	arpopReply   = 2

	// tcHMinPriority and tcQoptMaxQueue are TC_H_MIN_PRIORITY and
	// TC_QOPT_MAX_QUEUE (pkt_sched.h): the classid minors read as hw_tc.
	tcHMinPriority = 0xFFE0
	tcQoptMaxQueue = 16

	// ctLabelOutCst is flower_print_ct_label's char out[128].
	ctLabelOutCst = 128
)

// flowerReadsNull is C reading an attribute it never tested.
func flowerReadsNull(what string) error {
	return fmt.Errorf("%w: flower %s (tc reads it unchecked)", errNotImplemented, what)
}

// flowerFlag is f_flower.c struct flag_to_string.
type flowerFlag struct {
	flag uint32
	enc  bool // FLOWER_ENC_DST_FLAGS, else FLOWER_IP_FLAGS
	name string
}

// flowerFlagsStr is f_flower.c flags_str.
var flowerFlagsStr = []flowerFlag{
	{xtcpnl.TcaFlowerKeyFlagsIsFragment, false, "frag"},
	{xtcpnl.TcaFlowerKeyFlagsFragIsFirst, false, "firstfrag"},
	{xtcpnl.TcaFlowerKeyFlagsTunnelCsum, true, "tuncsum"},
	{xtcpnl.TcaFlowerKeyFlagsTunnelDontFragment, true, "tundf"},
	{xtcpnl.TcaFlowerKeyFlagsTunnelOam, true, "tunoam"},
	{xtcpnl.TcaFlowerKeyFlagsTunnelCritOpt, true, "tuncrit"},
}

// flowerCtStates is f_flower.c flower_ct_states.
var flowerCtStates = []struct {
	name string
	flag uint16
}{
	{"trk", xtcpnl.TcaFlowerKeyCtFlagsTracked},
	{"new", xtcpnl.TcaFlowerKeyCtFlagsNew},
	{"est", xtcpnl.TcaFlowerKeyCtFlagsEstablished},
	{"rel", xtcpnl.TcaFlowerKeyCtFlagsRelated},
	{"inv", xtcpnl.TcaFlowerKeyCtFlagsInvalid},
	{"rpl", xtcpnl.TcaFlowerKeyCtFlagsReply},
}

// deficit is the "!!!Deficit" parse_rtattr prints on stderr (FL9).
func (r *runner) deficit(d xtcpnl.AttrDeficit) {
	if d.Any() {
		r.errf("!!!Deficit %d, rta_len=%d\n", d.Len, d.RtaLen)
	}
}

// printFlowerOpt is flower_print_opt.
func printFlowerOpt(r *runner, opt []byte, handle uint32) error {
	o := r.o
	tb := r.nested(opt, xtcpnl.TcaFlowerMaxKnown)
	nested := xtcpnl.ParseTcFlowerNested(opt)

	r.flowerHead(tb, handle)
	o.openObject("keys")
	ethType, ipProto := r.flowerL2Keys(tb, nested)
	r.flowerL3Keys(tb, ethType, ipProto)
	if err := r.flowerEncKeys(tb); err != nil {
		return err
	}
	if err := r.flowerMetaKeys(tb, nested); err != nil {
		return err
	}
	o.closeObject()
	r.flowerClsFlags(tb)

	if tb.Has(xtcpnl.TcaFlowerAct) {
		return r.printActions(tb.Bytes(xtcpnl.TcaFlowerAct))
	}
	return nil
}

// flowerHead is flower_print_opt's handle, classid or hw_tc, and indev.
func (r *runner) flowerHead(tb xtcpnl.AttrTable, handle uint32) {
	o := r.o
	if handle != 0 {
		o.uint(printAny, "handle", "handle 0x"+u32hex(handle)+" ", handle)
	}
	if tb.Has(xtcpnl.TcaFlowerClassid) {
		h := attrU32(tb, xtcpnl.TcaFlowerClassid)
		if m := tcHMin(h); m < tcHMinPriority || m > tcHMinPriority+tcQoptMaxQueue-1 {
			c := r.classid(h)
			o.str(printAny, colorNone, "classid", "classid "+c+" ", c)
		} else { // FL3: the major is dropped
			o.uint(printAny, "hw_tc", "hw_tc "+u32s(m-tcHMinPriority)+" ", m-tcHMinPriority)
		}
	}
	if tb.Has(xtcpnl.TcaFlowerIndev) {
		s, _ := tb.Str(xtcpnl.TcaFlowerIndev)
		o.nl()
		o.str(printAny, colorNone, "indev", "  indev "+s, s)
	}
}

// flowerL2Keys is flower_print_opt's VLAN, MAC, eth_type, ip_proto, ip_tos,
// ip_ttl and MPLS keys. It returns eth_type and ip_proto, which choose
// what follows; eth_type is host order here, network order in C.
func (r *runner) flowerL2Keys(tb xtcpnl.AttrTable, nested xtcpnl.TcFlowerNested) (ethType uint16, ipProto uint8) {
	o := r.o
	ipProto = 0xff
	// FL15: u8s through print_uint with "%d".
	for _, k := range []struct {
		t    uint16
		name string
		u8   bool
	}{
		{xtcpnl.TcaFlowerKeyNumOfVlans, "num_of_vlans", true},
		{xtcpnl.TcaFlowerKeyVlanID, "vlan_id", false},
		{xtcpnl.TcaFlowerKeyVlanPrio, "vlan_prio", true},
		{xtcpnl.TcaFlowerKeyVlanEthType, "vlan_ethtype", false},
		{xtcpnl.TcaFlowerKeyCvlanID, "cvlan_id", false},
		{xtcpnl.TcaFlowerKeyCvlanPrio, "cvlan_prio", true},
		{xtcpnl.TcaFlowerKeyCvlanEthType, "cvlan_ethtype", false},
	} {
		if !tb.Has(k.t) {
			continue
		}
		b := tb.Bytes(k.t)
		o.nl()
		switch {
		case k.t == xtcpnl.TcaFlowerKeyVlanEthType || k.t == xtcpnl.TcaFlowerKeyCvlanEthType:
			p := r.llProtoN2A(rtaU16(b))
			o.str(printAny, colorNone, k.name, "  "+k.name+" "+p, p)
		case k.u8:
			v := uint32(rtaU8(b))
			o.uint(printAny, k.name, "  "+k.name+" "+u32s(v), v)
		default:
			v := uint32(rtaU16(b))
			o.uint(printAny, k.name, "  "+k.name+" "+u32s(v), v)
		}
	}

	r.flowerEthAddr("dst_mac", tb, xtcpnl.TcaFlowerKeyEthDst, xtcpnl.TcaFlowerKeyEthDstMask)
	r.flowerEthAddr("src_mac", tb, xtcpnl.TcaFlowerKeyEthSrc, xtcpnl.TcaFlowerKeyEthSrcMask)

	if tb.Has(xtcpnl.TcaFlowerKeyEthType) {
		ethType = rtaBE16(tb.Bytes(xtcpnl.TcaFlowerKeyEthType))
		r.flowerEthType(ethType)
	}
	if tb.Has(xtcpnl.TcaFlowerKeyIPProto) {
		ipProto = rtaU8(tb.Bytes(xtcpnl.TcaFlowerKeyIPProto))
		r.flowerIPProto(ipProto)
	}

	r.printMaskedU8("ip_tos", tb, xtcpnl.TcaFlowerKeyIPTos, xtcpnl.TcaFlowerKeyIPTosMask)
	r.printMaskedU8("ip_ttl", tb, xtcpnl.TcaFlowerKeyIPTTL, xtcpnl.TcaFlowerKeyIPTTLMask)

	if tb.Has(xtcpnl.TcaFlowerKeyMplsOpts) && nested.MplsOpts {
		r.flowerMplsOpts(tb.Bytes(xtcpnl.TcaFlowerKeyMplsOpts))
	}
	if tb.Has(xtcpnl.TcaFlowerKeyMplsLabel) {
		o.nl()
		r.printUintIndentNameValue("mpls_label", attrU32(tb, xtcpnl.TcaFlowerKeyMplsLabel))
	}
	r.flowerMaskedU8("mpls_tc", tb, xtcpnl.TcaFlowerKeyMplsTc, 0, nil)
	r.flowerMaskedU8("mpls_bos", tb, xtcpnl.TcaFlowerKeyMplsBos, 0, nil)
	r.flowerMaskedU8("mpls_ttl", tb, xtcpnl.TcaFlowerKeyMplsTTL, 0, nil)
	return ethType, ipProto
}

// flowerL3Keys is flower_print_opt's addresses, ports, tcp_flags, ICMP,
// l2tpv3_sid, spi, ARP and PPPoE keys.
func (r *runner) flowerL3Keys(tb xtcpnl.AttrTable, ethType uint16, ipProto uint8) {
	o := r.o
	r.flowerIPAddr("dst_ip", ethType, tb, xtcpnl.TcaFlowerKeyIPv4Dst, xtcpnl.TcaFlowerKeyIPv4DstMask,
		xtcpnl.TcaFlowerKeyIPv6Dst, xtcpnl.TcaFlowerKeyIPv6DstMask)
	r.flowerIPAddr("src_ip", ethType, tb, xtcpnl.TcaFlowerKeyIPv4Src, xtcpnl.TcaFlowerKeyIPv4SrcMask,
		xtcpnl.TcaFlowerKeyIPv6Src, xtcpnl.TcaFlowerKeyIPv6SrcMask)

	r.flowerPorts(tb, ipProto)

	if tb.Has(xtcpnl.TcaFlowerKeyTCPFlags) { // FL4: "/%x" with no 0x
		s := "0x" + strconv.FormatUint(uint64(rtaBE16(tb.Bytes(xtcpnl.TcaFlowerKeyTCPFlags))), 16)
		if tb.Has(xtcpnl.TcaFlowerKeyTCPFlagsMask) {
			s += "/" + strconv.FormatUint(uint64(rtaBE16(tb.Bytes(xtcpnl.TcaFlowerKeyTCPFlagsMask))), 16)
		}
		r.printIndentNameValue("tcp_flags", s)
	}

	if t, m, ok := flowerICMPAttrs(ethType, ipProto, false); ok {
		r.flowerMaskedU8("icmp_type", tb, t, m, nil)
	}
	if t, m, ok := flowerICMPAttrs(ethType, ipProto, true); ok {
		r.flowerMaskedU8("icmp_code", tb, t, m, nil)
	}

	if tb.Has(xtcpnl.TcaFlowerKeyL2tpv3Sid) {
		v := rtaBE32(tb.Bytes(xtcpnl.TcaFlowerKeyL2tpv3Sid))
		o.nl()
		o.uint(printAny, "l2tpv3_sid", "  l2tpv3_sid "+u32s(v), v)
	}
	if tb.Has(xtcpnl.TcaFlowerKeySpi) {
		v := rtaBE32(tb.Bytes(xtcpnl.TcaFlowerKeySpi))
		o.nl()
		o.hex(printAny, "spi", "  spi 0x"+u32hex(v), v)
	}

	r.flowerIPAddr("arp_sip", ethPIP, tb, xtcpnl.TcaFlowerKeyArpSip, xtcpnl.TcaFlowerKeyArpSipMask, 0, 0)
	r.flowerIPAddr("arp_tip", ethPIP, tb, xtcpnl.TcaFlowerKeyArpTip, xtcpnl.TcaFlowerKeyArpTipMask, 0, 0)
	r.flowerMaskedU8("arp_op", tb, xtcpnl.TcaFlowerKeyArpOp, xtcpnl.TcaFlowerKeyArpOpMask, arpOpName)
	r.flowerEthAddr("arp_sha", tb, xtcpnl.TcaFlowerKeyArpSha, xtcpnl.TcaFlowerKeyArpShaMask)
	r.flowerEthAddr("arp_tha", tb, xtcpnl.TcaFlowerKeyArpTha, xtcpnl.TcaFlowerKeyArpThaMask)

	if tb.Has(xtcpnl.TcaFlowerKeyPppoeSid) {
		v := uint32(rtaBE16(tb.Bytes(xtcpnl.TcaFlowerKeyPppoeSid)))
		o.nl()
		o.uint(printAny, "pppoe_sid", "  pppoe_sid "+u32s(v), v)
	}
	if tb.Has(xtcpnl.TcaFlowerKeyPppProto) {
		p := r.pppProtoN2A(rtaBE16(tb.Bytes(xtcpnl.TcaFlowerKeyPppProto)))
		o.nl()
		o.str(printAny, colorNone, "ppp_proto", "  ppp_proto "+p, p)
	}
}

// flowerEncKeys is flower_print_opt's tunnel keys: enc_* and enc_opts.
func (r *runner) flowerEncKeys(tb xtcpnl.AttrTable) error {
	o := r.o
	encFam := func(v4mask uint16) uint16 {
		if tb.Has(v4mask) {
			return ethPIP
		}
		return ethPIPv6
	}
	r.flowerIPAddr("enc_dst_ip", encFam(xtcpnl.TcaFlowerKeyEncIPv4DstMsk), tb,
		xtcpnl.TcaFlowerKeyEncIPv4Dst, xtcpnl.TcaFlowerKeyEncIPv4DstMsk,
		xtcpnl.TcaFlowerKeyEncIPv6Dst, xtcpnl.TcaFlowerKeyEncIPv6DstMsk)
	r.flowerIPAddr("enc_src_ip", encFam(xtcpnl.TcaFlowerKeyEncIPv4SrcMsk), tb,
		xtcpnl.TcaFlowerKeyEncIPv4Src, xtcpnl.TcaFlowerKeyEncIPv4SrcMsk,
		xtcpnl.TcaFlowerKeyEncIPv6Src, xtcpnl.TcaFlowerKeyEncIPv6SrcMsk)

	if tb.Has(xtcpnl.TcaFlowerKeyEncKeyID) {
		o.nl()
		r.printUintIndentNameValue("enc_key_id", rtaBE32(tb.Bytes(xtcpnl.TcaFlowerKeyEncKeyID)))
	}
	r.printMaskedBE16("enc_dst_port", tb, xtcpnl.TcaFlowerKeyEncUDPDstPort, xtcpnl.TcaFlowerKeyEncUDPDstPMsk)
	r.printMaskedU8("enc_tos", tb, xtcpnl.TcaFlowerKeyEncIPTos, xtcpnl.TcaFlowerKeyEncIPTosMask)
	r.printMaskedU8("enc_ttl", tb, xtcpnl.TcaFlowerKeyEncIPTTL, xtcpnl.TcaFlowerKeyEncIPTTLMask)
	return r.flowerEncOpts(tb)
}

// flowerMetaKeys is flower_print_opt's ip_flags, enc_flags, l2_miss, ct_*
// and cfm keys.
func (r *runner) flowerMetaKeys(tb xtcpnl.AttrTable, nested xtcpnl.TcFlowerNested) error {
	o := r.o
	if err := r.flowerMatchingFlags("ip_flags", false, tb, xtcpnl.TcaFlowerKeyFlags, xtcpnl.TcaFlowerKeyFlagsMask); err != nil {
		return err
	}
	if err := r.flowerMatchingFlags("enc_flags", true, tb, xtcpnl.TcaFlowerKeyEncFlags, xtcpnl.TcaFlowerKeyEncFlagsMask); err != nil {
		return err
	}

	if tb.Has(xtcpnl.TcaFlowerL2Miss) {
		v := uint32(rtaU8(tb.Bytes(xtcpnl.TcaFlowerL2Miss)))
		o.nl()
		o.uint(printAny, "l2_miss", "  l2_miss "+u32s(v), v)
	}

	if err := r.flowerCtState(tb); err != nil {
		return err
	}
	r.printMaskedU16("ct_zone", tb, xtcpnl.TcaFlowerKeyCtZone, xtcpnl.TcaFlowerKeyCtZoneMask)
	r.printMaskedU32("ct_mark", tb, xtcpnl.TcaFlowerKeyCtMark, xtcpnl.TcaFlowerKeyCtMarkMask)
	if err := r.flowerCtLabel(tb); err != nil {
		return err
	}

	if tb.Has(xtcpnl.TcaFlowerKeyCfm) && nested.Cfm {
		ctb, d := xtcpnl.ParseTcFlowerCfm(tb.Bytes(xtcpnl.TcaFlowerKeyCfm))
		r.deficit(d)
		o.nl()
		o.fp("  cfm")
		o.openObject("cfm")
		if ctb.Has(xtcpnl.TcaFlowerKeyCfmMdLevel) {
			v := rtaU8(ctb.Bytes(xtcpnl.TcaFlowerKeyCfmMdLevel))
			o.hhu(printAny, "mdl", " mdl "+u32s(uint32(v)), v)
		}
		if ctb.Has(xtcpnl.TcaFlowerKeyCfmOpcode) {
			v := rtaU8(ctb.Bytes(xtcpnl.TcaFlowerKeyCfmOpcode))
			o.hhu(printAny, "op", " op "+u32s(uint32(v)), v)
		}
		o.closeObject()
	}
	return nil
}

// flowerClsFlags is flower_print_opt's TCA_FLOWER_FLAGS: each on its own
// line, and in_hw_count after in_hw.
func (r *runner) flowerClsFlags(tb xtcpnl.AttrTable) {
	o := r.o
	if tb.Has(xtcpnl.TcaFlowerFlags) {
		flags := attrU32(tb, xtcpnl.TcaFlowerFlags)
		if flags&xtcpnl.TcaClsFlagsSkipHw != 0 {
			o.nl()
			o.boolean(printAny, "skip_hw", "  skip_hw", true)
		}
		if flags&xtcpnl.TcaClsFlagsSkipSw != 0 {
			o.nl()
			o.boolean(printAny, "skip_sw", "  skip_sw", true)
		}
		if flags&xtcpnl.TcaClsFlagsInHw != 0 {
			o.nl()
			o.boolean(printAny, "in_hw", "  in_hw", true)
			if tb.Has(xtcpnl.TcaFlowerInHwCount) { // FL19: one space
				c := attrU32(tb, xtcpnl.TcaFlowerInHwCount)
				o.uint(printAny, "in_hw_count", " in_hw_count "+u32s(c), c)
			}
		} else if flags&xtcpnl.TcaClsFlagsNotInHw != 0 {
			o.nl()
			o.boolean(printAny, "not_in_hw", "  not_in_hw", true)
		}
	}
}

// flowerEthAddr is flower_print_eth_addr: nothing unless the address is
// ETH_ALEN; a mask of another length is ignored.
func (r *runner) flowerEthAddr(name string, tb xtcpnl.AttrTable, key, mask uint16) {
	const ethAlen = 6
	a := tb.Bytes(key)
	if !tb.Has(key) || len(a) != ethAlen {
		return
	}
	out := llAddrN2A(a)
	if m := tb.Bytes(mask); tb.Has(mask) && len(m) == ethAlen {
		if bits := maskBits(m); bits < 0 {
			out += "/" + llAddrN2A(m)
		} else if bits < ethAlen*8 {
			out += "/" + strconv.Itoa(bits)
		}
	}
	r.printIndentNameValue(name, out)
}

// flowerEthType is flower_print_eth_type: four names, else "%04x" with no
// 0x (FL7).
func (r *runner) flowerEthType(t uint16) {
	var out string
	switch t {
	case ethPIP:
		out = "ipv4"
	case ethPIPv6:
		out = "ipv6"
	case ethPARP:
		out = "arp"
	case ethPRARP:
		out = "rarp"
	default:
		out = fmt.Sprintf("%04x", t)
	}
	r.o.nl()
	r.o.str(printAny, colorNone, "eth_type", "  eth_type "+out, out)
}

// flowerIPProto is flower_print_ip_proto.
func (r *runner) flowerIPProto(p uint8) {
	var out string
	switch p {
	case ipprotoTCP:
		out = "tcp"
	case ipprotoUDP:
		out = "udp"
	case ipprotoSCTP:
		out = "sctp"
	case ipprotoICMP:
		out = "icmp"
	case ipprotoICMPv6:
		out = "icmpv6"
	case ipprotoL2TP:
		out = "l2tp"
	case ipprotoESP:
		out = "esp"
	case ipprotoAH:
		out = "ah"
	default:
		out = fmt.Sprintf("0x%02x", p)
	}
	r.o.nl()
	r.o.str(printAny, colorNone, "ip_proto", "  ip_proto "+out, out)
}

// flowerIPAddr is flower_print_ip_addr: the family by eth_type, nothing
// unless address and mask are both that family's length.
func (r *runner) flowerIPAddr(name string, ethType uint16, tb xtcpnl.AttrTable, a4, m4, a6, m6 uint16) {
	var key, mask uint16
	var n int
	switch ethType {
	case ethPIP:
		key, mask, n = a4, m4, 4
	case ethPIPv6:
		key, mask, n = a6, m6, 16
	default:
		return
	}
	a, m := tb.Bytes(key), tb.Bytes(mask)
	if key == 0 || !tb.Has(key) || len(a) != n || !tb.Has(mask) || len(m) != n {
		return
	}
	out := rtAddrN2A(a)
	if bits := maskBits(m); bits < 0 {
		out += "/" + rtAddrN2A(m)
	} else if bits < n*8 {
		out += "/" + strconv.Itoa(bits)
	}
	r.printIndentNameValue(name, out)
}

// flowerPortTypes is flower_port_attr_type, _mask_type and
// flower_port_range_attr_type: dst then src for TCP, UDP and SCTP.
func flowerPortTypes(ipProto uint8) (keys, masks, mins, maxs [2]uint16, ok bool) {
	switch ipProto {
	case ipprotoTCP:
		keys = [2]uint16{xtcpnl.TcaFlowerKeyTCPDst, xtcpnl.TcaFlowerKeyTCPSrc}
		masks = [2]uint16{xtcpnl.TcaFlowerKeyTCPDstMask, xtcpnl.TcaFlowerKeyTCPSrcMask}
	case ipprotoUDP:
		keys = [2]uint16{xtcpnl.TcaFlowerKeyUDPDst, xtcpnl.TcaFlowerKeyUDPSrc}
		masks = [2]uint16{xtcpnl.TcaFlowerKeyUDPDstMask, xtcpnl.TcaFlowerKeyUDPSrcMask}
	case ipprotoSCTP:
		keys = [2]uint16{xtcpnl.TcaFlowerKeySCTPDst, xtcpnl.TcaFlowerKeySCTPSrc}
		masks = [2]uint16{xtcpnl.TcaFlowerKeySCTPDstMask, xtcpnl.TcaFlowerKeySCTPSrcMask}
	default:
		return keys, masks, mins, maxs, false
	}
	mins = [2]uint16{xtcpnl.TcaFlowerKeyPortDstMin, xtcpnl.TcaFlowerKeyPortSrcMin}
	maxs = [2]uint16{xtcpnl.TcaFlowerKeyPortDstMax, xtcpnl.TcaFlowerKeyPortSrcMax}
	return keys, masks, mins, maxs, true
}

// flowerPorts is flower_print_opt's ports, then its port ranges: a range
// is a JSON object under the same "dst_port"/"src_port" key an exact port
// uses as a number (FL6).
func (r *runner) flowerPorts(tb xtcpnl.AttrTable, ipProto uint8) {
	keys, masks, mins, maxs, ok := flowerPortTypes(ipProto)
	if !ok {
		return
	}
	names := [2]string{"dst_port", "src_port"}
	for i, name := range names {
		r.printMaskedBE16(name, tb, keys[i], masks[i])
	}
	for i, name := range names {
		if !tb.Has(mins[i]) || !tb.Has(maxs[i]) {
			continue
		}
		lo, hi := rtaBE16(tb.Bytes(mins[i])), rtaBE16(tb.Bytes(maxs[i]))
		if r.o.isJSON() {
			r.o.openObject(name)
			r.o.uint(printJSON, "start", "", uint32(lo))
			r.o.uint(printJSON, "end", "", uint32(hi))
			r.o.closeObject()
			continue
		}
		r.printIndentNameValue(name, u32s(uint32(lo))+"-"+u32s(uint32(hi)))
	}
}

// flowerICMPAttrs is flower_icmp_attr_type and _mask_type.
func flowerICMPAttrs(ethType uint16, ipProto uint8, code bool) (key, mask uint16, ok bool) {
	switch {
	case ethType == ethPIP && ipProto == ipprotoICMP:
		if code {
			return xtcpnl.TcaFlowerKeyICMPv4Code, xtcpnl.TcaFlowerKeyICMPv4CodeMsk, true
		}
		return xtcpnl.TcaFlowerKeyICMPv4Type, xtcpnl.TcaFlowerKeyICMPv4TypeMsk, true
	case ethType == ethPIPv6 && ipProto == ipprotoICMPv6:
		if code {
			return xtcpnl.TcaFlowerKeyICMPv6Code, xtcpnl.TcaFlowerKeyICMPv6CodeMsk, true
		}
		return xtcpnl.TcaFlowerKeyICMPv6Type, xtcpnl.TcaFlowerKeyICMPv6TypeMsk, true
	}
	return 0, 0, false
}

// arpOpName is flower_print_arp_op_to_name.
func arpOpName(op uint8) (string, bool) {
	switch op {
	case arpopRequest:
		return "request", true
	case arpopReply:
		return "reply", true
	}
	return "", false
}

// flowerMaskedU8 is flower_print_masked_u8: "%d" or a name, and "/%d" of
// a mask that is not 0xff, decimal where print_masked_* prints "/0x%x"
// (FL5); a JSON string. mask 0 is flower_print_u8's NULL.
func (r *runner) flowerMaskedU8(name string, tb xtcpnl.AttrTable, key, mask uint16, toStr func(uint8) (string, bool)) {
	if !tb.Has(key) {
		return
	}
	v, m := rtaU8(tb.Bytes(key)), uint8(0xff)
	if mask != 0 && tb.Has(mask) {
		m = rtaU8(tb.Bytes(mask))
	}
	out := ""
	named := false
	if m == 0xff && toStr != nil {
		out, named = toStr(v)
	}
	if !named {
		out = strconv.Itoa(int(v))
	}
	if m != 0xff {
		out += "/" + strconv.Itoa(int(m))
	}
	r.printIndentNameValue(name, out)
}

// flowerMplsOpts is flower_print_mpls_opts and flower_print_mpls_opt_lse:
// an attribute whose raw type is not LSE|NLA_F_NESTED gets C's message,
// whose "expecting" is LSE & NLA_F_NESTED, always 0 (FL8).
func (r *runner) flowerMplsOpts(b []byte) {
	o := r.o
	o.nl()
	o.fp("  mpls")
	o.openArray(printJSON, "mpls")
	m := xtcpnl.ParseTcFlowerMplsOpts(b)
	for _, l := range m.Lses {
		if !l.Lse {
			r.errf("rta_type 0x%x, expecting 0x%x (0x%x & 0x%x)\n", l.RawType,
				xtcpnl.TcaFlowerKeyMplsOptsLse&nlaFNested, xtcpnl.TcaFlowerKeyMplsOptsLse, nlaFNested)
			continue
		}
		r.deficit(l.Deficit)
		o.nl()
		o.fp("    lse")
		o.openObject("")
		u8 := func(t uint16, key string) {
			if l.Tb.Has(t) {
				v := rtaU8(l.Tb.Bytes(t))
				o.hhu(printAny, key, " "+key+" "+u32s(uint32(v)), v)
			}
		}
		u8(xtcpnl.TcaFlowerKeyMplsOptLseDepth, "depth")
		if l.Tb.Has(xtcpnl.TcaFlowerKeyMplsOptLseLabel) {
			v := rtaU32(l.Tb.Bytes(xtcpnl.TcaFlowerKeyMplsOptLseLabel))
			o.uint(printAny, "label", " label "+u32s(v), v)
		}
		u8(xtcpnl.TcaFlowerKeyMplsOptLseTc, "tc")
		u8(xtcpnl.TcaFlowerKeyMplsOptLseBos, "bos")
		u8(xtcpnl.TcaFlowerKeyMplsOptLseTTL, "ttl")
		o.closeObject()
	}
	r.deficit(m.Deficit)
	o.closeArray(printJSON, "")
}

// nlaFNested is NLA_F_NESTED.
const nlaFNested = 1 << 15

// flowerMatchingFlags is flower_print_matching_flags. It reads the key
// without testing it (FL14).
func (r *runner) flowerMatchingFlags(name string, enc bool, tb xtcpnl.AttrTable, key, mask uint16) error {
	if !tb.Has(mask) || len(tb.Bytes(mask)) != 4 {
		return nil
	}
	if !tb.Has(key) {
		return flowerReadsNull(name + " key")
	}
	o := r.o
	mtf, mtfMask := rtaBE32(tb.Bytes(key)), rtaBE32(tb.Bytes(mask))
	count := 0
	for _, f := range flowerFlagsStr {
		if f.enc != enc || mtfMask&f.flag == 0 {
			continue
		}
		count++
		if count == 1 {
			o.nl()
			o.fp("  " + name + " ")
			o.openObject(name)
		} else {
			o.fp("/")
		}
		set := mtf&f.flag != 0
		o.boolean(printJSON, f.name, "", set)
		if set {
			o.fp(f.name)
		} else {
			o.fp("no" + f.name)
		}
	}
	if count > 0 {
		o.closeObject()
	}
	return nil
}

// flowerCtState is flower_print_ct_state: "+name" or "-name" for each bit
// of the mask (all ones without one) in tc's table order. With none of
// them in the mask, C prints an uninitialised buffer (FL13).
func (r *runner) flowerCtState(tb xtcpnl.AttrTable) error {
	if !tb.Has(xtcpnl.TcaFlowerKeyCtState) {
		return nil
	}
	state, mask := rtaU16(tb.Bytes(xtcpnl.TcaFlowerKeyCtState)), uint16(0xffff)
	if tb.Has(xtcpnl.TcaFlowerKeyCtStateMask) {
		mask = rtaU16(tb.Bytes(xtcpnl.TcaFlowerKeyCtStateMask))
	}
	var out strings.Builder
	for _, s := range flowerCtStates {
		if mask&s.flag == 0 {
			continue
		}
		if state&s.flag != 0 {
			out.WriteString("+" + s.name)
		} else {
			out.WriteString("-" + s.name)
		}
	}
	if out.Len() == 0 {
		return flowerReadsNull("ct_state with no known mask bit")
	}
	r.o.nl()
	r.o.str(printAny, colorNone, "ct_state", "  ct_state "+out.String(), out.String())
	return nil
}

// flowerCtLabel is flower_print_ct_label: the key in hex, then "/" and
// the mask (sized by the key) unless the key is 16 bytes and the mask all
// ones. C reads the mask without testing it (FL12).
func (r *runner) flowerCtLabel(tb xtcpnl.AttrTable) error {
	if !tb.Has(xtcpnl.TcaFlowerKeyCtLabels) {
		return nil
	}
	key := tb.Bytes(xtcpnl.TcaFlowerKeyCtLabels)
	mask := tb.Bytes(xtcpnl.TcaFlowerKeyCtLabelsMask)
	if !tb.Has(xtcpnl.TcaFlowerKeyCtLabelsMask) || len(mask) < len(key) || 4*len(key)+2 > ctLabelOutCst {
		return flowerReadsNull("ct_label mask")
	}
	out := hexstringN2A(key)
	printMask := len(key) != 16
	for i := 0; !printMask && i < len(key); i++ {
		printMask = mask[i] != 0xff
	}
	if printMask {
		out += "/" + hexstringN2A(mask[:len(key)])
	}
	r.o.nl()
	r.o.str(printAny, colorNone, "ct_label", "  ct_label "+out, out)
	return nil
}

// flowerEncOpts is flower_print_enc_opts: geneve, vxlan, erspan, gtp or
// pfcp, the first the key nest holds; each prints its JSON array, then
// flower_print_enc_parts the text "key/mask,...". C parses the mask nest
// unchecked, and a mask without the key's kind leaves its buffer
// uninitialised (FL11).
func (r *runner) flowerEncOpts(tb xtcpnl.AttrTable) error {
	if !tb.Has(xtcpnl.TcaFlowerKeyEncOpts) {
		return nil
	}
	if !tb.Has(xtcpnl.TcaFlowerKeyEncOptsMask) {
		return flowerReadsNull("enc_opts mask")
	}
	ktb, kd := xtcpnl.ParseTcFlowerEncOpts(tb.Bytes(xtcpnl.TcaFlowerKeyEncOpts))
	r.deficit(kd)
	mtb, md := xtcpnl.ParseTcFlowerEncOpts(tb.Bytes(xtcpnl.TcaFlowerKeyEncOptsMask))
	r.deficit(md)
	kinds := []struct {
		t     uint16
		name  string
		print func(name string, b []byte) (string, error)
	}{
		{xtcpnl.TcaFlowerKeyEncOptsGeneve, "geneve", r.flowerGeneveOpts},
		{xtcpnl.TcaFlowerKeyEncOptsVxlan, "vxlan", r.flowerVxlanOpts},
		{xtcpnl.TcaFlowerKeyEncOptsErspan, "erspan", r.flowerErspanOpts},
		{xtcpnl.TcaFlowerKeyEncOptsGtp, "gtp", r.flowerGtpOpts},
		{xtcpnl.TcaFlowerKeyEncOptsPfcp, "pfcp", r.flowerPfcpOpts},
	}
	for _, k := range kinds {
		if !ktb.Has(k.t) {
			continue
		}
		key, err := k.print(k.name+"_opt_key", ktb.Bytes(k.t))
		if err != nil {
			return err
		}
		if !mtb.Has(k.t) {
			return flowerReadsNull(k.name + " opts mask")
		}
		msk, err := k.print(k.name+"_opt_mask", mtb.Bytes(k.t))
		if err != nil {
			return err
		}
		r.flowerEncParts(k.name, key, msk)
		return nil
	}
	return nil
}

// flowerEncParts is flower_print_enc_parts: strsep's tokens paired as
// "key/mask," with the last comma dropped; a mask with fewer tokens gives
// glibc's "(null)". Text only: PRINT_FP.
func (r *runner) flowerEncParts(kind, key, msk string) {
	keys, masks := strings.Split(key, ","), strings.Split(msk, ",")
	parts := make([]string, len(keys))
	for i, k := range keys {
		m := "(null)"
		if i < len(masks) {
			m = masks[i]
		}
		parts[i] = k + "/" + m
	}
	r.o.nl()
	r.o.fp("  " + kind + "_opts " + strings.Join(parts, ","))
}

// flowerGeneveOpts is flower_print_geneve_opts, whose walk steps data
// length + 20 whatever each option's size (FL10).
func (r *runner) flowerGeneveOpts(name string, b []byte) (string, error) {
	o := r.o
	g, err := xtcpnl.ParseTcFlowerGeneve(b)
	for _, d := range g.Deficits {
		r.deficit(d)
	}
	if err != nil {
		return "", flowerReadsNull(err.Error())
	}
	if len(g.Opts) == 0 {
		return "", flowerReadsNull("geneve opts: an empty nest leaves the buffer unwritten")
	}
	o.openArray(printJSON, name)
	strs := make([]string, 0, len(g.Opts))
	for _, opt := range g.Opts {
		o.openObject("")
		o.uint(printJSON, "class", "", uint32(opt.Class))
		o.uint(printJSON, "type", "", uint32(opt.Type))
		o.openArray(printJSON, "data")
		for _, c := range opt.Data {
			o.uint(printJSON, "", "", uint32(c))
		}
		o.closeArray(printJSON, "data")
		o.closeObject()
		strs = append(strs, fmt.Sprintf("%04x:%02x:%s", opt.Class, opt.Type, hexstringN2A(opt.Data)))
	}
	o.closeArray(printJSON, name)
	return strings.Join(strs, ","), nil
}

// flowerVxlanOpts is flower_print_vxlan_opts.
func (r *runner) flowerVxlanOpts(name string, b []byte) (string, error) {
	v, err := xtcpnl.ParseTcFlowerVxlan(b)
	r.deficit(v.Deficit)
	if err != nil {
		return "", flowerReadsNull(err.Error())
	}
	o := r.o
	o.openArray(printJSON, name)
	o.openObject("")
	o.uint(printJSON, "gbp", "", v.Gbp)
	o.closeObject()
	o.closeArray(printJSON, name)
	return u32s(v.Gbp), nil
}

// flowerErspanOpts is flower_print_erspan_opts.
func (r *runner) flowerErspanOpts(name string, b []byte) (string, error) {
	e, err := xtcpnl.ParseTcFlowerErspan(b)
	r.deficit(e.Deficit)
	if err != nil {
		return "", flowerReadsNull(err.Error())
	}
	o := r.o
	o.openArray(printJSON, name)
	o.openObject("")
	o.uint(printJSON, "ver", "", uint32(e.Ver))
	o.uint(printJSON, "index", "", e.Index)
	o.uint(printJSON, "dir", "", uint32(e.Dir))
	o.uint(printJSON, "hwid", "", uint32(e.Hwid))
	o.closeObject()
	o.closeArray(printJSON, name)
	return fmt.Sprintf("%d:%d:%d:%d", e.Ver, e.Index, e.Dir, e.Hwid), nil
}

// flowerGtpOpts is flower_print_gtp_opts: no JSON at all (FL1).
func (r *runner) flowerGtpOpts(_ string, b []byte) (string, error) {
	g, err := xtcpnl.ParseTcFlowerGtp(b)
	r.deficit(g.Deficit)
	if err != nil {
		return "", flowerReadsNull(err.Error())
	}
	return fmt.Sprintf("%02x:%02x", g.PduType, g.Qfi), nil
}

// flowerPfcpOpts is flower_print_pfcp_opts: the u64 seid through
// print_uint, so JSON keeps its low 32 bits (FL2); text "%02x:%llx"
// (FL18).
func (r *runner) flowerPfcpOpts(name string, b []byte) (string, error) {
	p, err := xtcpnl.ParseTcFlowerPfcp(b)
	r.deficit(p.Deficit)
	if err != nil {
		return "", flowerReadsNull(err.Error())
	}
	o := r.o
	o.openArray(printJSON, name)
	o.openObject("")
	o.uint(printJSON, "type", "", uint32(p.Type))
	o.uint(printJSON, "seid", "", uint32(p.Seid&0xffffffff))
	o.closeObject()
	o.closeArray(printJSON, name)
	return fmt.Sprintf("%02x:%x", p.Type, p.Seid), nil
}

// flowerKeywords is flower_parse_opt's keyword chain, in order: each
// matched by matches() (a prefix), or by strcmp where strict.
var flowerKeywords = []struct {
	kw     string
	strict bool
}{
	{"classid", false}, {"flowid", false}, {"hw_tc", false}, {"ip_flags", false},
	{"l2_miss", true}, {"verbose", false}, {"skip_hw", false}, {"skip_sw", false},
	{"ct_state", false}, {"ct_zone", false}, {"ct_mark", false}, {"ct_label", false},
	{"indev", false}, {"num_of_vlans", true}, {"vlan_id", false}, {"vlan_prio", false},
	{"vlan_ethtype", false}, {"cvlan_id", false}, {"cvlan_prio", false},
	{"cvlan_ethtype", false}, {"mpls", false}, {"mpls_label", false}, {"mpls_tc", false},
	{"mpls_bos", false}, {"mpls_ttl", false}, {"dst_mac", false}, {"src_mac", false},
	{"ip_proto", false}, {"ip_tos", false}, {"ip_ttl", false}, {"dst_ip", false},
	{"src_ip", false}, {"dst_port", false}, {"src_port", false}, {"tcp_flags", false},
	{"type", false}, {"code", false}, {"l2tpv3_sid", true}, {"spi", true},
	{"arp_tip", false}, {"arp_sip", false}, {"arp_op", false}, {"arp_tha", false},
	{"arp_sha", false}, {"pppoe_sid", true}, {"ppp_proto", true}, {"enc_dst_ip", false},
	{"enc_src_ip", false}, {"enc_key_id", false}, {"enc_dst_port", false},
	{"enc_tos", false}, {"enc_ttl", false}, {"geneve_opts", false}, {"vxlan_opts", false},
	{"erspan_opts", false}, {"gtp_opts", true}, {"pfcp_opts", true}, {"enc_flags", true},
	{"action", false}, {"cfm", true},
}

// flowerKeyword is the keyword flower_parse_opt takes arg as, if any.
func flowerKeyword(arg string) (string, bool) {
	for _, k := range flowerKeywords {
		if (k.strict && arg == k.kw) || (!k.strict && matches(arg, k.kw)) {
			return k.kw, true
		}
	}
	return "", false
}

// flowerUsage is f_flower.c explain().
const flowerUsage = "Usage: ... flower	[ MATCH-LIST ] [ verbose ]\n" +
	"			[ skip_sw | skip_hw ]\n" +
	"			[ action ACTION-SPEC ] [ classid CLASSID ]\n" +
	"\n" +
	"Where: MATCH-LIST := [ MATCH-LIST ] MATCH\n" +
	"       MATCH      := {	indev DEV-NAME |\n" +
	"			num_of_vlans VLANS_COUNT |\n" +
	"			vlan_id VID |\n" +
	"			vlan_prio PRIORITY |\n" +
	"			vlan_ethtype [ ipv4 | ipv6 | ETH-TYPE ] |\n" +
	"			cvlan_id VID |\n" +
	"			cvlan_prio PRIORITY |\n" +
	"			cvlan_ethtype [ ipv4 | ipv6 | ETH-TYPE ] |\n" +
	"			pppoe_sid PSID |\n" +
	"			ppp_proto [ ipv4 | ipv6 | mpls_uc | mpls_mc | PPP_PROTO ] |\n" +
	"			dst_mac MASKED-LLADDR |\n" +
	"			src_mac MASKED-LLADDR |\n" +
	"			ip_proto [tcp | udp | sctp | icmp | icmpv6 | l2tp | esp | ah | IP-PROTO ] |\n" +
	"			ip_tos MASKED-IP_TOS |\n" +
	"			ip_ttl MASKED-IP_TTL |\n" +
	"			mpls LSE-LIST |\n" +
	"			mpls_label LABEL |\n" +
	"			mpls_tc TC |\n" +
	"			mpls_bos BOS |\n" +
	"			mpls_ttl TTL |\n" +
	"			spi SPI-INDEX  |\n" +
	"			l2tpv3_sid LSID |\n" +
	"			dst_ip PREFIX |\n" +
	"			src_ip PREFIX |\n" +
	"			dst_port PORT-NUMBER |\n" +
	"			src_port PORT-NUMBER |\n" +
	"			tcp_flags MASKED-TCP_FLAGS |\n" +
	"			type MASKED-ICMP-TYPE |\n" +
	"			code MASKED-ICMP-CODE |\n" +
	"			arp_tip IPV4-PREFIX |\n" +
	"			arp_sip IPV4-PREFIX |\n" +
	"			arp_op [ request | reply | OP ] |\n" +
	"			arp_tha MASKED-LLADDR |\n" +
	"			arp_sha MASKED-LLADDR |\n" +
	"			enc_dst_ip [ IPV4-ADDR | IPV6-ADDR ] |\n" +
	"			enc_src_ip [ IPV4-ADDR | IPV6-ADDR ] |\n" +
	"			enc_key_id [ KEY-ID ] |\n" +
	"			enc_tos MASKED-IP_TOS |\n" +
	"			enc_ttl MASKED-IP_TTL |\n" +
	"			geneve_opts MASKED-OPTIONS |\n" +
	"			vxlan_opts MASKED-OPTIONS |\n" +
	"			erspan_opts MASKED-OPTIONS |\n" +
	"			gtp_opts MASKED-OPTIONS |\n" +
	"			pfcp_opts MASKED-OPTIONS |\n" +
	"			ip_flags IP-FLAGS |\n" +
	"			l2_miss L2_MISS |\n" +
	"			enc_dst_port [ port_number ] |\n" +
	"			ct_state MASKED_CT_STATE |\n" +
	"			ct_label MASKED_CT_LABEL |\n" +
	"			ct_mark MASKED_CT_MARK |\n" +
	"			ct_zone MASKED_CT_ZONE |\n" +
	"			cfm CFM |\n" +
	"			enc_flags ENCFLAG-LIST }\n" +
	"	LSE-LIST := [ LSE-LIST ] LSE\n" +
	"	LSE := lse depth DEPTH { label LABEL | tc TC | bos BOS | ttl TTL }\n" +
	"	FILTERID := X:Y:Z\n" +
	"	MASKED_LLADDR := { LLADDR | LLADDR/MASK | LLADDR/BITS }\n" +
	"	MASKED_CT_STATE := combination of {+|-} and flags trk,est,new,rel,rpl,inv\n" +
	"	CFM := { mdl LEVEL | op OPCODE }\n" +
	"	ENCFLAG-LIST := [ ENCFLAG-LIST/ ]ENCFLAG\n" +
	"	ENCFLAG := { [no]tuncsum | [no]tundf | [no]tunoam | [no]tuncrit }\n" +
	"	ACTION-SPEC := ... look at individual actions\n" +
	"\n" +
	"NOTE:	CLASSID, IP-PROTO are parsed as hexadecimal input.\n" +
	"NOTE:	There can be only used one mask per one prio. If user needs\n" +
	"	to specify different mask, he has to use different prio.\n"

// parseFlowerGet is flower_parse_opt as `filter get ... flower` reaches
// it: get_u32's strict handle (FL16), then the first word after the kind.
// An unknown word is "What is" and explain(), help explain() alone, both
// -1; a match key would need the write-side parser, so gotc refuses it
// (C parses it, sends it, and tc_filter_get then fails on the words left:
// FL21). With no words, the nest of TCA_FLOWER_FLAGS 0 and, unless the
// protocol is all, TCA_FLOWER_KEY_ETH_TYPE (FL17).
func (r *runner) parseFlowerGet(a filterArgs, rest []string, q *xtcpnl.TcFilterReq) ([]byte, error) {
	if a.hasHandle {
		h, ok := getU32(a.handle, 0)
		if !ok {
			r.errf("Illegal \"handle\"\n")
			return nil, retCode(1)
		}
		q.Handle = h
	}
	if len(rest) > 0 {
		if kw, ok := flowerKeyword(rest[0]); ok {
			return nil, fmt.Errorf("%w: flower %s in a get", errNotImplemented, kw)
		}
		if rest[0] != helpWord {
			r.errf("What is \"%s\"?\n", rest[0])
		}
		r.errf("%s", flowerUsage)
		return nil, retCode(1)
	}
	return xtcpnl.TcFlowerGetOpts(q.Protocol)
}
