package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The words of parse_encap_seg6, seg6local, ila, xfrm, ioam6 and rpl
// (ip/iproute_lwtunnel.c) no other parser in goip shares.
const (
	lwtKeySegs     = "segs"
	lwtKeyTundst   = "tundst"
	lwtKeyOif      = "oif"
	lwtKeyFlavors  = "flavors"
	lwtKeySrh      = "srh"
	lwtKeyVrftable = "vrftable"
	lwtKeyNH4      = "nh4"
	lwtKeyNH6      = "nh6"
	lwtKeyEndpoint = "endpoint"
	lwtKeyLblen    = "lblen"
	lwtKeyNflen    = "nflen"
	lwtKeyFreq     = "freq"
	lwtKeyTrace    = "trace"
	lwtKeyPrealloc = "prealloc"
	lwtKeyNS       = "ns"
	lwtKeySize     = "size"
	lwtKeyLinkDev  = "link_dev"
	lwtKeyHookType = "hook-type"

	// lwtSegbufCst is parse_encap_seg6's and seg6local's char segbuf[1024]:
	// strlcpy keeps 1023 bytes of `segs`.
	lwtSegbufCst = 1024
	// ioam6FreqBufCst is parse_encap_ioam6's char buf[16].
	ioam6FreqBufCst = 16
	// ioam6FreqMaxCst is IOAM6_IPTUNNEL_FREQ_MAX; the least is 1.
	ioam6FreqMaxCst = 1000000
	// ioam6TraceSizeMaxCst is IOAM6_TRACE_DATA_SIZE_MAX.
	ioam6TraceSizeMaxCst = 244
	// seg6LocalActionEndB6EncapCst and seg6LocalActionEndBPFCst are
	// SEG6_LOCAL_ACTION_END_B6_ENCAP and SEG6_LOCAL_ACTION_END_BPF.
	seg6LocalActionEndB6EncapCst = 10
	seg6LocalActionEndBPFCst     = 15
)

// seg6ModeTypes is seg6_mode_types (ip/iproute_lwtunnel.c:133), by strcmp.
var seg6ModeTypes = map[string]int32{
	"inline": xtcpnl.Seg6ModeInline, tnlKeyEncap: xtcpnl.Seg6ModeEncap, "l2encap": xtcpnl.Seg6ModeL2Encap,
	"encap.red": xtcpnl.Seg6ModeEncapRed, "l2encap.red": xtcpnl.Seg6ModeL2EncapRed,
}

// seg6ActionNames is seg6_action_names (ip/iproute_lwtunnel.c:391), by
// strcmp.
var seg6ActionNames = map[string]uint32{
	"End": 1, "End.X": 2, "End.T": 3, "End.DX2": 4, "End.DX6": 5, "End.DX4": 6, "End.DT6": 7,
	"End.DT4": 8, "End.B6": 9, "End.B6.Encaps": seg6LocalActionEndB6EncapCst, "End.BM": 11,
	"End.S": 12, "End.AS": 13, "End.AM": 14, "End.BPF": seg6LocalActionEndBPFCst, "End.DT46": 16,
}

// seg6FlavorNames is seg6_flavor_names (ip/iproute_lwtunnel.c:161), which
// read_seg6_local_flv_type matches by strcasecmp.
var seg6FlavorNames = map[string]int{
	"psp": xtcpnl.Seg6LocalFlvOpPSP, "usp": xtcpnl.Seg6LocalFlvOpUSP, "usd": xtcpnl.Seg6LocalFlvOpUSD,
	"next-csid": xtcpnl.Seg6LocalFlvOpNextCSID,
}

// ioam6ModeTypes is ioam6_mode_types (ip/iproute_lwtunnel.c:315).
var ioam6ModeTypes = map[string]uint8{
	"inline": xtcpnl.Ioam6ModeInline, tnlKeyEncap: xtcpnl.Ioam6ModeEncap, netnsAutoCst: xtcpnl.Ioam6ModeAuto,
}

// ilaHookTypes is ila_hook_name2type (ip/ila_common.h).
var ilaHookTypes = map[string]uint8{"output": 0, "input": 1}

// lwtValued reads the value after args[i] and refuses a second use of the
// word with duparg2, which names the value: the shape most of these
// parsers' keywords share.
func lwtValued(args []string, i int, seen map[string]bool) (int, string, error) {
	w := args[i]
	i, err := nextArg(args, i)
	if err != nil {
		return i, "", err
	}
	if seen[w] {
		return i, "", duparg2Err(w, args[i])
	}
	seen[w] = true
	return i, args[i], nil
}

// lwtSegs is parse_srh's and parse_rpl_srh's reading of segbuf: strlcpy's
// 1023 bytes, a slot per comma and one more, and get_addr in AF_INET6 of
// each field strtok finds, which exits 1 on a bad one.
func lwtSegs(v string) (int, [][16]byte, error) {
	if len(v) > lwtSegbufCst-1 {
		v = v[:lwtSegbufCst-1]
	}
	nsegs := strings.Count(v, ",") + 1
	var segs [][16]byte
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' }) {
		a, err := routeGetAddr(f, unix.AF_INET6)
		if err != nil {
			return 0, nil, err
		}
		var s [16]byte
		copy(s[:], a.data)
		segs = append(segs, s)
	}
	return nsegs, segs, nil
}

// lwtSRH is parse_srh: nsegs slots, one more unless encap, and the HMAC
// TLV for a non-zero key. A header past hdrlen's u8 is past the route's
// 1024 bytes too, and refused.
func lwtSRH(segbuf string, hmac uint32, encap bool) ([]byte, error) {
	nsegs, segs, err := lwtSegs(segbuf)
	if err != nil {
		return nil, err
	}
	if !encap {
		nsegs++
	}
	b, err := xtcpnl.SRH(nsegs, segs, hmac)
	if errors.Is(err, xtcpnl.ErrSRH) {
		return nil, fmt.Errorf("%w: addraw_l ERROR: message exceeded bound of %d", ErrInvalidArgument, routeEncapBufCst)
	}
	return b, err
}

// lwtResolve is ll_name_to_index for an encap's device: a miss is
// exit(nodev(name)), 255.
func lwtResolve(resolve linkResolver, name string) (uint32, error) {
	if resolve == nil {
		return 0, fmt.Errorf("encap device %q: no resolver: %w", name, ErrNotImplemented)
	}
	idx, err := resolve(name)
	if err != nil {
		return 0, err
	}
	if idx <= 0 {
		return 0, nodevExitErr(name)
	}
	return cint.Bits(idx), nil
}

// seg6EncapArgs is parse_encap_seg6 (ip/iproute_lwtunnel.c:955) from
// args[i]: mode, tunsrc, segs and hmac in any order, but tunsrc and segs
// after mode, until a word it doesn't know. hmac's get_u32 isn't checked,
// so a bad one is 0. With no mode, the mode is -1 and the SRH has one more
// slot, as parse_srh's encap is the mode.
func seg6EncapArgs(args []string, i int) (xtcpnl.LwtEncap, int, error) {
	e := xtcpnl.Seg6Encap{Mode: -1}
	seen := map[string]bool{}
	var segbuf string
	var hmac uint32
	for ; i < len(args); i++ {
		var v string
		var err error
		switch w := args[i]; w {
		case iptnlMode:
			if i, v, err = lwtValued(args, i, seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			m, ok := seg6ModeTypes[v]
			if !ok {
				return xtcpnl.LwtEncap{}, i, invargErr("\"mode\" value is invalid\n", v)
			}
			e.Mode = m
		case srTunsrcCst:
			if i, err = nextArg(args, i); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			if err = seg6Tunsrc(&e, args[i], seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
		case lwtKeySegs:
			if i, v, err = lwtValued(args, i, seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			if e.Mode == -1 {
				return xtcpnl.LwtEncap{}, i, invargErr("\"segs\" provided before \"mode\"\n", v)
			}
			segbuf = v
		case srHmacCst:
			if i, v, err = lwtValued(args, i, seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			hmac, _ = getU32(v, 0)
		default:
			return seg6EncapDone(&e, segbuf, hmac, i-1)
		}
	}
	return seg6EncapDone(&e, segbuf, hmac, len(args)-1)
}

// seg6Tunsrc is tunsrc's value: after mode, not with inline, once, and
// a 16-byte IPv6 address. The checks run in that order, all before
// get_addr.
func seg6Tunsrc(e *xtcpnl.Seg6Encap, v string, seen map[string]bool) error {
	switch {
	case e.Mode == -1:
		return invargErr("\"tunsrc\" provided before \"mode\"\n", v)
	case e.Mode == xtcpnl.Seg6ModeInline:
		return invargErr("\"tunsrc\" invalid with inline mode\n", v)
	case seen[srTunsrcCst]:
		return duparg2Err(srTunsrcCst, v)
	}
	seen[srTunsrcCst] = true
	a, err := routeGetAddr(v, unix.AF_INET6)
	if err != nil {
		return err
	}
	if a.family != unix.AF_INET6 || len(a.data) != 16 {
		return invargErr("\"tunsrc\" value is invalid\n", v)
	}
	e.TunSrc = a.data
	return nil
}

func seg6EncapDone(e *xtcpnl.Seg6Encap, segbuf string, hmac uint32, last int) (xtcpnl.LwtEncap, int, error) {
	srh, err := lwtSRH(segbuf, hmac, e.Mode != xtcpnl.Seg6ModeInline)
	if err != nil {
		return xtcpnl.LwtEncap{}, last, err
	}
	e.SRH = srh
	return xtcpnl.LwtEncap{Seg6: e}, last, nil
}

// seg6LocalParse is parse_encap_seg6local's state.
type seg6LocalParse struct {
	e       xtcpnl.Seg6LocalEncap
	seen    map[string]bool
	action  uint32
	srh     bool
	segbuf  string
	hmac    uint32
	resolve linkResolver
}

// seg6LocalEncapArgs is parse_encap_seg6local (ip/iproute_lwtunnel.c:1447)
// from args[i]: the keywords in any order, each written as it is read,
// until one it doesn't know; then "Missing action type" with none, and the
// SRH last. `srh segs S` as the last words breaks out on S, so the index
// returned is the word before it, and the caller reads S again.
func seg6LocalEncapArgs(args []string, i int, resolve linkResolver) (xtcpnl.LwtEncap, int, error) {
	p := seg6LocalParse{seen: map[string]bool{}, resolve: resolve}
	last := len(args) - 1
	for i < len(args) {
		var err error
		var stop, again bool
		switch args[i] {
		case xfrmActionCst, ruleTableKeywordCst, lwtKeyVrftable, lwtKeyNH4, lwtKeyNH6, iifKeywordCst, lwtKeyOif:
			i, err = p.valued(args, i)
		case xfrmCountCst:
			if p.seen[xfrmCountCst] {
				return xtcpnl.LwtEncap{}, i, duparg2Err(xfrmCountCst, xfrmCountCst)
			}
			p.seen[xfrmCountCst] = true
			p.e.Items = append(p.e.Items, xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalCounters})
		case lwtKeyFlavors:
			i, err = p.flavors(args, i)
		case lwtKeySrh:
			i, stop, again, err = p.srhArgs(args, i)
		case lwtKeyEndpoint:
			return xtcpnl.LwtEncap{}, i, fmt.Errorf("encap seg6local endpoint: a BPF program: %w", ErrNotImplemented)
		default:
			stop = true
		}
		if err != nil {
			return xtcpnl.LwtEncap{}, i, err
		}
		if stop {
			last = i - 1
			break
		}
		if !again {
			i++
		}
	}
	if p.action == 0 {
		return xtcpnl.LwtEncap{}, last, linkUsage(fmt.Errorf("%w: Missing action type", ErrInvalidArgument))
	}
	if p.srh {
		srh, err := lwtSRH(p.segbuf, p.hmac, p.action == seg6LocalActionEndB6EncapCst)
		if err != nil {
			return xtcpnl.LwtEncap{}, last, err
		}
		p.e.SRH = srh
	}
	return xtcpnl.LwtEncap{Seg6Local: &p.e}, last, nil
}

// valued is one of seg6local's words with a value, written as a u32 or an
// address. The refusals' texts are ip's.
func (p *seg6LocalParse) valued(args []string, i int) (int, error) {
	w := args[i]
	i, v, err := lwtValued(args, i, p.seen)
	if err != nil {
		return i, err
	}
	it := xtcpnl.Seg6LocalItem{}
	switch w {
	case xfrmActionCst:
		a, ok := seg6ActionNames[v]
		if !ok {
			return i, invargErr("\"action\" value is invalid\n", v)
		}
		if a == seg6LocalActionEndBPFCst {
			return i, fmt.Errorf("encap seg6local action End.BPF: a BPF program: %w", ErrNotImplemented)
		}
		p.action = a
		it = xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalAction, U32: a}
	case ruleTableKeywordCst, lwtKeyVrftable:
		t, ok := rtnlNameA2N(routeTableNames, v, 1<<32-1)
		if !ok {
			msg := "invalid table id\n"
			if w == lwtKeyVrftable {
				msg = "invalid vrf table id\n"
			}
			return i, invargErr(msg, v)
		}
		it = xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalTable, U32: t}
		if w == lwtKeyVrftable {
			it.Kind = xtcpnl.Seg6LocalVrfTable
		}
	case lwtKeyNH4, lwtKeyNH6:
		family, kind := uint8(unix.AF_INET), xtcpnl.Seg6LocalNH4
		if w == lwtKeyNH6 {
			family, kind = unix.AF_INET6, xtcpnl.Seg6LocalNH6
		}
		a, err := routeGetAddr(v, family)
		if err != nil {
			return i, err
		}
		it = xtcpnl.Seg6LocalItem{Kind: kind, Addr: a.data}
	default: // iif, oif
		idx, err := lwtResolve(p.resolve, v)
		if err != nil {
			return i, err
		}
		it = xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalIIF, U32: idx}
		if w == lwtKeyOif {
			it.Kind = xtcpnl.Seg6LocalOIF
		}
	}
	p.e.Items = append(p.e.Items, it)
	return i, nil
}

// flavors is seg6local_parse_flavors (ip/iproute_lwtunnel.c:1393): the
// list, then with next-csid `lblen` and `nflen`, each once and in that
// order. It looks at the word after the list, and after lblen's value,
// with NEXT_ARG_FWD, unchecked, then steps back with PREV_ARG. With no word
// there, ip reads past argv's end and crashes, which goip refuses as an
// incomplete command; after nflen's value it reads nothing.
func (p *seg6LocalParse) flavors(args []string, i int) (int, error) {
	i, v, err := lwtValued(args, i, p.seen)
	if err != nil {
		return i, err
	}
	f, ok := seg6Flavors(v)
	if !ok {
		return i, invargErr("invalid \"flavors\" attribute\n", v)
	}
	if f.Ops&(1<<xtcpnl.Seg6LocalFlvOpNextCSID) != 0 {
		for _, l := range []struct {
			word, msg string
			val       *uint8
			has       *bool
		}{
			{lwtKeyLblen, "\"locator-block length\" value is invalid\n", &f.LcblockBits, &f.HasLcblock},
			{lwtKeyNflen, "\"locator-node function length\" value is invalid\n", &f.LcnodeFnBits, &f.HasLcnodeFn},
		} {
			if i+1 >= len(args) {
				return i, incompleteErr()
			}
			if args[i+1] != l.word {
				continue
			}
			if i, v, err = lwtValued(args, i+1, p.seen); err != nil {
				return i, err
			}
			if *l.val, ok = getU8(v, 0); !ok {
				return i, invargErr(l.msg, v)
			}
			*l.has = true
		}
	}
	p.e.Items = append(p.e.Items, xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalFlavors, Flavors: f})
	return i, nil
}

// seg6Flavors is parse_seg6local_flavors: strtok over ',', each name by
// strcasecmp, none twice, and not empty.
func seg6Flavors(v string) (xtcpnl.Seg6LocalFlavorSet, bool) {
	var f xtcpnl.Seg6LocalFlavorSet
	if v == "" {
		return f, false
	}
	for _, s := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' }) {
		idx, ok := seg6FlavorNames[strings.ToLower(s)]
		if !ok || f.Ops&(1<<idx) != 0 {
			return f, false
		}
		f.Ops |= 1 << idx
	}
	return f, true
}

// srhArgs is seg6local's `srh segs S [hmac K]`. With nothing after S it
// breaks out of the loop on S (stop, the index on S). A word after S other
// than hmac is read again by the loop (again).
func (p *seg6LocalParse) srhArgs(args []string, i int) (int, bool, bool, error) {
	i, v, err := lwtValued(args, i, p.seen)
	if err != nil {
		return i, false, false, err
	}
	if v != lwtKeySegs {
		return i, false, false, invargErr("missing \"segs\" attribute for srh\n", v)
	}
	p.srh = true
	if i, v, err = lwtValued(args, i, p.seen); err != nil {
		return i, false, false, err
	}
	p.segbuf = v
	if i+1 >= len(args) {
		return i, true, false, nil
	}
	i++
	if args[i] != srHmacCst {
		return i, false, true, nil
	}
	if i, v, err = lwtValued(args, i, p.seen); err != nil {
		return i, false, false, err
	}
	p.hmac, _ = getU32(v, 0)
	return i, false, false, nil
}

// ilaEncapArgs is parse_encap_ila (ip/iproute_lwtunnel.c:1951): the
// locator (get_addr64, "Bad locator" and exit 1), then csum-mode,
// ident-type and hook-type, any number of times each, until a word it
// doesn't know.
func ilaEncapArgs(args []string, i int) (xtcpnl.LwtEncap, int, error) {
	loc, ok := ilaAddr64(args[i])
	if !ok {
		return xtcpnl.LwtEncap{}, i, fmt.Errorf("%w: Bad locator: %s", ErrInvalidArgument, args[i])
	}
	e := xtcpnl.IlaEncap{}
	binary.BigEndian.PutUint64(e.Locator[:], loc)
	for i++; i < len(args); i++ {
		var names map[string]uint8
		var kind uint16
		switch w := args[i]; w {
		case ilaCsumModeCst:
			names, kind = ilaCsumModes, xtcpnl.ILAAttrCsumMode
		case ilaIdentTypeCst:
			names, kind = ilaIdentTypes, xtcpnl.ILAAttrIdentType
		case lwtKeyHookType:
			names, kind = ilaHookTypes, xtcpnl.ILAAttrHookType
		default:
			return xtcpnl.LwtEncap{ILA: &e}, i - 1, nil
		}
		w := args[i]
		var err error
		if i, err = nextArg(args, i); err != nil {
			return xtcpnl.LwtEncap{}, i, err
		}
		v, ok := names[args[i]]
		if !ok {
			return xtcpnl.LwtEncap{}, i, invargErr(fmt.Sprintf("%q value is invalid\n", w), args[i])
		}
		e.Items = append(e.Items, xtcpnl.IlaItem{Kind: kind, U8: v})
	}
	return xtcpnl.LwtEncap{ILA: &e}, len(args) - 1, nil
}

// xfrmEncapArgs is parse_encap_xfrm (ip/iproute_lwtunnel.c:2229): if_id
// and link_dev. Its loop has no else, so a word it doesn't know is skipped
// and the loop runs to the end of argv: the rest of the command is the
// encap's. With no if_id, lwt_xfrm_usage is exit 255.
func xfrmEncapArgs(args []string, i int, resolve linkResolver) (xtcpnl.LwtEncap, int, error) {
	e := xtcpnl.XfrmEncap{}
	seen := map[string]bool{}
	for ; i < len(args); i++ {
		var v string
		var err error
		switch args[i] {
		case xfrmIfIDCst:
			if i, v, err = lwtValued(args, i, seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			id, ok := getU32(v, 0)
			if !ok || id == 0 {
				return xtcpnl.LwtEncap{}, i, invargErr("\"if_id\" value is invalid\n", v)
			}
			e.Items = append(e.Items, xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmIfID, U32: id})
		case lwtKeyLinkDev:
			if i, v, err = lwtValued(args, i, seen); err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			idx, err := lwtResolve(resolve, v)
			if err != nil {
				return xtcpnl.LwtEncap{}, i, err
			}
			e.Items = append(e.Items, xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmLink, U32: idx})
		case addrHelpKeywordCst:
			return xtcpnl.LwtEncap{}, i, lwtXfrmUsageErr()
		}
	}
	if !seen[xfrmIfIDCst] {
		return xtcpnl.LwtEncap{}, len(args) - 1, lwtXfrmUsageErr()
	}
	return xtcpnl.LwtEncap{Xfrm: &e}, len(args) - 1, nil
}

func lwtXfrmUsageErr() error {
	return linkUsage(fmt.Errorf("%w: Usage: ip route ... encap xfrm if_id IF_ID [ link_dev LINK ]", ErrInvalidArgument))
}

// ioam6EncapArgs is parse_encap_ioam6 (ip/iproute_lwtunnel.c:1150):
// freq, mode, tunsrc and tundst, each optional but in that order, then
// `trace prealloc` and type, ns and size in any order. Each NEXT_ARG
// before the trace needs a word after it. The trace loop stops either on a
// word it doesn't know or, with no word left, on the last value, which the
// caller then reads again.
func ioam6EncapArgs(args []string, i int) (xtcpnl.LwtEncap, int, error) {
	e := xtcpnl.Ioam6Encap{FreqK: 1, FreqN: 1, Mode: xtcpnl.Ioam6ModeInline}
	steps := []func([]string, int, *xtcpnl.Ioam6Encap) (int, error){ioam6Freq, ioam6Mode, ioam6Tunsrc, ioam6Tundst}
	var err error
	for _, step := range steps {
		if i, err = step(args, i, &e); err != nil {
			return xtcpnl.LwtEncap{}, i, err
		}
	}
	if args[i] != lwtKeyTrace {
		return xtcpnl.LwtEncap{}, i, missargErr(lwtKeyTrace)
	}
	if i, err = nextArg(args, i); err != nil {
		return xtcpnl.LwtEncap{}, i, err
	}
	if args[i] != lwtKeyPrealloc {
		return xtcpnl.LwtEncap{}, i, missargErr(lwtKeyPrealloc)
	}
	last, err := ioam6Trace(args, i, &e)
	if err != nil {
		return xtcpnl.LwtEncap{}, last, err
	}
	return xtcpnl.LwtEncap{Ioam6: &e}, last, nil
}

func ioam6Freq(args []string, i int, e *xtcpnl.Ioam6Encap) (int, error) {
	if args[i] != lwtKeyFreq {
		return i, nil
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v := args[i]
	if len(v) > ioam6FreqBufCst-1 {
		return i, invargErr("Invalid frequency (too long)", v)
	}
	k, n, ok := ioam6FreqKN(v)
	switch {
	case !ok:
		return i, invargErr("Invalid frequency (malformed)", v)
	case k < 1 || k > ioam6FreqMaxCst:
		return i, invargErr("Out of bound \"k\" frequency", v)
	case n < 1 || n > ioam6FreqMaxCst:
		return i, invargErr("Out of bound \"n\" frequency", v)
	case k > n:
		return i, invargErr("Frequency with k > n is forbidden", v)
	}
	e.FreqK, e.FreqN = k, n
	return nextArg(args, i)
}

// ioam6FreqKN is parse_ioam6_freq: exactly one '/', and both sides get_u32
// base 10.
func ioam6FreqKN(v string) (uint32, uint32, bool) {
	if strings.Count(v, "/") != 1 {
		return 0, 0, false
	}
	ks, ns, _ := strings.Cut(v, "/")
	k, okK := getU32(ks, 10)
	n, okN := getU32(ns, 10)
	return k, n, okK && okN
}

func ioam6Mode(args []string, i int, e *xtcpnl.Ioam6Encap) (int, error) {
	if args[i] != iptnlMode {
		return i, nil
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	m, ok := ioam6ModeTypes[args[i]]
	if !ok {
		return i, invargErr("Invalid mode", args[i])
	}
	e.Mode = m
	return nextArg(args, i)
}

// ioam6Tunsrc and ioam6Tundst refuse inline mode naming the keyword, as
// *argv is still on it, and a tundst missing from a mode that isn't
// inline is missarg.
func ioam6Tunsrc(args []string, i int, e *xtcpnl.Ioam6Encap) (int, error) {
	if args[i] != srTunsrcCst {
		return i, nil
	}
	return ioam6Addr(args, i, e, &e.TunSrc, "Inline mode does not need tunsrc", "Invalid IPv6 address for tunsrc")
}

func ioam6Tundst(args []string, i int, e *xtcpnl.Ioam6Encap) (int, error) {
	if args[i] != lwtKeyTundst {
		if e.Mode != xtcpnl.Ioam6ModeInline {
			return i, missargErr(lwtKeyTundst)
		}
		return i, nil
	}
	return ioam6Addr(args, i, e, &e.TunDst, "Inline mode does not need tundst", "Invalid IPv6 address for tundst")
}

func ioam6Addr(args []string, i int, e *xtcpnl.Ioam6Encap, dst *[]byte, inlineMsg, badMsg string) (int, error) {
	if e.Mode == xtcpnl.Ioam6ModeInline {
		return i, invargErr(inlineMsg, args[i])
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	a, err := routeGetAddr(args[i], unix.AF_INET6)
	if err != nil {
		return i, err
	}
	if a.family != unix.AF_INET6 || len(a.data) != 16 {
		return i, invargErr(badMsg, args[i])
	}
	*dst = a.data
	return nextArg(args, i)
}

// ioam6Trace is the trace loop from `prealloc` at args[i]: while
// NEXT_ARG_OK, NEXT_ARG_FWD and a word. It returns the index the caller
// continues after: the word before the one it stopped on.
func ioam6Trace(args []string, i int, e *xtcpnl.Ioam6Encap) (int, error) {
	seen := map[string]bool{}
	for i+1 < len(args) {
		i++
		w := args[i]
		if w != linkXstatsTypeCst && w != lwtKeyNS && w != lwtKeySize {
			break
		}
		var err error
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
		v := args[i]
		if seen[w] {
			return i, duparg2Err(w, v)
		}
		if err := ioam6TraceValue(e, w, v); err != nil {
			return i, err
		}
		seen[w] = true
	}
	for _, w := range []string{linkXstatsTypeCst, lwtKeyNS, lwtKeySize} {
		if !seen[w] {
			return i, missargErr(w)
		}
	}
	return i - 1, nil
}

func ioam6TraceValue(e *xtcpnl.Ioam6Encap, w, v string) error {
	switch w {
	case linkXstatsTypeCst:
		t, ok := getU32(v, 0)
		if !ok || t == 0 {
			return invargErr("Invalid trace type", v)
		}
		e.TraceType = t
	case lwtKeyNS:
		ns, ok := getU16(v, 0)
		if !ok {
			return invargErr("Invalid namespace ID", v)
		}
		e.TraceNS = ns
	default: // size
		s, ok := getU16(v, 0)
		switch {
		case !ok || s == 0:
			return invargErr("Invalid trace size", v)
		case s%4 != 0:
			return invargErr("Trace size must be a 4-octet multiple", v)
		case s > ioam6TraceSizeMaxCst:
			return invargErr("Trace size is too big", v)
		}
		e.TraceSize = s
	}
	return nil
}

// rplEncapArgs is parse_encap_rpl (ip/iproute_lwtunnel.c:1083): segs,
// once, until a word it doesn't know, then parse_rpl_srh.
func rplEncapArgs(args []string, i int) (xtcpnl.LwtEncap, int, error) {
	seen := map[string]bool{}
	var segbuf string
	last := len(args) - 1
	for ; i < len(args); i++ {
		if args[i] != lwtKeySegs {
			last = i - 1
			break
		}
		var err error
		if i, segbuf, err = lwtValued(args, i, seen); err != nil {
			return xtcpnl.LwtEncap{}, i, err
		}
	}
	nsegs, segs, err := lwtSegs(segbuf)
	if err != nil {
		return xtcpnl.LwtEncap{}, last, err
	}
	if _, err := xtcpnl.RplSRH(nsegs, segs); err != nil {
		return xtcpnl.LwtEncap{}, last, fmt.Errorf("%w: addraw_l ERROR: message exceeded bound of %d", ErrInvalidArgument, routeEncapBufCst)
	}
	return xtcpnl.LwtEncap{Rpl: &xtcpnl.RplEncap{NSegs: nsegs, Segs: segs}}, last, nil
}
