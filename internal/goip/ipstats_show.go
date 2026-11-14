package goip

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The leaves' show functions (ip/ipstats.c, ip/iplink_bridge.c,
// ip/iplink_bond.c), over the reply's tables.

// ipstatsShowHwSInfo is ipstats_show_hw_s_info: the info object on a line
// of its own, or nothing when the nest is absent.
func ipstatsShowHwSInfo(a *ipstatsAttrs, p *render.Printer, _ bool) error {
	at := a.get(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsHwSInfo)
	if at == nil {
		return nil
	}
	p.NL()
	h, err := ipstatsDissectHwSInfo(a.errOut, at)
	if err != nil {
		return err
	}
	p.OpenObject("info")
	if h.present {
		p.FPf("    %s", "l3_stats")
		p.OpenObject("l3_stats")
		h.printOne(p)
		p.CloseObject()
	}
	p.CloseObject()
	return nil
}

// ipstatsShowL3Stats is ipstats_show_hw_stats for l3_stats: request and
// used on the header's line, then the hw stats64 block. Without the
// hw_s_info nest it prints nothing, whatever L3_STATS holds.
func ipstatsShowL3Stats(a *ipstatsAttrs, p *render.Printer, _ bool) error {
	hwsi := a.get(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsHwSInfo)
	if hwsi == nil {
		return nil
	}
	stats := a.get(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, offloadXstatsL3Stats)
	h, err := ipstatsDissectHwSInfo(a.errOut, hwsi)
	if err != nil {
		return err
	}
	p.OpenObject("info")
	h.printOne(p)
	p.CloseObject()
	if stats != nil {
		p.NL()
		p.OpenObject("stats64")
		render.IPHwStats64(p, hwStats64Of(payload(stats, hwStats64SizeCst)))
		p.CloseObject()
	}
	return nil
}

// hwStats64SizeCst is sizeof(struct rtnl_hw_stats64), nine u64s.
const hwStats64SizeCst = 72

// hwStats64Of reads struct rtnl_hw_stats64 from its 72 bytes.
func hwStats64Of(b []byte) render.RtnlHwStats64 {
	u := func(i int) uint64 { return binary.NativeEndian.Uint64(b[8*i:]) }
	return render.RtnlHwStats64{
		RxPackets: u(0), TxPackets: u(1), RxBytes: u(2), TxBytes: u(3),
		RxErrors: u(4), TxErrors: u(5), RxDropped: u(6), TxDropped: u(7),
		Multicast: u(8),
	}
}

// ipstatsShowAfMpls is ipstats_stat_desc_show_afstats_mpls: nothing for a
// device with no AF_MPLS member, and ENOENT, which ends the command, for
// one whose member has no MPLS_STATS_LINK.
func ipstatsShowAfMpls(a *ipstatsAttrs, p *render.Printer, _ bool) error {
	at := a.get(unix.IFLA_STATS_AF_SPEC, unix.AF_MPLS)
	if at == nil {
		return nil
	}
	link := rtaTable(at, mplsStatsMaxCst, true)[mplsStatsLinkCst]
	if link == nil {
		return fmt.Errorf("%w: no MPLS_STATS_LINK", ErrStatsShow)
	}
	p.NL()
	p.OpenObject("mpls_stats")
	render.IPMplsLinkStats(p, mplsLinkStatsOf(link), "    ")
	p.CloseObject()
	return nil
}

// mplsLinkStatsOf is RTA_DATA(MPLS_STATS_LINK) read as struct
// mpls_link_stats, which ip does unchecked; a short payload reads as zeros.
func mplsLinkStatsOf(link []byte) xtcpnl.MplsLinkStats {
	b := payload(link, xtcpnl.MplsLinkStatsSizeCst)
	u := func(i int) uint64 { return binary.NativeEndian.Uint64(b[8*i:]) }
	return xtcpnl.MplsLinkStats{
		RxPackets: u(0), TxPackets: u(1), RxBytes: u(2), TxBytes: u(3),
		RxErrors: u(4), TxErrors: u(5), RxDropped: u(6), TxDropped: u(7),
		RxNoroute: u(8),
	}
}

// ipstatsShowXstats is ipstats_stat_desc_show_xstats: each member of the
// link type's nest whose type is inner, on a line of its own. The vlan
// suite is bridge_stat_desc_show_xstats, which puts them in a JSON array.
func ipstatsShowXstats(a *ipstatsAttrs, p *render.Printer, at, linkType, inner uint16,
	cb func(*render.Printer, []byte), vlans bool) {
	if vlans {
		p.OpenArray("vlans")
		defer p.CloseArray()
	}
	nest := a.get(at, linkType)
	if nest == nil {
		return
	}
	// rtattr_for_each_nested, which compares the type unmasked.
	for len(nest) >= unix.SizeofRtAttr {
		l := int(binary.NativeEndian.Uint16(nest[0:2]))
		if l < unix.SizeofRtAttr || l > len(nest) {
			return
		}
		if binary.NativeEndian.Uint16(nest[2:4]) == inner {
			p.NL()
			cb(p, nest[unix.SizeofRtAttr:l])
		}
		adv := (l + unix.RTA_ALIGNTO - 1) &^ (unix.RTA_ALIGNTO - 1)
		if adv >= len(nest) {
			return
		}
		nest = nest[adv:]
	}
}

// The xstats bodies, each read from RTA_DATA as the C reads its struct.

func ipstatsBridgeStp(p *render.Printer, v []byte) {
	var s xtcpnl.BridgeStpXstats
	if _, err := xtcpnl.DeserializeBridgeStpXstats(payload(v, xtcpnl.BrStpXstatsSizeCst), &s); err == nil {
		render.IPBridgeStp(p, s)
	}
}

func ipstatsBridgeMcast(p *render.Printer, v []byte) {
	var s xtcpnl.BrMcastStats
	if _, err := xtcpnl.DeserializeBrMcastStats(payload(v, xtcpnl.BrMcastStatsSizeCst), &s); err == nil {
		render.IPBridgeMcast(p, s)
	}
}

func ipstatsBridgeVlan(p *render.Printer, v []byte) {
	var s xtcpnl.BridgeVlanXstats
	if _, err := xtcpnl.DeserializeBridgeVlanXstats(payload(v, xtcpnl.BrVlanXstatsSizeCst), &s); err == nil {
		render.IPBridgeVlan(p, s)
	}
}

// ipstatsBond3ad is bond_print_3ad_stats' parse_rtattr: the first of each
// counter, read as eight bytes.
func ipstatsBond3ad(p *render.Printer, v []byte) {
	tb := rtaTable(v, xtcpnl.Bond3adStatCountCst, false)
	var s xtcpnl.Bond3adStats
	for i := range s.Values {
		if tb[i] != nil {
			s.Values[i] = binary.NativeEndian.Uint64(payload(tb[i], 8))
			s.Present[i] = true
		}
	}
	render.IPBond3ad(p, &s)
}
