package render

import (
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// printEncapRpl is print_encap_rpl and print_rpl_srh: `segs N [ … ]` with N
// the SRH's segments_left, and the addresses from segaddr[N-1] down to
// segaddr[0]; in JSON a "segs" array. ip prints nothing without the SRH.
// print_rpl_srh reads N addresses whatever the attribute's length; an
// address past the attribute's end, which the kernel never sends, is left
// out here rather than read.
func printEncapRpl(p *Printer, e *xtcpnl.RplEncapInfo) {
	if !e.HasSRH {
		return
	}
	p.OpenArray("segs")
	p.FPf("segs %d [ ", e.SegmentsLeft)
	for i := min(int(e.SegmentsLeft), len(e.Segs)) - 1; i >= 0; i-- {
		p.String(PrintAny, "", "%s ", addrString(e.Segs[i][:], unix.AF_INET6))
	}
	p.CloseArray()
	p.FPf("] ")
}

// ioam6ModeNames is ioam6_mode_types (ip/iproute_lwtunnel.c).
var ioam6ModeNames = map[uint8]string{
	xtcpnl.Ioam6ModeInline: "inline",
	xtcpnl.Ioam6ModeEncap:  encapCst,
	xtcpnl.Ioam6ModeAuto:   "auto",
}

// ioam6ModeName is format_ioam6mode_type.
func ioam6ModeName(mode uint8) string {
	if n, ok := ioam6ModeNames[mode]; ok {
		return n
	}
	return unknownNameCst
}

// printEncapIoam6 is print_encap_ioam6. Nothing prints without the mode,
// the trace and both freqs; the freq alone when a tunnel source is on an
// inline route, or a non-inline route has no tunnel destination.
func printEncapIoam6(p *Printer, e *xtcpnl.Ioam6EncapInfo) {
	if !e.HasMode || !e.HasTrace || !e.HasFreqK || !e.HasFreqN {
		return
	}
	p.U64(PrintAny, "freqk", "freq %d", uint64(e.FreqK))
	p.U64(PrintAny, "freqn", "/%d ", uint64(e.FreqN))
	inline := e.Mode == xtcpnl.Ioam6ModeInline
	if (e.HasSrc && inline) || (!e.HasDst && !inline) {
		return
	}
	p.String(PrintAny, "mode", "mode %s ", ioam6ModeName(e.Mode))
	if !inline {
		if e.HasSrc {
			p.String(PrintAny, "tunsrc", "tunsrc %s ", addrString(e.Src[:], unix.AF_INET6))
		}
		p.String(PrintAny, "tundst", "tundst %s ", addrString(e.Dst[:], unix.AF_INET6))
	}
	p.Null(PrintAny, "trace", "trace ")
	p.Null(PrintAny, "prealloc", "prealloc ")
	// print_hex: the JSON a "%x" string, the text through "type %#08x ".
	p.String(PrintJSON, "type", "", strconv.FormatUint(uint64(e.Trace.Type), 16))
	p.FPf("type %s ", cHexAlt08(e.Trace.Type))
	p.U64(PrintAny, "ns", "ns %d ", uint64(e.Trace.Namespace))
	p.U64(PrintAny, "size", "size %d ", uint64(e.Trace.RemLen)*4)
}

// cHexAlt08 is C's "%#08x": "0x" and the hex digits zero-padded to eight
// characters in all, except that zero has no "0x" and is "00000000".
func cHexAlt08(v uint32) string {
	const width = 8
	if v == 0 {
		return "00000000"
	}
	h := strconv.FormatUint(uint64(v), 16)
	for len(h)+2 < width {
		h = "0" + h
	}
	return "0x" + h
}

// printEncapBpf is print_encap_bpf: each present hook through
// print_encap_bpf_prog, then the headroom, which the kernel never dumps.
func printEncapBpf(p *Printer, e *xtcpnl.BpfEncapInfo) {
	for _, h := range []struct {
		label string
		prog  *xtcpnl.BpfProg
	}{{"in", &e.In}, {"out", &e.Out}, {"xmit", &e.Xmit}} {
		if h.prog.Present {
			printEncapBPFProg(p, h.label, h.prog.Name, h.prog.HasName)
		}
	}
	if e.HasHeadroom {
		p.U64(PrintAny, "headroom", " %d ", uint64(e.Headroom))
	}
}
