package xtcpnl

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// actKindDecode is each kind's decoder, reduced to what every kind shares:
// tc_gen (police's fields that match it) and the common part.
var actKindDecode = map[string]func([]byte) (TcGen, TcActCommon, error){
	TcActKindPolice: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcPolice(b)
		return TcGen{Index: o.P.Index, Capab: o.P.Capab, Action: o.P.Action, Refcnt: o.P.Refcnt, Bindcnt: o.P.Bindcnt},
			o.TcActCommon, err
	},
	TcActKindMirred: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcMirred(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindSkbedit: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcSkbedit(b)
		return o.P, o.TcActCommon, err
	},
	TcActKindVlan: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcVlan(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindCsum: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcCsum(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindNat: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcNat(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindSimple: func(b []byte) (TcGen, TcActCommon, error) { o, err := ParseTcSimple(b); return o.P, o.TcActCommon, err },
	TcActKindConnmark: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcConnmark(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindCtinfo: func(b []byte) (TcGen, TcActCommon, error) { o, err := ParseTcCtinfo(b); return o.P, o.TcActCommon, err },
	TcActKindSample: func(b []byte) (TcGen, TcActCommon, error) { o, err := ParseTcSample(b); return o.P, o.TcActCommon, err },
	TcActKindSkbmod: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcSkbmod(b)
		return o.P.TcGen, o.TcActCommon, err
	},
	TcActKindMpls: func(b []byte) (TcGen, TcActCommon, error) {
		o, err := ParseTcMpls(b)
		return o.P.TcGen, o.TcActCommon, err
	},
}

// wsTcActOpts is every action of kind in a 7_3_1 workbench row's
// RTM_NEWACTION replies, by tc_gen index: its TCA_ACT_OPTIONS.
func wsTcActOpts(t *testing.T, id, kind string) map[uint32][]byte {
	t.Helper()
	out := map[uint32][]byte{}
	for _, m := range pcapNlMsgs(t, filepath.Join("testdata", "7_3_1", "ws", id, "ip.pcap")) {
		if m.proto != unix.NETLINK_ROUTE || !isActReply(m.h.Type, m.h.Flags) {
			continue
		}
		ai, err := ParseTcAction(m.body)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range ai.Actions {
			if a.Kind != kind {
				continue
			}
			g, _, err := actKindDecode[kind](a.Options)
			if err != nil {
				t.Fatal(err)
			}
			out[g.Index] = a.Options
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: no %s action", id, kind)
	}
	return out
}

// isActReply is an action reply: the kernel answers a dump or a get with
// the request's own type, RTM_GETACTION (K5), and events with NEWACTION.
func isActReply(typ, flags uint16) bool {
	return (typ == unix.RTM_GETACTION || typ == unix.RTM_NEWACTION) && flags&unix.NLM_F_REQUEST == 0
}

// gen is tc_gen as "index action ref bind".
func gen(g TcGen) string { return fmt.Sprintf("%d %d %d %d", g.Index, g.Action, g.Refcnt, g.Bindcnt) }

// parms builds a parms attribute of n bytes: tc_gen with index 7, action
// pipe (3), then the rest of b.
func parms(atype uint16, n int, rest ...byte) []byte {
	b := cat(le32b(7), le32b(0), le32b(3), le32b(1), le32b(0), rest)
	if len(b) > n {
		b = b[:n]
	}
	for len(b) < n {
		b = append(b, 0)
	}
	return rtattr(atype, b)
}

// TestParseTcActKinds is the table for each kind's decoder: the corpus's
// actions (positive), short and odd attributes (negative and boundary), and
// the two request builders against tc's bytes (corner).
func TestParseTcActKinds(t *testing.T) {
	pol := wsTcActOpts(t, "tcaction/ec-005", TcActKindPolice)
	mir := wsTcActOpts(t, "tcaction/ec-019", TcActKindMirred)
	skb := wsTcActOpts(t, "tcaction/ec-025", TcActKindSkbedit)
	vlan := wsTcActOpts(t, "tcaction/ec-030", TcActKindVlan)
	mpls := wsTcActOpts(t, "tcaction/ec-034", TcActKindMpls)
	skbmod := wsTcActOpts(t, "tcaction/ec-037", TcActKindSkbmod)
	csum := wsTcActOpts(t, "tcaction/ec-040", TcActKindCsum)
	nat := wsTcActOpts(t, "tcaction/ec-042", TcActKindNat)
	connmark := wsTcActOpts(t, "tcaction/ec-044", TcActKindConnmark)
	ctinfo := wsTcActOpts(t, "tcaction/ec-045", TcActKindCtinfo)
	simple := wsTcActOpts(t, "tcaction/ec-048", TcActKindSimple)
	sample := wsTcActOpts(t, "tcaction/ec-049", TcActKindSample)

	tests := []struct {
		description string
		input       func() (string, error)
		expected    string
	}{
		// Positive: the corpus's actions.
		{"positive: police rate, burst and mtu", func() (string, error) {
			o, err := ParseTcPolice(pol[1])
			return fmt.Sprintf("%s rate %d burst %d mtu %d short %t tm %t", gen(TcGen{Index: o.P.Index, Action: o.P.Action,
				Refcnt: o.P.Refcnt, Bindcnt: o.P.Bindcnt}), o.P.Rate.Rate, o.P.Burst, o.P.Mtu,
				o.ParmsShort(TcPoliceSizeCst), o.HasTm), err
		}, "1 1 2 1 rate 125000 burst 1280000 mtu 2048 short false tm true"},
		{"positive: police peakrate", func() (string, error) {
			o, err := ParseTcPolice(pol[2])
			return fmt.Sprintf("peak %d", o.P.PeakRate.Rate), err
		}, "peak 250000"},
		{"positive: police RESULT, and pps with its burst in time units", func() (string, error) {
			o3, err := ParseTcPolice(pol[3])
			if err != nil {
				return "", err
			}
			o4, err := ParseTcPolice(pol[4])
			return fmt.Sprintf("%s | %s %s", hexOf(o3.Tb, TcaPoliceResult),
				hexOf(o4.Tb, TcaPolicePktrate64, TcaPolicePktburst64), hexOf(o4.Tb, TcaPoliceRate64)), err
		}, "03000000 | e803000000000000 5a62020000000000 -"},
		{"positive: mirred egress redirect to d1, and to a block", func() (string, error) {
			o, err := ParseTcMirred(mir[1])
			if err != nil {
				return "", err
			}
			o4, err := ParseTcMirred(mir[4])
			return fmt.Sprintf("%s eaction %d ifindex>0 %t | block %s", gen(o.P.TcGen), o.P.Eaction, o.P.Ifindex > 0,
				hexOf(o4.Tb, TcaMirredBlockid)), err
		}, "1 4 1 0 eaction 1 ifindex>0 true | block 07000000"},
		{"positive: skbedit queue_mapping, mark, mask, ptype and inheritdsfield (0x20)", func() (string, error) {
			o, err := ParseTcSkbedit(skb[2])
			return fmt.Sprintf("%s %s", gen(o.P), hexOf(o.Tb, TcaSkbeditQueueMapping, TcaSkbeditMark, TcaSkbeditMask,
				TcaSkbeditPtype, TcaSkbeditFlags)), err
		}, "2 3 1 0 0300 10000000 ff000000 0000 2000000000000000"},
		{"positive: vlan push", func() (string, error) {
			o, err := ParseTcVlan(vlan[2])
			return fmt.Sprintf("v_action %d %s", o.P.V, hexOf(o.Tb, TcaVlanPushVlanID, TcaVlanPushVlanProtocol,
				TcaVlanPushVlanPriority)), err
		}, "v_action 2 6400 88a8 03"},
		{"positive: mpls push (2)", func() (string, error) {
			o, err := ParseTcMpls(mpls[2])
			return fmt.Sprintf("m_action %d %s", o.P.V, hexOf(o.Tb, TcaMplsProto, TcaMplsLabel, TcaMplsTC, TcaMplsTTL,
				TcaMplsBos)), err
		}, "m_action 2 8847 64000000 03 40 -"},
		{"positive: skbmod etype", func() (string, error) {
			o, err := ParseTcSkbmod(skbmod[2])
			return fmt.Sprintf("flags %#x %s", o.P.Flags, hexOf(o.Tb, TcaSkbmodEtype)), err
		}, "flags 0x4 efbe"},
		{"positive: csum iph tcp", func() (string, error) {
			o, err := ParseTcCsum(csum[1])
			return fmt.Sprintf("flags %#x", o.P.V), err
		}, "flags 0x9"},
		{"positive: nat ingress 10.0.0.0/8", func() (string, error) {
			o, err := ParseTcNat(nat[1])
			return fmt.Sprintf("%v %v %v %d", o.P.OldAddr, o.P.NewAddr, o.P.Mask, o.P.Flags), err
		}, "[10 0 0 0] [192 168 0 1] [255 0 0 0] 0"},
		{"positive: connmark zone", func() (string, error) {
			o, err := ParseTcConnmark(connmark[1])
			return fmt.Sprintf("zone %d", o.P.Zone), err
		}, "zone 5"},
		{"positive: ctinfo dscp masks", func() (string, error) {
			o, err := ParseTcCtinfo(ctinfo[1])
			return hexOf(o.Tb, TcaCtinfoZone, TcaCtinfoParmsDscpMask, TcaCtinfoParmsDscpStatemask), err
		}, "0000 000000fc 00000001"},
		{"positive: simple sdata", func() (string, error) {
			o, err := ParseTcSimple(simple[1])
			return fmt.Sprintf("%q", o.Tb.Bytes(TcaDefData)), err
		}, `"hello\x00"`},
		{"positive: sample rate, group and trunc", func() (string, error) {
			o, err := ParseTcSample(sample[1])
			return hexOf(o.Tb, TcaSampleRate, TcaSamplePsampleGroup, TcaSampleTruncSize), err
		}, "64000000 05000000 40000000"},
		// Negative: short attributes, MIN-copied and flagged (AC17, AC21).
		{"negative: a 4-byte PARMS for each kind", func() (string, error) {
			var parts []string
			for _, k := range []struct {
				kind  string
				parms uint16
				size  int
			}{
				{TcActKindPolice, TcaPoliceTbf, TcPoliceSizeCst}, {TcActKindMirred, TcaMirredParms, TcMirredSizeCst},
				{TcActKindSkbedit, TcaSkbeditParms, TcGactSizeCst}, {TcActKindVlan, TcaVlanParms, TcVlanSizeCst},
				{TcActKindCsum, TcaCsumParms, TcCsumSizeCst}, {TcActKindNat, TcaNatParms, TcNatSizeCst},
				{TcActKindSimple, TcaDefParms, TcGactSizeCst}, {TcActKindConnmark, TcaConnmarkParms, TcConnmarkSizeCst},
				{TcActKindCtinfo, TcaCtinfoAct, TcGactSizeCst}, {TcActKindSample, TcaSampleParms, TcGactSizeCst},
				{TcActKindSkbmod, TcaSkbmodParms, TcSkbmodSizeCst}, {TcActKindMpls, TcaMplsParms, TcMplsSizeCst},
			} {
				g, c, err := actKindDecode[k.kind](parms(k.parms, 4))
				if err != nil {
					return "", err
				}
				parts = append(parts, fmt.Sprintf("%s:%d:%t:%d", k.kind, c.ParmsLen, c.ParmsShort(k.size), g.Index))
			}
			return strings.Join(parts, " "), nil
		}, "police:4:true:7 mirred:4:true:7 skbedit:4:true:7 vlan:4:true:7 csum:4:true:7 nat:4:true:7 simple:4:true:7 " +
			"connmark:4:true:7 ctinfo:4:true:7 sample:4:true:7 skbmod:4:true:7 mpls:4:true:7"},
		{"negative: a 2-byte mirred BLOCKID and a 4-byte skbedit FLAGS", func() (string, error) {
			m, err := ParseTcMirred(cat(parms(TcaMirredParms, TcMirredSizeCst), rtattr(TcaMirredBlockid, []byte{7, 0})))
			if err != nil {
				return "", err
			}
			s, err := ParseTcSkbedit(cat(parms(TcaSkbeditParms, TcGactSizeCst), rtattr(TcaSkbeditFlags, le32b(1))))
			return fmt.Sprintf("%d %d", len(m.Tb.Bytes(TcaMirredBlockid)), len(s.Tb.Bytes(TcaSkbeditFlags))), err
		}, "2 4"},
		{"negative: an attribute longer than the stream", func() (string, error) {
			_, err := ParseTcVlan([]byte{9, 0, 1, 0, 0, 0, 0, 0})
			return "", err
		}, "error"},
		// Boundary.
		{"boundary: vlan v_action 5 and mpls m_action 9 are kept raw (AC20)", func() (string, error) {
			v, err := ParseTcVlan(parms(TcaVlanParms, TcVlanSizeCst, 5, 0, 0, 0))
			if err != nil {
				return "", err
			}
			m, err := ParseTcMpls(parms(TcaMplsParms, TcMplsSizeCst, 9, 0, 0, 0))
			return fmt.Sprintf("%d %d", v.P.V, m.P.V), err
		}, "5 9"},
		{"boundary: simple sdata with no NUL is kept raw (AC21)", func() (string, error) {
			o, err := ParseTcSimple(rtattr(TcaDefData, []byte("abc")))
			return fmt.Sprintf("%q", o.Tb.Bytes(TcaDefData)), err
		}, `"abc"`},
		{"boundary: a type above the kind's max is dropped", func() (string, error) {
			o, err := ParseTcMirred(rtattr(TcaMirredMaxKnown+1, le32b(1)))
			return fmt.Sprintf("%t %d", o.HasParms, o.Tb.Len()), err
		}, "false 5"},
		{"boundary: a long PARMS is cut to the struct", func() (string, error) {
			o, err := ParseTcConnmark(parms(TcaConnmarkParms, 40, 5, 0, 0xff, 0xff))
			return fmt.Sprintf("%d %t %d", o.ParmsLen, o.ParmsShort(TcConnmarkSizeCst), o.P.Zone), err
		}, "40 false 5"},
		{"boundary: skbmod's flags sit at offset 24, past tc_gen's padding", func() (string, error) {
			o, err := ParseTcSkbmod(parms(TcaSkbmodParms, TcSkbmodSizeCst, 0xee, 0xee, 0xee, 0xee, 8))
			return fmt.Sprintf("%#x", o.P.Flags), err
		}, "0x8"},
		// Corner: the requests, against tc's.
		{"corner: get with an index", func() (string, error) {
			return reqDiff(t, "tcaction/ec-013", func(seq uint32) ([]byte, error) {
				return BuildTcActionGet(TcActKindPolice, 1, seq)
			})
		}, "same"},
		{"corner: get with no kind sends an empty tab (AC9)", func() (string, error) {
			return reqDiff(t, "tcaction/ec-003", func(seq uint32) ([]byte, error) { return BuildTcActionGet("", 0, seq) })
		}, "same"},
		{"corner: get with index 0 sends no INDEX", func() (string, error) {
			b, err := BuildTcActionGet(TcActKindGact, 0, 1)
			return fmt.Sprintf("%x", b[NlMsgHdrSizeCst:]), err
		}, "000000001400010010000100090001006761637400000000"},
		{"corner: a kind past 15 bytes is cut (AC10)", func() (string, error) {
			b, err := BuildTcActionGet("aaaaaaaaaaaaaaaaaa", 1, 1)
			return fmt.Sprintf("%t %t", bytes.Contains(b, []byte("aaaaaaaaaaaaaaa\x00")),
				bytes.Contains(b, []byte("aaaaaaaaaaaaaaaa"))), err
		}, "true false"},
		{"corner: list", func() (string, error) {
			return reqDiff(t, "tcaction/ec-005", func(seq uint32) ([]byte, error) {
				return BuildTcActionDump(TcActKindPolice, false, 0, false, seq)
			})
		}, "same"},
		{"corner: a terse list (-br)", func() (string, error) {
			return reqDiff(t, "tcaction/ec-054", func(seq uint32) ([]byte, error) {
				return BuildTcActionDump(TcActKindGact, true, 0, false, seq)
			})
		}, "same"},
		{"corner: a list since", func() (string, error) {
			return reqDiff(t, "tcaction/ec-055", func(seq uint32) ([]byte, error) {
				return BuildTcActionDump(TcActKindGact, false, 100000000, true, seq)
			})
		}, "same"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := tt.input()
			if err != nil {
				got = "error"
				if tt.expected != "error" {
					t.Fatal(err)
				}
			}
			if got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// reqDiff is "same" when build makes the row's captured RTM_GETACTION
// request byte for byte, else both in hex.
func reqDiff(t *testing.T, id string, build func(uint32) ([]byte, error)) (string, error) {
	t.Helper()
	want := wsTcShowRequest(t, id, unix.RTM_GETACTION)
	got, err := build(nlSeqOf(want))
	if err != nil {
		return "", err
	}
	if !bytes.Equal(got, want) {
		return fmt.Sprintf("got % x want % x", got, want), nil
	}
	return "same", nil
}

// TestWsTcAction decodes every action of the 12 kinds in the tcaction
// captures and in the tdc corpus's action pcaps: each must decode, with a
// full parms struct, and each kind must appear.
func TestWsTcAction(t *testing.T) {
	var pcaps []string
	for _, g := range []string{
		filepath.Join("testdata", "7_3_1", "ws", "tcaction", "*", "ip.pcap"),
		filepath.Join("testdata", "7_3_0", "ws", "tdc", "actions", "*", "*", "*.pcap"),
	} {
		m, err := filepath.Glob(g)
		if err != nil {
			t.Fatal(err)
		}
		if len(m) == 0 {
			t.Fatalf("no captures for %s", g)
		}
		pcaps = append(pcaps, m...)
	}
	sizes := map[string]int{
		TcActKindPolice: TcPoliceSizeCst, TcActKindMirred: TcMirredSizeCst, TcActKindSkbedit: TcGactSizeCst,
		TcActKindVlan: TcVlanSizeCst, TcActKindCsum: TcCsumSizeCst, TcActKindNat: TcNatSizeCst,
		TcActKindSimple: TcGactSizeCst, TcActKindConnmark: TcConnmarkSizeCst, TcActKindCtinfo: TcGactSizeCst,
		TcActKindSample: TcGactSizeCst, TcActKindSkbmod: TcSkbmodSizeCst, TcActKindMpls: TcMplsSizeCst,
	}
	seen := map[string]int{}
	for _, p := range pcaps {
		for i, m := range pcapNlMsgs(t, p) {
			if m.proto != unix.NETLINK_ROUTE || !isActReply(m.h.Type, m.h.Flags) {
				continue
			}
			ai, err := ParseTcAction(m.body)
			if err != nil {
				t.Fatalf("%s message %d: %v", p, i, err)
			}
			for _, a := range ai.Actions {
				dec, ok := actKindDecode[a.Kind]
				if !ok || a.Options == nil {
					continue
				}
				_, c, err := dec(a.Options)
				if err != nil {
					t.Errorf("%s message %d %s: %v", p, i, a.Kind, err)
					continue
				}
				// ctinfo and police send their parms as ACT and TBF.
				if !c.HasParms || c.ParmsShort(sizes[a.Kind]) {
					t.Errorf("%s message %d %s: parms %t of %d bytes", p, i, a.Kind, c.HasParms, c.ParmsLen)
				}
				seen[a.Kind]++
			}
		}
	}
	for k := range actKindDecode {
		if seen[k] == 0 {
			t.Errorf("no %s in the captures", k)
		}
	}
	t.Logf("decoded %v", seen)
}

// FuzzParseTcActKinds decodes an options payload with every kind's
// decoder: none may fail on anything ParseAttrTableFlags accepts, and the
// parms length is the attribute's.
func FuzzParseTcActKinds(f *testing.F) {
	for _, b := range wsTcSeeds(f, map[uint16]bool{unix.RTM_NEWACTION: true, unix.RTM_GETACTION: true}) {
		if ai, err := ParseTcAction(b); err == nil {
			for _, a := range ai.Actions {
				if a.Options != nil {
					f.Add(a.Options)
				}
			}
		}
	}
	f.Add(parms(TcaVlanParms, 4))
	f.Fuzz(func(t *testing.T, in []byte) {
		if _, err := ParseAttrTableFlags(in, 1, tcNestedFlags); err != nil {
			return
		}
		for k, dec := range actKindDecode {
			_, c, err := dec(in)
			if err != nil {
				t.Fatalf("%s: %v", k, err)
			}
			if c.ParmsLen > len(in) {
				t.Fatalf("%s: parms of %d from %d bytes", k, c.ParmsLen, len(in))
			}
		}
	})
}

func BenchmarkParseTcActKinds(b *testing.B) {
	tm := rtattr(TcaGactTm, make([]byte, TcfTSizeCst))
	cases := []struct {
		name string
		fn   func() error
	}{
		{"police", func() error {
			_, err := ParseTcPolice(cat(parms(TcaPoliceTbf, TcPoliceSizeCst), rtattr(TcaPoliceTm, make([]byte, TcfTSizeCst))))
			return err
		}},
		{"mirred", func() error { _, err := ParseTcMirred(cat(parms(TcaMirredParms, TcMirredSizeCst), tm)); return err }},
		{"skbedit", func() error { _, err := ParseTcSkbedit(cat(parms(TcaSkbeditParms, TcGactSizeCst), tm)); return err }},
		{"vlan", func() error { _, err := ParseTcVlan(cat(parms(TcaVlanParms, TcVlanSizeCst), tm)); return err }},
		{"csum", func() error { _, err := ParseTcCsum(parms(TcaCsumParms, TcCsumSizeCst)); return err }},
		{"nat", func() error { _, err := ParseTcNat(parms(TcaNatParms, TcNatSizeCst)); return err }},
		{"simple", func() error { _, err := ParseTcSimple(cat(parms(TcaDefParms, TcGactSizeCst), tm)); return err }},
		{"connmark", func() error { _, err := ParseTcConnmark(parms(TcaConnmarkParms, TcConnmarkSizeCst)); return err }},
		{"ctinfo", func() error { _, err := ParseTcCtinfo(parms(TcaCtinfoAct, TcGactSizeCst)); return err }},
		{"sample", func() error { _, err := ParseTcSample(cat(parms(TcaSampleParms, TcGactSizeCst), tm)); return err }},
		{"skbmod", func() error { _, err := ParseTcSkbmod(cat(parms(TcaSkbmodParms, TcSkbmodSizeCst), tm)); return err }},
		{"mpls", func() error { _, err := ParseTcMpls(cat(parms(TcaMplsParms, TcMplsSizeCst), tm)); return err }},
		{"get", func() error { _, err := BuildTcActionGet(TcActKindGact, 1, 1); return err }},
		{"dump", func() error { _, err := BuildTcActionDump(TcActKindGact, true, 1, true, 1); return err }},
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
