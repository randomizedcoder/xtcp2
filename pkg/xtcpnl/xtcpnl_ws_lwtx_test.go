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

// The lwtx rows' O3 (goip-lwt-srv6.md, LX3-T03). Each lwtx row is one encap
// form (tools/nlcapgen/matrix_lwtx.go), and its axes name what the encap
// holds: seg6's mode and slots, seg6local's action and attributes, ila's
// locator and u8s, xfrm's if_id and link, ioam6's frequencies, mode,
// addresses and trace header, and rpl's slots. An SRH's segments are
// checked against the row's own `segs` words, written last to first.
// lwtxProblem fails on an axis it has no check for.

// lwtxSpaces are the lwtx spaces' requests, as kwliveSpaces.
var lwtxSpaces = map[string]kwliveSpace{
	"seg6route6": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"seg6route4": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"seg6local":  {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"ila":        {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"xfrm":       {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtxreq":    {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtxhop":    {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtxnh":     {unix.RTM_NEWNEXTHOP, unix.SizeofNhmsg},
}

// lwtxEncapTypes are the encap axis's words as LWTUNNEL_ENCAP_*.
var lwtxEncapTypes = map[string]uint16{
	"seg6": unix.LWTUNNEL_ENCAP_SEG6, "seg6local": unix.LWTUNNEL_ENCAP_SEG6_LOCAL, "ila": unix.LWTUNNEL_ENCAP_ILA,
	"xfrm": unix.LWTUNNEL_ENCAP_XFRM, "ioam6": unix.LWTUNNEL_ENCAP_IOAM6, "rpl": unix.LWTUNNEL_ENCAP_RPL,
}

// lwtxSegsWord is the word before an SRH's segment list in a command.
const lwtxSegsWord = "segs"

// lwtxProblem checks an lwtx request body against the row's axes and, for
// an SRH's segments, its cmd.
func lwtxProblem(space string, ax map[string]string, cmd string, flags uint16, body []byte) error {
	sp, ok := lwtxSpaces[space]
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
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range ax {
			if !yield(k) {
				return
			}
		}
	}) {
		if err := lwtxAxis(k, ax, cmd, flags, attrs, encapAttr, typeAttr); err != nil {
			return fmt.Errorf("%s=%s: %w", k, ax[k], err)
		}
	}
	return nil
}

// lwtxAxis checks one axis: the request's own, its encap's, or a hop's
// encap's.
func lwtxAxis(k string, ax map[string]string, cmd string, flags uint16, attrs map[uint16][]byte, encapAttr, typeAttr uint16) error {
	val := ax[k]
	switch k {
	case "verb":
		return kwWantVerb(flags, val)
	case "id":
		return kwWantU32(attrs, NhaID, val)
	}
	if k == "oif" && val == "none" { // xfrm's swallowed dev: the route has none
		if _, ok := attrs[unix.RTA_OIF]; ok {
			return errors.New("an RTA_OIF")
		}
		return nil
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
		if want, ok := lwtxEncapTypes[val]; !ok || e.typ != want {
			return fmt.Errorf("encap type %d", e.typ)
		}
		return nil
	}
	switch typ {
	case "seg6":
		return lwtxSeg6Axis(k, val, ax[prefix+"hmac"], cmd, e)
	case "seg6local":
		return lwtxLocalAxis(k, val, ax["hmac"], cmd, e)
	case "ila":
		return lwtxIlaAxis(k, val, e)
	case "xfrm":
		return lwtxXfrmAxis(k, val, e)
	case "ioam6":
		return lwtxIoam6Axis(k, val, e)
	case "rpl":
		return lwtxRplAxis(k, val, cmd, e)
	}
	return fmt.Errorf("an axis of encap %q", typ)
}

// lwtxCmdSegs is the addresses of the cmd's `segs` list, as strtok finds
// them: none when there is no list, as segbuf stays empty.
func lwtxCmdSegs(cmd string) ([]netip.Addr, error) {
	f := strings.Fields(cmd)
	i := slices.Index(f, lwtxSegsWord)
	if i < 0 || i+1 >= len(f) {
		return nil, nil
	}
	var out []netip.Addr
	for s := range strings.SplitSeq(f[i+1], ",") {
		if s == "" {
			continue
		}
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// lwtxSRHSlots checks an SRH of type typ: its header, its nsegs slots and,
// with hmac, the HMAC flag and TLV, then the cmd's segments written last
// to first from slot nsegs-1, the slots below them zero. hdrSlots is the
// header byte that says how many: seg6's first_segment (nsegs-1) or rpl's
// segments_left (nsegs).
func lwtxSRHSlots(srh []byte, srhType byte, nsegs, hmac, cmd string) error {
	n, err := strconv.Atoi(nsegs)
	if err != nil {
		return err
	}
	tlv := 0
	if hmac != "" {
		tlv = 40
	}
	if len(srh) != 8+16*n+tlv || int(srh[1]) != len(srh)/8-1 || srh[2] != srhType {
		return fmt.Errorf("SRH %d bytes, hdrlen %d, type %d, want %d slots of type %d", len(srh), srh[1], srh[2], n, srhType)
	}
	if srhType == 4 && (int(srh[3]) != n-1 || int(srh[4]) != n-1) {
		return fmt.Errorf("segments_left %d, first_segment %d, want %d", srh[3], srh[4], n-1)
	}
	if srhType == 3 && int(srh[3]) != n {
		return fmt.Errorf("segments_left %d, want %d", srh[3], n)
	}
	if hmac != "" {
		k, err := strconv.ParseUint(hmac, 10, 32)
		if err != nil {
			return err
		}
		t := srh[len(srh)-40:]
		if srh[5]&0x08 == 0 || t[0] != 5 || t[1] != 38 || binary.BigEndian.Uint32(t[4:]) != uint32(k) {
			return fmt.Errorf("HMAC flags %#x, TLV %x", srh[5], t[:8])
		}
	}
	segs, err := lwtxCmdSegs(cmd)
	if err != nil {
		return err
	}
	for slot := range n {
		got := srh[8+16*slot : 8+16*slot+16]
		want := make([]byte, 16)
		if i := n - 1 - slot; i < len(segs) {
			want = segs[i].AsSlice()
		}
		if !slices.Equal(got, want) {
			return fmt.Errorf("slot %d is %x, want %x", slot, got, want)
		}
	}
	return nil
}

func lwtxSeg6Axis(k, val, hmac, cmd string, e lwtEncap) error {
	srhAttr, ok := e.nest[Seg6IptunnelSRH]
	if !ok || len(srhAttr) < 4 {
		return errors.New("no SEG6_IPTUNNEL_SRH")
	}
	switch k {
	case "mode":
		m, err := strconv.ParseInt(val, 10, 32)
		if err != nil {
			return err
		}
		if got := int32(binary.NativeEndian.Uint32(srhAttr)); got != int32(m) {
			return fmt.Errorf("mode %d", got)
		}
		return nil
	case "nsegs":
		return lwtxSRHSlots(srhAttr[4:], 4, val, hmac, cmd)
	case "hmac":
		return nil // with nsegs
	case "tunsrc":
		return lwtWantAddr(e.nest, Seg6IptunnelSrc, "ip6", val)
	}
	return fmt.Errorf("a seg6 axis %q", k)
}

func lwtxLocalAxis(k, val, hmac, cmd string, e lwtEncap) error {
	switch k {
	case "action":
		return kwWantU32(e.nest, Seg6LocalAction, val)
	case "table":
		return kwWantU32(e.nest, Seg6LocalTable, val)
	case "vrftable":
		return kwWantU32(e.nest, Seg6LocalVrfTable, val)
	case "nh4", "nh6":
		kind, fam := Seg6LocalNH4, "ip"
		if k == "nh6" {
			kind, fam = Seg6LocalNH6, "ip6"
		}
		return lwtWantAddr(e.nest, kind, fam, val)
	case "oif":
		if v, err := kwU32(e.nest, Seg6LocalOIF); err != nil || v == 0 {
			return fmt.Errorf("SEG6_LOCAL_OIF %d (%v), want a device", v, err)
		}
		return nil
	case "count":
		return lwtxCounters(e.nest[Seg6LocalCounters])
	case "flavors", "lblen", "nflen":
		f, err := kwAttrs(e.nest[Seg6LocalFlavors])
		if err != nil {
			return err
		}
		switch k {
		case "flavors":
			return kwWantU32(f, Seg6LocalFlvOperation, val)
		case "lblen":
			return lwtWantU8(f, Seg6LocalFlvLcblockBits, val)
		}
		return lwtWantU8(f, Seg6LocalFlvLcnodeFnBits, val)
	case "srh_nsegs":
		return lwtxSRHSlots(e.nest[Seg6LocalSRH], 4, val, hmac, cmd)
	case "hmac":
		return nil // with srh_nsegs
	}
	return fmt.Errorf("a seg6local axis %q", k)
}

// lwtxCounters is seg6local_fill_counters' nest: PACKETS, BYTES and
// ERRORS, each a u64 0.
func lwtxCounters(raw []byte) error {
	c, err := kwAttrs(raw)
	if err != nil {
		return err
	}
	if len(c) != 3 {
		return fmt.Errorf("%d counters", len(c))
	}
	for _, typ := range []uint16{Seg6LocalCntPackets, Seg6LocalCntBytes, Seg6LocalCntErrors} {
		b, err := kwFixed(c, typ, 8)
		if err != nil {
			return err
		}
		if binary.NativeEndian.Uint64(b) != 0 {
			return fmt.Errorf("counter %d is %x", typ, b)
		}
	}
	return nil
}

func lwtxIlaAxis(k, val string, e lwtEncap) error {
	switch k {
	case "locator":
		b, err := kwFixed(e.nest, ILAAttrLocator, 8)
		if err != nil {
			return err
		}
		var want [8]byte
		for i, g := range strings.Split(val, ":") {
			n, err := strconv.ParseUint(g, 16, 16)
			if err != nil {
				return err
			}
			binary.BigEndian.PutUint16(want[2*i:], uint16(n))
		}
		if !slices.Equal(b, want[:]) {
			return fmt.Errorf("locator %x", b)
		}
		return nil
	case "csum":
		return lwtWantU8(e.nest, ILAAttrCsumMode, val)
	case "ident":
		return lwtWantU8(e.nest, ILAAttrIdentType, val)
	case "hook":
		return lwtWantU8(e.nest, ILAAttrHookType, val)
	}
	return fmt.Errorf("an ila axis %q", k)
}

func lwtxXfrmAxis(k, val string, e lwtEncap) error {
	switch k {
	case "if_id":
		return kwWantU32(e.nest, LwtXfrmIfID, val)
	case "link":
		if v, err := kwU32(e.nest, LwtXfrmLink); err != nil || v == 0 {
			return fmt.Errorf("LWT_XFRM_LINK %d (%v), want a device", v, err)
		}
		return nil
	}
	return fmt.Errorf("an xfrm axis %q", k)
}

// lwtxIoam6Axis checks ioam6's attributes and its trace header: ns be16,
// remlen (byte 3) the size in words, and type_be32 the type shifted up a
// byte.
func lwtxIoam6Axis(k, val string, e lwtEncap) error {
	trace := func() ([]byte, error) { return kwFixed(e.nest, Ioam6IptunnelTrace, 8) }
	n, _ := strconv.ParseUint(val, 10, 32)
	switch k {
	case "freq_k":
		return kwWantU32(e.nest, Ioam6IptunnelFreqK, val)
	case "freq_n":
		return kwWantU32(e.nest, Ioam6IptunnelFreqN, val)
	case "mode":
		return lwtWantU8(e.nest, Ioam6IptunnelMode, val)
	case "tunsrc":
		return lwtWantAddr(e.nest, Ioam6IptunnelSrc, "ip6", val)
	case "tundst":
		return lwtWantAddr(e.nest, Ioam6IptunnelDst, "ip6", val)
	case "type", "ns", "size":
		t, err := trace()
		if err != nil {
			return err
		}
		var got uint64
		switch k {
		case "type":
			got = uint64(binary.BigEndian.Uint32(t[4:]) >> 8)
		case "ns":
			got = uint64(binary.BigEndian.Uint16(t))
		default:
			got = uint64(t[3]&0x7f) * 4
		}
		if got != n {
			return fmt.Errorf("trace %s %d, header %s", k, got, hex.EncodeToString(t))
		}
		return nil
	}
	return fmt.Errorf("an ioam6 axis %q", k)
}

func lwtxRplAxis(k, val, cmd string, e lwtEncap) error {
	if k != "nsegs" {
		return fmt.Errorf("an rpl axis %q", k)
	}
	return lwtxSRHSlots(e.nest[RplIptunnelSRH], 3, val, "", cmd)
}

// TestLwtxProblem holds lwtxProblem to hand-built requests: each check
// passes the right bytes and fails the wrong ones.
//
// go test ./pkg/xtcpnl/ -run TestLwtxProblem
func TestLwtxProblem(t *testing.T) {
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	rt6 := kwRtHdr(unix.AF_INET6, 0, 254)
	put := func(e LwtEncap) func(ab *AttrBuilder) error {
		return func(ab *AttrBuilder) error { return PutLwtEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, e) }
	}
	addr := func(s string) [16]byte { return netip.MustParseAddr(s).As16() }
	s1, s2 := addr("2001:db8:9::1"), addr("2001:db8:9::2")
	srh := func(nsegs int, hmac uint32, segs ...[16]byte) []byte {
		b, err := SRH(nsegs, segs, hmac)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	seg6 := func(mode int32, b []byte) LwtEncap { return LwtEncap{Seg6: &Seg6Encap{Mode: mode, SRH: b}} }
	// reversed is an SRH with the segments first to last, as no ip writes.
	reversed := srh(2, 0, s1, s2)
	copy(reversed[8:], s1[:])
	copy(reversed[24:], s2[:])
	ioam := func(size uint16, typ uint32) LwtEncap {
		return LwtEncap{Ioam6: &Ioam6Encap{FreqK: 1, FreqN: 1, Mode: Ioam6ModeInline, TraceType: typ, TraceNS: 1, TraceSize: size}}
	}
	const two = "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1,2001:db8:9::2 dev d0"
	facts := lwtTestFacts
	tests := []struct {
		description string
		space       string
		ax          map[string]string
		cmd         string
		body        []byte
		expected    string // "" passes; else in the error
	}{
		{description: "positive: seg6, two segments last to first", space: "seg6route6",
			ax: facts("verb", "add", "encap", "seg6", "mode", "1", "nsegs", "2"), cmd: two,
			body: kwBody(t, rt6, put(seg6(1, srh(2, 0, s1, s2))))},
		{description: "positive: seg6 inline's zero slot", space: "seg6route6",
			ax: facts("encap", "seg6", "mode", "0", "nsegs", "2"), cmd: "x segs 2001:db8:9::1",
			body: kwBody(t, rt6, put(seg6(0, srh(2, 0, s1))))},
		{description: "positive: seg6 hmac", space: "seg6route6",
			ax: facts("encap", "seg6", "mode", "1", "nsegs", "1", "hmac", "7"), cmd: "x segs 2001:db8:9::1",
			body: kwBody(t, rt6, put(seg6(1, srh(1, 7, s1))))},
		{description: "positive: ioam6's trace", space: "lwtxreq",
			ax:   facts("encap", "ioam6", "freq_k", "1", "freq_n", "1", "mode", "1", "type", "8388608", "ns", "1", "size", "12"),
			body: kwBody(t, rt6, put(ioam(12, 0x800000)))},
		{description: "positive: ila's locator and ident", space: "ila", ax: facts("encap", "ila", "locator", "2001:0:0:2", "ident", "1"),
			body: kwBody(t, rt6, put(LwtEncap{ILA: &IlaEncap{Locator: [8]byte{0x20, 0x01, 0, 0, 0, 0, 0, 2},
				Items: []IlaItem{{Kind: ILAAttrIdentType, U8: 1}}}}))},
		{description: "negative: the segments first to last", space: "seg6route6",
			ax: facts("encap", "seg6", "mode", "1", "nsegs", "2"), cmd: two,
			body: kwBody(t, rt6, put(seg6(1, reversed))), expected: "slot 0"},
		{description: "negative: remlen as the size, not words", space: "lwtxreq",
			ax:   facts("encap", "ioam6", "size", "12"),
			body: kwBody(t, rt6, put(ioam(48, 0x800000))), expected: "trace size 48"},
		{description: "negative: the wrong encap type", space: "seg6route6", ax: facts("encap", "seg6local"),
			body: kwBody(t, rt6, put(seg6(1, srh(1, 0, s1)))), expected: "encap type 5"},
		{description: "negative: an hmac flag missing", space: "seg6route6",
			ax: facts("encap", "seg6", "nsegs", "1", "hmac", "7"), cmd: "x segs 2001:db8:9::1",
			body: kwBody(t, rt6, put(seg6(1, srh(1, 0, s1)))), expected: "slots"},
		{description: "negative: xfrm's oif sent", space: "xfrm", ax: facts("oif", "none"),
			body: kwBody(t, rt6, func(ab *AttrBuilder) error { return ab.PutU32(unix.RTA_OIF, 2) }), expected: "RTA_OIF"},
		{description: "negative: an axis with no check", space: "xfrm", ax: facts("encap", "xfrm", "color", "red"),
			body: kwBody(t, rt6, put(LwtEncap{Xfrm: &XfrmEncap{Items: []XfrmItem{{Kind: LwtXfrmIfID, U32: 1}}}})), expected: `axis "color"`},
		{description: "boundary: ioam6's 24-bit type", space: "lwtxreq", ax: facts("encap", "ioam6", "type", strconv.Itoa(0xabcdef)),
			body: kwBody(t, rt6, put(ioam(12, 0xabcdef)))},
		{description: "corner: seg6local's counters, all zero", space: "seg6local", ax: facts("encap", "seg6local", "action", "1", "count", "1"),
			body: kwBody(t, rt6, put(LwtEncap{Seg6Local: &Seg6LocalEncap{Items: []Seg6LocalItem{
				{Kind: Seg6LocalAction, U32: 1}, {Kind: Seg6LocalCounters}}}}))},
		{description: "corner: rpl's segments_left is nsegs", space: "lwtxreq", ax: facts("encap", "rpl", "nsegs", "2"),
			cmd:  "x segs 2001:db8:9::1,2001:db8:9::2",
			body: kwBody(t, rt6, put(LwtEncap{Rpl: &RplEncap{NSegs: 2, Segs: [][16]byte{s1, s2}}}))},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			err := lwtxProblem(tt.space, tt.ax, tt.cmd, add, tt.body)
			switch {
			case tt.expected == "" && err != nil:
				t.Fatalf("err = %v, want none", err)
			case tt.expected != "" && (err == nil || !strings.Contains(err.Error(), tt.expected)):
				t.Fatalf("err = %v, want one with %q", err, tt.expected)
			}
		})
	}
}

// TestWsLwtxCorpus runs lwtxProblem over every captured lwtx row that sent
// a request: one each, but for the rows ip refuses before sending (the
// twelve at rc 255 and ila's Bad locator at rc 1), which O2 holds to none.
//
// go test ./pkg/xtcpnl/ -run TestWsLwtxCorpus
func TestWsLwtxCorpus(t *testing.T) {
	var checked, noRequest int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		space, _, _ := strings.Cut(row.ID, "/")
		sp, ok := lwtxSpaces[space]
		if !ok {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")), sp.typ)
			switch {
			case len(reqs) == 0 && row.Expect.RC != nil && (*row.Expect.RC == 255 || *row.Expect.RC == 1):
				noRequest++
				return
			case len(reqs) != 1:
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), sp.typ, row.Cmd)
			}
			checked++
			if err := lwtxProblem(space, row.Axes, row.Cmd, reqs[0].h.Flags, reqs[0].body); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d lwtx requests; %d refusals sent none", checked, noRequest)
	if checked != 60 || noRequest != 13 {
		t.Errorf("O3 checked %d lwtx requests and %d refusals, want 60 and 13", checked, noRequest)
	}
}
