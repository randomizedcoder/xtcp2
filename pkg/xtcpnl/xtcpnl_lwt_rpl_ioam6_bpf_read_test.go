package xtcpnl

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP8: print_encap_rpl, print_encap_ioam6 and print_encap_bpf's read of an
// RTA_ENCAP or NHA_ENCAP payload. The captured rows are the lwtenc spaces'
// dumps (7_3_1/ws/<row>/netlink_route_dump.pcap); the rest are hand-built.

var lwtencEncapCaptured = map[string]string{
	// encap rpl segs 2001:db8:9::1
	"rplroute/dup-1": "1c000100000203010000000020010db8000900000000000000000001",
	// encap rpl segs 2001:db8:9::1,2001:db8:9::2,2001:db8:9::3: the kernel
	// stores them last first
	"rplroute/dup-2": "3c000100000603030000000020010db800090000000000000000000320010db800090000000000000000000220010db8000900000000000000000001",
	// encap rpl segs ::
	"rplroute/dup-3": "1c000100000203010000000000000000000000000000000000000000",
	// encap ioam6 trace prealloc type 0x800000 ns 1 size 12: inline, freq 1/1
	"ioam6route/dup-1": "0800040001000000080005000100000005000100010000000c0003000001080380000000",
	// … mode encap tunsrc 2001:db8:1::10 tundst 2001:db8:9::1 freq 1/2
	"ioam6route/dup-2": "0800040001000000080005000200000005000100020000001400060020010db80001000000000000000000101400020020010db80009000000000000000000010c0003000001080380000000",
	// … mode auto tundst 2001:db8:9::1
	"ioam6route/ec-004": "0800040001000000080005000100000005000100030000001400020020010db80009000000000000000000010c0003000001080380000000",
	// … type 0x2 ns 1 size 12
	"ioam6route/ec-005": "0800040001000000080005000100000005000100010000000c0003000001000300000200",
	// … freq 1000000/1000000
	"ioam6route/ec-006": "0800040040420f000800050040420f0005000100010000000c0003000001080380000000",
	// … size 244, the largest
	"ioam6route/ec-008": "0800040001000000080005000100000005000100010000000c0003000001083d80000000",
	// … ns 65535
	"ioam6route/ec-010": "0800040001000000080005000100000005000100010000000c000300ffff080380000000",
	// encap bpf in obj lwt_bpf.o sec lwt_in
	"bpfroute/dup-1": "1c000100170002006c77745f6270662e6f3a5b6c77745f696e5d0000",
	// encap bpf in … out … xmit … headroom 16: no headroom comes back
	"bpfroute/ec-004": "1c000100170002006c77745f6270662e6f3a5b6c77745f696e5d00001c000200180002006c77745f6270662e6f3a5b6c77745f6f75745d0020000300190002006c77745f6270662e6f3a5b6c77745f786d69745d00000000",
	// encap bpf in obj lwt_bpf.o sec lwt_xmit: the in hook runs the xmit section
	"bpfroute/ec-007": "20000100190002006c77745f6270662e6f3a5b6c77745f786d69745d00000000",
	// encap bpf in … sec lwt_in in … sec lwt_out: the kernel keeps the last
	"bpfroute/ec-008": "1c000100180002006c77745f6270662e6f3a5b6c77745f6f75745d00",
	// encap bpf xmit obj lwt_bpf.o sec lwt_xmit
	"bpfroute/dup-3": "20000300190002006c77745f6270662e6f3a5b6c77745f786d69745d00000000",
}

func lwtencEncap(t testing.TB, row string) []byte {
	t.Helper()
	b, err := hex.DecodeString(lwtencEncapCaptured[row])
	if err != nil || len(b) == 0 {
		t.Fatalf("%s: %v", row, err)
	}
	return b
}

func ip6(s string) [16]byte { return netip.MustParseAddr(s).As16() }

func TestParseRplEncap(t *testing.T) {
	hdr := func(segsLeft byte) []byte { return []byte{0, 2, rplTypeCst, segsLeft, 0, 0, 0, 0} }
	a1 := ip6("2001:db8:9::1")
	tests := []struct {
		description string
		val         []byte
		expected    RplEncapInfo
		err         error
	}{
		{description: "positive: captured, one seg", val: lwtencEncap(t, "rplroute/dup-1"),
			expected: RplEncapInfo{SegmentsLeft: 1, Segs: [][16]byte{a1}, HasSRH: true}},
		{description: "positive: captured, three segs, last first", val: lwtencEncap(t, "rplroute/dup-2"),
			expected: RplEncapInfo{SegmentsLeft: 3, HasSRH: true,
				Segs: [][16]byte{ip6("2001:db8:9::3"), ip6("2001:db8:9::2"), a1}}},
		{description: "positive: captured, the unspecified address", val: lwtencEncap(t, "rplroute/dup-3"),
			expected: RplEncapInfo{SegmentsLeft: 1, Segs: [][16]byte{{}}, HasSRH: true}},
		{description: "boundary: an empty payload has nothing", val: nil, expected: RplEncapInfo{}},
		{description: "boundary: a 7-byte SRH is dropped", val: rtattr(RplIptunnelSRH, hdr(1)[:7]), expected: RplEncapInfo{}},
		{description: "boundary: a bare header has no segs", val: rtattr(RplIptunnelSRH, hdr(0)),
			expected: RplEncapInfo{HasSRH: true}},
		{description: "corner: segments_left past the segs is kept, as ip reads it",
			val:      rtattr(RplIptunnelSRH, concat(hdr(4), a1[:])),
			expected: RplEncapInfo{SegmentsLeft: 4, Segs: [][16]byte{a1}, HasSRH: true}},
		{description: "corner: a partial trailing seg is dropped",
			val:      rtattr(RplIptunnelSRH, concat(hdr(1), a1[:], a1[:15])),
			expected: RplEncapInfo{SegmentsLeft: 1, Segs: [][16]byte{a1}, HasSRH: true}},
		{description: "corner: the first of a repeated SRH wins",
			val:      concat(rtattr(RplIptunnelSRH, concat(hdr(1), a1[:])), rtattr(RplIptunnelSRH, hdr(9))),
			expected: RplEncapInfo{SegmentsLeft: 1, Segs: [][16]byte{a1}, HasSRH: true}},
		{description: "corner: a short first SRH still takes the slot", val: concat(rtattr(RplIptunnelSRH, nil),
			rtattr(RplIptunnelSRH, concat(hdr(1), a1[:]))), expected: RplEncapInfo{}},
		{description: "corner: an unknown attribute is skipped", val: concat(rtattr(9, []byte{1}), rtattr(RplIptunnelSRH, hdr(0))),
			expected: RplEncapInfo{HasSRH: true}},
		{description: "negative: an attribute longer than the payload", val: []byte{12, 0, 1, 0, 0, 2}, err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseRplEncap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

func TestParseIoam6Encap(t *testing.T) {
	trace := []byte{0, 1, 0x08, 0x03, 0x80, 0, 0, 0}
	inline := Ioam6EncapInfo{Mode: Ioam6ModeInline, FreqK: 1, FreqN: 1,
		Trace:   Ioam6Trace{Namespace: 1, RemLen: 3, Type: 0x800000},
		HasMode: true, HasFreqK: true, HasFreqN: true, HasTrace: true}
	with := func(f func(*Ioam6EncapInfo)) Ioam6EncapInfo {
		e := inline
		f(&e)
		return e
	}
	src, dst := ip6("2001:db8:1::10"), ip6("2001:db8:9::1")
	tests := []struct {
		description string
		val         []byte
		expected    Ioam6EncapInfo
		err         error
	}{
		{description: "positive: captured, inline", val: lwtencEncap(t, "ioam6route/dup-1"), expected: inline},
		{description: "positive: captured, encap with tunsrc and tundst", val: lwtencEncap(t, "ioam6route/dup-2"),
			expected: with(func(e *Ioam6EncapInfo) {
				e.Mode, e.FreqN, e.Src, e.Dst, e.HasSrc, e.HasDst = Ioam6ModeEncap, 2, src, dst, true, true
			})},
		{description: "positive: captured, auto with tundst", val: lwtencEncap(t, "ioam6route/ec-004"),
			expected: with(func(e *Ioam6EncapInfo) { e.Mode, e.Dst, e.HasDst = Ioam6ModeAuto, dst, true })},
		{description: "positive: captured, type 0x2", val: lwtencEncap(t, "ioam6route/ec-005"),
			expected: with(func(e *Ioam6EncapInfo) { e.Trace.Type = 0x2 })},
		{description: "positive: captured, freq 1000000/1000000", val: lwtencEncap(t, "ioam6route/ec-006"),
			expected: with(func(e *Ioam6EncapInfo) { e.FreqK, e.FreqN = 1000000, 1000000 })},
		{description: "boundary: captured, size 244 is remlen 61", val: lwtencEncap(t, "ioam6route/ec-008"),
			expected: with(func(e *Ioam6EncapInfo) { e.Trace.RemLen = 61 })},
		{description: "boundary: captured, ns 65535", val: lwtencEncap(t, "ioam6route/ec-010"),
			expected: with(func(e *Ioam6EncapInfo) { e.Trace.Namespace = 0xffff })},
		{description: "boundary: an empty payload has nothing", val: nil, expected: Ioam6EncapInfo{}},
		{description: "boundary: a 7-byte trace is dropped", val: rtattr(Ioam6IptunnelTrace, trace[:7]), expected: Ioam6EncapInfo{}},
		{description: "boundary: a 15-byte tundst is dropped", val: rtattr(Ioam6IptunnelDst, dst[:15]), expected: Ioam6EncapInfo{}},
		{description: "boundary: a 3-byte freq is dropped", val: rtattr(Ioam6IptunnelFreqK, []byte{1, 0, 0}), expected: Ioam6EncapInfo{}},
		{description: "boundary: an empty mode is dropped", val: rtattr(Ioam6IptunnelMode, nil), expected: Ioam6EncapInfo{}},
		{description: "corner: remlen's top bit is not part of it",
			val:      rtattr(Ioam6IptunnelTrace, []byte{0, 0, 0, 0xff, 0, 0, 0, 0xff}),
			expected: Ioam6EncapInfo{Trace: Ioam6Trace{RemLen: 0x7f}, HasTrace: true}},
		{description: "corner: the first of a repeated mode wins",
			val:      concat(rtattr(Ioam6IptunnelMode, []byte{Ioam6ModeEncap}), rtattr(Ioam6IptunnelMode, []byte{Ioam6ModeAuto})),
			expected: Ioam6EncapInfo{Mode: Ioam6ModeEncap, HasMode: true}},
		{description: "corner: tunsrc in inline mode is kept, for the renderer to refuse",
			val:      concat(rtattr(Ioam6IptunnelMode, []byte{Ioam6ModeInline}), rtattr(Ioam6IptunnelSrc, src[:])),
			expected: Ioam6EncapInfo{Mode: Ioam6ModeInline, Src: src, HasMode: true, HasSrc: true}},
		{description: "negative: an attribute longer than the payload", val: []byte{8, 0, 4, 0, 1}, err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseIoam6Encap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

func TestParseBpfEncap(t *testing.T) {
	prog := func(name string) []byte { return rtattr(LwtBpfProgName, append([]byte(name), 0)) }
	in := BpfProg{Name: "lwt_bpf.o:[lwt_in]", Present: true, HasName: true}
	tests := []struct {
		description string
		val         []byte
		expected    BpfEncapInfo
		err         error
	}{
		{description: "positive: captured, in", val: lwtencEncap(t, "bpfroute/dup-1"), expected: BpfEncapInfo{In: in}},
		{description: "positive: captured, all three hooks; headroom 16 was not dumped", val: lwtencEncap(t, "bpfroute/ec-004"),
			expected: BpfEncapInfo{In: in,
				Out:  BpfProg{Name: "lwt_bpf.o:[lwt_out]", Present: true, HasName: true},
				Xmit: BpfProg{Name: "lwt_bpf.o:[lwt_xmit]", Present: true, HasName: true}}},
		{description: "positive: captured, xmit alone", val: lwtencEncap(t, "bpfroute/dup-3"),
			expected: BpfEncapInfo{Xmit: BpfProg{Name: "lwt_bpf.o:[lwt_xmit]", Present: true, HasName: true}}},
		{description: "corner: captured, the in hook with the xmit section", val: lwtencEncap(t, "bpfroute/ec-007"),
			expected: BpfEncapInfo{In: BpfProg{Name: "lwt_bpf.o:[lwt_xmit]", Present: true, HasName: true}}},
		{description: "corner: captured, in twice: the kernel kept the second", val: lwtencEncap(t, "bpfroute/ec-008"),
			expected: BpfEncapInfo{In: BpfProg{Name: "lwt_bpf.o:[lwt_out]", Present: true, HasName: true}}},
		{description: "positive: a headroom", val: rtattr(LwtBpfXmitHeadroom, []byte{16, 0, 0, 0}),
			expected: BpfEncapInfo{Headroom: 16, HasHeadroom: true}},
		{description: "boundary: an empty payload has nothing", val: nil, expected: BpfEncapInfo{}},
		{description: "boundary: a 3-byte headroom is dropped", val: rtattr(LwtBpfXmitHeadroom, []byte{16, 0, 0}),
			expected: BpfEncapInfo{}},
		{description: "boundary: an empty nest is a hook with no name", val: rtattr(LwtBpfOut, nil),
			expected: BpfEncapInfo{Out: BpfProg{Present: true}}},
		{description: "corner: a name without its NUL is whole", val: rtattr(LwtBpfIn, rtattr(LwtBpfProgName, []byte("p"))),
			expected: BpfEncapInfo{In: BpfProg{Name: "p", Present: true, HasName: true}}},
		{description: "corner: the first of a repeated hook wins",
			val:      concat(rtattr(LwtBpfIn, prog("a")), rtattr(LwtBpfIn, prog("b"))),
			expected: BpfEncapInfo{In: BpfProg{Name: "a", Present: true, HasName: true}}},
		{description: "corner: a nest with only the fd has no name",
			val:      rtattr(LwtBpfXmit, rtattr(LwtBpfProgFD, []byte{4, 0, 0, 0})),
			expected: BpfEncapInfo{Xmit: BpfProg{Present: true}}},
		{description: "corner: a nested-flagged hook is read", val: rtattr(LwtBpfIn|unix.NLA_F_NESTED, prog("a")),
			expected: BpfEncapInfo{In: BpfProg{Name: "a", Present: true, HasName: true}}},
		{description: "negative: an attribute longer than the payload", val: []byte{12, 0, 1, 0, 1}, err: ErrRTAttrSmall},
		{description: "negative: a malformed prog nest", val: rtattr(LwtBpfIn, []byte{12, 0, 2, 0, 'a'}), err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseBpfEncap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

// cHexAlt08 is C's "%#08x": a 0x prefix counted in the width, and none for 0.
func cHexAlt08(v uint32) string {
	if v == 0 {
		return "00000000"
	}
	return fmt.Sprintf("0x%06x", v)
}

// lwtencText is the encap text ip prints for a decoded rpl, ioam6 or bpf
// encap, as print_encap_rpl, print_encap_ioam6 and print_encap_bpf write it,
// and false for another type. It checks the decode against the captured
// text; the renderer proper is NX8's.
func lwtencText(e *RouteEncap) (string, bool) {
	var b strings.Builder
	switch {
	case e.Rpl != nil:
		b.WriteString("encap rpl ")
		if e.Rpl.HasSRH {
			fmt.Fprintf(&b, "segs %d [ ", e.Rpl.SegmentsLeft)
			for i := int(e.Rpl.SegmentsLeft) - 1; i >= 0 && i < len(e.Rpl.Segs); i-- {
				b.WriteString(netip.AddrFrom16(e.Rpl.Segs[i]).String() + " ")
			}
			b.WriteString("] ")
		}
	case e.Ioam6 != nil:
		i := e.Ioam6
		b.WriteString("encap ioam6 ")
		if !i.HasMode || !i.HasTrace || !i.HasFreqK || !i.HasFreqN {
			break
		}
		fmt.Fprintf(&b, "freq %d/%d ", i.FreqK, i.FreqN)
		inline := i.Mode == Ioam6ModeInline
		if (i.HasSrc && inline) || (!i.HasDst && !inline) {
			break
		}
		fmt.Fprintf(&b, "mode %s ", map[uint8]string{Ioam6ModeInline: "inline", Ioam6ModeEncap: "encap", Ioam6ModeAuto: "auto"}[i.Mode])
		if !inline {
			if i.HasSrc {
				b.WriteString("tunsrc " + netip.AddrFrom16(i.Src).String() + " ")
			}
			b.WriteString("tundst " + netip.AddrFrom16(i.Dst).String() + " ")
		}
		fmt.Fprintf(&b, "trace prealloc type %s ns %d size %d ", cHexAlt08(i.Trace.Type), i.Trace.Namespace, int(i.Trace.RemLen)*4)
	case e.Bpf != nil:
		b.WriteString("encap bpf ")
		for _, h := range []struct {
			kw string
			p  BpfProg
		}{{"in", e.Bpf.In}, {"out", e.Bpf.Out}, {"xmit", e.Bpf.Xmit}} {
			if h.p.Present {
				b.WriteString(h.kw + " ")
				if h.p.HasName {
					b.WriteString(h.p.Name + " ")
				}
			}
		}
		if e.Bpf.HasHeadroom {
			fmt.Fprintf(&b, " %d ", e.Bpf.Headroom)
		}
	default:
		return "", false
	}
	return b.String(), true
}

// TestWsLwtencEncapText decodes every rpl, ioam6 and bpf encap in the lwtenc
// spaces' dumps, on a route, a hop and a nexthop, and finds the text ip
// printed for it, followed by the hop's or route's next token.
func TestWsLwtencEncapText(t *testing.T) {
	counts := map[uint16]int{}
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		fam, _, _ := strings.Cut(row.ID, "/")
		if _, ok := lwtencSpaces[fam]; !ok || row.Result.Status != "OK" {
			continue
		}
		out, err := os.ReadFile(filepath.Join(dir, "dump.out"))
		if err != nil {
			t.Fatal(err)
		}
		var encaps []*RouteEncap
		for _, m := range pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap")) {
			switch m.h.Type {
			case unix.RTM_NEWROUTE:
				r, err := ParseNewRoute(m.body)
				if err != nil {
					t.Fatalf("%s: %v", row.ID, err)
				}
				encaps = append(encaps, r.Encap)
				for i := range r.Multipath {
					encaps = append(encaps, r.Multipath[i].Encap)
				}
			case unix.RTM_NEWNEXTHOP:
				nh, err := ParseNewNexthop(m.body)
				if err != nil {
					t.Fatalf("%s: %v", row.ID, err)
				}
				encaps = append(encaps, nh.Encap)
			}
		}
		for _, e := range encaps {
			if e == nil {
				continue
			}
			text, ok := lwtencText(e)
			if !ok {
				continue
			}
			counts[e.Type]++
			if !strings.Contains(string(out), " "+text+"dev ") && !strings.Contains(string(out), " "+text+"via ") {
				t.Errorf("%s: %q is not in dump.out:\n%s", row.ID, text, out)
			}
		}
	}
	t.Logf("encaps checked: %v", counts)
	want := map[uint16]int{unix.LWTUNNEL_ENCAP_RPL: 10, unix.LWTUNNEL_ENCAP_IOAM6: 18, unix.LWTUNNEL_ENCAP_BPF: 17}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("encaps checked %v, want %v", counts, want)
	}
}

func FuzzParseRplIoam6BpfEncap(f *testing.F) {
	for _, h := range lwtencEncapCaptured {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		r, rerr := ParseRplEncap(b)
		if rerr != nil && !reflect.DeepEqual(r, RplEncapInfo{}) {
			t.Fatalf("rpl error %v with a value %+v", rerr, r)
		}
		i, ierr := ParseIoam6Encap(b)
		if ierr != nil && i != (Ioam6EncapInfo{}) {
			t.Fatalf("ioam6 error %v with a value %+v", ierr, i)
		}
		if i.Trace.RemLen > 0x7f {
			t.Fatalf("remlen %d has more than 7 bits", i.Trace.RemLen)
		}
		bp, berr := ParseBpfEncap(b)
		if berr != nil && bp != (BpfEncapInfo{}) {
			t.Fatalf("bpf error %v with a value %+v", berr, bp)
		}
		if (rerr == nil) != (ierr == nil) {
			t.Fatalf("the same walk disagrees: rpl %v, ioam6 %v", rerr, ierr)
		}
		if rerr != nil && berr == nil {
			t.Fatalf("bpf read a payload the outer walk refuses: %v", rerr)
		}
	})
}

func BenchmarkParseRplEncap(b *testing.B) {
	val := lwtencEncap(b, "rplroute/dup-2")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseRplEncap(val); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseIoam6Encap(b *testing.B) {
	val := lwtencEncap(b, "ioam6route/dup-2")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseIoam6Encap(val); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseBpfEncap(b *testing.B) {
	val := lwtencEncap(b, "bpfroute/ec-004")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseBpfEncap(val); err != nil {
			b.Fatal(err)
		}
	}
}
