package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip xfrm` is do_xfrm (ip/ipxfrm.c): usage with no argument, then the
// object by prefix, state (or sa), policy or monitor, and usage for help or
// anything else. `ip xfrm state` is do_xfrm_state (ip/xfrm_state.c): with
// no verb a list, else the verb by prefix, add, update, allocspi, delete,
// deleteall (or delall), list (show, lst), get, flush or count.
//
// goip runs the state reads, each on its own NETLINK_XFRM socket as ip's
// do: list, a GETSA dump each answer of which is printed, filtered on
// goip's side as ip's is; get, one GETSA; and count, one GETSADINFO. The
// state writes are xfrm_state_write.go's, the policy reads
// obj_xfrm_policy.go's; the policy writes are G7-T04's, and the monitor
// G7-T05's.

const (
	xfrmHelpCst    = "help"
	xfrmSrcCst     = "src"
	xfrmDstCst     = "dst"
	xfrmProtoCst   = "proto"
	xfrmSpiCst     = "spi"
	xfrmModeCst    = "mode"
	xfrmReqidCst   = "reqid"
	xfrmFlagCst    = "flag"
	xfrmMarkCst    = "mark"
	xfrmNokeysCst  = "nokeys"
	xfrmMaskAllCst = 0xffffffff
	// xfrmAddrFilterLenCst is struct xfrm_address_filter's 36 bytes.
	xfrmAddrFilterLenCst = 36
	// xfrmUsageCst is ip/ipxfrm.c's usage().
	xfrmUsageCst = "Usage: ip xfrm XFRM-OBJECT { COMMAND | help }\n" +
		"where  XFRM-OBJECT := state | policy | monitor"
	// xfrmStateUsageCst is ip/xfrm_state.c's usage().
	xfrmStateUsageCst = "Usage: ip xfrm state { add | update } ID [ ALGO-LIST ] [ mode MODE ]\n" +
		"        [ mark MARK [ mask MASK ] ] [ reqid REQID ] [ dir DIR ] [ seq SEQ ]\n" +
		"        [ replay-window SIZE ] [ replay-seq SEQ ] [ replay-oseq SEQ ]\n" +
		"        [ replay-seq-hi SEQ ] [ replay-oseq-hi SEQ ]\n" +
		"        [ flag FLAG-LIST ] [ sel SELECTOR ] [ LIMIT-LIST ] [ encap ENCAP ]\n" +
		"        [ coa ADDR[/PLEN] ] [ ctx CTX ] [ extra-flag EXTRA-FLAG-LIST ]\n" +
		"        [ offload [ crypto | packet ] dev DEV dir DIR ]\n" +
		"        [ output-mark OUTPUT-MARK [ mask MASK ] ]\n" +
		"        [ if_id IF_ID ] [ tfcpad LENGTH ] [ pcpu-num CPUNUM ]\n" +
		"Usage: ip xfrm state allocspi ID [ mode MODE ] [ mark MARK [ mask MASK ] ]\n" +
		"        [ reqid REQID ] [ dir DIR ] [ seq SEQ ] [ min SPI max SPI ] [ pcpu-num CPUNUM ]\n" +
		"Usage: ip xfrm state { delete | get } ID [ mark MARK [ mask MASK ] ]\n" +
		"Usage: ip xfrm state deleteall [ ID ] [ mode MODE ] [ reqid REQID ]\n" +
		"        [ flag FLAG-LIST ]\n" +
		"Usage: ip xfrm state list [ nokeys ] [ ID ] [ mode MODE ] [ reqid REQID ]\n" +
		"        [ flag FLAG-LIST ]\n" +
		"Usage: ip xfrm state flush [ proto XFRM-PROTO ]\n" +
		"Usage: ip xfrm state count\n" +
		"ID := [ src ADDR ] [ dst ADDR ] [ proto XFRM-PROTO ] [ spi SPI ]\n" +
		"XFRM-PROTO := esp | ah | comp | route2 | hao\n" +
		"ALGO-LIST := [ ALGO-LIST ] ALGO\n" +
		"ALGO := { enc | auth } ALGO-NAME ALGO-KEYMAT |\n" +
		"        auth-trunc ALGO-NAME ALGO-KEYMAT ALGO-TRUNC-LEN |\n" +
		"        aead ALGO-NAME ALGO-KEYMAT ALGO-ICV-LEN |\n" +
		"        comp ALGO-NAME\n" +
		"MODE := transport | tunnel | beet | ro | in_trigger\n" +
		"FLAG-LIST := [ FLAG-LIST ] FLAG\n" +
		"FLAG := noecn | decap-dscp | nopmtudisc | wildrecv | icmp | af-unspec | align4 | esn\n" +
		"EXTRA-FLAG-LIST := [ EXTRA-FLAG-LIST ] EXTRA-FLAG\n" +
		"EXTRA-FLAG := dont-encap-dscp | oseq-may-wrap\n" +
		"SELECTOR := [ src ADDR[/PLEN] ] [ dst ADDR[/PLEN] ] [ dev DEV ] [ UPSPEC ]\n" +
		"UPSPEC := proto { { tcp | udp | sctp | dccp } [ sport PORT ] [ dport PORT ] |\n" +
		"                  { icmp | ipv6-icmp | mobility-header } [ type NUMBER ] [ code NUMBER ] |\n" +
		"                  gre [ key { DOTTED-QUAD | NUMBER } ] | PROTO }\n" +
		"LIMIT-LIST := [ LIMIT-LIST ] limit LIMIT\n" +
		"LIMIT := { time-soft | time-hard | time-use-soft | time-use-hard } SECONDS |\n" +
		"         { byte-soft | byte-hard } SIZE | { packet-soft | packet-hard } COUNT\n" +
		"ENCAP := { espinudp | espinudp-nonike | espintcp } SPORT DPORT OADDR\n" +
		"DIR := in | out"
)

var (
	// ErrXfrmUsage is a refusal ip/xfrm*.c ends with exit(-1), 255: usage,
	// an unknown command or word, invarg and missarg.
	ErrXfrmUsage = errors.New("xfrm")
	// ErrXfrmRefused is one it ends with exit(1) itself: an SPI the protocol
	// cannot take, or an answer it cannot print ("An error :-)").
	ErrXfrmRefused = errors.New("xfrm refused")
	// ErrXfrmDumpTerminated is a dump the kernel refuses or an answer the
	// printer refuses: "Dump terminated", exit(1).
	ErrXfrmDumpTerminated = errors.New("dump terminated")
)

// xfrmUsage marks err, an invarg, missarg or incomplete_command, as one
// of ErrXfrmUsage's refusals.
func xfrmUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrXfrmUsage, err)
}

// runXfrm is do_xfrm.
func runXfrm(c *runCtx, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: %s", ErrXfrmUsage, xfrmUsageCst)
	}
	switch obj := args[0]; {
	case matchesPrefix(obj, "state"), matchesPrefix(obj, "sa"):
		return xfrmState(c, args[1:])
	case matchesPrefix(obj, "policy"):
		return xfrmPolicy(c, args[1:])
	case matchesPrefix(obj, "monitor"):
		return fmt.Errorf("xfrm monitor, G7-T05: %w", ErrNotImplemented)
	}
	return fmt.Errorf("%w: %s", ErrXfrmUsage, xfrmUsageCst)
}

// xfrmStateWrites are do_xfrm_state's write verbs, matched by prefix in
// its order ahead of list, get, flush and count; delete is ahead of
// deleteall, so `d` is delete.
var xfrmStateWrites = []struct {
	verb string
	run  func(*runCtx, []string) error
}{
	{xfrmAddCst, func(c *runCtx, a []string) error { return xfrmStateModify(c, xtcpnl.XfrmMsgNewSA, a) }},
	{xfrmUpdateCst, func(c *runCtx, a []string) error { return xfrmStateModify(c, xtcpnl.XfrmMsgUpdSA, a) }},
	{xfrmAllocspiCst, xfrmStateAllocspi},
	{fouDeleteCst, xfrmStateDelete},
	{xfrmDeleteallCst, xfrmStateDeleteall},
	{"delall", xfrmStateDeleteall},
}

// xfrmState is do_xfrm_state. goip renders none of its show_stats > 1
// forms (count's buckets, flush's "Flush state" line), so `-s -s` is
// refused.
func xfrmState(c *runCtx, args []string) error {
	if c.showStats > 1 {
		return fmt.Errorf("-s -s xfrm state: %w", ErrNotImplemented)
	}
	if len(args) == 0 {
		return xfrmStateList(c, nil)
	}
	verb, rest := args[0], args[1:]
	for _, w := range xfrmStateWrites {
		if matchesPrefix(verb, w.verb) {
			return w.run(c, rest)
		}
	}
	switch {
	case matchesAny(verb, []string{"list", "show", "lst"}):
		return xfrmStateList(c, rest)
	case matchesPrefix(verb, "get"):
		return xfrmStateGet(c, rest)
	case matchesPrefix(verb, xfrmFlushCst):
		return xfrmStateFlush(c, rest)
	case matchesPrefix(verb, "count"):
		return xfrmStateCount(c)
	case matchesPrefix(verb, xfrmHelpCst):
		return fmt.Errorf("%w: %s", ErrXfrmUsage, xfrmStateUsageCst)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip xfrm state help\"", ErrXfrmUsage, verb)
}

// xfrmFilter is struct xfrm_filter's SA half: the use flag, the SA the
// arguments describe, and a mask per field, the address masks a prefix
// length.
type xfrmFilter struct {
	use                           bool
	family                        uint16
	saddr, daddr                  xtcpnl.XfrmAddress
	proto, mode, flags            uint8
	spi, reqid                    uint32
	srcMask, dstMask              uint8
	protoMask, modeMask, flagMask uint8
	spiMask, reqidMask            uint32
}

// xfrmArgs is an argv as ip's loops walk it: i is *argv, and the C's
// argc is what is left from it.
type xfrmArgs struct {
	args []string
	i    int
}

// cur is *argv.
func (a *xfrmArgs) cur() string { return a.args[a.i] }

// next is NEXT_ARG: the next word, or incomplete_command.
func (a *xfrmArgs) next() error {
	a.i++
	if a.i >= len(a.args) {
		return xfrmUsage(incompleteErr())
	}
	return nil
}

// nextOK is NEXT_ARG_OK: a word follows this one.
func (a *xfrmArgs) nextOK() bool { return a.i+1 < len(a.args) }

// xfrmID is what xfrm_id_parse fills: the source address, struct xfrm_id,
// and the family when an address set it.
type xfrmID struct {
	saddr, daddr xtcpnl.XfrmAddress
	proto        uint8
	spi          uint32
}

// xfrmIDParse is xfrm_id_parse: src, dst, proto and spi by strcmp, each
// with its value, until another word, which is left for the caller as
// PREV_ARG leaves it. Each address is get_prefix's in preferred_family,
// setting *family when given and its mask in f. loose allows no protocol.
func xfrmIDParse(c *runCtx, a *xfrmArgs, id *xfrmID, family *uint16, f *xfrmFilter, loose bool) error {
	start := a.i
	var srcFam, dstFam uint8
	for {
		var err error
		switch a.cur() {
		case xfrmSrcCst:
			srcFam, err = xfrmIDAddr(c, a, &id.saddr, family, &f.srcMask, "src")
		case xfrmDstCst:
			dstFam, err = xfrmIDAddr(c, a, &id.daddr, family, &f.dstMask, "dst")
		case xfrmProtoCst:
			err = xfrmIDProto(a, id, f)
		case xfrmSpiCst:
			err = xfrmIDSpi(a, id, f)
		default:
			a.i--
			return xfrmIDCheck(a, id, srcFam, dstFam, start, loose)
		}
		if err != nil {
			return err
		}
		if !a.nextOK() {
			return xfrmIDCheck(a, id, srcFam, dstFam, start, loose)
		}
		a.i++
	}
}

// xfrmIDAddr is src's or dst's value: get_prefix's, refused when its
// family is AF_UNSPEC (any, default), its data and its bit length.
func xfrmIDAddr(c *runCtx, a *xfrmArgs, dst *xtcpnl.XfrmAddress, family *uint16, mask *uint8, key string) (uint8,
	error) {
	if err := a.next(); err != nil {
		return 0, err
	}
	p, err := routeGetPrefix(a.cur(), c.family)
	if err != nil {
		return 0, err
	}
	if p.family == unix.AF_UNSPEC {
		return 0, xfrmUsage(invargErr("value after \""+key+"\" has an unrecognized address family", a.cur()))
	}
	if family != nil {
		*family = uint16(p.family)
	}
	*dst = xtcpnl.XfrmAddress{}
	copy(dst[:], p.data)
	*mask = uint8(p.bitlen)
	return p.family, nil
}

// xfrmIDProto is proto's value, xfrm_xfrmproto_getbyname's.
func xfrmIDProto(a *xfrmArgs, id *xfrmID, f *xfrmFilter) error {
	if err := a.next(); err != nil {
		return err
	}
	p := render.XfrmProtoByName(a.cur())
	if p < 0 {
		return xfrmUsage(invargErr("XFRM-PROTO value is invalid", a.cur()))
	}
	id.proto, f.protoMask = uint8(p), 0xff
	return nil
}

// xfrmIDSpi is spi's value, get_be32 base 0.
func xfrmIDSpi(a *xfrmArgs, id *xfrmID, f *xfrmFilter) error {
	if err := a.next(); err != nil {
		return err
	}
	v, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("SPI value is invalid", a.cur()))
	}
	id.spi, f.spiMask = v, xfrmMaskAllCst
	return nil
}

// xfrmIDCheck is xfrm_id_parse's checks after its loop: one family for
// both addresses, an SPI the protocol can take, a protocol unless loose,
// and some word read.
func xfrmIDCheck(a *xfrmArgs, id *xfrmID, srcFam, dstFam uint8, start int, loose bool) error {
	if srcFam != 0 && dstFam != 0 && srcFam != dstFam {
		return xfrmUsage(invargErr("the same address family is required between values after \"src\" and \"dst\"",
			a.cur()))
	}
	if id.spi != 0 && id.proto != 0 {
		switch {
		case id.proto == unix.IPPROTO_ROUTING || id.proto == unix.IPPROTO_DSTOPTS:
			return fmt.Errorf("%w: \"spi\" is invalid with XFRM-PROTO value %q", ErrXfrmRefused,
				render.XfrmProtoName(id.proto))
		case id.proto == unix.IPPROTO_COMP && id.spi >= 0x10000:
			return fmt.Errorf("%w: SPI value is too large with XFRM-PROTO value %q", ErrXfrmRefused,
				render.XfrmProtoName(id.proto))
		}
	}
	if !loose && id.proto == 0 {
		return xfrmUsage(fmt.Errorf("Error: argument %q is required", "XFRM-PROTO"))
	}
	if a.i == start {
		return xfrmUsage(fmt.Errorf("Error: argument %q is required", "ID"))
	}
	return nil
}

// xfrmStateList is xfrm_state_list_or_deleteall's list: its filter's
// words, then the GETSA dump with the protocol and the address filter, each
// answer printed through the filter, nokeys hiding the keys.
func xfrmStateList(c *runCtx, args []string) error {
	f, nokeys, err := xfrmStateListArgs(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	var raw [64]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	var errs []error
	if f.proto != 0 {
		errs = append(errs, b.PutU8(xtcpnl.XfrmAttrProto, f.proto))
	}
	af := make([]byte, xfrmAddrFilterLenCst)
	copy(af[0:], f.saddr[:])
	copy(af[16:], f.daddr[:])
	binary.LittleEndian.PutUint16(af[32:], f.family)
	af[34], af[35] = f.srcMask, f.dstMask
	errs = append(errs, b.PutBytes(xtcpnl.XfrmAttrAddressFilter, af))
	if err := errors.Join(errs...); err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetSA, unix.NLM_F_DUMP, c.nextSeq(), nil, b.Bytes())
	if err != nil {
		return err
	}
	msgs, err := xs.XfrmDump(req)
	for i := 0; err == nil && i < len(msgs); i++ {
		err = xfrmStatePrint(c, &f, nokeys, msgs[i])
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrXfrmDumpTerminated, err)
	}
	return nil
}

// xfrmStateListArgs is the list's loop: nokeys, mode, reqid and flag by
// strcmp, each but nokeys with its value, else one ID. Any word, or a
// family, turns the filter on.
func xfrmStateListArgs(c *runCtx, args []string) (xfrmFilter, bool, error) {
	f := xfrmFilter{use: len(args) > 0 || c.family != unix.AF_UNSPEC, family: uint16(c.family)}
	nokeys := false
	idSeen := ""
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		var err error
		switch a.cur() {
		case xfrmNokeysCst:
			nokeys = true
		case xfrmModeCst:
			if err = a.next(); err == nil {
				f.mode, err = xfrmModeParse(a.cur())
				f.modeMask = 0xff
			}
		case xfrmReqidCst:
			if err = a.next(); err == nil {
				f.reqid, err = xfrmReqidParse(a.cur())
				f.reqidMask = xfrmMaskAllCst
			}
		case xfrmFlagCst:
			if err = a.next(); err == nil {
				err = xfrmStateFlagParse(a, &f.flags)
				f.flagMask = 0xff
			}
		default:
			if idSeen != "" {
				return f, false, xfrmUsage(invargErr("unknown", a.cur()))
			}
			idSeen = a.cur()
			var id xfrmID
			id.saddr, id.daddr = f.saddr, f.daddr
			if err = xfrmIDParse(c, a, &id, &f.family, &f, true); err == nil {
				f.saddr, f.daddr, f.proto, f.spi = id.saddr, id.daddr, id.proto, id.spi
				if c.family == unix.AF_UNSPEC {
					c.family = uint8(f.family)
				}
			}
		}
		if err != nil {
			return f, false, err
		}
	}
	return f, nokeys, nil
}

// xfrmModeParse is xfrm_mode_parse: each mode by prefix, in its order.
func xfrmModeParse(v string) (uint8, error) {
	for i, m := range []string{"transport", "tunnel", "ro", "in_trigger", "beet"} {
		if matchesPrefix(v, m) {
			return uint8(i), nil
		}
	}
	return 0, xfrmUsage(invargErr("MODE value is invalid", v))
}

// xfrmReqidParse is xfrm_reqid_parse, get_u32 base 0.
func xfrmReqidParse(v string) (uint32, error) {
	n, ok := getU32(v, 0)
	if !ok {
		return 0, xfrmUsage(invargErr("REQID value is invalid", v))
	}
	return n, nil
}

// xfrmStateFlagNames are xfrm_state_flag_parse's words, XFRM_STATE_*'s
// bits in order.
var xfrmStateFlagNames = []string{"noecn", "decap-dscp", "nopmtudisc", "wildrecv", "icmp", "af-unspec", "align4",
	"esn"}

// xfrmStateFlagParse is xfrm_state_flag_parse: a 0x value, get_u8 base 16,
// which sets the flags, or a run of flag words, each ORed in, until another
// word, which is left as PREV_ARG leaves it.
func xfrmStateFlagParse(a *xfrmArgs, flags *uint8) error {
	if v := a.cur(); len(v) > 2 && strings.HasPrefix(v, "0x") {
		n, ok := getU8(v, 16)
		if !ok {
			return xfrmUsage(invargErr("FLAG value is invalid", v))
		}
		*flags = n
		return nil
	}
	for {
		bit := -1
		for i, name := range xfrmStateFlagNames {
			if a.cur() == name {
				bit = i
			}
		}
		if bit < 0 {
			a.i--
			return nil
		}
		*flags |= 1 << bit
		if !a.nextOK() {
			return nil
		}
		a.i++
	}
}

// xfrmAddrMatch is xfrm_addr_match: whether bits leading bits of two
// addresses agree.
func xfrmAddrMatch(x1, x2 *xtcpnl.XfrmAddress, bits uint8) bool {
	words, rem := int(bits>>5), bits&0x1f
	if words > 4 {
		words = 4
	}
	if string(x1[:words*4]) != string(x2[:words*4]) {
		return false
	}
	if rem == 0 || words == 4 {
		return true
	}
	w1 := binary.BigEndian.Uint32(x1[words*4:])
	w2 := binary.BigEndian.Uint32(x2[words*4:])
	mask := uint32(0xffffffff) << (32 - rem)
	return (w1^w2)&mask == 0
}

// match is xfrm_state_filter_match.
func (f *xfrmFilter) match(x *xtcpnl.XfrmUsersaInfo) bool {
	switch {
	case !f.use:
		return true
	case f.family != unix.AF_UNSPEC && f.family != x.Family,
		f.srcMask != 0 && !xfrmAddrMatch(&x.Saddr, &f.saddr, f.srcMask),
		f.dstMask != 0 && !xfrmAddrMatch(&x.ID.Daddr, &f.daddr, f.dstMask),
		(x.ID.Proto^f.proto)&f.protoMask != 0,
		(x.ID.SPI^f.spi)&f.spiMask != 0,
		(x.Mode^f.mode)&f.modeMask != 0,
		(x.Reqid^f.reqid)&f.reqidMask != 0,
		f.flagMask != 0 && x.Flags&f.flags == 0:
		return false
	}
	return true
}

// xfrmStatePrint is __do_xfrm_state_print for a NEWSA, the one type a
// GETSA answers with: another type is "Not a state" on stderr and
// skipped; a body short of its struct ends the dump; an SA the filter
// does not match is skipped; else xfrm_state_info_print.
func xfrmStatePrint(c *runCtx, f *xfrmFilter, nokeys bool, m xfrmMsg) error {
	if m.typ != xtcpnl.XfrmMsgNewSA {
		_, err := fmt.Fprintf(c.errOut, "Not a state: %08x %08x %08x\n", xtcpnl.NlMsgHdrSizeCst+len(m.body), m.typ,
			m.flags)
		return err
	}
	if len(m.body) < xtcpnl.XfrmUsersaInfoSizeCst {
		return fmt.Errorf("BUG: wrong nlmsg len %d", len(m.body)-xtcpnl.XfrmUsersaInfoSizeCst)
	}
	s, err := xtcpnl.ParseXfrmState(xtcpnl.NlMsgHdr{Type: m.typ, Flags: m.flags}, m.body)
	if err != nil {
		return err
	}
	if !f.match(s.Info) {
		return nil
	}
	o := render.XfrmOpt{Stats: c.stats(), Nokeys: nokeys, Family: c.family,
		IndexName: func(idx int32) string { return genlIndexToName(c, idx) }}
	if err := xfrmProtoOpt(&o, s.Info.Sel.Proto); err != nil {
		return err
	}
	_, err = io.WriteString(c.out, render.XfrmStateInfo(s.Info, &s.Attrs, &o))
	return err
}

// xfrmProtoOpt sets o's strxf_proto for a selector's protocol: none, or
// one goip names; another is not implemented.
func xfrmProtoOpt(o *render.XfrmOpt, proto uint8) error {
	if proto == 0 {
		return nil
	}
	name, err := xfrmProtoName(proto)
	if err != nil {
		return fmt.Errorf("a selector's %w", err)
	}
	o.ProtoName = func(uint8) string { return name }
	return nil
}

// xfrmStateIDRequest is xfrm_state_get_or_delete's loop and request: mark,
// or one ID, which needs a protocol, then struct xfrm_usersa_id, its source
// as XFRMA_SRCADDR and the mark, when both its halves are set, as
// XFRMA_MARK; AF_INET when no address gave a family. get sends it as a
// GETSA, delete as a DELSA with NLM_F_ACK.
func xfrmStateIDRequest(c *runCtx, args []string) (hdr, attrs []byte, err error) {
	family := uint16(c.family)
	var id xfrmID
	var f xfrmFilter
	var mark [2]uint32
	var raw [128]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	idSeen := ""
	a := &xfrmArgs{args: args}
	for ; a.i < len(a.args); a.i++ {
		if a.cur() == xfrmMarkCst {
			if err := xfrmParseMark(a, &mark); err != nil {
				return nil, nil, err
			}
			continue
		}
		if idSeen != "" {
			return nil, nil, xfrmUsage(invargErr("unknown", a.cur()))
		}
		idSeen = a.cur()
		id = xfrmID{}
		if err := xfrmIDParse(c, a, &id, &family, &f, false); err != nil {
			return nil, nil, err
		}
		if err := b.PutBytes(xtcpnl.XfrmAttrSrcAddr, id.saddr[:]); err != nil {
			return nil, nil, err
		}
	}
	if mark[0]&mark[1] != 0 {
		v := binary.LittleEndian.AppendUint32(nil, mark[0])
		if err := b.PutBytes(xtcpnl.XfrmAttrMark, binary.LittleEndian.AppendUint32(v, mark[1])); err != nil {
			return nil, nil, err
		}
	}
	if family == unix.AF_UNSPEC {
		family = unix.AF_INET
	}
	hdr = make([]byte, xtcpnl.XfrmUsersaIDSizeCst)
	copy(hdr, id.daddr[:])
	binary.BigEndian.PutUint32(hdr[16:], id.spi)
	binary.LittleEndian.PutUint16(hdr[20:], family)
	hdr[22] = id.proto
	return hdr, b.Bytes(), nil
}

// xfrmStateGet is xfrm_state_get_or_delete's get: the GETSA, its answer
// printed unfiltered.
func xfrmStateGet(c *runCtx, args []string) error {
	hdr, attrs, err := xfrmStateIDRequest(c, args)
	if err != nil {
		return err
	}
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetSA, 0, c.nextSeq(), hdr, attrs)
	if err != nil {
		return err
	}
	typ, body, err := xs.XfrmTalk(req)
	if err != nil {
		return err
	}
	if err := xfrmStatePrint(c, &xfrmFilter{}, false, xfrmMsg{typ: typ, body: body}); err != nil {
		if errors.Is(err, ErrNotImplemented) {
			return err
		}
		return fmt.Errorf("%w: An error :-): %w", ErrXfrmRefused, err)
	}
	return nil
}

// xfrmStateDelete is xfrm_state_get_or_delete's delete: the DELSA, with
// NLM_F_ACK, to GOIP_WRITE_OUT.
func xfrmStateDelete(c *runCtx, args []string) error {
	hdr, attrs, err := xfrmStateIDRequest(c, args)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmWriteRequest(xtcpnl.XfrmMsgDelSA, unix.NLM_F_ACK, c.nextSeq(), hdr, attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// xfrmParseMark is xfrm_parse_mark: the value, get_u32 base 0, then mask
// and its value, or, when the next word is something else or there is
// none, an all-ones mask.
func xfrmParseMark(a *xfrmArgs, mark *[2]uint32) error {
	if err := a.next(); err != nil {
		return err
	}
	v, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("MARK value is invalid\n", a.cur()))
	}
	mark[0] = v
	if !a.nextOK() {
		mark[1] = xfrmMaskAllCst
		return nil
	}
	a.i++
	if a.cur() != "mask" {
		mark[1] = xfrmMaskAllCst
		a.i--
		return nil
	}
	if err := a.next(); err != nil {
		return err
	}
	m, ok := getU32(a.cur(), 0)
	if !ok {
		return xfrmUsage(invargErr("MASK value is invalid\n", a.cur()))
	}
	mark[1] = m
	return nil
}

// xfrmStateCount is xfrm_sad_getinfo: one GETSADINFO asking for every
// field, then print_sadinfo. Any word after count is unread.
func xfrmStateCount(c *runCtx) error {
	xs, err := xfrmSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildXfrmRequest(xtcpnl.XfrmMsgGetSADInfo, 0, c.nextSeq(),
		binary.LittleEndian.AppendUint32(nil, xfrmMaskAllCst), nil)
	if err != nil {
		return err
	}
	typ, body, err := xs.XfrmTalk(req)
	if err != nil {
		return err
	}
	info, err := xtcpnl.ParseXfrmInfo(xtcpnl.NlMsgHdr{Type: typ}, body)
	if err != nil {
		return fmt.Errorf("%w: SADinfo: %w", ErrXfrmRefused, err)
	}
	_, err = io.WriteString(c.out, render.XfrmSADInfo(&info, c.stats()))
	return err
}
