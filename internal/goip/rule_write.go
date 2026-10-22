package goip

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip rule add` and `ip rule delete` are iprule_modify (ip/iprule.c:962-1234):
// the argument loop over a fib_rule_hdr and its attributes, then one
// RTM_NEWRULE or RTM_DELRULE talked to the kernel with NLM_F_ACK. Nothing is
// resolved first: a device is sent by name, so `ip` sends the write alone
// (link-write/rtnl/rule_*). goip writes the request to GOIP_WRITE_OUT.

const (
	// ruleModifyBufCst is iprule_modify's `char buf[1024]` after the header.
	ruleModifyBufCst = 1024
	// rulePortMaxMaskCst and ruleDscpMaxMaskCst are PORT_MAX_MASK and
	// DSCP_MAX_MASK: a mask equal to them is not sent.
	rulePortMaxMaskCst = 0xFFFF
	ruleDscpMaxMaskCst = 0x3F
	// ruleLabelMaxMaskCst is LABEL_MAX_MASK, the flowlabel's 20 bits.
	ruleLabelMaxMaskCst = 0xFFFFF
	ruleAddVerbCst      = "add"
	ruleDeleteVerbCst   = "delete"
	ruleNotKeywordCst   = "not"
	ruleTableKeywordCst = "table"
	ruleFromKeywordCst  = "from"
	ruleTosKeywordCst   = "tos"
)

// ruleProtoNames is the getprotobyname subset goip knows for `ipproto`: the
// IANA names /etc/protocols carries for the protocols the kernel's
// FRA_IP_PROTO selector is used with. goip reads no config files, so any
// other name is refused rather than looked up.
var ruleProtoNames = map[string]uint8{
	"icmp": unix.IPPROTO_ICMP, "tcp": unix.IPPROTO_TCP, "udp": unix.IPPROTO_UDP,
	"ipv6-icmp": unix.IPPROTO_ICMPV6, "sctp": unix.IPPROTO_SCTP, "dccp": unix.IPPROTO_DCCP,
	"udplite": unix.IPPROTO_UDPLITE, "gre": unix.IPPROTO_GRE, "esp": unix.IPPROTO_ESP,
	"ah": unix.IPPROTO_AH,
}

// ruleWrite is iprule_modify's request: the fib_rule_hdr and the attributes
// in the order the loop adds them.
type ruleWrite struct {
	family  uint8
	dstLen  uint8
	srcLen  uint8
	tos     uint8
	table   uint8
	action  uint8
	flags   uint32
	attrs   xtcpnl.AttrBuilder
	tid     uint32
	tableOK bool
	l3mdev  bool
}

// frh is struct fib_rule_hdr: family, dst_len, src_len, tos, table, two
// reserved bytes, action and flags.
func (w *ruleWrite) frh() []byte {
	b := make([]byte, xtcpnl.FibRuleHdrSizeCst)
	b[0], b[1], b[2], b[3], b[4], b[7] = w.family, w.dstLen, w.srcLen, w.tos, w.table, w.action
	binary.LittleEndian.PutUint32(b[8:12], w.flags)
	return b
}

// ruleModify runs `ip rule add` or `ip rule delete`: the argument loop, the
// checks after it, then the request to GOIP_WRITE_OUT.
func ruleModify(c *runCtx, msgType uint16, args []string) error {
	w, err := parseRuleWriteArgs(c.family, msgType, args)
	if err != nil {
		return err
	}
	flags := uint16(unix.NLM_F_ACK)
	if msgType == unix.RTM_NEWRULE {
		flags |= unix.NLM_F_CREATE | unix.NLM_F_EXCL
	}
	b, err := xtcpnl.BuildWriteRequest(msgType, flags, c.nextSeq(), w.frh(), w.attrs.Bytes())
	if err != nil {
		return fmt.Errorf("goip: build rule request: %w", err)
	}
	return writeAddrOut(b)
}

// parseRuleWriteArgs is iprule_modify's argument loop and the checks after
// it. family is preferred_family, which the header keeps whatever family an
// address has, and which becomes AF_INET at the end if it is still unset.
func parseRuleWriteArgs(family uint8, msgType uint16, args []string) (*ruleWrite, error) {
	var raw [ruleModifyBufCst]byte
	w := &ruleWrite{family: family, action: unix.FR_ACT_UNSPEC, attrs: xtcpnl.NewAttrBuilder(raw[:])}
	verb := ruleDeleteVerbCst
	if msgType == unix.RTM_NEWRULE {
		verb = ruleAddVerbCst
		w.action = unix.FR_ACT_TO_TBL
	}
	if len(args) == 0 {
		// iproute2 ends the line with a period; Go error strings do not.
		return nil, fmt.Errorf("%w: \"ip rule %s\" requires arguments", ErrInvalidArgument, verb[:3])
	}
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = w.parseArg(args, i); err != nil {
			return nil, err
		}
	}
	if w.l3mdev && w.tid != 0 {
		return nil, fmt.Errorf("%w: table can not be specified for l3mdev rules", ErrInvalidArgument)
	}
	if w.family == unix.AF_UNSPEC {
		w.family = unix.AF_INET
	}
	if !w.tableOK && msgType == unix.RTM_NEWRULE {
		w.table = unix.RT_TABLE_MAIN
	}
	return w, nil
}

// parseArg is one turn of the loop, in its order: `not`, `from` and `to` by
// strcmp; the preference group, `tos`/`dsfield`, `realms`, `protocol`,
// `tun_id` and `table` by matches(), so `t` is tun_id and `p` preference;
// the rest as iprule_modify spells them. It returns the index of the last
// argument it used.
func (w *ruleWrite) parseArg(args []string, i int) (int, error) {
	a := args[i]
	switch {
	case a == ruleNotKeywordCst:
		w.flags |= unix.FIB_RULE_INVERT
		return i, nil
	case a == ruleFromKeywordCst, a == routeToKeywordCst:
		return w.prefix(args, i)
	case matchesPrefix(a, "preference"), matchesPrefix(a, "order"), matchesPrefix(a, "priority"):
		return w.u32Attr(args, i, unix.FRA_PRIORITY, "preference value is invalid\n")
	case a == ruleTosKeywordCst, matchesPrefix(a, "dsfield"):
		return w.tosArg(args, i)
	case a == "fwmark":
		return w.fwmark(args, i)
	case matchesPrefix(a, "realms"):
		return i, fmt.Errorf("rule realms: iproute2 reads realm names from rt_realms and goip reads "+
			"no config files: %w", ErrNotImplemented)
	case matchesPrefix(a, "protocol"):
		return w.protocol(args, i)
	case matchesPrefix(a, "tun_id"):
		return w.tunID(args, i)
	case matchesPrefix(a, ruleTableKeywordCst), a == "lookup":
		return w.tableArg(args, i)
	case matchesPrefix(a, "suppress_prefixlength"), a == "sup_pl":
		return w.suppressPrefixlen(args, i)
	case matchesPrefix(a, "suppress_ifgroup"), a == "sup_group":
		return w.suppressIfgroup(args, i)
	case a == devKeywordCst, a == "iif":
		return w.ifname(args, i, unix.FRA_IIFNAME, "\"iif\"/\"dev\" not a valid ifname")
	case a == "oif":
		return w.ifname(args, i, unix.FRA_OIFNAME, "\"oif\" not a valid ifname")
	case a == "l3mdev":
		w.tableOK, w.l3mdev = true, true
		return i, w.attrs.PutU8(unix.FRA_L3MDEV, 1)
	case a == "uidrange":
		return w.uidrange(args, i)
	case a == "nat", matchesPrefix(a, "map-to"):
		return i, fmt.Errorf("rule nat: route NAT is deprecated and goip does not build it: %w", ErrNotImplemented)
	}
	return w.parseSelectorArg(args, i)
}

// parseSelectorArg is the loop's arms after `nat`: the transport selectors,
// then the type arm.
func (w *ruleWrite) parseSelectorArg(args []string, i int) (int, error) {
	switch args[i] {
	case "ipproto":
		return w.ipproto(args, i)
	case "sport":
		return w.portRange(args, i, unix.FRA_SPORT_RANGE, xtcpnl.FraSportMask)
	case "dport":
		return w.portRange(args, i, unix.FRA_DPORT_RANGE, xtcpnl.FraDportMask)
	case "dscp":
		return w.dscp(args, i)
	case "flowlabel":
		return w.flowlabel(args, i)
	}
	return w.ruleType(args, i)
}

// prefix is `from` and `to`: get_prefix in the header's family, which exits
// on a bad prefix, the length into the header and the address as FRA_SRC or
// FRA_DST.
func (w *ruleWrite) prefix(args []string, i int) (int, error) {
	from := args[i] == ruleFromKeywordCst
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	p, err := routeGetPrefix(args[i], w.family)
	if err != nil {
		return i, err
	}
	bitlen := bitlenU8(p.bitlen)
	if from {
		w.srcLen = bitlen
		return i, w.attrs.PutBytes(unix.FRA_SRC, p.data)
	}
	w.dstLen = bitlen
	return i, w.attrs.PutBytes(unix.FRA_DST, p.data)
}

// u32Attr is get_u32 base 0 into a u32 attribute.
func (w *ruleWrite) u32Attr(args []string, i int, typ uint16, msg string) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v, ok := getU32(args[i], 0)
	if !ok {
		return i, invargErr(msg, args[i])
	}
	return i, w.attrs.PutU32(typ, v)
}

// tosArg is rtnl_dsfield_a2n into the header's tos.
func (w *ruleWrite) tosArg(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v, ok := rtnlDsfieldA2N(args[i])
	if !ok {
		return i, invargErr("TOS value is invalid\n", args[i])
	}
	w.tos = v
	return i, nil
}

// fwmark is FRA_FWMARK and, after a slash, FRA_FWMASK, each get_u32 base 0.
func (w *ruleWrite) fwmark(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	mark, mask, slash := strings.Cut(args[i], "/")
	v, ok := getU32(mark, 0)
	if !ok {
		return i, invargErr("fwmark value is invalid\n", mark)
	}
	if err = w.attrs.PutU32(unix.FRA_FWMARK, v); err != nil || !slash {
		return i, err
	}
	m, ok := getU32(mask, 0)
	if !ok {
		return i, invargErr("fwmask value is invalid\n", mask)
	}
	return i, w.attrs.PutU32(unix.FRA_FWMASK, m)
}

// protocol is rtnl_rtprot_a2n, FRA_PROTOCOL its low byte.
func (w *ruleWrite) protocol(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	p, ok := rtnlNameA2N(routeProtoNames, args[i], math.MaxUint32)
	if !ok {
		return i, invargErr(`"protocol" value is invalid`+"\n", args[i])
	}
	return i, w.attrs.PutBytes(unix.FRA_PROTOCOL, binary.LittleEndian.AppendUint32(nil, p)[:1])
}

// tunID is get_be64 base 0, FRA_TUN_ID in network order.
func (w *ruleWrite) tunID(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v, ok := cGetUnsigned(args[i], 0, math.MaxUint64)
	if !ok {
		return i, invargErr(`"tun_id" value is invalid`+"\n", args[i])
	}
	return i, w.attrs.PutBytes(unix.FRA_TUN_ID, binary.BigEndian.AppendUint64(nil, v))
}

// tableArg is rtnl_rttable_a2n: a table under 256 in the header, any other
// in FRA_TABLE with the header's RT_TABLE_UNSPEC.
func (w *ruleWrite) tableArg(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	tid, ok := rtnlNameA2N(routeTableNames, args[i], math.MaxUint32)
	if !ok {
		return i, invargErr("invalid table ID\n", args[i])
	}
	w.tid, w.tableOK = tid, true
	if tid < 256 {
		w.table = uint8(tid)
		return i, nil
	}
	w.table = unix.RT_TABLE_UNSPEC
	return i, w.attrs.PutU32(unix.FRA_TABLE, tid)
}

// suppressPrefixlen is get_s32 base 0, refused when negative.
func (w *ruleWrite) suppressPrefixlen(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	pl, ok := getS32(args[i], 0)
	if !ok || pl < 0 {
		return i, invargErr("suppress_prefixlength value is invalid\n", args[i])
	}
	return i, w.attrs.PutU32(unix.FRA_SUPPRESS_PREFIXLEN, uint32(pl))
}

// suppressIfgroup is rtnl_group_a2n: `default`, the one name the shipped
// group file and the built-in table hold, or a number base 0.
func (w *ruleWrite) suppressIfgroup(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	g, ok := rtnlNameA2N(map[string]uint32{addrDefaultGroupCst: 0}, args[i], math.MaxUint32)
	if !ok {
		return i, invargErr(`Invalid "suppress_ifgroup" value`+"\n", args[i])
	}
	return i, w.attrs.PutU32(unix.FRA_SUPPRESS_IFGROUP, g)
}

// ifname is check_ifname, then the name with its NUL.
func (w *ruleWrite) ifname(args []string, i int, typ uint16, msg string) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	if !checkIfname(args[i]) {
		return i, invargErr(msg, args[i])
	}
	return i, w.attrs.PutBytes(typ, append([]byte(args[i]), 0))
}

// uidrange is sscanf "%u-%u" into struct fib_rule_uid_range.
func (w *ruleWrite) uidrange(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	start, end, ok := scanUintPair(args[i])
	if !ok {
		return i, invargErr("invalid UID range\n", args[i])
	}
	b := binary.LittleEndian.AppendUint32(nil, start)
	return i, w.attrs.PutBytes(unix.FRA_UID_RANGE, binary.LittleEndian.AppendUint32(b, end))
}

// ipproto is inet_proto_a2n: get_u8 base 10, then the protocol's name.
func (w *ruleWrite) ipproto(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	p, ok := getU8(args[i], 10)
	if !ok {
		if p, ok = ruleProtoNames[args[i]]; !ok {
			return i, fmt.Errorf("rule ipproto %q: iproute2 looks the name up in /etc/protocols and goip "+
				"reads no config files: %w", args[i], ErrNotImplemented)
		}
	}
	return i, w.attrs.PutU8(unix.FRA_IP_PROTO, p)
}

// portRange is iprule_port_parse: START-END, or a port with an optional
// /MASK, each get_u16 base 0, into struct fib_rule_port_range and, when the
// mask is not PORT_MAX_MASK, a u16 mask attribute.
func (w *ruleWrite) portRange(args []string, i int, typ, maskTyp uint16) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	start, end, mask, err := parsePortRange(args[i])
	if err != nil {
		return i, err
	}
	b := binary.LittleEndian.AppendUint16(nil, start)
	if err = w.attrs.PutBytes(typ, binary.LittleEndian.AppendUint16(b, end)); err != nil || mask == rulePortMaxMaskCst {
		return i, err
	}
	return i, w.attrs.PutBytes(maskTyp, binary.LittleEndian.AppendUint16(nil, mask))
}

// parsePortRange is iprule_port_parse's three forms. A range takes no mask.
func parsePortRange(arg string) (start, end, mask uint16, err error) {
	mask = rulePortMaxMaskCst
	if lo, hi, ok := strings.Cut(arg, "-"); ok {
		var good bool
		if start, good = getU16(lo, 0); !good {
			return 0, 0, 0, invargErr("invalid port range start", lo)
		}
		if end, good = getU16(hi, 0); !good {
			return 0, 0, 0, invargErr("invalid port range end", hi)
		}
		return start, end, mask, nil
	}
	port, m, slash := strings.Cut(arg, "/")
	if slash {
		var good bool
		if mask, good = getU16(m, 0); !good {
			return 0, 0, 0, invargErr("invalid mask", m)
		}
	}
	start, good := getU16(port, 0)
	if !good {
		return 0, 0, 0, invargErr("invalid port", port)
	}
	return start, start, mask, nil
}

// dscp is iprule_dscp_parse: rtnl_dscp_a2n, then a get_u32 mask after a
// slash, FRA_DSCP and, unless it is DSCP_MAX_MASK, FRA_DSCP_MASK, each u8.
func (w *ruleWrite) dscp(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	val, m, slash := strings.Cut(args[i], "/")
	d, ok := rtnlDscpA2N(val)
	if !ok {
		return i, invargErr("invalid dscp", val)
	}
	mask := uint32(ruleDscpMaxMaskCst)
	if slash {
		if mask, ok = getU32(m, 0); !ok {
			return i, invargErr("invalid dscp mask", m)
		}
	}
	// addattr8 of each u32: its low byte.
	if err = w.attrs.PutBytes(xtcpnl.FraDscp, binary.LittleEndian.AppendUint32(nil, d)[:1]); err != nil ||
		mask == ruleDscpMaxMaskCst {
		return i, err
	}
	return i, w.attrs.PutBytes(xtcpnl.FraDscpMask, binary.LittleEndian.AppendUint32(nil, mask)[:1])
}

// flowlabel is iprule_flowlabel_parse: get_u32 base 0 and an optional mask,
// LABEL_MAX_MASK without one, both sent in network order.
func (w *ruleWrite) flowlabel(args []string, i int) (int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	val, m, slash := strings.Cut(args[i], "/")
	label, ok := getU32(val, 0)
	if !ok {
		return i, invargErr("invalid flowlabel", val)
	}
	mask := uint32(ruleLabelMaxMaskCst)
	if slash {
		if mask, ok = getU32(m, 0); !ok {
			return i, invargErr("invalid flowlabel mask", m)
		}
	}
	if err = w.attrs.PutBytes(xtcpnl.FraFlowlabel, binary.BigEndian.AppendUint32(nil, label)); err != nil {
		return i, err
	}
	return i, w.attrs.PutBytes(xtcpnl.FraFlowlabelMask, binary.BigEndian.AppendUint32(nil, mask))
}

// ruleType is the loop's else arm: an optional `type`, then `help`, `goto`
// and `nop` by matches(), or a route type by rtnl_rtntype_a2n, whose RTN_*
// values the FR_ACT_* ones share.
func (w *ruleWrite) ruleType(args []string, i int) (int, error) {
	var err error
	if args[i] == linkXstatsTypeCst {
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	a := args[i]
	switch {
	case matchesPrefix(a, routeHelpKeywordCst):
		return i, fmt.Errorf("%w: ip rule help is not implemented by goip", ErrNotImplemented)
	case matchesPrefix(a, "goto"):
		w.action = unix.FR_ACT_GOTO
		if i, err = w.u32Attr(args, i, unix.FRA_GOTO, "invalid target\n"); err != nil {
			return i, err
		}
	case matchesPrefix(a, "nop"):
		w.action = unix.FR_ACT_NOP
	default:
		t, ok := rtnlRtntypeA2N(a)
		if !ok {
			return i, invargErr("Failed to parse rule type", a)
		}
		w.action = t
	}
	w.tableOK = true
	return i, nil
}

// rtnlDsfieldA2N is rtnl_dsfield_a2n: a name from the rt_dsfield file
// iproute2 ships, then strtoul base 16 over the whole argument, at most 255.
func rtnlDsfieldA2N(arg string) (uint8, bool) {
	if v, ok := dsfieldNames[arg]; ok {
		return v, true
	}
	r := cStrtoul(arg, 16)
	if r.end == 0 || r.end != len(arg) || r.val > math.MaxUint8 {
		return 0, false
	}
	return uint8(r.val), true
}

// rtnlDscpA2N is rtnl_dscp_a2n: get_u32 base 0, or a DS field name or hex
// value shifted down to its DSCP.
func rtnlDscpA2N(arg string) (uint32, bool) {
	if v, ok := getU32(arg, 0); ok {
		return v, true
	}
	ds, ok := rtnlDsfieldA2N(arg)
	return uint32(ds) >> 2, ok
}

// dsfieldNames is etc/iproute2/rt_dsfield as iproute2-7.1.0 ships it.
var dsfieldNames = map[string]uint8{
	addrDefaultGroupCst: 0x0,
	"AF11":              0x28, "AF12": 0x30, "AF13": 0x38,
	"AF21": 0x48, "AF22": 0x50, "AF23": 0x58,
	"AF31": 0x68, "AF32": 0x70, "AF33": 0x78,
	"AF41": 0x88, "AF42": 0x90, "AF43": 0x98,
	"CS1": 0x20, "CS2": 0x40, "CS3": 0x60, "CS4": 0x80,
	"CS5": 0xA0, "CS6": 0xC0, "CS7": 0xE0,
	"EF": 0xB8,
}

// scanUintPair is sscanf(arg, "%u-%u", ...) == 2: each %u is strtoul base
// 10 after any leading space, a negative value wrapped to 32 bits, and
// anything after the second number is ignored.
func scanUintPair(arg string) (a, b uint32, ok bool) {
	r := cStrtoul(arg, 10)
	if r.end == 0 || r.end >= len(arg) || arg[r.end] != '-' {
		return 0, 0, false
	}
	r2 := cStrtoul(arg[r.end+1:], 10)
	if r2.end == 0 {
		return 0, 0, false
	}
	return lowU32(r.val), lowU32(r2.val), true
}

// lowU32 is a 64-bit strtoul result stored into an unsigned int: its low 32
// bits.
func lowU32(v uint64) uint32 {
	return binary.LittleEndian.Uint32(binary.LittleEndian.AppendUint64(nil, v))
}
