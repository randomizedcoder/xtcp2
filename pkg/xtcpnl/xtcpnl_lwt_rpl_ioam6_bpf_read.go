package xtcpnl

import "encoding/binary"

// LWT_BPF_* (include/uapi/linux/lwtunnel.h): an LWTUNNEL_ENCAP_BPF payload's
// attributes. IN, OUT and XMIT are nests of LWT_BPF_PROG_*.
const (
	LwtBpfIn           uint16 = 1
	LwtBpfOut          uint16 = 2
	LwtBpfXmit         uint16 = 3
	LwtBpfXmitHeadroom uint16 = 4 // u32; the kernel takes it but never dumps it
)

const (
	rplSRHHdrLenCst = 8  // struct ipv6_rpl_sr_hdr before its segments
	rplSegLenCst    = 16 // struct in6_addr
	in6AddrLenCst   = 16
)

// RplEncapInfo is an LWTUNNEL_ENCAP_RPL RTA_ENCAP payload on a read, as
// print_encap_rpl (ip/iproute_lwtunnel.c) reads it.
type RplEncapInfo struct {
	// SegmentsLeft is the SRH's segments_left, which print_rpl_srh prints
	// and counts its loop by: it prints segaddr[SegmentsLeft-1] down to
	// segaddr[0], without checking them against the attribute's length.
	SegmentsLeft uint8
	// Segs are the SRH's whole 16-byte addresses in wire order, as many as
	// the attribute holds.
	Segs [][rplSegLenCst]byte
	// HasSRH is RPL_IPTUNNEL_SRH present with at least the SRH's 8-byte
	// header; ip prints nothing without it.
	HasSRH bool
}

// ParseRplEncap decodes an LWTUNNEL_ENCAP_RPL RTA_ENCAP payload. The first
// of a repeated attribute wins, as in parse_rtattr, and an SRH shorter than
// its header is dropped.
func ParseRplEncap(val []byte) (RplEncapInfo, error) {
	var (
		e    RplEncapInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) || atype != RplIptunnelSRH || len(v) < rplSRHHdrLenCst {
			return
		}
		e.HasSRH, e.SegmentsLeft = true, v[3]
		segs := v[rplSRHHdrLenCst:]
		if n := len(segs) / rplSegLenCst; n > 0 {
			e.Segs = make([][rplSegLenCst]byte, n)
			for i := range e.Segs {
				copy(e.Segs[i][:], segs[i*rplSegLenCst:])
			}
		}
	})
	if err != nil {
		return RplEncapInfo{}, err
	}
	return e, nil
}

// Ioam6Trace is IOAM6_IPTUNNEL_TRACE's struct ioam6_trace_hdr, the fields
// print_encap_ioam6 prints.
type Ioam6Trace struct {
	Namespace uint16 // namespace_id, read big-endian: `ns`
	// RemLen is remlen, in 4-octet units: ip prints `size` as RemLen*4.
	RemLen uint8
	// Type is ntohl(type_be32)>>8, the 24 trace-type bits ip prints as
	// `type %#08x`.
	Type uint32
}

// Ioam6EncapInfo is an LWTUNNEL_ENCAP_IOAM6 RTA_ENCAP payload on a read, as
// print_encap_ioam6 reads it. ip prints nothing without the mode, trace and
// both freqs, and stops after the freq when the mode and the tunnel
// addresses disagree; that choice is the renderer's.
type Ioam6EncapInfo struct {
	Mode         uint8 // IOAM6_IPTUNNEL_MODE_*
	FreqK, FreqN uint32
	Src, Dst     [in6AddrLenCst]byte // `tunsrc`, `tundst`
	Trace        Ioam6Trace

	HasMode, HasFreqK, HasFreqN, HasSrc, HasDst, HasTrace bool
}

// ParseIoam6Encap decodes an LWTUNNEL_ENCAP_IOAM6 RTA_ENCAP payload. The
// first of a repeated attribute wins, and one shorter than its width is
// dropped.
func ParseIoam6Encap(val []byte) (Ioam6EncapInfo, error) {
	var (
		e    Ioam6EncapInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case Ioam6IptunnelMode:
			if len(v) >= 1 {
				e.Mode, e.HasMode = v[0], true
			}
		case Ioam6IptunnelFreqK:
			if len(v) >= 4 {
				e.FreqK, e.HasFreqK = binary.LittleEndian.Uint32(v), true
			}
		case Ioam6IptunnelFreqN:
			if len(v) >= 4 {
				e.FreqN, e.HasFreqN = binary.LittleEndian.Uint32(v), true
			}
		case Ioam6IptunnelSrc:
			if len(v) >= in6AddrLenCst {
				e.HasSrc = copy(e.Src[:], v) > 0
			}
		case Ioam6IptunnelDst:
			if len(v) >= in6AddrLenCst {
				e.HasDst = copy(e.Dst[:], v) > 0
			}
		case Ioam6IptunnelTrace:
			if len(v) >= ioam6TraceLenCst {
				e.Trace = Ioam6Trace{
					Namespace: binary.BigEndian.Uint16(v[0:2]),
					RemLen:    v[3] & 0x7f,
					Type:      binary.BigEndian.Uint32(v[4:8]) >> 8,
				}
				e.HasTrace = true
			}
		}
	})
	if err != nil {
		return Ioam6EncapInfo{}, err
	}
	return e, nil
}

// BpfProg is one LWT_BPF_IN, OUT or XMIT nest: ip prints the hook when the
// nest is present, then the program's name when it has one (`<unknown>` in
// JSON when it has none).
type BpfProg struct {
	Name             string // LWT_BPF_PROG_NAME up to its NUL
	Present, HasName bool
}

// BpfEncapInfo is an LWTUNNEL_ENCAP_BPF RTA_ENCAP payload on a read, as
// print_encap_bpf reads it.
type BpfEncapInfo struct {
	In, Out, Xmit BpfProg
	// Headroom is LWT_BPF_XMIT_HEADROOM, which ip would print, but the
	// kernel's bpf_fill_encap_info never dumps it.
	Headroom    uint32
	HasHeadroom bool
}

// ParseBpfEncap decodes an LWTUNNEL_ENCAP_BPF RTA_ENCAP payload. The first
// of a repeated attribute wins, a short headroom is dropped, and a malformed
// prog nest is an error.
func ParseBpfEncap(val []byte) (BpfEncapInfo, error) {
	var (
		e       BpfEncapInfo
		seen    attrSeen
		nestErr error
	)
	prog := func(p *BpfProg, v []byte) {
		name, ok, err := parseBPFProgName(v)
		if err != nil {
			nestErr = err
			return
		}
		*p = BpfProg{Name: name, Present: true, HasName: ok}
	}
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) || nestErr != nil {
			return
		}
		switch atype {
		case LwtBpfIn:
			prog(&e.In, v)
		case LwtBpfOut:
			prog(&e.Out, v)
		case LwtBpfXmit:
			prog(&e.Xmit, v)
		case LwtBpfXmitHeadroom:
			if len(v) >= 4 {
				e.Headroom, e.HasHeadroom = binary.LittleEndian.Uint32(v), true
			}
		}
	})
	if err == nil {
		err = nestErr
	}
	if err != nil {
		return BpfEncapInfo{}, err
	}
	return e, nil
}
