package render

import (
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestEncapViewOfRplIoam6Bpf renders the three NX8 encaps as
// print_encap_rpl, print_encap_ioam6 and print_encap_bpf do; the positive
// rows are the lwtenc corpus' (testdata/<kver>/ws/{rplroute,ioam6route,
// bpfroute}) ip output.
//
// go test ./internal/goip/render/ -run TestEncapViewOfRplIoam6Bpf
func TestEncapViewOfRplIoam6Bpf(t *testing.T) {
	t.Parallel()
	raw := []byte{0}
	a6 := func(s string) [16]byte { return netip.MustParseAddr(s).As16() }
	rpl := func(e xtcpnl.RplEncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_RPL, HasType: true, Raw: raw, Rpl: &e}
	}
	ioam6 := func(e xtcpnl.Ioam6EncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_IOAM6, HasType: true, Raw: raw, Ioam6: &e}
	}
	bpf := func(e xtcpnl.BpfEncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_BPF, HasType: true, Raw: raw, Bpf: &e}
	}
	s1, s2, s3 := a6("2001:db8:9::1"), a6("2001:db8:9::2"), a6("2001:db8:9::3")
	// The SRH stores the segments reversed: ip prints segaddr[N-1] first.
	three := xtcpnl.RplEncapInfo{HasSRH: true, SegmentsLeft: 3, Segs: [][16]byte{s3, s2, s1}}
	tr := xtcpnl.Ioam6Trace{Namespace: 1, RemLen: 3, Type: 0x800000}
	full := func(mode uint8) xtcpnl.Ioam6EncapInfo {
		return xtcpnl.Ioam6EncapInfo{Mode: mode, FreqK: 1, FreqN: 2, Src: a6("2001:db8::1"), Dst: a6("2001:db8::2"),
			Trace: tr, HasMode: true, HasFreqK: true, HasFreqN: true, HasSrc: true, HasDst: true, HasTrace: true}
	}
	inline := full(xtcpnl.Ioam6ModeInline)
	inline.HasSrc, inline.HasDst = false, false
	auto := full(xtcpnl.Ioam6ModeAuto)
	auto.HasSrc = false
	noDst := full(xtcpnl.Ioam6ModeEncap)
	noDst.HasDst = false
	srcInline := full(xtcpnl.Ioam6ModeInline)
	srcInline.HasDst = false
	noTrace := full(xtcpnl.Ioam6ModeEncap)
	noTrace.HasTrace = false
	odd := inline
	odd.HasDst = true // mode 9 is not inline, so it needs the destination
	odd.Mode, odd.Trace = 9, xtcpnl.Ioam6Trace{Namespace: 65535, RemLen: 0x7f, Type: 2}
	zero := inline
	zero.Trace = xtcpnl.Ioam6Trace{}
	prog := func(n string) xtcpnl.BpfProg { return xtcpnl.BpfProg{Name: n, Present: true, HasName: true} }

	tests := []struct {
		description string
		e           *xtcpnl.RouteEncap
		text        string
		json        string
	}{
		{"positive: rpl three segs, segaddr[2] first",
			rpl(three), " encap rpl segs 3 [ 2001:db8:9::1 2001:db8:9::2 2001:db8:9::3 ] ",
			`{"encap_type":"rpl","segs":["2001:db8:9::1","2001:db8:9::2","2001:db8:9::3"]}`},
		{"positive: rpl one seg", rpl(xtcpnl.RplEncapInfo{HasSRH: true, SegmentsLeft: 1, Segs: [][16]byte{s1}}),
			" encap rpl segs 1 [ 2001:db8:9::1 ] ", `{"encap_type":"rpl","segs":["2001:db8:9::1"]}`},
		{"boundary: rpl segments_left 0 prints an empty list",
			rpl(xtcpnl.RplEncapInfo{HasSRH: true, Segs: [][16]byte{s1}}),
			" encap rpl segs 0 [ ] ", `{"encap_type":"rpl","segs":[]}`},
		{"corner: rpl segments_left past the attribute prints the addresses it has",
			rpl(xtcpnl.RplEncapInfo{HasSRH: true, SegmentsLeft: 5, Segs: [][16]byte{s2, s1}}),
			" encap rpl segs 5 [ 2001:db8:9::1 2001:db8:9::2 ] ", `{"encap_type":"rpl","segs":["2001:db8:9::1","2001:db8:9::2"]}`},
		{"negative: rpl without the SRH prints the type alone", rpl(xtcpnl.RplEncapInfo{}),
			" encap rpl ", `{"encap_type":"rpl"}`},
		{"positive: ioam6 encap mode with both tunnel addresses", ioam6(full(xtcpnl.Ioam6ModeEncap)),
			" encap ioam6 freq 1/2 mode encap tunsrc 2001:db8::1 tundst 2001:db8::2 trace prealloc type 0x800000 ns 1 size 12 ",
			`{"encap_type":"ioam6","freqk":1,"freqn":2,"mode":"encap","tunsrc":"2001:db8::1","tundst":"2001:db8::2",` +
				`"trace":null,"prealloc":null,"type":"800000","ns":1,"size":12}`},
		{"positive: ioam6 inline", ioam6(inline),
			" encap ioam6 freq 1/2 mode inline trace prealloc type 0x800000 ns 1 size 12 ",
			`{"encap_type":"ioam6","freqk":1,"freqn":2,"mode":"inline","trace":null,"prealloc":null,"type":"800000","ns":1,"size":12}`},
		{"positive: ioam6 auto with a destination alone", ioam6(auto),
			" encap ioam6 freq 1/2 mode auto tundst 2001:db8::2 trace prealloc type 0x800000 ns 1 size 12 ",
			`{"encap_type":"ioam6","freqk":1,"freqn":2,"mode":"auto","tundst":"2001:db8::2",` +
				`"trace":null,"prealloc":null,"type":"800000","ns":1,"size":12}`},
		{"negative: ioam6 encap without a destination stops after the freq", ioam6(noDst),
			" encap ioam6 freq 1/2 ", `{"encap_type":"ioam6","freqk":1,"freqn":2}`},
		{"negative: ioam6 inline with a source stops after the freq", ioam6(srcInline),
			" encap ioam6 freq 1/2 ", `{"encap_type":"ioam6","freqk":1,"freqn":2}`},
		{"negative: ioam6 without the trace prints the type alone", ioam6(noTrace), " encap ioam6 ", `{"encap_type":"ioam6"}`},
		{"boundary: ioam6 ns 65535, remlen 127, type 2, an unnamed mode", ioam6(odd),
			" encap ioam6 freq 1/2 mode <unknown> tundst 2001:db8::2 trace prealloc type 0x000002 ns 65535 size 508 ",
			`{"encap_type":"ioam6","freqk":1,"freqn":2,"mode":"<unknown>","tundst":"2001:db8::2","trace":null,"prealloc":null,"type":"2","ns":65535,"size":508}`},
		{"corner: ioam6 type 0 is C's 00000000 without 0x", ioam6(zero),
			" encap ioam6 freq 1/2 mode inline trace prealloc type 00000000 ns 0 size 0 ",
			`{"encap_type":"ioam6","freqk":1,"freqn":2,"mode":"inline","trace":null,"prealloc":null,"type":"0","ns":0,"size":0}`},
		{"positive: bpf all three hooks",
			bpf(xtcpnl.BpfEncapInfo{In: prog("lwt_bpf.o:[lwt_in]"), Out: prog("lwt_bpf.o:[lwt_out]"), Xmit: prog("lwt_bpf.o:[lwt_xmit]")}),
			" encap bpf in lwt_bpf.o:[lwt_in] out lwt_bpf.o:[lwt_out] xmit lwt_bpf.o:[lwt_xmit] ",
			`{"encap_type":"bpf","in":"lwt_bpf.o:[lwt_in]","out":"lwt_bpf.o:[lwt_out]","xmit":"lwt_bpf.o:[lwt_xmit]"}`},
		{"positive: bpf xmit alone", bpf(xtcpnl.BpfEncapInfo{Xmit: prog("lwt_bpf.o:[lwt_xmit]")}),
			" encap bpf xmit lwt_bpf.o:[lwt_xmit] ", `{"encap_type":"bpf","xmit":"lwt_bpf.o:[lwt_xmit]"}`},
		{"corner: bpf a nameless prog is its label in text and <unknown> in JSON",
			bpf(xtcpnl.BpfEncapInfo{In: xtcpnl.BpfProg{Present: true}}),
			" encap bpf in ", `{"encap_type":"bpf","in":"<unknown>"}`},
		{"boundary: bpf headroom 4294967295, which the kernel never dumps",
			bpf(xtcpnl.BpfEncapInfo{Out: prog("o"), Headroom: 4294967295, HasHeadroom: true}),
			" encap bpf out o  4294967295 ", `{"encap_type":"bpf","out":"o","headroom":4294967295}`},
		{"negative: bpf with no hooks prints the type alone", bpf(xtcpnl.BpfEncapInfo{}), " encap bpf ", `{"encap_type":"bpf"}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			v := EncapViewOf(tc.e, fakeNames{}, false)
			if v == nil {
				t.Fatalf("no view, expected %q", tc.text)
			}
			if got := v.Text(); got != tc.text {
				t.Errorf("text %q, expected %q", got, tc.text)
			}
			if js, err := v.MarshalJSON(); err != nil || string(js) != tc.json {
				t.Errorf("json %s (%v), expected %s", js, err, tc.json)
			}
		})
	}
}

// TestCHexAlt08 is C's "%#08x".
//
// go test ./internal/goip/render/ -run TestCHexAlt08
func TestCHexAlt08(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		v           uint32
		expected    string
	}{
		{"positive: six digits fill the width", 0x800000, "0x800000"},
		{"positive: one digit is zero-padded", 2, "0x000002"},
		{"boundary: zero has no 0x", 0, "00000000"},
		{"boundary: eight digits overflow the width", 0xffffffff, "0xffffffff"},
		{"corner: seven digits", 0x1000000, "0x1000000"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			if got := cHexAlt08(tc.v); got != tc.expected {
				t.Errorf("%q, expected %q", got, tc.expected)
			}
		})
	}
}
