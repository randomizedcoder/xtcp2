package xtcpnl

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The captures' fixtures have d0 as ifindex 3 and d1 as 4.
const (
	wsD0 = 3
	wsD1 = 4

	tcHClsactIngress = 0xFFFFFFF2 // TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_INGRESS)
	tcHClsactEgress  = 0xFFFFFFF3 // TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_EGRESS)
	ethPIP           = 0x0800
	ethPAll          = 0x0003
)

// wsTcFilter is the filter of kind and handle in the kernel's replies of a
// 7_3_1 workbench row.
func wsTcFilter(t *testing.T, id, kind string, handle uint32) TcInfo {
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
		if ti.Kind == kind && ti.Hdr.Handle == handle {
			return ti
		}
	}
	t.Fatalf("%s: no %s filter with handle %#x", id, kind, handle)
	return TcInfo{}
}

// fmtErr is vals separated by spaces, or the error.
func fmtErr(t *testing.T, err error, vals ...any) string {
	t.Helper()
	if err != nil {
		return "err: " + err.Error()
	}
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, " ")
}

// errName names the error classes the tables expect.
func errName(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, ErrTcShort):
		return "short"
	case errors.Is(err, ErrRTAttrSmall):
		return "rtattr"
	}
	return err.Error()
}

// u32Sel is a struct tc_u32_sel with nkeys claimed and keys appended.
func u32Sel(flags, nkeys uint8, keys ...[]byte) []byte {
	b := make([]byte, TcU32SelSizeCst, TcU32SelSizeCst+len(keys)*TcU32KeySizeCst)
	b[0], b[2] = flags, nkeys
	for _, k := range keys {
		b = append(b, k...)
	}
	return b
}

// u32Key is a struct tc_u32_key.
func u32Key(mask, val uint32, off int32) []byte {
	return cat(be32b(mask), be32b(val), le32b(uint32(off)), le32b(0))
}

func TestParseTcFopt(t *testing.T) {
	tests := []struct {
		description string
		kind        string
		opt         func(t *testing.T) []byte
		expectedErr error
		expected    func(t *testing.T, f TcFopt)
	}{
		{
			description: "positive: u32 2-key SEL from the corpus (L2)",
			kind:        TcFKindU32,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-007", TcFKindU32, 0x80100800).Options // 801::800
			},
			expected: func(t *testing.T, f TcFopt) {
				s := f.U32Sel
				if !f.HasU32Sel || f.U32SelShort || s.NKeys != 2 || len(s.Keys) != 2 || s.KeysShort {
					t.Fatalf("sel %+v", s)
				}
				want := []TcU32Key{{Mask: 0xff000000, Val: 0x0a000000, Off: 12}, {Mask: 0xffff, Val: 80, Off: 20}}
				if !slices.Equal(s.Keys, want) {
					t.Errorf("keys %+v, expected %+v", s.Keys, want)
				}
				if c, ok := f.Tb.U32(TcaU32Classid); !ok || c != 0x10001 {
					t.Errorf("classid %#x %t", c, ok)
				}
			},
		},
		{
			description: "positive: u32 link with offset ... eat (L1)",
			kind:        TcFKindU32,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-007", TcFKindU32, 0x80000800).Options // 800::800
			},
			expected: func(t *testing.T, f TcFopt) {
				s := f.U32Sel
				if s.Flags&(TcU32VarOffset|TcU32Eat) != TcU32VarOffset|TcU32Eat || s.Offmask != 0x0f00 || s.Offshift != 6 {
					t.Errorf("sel %+v", s)
				}
				if l, ok := f.Tb.U32(TcaU32Link); !ok || l != 0x00200000 {
					t.Errorf("link %#x %t", l, ok)
				}
			},
		},
		{
			description: "positive: matchall with gact",
			kind:        TcFKindMatchall,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-028", TcFKindMatchall, 1).Options
			},
			expected: func(t *testing.T, f TcFopt) {
				if fl, _ := f.Tb.U32(TcaMatchallFlags); fl&TcaClsFlagsSkipHw == 0 {
					t.Errorf("flags %#x, expected skip_hw", fl)
				}
				tab, err := ParseTcActTab(f.Tb.Bytes(TcaMatchallAct))
				if err != nil || !tab.Has(1) {
					t.Fatalf("act tab: %v, order 1 %t", err, tab.Has(1))
				}
				one, err := ParseTcActOne(tab.Bytes(1))
				if err != nil {
					t.Fatal(err)
				}
				if k, _ := one.Str(TcaActKind); k != TcActKindGact {
					t.Errorf("kind %q", k)
				}
			},
		},
		{
			description: "positive: basic with an ematch tree",
			kind:        TcFKindBasic,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-042", TcFKindBasic, 2).Options
			},
			expected: func(t *testing.T, f TcFopt) {
				tree, err := ParseTcEmatchTree(f.Tb.Bytes(TcaBasicEmatches))
				if err != nil || tree.Hdr.NMatches != 2 {
					t.Fatalf("tree %+v %v", tree.Hdr, err)
				}
				h1, _, err1 := DecodeTcfEmatch(tree.List.Bytes(1))
				h2, _, err2 := DecodeTcfEmatch(tree.List.Bytes(2))
				if err1 != nil || err2 != nil || h1.Kind != TcfEmKindU32 || h1.Flags != TcfEmRelAnd ||
					h2.Kind != TcfEmKindMeta || h2.Flags != TcfEmRelEnd {
					t.Errorf("matches %+v %+v (%v %v)", h1, h2, err1, err2)
				}
			},
		},
		{
			description: "positive: flow hash keys src,dst",
			kind:        TcFKindFlow,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-038", TcFKindFlow, 1).Options
			},
			expected: func(t *testing.T, f TcFopt) {
				k, _ := f.Tb.U32(TcaFlowKeys)
				m, _ := f.Tb.U32(TcaFlowMode)
				d, _ := f.Tb.U32(TcaFlowDivisor)
				if k != 3 || m != FlowModeHash || d != 1024 {
					t.Errorf("keys %#x mode %d divisor %d", k, m, d)
				}
			},
		},
		{
			description: "positive: route to and from realms (F3)",
			kind:        TcFKindRoute,
			opt: func(t *testing.T) []byte {
				return wsTcFilter(t, "tcfilter/ec-024", TcFKindRoute, 0x10002).Options
			},
			expected: func(t *testing.T, f TcFopt) {
				to, _ := f.Tb.U32(TcaRoute4To)
				from, _ := f.Tb.U32(TcaRoute4From)
				if to != 2 || from != 1 {
					t.Errorf("to %d from %d", to, from)
				}
			},
		},
		{
			description: "negative: a 15-byte u32 SEL is short (F17)",
			kind:        TcFKindU32,
			opt:         func(*testing.T) []byte { return rtattr(TcaU32Sel, make([]byte, 15)) },
			expected: func(t *testing.T, f TcFopt) {
				if !f.HasU32Sel || !f.U32SelShort {
					t.Errorf("has %t short %t", f.HasU32Sel, f.U32SelShort)
				}
			},
		},
		{
			description: "negative: a SEL claiming 4 keys with one is truncated (L5)",
			kind:        TcFKindU32,
			opt: func(*testing.T) []byte {
				return rtattr(TcaU32Sel, u32Sel(TcU32Terminal, 4, u32Key(0xff, 1, 8)))
			},
			expected: func(t *testing.T, f TcFopt) {
				s := f.U32Sel
				if f.U32SelShort || s.NKeys != 4 || len(s.Keys) != 1 || !s.KeysShort || s.Keys[0].Off != 8 {
					t.Errorf("sel %+v", s)
				}
			},
		},
		{
			description: "negative: tcindex (gone from the kernel) has no decoder",
			kind:        "tcindex",
			opt:         func(*testing.T) []byte { return nil },
			expectedErr: ErrTcKindUnknown,
		},
		{
			description: "negative: an attribute longer than the nest",
			kind:        TcFKindFw,
			opt:         func(*testing.T) []byte { return rtattrOverrunning(TcaFwClassid, 12) },
			expectedErr: ErrRTAttrSmall,
		},
		{
			description: "boundary: a PCNT with fewer counts than keys (F12)",
			kind:        TcFKindU32,
			opt: func(*testing.T) []byte {
				return cat(rtattr(TcaU32Sel, u32Sel(0, 2, u32Key(1, 1, 0), u32Key(2, 2, 4))),
					rtattr(TcaU32Pcnt, cat(le64b(7), le64b(5), le64b(3))))
			},
			expected: func(t *testing.T, f TcFopt) {
				p := f.U32Pcnt
				if f.U32PcntShort || p.Rcnt != 7 || p.Rhit != 5 || !slices.Equal(p.Kcnts, []uint64{3}) {
					t.Errorf("pcnt %+v", p)
				}
			},
		},
		{
			description: "boundary: a PCNT one byte short of rcnt and rhit",
			kind:        TcFKindU32,
			opt:         func(*testing.T) []byte { return rtattr(TcaU32Pcnt, make([]byte, 15)) },
			expected: func(t *testing.T, f TcFopt) {
				if !f.HasU32Pcnt || !f.U32PcntShort {
					t.Errorf("has %t short %t", f.HasU32Pcnt, f.U32PcntShort)
				}
			},
		},
		{
			description: "boundary: a SEL of exactly its header has no keys",
			kind:        TcFKindU32,
			opt:         func(*testing.T) []byte { return rtattr(TcaU32Sel, u32Sel(0, 0)) },
			expected: func(t *testing.T, f TcFopt) {
				if f.U32SelShort || len(f.U32Sel.Keys) != 0 || f.U32Sel.KeysShort {
					t.Errorf("sel %+v", f.U32Sel)
				}
			},
		},
		{
			description: "corner: a short MARK is flagged, not an error",
			kind:        TcFKindU32,
			opt:         func(*testing.T) []byte { return rtattr(TcaU32Mark, make([]byte, 8)) },
			expected: func(t *testing.T, f TcFopt) {
				if !f.HasU32Mark || !f.U32MarkShort {
					t.Errorf("has %t short %t", f.HasU32Mark, f.U32MarkShort)
				}
			},
		},
		{
			description: "corner: a matchall PCNT one byte short",
			kind:        TcFKindMatchall,
			opt:         func(*testing.T) []byte { return rtattr(TcaMatchallPcnt, make([]byte, 7)) },
			expected: func(t *testing.T, f TcFopt) {
				if !f.HasMaPcnt || !f.MaPcntShort {
					t.Errorf("has %t short %t", f.HasMaPcnt, f.MaPcntShort)
				}
			},
		},
		{
			description: "corner: a nest flagged NLA_F_NESTED still lands at its type",
			kind:        TcFKindFw,
			opt:         func(*testing.T) []byte { return rtattr(TcaFwAct|unix.NLA_F_NESTED, nil) },
			expected: func(t *testing.T, f TcFopt) {
				if !f.Tb.Has(TcaFwAct) {
					t.Error("TCA_FW_ACT absent")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			f, err := ParseTcFopt(tt.kind, tt.opt(t))
			if tt.expectedErr != nil {
				if !errors.Is(err, tt.expectedErr) {
					t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f.Kind != tt.kind {
				t.Errorf("kind %q", f.Kind)
			}
			tt.expected(t, f)
		})
	}
}

// ematchNest is an ematch tree of matches, each a header and payload.
func ematchNest(nmatches uint16, matches ...[]byte) []byte {
	list := make([]byte, 0, len(matches)*(RTAttrSizeCst+TcfEmatchHdrSizeCst+16))
	idx := uint16(1)
	for _, m := range matches {
		list = append(list, rtattr(idx, m)...)
		idx++
	}
	return cat(rtattr(TcaEmatchTreeHdr, cat(le16(nmatches), le16(0))), rtattr(TcaEmatchTreeList, list))
}

// ematchHdr is a struct tcf_ematch_hdr.
func ematchHdr(kind, flags uint16) []byte { return cat(le16(0), le16(kind), le16(flags), le16(0)) }

func TestParseTcEmatch(t *testing.T) {
	tests := []struct {
		description string
		run         func(t *testing.T) string
		expected    string
	}{
		{"positive: cmp's bitfields, low nibble first", func(t *testing.T) string {
			c, err := DecodeTcfEmCmp(cat(le32b(0x80000000), le32b(0xff), le16(9), []byte{0x12, 0x21}))
			return fmtErr(t, err, c.Val, c.Mask, c.Off, c.Align, c.Flags, c.Layer, c.Opnd)
		}, "2147483648 255 9 2 1 1 2"},
		{"positive: an nbyte needle", func(t *testing.T) string {
			n, err := DecodeTcfEmNbyte(cat(le16(4), []byte{3, 0x20}, []byte("abc")))
			return fmtErr(t, err, n.Off, n.Len, n.Layer, string(n.Needle))
		}, "4 3 2 abc"},
		{"positive: the corpus tree of basic pref 3, not nbyte or meta", func(t *testing.T) string {
			ti := wsTcFilter(t, "tcfilter/ec-042", TcFKindBasic, 3)
			f, err := ParseTcFopt(TcFKindBasic, ti.Options)
			if err != nil {
				t.Fatal(err)
			}
			tree, err := ParseTcEmatchTree(f.Tb.Bytes(TcaBasicEmatches))
			h1, d1, _ := DecodeTcfEmatch(tree.List.Bytes(1))
			nb, _ := DecodeTcfEmNbyte(d1)
			return fmtErr(t, err, tree.Hdr.NMatches, h1.Kind, h1.Flags, string(nb.Needle))
		}, "2 2 6 abc"},
		{"negative: a match shorter than its header", func(t *testing.T) string {
			_, _, err := DecodeTcfEmatch(make([]byte, 7))
			return errName(err)
		}, "short"},
		{"negative: a cmp one byte short", func(t *testing.T) string {
			_, err := DecodeTcfEmCmp(make([]byte, 11))
			return errName(err)
		}, "short"},
		{"negative: an nbyte needle longer than its payload", func(t *testing.T) string {
			_, err := DecodeTcfEmNbyte(cat(le16(0), le16(5), []byte("abc")))
			return errName(err)
		}, "short"},
		{"negative: a short tree header is flagged", func(t *testing.T) string {
			tree, err := ParseTcEmatchTree(rtattr(TcaEmatchTreeHdr, []byte{1}))
			return fmtErr(t, err, tree.HasHdr, tree.HdrShort)
		}, "true true"},
		{"boundary: a last match carrying AND reads one past the list (F2)", func(t *testing.T) string {
			tree, err := ParseTcEmatchTree(ematchNest(1, cat(ematchHdr(TcfEmKindCmp, TcfEmRelAnd), make([]byte, 12))))
			h, _, _ := DecodeTcfEmatch(tree.List.Bytes(1))
			return fmtErr(t, err, h.Flags&TcfEmRelMask, tree.List.Has(2))
		}, "1 false"},
		{"boundary: matches past nmatches are dropped", func(t *testing.T) string {
			tree, err := ParseTcEmatchTree(ematchNest(1, ematchHdr(TcfEmKindCmp, 0), ematchHdr(TcfEmKindU32, 0)))
			return fmtErr(t, err, tree.List.Len(), tree.List.Has(1))
		}, "2 true"},
		{"boundary: nmatches 0 has no list", func(t *testing.T) string {
			tree, err := ParseTcEmatchTree(ematchNest(0, ematchHdr(TcfEmKindCmp, 0)))
			return fmtErr(t, err, tree.List.Len())
		}, "0"},
		{"corner: a meta INT value of 8 bytes reads its low 4 as tc does", func(t *testing.T) string {
			hdr := cat(le16(TcfMetaTypeInt<<12|12), []byte{0, TcfEmOpndEq}, le16(TcfMetaTypeInt<<12), []byte{0, 0})
			m, err := ParseTcEmMeta(cat(rtattr(TcaEmMetaHdr, hdr), rtattr(TcaEmMetaRvalue, le64b(0x1_80000000))))
			v, _ := m.Tb.U32(TcaEmMetaRvalue)
			return fmtErr(t, err, m.Left.ID(), m.Left.Type(), m.Right.ID(), v)
		}, "12 1 0 2147483648"},
		{"corner: a meta header one byte short is flagged", func(t *testing.T) string {
			m, err := ParseTcEmMeta(rtattr(TcaEmMetaHdr, make([]byte, 7)))
			return fmtErr(t, err, m.HasHdr, m.HdrShort)
		}, "true true"},
		{"corner: canid ignores a trailing partial rule", func(t *testing.T) string {
			r := DecodeCanFilters(cat(le32b(0x123), le32b(CanEffFlag|CanSffMask), []byte{1, 2, 3}))
			return fmtErr(t, nil, len(r), r[0].ID, r[0].Mask)
		}, "1 291 2147485695"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.run(t); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestParseTcActTab(t *testing.T) {
	gactOf := func(t *testing.T, id, kind string, handle uint32, tbAct uint16) TcGactOpt {
		ti := wsTcFilter(t, id, kind, handle)
		f, err := ParseTcFopt(kind, ti.Options)
		if err != nil {
			t.Fatal(err)
		}
		tab, err := ParseTcActTab(f.Tb.Bytes(tbAct))
		if err != nil {
			t.Fatal(err)
		}
		one, err := ParseTcActOne(tab.Bytes(1))
		if err != nil {
			t.Fatal(err)
		}
		g, err := ParseTcGact(one.Bytes(TcaActOptions))
		if err != nil {
			t.Fatal(err)
		}
		return g
	}
	tests := []struct {
		description string
		run         func(t *testing.T) string
		expected    string
	}{
		{"positive: gact drop from the corpus", func(t *testing.T) string {
			g := gactOf(t, "tcfilter/ec-028", TcFKindMatchall, 1, TcaMatchallAct)
			return fmtErr(t, nil, g.HasParms, g.Parms.Action, g.Parms.Refcnt, g.Parms.Bindcnt, g.HasTm)
		}, "true 2 1 1 true"},
		{"positive: an action table by order", func(t *testing.T) string {
			tab, err := ParseTcActTab(cat(rtattr(2, nil), rtattr(1, nil)))
			return fmtErr(t, err, tab.Has(0), tab.Has(1), tab.Has(2))
		}, "false true true"},
		{"negative: a malformed action table", func(t *testing.T) string {
			_, err := ParseTcActTab(rtattrOverrunning(1, 40))
			return errName(err)
		}, "rtattr"},
		{"boundary: order 32 is kept, 33 dropped", func(t *testing.T) string {
			tab, err := ParseTcActTab(cat(rtattr(32, nil), rtattr(33, nil)))
			return fmtErr(t, err, tab.Has(32), tab.Len())
		}, "true 33"},
		{"boundary: a short PARMS is MIN-copied (L28)", func(t *testing.T) string {
			g, err := ParseTcGact(rtattr(TcaGactParms, cat(le32b(7), le32b(0), []byte{2})))
			return fmtErr(t, err, g.HasParms, g.Parms.Index, g.Parms.Action, g.Parms.Refcnt)
		}, "true 7 2 0"},
		{"corner: gact without PROB (F19)", func(t *testing.T) string {
			g := gactOf(t, "tcfilter/ec-028", TcFKindMatchall, 1, TcaMatchallAct)
			return fmtErr(t, nil, g.HasProb, g.Prob)
		}, "false {0 0 0}"},
		{"corner: a short nla_bitfield32 is MIN-copied", func(t *testing.T) string {
			bf := DecodeNlaBitfield32([]byte{1, 0, 0, 0, 3})
			return fmtErr(t, nil, bf.Value, bf.Selector)
		}, "1 3"},
		{"corner: a tcf_t of 16 bytes zero-fills expires and firstuse", func(t *testing.T) string {
			tm := DecodeTcfT(cat(le64b(500), le64b(200)))
			return fmtErr(t, nil, tm)
		}, "{500 200 0 0}"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.run(t); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestBuildTcFilterRequest(t *testing.T) {
	f, c := uint16(unix.RTM_GETTFILTER), uint16(unix.RTM_GETCHAIN)
	flowOpts := rtattr(TcaOptions, rtattr(TcaFlowMode, le32b(FlowModeMap)))
	tests := []struct {
		description string
		row         string // a 7_3_1 workbench row whose ip.pcap holds the request
		msgType     uint16
		get         bool
		q           TcFilterReq
		terse       bool
		kind        string
		opts        []byte
		expectedErr error
	}{
		{"positive: filter show dev d0 ingress", "tcfilter/ec-007", f, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress}, false, "", nil, nil},
		{"positive: filter show dev d1, no parent", "tcfilter/ec-024", f, false,
			TcFilterReq{Ifindex: wsD1}, false, "", nil, nil},
		{"positive: get u32 800::800", "tcfilter/ec-021", f, true,
			TcFilterReq{Ifindex: wsD0, Handle: 0x80000800, Parent: tcHClsactIngress, Prio: 1, Protocol: ethPIP},
			false, TcFKindU32, nil, nil},
		{"positive: get cgroup, an empty TCA_OPTIONS", "tcfilter/ng-021", f, true,
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactEgress, Prio: 1, Protocol: ethPAll},
			false, TcFKindCgroup, rtattr(TcaOptions, nil), nil},
		{"positive: get flow, a nest with TCA_FLOW_MODE", "tcfilter/ec-041", f, true,
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: 0x10000, Prio: 30, Protocol: ethPIP},
			false, TcFKindFlow, flowOpts, nil},
		{"positive: chain dump", "tcfilter/ec-054", c, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress}, false, "", nil, nil},
		{"boundary: pref 70000 keeps its low 16 bits (L40)", "tcfilter/ec-019", f, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress, Prio: 70000}, false, "", nil, nil},
		{"boundary: block 10", "tcfilter/ec-053", f, false,
			TcFilterReq{Ifindex: TcmIfindexMagicBlock, Parent: 10}, false, "", nil, nil},
		{"boundary: a handle past 2^32 is 1 (F5)", "tcfilter/ec-037", f, true,
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactEgress, Prio: 3, Protocol: ethPAll},
			false, TcFKindMatchall, nil, nil},
		{"corner: -br adds the TERSE bitfield (F21)", "tcfilter/ng-019", f, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress}, true, "", nil, nil},
		{"corner: chain 5", "tcfilter/ec-050", f, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress, Chain: 5, ChainSet: true}, false, "", nil, nil},
		{"corner: chain 0 is still sent", "tcfilter/ec-051", f, false,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress, ChainSet: true}, false, "", nil, nil},
		{"corner: chain get sends no kind (F9)", "tcfilter/ec-061", c, true,
			TcFilterReq{Ifindex: wsD0, Parent: tcHClsactIngress, Chain: 9, ChainSet: true}, false, TcFKindU32, nil, nil},
		{"corner: get with a chain puts TCA_CHAIN before TCA_KIND", "tcfilter/ec-062", f, true,
			TcFilterReq{Ifindex: wsD0, Handle: 1, Parent: tcHClsactIngress, Prio: 1, Protocol: ethPIP, Chain: 5, ChainSet: true},
			false, TcFKindBasic, nil, nil},
		{"negative: a qdisc dump type", "", uint16(unix.RTM_GETQDISC), false, TcFilterReq{}, false, "", nil, ErrTcFilterReqType},
		{"negative: a filter write type", "", uint16(unix.RTM_NEWTFILTER), true, TcFilterReq{}, false, "u32", nil, ErrTcFilterReqType},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var want []byte
			seq := uint32(1)
			if tt.row != "" {
				want = wsTcShowRequest(t, tt.row, tt.msgType)
				seq = nlSeqOf(want)
			}
			var got []byte
			var err error
			if tt.get {
				got, err = BuildTcFilterGet(tt.msgType, tt.q, tt.kind, tt.opts, seq)
			} else {
				got, err = BuildTcFilterDump(tt.msgType, tt.q, tt.terse, seq)
			}
			if !errors.Is(err, tt.expectedErr) {
				t.Fatalf("err = %v, expected %v", err, tt.expectedErr)
			}
			if err == nil && !bytes.Equal(got, want) {
				t.Fatalf("got  % x\nwant % x", got, want)
			}
		})
	}
}

// wsTcFilterSliceKinds are the filter kinds gotc prints; each must appear.
var wsTcFilterSliceKinds = []string{
	TcFKindU32, TcFKindFw, TcFKindRoute, TcFKindBasic, TcFKindCgroup, TcFKindFlow, TcFKindMatchall, TcFKindFlower,
	TcFKindBpf,
}

// TestWsTcFilterKinds decodes every RTM_NEWTFILTER in any workbench
// capture (the 7_3_0 tdc corpus and 7_3_1 tcfilter): an in-slice kind's
// options, its actions (gact's options) and its ematches must decode, and
// any other kind must be ErrTcKindUnknown.
func TestWsTcFilterKinds(t *testing.T) {
	seen := map[string]int{}
	var other, gacts, ematches int
	for _, dir := range wsRows(t) {
		pcaps, err := filepath.Glob(filepath.Join(dir, "*.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range pcaps {
			if base := filepath.Base(p); base != "ip.pcap" && !strings.HasPrefix(base, "netlink_route_") {
				continue
			}
			for i, m := range pcapNlMsgs(t, p) {
				if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWTFILTER || m.h.Flags&unix.NLM_F_CREATE != 0 {
					continue
				}
				ti, err := ParseTcMsg(m.body)
				if err != nil {
					t.Fatalf("%s message %d: %v", p, i, err)
				}
				if !slices.Contains(wsTcFilterSliceKinds, ti.Kind) {
					if _, err := ParseTcFopt(ti.Kind, ti.Options); !errors.Is(err, ErrTcKindUnknown) {
						t.Errorf("%s message %d: kind %q: err %v, expected ErrTcKindUnknown", p, i, ti.Kind, err)
					}
					other++
					continue
				}
				seen[ti.Kind]++
				g, e, err := wsTcFilterProblem(ti)
				if err != nil {
					t.Errorf("%s message %d (kind %s): %v", p, i, ti.Kind, err)
				}
				gacts += g
				ematches += e
			}
		}
	}
	for _, k := range wsTcFilterSliceKinds {
		if seen[k] == 0 {
			t.Errorf("kind %s is in no capture", k)
		}
	}
	if gacts == 0 || ematches == 0 {
		t.Errorf("%d gact actions, %d ematches decoded", gacts, ematches)
	}
	t.Logf("decoded %v, %d gact actions, %d ematches; %d messages of other kinds", seen, gacts, ematches, other)
}

// tcFilterActTb and tcFilterEmTb are each kind's ACT and EMATCHES types.
var (
	tcFilterActTb = map[string]uint16{
		TcFKindU32: TcaU32Act, TcFKindFw: TcaFwAct, TcFKindRoute: TcaRoute4Act, TcFKindBasic: TcaBasicAct,
		TcFKindCgroup: TcaCgroupAct, TcFKindFlow: TcaFlowAct, TcFKindMatchall: TcaMatchallAct,
		TcFKindFlower: TcaFlowerAct, TcFKindBpf: TcaBpfAct,
	}
	tcFilterEmTb = map[string]uint16{
		TcFKindBasic: TcaBasicEmatches, TcFKindCgroup: TcaCgroupEmatches, TcFKindFlow: TcaFlowEmatches,
	}
)

func wsTcFilterProblem(ti TcInfo) (gacts, ematches int, err error) {
	if !ti.HasOptions {
		return 0, 0, nil
	}
	f, err := ParseTcFopt(ti.Kind, ti.Options)
	if err != nil {
		return 0, 0, err
	}
	if f.U32SelShort || f.U32Sel.KeysShort || f.U32PcntShort || f.U32MarkShort || f.MaPcntShort || f.Bpf.Ops.Short ||
		f.Bpf.NameNoNUL {
		return 0, 0, errors.New("a short structure")
	}
	if b := f.Tb.Bytes(tcFilterActTb[ti.Kind]); b != nil {
		if gacts, err = walkTcActs(b); err != nil {
			return 0, 0, err
		}
	}
	if em, ok := tcFilterEmTb[ti.Kind]; ok && f.Tb.Has(em) {
		if ematches, err = walkTcEmatches(f.Tb.Bytes(em)); err != nil {
			return gacts, 0, err
		}
	}
	return gacts, ematches, nil
}

// walkTcActs decodes every action of a table, and gact's options.
func walkTcActs(b []byte) (int, error) {
	tab, err := ParseTcActTab(b)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := uint16(0); i <= TcaActMaxPrioCst; i++ {
		if !tab.Has(i) {
			continue
		}
		one, err := ParseTcActOne(tab.Bytes(i))
		if err != nil {
			return n, err
		}
		// A terse dump's action has no TCA_ACT_OPTIONS (tcf_action_dump_terse).
		if k, _ := one.Str(TcaActKind); k == TcActKindGact && one.Has(TcaActOptions) {
			g, err := ParseTcGact(one.Bytes(TcaActOptions))
			if err != nil {
				return n, err
			}
			if !g.HasParms {
				return n, errors.New("gact without PARMS")
			}
			n++
		}
	}
	return n, nil
}

// walkTcEmatches decodes every match of a tree with its kind's decoder.
func walkTcEmatches(b []byte) (int, error) {
	tree, err := ParseTcEmatchTree(b)
	if err != nil {
		return 0, err
	}
	if !tree.HasHdr || tree.HdrShort {
		return 0, errors.New("no tree header")
	}
	n := 0
	// i != 0 stops the walk where a u16 wraps, at nmatches 65535.
	for i := uint16(1); i != 0 && i <= tree.Hdr.NMatches; i++ {
		h, data, err := DecodeTcfEmatch(tree.List.Bytes(i))
		if err != nil {
			return n, err
		}
		switch h.Kind {
		case TcfEmKindCmp:
			_, err = DecodeTcfEmCmp(data)
		case TcfEmKindU32:
			_, err = DecodeTcU32Key(data)
		case TcfEmKindNbyte:
			_, err = DecodeTcfEmNbyte(data)
		case TcfEmKindCanid:
			DecodeCanFilters(data)
		case TcfEmKindMeta:
			var m TcEmMeta
			if m, err = ParseTcEmMeta(data); err == nil && (!m.HasHdr || m.HdrShort) {
				err = errors.New("meta without a header")
			}
		}
		if err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// FuzzParseTcFopt runs every filter kind's decoder, the kind being input
// too, with its actions and ematches.
func FuzzParseTcFopt(f *testing.F) {
	for _, b := range wsTcSeeds(f, map[uint16]bool{unix.RTM_NEWTFILTER: true}) {
		if ti, err := ParseTcMsg(b); err == nil && ti.HasOptions {
			f.Add(ti.Kind, ti.Options)
		}
	}
	for _, k := range wsTcFilterSliceKinds {
		f.Add(k, []byte{})
	}
	f.Fuzz(func(t *testing.T, kind string, in []byte) {
		fo, err := ParseTcFopt(kind, in)
		if err != nil {
			return
		}
		if fo.Kind != kind {
			t.Fatalf("kind %q, expected %q", fo.Kind, kind)
		}
		if len(fo.U32Sel.Keys) > int(fo.U32Sel.NKeys) || len(fo.U32Sel.Keys)*TcU32KeySizeCst > len(in) {
			t.Fatalf("%d keys from %d bytes", len(fo.U32Sel.Keys), len(in))
		}
		if b := fo.Tb.Bytes(tcFilterActTb[kind]); b != nil {
			if n, err := walkTcActs(b); err == nil && n > TcaActMaxPrioCst+1 {
				t.Fatalf("%d gact actions in a table of %d", n, TcaActMaxPrioCst+1)
			}
		}
		if em, ok := tcFilterEmTb[kind]; ok && fo.Tb.Has(em) {
			if n, err := walkTcEmatches(fo.Tb.Bytes(em)); err == nil && n > len(in) {
				t.Fatalf("%d ematches from %d bytes", n, len(in))
			}
		}
	})
}

// FuzzParseTcEmatchTree walks a tree with every ematch decoder.
func FuzzParseTcEmatchTree(f *testing.F) {
	f.Add(ematchNest(1, cat(ematchHdr(TcfEmKindCmp, TcfEmRelAnd), make([]byte, 12))))
	f.Add(ematchNest(2, ematchHdr(TcfEmKindMeta, 0), cat(ematchHdr(TcfEmKindNbyte, 0), le16(0), le16(2), []byte("ab"))))
	f.Fuzz(func(t *testing.T, in []byte) {
		if n, err := walkTcEmatches(in); err == nil && n > len(in) {
			t.Fatalf("%d ematches from %d bytes", n, len(in))
		}
		if n, err := DecodeTcfEmNbyte(in); err == nil && len(n.Needle) != int(n.Len) {
			t.Fatalf("needle %d, len %d", len(n.Needle), n.Len)
		}
	})
}

// FuzzParseTcActTab walks an action table, decoding gact's options.
func FuzzParseTcActTab(f *testing.F) {
	f.Add(rtattr(1, cat(rtattr(TcaActKind, []byte("gact\x00")), rtattr(TcaActOptions, rtattr(TcaGactParms, make([]byte, 20))))))
	f.Fuzz(func(t *testing.T, in []byte) {
		if n, err := walkTcActs(in); err == nil && n > TcaActMaxPrioCst+1 {
			t.Fatalf("%d gact actions", n)
		}
		if g, err := ParseTcGact(in); err == nil && g.HasProb && !g.Tb.Has(TcaGactProb) {
			t.Fatal("prob without TCA_GACT_PROB")
		}
	})
}

func BenchmarkParseTcFopt(b *testing.B) {
	u32 := rtattr(TcaU32Sel, u32Sel(TcU32Terminal, 2, u32Key(0xff000000, 0x0a000000, 12), u32Key(0xffff, 80, 20)))
	cases := []struct {
		kind string
		opt  []byte
	}{
		{TcFKindU32, cat(u32, rtattr(TcaU32Hash, le32b(0x80000000)), rtattr(TcaU32Classid, le32b(0x10001)))},
		{TcFKindMatchall, cat(rtattr(TcaMatchallFlags, le32b(1)), rtattr(TcaMatchallPcnt, le64b(0)))},
		{TcFKindFlow, cat(rtattr(TcaFlowKeys, le32b(3)), rtattr(TcaFlowMode, le32b(1)), rtattr(TcaFlowDivisor, le32b(1024)))},
	}
	for _, c := range cases {
		b.Run(c.kind, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := ParseTcFopt(c.kind, c.opt); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkParseTcEmatchTree(b *testing.B) {
	in := ematchNest(2, cat(ematchHdr(TcfEmKindCmp, TcfEmRelAnd), make([]byte, 12)), cat(ematchHdr(TcfEmKindU32, 0), make([]byte, 16)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := walkTcEmatches(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseTcGact(b *testing.B) {
	in := cat(rtattr(TcaGactParms, make([]byte, TcGactSizeCst)), rtattr(TcaGactTm, make([]byte, TcfTSizeCst)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseTcGact(in); err != nil {
			b.Fatal(err)
		}
	}
}
