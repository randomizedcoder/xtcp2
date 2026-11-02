package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"slices"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// What the IP tunnels' parse_opts share (ip/link_gre.c, link_gre6.c,
// link_iptnl.c, link_ip6tnl.c, link_vti.c, link_vti6.c and ip/tunnel.c,
// iproute2 v7.2.0; KG4, docs/netlink/goip-link-kinds.md): the set's
// prefetch, tnl_parse_key, the encap words and the usage line every
// unknown word prints.
//
//   - Every parser but xfrm's reads the device back before a set, change
//     or delete whose kind has words, and refuses with `Failed to get
//     existing tunnel info.` (1) when the reply has no IFLA_LINKINFO, no
//     IFLA_INFO_DATA, or an endpoint get_addr_rta can't read.
//   - No tunnel parser has a check_duparg: a repeated word overwrites.
//   - An unknown word, help and "" among them, prints the usage and
//     returns -1 (1).

// linkDataFetcher is the prefetch's get, Service.LinkInfoDataByIndex.
type linkDataFetcher func(family uint8, index int32) (service.LinkInfoData, error)

// kindEnv is what a kind's parse_opt sees besides its words: where its
// warnings go, the verb, preferred_family, the set's ifindex (0 on an
// add), ll_name_to_index and the prefetch.
type kindEnv struct {
	errOut  io.Writer
	verb    *linkWriteVerb
	family  uint8
	index   int32
	resolve linkResolver
	fetch   linkDataFetcher
}

// The tunnel words several parsers spell the same way.
const (
	tnlKeyKey      = "key"
	tnlKeyIkey     = "ikey"
	tnlKeyOkey     = "okey"
	tnlKeyEncap    = "encap"
	tnlKeyFwmark   = "fwmark"
	tnlKeyIgnoreDF = "ignore-df"
	tnlKeyLocal    = addrLocalKeywordCst
)

// tunnelGetFailedText is get_failed's text.
const tunnelGetFailedText = "Failed to get existing tunnel info."

var errTunnelGetFailed = fmt.Errorf("%w: %s", ErrInvalidArgument, tunnelGetFailedText)

// tunnelPrefetch is the get a tunnel parser starts a set with: nothing on
// a create; otherwise the device's IFLA_INFO_DATA, never nil, after
// get_failed's checks. Each endpoint id must hold get_addr_rta's 4 or 16
// bytes, and those of family. A reply of a kind outside kinds that passes
// them is not implemented, since ip reads it by the wrong ids.
func (env *kindEnv) tunnelPrefetch(kinds []string, family uint8, endpoints ...uint16) ([]byte, error) {
	if env.verb.create() {
		return nil, nil
	}
	if env.fetch == nil {
		return nil, fmt.Errorf("tunnel prefetch with no fetcher: %w", ErrNotImplemented)
	}
	d, err := env.fetch(env.family, env.index)
	if err != nil {
		var errno unix.Errno
		if errors.As(err, &errno) {
			return nil, errTunnelGetFailed
		}
		return nil, err
	}
	if !d.HasLinkInfo || !d.HasInfoData {
		return nil, errTunnelGetFailed
	}
	want := 4
	if family == unix.AF_INET6 {
		want = 16
	}
	bad := false
	werr := xtcpnl.WalkRTAttrsNested(d.Data, func(atype uint16, val []byte) {
		if slices.Contains(endpoints, atype&^unix.NLA_F_NESTED) && len(val) != want {
			bad = true
		}
	})
	if werr != nil {
		return nil, fmt.Errorf("goip: decode IFLA_INFO_DATA: %w", werr)
	}
	if bad {
		return nil, errTunnelGetFailed
	}
	if !slices.Contains(kinds, d.Kind) {
		return nil, fmt.Errorf("a %s parser's prefetch of a %q device: %w", kinds[0], d.Kind, ErrNotImplemented)
	}
	if d.Data == nil {
		return []byte{}, nil
	}
	return d.Data, nil
}

// tunnelBool is a flag word's 1 or 0.
func tunnelBool(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// tunnelUsageErr is a tunnel parser's usage, of which goip prints the
// first line.
func tunnelUsageErr(line string) error {
	return fmt.Errorf("%w: %s", ErrInvalidArgument, line)
}

// tnlParseKey is tnl_parse_key (ip/tunnel.c:164-178): a value with a '.' is
// get_addr32's address, read big-endian, whose failure is exit(1);
// anything else is get_unsigned base 0, whose failure is exit(-1) naming
// the keyword.
func tnlParseKey(name, v string) (uint64, error) {
	if strings.IndexByte(v, '.') >= 0 {
		a, ok := getAddrIPv4Parts(v)
		if !ok {
			return 0, fmt.Errorf("%w: Error: an IP address is expected rather than %q", ErrInvalidArgument, v)
		}
		b := a.As4()
		return uint64(binary.BigEndian.Uint32(b[:])), nil
	}
	n, ok := getU32(v, 0)
	if !ok {
		return 0, linkUsage(fmt.Errorf("%w: invalid value for %q: %q; it should be an unsigned integer",
			ErrInvalidArgument, name, v))
	}
	return uint64(n), nil
}

// tunnelU8 is get_u8 base 0, whose failure is invarg with text.
func tunnelU8(v, text string) (uint64, error) {
	n, ok := getU8(v, 0)
	if !ok {
		return 0, linkUsage(invargErr(text, v))
	}
	return uint64(n), nil
}

// tunnelAddr is get_addr with the kind's fixed family: the zero Addr for
// any, all, default and the zero address, which ip doesn't send.
func tunnelAddr(v string, family uint8) (netip.Addr, error) {
	p, err := routeGetAddr(v, family)
	if err != nil {
		return netip.Addr{}, err
	}
	a, ok := netip.AddrFromSlice(p.data)
	if !ok || a.IsUnspecified() {
		return netip.Addr{}, nil
	}
	return a, nil
}

// tunnelEncapOp is what an encap word sets.
type tunnelEncapOp uint8

const (
	tunnelEncapType tunnelEncapOp = iota + 1
	tunnelEncapFlag
	tunnelEncapSport
	tunnelEncapDport
)

// tunnelEncapSet is one encap word: the op, its value (a flag's 1 or 0) and
// a flag's TUNNEL_ENCAP_FLAG_* bit.
type tunnelEncapSet struct {
	op    tunnelEncapOp
	value uint64
	bit   uint16
}

// tunnelEncapFlags are the six strcmp flag words. The usage's
// `[no]encap-csum6` is not one of them.
var tunnelEncapFlags = map[string]tunnelEncapSet{
	"encap-csum":        {tunnelEncapFlag, 1, xtcpnl.TunnelEncapFlagCsum},
	"noencap-csum":      {tunnelEncapFlag, 0, xtcpnl.TunnelEncapFlagCsum},
	"encap-udp6-csum":   {tunnelEncapFlag, 1, xtcpnl.TunnelEncapFlagCsum6},
	"noencap-udp6-csum": {tunnelEncapFlag, 0, xtcpnl.TunnelEncapFlagCsum6},
	"encap-remcsum":     {tunnelEncapFlag, 1, xtcpnl.TunnelEncapFlagRemcsum},
	"noencap-remcsum":   {tunnelEncapFlag, 0, xtcpnl.TunnelEncapFlagRemcsum},
}

// tunnelEncapTypes are encap's strcmp values.
var tunnelEncapTypes = map[string]uint64{
	"fou":  xtcpnl.TunnelEncapFOU,
	"gue":  xtcpnl.TunnelEncapGUE,
	"none": xtcpnl.TunnelEncapNone,
}

// tunnelEncapWord reads word if it is one of the strcmp encap words,
// taking its value from next. ok is false for any other word.
func tunnelEncapWord(word string, next func() (string, error)) (s tunnelEncapSet, ok bool, err error) {
	if f, isFlag := tunnelEncapFlags[word]; isFlag {
		return f, true, nil
	}
	switch word {
	case "noencap":
		return tunnelEncapSet{op: tunnelEncapType, value: xtcpnl.TunnelEncapNone}, true, nil
	case tnlKeyEncap, "encap-sport", "encap-dport":
	default:
		return tunnelEncapSet{}, false, nil
	}
	v, err := next()
	if err != nil {
		return tunnelEncapSet{}, true, err
	}
	switch word {
	case tnlKeyEncap:
		t, known := tunnelEncapTypes[v]
		if !known {
			return tunnelEncapSet{}, true, linkUsage(invargErr("Invalid encap type.", v))
		}
		return tunnelEncapSet{op: tunnelEncapType, value: t}, true, nil
	case "encap-sport":
		if v == netnsAutoCst {
			return tunnelEncapSet{op: tunnelEncapSport}, true, nil
		}
		n, valid := getU16(v, 0)
		if !valid {
			return tunnelEncapSet{}, true, linkUsage(invargErr("Invalid source port.", v))
		}
		return tunnelEncapSet{op: tunnelEncapSport, value: uint64(n)}, true, nil
	default:
		n, valid := getU16(v, 0)
		if !valid {
			return tunnelEncapSet{}, true, linkUsage(invargErr("Invalid destination port.", v))
		}
		return tunnelEncapSet{op: tunnelEncapDport, value: uint64(n)}, true, nil
	}
}

// tunnelArgs walks a parse_opt's argv: word is the current word, and next
// is NEXT_ARG, whose end is incomplete_command (255).
type tunnelArgs struct {
	args []string
	i    int
}

func (a *tunnelArgs) more() bool   { return a.i < len(a.args) }
func (a *tunnelArgs) word() string { return a.args[a.i] }

func (a *tunnelArgs) next() (string, error) {
	if a.i+1 >= len(a.args) {
		return "", linkUsage(incompleteErr())
	}
	a.i++
	return a.args[a.i], nil
}
