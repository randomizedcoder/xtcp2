package goip

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestLwtEncapArgsSrv6 is lwt_parse_encap for seg6, seg6local, ila, xfrm,
// ioam6 and rpl: what each argv parses to, the index of the last word the
// type used, and ip's refusals, including the argv quirks goip copies
// (goip-lwt-srv6.md, "ip's quirks goip copies"). The resolver knows d0 as
// 3. LX2-T01.
//
// go test ./internal/goip/ -run TestLwtEncapArgsSrv6
func TestLwtEncapArgsSrv6(t *testing.T) {
	addr := func(s string) []byte {
		a, err := routeGetAddr(s, unix.AF_INET6)
		if err != nil {
			t.Fatal(err)
		}
		return a.data
	}
	seg := func(s string) [16]byte { return [16]byte(addr(s)) }
	const s1, s3 = "2001:db8:9::1", "2001:db8:9::1,2001:db8:9::2,2001:db8:9::3"
	a1, a2, a3 := seg(s1), seg("2001:db8:9::2"), seg("2001:db8:9::3")
	srh := func(nsegs int, hmac uint32, segs ...[16]byte) []byte {
		b, err := xtcpnl.SRH(nsegs, segs, hmac)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	seg6 := func(mode int32, srh, src []byte) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Seg6: &xtcpnl.Seg6Encap{Mode: mode, SRH: srh, TunSrc: src}}
	}
	local := func(srh []byte, items ...xtcpnl.Seg6LocalItem) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Seg6Local: &xtcpnl.Seg6LocalEncap{Items: items, SRH: srh}}
	}
	u32 := func(kind uint16, v uint32) xtcpnl.Seg6LocalItem { return xtcpnl.Seg6LocalItem{Kind: kind, U32: v} }
	act := func(a uint32) xtcpnl.Seg6LocalItem { return u32(xtcpnl.Seg6LocalAction, a) }
	flv := func(f xtcpnl.Seg6LocalFlavorSet) xtcpnl.Seg6LocalItem {
		return xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalFlavors, Flavors: f}
	}
	const csid = 1 << xtcpnl.Seg6LocalFlvOpNextCSID
	loc := [8]byte{0x20, 0x01, 0, 0, 0, 0, 0, 0x02}
	ila := func(l [8]byte, items ...xtcpnl.IlaItem) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{ILA: &xtcpnl.IlaEncap{Locator: l, Items: items}}
	}
	xfrm := func(items ...xtcpnl.XfrmItem) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Xfrm: &xtcpnl.XfrmEncap{Items: items}}
	}
	trace := " trace prealloc type 0x800000 ns 1 size 12"
	ioam := func(k, n uint32, mode uint8, src, dst []byte, size uint16) xtcpnl.LwtEncap {
		return xtcpnl.LwtEncap{Ioam6: &xtcpnl.Ioam6Encap{FreqK: k, FreqN: n, Mode: mode, TunSrc: src, TunDst: dst,
			TraceType: 0x800000, TraceNS: 1, TraceSize: size}}
	}
	const (
		inline = xtcpnl.Ioam6ModeInline
		encap  = xtcpnl.Seg6ModeEncap
	)
	tests := []struct {
		description string
		args        string // from args[0], the encap
		expected    xtcpnl.LwtEncap
		last        int    // the index of the last word used
		err         error  // with errText, a refusal
		errText     string // in the refusal's message
	}{
		// seg6
		{description: "positive: seg6 encap, one segment", args: "encap seg6 mode encap segs " + s1 + " dev d0",
			expected: seg6(encap, srh(1, 0, a1), nil), last: 5},
		{description: "positive: seg6 encap, three segments", args: "encap seg6 mode encap segs " + s3 + " dev d0",
			expected: seg6(encap, srh(3, 0, a1, a2, a3), nil), last: 5},
		{description: "positive: seg6 inline has the extra slot", args: "encap seg6 mode inline segs " + s1 + " dev d0",
			expected: seg6(xtcpnl.Seg6ModeInline, srh(2, 0, a1), nil), last: 5},
		{description: "positive: seg6 l2encap.red", args: "encap seg6 mode l2encap.red segs " + s1,
			expected: seg6(xtcpnl.Seg6ModeL2EncapRed, srh(1, 0, a1), nil), last: 5},
		{description: "positive: seg6 hmac", args: "encap seg6 mode encap segs " + s1 + " hmac 7 dev d0",
			expected: seg6(encap, srh(1, 7, a1), nil), last: 7},
		{description: "positive: seg6 tunsrc", args: "encap seg6 mode encap tunsrc 2001:db8:1::10 segs " + s1 + " dev d0",
			expected: seg6(encap, srh(1, 0, a1), addr("2001:db8:1::10")), last: 7},
		{description: "positive: seg6's hmac before mode", args: "encap seg6 hmac 1 mode encap segs " + s1,
			expected: seg6(encap, srh(1, 1, a1), nil), last: 7},
		{description: "negative: seg6's segs before mode", args: "encap seg6 segs " + s1 + " mode encap",
			err: ErrInvalidArgument, errText: `"segs" provided before "mode"`},
		{description: "negative: seg6's tunsrc before mode", args: "encap seg6 tunsrc 2001:db8:1::10 mode encap",
			err: ErrInvalidArgument, errText: `"tunsrc" provided before "mode"`},
		{description: "negative: seg6's tunsrc with inline", args: "encap seg6 mode inline tunsrc 2001:db8:1::10 segs " + s1,
			err: ErrInvalidArgument, errText: `"tunsrc" invalid with inline mode`},
		{description: "negative: seg6's second segs", args: "encap seg6 mode encap segs " + s1 + " segs " + s1,
			err: ErrInvalidArgument, errText: `either "segs" is duplicate`},
		{description: "negative: seg6's unknown mode", args: "encap seg6 mode foo segs " + s1,
			err: ErrInvalidArgument, errText: `"mode" value is invalid`},
		{description: "negative: seg6's bad segment", args: "encap seg6 mode encap segs 10.0.0.1 dev d0",
			err: ErrInvalidArgument, errText: "inet6 address is expected"},
		{description: "boundary: seg6 with no mode is -1, one zero slot", args: "encap seg6 hmac 1 dev d0",
			expected: seg6(-1, srh(1, 1), nil), last: 3},
		{description: "corner: seg6's hmac x is hmac 0", args: "encap seg6 mode encap segs " + s1 + " hmac x dev d0",
			expected: seg6(encap, srh(1, 0, a1), nil), last: 7},
		{description: "corner: seg6's empty field is a zero slot", args: "encap seg6 mode encap segs 2001:db8:9::1,,2001:db8:9::3",
			expected: seg6(encap, srh(3, 0, a1, a3), nil), last: 5},
		// seg6local
		{description: "positive: seg6local End", args: "encap seg6local action End dev d0", expected: local(nil, act(1)), last: 3},
		{description: "positive: seg6local End count", args: "encap seg6local action End count dev d0",
			expected: local(nil, act(1), xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalCounters}), last: 4},
		{description: "positive: seg6local next-csid with both lengths",
			args:     "encap seg6local action End flavors next-csid lblen 32 nflen 16 dev d0",
			expected: local(nil, act(1), flv(xtcpnl.Seg6LocalFlavorSet{Ops: csid, LcblockBits: 32, LcnodeFnBits: 16, HasLcblock: true, HasLcnodeFn: true})),
			last:     9},
		{description: "positive: seg6local End.X nh6 and oif", args: "encap seg6local action End.X nh6 2001:db8:1::1 oif d0 dev d0",
			expected: local(nil, act(2), xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalNH6, Addr: addr("2001:db8:1::1")}, u32(xtcpnl.Seg6LocalOIF, 3)),
			last:     7},
		{description: "positive: seg6local End.DX4 nh4", args: "encap seg6local action End.DX4 nh4 192.0.2.1 dev d0",
			expected: local(nil, act(6), xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalNH4, Addr: []byte{192, 0, 2, 1}}), last: 5},
		{description: "positive: seg6local End.DT4 vrftable", args: "encap seg6local action End.DT4 vrftable 100 dev d0",
			expected: local(nil, act(8), u32(xtcpnl.Seg6LocalVrfTable, 100)), last: 5},
		{description: "positive: seg6local End.B6's srh, a word after it read again", args: "encap seg6local action End.B6 srh segs " + s3 + " dev d0",
			expected: local(srh(4, 0, a1, a2, a3), act(9)), last: 6},
		{description: "positive: seg6local End.B6.Encaps' srh and hmac", args: "encap seg6local action End.B6.Encaps srh segs " + s1 + " hmac 7 dev d0",
			expected: local(srh(1, 7, a1), act(10)), last: 8},
		{description: "negative: seg6local with no action", args: "encap seg6local table 100 dev d0",
			err: ErrLinkUsage, errText: "Missing action type"},
		{description: "negative: seg6local's unknown action", args: "encap seg6local action End.Q dev d0",
			err: ErrInvalidArgument, errText: `"action" value is invalid`},
		{description: "negative: seg6local's oif that doesn't exist", args: "encap seg6local action End.DX2 oif nosuch dev d0",
			err: ErrLinkUsage, errText: `Cannot find device "nosuch"`},
		{description: "positive: seg6local End.BPF without an endpoint is the kernel's to refuse", args: "encap seg6local action End.BPF dev d0",
			expected: local(nil, act(15)), last: 3},
		{description: "negative: seg6local endpoint's object that doesn't open", args: "encap seg6local action End.BPF endpoint obj /nonexistent/x.o",
			err: ErrBpfLoad, errText: "Error opening object /nonexistent/x.o: No such file or directory"},
		{description: "negative: seg6local's flavor twice", args: "encap seg6local action End flavors psp,psp dev d0",
			err: ErrInvalidArgument, errText: `invalid "flavors" attribute`},
		{description: "negative: seg6local's srh without segs", args: "encap seg6local action End.B6 srh foo",
			err: ErrInvalidArgument, errText: `missing "segs" attribute for srh`},
		{description: "negative: seg6local's count twice", args: "encap seg6local action End count count",
			err: ErrInvalidArgument, errText: `either "count" is duplicate, or "count" is garbage`},
		{description: "boundary: next-csid's nflen value last reads nothing past it", args: "encap seg6local action End flavors next-csid nflen 16",
			expected: local(nil, act(1), flv(xtcpnl.Seg6LocalFlavorSet{Ops: csid, LcnodeFnBits: 16, HasLcnodeFn: true})), last: 7},
		{description: "boundary: next-csid last, where ip reads past argv", args: "encap seg6local action End flavors next-csid",
			err: ErrInvalidArgument, errText: "Command line is not complete"},
		{description: "boundary: next-csid's lblen value last, where ip reads past argv for nflen", args: "encap seg6local action End flavors next-csid lblen 32",
			err: ErrInvalidArgument, errText: "Command line is not complete"},
		{description: "corner: seg6local next-csid alone stops before dev", args: "encap seg6local action End flavors next-csid dev d0",
			expected: local(nil, act(1), flv(xtcpnl.Seg6LocalFlavorSet{Ops: csid})), last: 5},
		{description: "corner: seg6local's flavors by strcasecmp", args: "encap seg6local action End flavors PSP dev d0",
			expected: local(nil, act(1), flv(xtcpnl.Seg6LocalFlavorSet{Ops: 1 << xtcpnl.Seg6LocalFlvOpPSP})), last: 5},
		{description: "corner: seg6local's srh segs S last, the index before S", args: "encap seg6local action End.B6 srh segs " + s1,
			expected: local(srh(2, 0, a1), act(9)), last: 5},
		{description: "corner: seg6local's table by name", args: "encap seg6local action End.T table main",
			expected: local(nil, act(3), u32(xtcpnl.Seg6LocalTable, 254)), last: 5},
		{description: "corner: seg6local's srh hmac x is hmac 0", args: "encap seg6local action End.B6.Encaps srh segs " + s1 + " hmac x",
			expected: local(srh(1, 0, a1), act(10)), last: 8},
		// ila
		{description: "positive: ila luid", args: "encap ila 2001:0:0:2 ident-type luid dev d0",
			expected: ila(loc, xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrIdentType, U8: 1}), last: 4},
		{description: "positive: ila's keywords in argv order", args: "encap ila 2001:0:0:2 csum-mode no-action ident-type luid hook-type output dev d0",
			expected: ila(loc, xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrCsumMode, U8: 2}, xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrIdentType, U8: 1},
				xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrHookType, U8: 0}), last: 8},
		{description: "negative: ila's five groups", args: "encap ila 2001:0:0:2:3 dev d0", err: ErrInvalidArgument, errText: "Bad locator: 2001:0:0:2:3"},
		{description: "negative: ila's unknown csum-mode", args: "encap ila 2001:0:0:2 csum-mode foo dev d0",
			err: ErrInvalidArgument, errText: `"csum-mode" value is invalid`},
		{description: "corner: ila's short locator is zeros, not ip's stack", args: "encap ila 2001 dev d0",
			expected: ila([8]byte{0x20, 0x01}), last: 2},
		{description: "corner: ila's keyword twice is written twice", args: "encap ila 2001:0:0:2 ident-type luid ident-type luid",
			expected: ila(loc, xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrIdentType, U8: 1}, xtcpnl.IlaItem{Kind: xtcpnl.ILAAttrIdentType, U8: 1}), last: 6},
		// xfrm
		{description: "positive: xfrm if_id", args: "encap xfrm if_id 1", expected: xfrm(xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmIfID, U32: 1}), last: 3},
		{description: "positive: xfrm if_id and link_dev", args: "encap xfrm if_id 1 link_dev d0",
			expected: xfrm(xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmIfID, U32: 1}, xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmLink, U32: 3}), last: 5},
		{description: "negative: xfrm if_id 0", args: "encap xfrm if_id 0", err: ErrInvalidArgument, errText: `"if_id" value is invalid`},
		{description: "negative: xfrm with no if_id", args: "encap xfrm link_dev d0", err: ErrLinkUsage, errText: "encap xfrm if_id IF_ID"},
		{description: "negative: xfrm's link_dev that doesn't exist", args: "encap xfrm if_id 1 link_dev nosuch",
			err: ErrLinkUsage, errText: `Cannot find device "nosuch"`},
		{description: "corner: xfrm swallows the rest of the command", args: "encap xfrm if_id 1 dev d0",
			expected: xfrm(xtcpnl.XfrmItem{Kind: xtcpnl.LwtXfrmIfID, U32: 1}), last: 5},
		// ioam6
		{description: "positive: ioam6 inline, the trace alone", args: "encap ioam6" + trace + " dev d0",
			expected: ioam(1, 1, inline, nil, nil, 12), last: 9},
		{description: "positive: ioam6 encap, every keyword",
			args:     "encap ioam6 freq 1/2 mode encap tunsrc 2001:db8:1::10 tundst " + s1 + trace + " dev d0",
			expected: ioam(1, 2, xtcpnl.Ioam6ModeEncap, addr("2001:db8:1::10"), a1[:], 12), last: 17},
		{description: "negative: ioam6's tundst with inline", args: "encap ioam6 mode inline tundst " + s1 + trace,
			err: ErrInvalidArgument, errText: "Inline mode does not need tundst"},
		{description: "negative: ioam6's size not a multiple of 4", args: "encap ioam6 trace prealloc type 0x800000 ns 1 size 13 dev d0",
			err: ErrInvalidArgument, errText: "Trace size must be a 4-octet multiple"},
		{description: "negative: ioam6's k over n", args: "encap ioam6 freq 3/2" + trace + " dev d0",
			err: ErrInvalidArgument, errText: "Frequency with k > n is forbidden"},
		{description: "negative: ioam6 out of order", args: "encap ioam6 mode inline freq 1/2" + trace,
			err: ErrLinkUsage, errText: `argument "trace" is required`},
		{description: "negative: ioam6 encap with no tundst", args: "encap ioam6 mode encap" + trace,
			err: ErrLinkUsage, errText: `argument "tundst" is required`},
		{description: "negative: ioam6 with no ns", args: "encap ioam6 trace prealloc type 0x800000 size 12 dev d0",
			err: ErrLinkUsage, errText: `argument "ns" is required`},
		{description: "boundary: ioam6's size 244", args: "encap ioam6 trace prealloc type 0x800000 ns 1 size 244 dev d0",
			expected: ioam(1, 1, inline, nil, nil, 244), last: 9},
		{description: "boundary: ioam6's size 248 is too big", args: "encap ioam6 trace prealloc type 0x800000 ns 1 size 248 dev d0",
			err: ErrInvalidArgument, errText: "Trace size is too big"},
		{description: "boundary: ioam6's freq k = n", args: "encap ioam6 freq 5/5" + trace + " dev d0",
			expected: ioam(5, 5, inline, nil, nil, 12), last: 11},
		{description: "corner: ioam6's trace at the end of argv, the last value read again", args: "encap ioam6" + trace,
			expected: ioam(1, 1, inline, nil, nil, 12), last: 8},
		// rpl
		{description: "positive: rpl's two segments", args: "encap rpl segs 2001:db8:9::1,2001:db8:9::2 dev d0",
			expected: xtcpnl.LwtEncap{Rpl: &xtcpnl.RplEncap{NSegs: 2, Segs: [][16]byte{a1, a2}}}, last: 3},
		{description: "negative: rpl's segs twice", args: "encap rpl segs " + s1 + " segs " + s1,
			err: ErrInvalidArgument, errText: `either "segs" is duplicate`},
	}
	resolve := func(name string) (int32, error) {
		if name == "d0" {
			return 3, nil
		}
		return 0, nil
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			got, last, err := lwtEncapArgs(strings.Fields(tt.args), 0, resolve)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("err = %v, want %v with %q", err, tt.err, tt.errText)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if last != tt.last {
				t.Errorf("last word %d, want %d", last, tt.last)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got  %s\nwant %s", lwtSrv6String(got), lwtSrv6String(tt.expected))
			}
		})
	}
}

// lwtSrv6String shows an LwtEncap's pointed-to value in a failure.
func lwtSrv6String(e xtcpnl.LwtEncap) string {
	var b strings.Builder
	for _, p := range []any{e.Seg6, e.Seg6Local, e.ILA, e.Xfrm, e.Ioam6, e.Rpl} {
		if v := reflect.ValueOf(p); !v.IsNil() {
			b.WriteString(strings.TrimPrefix(reflect.Indirect(v).Type().Name(), "xtcpnl."))
			fmt.Fprintf(&b, "%+v", reflect.Indirect(v).Interface())
		}
	}
	return b.String()
}
