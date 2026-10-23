package render

import (
	"fmt"
	"math/bits"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The NETLINK_XFRM printers of ip/ipxfrm.c, as iproute2-7.1.0 has them.
// `ip xfrm` prints text alone, with no JSON form, so these return the text.
// The newline is _SL_'s, `\n` but under -o, which goip refuses outside
// address.

// XfrmOpt is what the printers read besides the message: show_stats,
// whether keys are hidden, preferred_family for a selector with no family,
// ll_index_to_name for a selector's or an offload's device, strxf_proto's
// getprotobynumber for a selector's upper protocol, which goip, reading no
// config files, has the caller name, and the zone strftime's localtime
// uses.
type XfrmOpt struct {
	Stats     bool
	Nokeys    bool
	Family    uint8
	IndexName func(int32) string
	ProtoName func(uint8) string
	Zone      *time.Location
}

// The XFRM protocols strxf_xfrmproto names, in xfrmproto_types' order.
var xfrmProtoNames = []struct {
	name  string
	proto int
}{
	{"esp", unix.IPPROTO_ESP}, {"ah", unix.IPPROTO_AH}, {"comp", unix.IPPROTO_COMP},
	{"route2", unix.IPPROTO_ROUTING}, {"hao", unix.IPPROTO_DSTOPTS},
	{"ipsec-any", 255},
}

// XfrmProtoName is strxf_xfrmproto: the name, or the number.
func XfrmProtoName(proto uint8) string {
	for _, p := range xfrmProtoNames {
		if p.proto == int(proto) {
			return p.name
		}
	}
	return strconv.FormatUint(uint64(proto), 10)
}

// XfrmProtoByName is xfrm_xfrmproto_getbyname, by strcmp: the number, or -1.
func XfrmProtoByName(name string) int {
	for _, p := range xfrmProtoNames {
		if p.name == name {
			return p.proto
		}
	}
	return -1
}

// xfrmAlgoTypeName is strxf_algotype.
func xfrmAlgoTypeName(t uint16) string {
	switch t {
	case xtcpnl.XfrmAttrAlgCrypt:
		return "enc"
	case xtcpnl.XfrmAttrAlgAuth:
		return "auth"
	case xtcpnl.XfrmAttrAlgComp:
		return "comp"
	case xtcpnl.XfrmAttrAlgAEAD:
		return "aead"
	case xtcpnl.XfrmAttrAlgAuthTrunc:
		return "auth-trunc"
	}
	return strconv.FormatUint(uint64(t), 10)
}

// xfrmModeName is xfrm_id_info_print's mode word.
func xfrmModeName(mode uint8) string {
	names := [...]string{"transport", "tunnel", "ro", "in_trigger", "beet"}
	if int(mode) < len(names) {
		return names[mode]
	}
	return strconv.FormatUint(uint64(mode), 10)
}

// xfrmShareName is strxf_share.
func xfrmShareName(share uint8) string {
	names := [...]string{"any", "session", "user", "unique"}
	if int(share) < len(names) {
		return names[share]
	}
	return strconv.FormatUint(uint64(share), 10)
}

// xfrmMask8 is strxf_mask8: the byte's eight bits, high first.
func xfrmMask8(v uint8) string {
	return fmt.Sprintf("%08b", v)
}

// xfrmNoFamilyCst is rt_addr_n2a's answer for a family it cannot print.
const xfrmNoFamilyCst = "???"

// xfrmAddrN2a is rt_addr_n2a over an xfrm_address_t: inet_ntop for
// AF_INET and AF_INET6, "???" for any other family.
func xfrmAddrN2a(family uint16, a xtcpnl.XfrmAddress) string {
	switch family {
	case unix.AF_INET:
		return netip.AddrFrom4([4]byte(a[:4])).String()
	case unix.AF_INET6:
		return inet6Ntop(a)
	}
	return xfrmNoFamilyCst
}

// xfrmTime is strxf_time: "-" for zero, else localtime's
// "%Y-%m-%d %T".
func (o *XfrmOpt) xfrmTime(t uint64) string {
	if t == 0 {
		return "-"
	}
	zone := o.Zone
	if zone == nil {
		zone = time.Local
	}
	return time.Unix(int64(t), 0).In(zone).Format("2006-01-02 15:04:05")
}

// xfrmLimit is strxf_limit: "(INF)" for XFRM_INF, else the number.
func xfrmLimit(v uint64) string {
	if v == ^uint64(0) {
		return "(INF)"
	}
	return strconv.FormatUint(v, 10)
}

// xfrmFlags is XFRM_FLAG_PRINT over each named flag in order, each name
// followed by a space while bits remain, then what is left in hex.
func xfrmFlags(b *strings.Builder, flags uint32, names []xfrmFlagName) {
	for _, n := range names {
		if flags&n.bit == 0 {
			continue
		}
		flags &^= n.bit
		b.WriteString(n.name)
		if flags != 0 {
			b.WriteString(" ")
		}
	}
	if flags != 0 {
		b.WriteString(strconv.FormatUint(uint64(flags), 16))
	}
}

// xfrmFlagName is one XFRM_FLAG_PRINT.
type xfrmFlagName struct {
	bit  uint32
	name string
}

// xfrmStateFlags are xfrm_state_info_print's flags, XFRM_STATE_*.
var xfrmStateFlags = []xfrmFlagName{
	{1, "noecn"}, {2, "decap-dscp"}, {4, "nopmtudisc"}, {8, "wildrecv"}, {16, "icmp"}, {32, "af-unspec"},
	{64, "align4"}, {128, "esn"},
}

// xfrmExtraFlags are its extra flags, XFRM_SA_XFLAG_*.
var xfrmExtraFlags = []xfrmFlagName{{1, "dont-encap-dscp"}, {2, "oseq-may-wrap"}}

// xfrmIDInfo is xfrm_id_info_print: the title, the addresses, then a line
// of the protocol, the SPI when forced, set or under -s, the reqid and the
// mode.
func (o *XfrmOpt) xfrmIDInfo(b *strings.Builder, saddr xtcpnl.XfrmAddress, id xtcpnl.XfrmID, mode uint8,
	reqid uint32, family uint16, forceSPI bool, prefix, title string) {
	b.WriteString(title)
	b.WriteString("src " + xfrmAddrN2a(family, saddr) + " ")
	b.WriteString("dst " + xfrmAddrN2a(family, id.Daddr) + "\n")
	b.WriteString(prefix + "\t")
	b.WriteString("proto " + XfrmProtoName(id.Proto) + " ")
	if o.Stats || forceSPI || id.SPI != 0 {
		fmt.Fprintf(b, "spi 0x%08x", id.SPI)
		if o.Stats {
			fmt.Fprintf(b, "(%d)", id.SPI)
		}
		b.WriteString(" ")
	}
	fmt.Fprintf(b, "reqid %d", reqid)
	if o.Stats {
		fmt.Fprintf(b, "(0x%08x)", reqid)
	}
	b.WriteString(" mode " + xfrmModeName(mode) + "\n")
}

// XfrmStateInfo is xfrm_state_info_print with no prefix and no title: the
// SA's identity, its replay window and flags, its attributes, its selector
// when not all zero, under -s its lifetimes and statistics, and its
// security context.
func XfrmStateInfo(info *xtcpnl.XfrmUsersaInfo, a *xtcpnl.XfrmAttrs, o *XfrmOpt) string {
	var b strings.Builder
	o.xfrmIDInfo(&b, info.Saddr, info.ID, info.Mode, info.Reqid, info.Family, xfrmProtoIsIPsec(info.ID.Proto),
		"", "")
	const buf = "\t"
	b.WriteString(buf)
	fmt.Fprintf(&b, "replay-window %d ", info.ReplayWindow)
	if o.Stats {
		fmt.Fprintf(&b, "seq 0x%08d ", info.Seq)
	}
	if o.Stats || info.Flags != 0 {
		b.WriteString("flag ")
		xfrmFlags(&b, uint32(info.Flags), xfrmStateFlags)
	}
	if o.Stats && a.ExtraFlags != nil {
		b.WriteString("extra_flag ")
		xfrmFlags(&b, *a.ExtraFlags, xfrmExtraFlags)
	}
	if o.Stats {
		b.WriteString(" (0x" + xfrmMask8(info.Flags) + ")")
	}
	b.WriteString("\n")
	o.xfrmAttrs(&b, a, info.Family, buf, true)
	if info.Sel != (xtcpnl.XfrmSelector{}) {
		o.xfrmSelector(&b, &info.Sel, info.Family, buf+"sel ")
	}
	if o.Stats {
		o.xfrmLifetime(&b, &info.Lft, &info.Curlft, buf)
		b.WriteString(buf + "stats:\n")
		fmt.Fprintf(&b, "%s  replay-window %d replay %d failed %d\n", buf, info.Stats.ReplayWindow,
			info.Stats.Replay, info.Stats.IntegrityFailed)
	}
	if a.SecCtx != nil {
		xfrmSecCtx(&b, a.SecCtx)
	}
	return b.String()
}

// xfrmProtoIsIPsec is xfrm_xfrmproto_is_ipsec: ESP, AH or IPComp.
func xfrmProtoIsIPsec(proto uint8) bool {
	return proto == unix.IPPROTO_ESP || proto == unix.IPPROTO_AH || proto == unix.IPPROTO_COMP
}

// xfrmSecCtx is xfrm_sec_ctx_print.
func xfrmSecCtx(b *strings.Builder, c *xtcpnl.XfrmSecCtx) {
	b.WriteString("\tsecurity context " + string(c.Ctx) + " \n")
}

// xfrmLifetime is xfrm_lifetime_print of both structs.
func (o *XfrmOpt) xfrmLifetime(b *strings.Builder, cfg *xtcpnl.XfrmLifetimeCfg, cur *xtcpnl.XfrmLifetimeCur,
	prefix string) {
	b.WriteString(prefix + "lifetime config:\n")
	fmt.Fprintf(b, "%s  limit: soft %s(bytes), hard %s(bytes)\n", prefix, xfrmLimit(cfg.SoftByteLimit),
		xfrmLimit(cfg.HardByteLimit))
	fmt.Fprintf(b, "%s  limit: soft %s(packets), hard %s(packets)\n", prefix, xfrmLimit(cfg.SoftPacketLimit),
		xfrmLimit(cfg.HardPacketLimit))
	fmt.Fprintf(b, "%s  expire add: soft %d(sec), hard %d(sec)\n", prefix, cfg.SoftAddExpiresSeconds,
		cfg.HardAddExpiresSeconds)
	fmt.Fprintf(b, "%s  expire use: soft %d(sec), hard %d(sec)\n", prefix, cfg.SoftUseExpiresSeconds,
		cfg.HardUseExpiresSeconds)
	b.WriteString(prefix + "lifetime current:\n")
	fmt.Fprintf(b, "%s  %d(bytes), %d(packets)\n", prefix, cur.Bytes, cur.Packets)
	fmt.Fprintf(b, "%s  add %s use %s\n", prefix, o.xfrmTime(cur.AddTime), o.xfrmTime(cur.UseTime))
}

// xfrmSelector is xfrm_selector_print: the addresses in the selector's
// family, else the message's, else preferred_family, then the upper
// protocol by name and its ports, ICMP type and code, GRE key or MH type,
// the device and under -s the uid.
func (o *XfrmOpt) xfrmSelector(b *strings.Builder, sel *xtcpnl.XfrmSelector, family uint16, prefix string) {
	f := sel.Family
	if f == unix.AF_UNSPEC {
		f = family
	}
	if f == unix.AF_UNSPEC {
		f = uint16(o.Family)
	}
	b.WriteString(prefix)
	fmt.Fprintf(b, "src %s/%d ", xfrmAddrN2a(f, sel.Saddr), sel.PrefixlenS)
	fmt.Fprintf(b, "dst %s/%d ", xfrmAddrN2a(f, sel.Daddr), sel.PrefixlenD)
	if sel.Proto != 0 {
		b.WriteString("proto " + o.ProtoName(sel.Proto) + " ")
	}
	switch sel.Proto {
	case unix.IPPROTO_ICMP, unix.IPPROTO_ICMPV6:
		if sel.SportMask != 0 {
			fmt.Fprintf(b, "type %d ", sel.Sport)
		}
		if sel.DportMask != 0 {
			fmt.Fprintf(b, "code %d ", sel.Dport)
		}
	case unix.IPPROTO_GRE:
		if sel.SportMask != 0 || sel.DportMask != 0 {
			fmt.Fprintf(b, "key %d ", uint32(sel.Sport)<<16+uint32(sel.Dport))
		}
	case unix.IPPROTO_MH:
		if sel.SportMask != 0 {
			fmt.Fprintf(b, "type %d ", sel.Sport)
		}
		if sel.DportMask != 0 && o.Stats {
			// The raw, network-order u16.
			fmt.Fprintf(b, "(dport) 0x%04x ", bits.ReverseBytes16(sel.Dport))
		}
	default:
		if sel.SportMask != 0 {
			fmt.Fprintf(b, "sport %d ", sel.Sport)
		}
		if sel.DportMask != 0 {
			fmt.Fprintf(b, "dport %d ", sel.Dport)
		}
	}
	if sel.Ifindex > 0 {
		b.WriteString("dev " + o.IndexName(sel.Ifindex) + " ")
	}
	if o.Stats {
		fmt.Fprintf(b, "uid %d", sel.User)
	}
	b.WriteString("\n")
}

// xfrmAlgo is __xfrm_algo_print: the type, the name and the key, hidden,
// or in hex with its bits under -s, and, when newline is set, the end of
// the line. alg_key_len / 8 key bytes print.
func (o *XfrmOpt) xfrmAlgo(b *strings.Builder, typ uint16, a *xtcpnl.XfrmAlgo, prefix string, newline bool) {
	b.WriteString(prefix + xfrmAlgoTypeName(typ) + " " + a.Name + " ")
	keylen := min(int(a.KeyBits/8), len(a.Key))
	switch {
	case o.Nokeys:
		b.WriteString("<<Keys hidden>>")
	case keylen > 0:
		fmt.Fprintf(b, "0x%x", a.Key[:keylen])
		if o.Stats {
			fmt.Fprintf(b, " (%d bits)", a.KeyBits)
		}
	}
	if newline {
		b.WriteString("\n")
	}
}

// xfrmAttrs is xfrm_xfrma_print, in its order: the mark and output mark,
// the algorithms, the encapsulation, the templates, the care-of address,
// the last use, the replay state, the offload, the interface id, the TFC
// padding, the SA's direction and its per-CPU number. dir is whether the
// offload prints its direction, as an SA's does.
func (o *XfrmOpt) xfrmAttrs(b *strings.Builder, a *xtcpnl.XfrmAttrs, family uint16, prefix string, dir bool) {
	o.xfrmMarks(b, a)
	if a.AlgAuth != nil && a.AlgAuthTrunc == nil {
		o.xfrmAlgo(b, xtcpnl.XfrmAttrAlgAuth, a.AlgAuth, prefix, true)
	}
	if a.AlgAuthTrunc != nil {
		o.xfrmAlgo(b, xtcpnl.XfrmAttrAlgAuthTrunc, a.AlgAuthTrunc, prefix, false)
		fmt.Fprintf(b, " %d\n", int32(a.AlgAuthTrunc.ICVTruncBits))
	}
	if a.AlgAEAD != nil {
		o.xfrmAlgo(b, xtcpnl.XfrmAttrAlgAEAD, a.AlgAEAD, prefix, false)
		fmt.Fprintf(b, " %d\n", int32(a.AlgAEAD.ICVTruncBits))
	}
	if a.AlgCrypt != nil {
		o.xfrmAlgo(b, xtcpnl.XfrmAttrAlgCrypt, a.AlgCrypt, prefix, true)
	}
	if a.AlgComp != nil {
		o.xfrmAlgo(b, xtcpnl.XfrmAttrAlgComp, a.AlgComp, prefix, true)
	}
	if e := a.Encap; e != nil {
		fmt.Fprintf(b, "%sencap type %s sport %d dport %d addr %s\n", prefix, xfrmEncapName(e.Type), e.Sport,
			e.Dport, xfrmAddrN2a(family, e.OA))
	}
	for i := range a.Tmpl {
		o.xfrmTmpl(b, &a.Tmpl[i], prefix)
	}
	if a.CoAddr != nil {
		b.WriteString(prefix + "coa " + xfrmAddrN2a(family, *a.CoAddr) + "\n")
	}
	if a.LastUsed != nil {
		b.WriteString(prefix + "lastused " + o.xfrmTime(*a.LastUsed) + "\n")
	}
	if r := a.ReplayVal; r != nil {
		fmt.Fprintf(b, "%santi-replay context: seq 0x%x, oseq 0x%x, bitmap 0x%08x\n", prefix, r.Seq, r.OSeq,
			r.Bitmap)
	}
	if r := a.ReplayESNVal; r != nil {
		xfrmReplayESN(b, r, prefix)
	}
	o.xfrmTail(b, a, prefix, dir)
}

// xfrmMarks is xfrm_xfrma_print's mark line: the mark and the output mark
// with its mask, or the output mark alone.
func (o *XfrmOpt) xfrmMarks(b *strings.Builder, a *xtcpnl.XfrmAttrs) {
	switch {
	case a.Mark != nil:
		fmt.Fprintf(b, "\tmark %s/%s ", cHex0x(uint64(a.Mark.V)), cHex0x(uint64(a.Mark.M)))
		xfrmOutputMark(b, a)
		b.WriteString("\n")
	case a.SetMark != nil:
		b.WriteString("\t")
		xfrmOutputMark(b, a)
		b.WriteString("\n")
	}
}

// xfrmOutputMark is xfrm_output_mark_print, nothing when absent.
func xfrmOutputMark(b *strings.Builder, a *xtcpnl.XfrmAttrs) {
	if a.SetMark == nil {
		return
	}
	fmt.Fprintf(b, "output-mark 0x%x", *a.SetMark)
	if a.SetMarkMask != nil {
		fmt.Fprintf(b, "/0x%x", *a.SetMarkMask)
	}
}

// cHex0x is printf's %#x: 0 for zero, else 0x and the hex.
func cHex0x(v uint64) string {
	if v == 0 {
		return "0"
	}
	return "0x" + strconv.FormatUint(v, 16)
}

// xfrmReplayESN is the anti-replay ESN block: the sequence numbers, the
// window and bitmap length, then the bitmap from its last word, eight to a
// line.
func xfrmReplayESN(b *strings.Builder, r *xtcpnl.XfrmReplayStateESN, prefix string) {
	b.WriteString(prefix + "anti-replay esn context:\n")
	fmt.Fprintf(b, "%s seq-hi 0x%x, seq 0x%x, oseq-hi 0x%x, oseq 0x%x\n", prefix, r.SeqHi, r.Seq, r.OSeqHi, r.OSeq)
	fmt.Fprintf(b, "%s replay_window %d, bitmap-length %d", prefix, r.ReplayWindow, len(r.Bmp))
	for i, j := len(r.Bmp), 0; i > 0; i-- {
		if j%8 == 0 {
			b.WriteString("\n" + prefix + " ")
		}
		j++
		fmt.Fprintf(b, "%08x ", r.Bmp[i-1])
	}
	b.WriteString("\n")
}

// xfrmTail is xfrm_xfrma_print's last attributes: the offload, the
// interface id, the TFC padding, the SA's direction and its per-CPU
// number.
func (o *XfrmOpt) xfrmTail(b *strings.Builder, a *xtcpnl.XfrmAttrs, prefix string, dir bool) {
	if x := a.OffloadDev; x != nil {
		b.WriteString(prefix + "crypto offload parameters: dev " + o.IndexName(x.Ifindex) + " ")
		if dir {
			d := "out"
			if x.Flags&xfrmOffloadInboundCst != 0 {
				d = "in"
			}
			b.WriteString("dir " + d + " ")
		}
		m := "crypto"
		if x.Flags&xfrmOffloadPacketCst != 0 {
			m = "packet"
		}
		b.WriteString("mode " + m + "\n")
	}
	if a.IfID != nil {
		b.WriteString(prefix + "if_id " + cHex0x(uint64(*a.IfID)) + "\n")
	}
	if a.TFCPad != nil {
		fmt.Fprintf(b, "%stfcpad %d\n", prefix, *a.TFCPad)
	}
	if a.SADir != nil {
		switch *a.SADir {
		case xfrmSADirInCst:
			b.WriteString("\tdir in\n")
		case xfrmSADirOutCst:
			b.WriteString("\tdir out\n")
		default:
			fmt.Fprintf(b, "\tdir other (%d)\n", *a.SADir)
		}
	}
	if a.SAPcpu != nil {
		fmt.Fprintf(b, "\tpcpu-num %d\n", *a.SAPcpu)
	}
}

// The offload flags and SA directions xfrm_xfrma_print reads:
// XFRM_OFFLOAD_INBOUND and _PACKET, XFRM_SA_DIR_IN and _OUT.
const (
	xfrmOffloadInboundCst = 2
	xfrmOffloadPacketCst  = 4
	xfrmSADirInCst        = 1
	xfrmSADirOutCst       = 2
)

// xfrmEncapName is xfrm_xfrma_print's encapsulation type.
func xfrmEncapName(t uint16) string {
	switch t {
	case 1:
		return "espinudp-nonike"
	case 2:
		return "espinudp"
	case 7:
		return "espintcp"
	}
	return strconv.FormatUint(uint64(t), 10)
}

// xfrmTmpl is xfrm_tmpl_print for one template: its identity under
// "tmpl ", its level and share when it has them or under -s, and under -s
// its algorithm masks.
func (o *XfrmOpt) xfrmTmpl(b *strings.Builder, t *xtcpnl.XfrmUserTmpl, prefix string) {
	b.WriteString(prefix)
	o.xfrmIDInfo(b, t.Saddr, t.ID, t.Mode, t.Reqid, t.Family, false, prefix, "tmpl ")
	if o.Stats || t.Optional != 0 {
		b.WriteString(prefix + "\t")
		switch t.Optional {
		case 0:
			b.WriteString("level required ")
		case 1:
			b.WriteString("level use ")
		default:
			fmt.Fprintf(b, "level %d ", t.Optional)
		}
		if o.Stats {
			b.WriteString("share " + xfrmShareName(t.Share) + " ")
		}
		b.WriteString("\n")
	}
	if o.Stats {
		fmt.Fprintf(b, "%s\tenc-mask %08x auth-mask %08x comp-mask %08x\n", prefix, t.EAlgos, t.AAlgos, t.CAlgos)
	}
}

// XfrmSADInfo is print_sadinfo: the SA count and, under -s, the hash
// table's buckets, or for an answer with no count "BAD SAD info returned",
// which ip prints to stdout and exits 0 after.
func XfrmSADInfo(info *xtcpnl.XfrmInfo, stats bool) string {
	if info.SADCnt == nil {
		return "BAD SAD info returned\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\t SAD count %d", *info.SADCnt)
	if stats && info.SADHInfo != nil {
		fmt.Fprintf(&b, " (buckets count %d Max %d)", int32(info.SADHInfo.Count), int32(info.SADHInfo.MaxCount))
	}
	b.WriteString("\n")
	return b.String()
}
