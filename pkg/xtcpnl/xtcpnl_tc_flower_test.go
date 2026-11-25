package xtcpnl

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// wsTcFlowerPref is the flower filter of pref in the kernel's replies of a
// 7_3_1 workbench row, its options parsed.
func wsTcFlowerPref(t *testing.T, id string, pref uint16) TcFopt {
	t.Helper()
	dir := filepath.Join("testdata", "7_3_1", "ws", id)
	for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWTFILTER {
			continue
		}
		ti, err := ParseTcMsg(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if ti.Kind != TcFKindFlower || ti.FilterPrio != pref || !ti.HasOptions {
			continue
		}
		f, err := ParseTcFopt(ti.Kind, ti.Options)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	t.Fatalf("%s: no flower filter of pref %d", id, pref)
	return TcFopt{}
}

// hexOf is each type's payload in hex, "-" when absent, space separated.
func hexOf(tb AttrTable, types ...uint16) string {
	parts := make([]string, len(types))
	for i, ty := range types {
		parts[i] = "-"
		if tb.Has(ty) {
			parts[i] = fmt.Sprintf("%x", tb.Bytes(ty))
		}
	}
	return strings.Join(parts, " ")
}

// geneveStr is a walk as "class:type:data" steps, deficits and the error.
func geneveStr(g TcFlowerGeneve, err error) string {
	parts := make([]string, 0, len(g.Opts)+2)
	for _, o := range g.Opts {
		parts = append(parts, fmt.Sprintf("%04x:%02x:%x", o.Class, o.Type, o.Data))
	}
	for _, d := range g.Deficits {
		parts = append(parts, fmt.Sprintf("deficit %d", d.Len))
	}
	if err != nil {
		parts = append(parts, "err "+err.Error())
	}
	return strings.Join(parts, " ")
}

// mplsStr is a walk's LSEs as "raw:depth label tc bos ttl" and its deficit.
func mplsStr(m TcFlowerMplsOpts) string {
	parts := make([]string, 0, len(m.Lses)+1)
	for _, l := range m.Lses {
		s := fmt.Sprintf("%#x", l.RawType)
		if l.Lse {
			s += ":" + hexOf(l.Tb, TcaFlowerKeyMplsOptLseDepth, TcaFlowerKeyMplsOptLseLabel,
				TcaFlowerKeyMplsOptLseTc, TcaFlowerKeyMplsOptLseBos, TcaFlowerKeyMplsOptLseTTL)
		}
		parts = append(parts, s)
	}
	if m.Deficit.Any() {
		parts = append(parts, fmt.Sprintf("deficit %d", m.Deficit.Len))
	}
	return strings.Join(parts, " ")
}

// errS is "" or " err <text>".
func errS(err error) string {
	if err == nil {
		return ""
	}
	return " err " + err.Error()
}

// encOpt is the payload of opts' first option of type kind.
func encOpt(t *testing.T, f TcFopt, attr, kind uint16) []byte {
	t.Helper()
	tb, d := ParseTcFlowerEncOpts(f.Tb.Bytes(attr))
	if d.Any() || !tb.Has(kind) {
		t.Fatalf("enc opts %d: deficit %d, has %d %t", attr, d.Len, kind, tb.Has(kind))
	}
	return tb.Bytes(kind)
}

// geneveOpt is one GENEVE option: class, type and data attributes.
func geneveOpt(class uint16, typ uint8, data []byte) []byte {
	return cat(rtattr(TcaFlowerKeyEncOptGeneveClass, be16b(class)),
		rtattr(TcaFlowerKeyEncOptGeneveType, []byte{typ}),
		rtattr(TcaFlowerKeyEncOptGeneveData, data))
}

func TestParseTcFlower(t *testing.T) {
	nested := uint16(unix.NLA_F_NESTED)
	tests := []struct {
		description string
		got         func(t *testing.T) string
		expected    string
	}{
		// Positive: the captures.
		{"positive: L3/L4 keys and masks (fll3 pref 1)", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-019", 1)
			return hexOf(f.Tb, TcaFlowerKeyEthType, TcaFlowerKeyIPProto,
				TcaFlowerKeyIPv4Dst, TcaFlowerKeyIPv4DstMask, TcaFlowerKeyIPv4Src, TcaFlowerKeyIPv4SrcMask,
				TcaFlowerKeyTCPDst, TcaFlowerKeyTCPDstMask, TcaFlowerKeyTCPSrc, TcaFlowerKeyTCPSrcMask,
				TcaFlowerKeyTCPFlags, TcaFlowerKeyTCPFlagsMask, TcaFlowerKeyUDPDst)
		}, "0800 06 0a000000 ff000000 0a010000 ffff0000 0050 ffff 03e8 ff00 0002 0012 -"},
		{"positive: a port range, and v4-mapped v6 (fll3 pref 2, FL6)", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-020", 2)
			return hexOf(f.Tb, TcaFlowerKeyIPProto, TcaFlowerKeyPortDstMin, TcaFlowerKeyPortDstMax,
				TcaFlowerKeyUDPSrc, TcaFlowerKeyIPv6Src, TcaFlowerKeyIPv6DstMask)
		}, "11 03e8 07d0 0035 00000000000000000000ffff01020304 ffffffff000000000000000000000000"},
		{"positive: ICMP type and code, with the type masked (fll3 pref 4, FL5)", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-021", 4)
			return hexOf(f.Tb, TcaFlowerKeyIPProto, TcaFlowerKeyICMPv4Type, TcaFlowerKeyICMPv4TypeMsk,
				TcaFlowerKeyICMPv4Code, TcaFlowerKeyICMPv4CodeMsk)
		}, "01 08 0f 00 ff"},
		{"positive: geneve, two options and their masks", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-026", 3)
			k, err := ParseTcFlowerGeneve(encOpt(t, f, TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsGeneve))
			m, merr := ParseTcFlowerGeneve(encOpt(t, f, TcaFlowerKeyEncOptsMask, TcaFlowerKeyEncOptsGeneve))
			return geneveStr(k, err) + " / " + geneveStr(m, merr)
		}, "0102:80:11223344 0103:81:3344556677889900 / ffff:ff:ffffffff 0fff:ff:ffffffffffffffff"},
		{"positive: vxlan gbp and mask", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-022", 4)
			k, err := ParseTcFlowerVxlan(encOpt(t, f, TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsVxlan))
			m, merr := ParseTcFlowerVxlan(encOpt(t, f, TcaFlowerKeyEncOptsMask, TcaFlowerKeyEncOptsVxlan))
			return fmt.Sprintf("%d%s / %#x%s", k.Gbp, errS(err), m.Gbp, errS(merr))
		}, "1234 / 0xffff"},
		{"positive: erspan v1 index, v2 dir and hwid", func(t *testing.T) string {
			v1, err1 := ParseTcFlowerErspan(encOpt(t, wsTcFlowerPref(t, "tcflower/ec-022", 5),
				TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsErspan))
			v2, err2 := ParseTcFlowerErspan(encOpt(t, wsTcFlowerPref(t, "tcflower/ec-022", 6),
				TcaFlowerKeyEncOptsMask, TcaFlowerKeyEncOptsErspan))
			return fmt.Sprintf("%d:%d:%d:%d%s %d:%d:%d:%#x%s", v1.Ver, v1.Index, v1.Dir, v1.Hwid, errS(err1),
				v2.Ver, v2.Index, v2.Dir, v2.Hwid, errS(err2))
		}, "1:12:0:0 2:0:1:0x3f"},
		{"positive: gtp", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-027", 7)
			g, err := ParseTcFlowerGtp(encOpt(t, f, TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsGtp))
			return fmt.Sprintf("%d:%d%s", g.PduType, g.Qfi, errS(err))
		}, "1:8"},
		{"positive: pfcp keeps the 64-bit seid (FL2)", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-028", 8)
			k, err := ParseTcFlowerPfcp(encOpt(t, f, TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsPfcp))
			m, merr := ParseTcFlowerPfcp(encOpt(t, f, TcaFlowerKeyEncOptsMask, TcaFlowerKeyEncOptsPfcp))
			return fmt.Sprintf("%d:%#x%s / %#x:%#x%s", k.Type, k.Seid, errS(err), m.Type, m.Seid, errS(merr))
		}, "1:0x123456789 / 0xff:0xffffffffff"},
		{"positive: the MPLS LSE list, flagged NLA_F_NESTED", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-010", 9)
			return fmt.Sprintf("%t %s", wsNested(t, "tcflower/ec-010", 9).MplsOpts,
				mplsStr(ParseTcFlowerMplsOpts(f.Tb.Bytes(TcaFlowerKeyMplsOpts))))
		}, "true 0x8001:01 64000000 03 - - 0x8001:02 c8000000 - 01 09"},
		{"positive: cfm", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-001", 10)
			tb, d := ParseTcFlowerCfm(f.Tb.Bytes(TcaFlowerKeyCfm))
			return fmt.Sprintf("%t %s %d", wsNested(t, "tcflower/ec-001", 10).Cfm,
				hexOf(tb, TcaFlowerKeyCfmMdLevel, TcaFlowerKeyCfmOpcode), d.Len)
		}, "true 05 01 0"},
		{"positive: ct state, zone, mark and label masks", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-029", 1)
			return hexOf(f.Tb, TcaFlowerKeyCtState, TcaFlowerKeyCtStateMask, TcaFlowerKeyCtZone,
				TcaFlowerKeyCtMark, TcaFlowerKeyCtMarkMask, TcaFlowerKeyCtLabelsMask)
		}, "0a00 0b00 0500 10000000 ff000000 ffff0000000000000000000000000000"},
		// Negative: what C reads through NULL.
		{"negative: an unaligned geneve data length walks into padding (FL10)", func(*testing.T) string {
			return geneveStr(ParseTcFlowerGeneve(geneveOpt(0x0102, 0x80, []byte{1, 2, 3, 4, 5})))
		}, "0102:80:0102030405 deficit 3 err xtcpnl: flower option attribute missing: geneve class"},
		{"negative: an unpadded last type steps past the nest (FL10)", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyEncOptGeneveClass, be16b(1)), rtattr(TcaFlowerKeyEncOptGeneveData, le32b(0)),
				rtattr(TcaFlowerKeyEncOptGeneveType, []byte{2})[:5])
			return geneveStr(ParseTcFlowerGeneve(b))
		}, "0001:02:00000000 deficit -3 err xtcpnl: flower option attribute missing: geneve walk 3 bytes past the nest"},
		{"negative: a geneve option without data", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyEncOptGeneveClass, be16b(1)), rtattr(TcaFlowerKeyEncOptGeneveType, []byte{2}))
			return geneveStr(ParseTcFlowerGeneve(b))
		}, "err xtcpnl: flower option attribute missing: geneve data"},
		{"negative: a short geneve type", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyEncOptGeneveClass, be16b(1)), rtattr(TcaFlowerKeyEncOptGeneveType, nil),
				rtattr(TcaFlowerKeyEncOptGeneveData, le32b(0)))
			return geneveStr(ParseTcFlowerGeneve(b))
		}, "err xtcpnl: flower option attribute missing: geneve type"},
		{"negative: an LSE without NLA_F_NESTED is skipped (FL8)", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyMplsOptsLse, rtattr(TcaFlowerKeyMplsOptLseDepth, []byte{1})),
				rtattr(TcaFlowerKeyMplsOptsLse|nested, rtattr(TcaFlowerKeyMplsOptLseDepth, []byte{2})))
			return mplsStr(ParseTcFlowerMplsOpts(b))
		}, "0x1 0x8001:02 - - - -"},
		{"negative: erspan without a version", func(*testing.T) string {
			_, err := ParseTcFlowerErspan(rtattr(TcaFlowerKeyEncOptErspanIndex, be32b(1)))
			return errS(err)
		}, " err xtcpnl: flower option attribute missing: erspan ver"},
		{"negative: erspan v1 without an index", func(*testing.T) string {
			o, err := ParseTcFlowerErspan(rtattr(TcaFlowerKeyEncOptErspanVer, []byte{1}))
			return fmt.Sprintf("%d%s", o.Ver, errS(err))
		}, "1 err xtcpnl: flower option attribute missing: erspan index"},
		{"negative: erspan v2 without a dir", func(*testing.T) string {
			o, err := ParseTcFlowerErspan(cat(rtattr(TcaFlowerKeyEncOptErspanVer, []byte{2}),
				rtattr(TcaFlowerKeyEncOptErspanHwid, []byte{7})))
			return fmt.Sprintf("%d%s", o.Hwid, errS(err))
		}, "7 err xtcpnl: flower option attribute missing: erspan dir"},
		{"negative: erspan v2 without a hwid", func(*testing.T) string {
			_, err := ParseTcFlowerErspan(rtattr(TcaFlowerKeyEncOptErspanVer, []byte{2}))
			return errS(err)
		}, " err xtcpnl: flower option attribute missing: erspan hwid"},
		{"negative: a short vxlan gbp", func(*testing.T) string {
			_, err := ParseTcFlowerVxlan(rtattr(TcaFlowerKeyEncOptVxlanGbp, []byte{1, 2}))
			return errS(err)
		}, " err xtcpnl: flower option attribute missing: vxlan gbp"},
		{"negative: gtp without a pdu type, then without a qfi", func(*testing.T) string {
			_, err1 := ParseTcFlowerGtp(nil)
			_, err2 := ParseTcFlowerGtp(rtattr(TcaFlowerKeyEncOptGtpPduType, []byte{1}))
			return errS(err1) + errS(err2)
		}, " err xtcpnl: flower option attribute missing: gtp pdu_type err xtcpnl: flower option attribute missing: gtp qfi"},
		{"negative: pfcp without a type, then a short seid", func(*testing.T) string {
			_, err1 := ParseTcFlowerPfcp(nil)
			_, err2 := ParseTcFlowerPfcp(cat(rtattr(TcaFlowerKeyEncOptPfcpType, []byte{1}),
				rtattr(TcaFlowerKeyEncOptPfcpSeid, be32b(1))))
			return errS(err1) + errS(err2)
		}, " err xtcpnl: flower option attribute missing: pfcp type err xtcpnl: flower option attribute missing: pfcp seid"},
		// Boundary.
		{"boundary: a type above TCA_FLOWER_MAX is dropped", func(t *testing.T) string {
			f, err := ParseTcFopt(TcFKindFlower, cat(rtattr(TcaFlowerMaxKnown+1, le32b(1)), rtattr(TcaFlowerMaxKnown, be32b(2))))
			if err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%d %s", f.Tb.Len(), hexOf(f.Tb, TcaFlowerMaxKnown))
		}, "112 00000002"},
		{"boundary: a duplicate attribute, the first wins", func(t *testing.T) string {
			f, err := ParseTcFopt(TcFKindFlower, cat(rtattr(TcaFlowerKeyIPProto, []byte{6}), rtattr(TcaFlowerKeyIPProto, []byte{17})))
			if err != nil {
				t.Fatal(err)
			}
			return hexOf(f.Tb, TcaFlowerKeyIPProto)
		}, "06"},
		{"boundary: an unpadded last attribute leaves a negative deficit", func(*testing.T) string {
			b := rtattr(TcaFlowerKeyEncOptGtpPduType, []byte{1})[:5]
			_, d := ParseAttrTableLoose(b, TcaFlowerKeyEncOptGtpMaxKnown, 0)
			return fmt.Sprintf("%d %t %t", d.Len, d.HasRtaLen, d.Any())
		}, "-3 false true"},
		{"boundary: a bad header leaves a deficit with its rta_len", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyCfmMdLevel, []byte{5}), le16(99), le16(TcaFlowerKeyCfmOpcode))
			tb, d := ParseTcFlowerCfm(b)
			return fmt.Sprintf("%s %d %d %t", hexOf(tb, TcaFlowerKeyCfmMdLevel, TcaFlowerKeyCfmOpcode), d.Len, d.RtaLen, d.HasRtaLen)
		}, "05 - 4 99 true"},
		{"boundary: a table max past the bound is empty", func(*testing.T) string {
			tb, d := ParseAttrTableLoose(rtattr(1, nil), attrTableMaxCst+1, 0)
			return fmt.Sprintf("%d %d", tb.Len(), d.Len)
		}, "0 0"},
		// Corner.
		{"corner: a key flags mask with no key (FL14)", func(t *testing.T) string {
			f, err := ParseTcFopt(TcFKindFlower, rtattr(TcaFlowerKeyFlagsMask, be32b(TcaFlowerKeyFlagsIsFragment)))
			if err != nil {
				t.Fatal(err)
			}
			return hexOf(f.Tb, TcaFlowerKeyFlags, TcaFlowerKeyFlagsMask)
		}, "- 00000001"},
		{"corner: ip_flags frag/nofirstfrag (fll3 pref 10)", func(t *testing.T) string {
			f := wsTcFlowerPref(t, "tcflower/ec-013", 10)
			return hexOf(f.Tb, TcaFlowerKeyFlags, TcaFlowerKeyFlagsMask)
		}, "00000001 00000003"},
		{"corner: MPLS opts and cfm without NLA_F_NESTED print nothing", func(*testing.T) string {
			n := ParseTcFlowerNested(cat(rtattr(TcaFlowerKeyMplsOpts, nil), rtattr(TcaFlowerKeyCfm|nested, nil),
				rtattr(TcaFlowerKeyMplsOpts|nested, nil)))
			return fmt.Sprintf("%t %t", n.MplsOpts, n.Cfm)
		}, "false true"},
		{"corner: a geneve step takes the first of each type from all that remains", func(*testing.T) string {
			b := cat(rtattr(TcaFlowerKeyEncOptGeneveData, le32b(0x44332211)), rtattr(TcaFlowerKeyEncOptGeneveClass, be16b(7)),
				rtattr(TcaFlowerKeyEncOptGeneveType, []byte{9}))
			return geneveStr(ParseTcFlowerGeneve(b))
		}, "0007:09:11223344"},
		{"corner: a gtp option with a nested-flagged attribute drops it", func(*testing.T) string {
			_, err := ParseTcFlowerGtp(cat(rtattr(TcaFlowerKeyEncOptGtpPduType|nested, []byte{1}),
				rtattr(TcaFlowerKeyEncOptGtpQfi, []byte{2})))
			return errS(err)
		}, " err xtcpnl: flower option attribute missing: gtp pdu_type"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.got(t); got != tt.expected {
				t.Errorf("got  %q\nwant %q", got, tt.expected)
			}
		})
	}
}

// wsNested is ParseTcFlowerNested of the flower filter of pref in a row.
func wsNested(t *testing.T, id string, pref uint16) TcFlowerNested {
	t.Helper()
	dir := filepath.Join("testdata", "7_3_1", "ws", id)
	for _, m := range pcapNlMsgs(t, filepath.Join(dir, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWTFILTER {
			continue
		}
		ti, err := ParseTcMsg(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if ti.Kind == TcFKindFlower && ti.FilterPrio == pref && ti.HasOptions {
			return ParseTcFlowerNested(ti.Options)
		}
	}
	t.Fatalf("%s: no flower filter of pref %d", id, pref)
	return TcFlowerNested{}
}

func TestBuildTcFlowerGet(t *testing.T) {
	f := uint16(unix.RTM_GETTFILTER)
	tests := []struct {
		description string
		row         string // a 7_3_1 workbench row whose ip.pcap holds the request
		q           TcFilterReq
		proto       uint16
		expected    string // the kind options in hex
	}{
		{"positive: protocol ip sends FLAGS 0 and ETH_TYPE", "tcflower/ec-019",
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactIngress, Prio: 1, Protocol: ethPIP}, ethPIP,
			"1400020008001600000000000600080008000000"},
		{"positive: a chain get", "tcflower/ec-040",
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactIngress, Prio: 1, Protocol: ethPIP, Chain: 3, ChainSet: true},
			ethPIP, "1400020008001600000000000600080008000000"},
		{"boundary: handle 0xffffffff", "tcflower/ec-038",
			TcFilterReq{Ifindex: wsD0, Handle: 0xffffffff, Parent: tcHClsactIngress, Prio: 4, Protocol: ethPIP}, ethPIP,
			"1400020008001600000000000600080008000000"},
		{"corner: protocol all sends FLAGS only (FL17)", "tcflower/ec-011",
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactIngress, Prio: 11, Protocol: ethPAll}, ethPAll,
			"0c0002000800160000000000"},
		{"corner: an unnamed protocol", "tcflower/ec-012",
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactIngress, Prio: 13, Protocol: 0x88b5}, 0x88b5,
			"1400020008001600000000000600080088b50000"},
		{"corner: a block get sends the block index, whatever the parent (FL22)", "tcflower/ec-041",
			TcFilterReq{Ifindex: TcmIfindexMagicBlock, Handle: 1, Parent: 20, Prio: 1, Protocol: ethPIP}, ethPIP,
			"1400020008001600000000000600080008000000"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			opts, err := TcFlowerGetOpts(tt.proto)
			if err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprintf("%x", opts); got != tt.expected {
				t.Errorf("opts %s, expected %s", got, tt.expected)
			}
			want := wsTcShowRequest(t, tt.row, f)
			got, err := BuildTcFilterGet(f, tt.q, TcFKindFlower, opts, nlSeqOf(want))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("got  % x\nwant % x", got, want)
			}
		})
	}
}

// TestWsTcFlower decodes every flower filter in the tcflower captures:
// each nest flower_print_opt opens must decode with no missing attribute
// and no deficit, and each enc option kind, LSEs and cfm must appear.
func TestWsTcFlower(t *testing.T) {
	seen := map[string]int{}
	pcaps, err := filepath.Glob(filepath.Join("testdata", "7_3_1", "ws", "tcflower", "*", "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pcaps) == 0 {
		t.Fatal("no tcflower captures")
	}
	for _, p := range pcaps {
		for i, m := range pcapNlMsgs(t, p) {
			if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWTFILTER || m.h.Flags&unix.NLM_F_REQUEST != 0 {
				continue
			}
			ti, err := ParseTcMsg(m.body)
			if err != nil {
				t.Fatalf("%s message %d: %v", p, i, err)
			}
			if ti.Kind != TcFKindFlower || !ti.HasOptions {
				continue
			}
			seen["filter"]++
			if err := wsTcFlowerProblem(ti.Options, seen); err != nil {
				t.Errorf("%s message %d: %v", p, i, err)
			}
		}
	}
	for _, k := range []string{"geneve", "vxlan", "erspan", "gtp", "pfcp", "lse", "cfm", "act"} {
		if seen[k] == 0 {
			t.Errorf("no %s in the captures", k)
		}
	}
	t.Logf("decoded %v", seen)
}

// wsTcFlowerProblem decodes every nest of one flower filter's options.
func wsTcFlowerProblem(opt []byte, seen map[string]int) error {
	f, err := ParseTcFopt(TcFKindFlower, opt)
	if err != nil {
		return err
	}
	if b := f.Tb.Bytes(TcaFlowerAct); b != nil {
		if _, err := walkTcActs(b); err != nil {
			return err
		}
		seen["act"]++
	}
	for _, attr := range []uint16{TcaFlowerKeyEncOpts, TcaFlowerKeyEncOptsMask} {
		if !f.Tb.Has(attr) {
			continue
		}
		tb, d := ParseTcFlowerEncOpts(f.Tb.Bytes(attr))
		if d.Any() {
			return fmt.Errorf("enc opts deficit %d", d.Len)
		}
		if err := wsTcFlowerEnc(tb, seen); err != nil {
			return err
		}
	}
	n := ParseTcFlowerNested(opt)
	if f.Tb.Has(TcaFlowerKeyMplsOpts) {
		if !n.MplsOpts {
			return errors.New("MPLS opts without NLA_F_NESTED")
		}
		m := ParseTcFlowerMplsOpts(f.Tb.Bytes(TcaFlowerKeyMplsOpts))
		if m.Deficit.Any() {
			return fmt.Errorf("MPLS deficit %d", m.Deficit.Len)
		}
		for _, l := range m.Lses {
			if !l.Lse || l.Deficit.Any() {
				return fmt.Errorf("LSE raw type %#x, deficit %d", l.RawType, l.Deficit.Len)
			}
			seen["lse"]++
		}
	}
	if f.Tb.Has(TcaFlowerKeyCfm) {
		if _, d := ParseTcFlowerCfm(f.Tb.Bytes(TcaFlowerKeyCfm)); d.Any() || !n.Cfm {
			return fmt.Errorf("cfm deficit %d, nested %t", d.Len, n.Cfm)
		}
		seen["cfm"]++
	}
	return nil
}

// wsTcFlowerEnc decodes the option kinds of one ENC_OPTS table.
func wsTcFlowerEnc(tb AttrTable, seen map[string]int) error {
	type dec struct {
		name string
		fn   func([]byte) (AttrDeficit, error)
	}
	decs := map[uint16]dec{
		TcaFlowerKeyEncOptsGeneve: {"geneve", func(b []byte) (AttrDeficit, error) {
			g, err := ParseTcFlowerGeneve(b)
			if len(g.Deficits) > 0 {
				return g.Deficits[0], err
			}
			return AttrDeficit{}, err
		}},
		TcaFlowerKeyEncOptsVxlan: {"vxlan", func(b []byte) (AttrDeficit, error) {
			o, err := ParseTcFlowerVxlan(b)
			return o.Deficit, err
		}},
		TcaFlowerKeyEncOptsErspan: {"erspan", func(b []byte) (AttrDeficit, error) {
			o, err := ParseTcFlowerErspan(b)
			return o.Deficit, err
		}},
		TcaFlowerKeyEncOptsGtp: {"gtp", func(b []byte) (AttrDeficit, error) {
			o, err := ParseTcFlowerGtp(b)
			return o.Deficit, err
		}},
		TcaFlowerKeyEncOptsPfcp: {"pfcp", func(b []byte) (AttrDeficit, error) {
			o, err := ParseTcFlowerPfcp(b)
			return o.Deficit, err
		}},
	}
	for k, d := range decs {
		if !tb.Has(k) {
			continue
		}
		def, err := d.fn(tb.Bytes(k))
		if err != nil {
			return err
		}
		if def.Any() {
			return fmt.Errorf("%s deficit %d", d.name, def.Len)
		}
		seen[d.name]++
	}
	return nil
}

// FuzzParseTcFlowerOpt decodes a flower TCA_OPTIONS and every nest in it.
func FuzzParseTcFlowerOpt(f *testing.F) {
	for _, b := range wsTcSeeds(f, map[uint16]bool{unix.RTM_NEWTFILTER: true}) {
		if ti, err := ParseTcMsg(b); err == nil && ti.Kind == TcFKindFlower && ti.HasOptions {
			f.Add(ti.Options)
		}
	}
	f.Add(rtattr(TcaFlowerKeyMplsOpts|uint16(unix.NLA_F_NESTED), rtattr(tcFlowerLseRawCst, rtattr(1, []byte{1}))))
	f.Fuzz(func(t *testing.T, in []byte) {
		fo, err := ParseTcFopt(TcFKindFlower, in)
		if err != nil {
			return
		}
		if fo.Tb.Len() != int(TcaFlowerMaxKnown)+1 {
			t.Fatalf("table of %d", fo.Tb.Len())
		}
		m := ParseTcFlowerMplsOpts(fo.Tb.Bytes(TcaFlowerKeyMplsOpts))
		if len(m.Lses)*RTAttrSizeCst > len(in) {
			t.Fatalf("%d LSEs from %d bytes", len(m.Lses), len(in))
		}
		seen := map[string]int{}
		if wsTcFlowerProblem(in, seen) == nil && seen["lse"]*RTAttrSizeCst > len(in) {
			t.Fatalf("%d LSEs from %d bytes", seen["lse"], len(in))
		}
	})
}

// FuzzParseTcFlowerEncOpts walks an option payload with each decoder.
func FuzzParseTcFlowerEncOpts(f *testing.F) {
	f.Add(cat(geneveOpt(1, 2, le32b(3)), geneveOpt(4, 5, le64b(6))))
	f.Add(geneveOpt(1, 2, []byte{1, 2, 3, 4, 5}))
	f.Add(cat(rtattr(TcaFlowerKeyEncOptPfcpType, []byte{1}), rtattr(TcaFlowerKeyEncOptPfcpSeid, le64b(2))))
	f.Fuzz(func(t *testing.T, in []byte) {
		g, err := ParseTcFlowerGeneve(in)
		total := 0
		for _, o := range g.Opts {
			total += len(o.Data) + tcFlowerGeneveHdrCst
		}
		if err == nil && total != len(in) {
			t.Fatalf("walk of %d bytes over %d", total, len(in))
		}
		for _, d := range g.Deficits {
			if d.Len > len(in) || d.Len < -3 {
				t.Fatalf("deficit %d of %d bytes", d.Len, len(in))
			}
		}
		if o, err := ParseTcFlowerErspan(in); err == nil && o.Ver == 1 && (o.Dir != 0 || o.Hwid != 0) {
			t.Fatalf("v1 with dir %d hwid %d", o.Dir, o.Hwid)
		}
		if _, err := ParseTcFlowerVxlan(in); err != nil && !errors.Is(err, ErrTcFlowerOptMissing) {
			t.Fatal(err)
		}
		if _, err := ParseTcFlowerGtp(in); err != nil && !errors.Is(err, ErrTcFlowerOptMissing) {
			t.Fatal(err)
		}
		if _, err := ParseTcFlowerPfcp(in); err != nil && !errors.Is(err, ErrTcFlowerOptMissing) {
			t.Fatal(err)
		}
	})
}

func BenchmarkParseTcFlower(b *testing.B) {
	l3 := cat(rtattr(TcaFlowerKeyEthType, be16b(ethPIP)), rtattr(TcaFlowerKeyIPProto, []byte{6}),
		rtattr(TcaFlowerKeyIPv4Dst, be32b(0x0a000001)), rtattr(TcaFlowerKeyIPv4DstMask, be32b(0xffffffff)),
		rtattr(TcaFlowerKeyTCPDst, be16b(80)), rtattr(TcaFlowerFlags, le32b(0)))
	geneve := cat(geneveOpt(0x0102, 0x80, le32b(1)), geneveOpt(0x0103, 0x81, le64b(2)))
	lse := rtattr(tcFlowerLseRawCst, cat(rtattr(TcaFlowerKeyMplsOptLseDepth, []byte{1}),
		rtattr(TcaFlowerKeyMplsOptLseLabel, le32b(100))))
	pfcp := cat(rtattr(TcaFlowerKeyEncOptPfcpType, []byte{1}), rtattr(TcaFlowerKeyEncOptPfcpSeid, le64b(2)))
	cases := []struct {
		name string
		fn   func() error
	}{
		{"opt", func() error { _, err := ParseTcFopt(TcFKindFlower, l3); return err }},
		{"geneve", func() error { _, err := ParseTcFlowerGeneve(geneve); return err }},
		{"mpls", func() error { ParseTcFlowerMplsOpts(cat(lse, lse)); return nil }},
		{"pfcp", func() error { _, err := ParseTcFlowerPfcp(pfcp); return err }},
		{"get", func() error { _, err := TcFlowerGetOpts(ethPIP); return err }},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := c.fn(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
