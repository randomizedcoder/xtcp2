package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The lwt rows' O3 (goip-lwt-encap.md, LE3-T03). Each lwt row is one encap
// form (tools/nlcapgen/matrix_lwt.go), and its axes name what the encap
// holds: the type, each sub-attribute's value at its width and byte order,
// the option nests and the flags. A hop's axes are hopN_, for the Nth
// rtnexthop of RTA_MULTIPATH. lwtProblem fails on an axis it has no check
// for.

// lwtSpaces are the lwt spaces' requests, as kwliveSpaces.
var lwtSpaces = map[string]kwliveSpace{
	"lwtroute4": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtroute6": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwthop":    {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtnh":     {unix.RTM_NEWNEXTHOP, unix.SizeofNhmsg},
}

// lwtEncapTypes are the encap axis's words as LWTUNNEL_ENCAP_*.
var lwtEncapTypes = map[string]uint16{
	"mpls": unix.LWTUNNEL_ENCAP_MPLS,
	"ip":   unix.LWTUNNEL_ENCAP_IP,
	"ip6":  unix.LWTUNNEL_ENCAP_IP6,
}

// lwtOptKinds are the opts axis's words as LWTUNNEL_IP_OPTS_*.
var lwtOptKinds = map[string]uint16{
	"geneve": LwtunnelIPOptsGeneve,
	"vxlan":  LwtunnelIPOptsVxlan,
	"erspan": LwtunnelIPOptsErspan,
}

// lwtEncap is one encap's two attributes, from a request or a hop.
type lwtEncap struct {
	typ  uint16
	nest map[uint16][]byte
	opts []byte // LWTUNNEL_IP_OPTS' payload, nil if none
}

// lwtFind is the encap among attrs: encapAttr's nest and typeAttr's u16.
func lwtFind(attrs map[uint16][]byte, encapAttr, typeAttr uint16) (lwtEncap, error) {
	t, err := kwFixed(attrs, typeAttr, 2)
	if err != nil {
		return lwtEncap{}, err
	}
	raw, ok := attrs[encapAttr]
	if !ok {
		return lwtEncap{}, fmt.Errorf("no attribute %d", encapAttr)
	}
	nest, err := kwAttrs(raw)
	if err != nil {
		return lwtEncap{}, err
	}
	return lwtEncap{typ: binary.NativeEndian.Uint16(t), nest: nest, opts: nest[LwtunnelIPOpts]}, nil
}

// lwtHop returns RTA_MULTIPATH's nth rtnexthop's attributes, from 1.
func lwtHop(attrs map[uint16][]byte, n int) (map[uint16][]byte, error) {
	mp, ok := attrs[unix.RTA_MULTIPATH]
	if !ok {
		return nil, errors.New("no RTA_MULTIPATH")
	}
	for i := 1; len(mp) >= unix.SizeofRtNexthop; i++ {
		l := int(binary.NativeEndian.Uint16(mp))
		if l < unix.SizeofRtNexthop || l > len(mp) {
			return nil, fmt.Errorf("rtnh_len %d of %d", l, len(mp))
		}
		if i == n {
			return kwAttrs(mp[unix.SizeofRtNexthop:l])
		}
		mp = mp[min((l+3)&^3, len(mp)):]
	}
	return nil, fmt.Errorf("no hop %d", n)
}

// lwtProblem checks an lwt request body against the row's axes.
func lwtProblem(space string, ax map[string]string, flags uint16, body []byte) error {
	sp, ok := lwtSpaces[space]
	if !ok {
		return fmt.Errorf("space %q", space)
	}
	if len(body) < sp.hdrLen {
		return fmt.Errorf("body %d bytes, header %d", len(body), sp.hdrLen)
	}
	attrs, err := kwAttrs(body[sp.hdrLen:])
	if err != nil {
		return err
	}
	encapAttr, typeAttr := uint16(unix.RTA_ENCAP), uint16(unix.RTA_ENCAP_TYPE)
	if sp.typ == unix.RTM_NEWNEXTHOP {
		encapAttr, typeAttr = NhaEncap, NhaEncapType
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range ax {
			if !yield(k) {
				return
			}
		}
	})
	for _, k := range keys {
		if err := lwtAxis(k, ax, flags, attrs, encapAttr, typeAttr); err != nil {
			return fmt.Errorf("%s=%s: %w", k, ax[k], err)
		}
	}
	return nil
}

// lwtAxis checks one axis: the request's own, or its encap's, or a hop's
// encap's.
func lwtAxis(k string, ax map[string]string, flags uint16, attrs map[uint16][]byte, encapAttr, typeAttr uint16) error {
	val := ax[k]
	switch k {
	case "verb":
		return kwWantVerb(flags, val)
	case "id":
		return kwWantU32(attrs, NhaID, val)
	}
	prefix := ""
	if h, rest, ok := strings.Cut(k, "_"); ok && strings.HasPrefix(h, "hop") {
		n, err := strconv.Atoi(strings.TrimPrefix(h, "hop"))
		if err != nil {
			return err
		}
		if attrs, err = lwtHop(attrs, n); err != nil {
			return err
		}
		prefix, k = h+"_", rest
	}
	e, err := lwtFind(attrs, encapAttr, typeAttr)
	if err != nil {
		return err
	}
	typ := ax[prefix+"encap"]
	if k == "encap" {
		if want, ok := lwtEncapTypes[val]; !ok || e.typ != want {
			return fmt.Errorf("encap type %d", e.typ)
		}
		return nil
	}
	if typ == "mpls" {
		return lwtMplsAxis(k, val, e)
	}
	return lwtIPAxis(k, val, typ, e)
}

func lwtMplsAxis(k, val string, e lwtEncap) error {
	switch k {
	case "labels":
		return kwWantLabels(e.nest, val)
	case "ttl":
		return lwtWantU8(e.nest, unix.MPLS_IPTUNNEL_TTL, val)
	}
	return errors.New("no O3 check for this axis")
}

// lwtIPAxis checks an ip or ip6 encap's axis. ID, FLAGS, geneve's CLASS
// and erspan's INDEX are big-endian; vxlan's GBP is host order.
func lwtIPAxis(k, val, typ string, e lwtEncap) error {
	switch k {
	case "tun_id":
		b, err := kwFixed(e.nest, LwtunnelIPID, 8)
		if err != nil {
			return err
		}
		if strconv.FormatUint(binary.BigEndian.Uint64(b), 10) != val {
			return fmt.Errorf("LWTUNNEL_IP_ID %x", b)
		}
		return nil
	case "dst":
		return lwtWantAddr(e.nest, LwtunnelIPDst, typ, val)
	case "src":
		return lwtWantAddr(e.nest, LwtunnelIPSrc, typ, val)
	case "tos":
		return lwtWantU8(e.nest, LwtunnelIPTos, val)
	case "ttl":
		return lwtWantU8(e.nest, LwtunnelIPTTL, val)
	case "flags":
		b, err := kwFixed(e.nest, LwtunnelIPFlags, 2)
		if err != nil {
			return err
		}
		if strconv.Itoa(int(binary.BigEndian.Uint16(b))) != val {
			return fmt.Errorf("LWTUNNEL_IP_FLAGS % x", b)
		}
		return nil
	}
	geneve, isGeneve := strings.CutPrefix(k, "geneve")
	if !isGeneve && !slices.Contains([]string{"opts", "gbp", "ver", "index"}, k) {
		return errors.New("no O3 check for this axis")
	}
	opts, err := ParseNetlinkAttributes(e.opts)
	if err != nil {
		return err
	}
	if len(opts) == 0 {
		return errors.New("no LWTUNNEL_IP_OPTS")
	}
	switch k {
	case "opts":
		want := lwtOptKinds[val]
		for i := range opts {
			if opts[i].ID() != want || opts[i].Type&unix.NLA_F_NESTED == 0 {
				return fmt.Errorf("option %d is type %#x", i, opts[i].Type)
			}
		}
		return nil
	case "gbp":
		v, err := lwtOptNest(opts, 0)
		if err != nil {
			return err
		}
		return kwWantU32(v, LwtunnelIPOptVxlanGBP, val)
	case "ver":
		v, err := lwtOptNest(opts, 0)
		if err != nil {
			return err
		}
		return lwtWantU8(v, LwtunnelIPOptErspanVer, val)
	case "index":
		v, err := lwtOptNest(opts, 0)
		if err != nil {
			return err
		}
		b, err := kwFixed(v, LwtunnelIPOptErspanIndex, 4)
		if err != nil {
			return err
		}
		if strconv.FormatUint(uint64(binary.BigEndian.Uint32(b)), 10) != val {
			return fmt.Errorf("ERSPAN INDEX % x", b)
		}
		return nil
	}
	i, err := strconv.Atoi(geneve)
	if err != nil {
		return err
	}
	return lwtWantGeneve(opts, i-1, val)
}

// lwtOptNest is the ith option nest's members.
func lwtOptNest(opts []NetlinkAttribute, i int) (map[uint16][]byte, error) {
	if i >= len(opts) {
		return nil, fmt.Errorf("%d options, want option %d", len(opts), i+1)
	}
	return kwAttrs(opts[i].Data)
}

// lwtWantGeneve checks the ith geneve option against CLASS:TYPE:DATA, the
// class and type in decimal and the data in hex.
func lwtWantGeneve(opts []NetlinkAttribute, i int, val string) error {
	g, err := lwtOptNest(opts, i)
	if err != nil {
		return err
	}
	f := strings.Split(val, ":")
	if len(f) != 3 {
		return fmt.Errorf("axis %q", val)
	}
	c, err := kwFixed(g, LwtunnelIPOptGeneveClass, 2)
	if err != nil {
		return err
	}
	if strconv.Itoa(int(binary.BigEndian.Uint16(c))) != f[0] {
		return fmt.Errorf("geneve CLASS % x", c)
	}
	if err := lwtWantU8(g, LwtunnelIPOptGeneveType, f[1]); err != nil {
		return err
	}
	if d := hex.EncodeToString(g[LwtunnelIPOptGeneveData]); d != f[2] {
		return fmt.Errorf("geneve DATA %s", d)
	}
	return nil
}

func lwtWantU8(attrs map[uint16][]byte, typ uint16, val string) error {
	got, err := kwU8(attrs, typ)
	if err != nil {
		return err
	}
	if strconv.Itoa(int(got)) != val {
		return fmt.Errorf("attribute %d is %d", typ, got)
	}
	return nil
}

// lwtWantAddr checks DST or SRC is the axis's address, 4 bytes for ip and
// 16 for ip6.
func lwtWantAddr(attrs map[uint16][]byte, typ uint16, encap, val string) error {
	a, err := netip.ParseAddr(val)
	if err != nil {
		return err
	}
	if a.Is6() != (encap == "ip6") {
		return fmt.Errorf("a %s address in %s", val, encap)
	}
	want := a.AsSlice()
	got, err := kwFixed(attrs, typ, len(want))
	if err != nil {
		return err
	}
	if !slices.Equal(got, want) {
		return fmt.Errorf("attribute %d is %x", typ, got)
	}
	return nil
}

func TestLwtProblem(t *testing.T) {
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	rt := func(family byte) []byte { return kwRtHdr(family, 0, 254) }
	nhm := make([]byte, unix.SizeofNhmsg)
	ipEncap := func(e IPEncap) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error { return PutIPEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, e) }
	}
	// raw is RTA_ENCAP holding sub, then RTA_ENCAP_TYPE typ, for wire
	// shapes PutIPEncap won't write.
	raw := func(typ uint16, sub func(ab *AttrBuilder) error) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			n, err := ab.BeginNest(unix.RTA_ENCAP | unix.NLA_F_NESTED)
			if err != nil {
				return err
			}
			if err := sub(ab); err != nil {
				return err
			}
			if err := ab.EndNest(n); err != nil {
				return err
			}
			return ab.PutU16(unix.RTA_ENCAP_TYPE, typ)
		}
	}
	dst4 := IPEncapItem{Kind: LwtunnelIPDst, Addr: []byte{203, 0, 113, 5}}
	id1 := IPEncapItem{Kind: LwtunnelIPID, U64: 1}
	g := func(class uint16, typ uint8, data ...byte) GeneveOpt {
		return GeneveOpt{Class: class, Type: typ, Data: data, HasClass: true, HasType: true, HasData: true}
	}
	opts := func(o IPEncapOpts) IPEncapItem { return IPEncapItem{Kind: LwtunnelIPOpts, Opts: o} }
	v4 := func(flags uint16, items ...IPEncapItem) IPEncap {
		return IPEncap{Family: unix.AF_INET, Items: items, Flags: flags}
	}
	hops := func(hop ...[]byte) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error { return ab.PutBytes(unix.RTA_MULTIPATH, slices.Concat(hop...)) }
	}
	mpls := func(dst []byte, ttl uint8, hasTTL bool) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			return PutMplsEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, MplsEncap{Dst: dst, TTL: ttl, HasTTL: hasTTL})
		}
	}
	ipAx := func(kv ...string) map[string]string {
		return lwtTestFacts(append([]string{"encap", "ip"}, kv...)...)
	}
	cases := []struct {
		description string
		space       string
		axes        map[string]string
		body        []byte
		expected    string // "" for no problem, else a substring of it
	}{
		{"positive: mpls, a stack and ttl", "lwtroute4", lwtTestFacts("verb", "add", "encap", "mpls", "labels", "100/200", "ttl", "10"),
			kwBody(t, rt(unix.AF_INET), mpls(kwMplsDst(1, 100, 200), 10, true)), ""},
		{"boundary: mpls ttl 0", "lwtroute4", lwtTestFacts("encap", "mpls", "ttl", "0"),
			kwBody(t, rt(unix.AF_INET), mpls(kwMplsDst(0, 100), 0, true)), ""},
		{"negative: mpls ttl 0 not sent", "lwtroute4", lwtTestFacts("encap", "mpls", "ttl", "0"),
			kwBody(t, rt(unix.AF_INET), mpls(kwMplsDst(0, 100), 0, false)), "no attribute 2"},
		{"positive: ip, every scalar and the flags", "lwtroute4",
			ipAx("tun_id", "1", "dst", "203.0.113.5", "src", "192.0.2.10", "tos", "16", "ttl", "64", "flags", "13"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(LwtTunnelKey|LwtTunnelCsum|LwtTunnelSeq, id1, dst4,
				IPEncapItem{Kind: LwtunnelIPSrc, Addr: []byte{192, 0, 2, 10}}, IPEncapItem{Kind: LwtunnelIPTos, U8: 16},
				IPEncapItem{Kind: LwtunnelIPTTL, U8: 64}))), ""},
		{"negative: the flags little-endian", "lwtroute4", ipAx("flags", "13"),
			kwBody(t, rt(unix.AF_INET), raw(unix.LWTUNNEL_ENCAP_IP, func(ab *AttrBuilder) error {
				return ab.PutU16(LwtunnelIPFlags, 13)
			})), "LWTUNNEL_IP_FLAGS 0d 00"},
		{"negative: the id little-endian", "lwtroute4", ipAx("tun_id", "1"),
			kwBody(t, rt(unix.AF_INET), raw(unix.LWTUNNEL_ENCAP_IP, func(ab *AttrBuilder) error {
				return ab.PutBytes(LwtunnelIPID, binary.LittleEndian.AppendUint64(nil, 1))
			})), "LWTUNNEL_IP_ID"},
		{"negative: the encap type ip6 for ip", "lwtroute4", ipAx(),
			kwBody(t, rt(unix.AF_INET), raw(unix.LWTUNNEL_ENCAP_IP6, func(*AttrBuilder) error { return nil })), "encap type 4"},
		{"positive: two geneve options", "lwtroute4", ipAx("opts", "geneve", "geneve1", "258:128:00112233", "geneve2", "258:129:44556677"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, opts(IPEncapOpts{Kind: LwtunnelIPOptsGeneve,
				Geneve: []GeneveOpt{g(0x102, 0x80, 0, 0x11, 0x22, 0x33), g(0x102, 0x81, 0x44, 0x55, 0x66, 0x77)}})))), ""},
		{"negative: geneve's class little-endian", "lwtroute4", ipAx("geneve1", "258:128:00112233"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, opts(IPEncapOpts{Kind: LwtunnelIPOptsGeneve,
				Geneve: []GeneveOpt{g(0x201, 0x80, 0, 0x11, 0x22, 0x33)}})))), "geneve CLASS 02 01"},
		{"positive: vxlan's gbp", "lwtroute4", ipAx("opts", "vxlan", "gbp", "2048"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, opts(IPEncapOpts{Kind: LwtunnelIPOptsVxlan, VxlanGBP: 2048})))), ""},
		{"positive: erspan's ver and index", "lwtroute4", ipAx("opts", "erspan", "ver", "1", "index", "4660"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, opts(IPEncapOpts{Kind: LwtunnelIPOptsErspan,
				Erspan: ErspanOpt{Ver: 1, Index: 0x1234, HasVer: true, HasIndex: true}})))), ""},
		{"negative: erspan's INDEX little-endian", "lwtroute4", ipAx("index", "4660"),
			kwBody(t, rt(unix.AF_INET), raw(unix.LWTUNNEL_ENCAP_IP, func(ab *AttrBuilder) error {
				o, err := ab.BeginNest(LwtunnelIPOpts | unix.NLA_F_NESTED)
				if err != nil {
					return err
				}
				if err := putNested(ab, LwtunnelIPOptsErspan, func() error {
					return ab.PutBytes(LwtunnelIPOptErspanIndex, binary.LittleEndian.AppendUint32(nil, 0x1234))
				}); err != nil {
					return err
				}
				return ab.EndNest(o)
			})), "ERSPAN INDEX 34 12"},
		{"negative: an option kind not the axis", "lwtroute4", ipAx("opts", "geneve"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, opts(IPEncapOpts{Kind: LwtunnelIPOptsVxlan, VxlanGBP: 1})))), "option 0"},
		{"positive: ip6's dst, 16 bytes", "lwtroute6", lwtTestFacts("encap", "ip6", "dst", "2001:db8:9::1"),
			kwBody(t, rt(unix.AF_INET6), func(ab *AttrBuilder) error {
				return PutIPEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, IPEncap{Family: unix.AF_INET6,
					Items: []IPEncapItem{{Kind: LwtunnelIPDst, Addr: netip.MustParseAddr("2001:db8:9::1").AsSlice()}}})
			}), ""},
		{"positive: the second hop's encap", "lwthop", lwtTestFacts("hop2_encap", "ip", "hop2_tun_id", "2"),
			kwBody(t, rt(unix.AF_INET), hops(kwHop(t, 0, nil), kwHop(t, 0, ipEncap(v4(0, IPEncapItem{Kind: LwtunnelIPID, U64: 2}))))), ""},
		{"negative: a hop's encap on the route", "lwthop", lwtTestFacts("hop1_encap", "ip"),
			kwBody(t, rt(unix.AF_INET), func(ab *AttrBuilder) error {
				if err := ipEncap(v4(0, id1))(ab); err != nil {
					return err
				}
				return hops(kwHop(t, 0, nil))(ab)
			}), "no attribute 21"},
		{"negative: no second hop", "lwthop", lwtTestFacts("hop2_encap", "ip"),
			kwBody(t, rt(unix.AF_INET), hops(kwHop(t, 0, nil))), "no hop 2"},
		{"positive: a nexthop's id apart from the tunnel's", "lwtnh", lwtTestFacts("id", "61", "encap", "ip", "tun_id", "1"),
			kwBody(t, nhm, func(ab *AttrBuilder) error {
				if err := ab.PutU32(NhaID, 61); err != nil {
					return err
				}
				return PutIPEncap(ab, NhaEncap, NhaEncapType, v4(0, id1))
			}), ""},
		{"corner: an axis with no check", "lwtroute4", ipAx("as", "1"),
			kwBody(t, rt(unix.AF_INET), ipEncap(v4(0, id1))), "no O3 check"},
		{"corner: a space not in the family", "kwnh", lwtTestFacts(), nil, `space "kwnh"`},
		{"boundary: a body shorter than its header", "lwtroute4", lwtTestFacts(), make([]byte, 4), "header 12"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := lwtProblem(tc.space, tc.axes, add, tc.body)
			switch {
			case tc.expected == "" && err != nil:
				t.Errorf("problem %v, want none", err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Errorf("problem %v, want one with %q", err, tc.expected)
			}
		})
	}
}

func lwtTestFacts(kv ...string) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

// TestWsLwtCorpus runs lwtProblem over every captured lwt row that sent a
// request: one each, but for the four rc 255 rows, which O2 holds to none.
func TestWsLwtCorpus(t *testing.T) {
	var checked, noRequest int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		space, _, _ := strings.Cut(row.ID, "/")
		sp, ok := lwtSpaces[space]
		if !ok {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), sp.typ)
			switch {
			case len(reqs) == 0 && row.Expect.RC != nil && *row.Expect.RC == 255:
				noRequest++
				return
			case len(reqs) != 1:
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), sp.typ, row.Cmd)
			}
			checked++
			if err := lwtProblem(space, row.Axes, reqs[0].h.Flags, reqs[0].body); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d lwt requests; %d refusals sent none", checked, noRequest)
	if checked != 35 || noRequest != 4 {
		t.Errorf("O3 checked %d lwt requests and %d refusals, want 35 and 4", checked, noRequest)
	}
}
