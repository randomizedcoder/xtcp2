package xtcpnl

import (
	"encoding/binary"
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

// The kwlive rows' O3 (goip-live-forms.md, LF2-T02). Each kwlive row is
// one keyword form (tools/nlcapgen/matrix_kwlive.go), and its axes name
// what the form sends; kwliveProblem checks the request carries each, at
// its width, and fails on an axis it has no check for.

// kwliveSpace is a kwlive space's request: its message type and the
// length of the family header before the attributes.
type kwliveSpace struct {
	typ    uint16
	hdrLen int
}

var kwliveSpaces = map[string]kwliveSpace{
	"kwroute4": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"kwroute6": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"kwlink":   {unix.RTM_NEWLINK, unix.SizeofIfInfomsg},
	"kwnh":     {unix.RTM_NEWNEXTHOP, unix.SizeofNhmsg},
	"kwrule":   {unix.RTM_NEWRULE, FibRuleHdrSizeCst},
}

// kwAttrs maps each attribute type to its payload, refusing a repeat.
func kwAttrs(data []byte) (map[uint16][]byte, error) {
	attrs, err := ParseNetlinkAttributes(data)
	if err != nil {
		return nil, err
	}
	out := make(map[uint16][]byte, len(attrs))
	for i := range attrs {
		id := attrs[i].ID()
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("attribute %d twice", id)
		}
		out[id] = attrs[i].Data
	}
	return out, nil
}

// kwFixed returns attribute typ's payload, which must be width bytes.
func kwFixed(attrs map[uint16][]byte, typ uint16, width int) ([]byte, error) {
	b, ok := attrs[typ]
	if !ok {
		return nil, fmt.Errorf("no attribute %d", typ)
	}
	if len(b) != width {
		return nil, fmt.Errorf("attribute %d is %d bytes, want %d", typ, len(b), width)
	}
	return b, nil
}

func kwU32(attrs map[uint16][]byte, typ uint16) (uint32, error) {
	b, err := kwFixed(attrs, typ, 4)
	if err != nil {
		return 0, err
	}
	return binary.NativeEndian.Uint32(b), nil
}

func kwU8(attrs map[uint16][]byte, typ uint16) (uint8, error) {
	b, err := kwFixed(attrs, typ, 1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// kwWantU32 checks attribute typ is a u32 equal to the axis.
func kwWantU32(attrs map[uint16][]byte, typ uint16, axis string) error {
	want, err := strconv.ParseUint(axis, 10, 32)
	if err != nil {
		return err
	}
	got, err := kwU32(attrs, typ)
	if err != nil {
		return err
	}
	if uint64(got) != want {
		return fmt.Errorf("attribute %d is %d, want %d", typ, got, want)
	}
	return nil
}

// kwWantFlow checks a realms attribute: get_rt_realms_or_raw puts FROM in
// the high 16 bits and TO in the low.
func kwWantFlow(attrs map[uint16][]byte, typ uint16, axis string) error {
	want, err := strconv.ParseUint(axis, 10, 32)
	if err != nil {
		return err
	}
	got, err := kwU32(attrs, typ)
	if err != nil {
		return err
	}
	from, to := got>>16, got&0xffff
	if uint64(from) != want>>16 || uint64(to) != want&0xffff {
		return fmt.Errorf("realms %d/%d, want %d/%d", from, to, want>>16, want&0xffff)
	}
	return nil
}

// kwWantTable checks the table: iproute_modify and iprule_modify put one
// below 256 in the header's table byte, and one above in a u32 attribute
// with the byte RT_TABLE_UNSPEC.
func kwWantTable(hdrTable byte, attrs map[uint16][]byte, typ uint16, axis string) error {
	want, err := strconv.ParseUint(axis, 10, 32)
	if err != nil {
		return err
	}
	_, hasAttr := attrs[typ]
	if want < 256 {
		if uint64(hdrTable) != want || hasAttr {
			return fmt.Errorf("table byte %d (attribute %v), want %d and none", hdrTable, hasAttr, want)
		}
		return nil
	}
	if hdrTable != unix.RT_TABLE_UNSPEC {
		return fmt.Errorf("table byte %d for table %d, want %d", hdrTable, want, unix.RT_TABLE_UNSPEC)
	}
	return kwWantU32(attrs, typ, axis)
}

// kwWantVerb checks the add flags: ip's add sends NLM_F_CREATE|NLM_F_EXCL,
// and link's set neither.
func kwWantVerb(flags uint16, axis string) error {
	const add = unix.NLM_F_CREATE | unix.NLM_F_EXCL
	var want uint16
	switch axis {
	case "add":
		want = add
	case "set":
	default:
		return fmt.Errorf("verb %q", axis)
	}
	if flags&add != want {
		return fmt.Errorf("flags %#x for %s", flags, axis)
	}
	return nil
}

// kwFirstHop returns RTA_MULTIPATH's first rtnexthop and its attributes.
func kwFirstHop(attrs map[uint16][]byte) (flags uint8, hop map[uint16][]byte, err error) {
	mp, ok := attrs[unix.RTA_MULTIPATH]
	if !ok {
		return 0, nil, errors.New("no RTA_MULTIPATH")
	}
	if len(mp) < unix.SizeofRtNexthop {
		return 0, nil, fmt.Errorf("RTA_MULTIPATH is %d bytes", len(mp))
	}
	n := int(binary.NativeEndian.Uint16(mp))
	if n < unix.SizeofRtNexthop || n > len(mp) {
		return 0, nil, fmt.Errorf("first rtnh_len %d of %d", n, len(mp))
	}
	hop, err = kwAttrs(mp[unix.SizeofRtNexthop:n])
	return mp[2], hop, err
}

// kwWantVia checks an RTA_VIA: a u16 family, then that family's address.
func kwWantVia(hop map[uint16][]byte, axis string) error {
	want, err := strconv.ParseUint(axis, 10, 16)
	if err != nil {
		return err
	}
	b, ok := hop[unix.RTA_VIA]
	if !ok || len(b) < 2 {
		return errors.New("no RTA_VIA in the first hop")
	}
	fam := binary.NativeEndian.Uint16(b)
	alen := map[uint16]int{unix.AF_INET: 4, unix.AF_INET6: 16}[fam]
	if uint64(fam) != want || len(b) != 2+alen {
		return fmt.Errorf("RTA_VIA family %d, %d bytes, want %d", fam, len(b), want)
	}
	return nil
}

// kwWantLabels checks MPLS_IPTUNNEL_DST: mpls_pton writes each label
// shifted 12, traffic class and ttl 0, and the bottom-of-stack bit on the
// last entry alone.
func kwWantLabels(encap map[uint16][]byte, axis string) error {
	dst := encap[unix.MPLS_IPTUNNEL_DST]
	labels := strings.Split(axis, "/")
	if len(dst) != 4*len(labels) {
		return fmt.Errorf("MPLS_IPTUNNEL_DST is %d bytes for %d labels", len(dst), len(labels))
	}
	for i, l := range labels {
		want, err := strconv.ParseUint(l, 10, 20)
		if err != nil {
			return err
		}
		e := binary.BigEndian.Uint32(dst[4*i:])
		bos := e&(1<<8) != 0
		if uint64(e>>12) != want || e&0xeff != 0 || bos != (i == len(labels)-1) {
			return fmt.Errorf("label entry %d is %#08x, want label %d, bottom %v", i, e, want, i == len(labels)-1)
		}
	}
	return nil
}

// kwWantGateway checks NHA_GATEWAY is the axis's address and nh_family its
// family.
func kwWantGateway(family byte, attrs map[uint16][]byte, axis string) error {
	a, err := netip.ParseAddr(axis)
	if err != nil {
		return err
	}
	wantFam := byte(unix.AF_INET)
	if a.Is6() {
		wantFam = unix.AF_INET6
	}
	want := a.AsSlice()
	got, err := kwFixed(attrs, NhaGateway, len(want))
	if err != nil {
		return err
	}
	if family != wantFam || !slices.Equal(got, want) {
		return fmt.Errorf("gateway %x family %d, want %x family %d", got, family, want, wantFam)
	}
	return nil
}

// kwliveProblem checks a kwlive request body against the row's axes.
func kwliveProblem(space string, ax map[string]string, flags uint16, body []byte) error {
	sp, ok := kwliveSpaces[space]
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
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range ax {
			if !yield(k) {
				return
			}
		}
	})
	for _, k := range keys {
		if err := kwliveAxis(sp.typ, k, ax[k], flags, body, attrs); err != nil {
			return fmt.Errorf("%s=%s: %w", k, ax[k], err)
		}
	}
	return nil
}

// kwliveAxis checks one axis of a request of type typ.
func kwliveAxis(typ uint16, k, val string, flags uint16, body []byte, attrs map[uint16][]byte) error {
	if k == "verb" {
		return kwWantVerb(flags, val)
	}
	switch typ {
	case unix.RTM_NEWROUTE:
		return kwRouteAxis(k, val, body, attrs)
	case unix.RTM_NEWLINK:
		return kwLinkAxis(k, val, body, attrs)
	case unix.RTM_NEWNEXTHOP:
		return kwNhAxis(k, val, body, attrs)
	case unix.RTM_NEWRULE:
		return kwRuleAxis(k, val, body, attrs)
	}
	return fmt.Errorf("type %d", typ)
}

func kwRouteAxis(k, val string, body []byte, attrs map[uint16][]byte) error {
	switch k {
	case "tos":
		if strconv.Itoa(int(body[3])) != val {
			return fmt.Errorf("rtm_tos %d", body[3])
		}
		return nil
	case "flow":
		return kwWantFlow(attrs, unix.RTA_FLOW, val)
	case "table":
		return kwWantTable(body[4], attrs, unix.RTA_TABLE, val)
	case "pref":
		got, err := kwU8(attrs, unix.RTA_PREF)
		if err != nil {
			return err
		}
		if strconv.Itoa(int(got)) != val {
			return fmt.Errorf("RTA_PREF %d", got)
		}
		return nil
	}
	hopFlags, hop, err := kwFirstHop(attrs)
	if err != nil {
		return err
	}
	switch k {
	case "hop1_onlink":
		if (hopFlags&unix.RTNH_F_ONLINK != 0) != (val == "1") {
			return fmt.Errorf("rtnh_flags %#x", hopFlags)
		}
		return nil
	case "hop1_via_family":
		return kwWantVia(hop, val)
	case "hop1_flow":
		return kwWantFlow(hop, unix.RTA_FLOW, val)
	}
	return errors.New("no O3 check for this axis")
}

func kwLinkAxis(k, val string, body []byte, attrs map[uint16][]byte) error {
	switch k {
	case "group":
		// a set by group alone names no device
		if idx := binary.NativeEndian.Uint32(body[4:]); idx != 0 {
			return fmt.Errorf("ifi_index %d", idx)
		}
		return kwWantU32(attrs, unix.IFLA_GROUP, val)
	case "mtu":
		return kwWantU32(attrs, unix.IFLA_MTU, val)
	case "ifindex":
		if idx := int32(binary.NativeEndian.Uint32(body[4:])); strconv.Itoa(int(idx)) != val {
			return fmt.Errorf("ifi_index %d", idx)
		}
		return nil
	}
	xdp, ok := attrs[unix.IFLA_XDP]
	if !ok {
		return errors.New("no IFLA_XDP")
	}
	nest, err := kwAttrs(xdp)
	if err != nil {
		return err
	}
	switch k {
	case "xdp_fd":
		fd, err := kwU32(nest, unix.IFLA_XDP_FD)
		if err != nil {
			return err
		}
		if strconv.Itoa(int(int32(fd))) != val {
			return fmt.Errorf("IFLA_XDP_FD %d", int32(fd))
		}
		return nil
	case "xdp_flags":
		// xdp_parse adds XDP_FLAGS_UPDATE_IF_NOEXIST unless -force
		want, err := strconv.ParseUint(val, 10, 32)
		if err != nil {
			return err
		}
		return kwWantU32(nest, unix.IFLA_XDP_FLAGS, strconv.FormatUint(want|unix.XDP_FLAGS_UPDATE_IF_NOEXIST, 10))
	}
	return errors.New("no O3 check for this axis")
}

func kwNhAxis(k, val string, body []byte, attrs map[uint16][]byte) error {
	switch k {
	case "id":
		return kwWantU32(attrs, NhaID, val)
	case "gw":
		return kwWantGateway(body[0], attrs, val)
	case "hw_stats":
		return kwWantU32(attrs, NhaHwStatsEnable, val)
	}
	if t, err := kwFixed(attrs, NhaEncapType, 2); err != nil {
		return err
	} else if typ := binary.NativeEndian.Uint16(t); typ != unix.LWTUNNEL_ENCAP_MPLS {
		return fmt.Errorf("NHA_ENCAP_TYPE %d", typ)
	}
	encap, err := kwAttrs(attrs[NhaEncap])
	if err != nil {
		return err
	}
	switch k {
	case "labels":
		return kwWantLabels(encap, val)
	case "ttl":
		got, err := kwU8(encap, unix.MPLS_IPTUNNEL_TTL)
		if err != nil {
			return err
		}
		if strconv.Itoa(int(got)) != val {
			return fmt.Errorf("MPLS_IPTUNNEL_TTL %d", got)
		}
		return nil
	}
	return errors.New("no O3 check for this axis")
}

func kwRuleAxis(k, val string, body []byte, attrs map[uint16][]byte) error {
	switch k {
	case "pref":
		return kwWantU32(attrs, unix.FRA_PRIORITY, val)
	case "flow":
		return kwWantFlow(attrs, unix.FRA_FLOW, val)
	case "table":
		return kwWantTable(body[4], attrs, unix.FRA_TABLE, val)
	}
	return errors.New("no O3 check for this axis")
}

// kwBody is a hand-built request: a family header, then attributes.
func kwBody(t *testing.T, hdr []byte, put func(ab *AttrBuilder) error) []byte {
	t.Helper()
	ab := NewAttrBuilder(make([]byte, 512))
	if put != nil {
		if err := put(&ab); err != nil {
			t.Fatal(err)
		}
	}
	return append(slices.Clone(hdr), ab.Bytes()...)
}

// kwRtHdr is an rtmsg with the given tos and table bytes.
func kwRtHdr(family, tos, table byte) []byte {
	h := make([]byte, unix.SizeofRtMsg)
	h[0], h[3], h[4] = family, tos, table
	return h
}

// kwIfHdr is an ifinfomsg with the given index.
func kwIfHdr(idx int32) []byte {
	h := make([]byte, unix.SizeofIfInfomsg)
	binary.NativeEndian.PutUint32(h[4:], uint32(idx))
	return h
}

// kwHop is an RTA_MULTIPATH payload of one rtnexthop with flags and attrs.
func kwHop(t *testing.T, flags byte, put func(ab *AttrBuilder) error) []byte {
	t.Helper()
	attrs := kwBody(t, nil, put)
	h := make([]byte, unix.SizeofRtNexthop, unix.SizeofRtNexthop+len(attrs))
	binary.NativeEndian.PutUint16(h, uint16(len(h)+len(attrs)))
	h[2] = flags
	return append(h, attrs...)
}

// kwMplsDst is the label stack mpls_pton writes, with the bottom bit on
// entry bos (-1 for none).
func kwMplsDst(bos int, labels ...uint32) []byte {
	var b []byte
	for i, l := range labels {
		e := l << 12
		if i == bos {
			e |= 1 << 8
		}
		b = binary.BigEndian.AppendUint32(b, e)
	}
	return b
}

func TestKwliveProblem(t *testing.T) {
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	const set = unix.NLM_F_REQUEST | unix.NLM_F_ACK
	u32 := func(typ uint16, v uint32) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error { return ab.PutU32(typ, v) }
	}
	via := func(fam uint16, alen int) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			return ab.PutBytes(unix.RTA_VIA, append(binary.NativeEndian.AppendUint16(nil, fam), make([]byte, alen)...))
		}
	}
	multipath := func(hop []byte) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error { return ab.PutBytes(unix.RTA_MULTIPATH, hop) }
	}
	nhMpls := func(fam byte, dst []byte, ttl uint8, gw []byte) []byte {
		hdr := make([]byte, unix.SizeofNhmsg)
		hdr[0] = fam
		return kwBody(t, hdr, func(ab *AttrBuilder) error {
			if err := ab.PutU32(NhaID, 51); err != nil {
				return err
			}
			if err := PutMplsEncap(ab, NhaEncap, NhaEncapType, MplsEncap{Dst: dst, TTL: ttl, HasTTL: ttl != 0}); err != nil {
				return err
			}
			return ab.PutBytes(NhaGateway, gw)
		})
	}
	xdp := func(flags uint32) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error {
			nest, err := ab.BeginNest(unix.IFLA_XDP | unix.NLA_F_NESTED)
			if err != nil {
				return err
			}
			if err := ab.PutU32(unix.IFLA_XDP_FD, ^uint32(0)); err != nil {
				return err
			}
			if err := ab.PutU32(unix.IFLA_XDP_FLAGS, flags); err != nil {
				return err
			}
			return ab.EndNest(nest)
		}
	}
	v4gw := []byte{192, 0, 2, 1}
	cases := []struct {
		description string
		space       string
		axes        map[string]string
		flags       uint16
		body        []byte
		expected    string // "" for no problem, else a substring of it
	}{
		{"positive: tos", "kwroute4", map[string]string{"verb": "add", "tos": "16"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0x10, 254), nil), ""},
		{"negative: the tos byte not the axis", "kwroute4", map[string]string{"tos": "16"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), nil), "rtm_tos 0"},
		{"positive: realms TO", "kwroute4", map[string]string{"flow": "5"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), u32(unix.RTA_FLOW, 5)), ""},
		{"positive: realms FROM/TO", "kwroute4", map[string]string{"flow": "65538"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), u32(unix.RTA_FLOW, 1<<16|2)), ""},
		{"negative: realms FROM not shifted", "kwroute4", map[string]string{"flow": "65538"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), u32(unix.RTA_FLOW, 3)), "realms 0/3, want 1/2"},
		{"positive: vrf, table below 256", "kwroute4", map[string]string{"table": "100"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 100), nil), ""},
		{"boundary: vrf, table above 255", "kwroute6", map[string]string{"table": "1111"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET6, 0, 0), u32(unix.RTA_TABLE, 1111)), ""},
		{"negative: table above 255 in the byte", "kwroute6", map[string]string{"table": "1111"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET6, 0, 1111&0xff), u32(unix.RTA_TABLE, 1111)), "table byte 87"},
		{"negative: table below 256 sent as an attribute too", "kwroute4", map[string]string{"table": "100"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 100), u32(unix.RTA_TABLE, 100)), "attribute true"},
		{"positive: pref", "kwroute6", map[string]string{"pref": "255"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET6, 0, 254), func(ab *AttrBuilder) error { return ab.PutU8(unix.RTA_PREF, 255) }), ""},
		{"negative: RTA_PREF as a u32", "kwroute6", map[string]string{"pref": "1"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET6, 0, 254), u32(unix.RTA_PREF, 1)), "4 bytes, want 1"},
		{"positive: a hop's onlink", "kwroute4", map[string]string{"hop1_onlink": "1"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), multipath(kwHop(t, unix.RTNH_F_ONLINK, nil))), ""},
		{"negative: no onlink on the hop", "kwroute4", map[string]string{"hop1_onlink": "1"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), multipath(kwHop(t, 0, nil))), "rtnh_flags 0"},
		{"positive: a hop via inet6", "kwroute4", map[string]string{"hop1_via_family": "10"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), multipath(kwHop(t, 0, via(unix.AF_INET6, 16)))), ""},
		{"negative: RTA_VIA's address the wrong length", "kwroute4", map[string]string{"hop1_via_family": "10"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), multipath(kwHop(t, 0, via(unix.AF_INET6, 4)))), "6 bytes"},
		{"positive: a hop's realms", "kwroute4", map[string]string{"hop1_flow": "3"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), multipath(kwHop(t, 0, u32(unix.RTA_FLOW, 3)))), ""},
		{"negative: no RTA_MULTIPATH", "kwroute4", map[string]string{"hop1_flow": "3"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), nil), "no RTA_MULTIPATH"},
		{"negative: an add without NLM_F_EXCL", "kwroute4", map[string]string{"verb": "add"}, add &^ unix.NLM_F_EXCL,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), nil), "flags"},
		{"positive: encap mpls, two labels", "kwnh", map[string]string{"id": "51", "labels": "101/102", "gw": "192.0.2.1"}, add,
			nhMpls(unix.AF_INET, kwMplsDst(1, 101, 102), 0, v4gw), ""},
		{"negative: the bottom bit on the first of two labels", "kwnh", map[string]string{"labels": "101/102"}, add,
			nhMpls(unix.AF_INET, kwMplsDst(0, 101, 102), 0, v4gw), "entry 0"},
		{"negative: no bottom bit", "kwnh", map[string]string{"labels": "101"}, add,
			nhMpls(unix.AF_INET, kwMplsDst(-1, 101), 0, v4gw), "bottom true"},
		{"positive: encap mpls with ttl", "kwnh", map[string]string{"labels": "101", "ttl": "10"}, add,
			nhMpls(unix.AF_INET, kwMplsDst(0, 101), 10, v4gw), ""},
		{"positive: an IPv6 gateway", "kwnh", map[string]string{"gw": "2001:db8:1::1"}, add,
			nhMpls(unix.AF_INET6, kwMplsDst(0, 101), 0, netip.MustParseAddr("2001:db8:1::1").AsSlice()), ""},
		{"negative: an IPv6 gateway, nh_family AF_INET", "kwnh", map[string]string{"gw": "2001:db8:1::1"}, add,
			nhMpls(unix.AF_INET, kwMplsDst(0, 101), 0, netip.MustParseAddr("2001:db8:1::1").AsSlice()), "family 2"},
		{"positive: hw_stats off", "kwnh", map[string]string{"hw_stats": "0"}, add,
			kwBody(t, make([]byte, unix.SizeofNhmsg), u32(NhaHwStatsEnable, 0)), ""},
		{"positive: set by group alone", "kwlink", map[string]string{"verb": "set", "group": "5", "mtu": "1400"}, set,
			kwBody(t, kwIfHdr(0), func(ab *AttrBuilder) error {
				if err := ab.PutU32(unix.IFLA_GROUP, 5); err != nil {
					return err
				}
				return ab.PutU32(unix.IFLA_MTU, 1400)
			}), ""},
		{"negative: a group set naming a device", "kwlink", map[string]string{"group": "5"}, set,
			kwBody(t, kwIfHdr(3), u32(unix.IFLA_GROUP, 5)), "ifi_index 3"},
		{"positive: xdpoffload off", "kwlink", map[string]string{"xdp_fd": "-1", "xdp_flags": "8"}, set,
			kwBody(t, kwIfHdr(3), xdp(unix.XDP_FLAGS_HW_MODE|unix.XDP_FLAGS_UPDATE_IF_NOEXIST)), ""},
		{"negative: xdp flags without UPDATE_IF_NOEXIST", "kwlink", map[string]string{"xdp_flags": "8"}, set,
			kwBody(t, kwIfHdr(3), xdp(unix.XDP_FLAGS_HW_MODE)), "is 8, want 9"},
		{"positive: index on a create", "kwlink", map[string]string{"verb": "add", "ifindex": "77"}, add,
			kwBody(t, kwIfHdr(77), nil), ""},
		{"positive: rule realms and table", "kwrule", map[string]string{"pref": "200", "flow": "65538", "table": "100"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 100), func(ab *AttrBuilder) error {
				if err := ab.PutU32(unix.FRA_PRIORITY, 200); err != nil {
					return err
				}
				return ab.PutU32(unix.FRA_FLOW, 1<<16|2)
			}), ""},
		{"corner: an axis with no check", "kwrule", map[string]string{"fwmark": "1"}, add,
			kwBody(t, kwRtHdr(unix.AF_INET, 0, 100), nil), "no O3 check"},
		{"corner: a space not in the family", "route4", map[string]string{}, add, nil, `space "route4"`},
		{"boundary: a body shorter than its header", "kwnh", map[string]string{}, add, make([]byte, 4), "header 8"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			err := kwliveProblem(tc.space, tc.axes, tc.flags, tc.body)
			switch {
			case tc.expected == "" && err != nil:
				t.Errorf("problem %v, want none", err)
			case tc.expected != "" && (err == nil || !strings.Contains(err.Error(), tc.expected)):
				t.Errorf("problem %v, want one with %q", err, tc.expected)
			}
		})
	}
}

// TestWsKwliveCorpus runs kwliveProblem over every captured kwlive row:
// each sends exactly one request, refused or not, since the kernel refuses
// all four rc 2 forms and the duplicates.
func TestWsKwliveCorpus(t *testing.T) {
	var checked, noRequest int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		space, _, _ := strings.Cut(row.ID, "/")
		sp, ok := kwliveSpaces[space]
		if !ok {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), sp.typ)
			switch len(reqs) {
			case 0:
				noRequest++
				t.Errorf("O3: no request of type %d\n  cmd: %s", sp.typ, row.Cmd)
				return
			case 1:
			default:
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), sp.typ, row.Cmd)
			}
			checked++
			if err := kwliveProblem(space, row.Axes, reqs[0].h.Flags, reqs[0].body); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d kwlive requests; %d rows sent none", checked, noRequest)
	if checked == 0 {
		t.Error("O3 checked no kwlive request: are there kw* rows under testdata/*/ws?")
	}
}
