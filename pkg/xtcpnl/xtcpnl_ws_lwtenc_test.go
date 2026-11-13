package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The lwtenc rows' O3 (goip-parity-next.md, NC8-T05). Each row is one rpl,
// ioam6 or bpf encap (tools/nlcapgen/matrix_lwtenc.go), on a route, a
// nexthop object or a multipath hop, and its axes name what the encap
// holds. rpl and ioam6 reuse lwtx's checks; rpl's seg0 and seg2 name the
// cmd's first and third segments, which ip writes last to first. bpf's
// in, out and xmit name each hook's section: ip sends the program's fd
// (which varies, so it is not checked) and its name "<basename>:[<sec>]",
// which the kernel echoes. lwtencProblem fails on an axis it has no check
// for.

// lwtencSpaces are the lwtenc spaces' requests, as kwliveSpaces.
var lwtencSpaces = map[string]kwliveSpace{
	"rplroute":   {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"ioam6route": {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"bpfroute":   {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtenchop":  {unix.RTM_NEWROUTE, unix.SizeofRtMsg},
	"lwtencnh":   {unix.RTM_NEWNEXTHOP, unix.SizeofNhmsg},
}

// lwtencBpfObj is the object's basename, as ip names a program.
const lwtencBpfObj = "lwt_bpf.o"

var (
	lwtencEncapTypes = map[string]uint16{
		"rpl": unix.LWTUNNEL_ENCAP_RPL, "ioam6": unix.LWTUNNEL_ENCAP_IOAM6, "bpf": unix.LWTUNNEL_ENCAP_BPF,
	}
	lwtencBpfHooks = map[string]uint16{"in": LwtBpfIn, "out": LwtBpfOut, "xmit": LwtBpfXmit}
)

// lwtencProblem checks an lwtenc request body against the row's axes.
func lwtencProblem(space string, ax map[string]string, cmd string, flags uint16, body []byte) error {
	sp, ok := lwtencSpaces[space]
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
	keys := make([]string, 0, len(ax))
	for k := range ax {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if err := lwtencAxis(k, ax, cmd, flags, attrs, encapAttr, typeAttr); err != nil {
			return fmt.Errorf("%s=%s: %w", k, ax[k], err)
		}
	}
	return nil
}

// lwtencAxis checks one axis: the request's own, its encap's, or a hop's
// encap's.
func lwtencAxis(k string, ax map[string]string, cmd string, flags uint16, attrs map[uint16][]byte, encapAttr, typeAttr uint16) error {
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
	tb, err := kwFixed(attrs, typeAttr, 2)
	if err != nil {
		return err
	}
	raw, ok := attrs[encapAttr]
	if !ok {
		return fmt.Errorf("no attribute %d", encapAttr)
	}
	typ := ax[prefix+"encap"]
	if k == "encap" {
		if want, ok := lwtencEncapTypes[val]; !ok || binary.NativeEndian.Uint16(tb) != want {
			return fmt.Errorf("encap type %d", binary.NativeEndian.Uint16(tb))
		}
		return nil
	}
	switch typ {
	case "rpl", "ioam6":
		if k == "ns_added" {
			return nil // a setup step, `ip ioam namespace add`: nothing in the request
		}
		e, err := lwtFind(attrs, encapAttr, typeAttr)
		if err != nil {
			return err
		}
		if typ == "ioam6" {
			return lwtxIoam6Axis(k, val, e)
		}
		return lwtencRplAxis(k, val, ax[prefix+"nsegs"], cmd, e)
	case "bpf":
		return lwtencBpfAxis(k, val, raw)
	}
	return fmt.Errorf("an axis of encap %q", typ)
}

// lwtencRplAxis checks rpl's slots, then seg0 and seg2: the cmd's segment
// i is in slot nsegs-1-i.
func lwtencRplAxis(k, val, nsegs, cmd string, e lwtEncap) error {
	i, err := strconv.Atoi(strings.TrimPrefix(k, "seg"))
	if k == "nsegs" || err != nil {
		return lwtxRplAxis(k, val, cmd, e)
	}
	n, err := strconv.Atoi(nsegs)
	if err != nil {
		return err
	}
	want, err := netip.ParseAddr(val)
	if err != nil {
		return err
	}
	srh := e.nest[RplIptunnelSRH]
	slot := n - 1 - i
	if slot < 0 || len(srh) < 8+16*(slot+1) {
		return fmt.Errorf("no slot %d in %d bytes", slot, len(srh))
	}
	if got := srh[8+16*slot : 8+16*slot+16]; !slices.Equal(got, want.AsSlice()) {
		return fmt.Errorf("slot %d is %x", slot, got)
	}
	return nil
}

// lwtencBpfAxis checks a bpf encap, walked as a list because a repeated
// hook sends two nests: in, out and xmit are the last nest of the hook,
// in_first the first of two. headroom is the one XMIT_HEADROOM: ip sends
// only the first a cmd gives (parse_encap_bpf's headroom_set).
func lwtencBpfAxis(k, val string, raw []byte) error {
	list, err := ParseNetlinkAttributes(raw)
	if err != nil {
		return err
	}
	of := func(typ uint16) [][]byte {
		var out [][]byte
		for i := range list {
			if list[i].ID() == typ {
				out = append(out, list[i].Data)
			}
		}
		return out
	}
	if k == "headroom" {
		h := of(LwtBpfXmitHeadroom)
		if len(h) != 1 {
			return fmt.Errorf("%d LWT_BPF_XMIT_HEADROOM, want one", len(h))
		}
		return kwWantU32(map[uint16][]byte{LwtBpfXmitHeadroom: h[0]}, LwtBpfXmitHeadroom, val)
	}
	hook, first := k, false
	if k == "in_first" {
		hook, first = "in", true
	}
	typ, ok := lwtencBpfHooks[hook]
	if !ok {
		return fmt.Errorf("a bpf axis %q", k)
	}
	nests := of(typ)
	switch {
	case len(nests) == 0:
		return fmt.Errorf("no hook %d", typ)
	case first && len(nests) < 2:
		return fmt.Errorf("%d nests of hook %d, want two", len(nests), typ)
	}
	nest := nests[len(nests)-1]
	if first {
		nest = nests[0]
	}
	prog, err := kwAttrs(nest)
	if err != nil {
		return err
	}
	if _, err := kwFixed(prog, LwtBpfProgFD, 4); err != nil {
		return err
	}
	if got, want := string(prog[LwtBpfProgName]), lwtencBpfObj+":["+val+"]\x00"; got != want {
		return fmt.Errorf("prog name %q, want %q", got, want)
	}
	return nil
}

// lwtencBpfBody is a v4 route body with a bpf encap of the given hooks,
// each "hook=section", and a headroom when it isn't 0 (two when twice).
func lwtencBpfBody(t *testing.T, headroom uint32, hooks ...string) []byte {
	return lwtencBpfBodyN(t, headroom, 1, hooks...)
}

func lwtencBpfBodyN(t *testing.T, headroom uint32, copies int, hooks ...string) []byte {
	t.Helper()
	return kwBody(t, kwRtHdr(unix.AF_INET, 0, 254), func(ab *AttrBuilder) error {
		m, err := ab.BeginNest(unix.RTA_ENCAP)
		if err != nil {
			return err
		}
		for _, h := range hooks {
			hook, sec, _ := strings.Cut(h, "=")
			p, err := ab.BeginNest(lwtencBpfHooks[hook])
			if err != nil {
				return err
			}
			if err := ab.PutU32(LwtBpfProgFD, 7); err != nil {
				return err
			}
			if err := ab.PutString(LwtBpfProgName, lwtencBpfObj+":["+sec+"]"); err != nil {
				return err
			}
			if err := ab.EndNest(p); err != nil {
				return err
			}
		}
		for range copies * min(int(headroom), 1) {
			if err := ab.PutU32(LwtBpfXmitHeadroom, headroom); err != nil {
				return err
			}
		}
		if err := ab.EndNest(m); err != nil {
			return err
		}
		return ab.PutU16(unix.RTA_ENCAP_TYPE, unix.LWTUNNEL_ENCAP_BPF)
	})
}

// TestLwtencProblem holds lwtencProblem to hand-built requests: each check
// passes the right bytes and fails the wrong ones.
//
// go test ./pkg/xtcpnl/ -run TestLwtencProblem
func TestLwtencProblem(t *testing.T) {
	const add = unix.NLM_F_REQUEST | unix.NLM_F_ACK | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	rt6 := kwRtHdr(unix.AF_INET6, 0, 254)
	s1 := netip.MustParseAddr("2001:db8:9::1").As16()
	s3 := netip.MustParseAddr("2001:db8:9::3").As16()
	rpl := func(segs ...[16]byte) []byte {
		return kwBody(t, rt6, func(ab *AttrBuilder) error {
			return PutLwtEncap(ab, unix.RTA_ENCAP, unix.RTA_ENCAP_TYPE, LwtEncap{Rpl: &RplEncap{NSegs: len(segs), Segs: segs}})
		})
	}
	facts := lwtTestFacts
	const three = "x segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3"
	s2 := netip.MustParseAddr("2001:db8:9::2").As16()
	tests := []struct {
		description string
		space       string
		ax          map[string]string
		cmd         string
		body        []byte
		expected    string // "" passes; else in the error
	}{
		{description: "positive: bpf in", space: "bpfroute", ax: facts("verb", "add", "encap", "bpf", "in", "lwt_in"),
			body: lwtencBpfBody(t, 0, "in=lwt_in")},
		{description: "positive: all three hooks and headroom", space: "bpfroute",
			ax:   facts("encap", "bpf", "in", "lwt_in", "out", "lwt_out", "xmit", "lwt_xmit", "headroom", "16"),
			body: lwtencBpfBody(t, 16, "in=lwt_in", "out=lwt_out", "xmit=lwt_xmit")},
		{description: "positive: rpl's segments last to first", space: "rplroute",
			ax: facts("encap", "rpl", "nsegs", "3", "seg0", "2001:db8:9::1", "seg2", "2001:db8:9::3"), cmd: three,
			body: rpl(s1, s2, s3)},
		{description: "negative: the wrong program name", space: "bpfroute", ax: facts("encap", "bpf", "in", "lwt_in"),
			body: lwtencBpfBody(t, 0, "in=lwt_out"), expected: "prog name"},
		{description: "negative: the hook missing", space: "bpfroute", ax: facts("encap", "bpf", "out", "lwt_out"),
			body: lwtencBpfBody(t, 0, "in=lwt_out"), expected: "no hook 2"},
		{description: "negative: seg0 in the first slot", space: "rplroute",
			ax: facts("encap", "rpl", "nsegs", "3", "seg0", "2001:db8:9::3"), cmd: three,
			body: rpl(s1, s2, s3), expected: "seg0=2001:db8:9::3: slot 2"},
		{description: "negative: the wrong encap type", space: "bpfroute", ax: facts("encap", "rpl"),
			body: lwtencBpfBody(t, 0, "in=lwt_in"), expected: "encap type 6"},
		{description: "negative: an axis with no check", space: "bpfroute", ax: facts("encap", "bpf", "color", "red"),
			body: lwtencBpfBody(t, 0, "in=lwt_in"), expected: `a bpf axis "color"`},
		{description: "negative: an unknown space", space: "lwtx", ax: facts(), body: rpl(s1), expected: "space"},
		{description: "boundary: headroom 256", space: "bpfroute", ax: facts("encap", "bpf", "headroom", "256"),
			body: lwtencBpfBody(t, 256, "xmit=lwt_xmit")},
		{description: "boundary: headroom absent", space: "bpfroute", ax: facts("encap", "bpf", "headroom", "1"),
			body: lwtencBpfBody(t, 0, "xmit=lwt_xmit"), expected: "0 LWT_BPF_XMIT_HEADROOM"},
		{description: "corner: two headrooms, which ip never sends", space: "bpfroute", ax: facts("encap", "bpf", "headroom", "8"),
			body: lwtencBpfBodyN(t, 8, 2, "xmit=lwt_xmit"), expected: "2 LWT_BPF_XMIT_HEADROOM"},
		{description: "corner: in twice, the last and the first", space: "bpfroute",
			ax:   facts("encap", "bpf", "in", "lwt_out", "in_first", "lwt_in"),
			body: lwtencBpfBody(t, 0, "in=lwt_in", "in=lwt_out")},
		{description: "corner: in_first with one nest", space: "bpfroute", ax: facts("encap", "bpf", "in_first", "lwt_in"),
			body: lwtencBpfBody(t, 0, "in=lwt_in"), expected: "want two"},
		{description: "corner: ns_added is setup, not request", space: "rplroute",
			ax: facts("encap", "rpl", "ns_added", "1"), body: rpl(s1)},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			err := lwtencProblem(tt.space, tt.ax, tt.cmd, add, tt.body)
			switch {
			case tt.expected == "" && err != nil:
				t.Fatalf("err = %v, want none", err)
			case tt.expected != "" && (err == nil || !strings.Contains(err.Error(), tt.expected)):
				t.Fatalf("err = %v, want one with %q", err, tt.expected)
			}
		})
	}
}

// TestWsLwtencCorpus runs lwtencProblem over every captured lwtenc row that
// sent a request: one each, but for the rows ip refuses before sending (11
// at rc 255, rpl's IPv4 seg at rc 1), which O2 holds to none.
//
// go test ./pkg/xtcpnl/ -run TestWsLwtencCorpus
func TestWsLwtencCorpus(t *testing.T) {
	var checked, noRequest int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		space, _, _ := strings.Cut(row.ID, "/")
		sp, ok := lwtencSpaces[space]
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
			if err := lwtencProblem(space, row.Axes, row.Cmd, reqs[0].h.Flags, reqs[0].body); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d lwtenc requests; %d refusals sent none", checked, noRequest)
	if checked != 51 || noRequest != 12 {
		t.Errorf("O3 checked %d lwtenc requests and %d refusals, want 51 and 12", checked, noRequest)
	}
}
