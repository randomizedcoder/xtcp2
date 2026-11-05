package goip

import (
	"fmt"
	"slices"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// lwtEncapTypes are read_encap_type's names (ip/iproute_lwtunnel.c:76).
var lwtEncapTypes = []string{mplsKeywordCst, lwtEncapIPCst, lwtEncapIP6Cst, lwtEncapILACst, "bpf", lwtEncapSeg6Cst,
	lwtEncapSeg6LocalCst, lwtEncapRplCst, lwtEncapIoam6Cst, lwtEncapXfrmCst}

// mplsKeywordCst is "mpls": read_family's word for AF_MPLS, and an encap
// type.
const mplsKeywordCst = "mpls"

// The ip and ip6 encap types, and the words parse_encap_ip and
// parse_encap_ip6 share.
const (
	lwtEncapIPCst        = "ip"
	lwtEncapIP6Cst       = "ip6"
	lwtEncapILACst       = "ila"
	lwtEncapSeg6Cst      = "seg6"
	lwtEncapSeg6LocalCst = "seg6local"
	lwtEncapRplCst       = "rpl"
	lwtEncapIoam6Cst     = "ioam6"
	lwtEncapXfrmCst      = "xfrm"

	lwtKeyID     = "id"
	lwtKeyDst    = "dst"
	lwtKeySrc    = "src"
	lwtKeyGeneve = "geneve_opts"
	lwtKeyVxlan  = "vxlan_opts"
	lwtKeyErspan = "erspan_opts"
	lwtKeyOpts   = "opts" // duparg2's name for a second option word
	lwtKeyCsum   = "csum"
)

// lwtTunnelFlagWords are parse_encap_ip's flag words and their TUNNEL_* bits.
var lwtTunnelFlagWords = map[string]uint16{
	tnlKeyKey:  xtcpnl.LwtTunnelKey,
	lwtKeyCsum: xtcpnl.LwtTunnelCsum,
	greSeq:     xtcpnl.LwtTunnelSeq,
}

// lwtEncapArgs is lwt_parse_encap (ip/iproute_lwtunnel.c:2278) from the
// `encap` at args[i]: the type (read_encap_type, strcmp), a second
// NEXT_ARG, then argc <= 1's missarg, so the type's first argument can't
// be the last word, then the type's parser. It returns the encap and the
// index of the last word the parser used. resolve is ll_name_to_index for
// seg6local's iif and oif and xfrm's link_dev, which ip looks up as it
// reads them. Every type is built but bpf, which loads a program object.
func lwtEncapArgs(args []string, i int, resolve linkResolver) (xtcpnl.LwtEncap, int, error) {
	i, err := nextArg(args, i)
	if err != nil {
		return xtcpnl.LwtEncap{}, i, err
	}
	typ := args[i]
	if !slices.Contains(lwtEncapTypes, typ) {
		if typ == addrHelpKeywordCst {
			return xtcpnl.LwtEncap{}, i, fmt.Errorf("encap help: %w", ErrNotImplemented)
		}
		return xtcpnl.LwtEncap{}, i, invargErr("\"encap type\" value is invalid\n", typ)
	}
	if i, err = nextArg(args, i); err != nil {
		return xtcpnl.LwtEncap{}, i, err
	}
	if i == len(args)-1 {
		return xtcpnl.LwtEncap{}, i, fmt.Errorf("%w: Error: argument \"encap type\" is required", ErrInvalidArgument)
	}
	switch typ {
	case mplsKeywordCst:
		m, j, err := mplsEncapArgs(args, i)
		if err != nil {
			return xtcpnl.LwtEncap{}, j, err
		}
		return xtcpnl.LwtEncap{MPLS: &m}, j, nil
	case lwtEncapIPCst:
		return ipEncapArgs(args, i, unix.AF_INET)
	case lwtEncapIP6Cst:
		return ipEncapArgs(args, i, unix.AF_INET6)
	case lwtEncapSeg6Cst:
		return seg6EncapArgs(args, i)
	case lwtEncapSeg6LocalCst:
		return seg6LocalEncapArgs(args, i, resolve)
	case lwtEncapILACst:
		return ilaEncapArgs(args, i)
	case lwtEncapXfrmCst:
		return xfrmEncapArgs(args, i, resolve)
	case lwtEncapIoam6Cst:
		return ioam6EncapArgs(args, i)
	case lwtEncapRplCst:
		return rplEncapArgs(args, i)
	}
	return xtcpnl.LwtEncap{}, i, fmt.Errorf("encap %s: %w", typ, ErrNotImplemented)
}

// mplsEncapArgs is parse_encap_mpls (ip/iproute_lwtunnel.c:1590) from the
// label stack at args[i]: get_addr in AF_MPLS, which exits 1 on a bad one,
// then any number of `ttl N` (get_u8 base 0, duparg2 on a second). It
// returns the index of the last argument it used.
func mplsEncapArgs(args []string, i int) (xtcpnl.MplsEncap, int, error) {
	var e xtcpnl.MplsEncap
	label := args[i]
	// __get_addr_1 refuses default, all and any in AF_MPLS before
	// mpls_pton; mpls_pton would too, since none starts with a digit.
	var tooMany, ok bool
	e.Dst, tooMany, ok = mplsPton(label)
	if !ok {
		msg := fmt.Sprintf("Error: mpls address is expected rather than %q.", label)
		if tooMany {
			msg = "Error: too many labels.\n" + msg
		}
		return e, i, fmt.Errorf("%w: %s", ErrInvalidArgument, msg)
	}
	for i+1 < len(args) && args[i+1] == kindKeyTTL {
		var err error
		if i, err = nextArg(args, i+1); err != nil {
			return e, i, err
		}
		if e.HasTTL {
			return e, i, duparg2Err(kindKeyTTL, args[i])
		}
		if e.TTL, ok = getU8(args[i], 0); !ok {
			return e, i, invargErr("\"ttl\" value is invalid\n", args[i])
		}
		e.HasTTL = true
	}
	return e, i, nil
}

// ipEncapParse is parse_encap_ip's or parse_encap_ip6's state: the encap
// so far, and the words seen, for duparg2.
type ipEncapParse struct {
	e    xtcpnl.IPEncap
	seen map[string]bool
	// The words for LWTUNNEL_IP_TOS and LWTUNNEL_IP_TTL: tos and ttl, or
	// IPv6's tc and hoplimit.
	tos, ttl string
}

// ipEncapArgs is parse_encap_ip (ip/iproute_lwtunnel.c:1817) for AF_INET
// and parse_encap_ip6 (2029) for AF_INET6, from args[i]: strcmp words until
// one it doesn't know, which ends the type. Each word's second use is
// duparg2, which for a valued word names its value. It returns the index
// of the last word used: the type's own, when the first is unknown.
func ipEncapArgs(args []string, i int, family uint8) (xtcpnl.LwtEncap, int, error) {
	p := ipEncapParse{e: xtcpnl.IPEncap{Family: family}, seen: map[string]bool{}, tos: tosKeywordCst, ttl: kindKeyTTL}
	if family == unix.AF_INET6 {
		p.tos, p.ttl = "tc", kindKeyHoplimit
	}
	for ; i < len(args); i++ {
		var err error
		switch w := args[i]; w {
		case lwtKeyID, lwtKeyDst, lwtKeySrc, p.tos, p.ttl:
			i, err = p.valued(args, i)
		case lwtKeyGeneve, lwtKeyVxlan, lwtKeyErspan:
			i, err = p.opts(args, i)
		default:
			bit, ok := lwtTunnelFlagWords[w]
			if !ok {
				return xtcpnl.LwtEncap{IP: &p.e}, i - 1, nil
			}
			if p.seen[w] {
				return xtcpnl.LwtEncap{}, i, duparg2Err(w, w)
			}
			p.seen[w] = true
			p.e.Flags |= bit
		}
		if err != nil {
			return xtcpnl.LwtEncap{}, i, err
		}
	}
	return xtcpnl.LwtEncap{IP: &p.e}, len(args) - 1, nil
}

// valued is one of the words with a value: NEXT_ARG, then duparg2 on a
// second use, then the value's parser and its invarg.
func (p *ipEncapParse) valued(args []string, i int) (int, error) {
	w := args[i]
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v := args[i]
	if p.seen[w] {
		return i, duparg2Err(w, v)
	}
	p.seen[w] = true
	it := xtcpnl.IPEncapItem{}
	ok := true
	switch w {
	case lwtKeyID:
		it.Kind = xtcpnl.LwtunnelIPID
		it.U64, ok = getU64(v, 0)
	case lwtKeyDst, lwtKeySrc:
		it.Kind = xtcpnl.LwtunnelIPDst
		if w == lwtKeySrc {
			it.Kind = xtcpnl.LwtunnelIPSrc
		}
		a, err := routeGetAddr(v, p.e.Family)
		if err != nil {
			return i, err
		}
		it.Addr = a.data
	case p.tos:
		it.Kind = xtcpnl.LwtunnelIPTos
		it.U8, ok = rtnlDsfieldA2N(v)
	default: // p.ttl
		it.Kind = xtcpnl.LwtunnelIPTTL
		it.U8, ok = getU8(v, 0)
	}
	if !ok {
		return i, invargErr(fmt.Sprintf("%q value is invalid\n", w), v)
	}
	p.e.Items = append(p.e.Items, it)
	return i, nil
}

// opts is geneve_opts, vxlan_opts or erspan_opts: one counter for all
// three, checked before NEXT_ARG, so duparg2 names the second word itself.
// A refusal's invarg prints the value as strsep left it, cut at its first
// ',' or ':'.
func (p *ipEncapParse) opts(args []string, i int) (int, error) {
	w := args[i]
	if p.seen[lwtKeyOpts] {
		return i, duparg2Err(lwtKeyOpts, w)
	}
	p.seen[lwtKeyOpts] = true
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v := args[i]
	var o xtcpnl.IPEncapOpts
	var ok bool
	switch w {
	case lwtKeyGeneve:
		o, ok = geneveOptsArg(v)
	case lwtKeyVxlan:
		o.Kind = xtcpnl.LwtunnelIPOptsVxlan
		o.VxlanGBP, ok = getU32(v, 0)
	default:
		o, ok = erspanOptsArg(v)
	}
	if !ok {
		cut, _, _ := strings.Cut(v, ",")
		cut, _, _ = strings.Cut(cut, ":")
		return i, invargErr(fmt.Sprintf("%q value is invalid\n", w), cut)
	}
	p.e.Items = append(p.e.Items, xtcpnl.IPEncapItem{Kind: xtcpnl.LwtunnelIPOpts, Opts: o})
	return i, nil
}

// geneveOptsArg is lwtunnel_parse_geneve_opts (ip/iproute_lwtunnel.c:1708):
// each ','-separated option is lwtunnel_parse_geneve_opt (1639), whose
// ':'-separated fields are CLASS (get_be16 base 0), TYPE (get_u8 base 0)
// and DATA (hex2mem of strlen/2 bytes). An empty field is skipped, and a
// fourth, empty or not, is "Unknown "geneve_opts" type".
func geneveOptsArg(v string) (xtcpnl.IPEncapOpts, bool) {
	o := xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsGeneve}
	for opt := range strings.SplitSeq(v, ",") {
		var g xtcpnl.GeneveOpt
		for n, f := range strings.Split(opt, ":") {
			ok := true
			switch {
			case n > 2:
				return o, false
			case f == "":
			case n == 0:
				g.Class, ok = getU16(f, 0)
				g.HasClass = true
			case n == 1:
				g.Type, ok = getU8(f, 0)
				g.HasType = true
			default:
				g.Data, ok = hex2mem(f, len(f)/2)
				g.HasData = true
			}
			if !ok {
				return o, false
			}
		}
		o.Geneve = append(o.Geneve, g)
	}
	return o, true
}

// erspanOptsArg is lwtunnel_parse_erspan_opts (ip/iproute_lwtunnel.c:1741):
// VER, INDEX (get_be32 base 0), DIR and HWID (get_u8 base 0), ':'-separated.
// An empty field is skipped, and a fifth is refused.
func erspanOptsArg(v string) (xtcpnl.IPEncapOpts, bool) {
	o := xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsErspan}
	s := &o.Erspan
	for n, f := range strings.Split(v, ":") {
		ok := true
		switch {
		case n > 3:
			return o, false
		case f == "":
		case n == 0:
			s.Ver, ok = getU8(f, 0)
			s.HasVer = true
		case n == 1:
			s.Index, ok = getU32(f, 0)
			s.HasIndex = true
		case n == 2:
			s.Dir, ok = getU8(f, 0)
			s.HasDir = true
		default:
			s.Hwid, ok = getU8(f, 0)
			s.HasHwid = true
		}
		if !ok {
			return o, false
		}
	}
	return o, true
}

// hex2mem is lib/utils.c's: count bytes from pairs of hex digits, either
// case, refusing anything else. A trailing odd digit is past count and not
// read.
func hex2mem(s string, count int) ([]byte, bool) {
	out := make([]byte, count)
	for i := range out {
		hi, okHi := hexDigit(s[2*i])
		lo, okLo := hexDigit(s[2*i+1])
		if !okHi || !okLo {
			return nil, false
		}
		out[i] = hi<<4 | lo
	}
	return out, true
}

// hexDigit is get_hex: a hex digit's value, or false.
func hexDigit(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
