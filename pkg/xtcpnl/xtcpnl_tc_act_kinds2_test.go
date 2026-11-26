package xtcpnl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The crafted options below follow the kernel's dump order for each kind
// (net/sched/act_<kind>.c tcf_<kind>_dump), with tc_gen index 7, action
// pipe, ref 1.

func a2le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

func a2tg() []byte { return cat(le32b(7), le32b(0), le32b(3), le32b(1), le32b(0)) }

// a2peditSel is a tc_pedit_sel claiming nkeys keys and holding keys of
// them: key i at offset 4i, val i+1, mask 0x00ffffff.
func a2peditSel(nkeys uint8, keys int) []byte {
	b := cat(a2tg(), []byte{nkeys, 0, 0, 0})
	for i := range keys {
		u := uint32(i)
		b = cat(b, le32b(0x00ffffff), le32b(u+1), le32b(4*u), le32b(0), le32b(0), le32b(0))
	}
	return b
}

// a2keyEx is one KEY_EX nest with HTYPE ip4 and CMD add, either left out
// when its flag is false.
func a2keyEx(htype, cmd bool) []byte {
	var kids []byte
	if htype {
		kids = cat(kids, rtattr(TcaPeditKeyExHtype, a2le16(PeditHtypeIP4)))
	}
	if cmd {
		kids = cat(kids, rtattr(TcaPeditKeyExCmd, a2le16(PeditCmdAdd)))
	}
	return rtattr(TcaPeditKeyEx, kids)
}

// a2pedit is pedit's options: a sel of nkeys claimed and keys held, as
// PARMS, or as PARMS_EX with exKids full KEY_EX children.
func a2pedit(nkeys uint8, keys int, ex bool, exKids int) []byte {
	if !ex {
		return rtattr(TcaPeditParms, a2peditSel(nkeys, keys))
	}
	var kids []byte
	for range exKids {
		kids = cat(kids, a2keyEx(true, true))
	}
	return cat(rtattr(TcaPeditParmsEx, a2peditSel(nkeys, keys)), rtattr(TcaPeditKeysEx, kids))
}

// a2geneveKids is n geneve options' children: class 0x0102+i, type
// 0x80+i, four bytes of data.
func a2geneveKids(n int) []byte {
	var b []byte
	for i := range n {
		u := uint16(i)
		b = cat(b, rtattr(TcaTunnelKeyEncOptGeneveClass, be16b(0x0102+u)),
			rtattr(TcaTunnelKeyEncOptGeneveType, []byte{0x80 + byte(i)}),
			rtattr(TcaTunnelKeyEncOptGeneveData, be32b(0x00880022+uint32(u))))
	}
	return b
}

// a2tk is tunnel_key's options: set, with an ENC_OPTS of opts when given.
func a2tk(opts []byte) []byte {
	b := cat(rtattr(TcaTunnelKeyParms, cat(a2tg(), le32b(TcaTunnelKeyActSet))),
		rtattr(TcaTunnelKeyEncIPv4Src, []byte{10, 0, 0, 1}), rtattr(TcaTunnelKeyEncIPv4Dst, []byte{10, 0, 0, 2}))
	if opts != nil {
		b = cat(b, rtattr(TcaTunnelKeyEncOpts|unix.NLA_F_NESTED, opts))
	}
	return b
}

func a2ife() []byte {
	return cat(rtattr(TcaIfeParms, cat(a2tg(), a2le16(IfeEncode), []byte{0, 0})),
		rtattr(TcaIfeDmac, []byte{2, 0, 0, 0, 0, 1}), rtattr(TcaIfeType, a2le16(0xED3E)),
		rtattr(TcaIfeMetalst, rtattr(IfeMetaSkbmark, le32b(7))))
}

// a2ct is a commit with a v6 src nat range, ports and a full label.
func a2ct() []byte {
	v6 := func(last byte) []byte { return []byte{0x20, 1, 0xd, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, last} }
	ones := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	return cat(rtattr(TcaCtParms, a2tg()), rtattr(TcaCtAction, a2le16(TcaCtActCommit|TcaCtActNat|TcaCtActNatSrc)),
		rtattr(TcaCtZone, a2le16(0)), rtattr(TcaCtLabels, v6(1)), rtattr(TcaCtLabelsMask, ones),
		rtattr(TcaCtNatIPv6Min, v6(1)), rtattr(TcaCtNatIPv6Max, v6(9)),
		rtattr(TcaCtNatPortMin, be16b(1000)), rtattr(TcaCtNatPortMax, be16b(2000)))
}

// a2gate is a gate of n entries, even ones open, every ipv and max-octets
// a wildcard.
func a2gate(n int) []byte {
	var list []byte
	for i := range n {
		e := rtattr(TcaGateEntryIndex, le32b(uint32(i)))
		if i%2 == 0 {
			e = cat(e, rtattr(TcaGateEntryGate, nil))
		}
		e = cat(e, rtattr(TcaGateEntryInterval, le32b(100000000)), rtattr(TcaGateEntryMaxOctets, le32b(math.MaxUint32)),
			rtattr(TcaGateEntryIpv, le32b(math.MaxUint32)))
		list = cat(list, rtattr(TcaGateOneEntry, e))
	}
	return cat(rtattr(TcaGateParms, a2tg()), rtattr(TcaGateBaseTime, le64b(0)), rtattr(TcaGateClockid, le32b(11)),
		rtattr(TcaGatePriority, le32b(2)), rtattr(TcaGateEntryList, list))
}

// a2ops is n sock_filters "6 0 0 i".
func a2ops(n int) []byte {
	b := make([]byte, 0, n*TcBpfInsnSizeCst)
	for i := range n {
		b = cat(b, a2le16(6), []byte{0, 0}, le32b(uint32(i)))
	}
	return b
}

// a2actBpf is the bpf action with OPS_LEN opsLen over n instructions.
func a2actBpf(opsLen uint16, n int) []byte {
	return cat(rtattr(TcaActBpfParms, a2tg()), rtattr(TcaActBpfOpsLen, a2le16(opsLen)), rtattr(TcaActBpfOps, a2ops(n)))
}

func a2bpfFilter() []byte {
	return cat(rtattr(TcaBpfClassid, le32b(0x10001)), rtattr(TcaBpfOpsLen, a2le16(4)), rtattr(TcaBpfOps, a2ops(4)),
		rtattr(TcaBpfFlags, le32b(TcaBpfFlagActDirect)), rtattr(TcaBpfFlagsGen, le32b(9)))
}

// The benchmarks' inputs.
var (
	benchPedit     = a2pedit(8, 8, true, 8)
	benchTunnelKey = a2tk(rtattr(TcaTunnelKeyEncOptsGeneve, a2geneveKids(2)))
	benchIfe       = a2ife()
	benchCt        = a2ct()
	benchGate      = a2gate(16)
	benchActBpf    = a2actBpf(64, 64)
	benchBpfFilter = a2bpfFilter()
)

// wsTcFilterOpts is every filter of kind in a 7_3_1 workbench row's
// RTM_NEWTFILTER replies, by handle: its TCA_OPTIONS.
func wsTcFilterOpts(t *testing.T, id, kind string) map[uint32][]byte {
	t.Helper()
	out := map[uint32][]byte{}
	for _, m := range pcapNlMsgs(t, filepath.Join("testdata", "7_3_1", "ws", id, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWTFILTER || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		ti, err := ParseTcMsg(m.body)
		if err != nil {
			t.Fatal(err)
		}
		if ti.Kind == kind && ti.HasOptions {
			out[ti.Hdr.Handle] = ti.Options
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no %s filter", id, kind)
	}
	return out
}

func peditStr(o TcPeditOpt) string {
	ht, cmd := make([]uint16, 0, len(o.KeysEx)), make([]uint16, 0, len(o.KeysEx))
	for _, k := range o.KeysEx {
		ht, cmd = append(ht, k.Htype), append(cmd, k.Cmd)
	}
	s := fmt.Sprintf("ex %t nkeys %d keys %d short %t ret %d htypes %v cmds %v", o.Ex, o.Nkeys, len(o.Keys), o.KeysShort,
		o.KeysExRet, ht, cmd)
	for i, k := range o.Keys {
		if i < 3 {
			s += fmt.Sprintf(" | off %d val %08x mask %08x", k.Off, bits.ReverseBytes32(k.Val), bits.ReverseBytes32(k.Mask))
		}
	}
	return s
}

func geneveOptsStr(o TcTunnelKeyEncOpts) string {
	parts := make([]string, 0, len(o.Geneve))
	for _, g := range o.Geneve {
		parts = append(parts, fmt.Sprintf("%04x:%02x:%x", g.Class, g.Type, g.Data))
	}
	return fmt.Sprintf("%s bad %t", strings.Join(parts, ","), o.GeneveBad)
}

func gateStr(o TcGateOpt) string {
	parts := make([]string, 0, len(o.Entries))
	for _, e := range o.Entries {
		parts = append(parts, fmt.Sprintf("%d %t %d %d %d %t", e.Index, e.Gate, e.Interval, e.Ipv, e.MaxOctets, e.Short))
	}
	return strings.Join(parts, " | ")
}

func cIntStr(c TcCInt) string {
	return fmt.Sprintf("%#x has %t len %d past %t", c.V, c.Has, c.Len, c.Past)
}

// TestParseTcActKinds2 is the table for the six kinds' decoders and the bpf
// filter's: the tcaction2 corpus (positive), parms and children short or
// missing (negative), counts at and past their payloads (boundary), and
// the reads tc makes past an attribute (corner).
func TestParseTcActKinds2(t *testing.T) {
	ped := wsTcActOpts(t, "tcaction2/pw-001", TcActKindPedit)
	tk := wsTcActOpts(t, "tcaction2/ec-010", TcActKindTunnelKey)
	ife := wsTcActOpts(t, "tcaction2/ec-018", TcActKindIfe)
	ct := wsTcActOpts(t, "tcaction2/ec-024", TcActKindCt)
	gate := wsTcActOpts(t, "tcaction2/ec-033", TcActKindGate)
	abpf := wsTcActOpts(t, "tcaction2/ec-040", TcActKindBpf)
	fbpf := wsTcFilterOpts(t, "tcaction2/ec-044", TcFKindBpf)

	short := func(dec func([]byte) (TcGen, TcActCommon, error), parms uint16) func() (string, error) {
		return func() (string, error) {
			g, c, err := dec(rtattr(parms, le32b(7)))
			return fmt.Sprintf("short %t len %d index %d action %d", c.HasParms && c.ParmsLen < 20, c.ParmsLen, g.Index,
				g.Action), err
		}
	}
	tests := []struct {
		description string
		input       func() (string, error)
		expected    string
	}{
		// Positive: the corpus.
		{"positive: pedit ex, 8 keys over eth, ip6, tcp and udp", func() (string, error) {
			o, err := ParseTcPedit(ped[3])
			return peditStr(o), err
		}, "ex true nkeys 8 keys 8 short false ret 0 htypes [1 1 3 3 3 3 4 5] cmds [0 0 0 0 0 0 0 0]" +
			" | off 0 val 02000000 mask 00000000 | off 4 val 00090000 mask 0000ffff | off 24 val 20010db8 mask 00000000"},
		{"positive: pedit ex ip ttl add 1", func() (string, error) {
			o, err := ParseTcPedit(ped[2])
			return peditStr(o), err
		}, "ex true nkeys 1 keys 1 short false ret 0 htypes [2] cmds [1] | off 8 val 01000000 mask 00ffffff"},
		{"positive: legacy pedit, 2 keys and a retain", func() (string, error) {
			o, err := ParseTcPedit(ped[4])
			return fmt.Sprintf("%s action %d", peditStr(o), o.P.Action), err
		}, "ex false nkeys 2 keys 2 short false ret 0 htypes [] cmds [] | off 12 val 08000000 mask 00ffffff" +
			" | off 16 val 0a000001 mask 00000000 action 2"},
		{"positive: tunnel_key set v4 with every scalar", func() (string, error) {
			o, err := ParseTcTunnelKey(tk[2])
			return fmt.Sprintf("t_action %d %s", o.TAction, hexOf(o.Tb, TcaTunnelKeyEncIPv4Src, TcaTunnelKeyEncIPv4Dst,
				TcaTunnelKeyEncKeyID, TcaTunnelKeyEncDstPort, TcaTunnelKeyNoCsum, TcaTunnelKeyEncTos, TcaTunnelKeyEncTTL,
				TcaTunnelKeyNoFrag)), err
		}, "t_action 1 0a000001 0a000002 00000007 12b5 01 10 40 "},
		{"positive: tunnel_key unset", func() (string, error) {
			o, err := ParseTcTunnelKey(tk[1])
			return fmt.Sprintf("t_action %d opts %t", o.TAction, o.HasOpts), err
		}, "t_action 2 opts false"},
		{"positive: tunnel_key v6 with 2 geneve options", func() (string, error) {
			o, err := ParseTcTunnelKey(tk[3])
			return geneveOptsStr(o.Opts) + " " + hexOf(o.Tb, TcaTunnelKeyEncIPv6Dst), err
		}, "0102:80:00880022,0103:81:00a000b0 bad false 20010db8000000000000000000000002"},
		{"positive: tunnel_key vxlan gbp, erspan v1 and v2", func() (string, error) {
			v, err := ParseTcTunnelKey(tk[4])
			if err != nil {
				return "", err
			}
			e1, err := ParseTcTunnelKey(tk[5])
			if err != nil {
				return "", err
			}
			e2, err := ParseTcTunnelKey(tk[6])
			return fmt.Sprintf("%+v %+v %+v", v.Opts.Vxlan, e1.Opts.Erspan, e2.Opts.Erspan), err
		}, "{Gbp:4660 GbpOK:true} {Ver:1 Index:12 Dir:0 Hwid:0 OK:true} {Ver:2 Index:0 Dir:1 Hwid:7 OK:true}"},
		{"positive: ife encode with type, macs and use mark 7", func() (string, error) {
			o, err := ParseTcIfe(ife[1])
			return fmt.Sprintf("flags %d %s | %s", o.Flags, hexOf(o.Tb, TcaIfeType, TcaIfeDmac, TcaIfeSmac),
				hexOf(o.Meta, IfeMetaSkbmark, IfeMetaTcindex, IfeMetaPrio)), err
		}, "flags 1 3eed 020000000001 020000000002 | 07000000 - -"},
		{"positive: ife allow mark, use tcindex 3, allow prio; and decode", func() (string, error) {
			o, err := ParseTcIfe(ife[2])
			if err != nil {
				return "", err
			}
			d, err := ParseTcIfe(ife[3])
			return fmt.Sprintf("%s | flags %d %s", hexOf(o.Meta, IfeMetaSkbmark, IfeMetaTcindex, IfeMetaPrio), d.Flags,
				hexOf(d.Tb, TcaIfeType)), err
		}, " 0300  | flags 0 0000"},
		{"positive: ct commit with zone, masked mark and label", func() (string, error) {
			o, err := ParseTcCt(ct[2])
			return fmt.Sprintf("action %d %s labels-unchecked %t", o.CtAction, hexOf(o.Tb, TcaCtZone, TcaCtMark,
				TcaCtMarkMask, TcaCtLabels, TcaCtLabelsMask), o.LabelsUnchecked), err
		}, "action 1 9401 42000000 f0000000 12312300000000000000000000000001 ffffffff000000000000000000000001" +
			" labels-unchecked false"},
		{"positive: ct src nat range with ports", func() (string, error) {
			o, err := ParseTcCt(ct[5])
			return fmt.Sprintf("action %d %s nat-unchecked %t", o.CtAction, hexOf(o.Tb, TcaCtNatIPv4Min, TcaCtNatIPv4Max,
				TcaCtNatPortMin, TcaCtNatPortMax), o.NatUnchecked), err
		}, "action 25 0a000001 0a000009 03e8 07d0 nat-unchecked false"},
		{"positive: gate with a 3-entry schedule", func() (string, error) {
			o, err := ParseTcGate(gate[1])
			return gateStr(o) + " / " + hexOf(o.Tb, TcaGatePriority, TcaGateClockid, TcaGateFlags), err
		}, "0 true 100000000 3 8000 false | 1 false 100000000 -1 -1 false | 2 true 100000000 1 -1 false" +
			" / 02000000 0b000000 00000000"},
		{"positive: bpf action, 4 instructions", func() (string, error) {
			o, err := ParseTcActBpf(abpf[1])
			return fmt.Sprintf("%t %d %v short %t name %t", o.HasOps, o.Ops.Len, o.Ops.Insns, o.Ops.Short, o.HasName), err
		}, "true 4 [{40 0 0 12} {21 0 1 2048} {6 0 0 262144} {6 0 0 0}] short false name false"},
		{"positive: bpf filter with classid; with da and skip_hw", func() (string, error) {
			f1, err := ParseTcFopt(TcFKindBpf, fbpf[1])
			if err != nil {
				return "", err
			}
			f2, err := ParseTcFopt(TcFKindBpf, fbpf[2])
			return fmt.Sprintf("%s %d | %s %v", hexOf(f1.Tb, TcaBpfClassid, TcaBpfFlags), len(f1.Bpf.Ops.Insns),
				hexOf(f2.Tb, TcaBpfFlags, TcaBpfFlagsGen), f2.Bpf.Ops.Insns), err
		}, "01000100 - 4 | 01000000 09000000 [{6 0 0 0}]"},
		// Negative: parms 4 bytes long, MIN-copied (AC40).
		{"negative: pedit PARMS of 4 bytes", short(actKindDecode[TcActKindPedit], TcaPeditParms),
			"short true len 4 index 7 action 0"},
		{"negative: tunnel_key PARMS of 4 bytes", short(actKindDecode[TcActKindTunnelKey], TcaTunnelKeyParms),
			"short true len 4 index 7 action 0"},
		{"negative: ife PARMS of 4 bytes", short(actKindDecode[TcActKindIfe], TcaIfeParms),
			"short true len 4 index 7 action 0"},
		{"negative: ct PARMS of 4 bytes", short(actKindDecode[TcActKindCt], TcaCtParms),
			"short true len 4 index 7 action 0"},
		{"negative: gate PARMS of 4 bytes", short(actKindDecode[TcActKindGate], TcaGateParms),
			"short true len 4 index 7 action 0"},
		{"negative: bpf PARMS of 4 bytes", short(actKindDecode[TcActKindBpf], TcaActBpfParms),
			"short true len 4 index 7 action 0"},
		{"negative: KEY_EX without HTYPE; without CMD (AC45)", func() (string, error) {
			k1, r1 := ParseTcPeditKeysEx(a2keyEx(false, true), 1)
			k2, r2 := ParseTcPeditKeysEx(a2keyEx(true, false), 1)
			return fmt.Sprintf("%+v %d %+v %d", k1, r1, k2, r2), nil
		}, "[{Htype:0 Cmd:1 HtypeOK:false CmdOK:true}] 0 [{Htype:2 Cmd:0 HtypeOK:true CmdOK:false}] 0"},
		{"negative: KEYS_EX of more, fewer or foreign children than nkeys", func() (string, error) {
			_, more := ParseTcPeditKeysEx(cat(a2keyEx(true, true), a2keyEx(true, true)), 1)
			_, fewer := ParseTcPeditKeysEx(a2keyEx(true, true), 2)
			_, foreign := ParseTcPeditKeysEx(rtattr(TcaPeditKeyEx|unix.NLA_F_NESTED, nil), 1)
			o, err := ParseTcPedit(rtattr(TcaPeditParmsEx, a2peditSel(1, 1)))
			return fmt.Sprintf("%d %d %d | keys-ex %t", more, fewer, foreign, o.HasKeysEx), err
		}, "-1 1 -1 | keys-ex false"},
		{"negative: a geneve option without CLASS (AC46)", func() (string, error) {
			o := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsGeneve, cat(
				rtattr(TcaTunnelKeyEncOptGeneveType, []byte{1}), rtattr(TcaTunnelKeyEncOptGeneveData, be32b(1)))))
			return geneveOptsStr(o), nil
		}, " bad true"},
		{"negative: geneve data of 6 bytes puts tc's 20-byte stride off the next option (AC46)", func() (string, error) {
			o := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsGeneve, cat(rtattr(TcaTunnelKeyEncOptGeneveClass, be16b(1)),
				rtattr(TcaTunnelKeyEncOptGeneveType, []byte{2}), rtattr(TcaTunnelKeyEncOptGeneveData, []byte{1, 2, 3, 4, 5, 6}))))
			return geneveOptsStr(o), nil
		}, "0001:02:010203040506 bad true"},
		{"negative: vxlan without GBP; erspan v2 without HWID; v1 with a 2-byte INDEX (AC47)", func() (string, error) {
			v := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsVxlan, nil))
			e2 := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsErspan, cat(rtattr(TcaTunnelKeyEncOptErspanVer, []byte{2}),
				rtattr(TcaTunnelKeyEncOptErspanDir, []byte{1}))))
			e1 := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsErspan, cat(rtattr(TcaTunnelKeyEncOptErspanVer, []byte{1}),
				rtattr(TcaTunnelKeyEncOptErspanIndex, []byte{0, 1}))))
			return fmt.Sprintf("%t %t %t", v.Vxlan.GbpOK, e2.Erspan.OK, e1.Erspan.OK), nil
		}, "false false false"},
		{"negative: ct NAT_IPV4_MAX without MIN, under nat src; and with nat off (AC54)", func() (string, error) {
			opt := func(act uint16) []byte {
				return cat(rtattr(TcaCtParms, a2tg()), rtattr(TcaCtAction, a2le16(act)),
					rtattr(TcaCtNatIPv4Max, []byte{10, 0, 0, 9}))
			}
			on, err := ParseTcCt(opt(TcaCtActNat | TcaCtActNatSrc))
			if err != nil {
				return "", err
			}
			off, err := ParseTcCt(opt(TcaCtActCommit))
			return fmt.Sprintf("%t %t", on.NatUnchecked, off.NatUnchecked), err
		}, "true false"},
		{"negative: ct v4 bounds equal fall through to an IPv6 MAX without MIN (AC54)", func() (string, error) {
			v4 := []byte{10, 0, 0, 1}
			base := cat(rtattr(TcaCtParms, a2tg()), rtattr(TcaCtAction, a2le16(TcaCtActNat|TcaCtActNatSrc)),
				rtattr(TcaCtNatIPv4Min, v4), rtattr(TcaCtNatIPv4Max, v4))
			eq, err := ParseTcCt(cat(base, rtattr(TcaCtNatIPv6Max, make([]byte, 16))))
			if err != nil {
				return "", err
			}
			ne, err := ParseTcCt(cat(rtattr(TcaCtParms, a2tg()), rtattr(TcaCtAction, a2le16(TcaCtActNat|TcaCtActNatSrc)),
				rtattr(TcaCtNatIPv4Min, v4), rtattr(TcaCtNatIPv4Max, []byte{10, 0, 0, 9}),
				rtattr(TcaCtNatIPv6Max, make([]byte, 16))))
			return fmt.Sprintf("%t %t", eq.NatUnchecked, ne.NatUnchecked), err
		}, "true false"},
		{"negative: ct LABELS without its mask; with an 8-byte mask (AC55)", func() (string, error) {
			l := rtattr(TcaCtLabels, make([]byte, 16))
			a, err := ParseTcCt(cat(rtattr(TcaCtParms, a2tg()), l))
			if err != nil {
				return "", err
			}
			b, err := ParseTcCt(cat(rtattr(TcaCtParms, a2tg()), l, rtattr(TcaCtLabelsMask, make([]byte, 8))))
			return fmt.Sprintf("%t %t", a.LabelsUnchecked, b.LabelsUnchecked), err
		}, "true true"},
		// Boundary.
		{"boundary: pedit nkeys 255 (u8's max), all there", func() (string, error) {
			o, err := ParseTcPedit(a2pedit(255, 255, false, 0))
			return fmt.Sprintf("keys %d short %t last off %d", len(o.Keys), o.KeysShort, o.Keys[254].Off), err
		}, "keys 255 short false last off 1016"},
		{"boundary: pedit nkeys 3 over a payload of 2 keys (AC40)", func() (string, error) {
			o, err := ParseTcPedit(a2pedit(3, 2, false, 0))
			return fmt.Sprintf("keys %d short %t", len(o.Keys), o.KeysShort), err
		}, "keys 2 short true"},
		{"boundary: OPS_LEN 0, = payload/8, = payload/8+1 (AC63); OPS_LEN of 1 byte", func() (string, error) {
			var parts []string
			for _, n := range []uint16{0, 4, 5} {
				o, err := ParseTcActBpf(a2actBpf(n, 4))
				if err != nil {
					return "", err
				}
				parts = append(parts, fmt.Sprintf("%d:%d:%t", n, len(o.Ops.Insns), o.Ops.Short))
			}
			o, err := ParseTcActBpf(cat(rtattr(TcaActBpfParms, a2tg()), rtattr(TcaActBpfOpsLen, []byte{1}),
				rtattr(TcaActBpfOps, a2ops(1))))
			return strings.Join(parts, " ") + fmt.Sprintf(" short-len %t", o.Ops.Short), err
		}, "0:0:false 4:4:false 5:4:true short-len true"},
		{"boundary: gate ipv and max-octets -1, INT32_MIN; a 2-byte IPV (AC60)", func() (string, error) {
			e := ParseTcGateEntries(cat(
				rtattr(TcaGateOneEntry, cat(rtattr(TcaGateEntryIpv, le32b(math.MaxUint32)),
					rtattr(TcaGateEntryMaxOctets, le32b(1<<31)))),
				rtattr(TcaGateOneEntry, rtattr(TcaGateEntryIpv, []byte{1, 0})),
				rtattr(TcaGateOneEntry, nil)))
			return gateStr(TcGateOpt{Entries: e}), nil
		}, "0 false 0 -1 -2147483648 false | 0 false 0 0 -1 true | 0 false 0 -1 -1 false"},
		{"boundary: ct labels of 63 and 64 bytes against out[256] (AC55)", func() (string, error) {
			lab := func(n int) []byte {
				return cat(rtattr(TcaCtParms, a2tg()), rtattr(TcaCtLabels, make([]byte, n)), rtattr(TcaCtLabelsMask, make([]byte, n)))
			}
			a, err := ParseTcCt(lab(63))
			if err != nil {
				return "", err
			}
			b, err := ParseTcCt(lab(64))
			return fmt.Sprintf("%t %t", a.LabelsUnchecked, b.LabelsUnchecked), err
		}, "false true"},
		// Corner.
		{"corner: the ftp helper's family is 0 (K13); proto 6", func() (string, error) {
			o, err := ParseTcCt(ct[8])
			name, ok := TcCString(o.Tb.Bytes(TcaCtHelperName))
			return fmt.Sprintf("%s / %s / %s %t", cIntStr(o.HelperFamily), cIntStr(o.HelperProto), name, ok), err
		}, "0x0 has true len 1 past false / 0x6 has true len 1 past false / ftp true"},
		{"corner: HELPER_FAMILY as 1 byte with set padding, as 4 bytes, and last with no padding (AC56)", func() (string, error) {
			one := rtattr(TcaCtHelperFamily, []byte{2})
			one[5], one[6], one[7] = 1, 1, 1
			four := rtattr(TcaCtHelperFamily, le32b(10))
			last := rtattr(TcaCtHelperFamily, []byte{2})[:5]
			return fmt.Sprintf("%s / %s / %s", cIntStr(tcCIntAt(one, TcaCtHelperFamily)),
				cIntStr(tcCIntAt(four, TcaCtHelperFamily)), cIntStr(tcCIntAt(last, TcaCtHelperFamily))), nil
		}, "0x1010102 has true len 1 past false / 0xa has true len 4 past false / 0x2 has true len 1 past true"},
		{"corner: a NAME with no NUL (AC64); with one", func() (string, error) {
			a, err := ParseTcActBpf(cat(rtattr(TcaActBpfParms, a2tg()), rtattr(TcaActBpfName, []byte("prog"))))
			if err != nil {
				return "", err
			}
			b, err := ParseTcFopt(TcFKindBpf, rtattr(TcaBpfName, []byte("cls\x00x")))
			return fmt.Sprintf("%q %t | %q %t", a.Name, a.NameNoNUL, b.Bpf.Name, b.Bpf.NameNoNUL), err
		}, `"prog" true | "cls" false`},
		{"corner: a geneve list whose first option lacks TYPE borrows the second's", func() (string, error) {
			o := ParseTcTunnelKeyOpts(rtattr(TcaTunnelKeyEncOptsGeneve, cat(
				rtattr(TcaTunnelKeyEncOptGeneveClass, be16b(1)), rtattr(TcaTunnelKeyEncOptGeneveData, be32b(1)),
				rtattr(TcaTunnelKeyEncOptGeneveType, []byte{9}))))
			return geneveOptsStr(o), nil
		}, "0001:09:00000001 bad false"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := tt.input()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.expected {
				t.Errorf("got  %q\nwant %q", got, tt.expected)
			}
		})
	}
}

// act2Problem is what TestWsTcAction2 rejects in one of the six kinds'
// options: anything a printer would read past or through NULL.
func act2Problem(kind string, opt []byte) error {
	var bad bool
	var err error
	switch kind {
	case TcActKindPedit:
		var o TcPeditOpt
		o, err = ParseTcPedit(opt)
		bad = o.KeysShort || (o.Ex && (!o.HasKeysEx || o.KeysExRet != 0))
		for _, k := range o.KeysEx {
			bad = bad || !k.HtypeOK || !k.CmdOK
		}
	case TcActKindTunnelKey:
		var o TcTunnelKeyOpt
		o, err = ParseTcTunnelKey(opt)
		e := o.Opts
		bad = e.GeneveBad || (e.Tb.Has(TcaTunnelKeyEncOptsVxlan) && !e.Vxlan.GbpOK) ||
			(e.Tb.Has(TcaTunnelKeyEncOptsErspan) && !e.Erspan.OK)
	case TcActKindIfe:
		_, err = ParseTcIfe(opt)
	case TcActKindCt:
		var o TcCtOpt
		o, err = ParseTcCt(opt)
		bad = o.NatUnchecked || o.LabelsUnchecked || o.HelperFamily.Past || o.HelperProto.Past
	case TcActKindGate:
		var o TcGateOpt
		o, err = ParseTcGate(opt)
		for _, e := range o.Entries {
			bad = bad || e.Short
		}
	case TcActKindBpf:
		var o TcActBpfOpt
		o, err = ParseTcActBpf(opt)
		bad = o.Ops.Short || o.NameNoNUL
	default:
		return nil
	}
	if err == nil && bad {
		err = errors.New("a read tc makes past its attribute")
	}
	return err
}

// act2Tab checks the six kinds in an action table (a filter's ACT).
func act2Tab(b []byte, seen map[string]int) error {
	tab, err := ParseTcActTab(b)
	if err != nil {
		return err
	}
	for i := uint16(0); i <= TcaActMaxPrioCst; i++ {
		if !tab.Has(i) {
			continue
		}
		one, err := ParseTcActOne(tab.Bytes(i))
		if err != nil {
			return err
		}
		k, _ := one.Str(TcaActKind)
		if _, ok := tcAct2Sizes[k]; !ok || !one.Has(TcaActOptions) {
			continue
		}
		if err := act2Problem(k, one.Bytes(TcaActOptions)); err != nil {
			return fmt.Errorf("%s at bind 1: %w", k, err)
		}
		seen[k+"@1"]++
	}
	return nil
}

var tcAct2Sizes = map[string]int{
	TcActKindPedit: TcPeditSelSizeCst, TcActKindTunnelKey: TcTunnelKeySizeCst, TcActKindIfe: TcIfeSizeCst,
	TcActKindCt: TcCtSizeCst, TcActKindGate: TcGateSizeCst, TcActKindBpf: TcActBpfSizeCst,
}

// tcAction2Pcaps is the tcaction2 corpus and the tdc pcaps of the six
// kinds and of the bpf filter.
func tcAction2Pcaps(tb testing.TB) []string {
	tb.Helper()
	globs := make([]string, 0, 2+len(tcAct2Sizes))
	globs = append(globs, filepath.Join("testdata", "7_3_1", "ws", "tcaction2", "*", "ip.pcap"),
		filepath.Join("testdata", "7_3_0", "ws", "tdc", "filters", "bpf", "*", "*.pcap"))
	for k := range tcAct2Sizes {
		globs = append(globs, filepath.Join("testdata", "7_3_0", "ws", "tdc", "actions", k, "*", "*.pcap"))
	}
	var out []string
	for _, g := range globs {
		m, err := filepath.Glob(g)
		if err != nil {
			tb.Fatal(err)
		}
		if len(m) == 0 {
			tb.Fatalf("no captures for %s", g)
		}
		out = append(out, m...)
	}
	return out
}

// TestWsTcAction2 decodes every action of the six kinds, in `tc actions`
// replies (bind 0) and inside filters (bind 1), and every bpf filter, in
// the tcaction2 captures and the tdc pcaps: none may need a read past its
// attribute, and each kind must appear at bind 0, and the six again at
// bind 1 in the tcaction2 matchall.
func TestWsTcAction2(t *testing.T) {
	seen := map[string]int{}
	for _, p := range tcAction2Pcaps(t) {
		for i, m := range pcapNlMsgs(t, p) {
			if m.proto != unix.NETLINK_ROUTE || m.h.Flags&unix.NLM_F_REQUEST != 0 {
				continue
			}
			switch {
			case isActReply(m.h.Type, m.h.Flags):
				ai, err := ParseTcAction(m.body)
				if err != nil {
					t.Fatalf("%s message %d: %v", p, i, err)
				}
				for _, a := range ai.Actions {
					if _, ok := tcAct2Sizes[a.Kind]; !ok || a.Options == nil {
						continue
					}
					if err := act2Problem(a.Kind, a.Options); err != nil {
						t.Errorf("%s message %d %s: %v", p, i, a.Kind, err)
					}
					seen[a.Kind]++
				}
			case m.h.Type == unix.RTM_NEWTFILTER:
				ti, err := ParseTcMsg(m.body)
				if err != nil {
					t.Fatalf("%s message %d: %v", p, i, err)
				}
				if !ti.HasOptions || (ti.Kind != TcFKindBpf && ti.Kind != TcFKindMatchall) {
					continue
				}
				f, err := ParseTcFopt(ti.Kind, ti.Options)
				if err != nil {
					t.Fatalf("%s message %d: %v", p, i, err)
				}
				act := TcaMatchallAct
				if ti.Kind == TcFKindBpf {
					act = TcaBpfAct
					if f.Bpf.Ops.Short || f.Bpf.NameNoNUL {
						t.Errorf("%s message %d: bpf filter %+v", p, i, f.Bpf)
					}
					seen["filter:bpf"]++
				}
				if b := f.Tb.Bytes(act); b != nil {
					if err := act2Tab(b, seen); err != nil {
						t.Errorf("%s message %d: %v", p, i, err)
					}
				}
			}
		}
	}
	for k := range tcAct2Sizes {
		if seen[k] == 0 || seen[k+"@1"] == 0 {
			t.Errorf("%s: %d at bind 0, %d at bind 1", k, seen[k], seen[k+"@1"])
		}
	}
	if seen["filter:bpf"] == 0 {
		t.Error("no bpf filter")
	}
	t.Logf("decoded %v", seen)
}

// FuzzParseTcActKinds2 runs the list decoders on any bytes: none may
// panic, and nothing decoded may be longer than its input.
func FuzzParseTcActKinds2(f *testing.F) {
	f.Add(benchPedit, uint16(8))
	f.Add(benchTunnelKey, uint16(2))
	f.Add(benchGate, uint16(16))
	f.Add(a2ops(4), uint16(5))
	f.Add(rtattr(TcaTunnelKeyEncOptsGeneve, a2geneveKids(3)), uint16(1))
	f.Fuzz(func(t *testing.T, in []byte, n uint16) {
		keys, _ := ParseTcPeditKeysEx(in, int(n%256))
		if len(keys) > int(n%256) {
			t.Fatalf("%d keys for nkeys %d", len(keys), n%256)
		}
		o := ParseTcTunnelKeyOpts(in)
		for _, g := range o.Geneve {
			if len(g.Data) > len(in) {
				t.Fatal("geneve data longer than the input")
			}
		}
		if e := ParseTcGateEntries(in); len(e)*RTAttrSizeCst > len(in) {
			t.Fatalf("%d entries from %d bytes", len(e), len(in))
		}
		if ops := ParseTcBpfOps(in, n); len(ops.Insns)*TcBpfInsnSizeCst > len(in) || ops.Short != (int(n) > len(in)/8) {
			t.Fatalf("ops %d short %t from %d bytes for %d", len(ops.Insns), ops.Short, len(in), n)
		}
		if c := tcCIntAt(in, TcaCtHelperFamily); c.Has && c.Len > len(in) {
			t.Fatalf("helper family of %d bytes from %d", c.Len, len(in))
		}
	})
}

// BenchmarkWsTcAction2 decodes every action of the six kinds in the
// tcaction2 corpus per iteration.
func BenchmarkWsTcAction2(b *testing.B) {
	type act struct {
		kind string
		opt  []byte
	}
	var acts []act
	m, err := filepath.Glob(filepath.Join("testdata", "7_3_1", "ws", "tcaction2", "*", "ip.pcap"))
	if err != nil {
		b.Fatal(err)
	}
	for _, p := range m {
		for _, msg := range pcapNlMsgs(b, p) {
			if msg.proto != unix.NETLINK_ROUTE || !isActReply(msg.h.Type, msg.h.Flags) {
				continue
			}
			ai, err := ParseTcAction(msg.body)
			if err != nil {
				b.Fatal(err)
			}
			for _, a := range ai.Actions {
				if _, ok := tcAct2Sizes[a.Kind]; ok && a.Options != nil {
					acts = append(acts, act{a.Kind, a.Options})
				}
			}
		}
	}
	if len(acts) == 0 {
		b.Fatal("no actions")
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, a := range acts {
			if err := act2Problem(a.kind, a.opt); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(acts)), "ns/action")
}

// TestParseTcActKinds2Allocs pins the decoders' allocations: a regression
// is a failure here, not a number in a benchmark. The budgets were
// measured, then pinned: the table and the parms copy, then one per list
// (ife's meta table, the instructions, the entries, the keys), and a
// list's elements add none.
func TestParseTcActKinds2Allocs(t *testing.T) {
	allocs := func(fn func() error) float64 {
		return testing.AllocsPerRun(100, func() {
			if err := fn(); err != nil {
				t.Fatal(err)
			}
		})
	}
	pedit := func(n uint8, ex bool) func() error {
		in := a2pedit(n, int(n), ex, int(n))
		return func() error { _, err := ParseTcPedit(in); return err }
	}
	gate := func(n int) func() error {
		in := a2gate(n)
		return func() error { _, err := ParseTcGate(in); return err }
	}
	ops := func(n int) func() error {
		in := a2actBpf(uint16(n), n)
		return func() error { _, err := ParseTcActBpf(in); return err }
	}
	geneve := func(n int) func() error {
		in := a2tk(rtattr(TcaTunnelKeyEncOptsGeneve, a2geneveKids(n)))
		return func() error { _, err := ParseTcTunnelKey(in); return err }
	}
	tests := []struct {
		description string
		input       func() float64
		expected    float64
	}{
		{"positive: ife", func() float64 { return allocs(func() error { _, err := ParseTcIfe(benchIfe); return err }) }, 3},
		{"positive: ct with nat and labels", func() float64 {
			return allocs(func() error { _, err := ParseTcCt(benchCt); return err })
		}, 2},
		{"positive: bpf action header and 64 ops", func() float64 { return allocs(ops(64)) }, 3},
		{"positive: bpf filter", func() float64 {
			return allocs(func() error { _, err := ParseTcFopt(TcFKindBpf, benchBpfFilter); return err })
		}, 2},
		{"boundary: pedit 255 keys adds nothing over 1 key", func() float64 { return allocs(pedit(255, false)) - allocs(pedit(1, false)) }, 0},
		{"boundary: pedit ex 255 keys adds nothing over 1 key", func() float64 { return allocs(pedit(255, true)) - allocs(pedit(1, true)) }, 0},
		{"boundary: gate 64 entries adds nothing over 1", func() float64 { return allocs(gate(64)) - allocs(gate(1)) }, 0},
		{"boundary: 4096 ops add nothing over 1", func() float64 { return allocs(ops(4096)) - allocs(ops(1)) }, 0},
		{"boundary: 16 geneve options add nothing over 1", func() float64 { return allocs(geneve(16)) - allocs(geneve(1)) }, 0},
		{"corner: an empty table, PARMS only", func() float64 {
			in := rtattr(TcaGateParms, a2tg())
			return allocs(func() error { _, err := ParseTcGate(in); return err })
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.input(); got != tt.expected {
				t.Errorf("got %v allocations, want %v", got, tt.expected)
			}
		})
	}
}
