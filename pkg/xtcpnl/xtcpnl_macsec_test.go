package xtcpnl

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// macsecRender shows a decoded macsec body as its command, then each
// field present, or the error. A statistics nest is its counters in
// order, "-" for one absent.
func macsecRender(m Macsec, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out := []string{"cmd=" + strconv.Itoa(int(m.Command)), "ver=" + strconv.Itoa(int(m.Version))}
	if m.Ifindex != nil {
		out = append(out, "ifindex="+u32s(*m.Ifindex))
	}
	if m.RxSC != nil {
		out = append(out, "rxsc{"+macsecRenderRxSC(m.RxSC)+"}")
	}
	if m.SA != nil {
		out = append(out, "sa{"+macsecRenderSA(m.SA)+"}")
	}
	if s := m.SecY; s != nil {
		var f []string
		if s.SCI != nil {
			f = append(f, fmt.Sprintf("sci=%016x", *s.SCI))
		}
		if s.Window != nil {
			f = append(f, "window="+u32s(*s.Window))
		}
		if s.CipherSuite != nil {
			f = append(f, "cipher=0x"+strconv.FormatUint(*s.CipherSuite, 16))
		}
		for _, v := range []struct {
			name string
			v    *uint8
		}{
			{"enc_sa", s.EncodingSA}, {"icv", s.ICVLen}, {"protect", s.Protect}, {"replay", s.Replay}, {"oper", s.Oper},
			{"validate", s.Validate}, {"encrypt", s.Encrypt}, {"inc_sci", s.IncSCI}, {"es", s.ES}, {"scb", s.SCB},
		} {
			if v.v != nil {
				f = append(f, v.name+"="+strconv.Itoa(int(*v.v)))
			}
		}
		out = append(out, "secy{"+strings.Join(f, " ")+"}")
	}
	if m.TxSAs != nil {
		items := make([]string, 0, len(m.TxSAs))
		for i := range m.TxSAs {
			items = append(items, macsecRenderSA(&m.TxSAs[i]))
		}
		out = append(out, "txsa["+strings.Join(items, ";")+"]")
	}
	if m.RxSCs != nil {
		items := make([]string, 0, len(m.RxSCs))
		for i := range m.RxSCs {
			items = append(items, macsecRenderRxSC(&m.RxSCs[i]))
		}
		out = append(out, "rxsc["+strings.Join(items, ";")+"]")
	}
	if m.TxscStats != nil {
		out = append(out, "txsc_stats="+macsecRenderStats(m.TxscStats))
	}
	if m.SecYStats != nil {
		out = append(out, "secy_stats="+macsecRenderStats(m.SecYStats))
	}
	if m.HasOffload {
		t := ""
		if m.OffloadType != nil {
			t = "type=" + strconv.Itoa(int(*m.OffloadType))
		}
		out = append(out, "offload{"+t+"}")
	}
	return strings.Join(out, " ")
}

// macsecRenderStats shows counters in order, "-" for one absent.
func macsecRenderStats[T uint32 | uint64](s []*T) string {
	var out []string
	for _, v := range s {
		if v == nil {
			out = append(out, "-")
			continue
		}
		out = append(out, strconv.FormatUint(uint64(*v), 10))
	}
	return strings.Join(out, ",")
}

// macsecRenderRxSC shows a receive channel's fields present.
func macsecRenderRxSC(c *MacsecRxSC) string {
	var f []string
	if c.SCI != nil {
		f = append(f, fmt.Sprintf("sci=%016x", *c.SCI))
	}
	if c.Active != nil {
		f = append(f, "active="+strconv.Itoa(int(*c.Active)))
	}
	if c.SAs != nil {
		items := make([]string, 0, len(c.SAs))
		for i := range c.SAs {
			items = append(items, macsecRenderSA(&c.SAs[i]))
		}
		f = append(f, "sas["+strings.Join(items, ";")+"]")
	}
	if c.Stats != nil {
		f = append(f, "stats="+macsecRenderStats(c.Stats))
	}
	return strings.Join(f, " ")
}

// macsecRenderSA shows an SA's fields present; a PN read from 8 bytes is
// marked (xpn).
func macsecRenderSA(s *MacsecSA) string {
	var f []string
	if s.AN != nil {
		f = append(f, "an="+strconv.Itoa(int(*s.AN)))
	}
	if s.Active != nil {
		f = append(f, "active="+strconv.Itoa(int(*s.Active)))
	}
	if s.PN != nil {
		pn := "pn=" + strconv.FormatUint(*s.PN, 10)
		if s.XPN {
			pn += "(xpn)"
		}
		f = append(f, pn)
	}
	for _, b := range []struct {
		name string
		v    []byte
	}{{"key", s.Key}, {"keyid", s.KeyID}, {"salt", s.Salt}} {
		if b.v != nil {
			f = append(f, b.name+"="+hex.EncodeToString(b.v))
		}
	}
	if s.SSCI != nil {
		f = append(f, fmt.Sprintf("ssci=%08x", *s.SSCI))
	}
	if s.Stats != nil {
		f = append(f, "stats="+macsecRenderStats(s.Stats))
	}
	return strings.Join(f, " ")
}

// TestParseMacsec decodes macsec bodies. The positive rows are bodies
// transcribed from link-write/genl/macsec_*; the rest are hand-built wire
// edges the corpus has none of.
//
// go test ./pkg/xtcpnl/ -run 'TestParseMacsec$'
func TestParseMacsec(t *testing.T) {
	hdr := []byte{MacsecCmdAddTxsa, 1, 0, 0}
	ifindex := rtattr(MacsecAttrIfindex, le32(4))
	sa := func(attrs ...[]byte) []byte { return cat(hdr, ifindex, rtattr(MacsecAttrSAConfig, cat(attrs...))) }
	u64 := func(v uint64) []byte { return cat(le32(uint32(v)), le32(uint32(v>>32))) }
	genlErr := "error: " + ErrGenericNetlink.Error()
	tests := []struct {
		description string
		body        []byte
		expected    string
	}{
		{
			description: "positive: ip's show is a GET_TXSC of the genl header alone (macsec_show)",
			body:        mustHex(t, "00010000"),
			expected:    "cmd=0 ver=1",
		},
		{
			description: "positive: the kernel's dump of a SecY with no SA or channel (macsec_show)",
			body: mustHex(t, "0001000008000100040000000c00090005000100000000006c0004000c00010001020304050600010c00040001000001000280000500050010000000"+
				"050008000100000005000600010000000500070000000000050009000200000005000a000000000005000b000100000005000c000000000005000d00"+
				"000000000500020000000000340007000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000"+
				"00000000640008000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c000500"+
				"00000000000000000c00060000000000000000000c00070000000000000000000c00080000000000000000000400050004000600"),
			expected: "cmd=0 ver=1 ifindex=4 secy{sci=0102030405060001 cipher=0x80020001000001 enc_sa=0 icv=16 protect=1 replay=0 oper=1 validate=2 encrypt=0 inc_sci=1 es=0 scb=0} txsa[] rxsc[] txsc_stats=0,0,0,0 secy_stats=0,0,0,0,0,0,0,0 offload{type=0}",
		},
		{
			description: "positive: ip's tx SA add, the SA nested after the ifindex (macsec_txsa_add)",
			body: mustHex(t, "040100000800010004000000440003000500010000000000080003000100000014000500010000000000000000000000000000001400040081818181"+
				"8181818181818181818181810500020001000000"),
			expected: "cmd=4 ver=1 ifindex=4 sa{an=0 active=1 pn=1 key=81818181818181818181818181818181 keyid=01000000000000000000000000000000}",
		},
		{
			description: "positive: the kernel's dump with that tx SA, its key id and no key (macsec_txsa_add)",
			body: mustHex(t, "0001000008000100040000000c00090005000100000000006c0004000c00010001020304050600010c00040001000001000280000500050010000000"+
				"050008000100000005000600010000000500070000000000050009000200000005000a000000000005000b000100000005000c000000000005000d00"+
				"000000000500020000000000340007000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000"+
				"00000000640008000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c000500"+
				"00000000000000000c00060000000000000000000c00070000000000000000000c000800000000000000000048000500440001001400060008000600"+
				"000000000800070000000000050001000000000008000300010000001400050001000000000000000000000000000000050002000100000004000600"),
			expected: "cmd=0 ver=1 ifindex=4 secy{sci=0102030405060001 cipher=0x80020001000001 enc_sa=0 icv=16 protect=1 replay=0 oper=1 validate=2 encrypt=0 inc_sci=1 es=0 scb=0} txsa[an=0 active=1 pn=1 keyid=01000000000000000000000000000000 stats=-,-,-,-,-,0,0] rxsc[] txsc_stats=0,0,0,0 secy_stats=0,0,0,0,0,0,0,0 offload{type=0}",
		},
		{
			description: "positive: ip's tx SA set, a new packet number and off (macsec_txsa_set)",
			body:        mustHex(t, "0601000008000100040000001c000300050001000000000008000300320000000500020000000000"),
			expected:    "cmd=6 ver=1 ifindex=4 sa{an=0 active=0 pn=50}",
		},
		{
			description: "positive: ip's tx SA delete, the SA number alone (macsec_txsa_del)",
			body:        mustHex(t, "0501000008000100040000000c0003000500010000000000"),
			expected:    "cmd=5 ver=1 ifindex=4 sa{an=0}",
		},
		{
			description: "positive: ip's channel add by address and port, the SCI in network order (macsec_rxsc_add_port)",
			body:        mustHex(t, "010100000800010004000000100002000c0001000200000000010001"),
			expected:    "cmd=1 ver=1 ifindex=4 rxsc{sci=0200000000010001}",
		},
		{
			description: "positive: ip's rx SA add, the channel then the SA (macsec_rxsa_add)",
			body: mustHex(t, "070100000800010004000000100002000c0001000a0b0c0d0e0f00014400030005000100000000000800030001000000140005000200000000000000"+
				"000000000000000014000400828282828282828282828282828282820500020001000000"),
			expected: "cmd=7 ver=1 ifindex=4 rxsc{sci=0a0b0c0d0e0f0001} sa{an=0 active=1 pn=1 key=82828282828282828282828282828282 keyid=02000000000000000000000000000000}",
		},
		{
			description: "positive: the kernel's dump with a channel holding an rx SA (macsec_rxsa_add)",
			body: mustHex(t, "0001000008000100040000000c00090005000100000000006c0004000c00010001020304050600010c00040001000001000280000500050010000000"+
				"050008000100000005000600010000000500070000000000050009000200000005000a000000000005000b000100000005000c000000000005000d00"+
				"000000000500020000000000340007000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000"+
				"00000000640008000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c000500"+
				"00000000000000000c00060000000000000000000c00070000000000000000000c000800000000000000000004000500f8000600f400010005000200"+
				"010000000c0001000a0b0c0d0e0f00017c0004000c00010000000000000000000c00020000000000000000000c00030000000000000000000c000400"+
				"00000000000000000c00050000000000000000000c00060000000000000000000c00070000000000000000000c00080000000000000000000c000900"+
				"00000000000000000c000a000000000000000000600003005c0001002c00060008000100000000000800020000000000080003000000000008000400"+
				"0000000008000500000000000500010000000000080003000100000014000500020000000000000000000000000000000500020001000000"),
			expected: "cmd=0 ver=1 ifindex=4 secy{sci=0102030405060001 cipher=0x80020001000001 enc_sa=0 icv=16 protect=1 replay=0 oper=1 validate=2 encrypt=0 inc_sci=1 es=0 scb=0} txsa[] rxsc[sci=0a0b0c0d0e0f0001 active=1 sas[an=0 active=1 pn=1 keyid=02000000000000000000000000000000 stats=0,0,0,0,0,-,-] stats=0,0,0,0,0,0,0,0,0,0] txsc_stats=0,0,0,0 secy_stats=0,0,0,0,0,0,0,0 offload{type=0}",
		},
		{
			description: "positive: ip's XPN tx SA add, a 64-bit packet number, the salt and the SSCI (macsec_xpn_txsa_add)",
			body: mustHex(t, "0401000008000100040000006000030005000100000000000c0003000100000000000000100009000102030405060708090a0b0c0800080000000001"+
				"140005000100000000000000000000000000000014000400818181818181818181818181818181810500020001000000"),
			expected: "cmd=4 ver=1 ifindex=4 sa{an=0 active=1 pn=1(xpn) key=81818181818181818181818181818181 keyid=01000000000000000000000000000000 salt=0102030405060708090a0b0c ssci=00000001}",
		},
		{
			description: "positive: the kernel's XPN dump, the SSCI back and no salt (macsec_xpn_txsa_add)",
			body: mustHex(t, "0001000008000100040000000c00090005000100000000006c0004000c00010001020304050600010c0004000300000100c280000500050010000000"+
				"050008000100000005000600010000000500070000000000050009000200000005000a000000000005000b000100000005000c000000000005000d00"+
				"000000000500020000000000340007000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000"+
				"00000000640008000c00010000000000000000000c00020000000000000000000c00030000000000000000000c00040000000000000000000c000500"+
				"00000000000000000c00060000000000000000000c00070000000000000000000c000800000000000000000054000500500001001400060008000600"+
				"00000000080007000000000005000100000000000c000300010000000000000014000500010000000000000000000000000000000800080000000001"+
				"050002000100000004000600"),
			expected: "cmd=0 ver=1 ifindex=4 secy{sci=0102030405060001 cipher=0x80c20001000003 enc_sa=0 icv=16 protect=1 replay=0 oper=1 validate=2 encrypt=0 inc_sci=1 es=0 scb=0} txsa[an=0 active=1 pn=1(xpn) keyid=01000000000000000000000000000000 ssci=00000001 stats=-,-,-,-,-,0,0] rxsc[] txsc_stats=0,0,0,0 secy_stats=0,0,0,0,0,0,0,0 offload{type=0}",
		},
		{
			description: "boundary: a 32-bit packet number is not XPN (macsec_xpn_txsa_pn32)",
			body: mustHex(t, "0401000008000100040000003c0003000500010000000000080003000100000014000500010000000000000000000000000000001400040081818181"+
				"818181818181818181818181"),
			expected: "cmd=4 ver=1 ifindex=4 sa{an=0 pn=1 key=81818181818181818181818181818181 keyid=01000000000000000000000000000000}",
		},
		{
			description: "positive: ip's offload off (macsec_offload_off)",
			body:        mustHex(t, "0a01000008000100040000000c0009000500010000000000"),
			expected:    "cmd=10 ver=1 ifindex=4 offload{type=0}",
		},
		{
			description: "boundary: a body one byte short of the genl header is an error",
			body:        hdr[:3],
			expected:    genlErr + ": short genlmsghdr",
		},
		{
			description: "boundary: the largest packet numbers, 32 and 64 bits",
			body: cat(hdr, rtattr(MacsecAttrRxscConfig, rtattr(MacsecRxscAttrSCI, u64(^uint64(0)))),
				rtattr(MacsecAttrTxsaList, cat(rtattr(1, rtattr(MacsecSAAttrPN, le32(0xffffffff))), rtattr(2, rtattr(MacsecSAAttrPN, u64(^uint64(0))))))),
			expected: "cmd=4 ver=1 rxsc{sci=ffffffffffffffff} txsa[pn=4294967295;pn=18446744073709551615(xpn)]",
		},
		{
			description: "boundary: an empty offload nest has no type",
			body:        cat(hdr, rtattr(MacsecAttrOffload, nil)),
			expected:    "cmd=4 ver=1 offload{}",
		},
		{
			description: "corner: every SA counter decodes by its own number",
			body: sa(rtattr(MacsecSAAttrStats, cat(rtattr(1, le32(1)), rtattr(2, le32(2)), rtattr(3, le32(3)), rtattr(4, le32(4)),
				rtattr(5, le32(5)), rtattr(6, le32(6)), rtattr(7, le32(7))))),
			expected: "cmd=4 ver=1 ifindex=4 sa{stats=1,2,3,4,5,6,7}",
		},
		{
			description: "corner: every SecY, tx channel and rx channel counter decodes by its own number, absent ones shown",
			body: cat(hdr, rtattr(MacsecAttrTxscStats, cat(rtattr(1, u64(1)), rtattr(4, u64(4)), rtattr(5, nil))),
				rtattr(MacsecAttrSecYStats, cat(rtattr(2, u64(2)), rtattr(8, u64(8)))),
				rtattr(MacsecAttrRxscList, rtattr(1, rtattr(MacsecRxscAttrStats, cat(rtattr(3, u64(3)), rtattr(10, u64(1<<40))))))),
			expected: "cmd=4 ver=1 rxsc[stats=-,-,3,-,-,-,-,-,-,1099511627776] txsc_stats=1,-,-,4 secy_stats=-,2,-,-,-,-,-,8",
		},
		{
			description: "corner: every SecY field decodes by its own number",
			body: cat(hdr, rtattr(MacsecAttrSecY, cat(rtattr(MacsecSecYAttrSCI, []byte{1, 2, 3, 4, 5, 6, 0, 1}),
				rtattr(MacsecSecYAttrEncodingSA, []byte{3}), rtattr(MacsecSecYAttrWindow, le32(32)),
				rtattr(MacsecSecYAttrCipherSuite, u64(0x80C20001000004)), rtattr(MacsecSecYAttrICVLen, []byte{16}),
				rtattr(MacsecSecYAttrProtect, []byte{4}), rtattr(MacsecSecYAttrReplay, []byte{5}), rtattr(MacsecSecYAttrOper, []byte{6}),
				rtattr(MacsecSecYAttrValidate, []byte{7}), rtattr(MacsecSecYAttrEncrypt, []byte{8}), rtattr(MacsecSecYAttrIncSCI, []byte{9}),
				rtattr(MacsecSecYAttrES, []byte{10}), rtattr(MacsecSecYAttrSCB, []byte{11}), rtattr(14, nil)))),
			expected: "cmd=4 ver=1 secy{sci=0102030405060001 window=32 cipher=0x80c20001000004 enc_sa=3 icv=16 protect=4 replay=5 oper=6 validate=7 encrypt=8 inc_sci=9 es=10 scb=11}",
		},
		{
			description: "corner: list entries are read in the order they came, whatever their numbers",
			body:        cat(hdr, rtattr(MacsecAttrTxsaList, cat(rtattr(7, rtattr(MacsecSAAttrAN, []byte{3})), rtattr(1, rtattr(MacsecSAAttrAN, []byte{1}))))),
			expected:    "cmd=4 ver=1 txsa[an=3;an=1]",
		},
		{
			description: "corner: the pads, an unknown attribute and an unknown command are not errors",
			body: cat([]byte{42, 1, 0, 0}, rtattr(MacsecAttrRxscConfig, cat(rtattr(5, nil), rtattr(9, nil))), rtattr(99, le32(1)),
				rtattr(MacsecAttrOffload, rtattr(2, nil))),
			expected: "cmd=42 ver=1 rxsc{} offload{}",
		},
		{
			description: "negative: a packet number neither 4 nor 8 bytes is an error",
			body:        sa(rtattr(MacsecSAAttrPN, []byte{1, 0})),
			expected:    genlErr + ": attribute 3 needs an unflagged 4- or 8-byte scalar",
		},
		{
			description: "negative: an SSCI narrower than a u32 is an error",
			body:        sa(rtattr(MacsecSAAttrSSCI, []byte{1})),
			expected:    genlErr + ": attribute 8 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: an SCI narrower than a u64 is an error",
			body:        cat(hdr, rtattr(MacsecAttrRxscConfig, rtattr(MacsecRxscAttrSCI, le32(1)))),
			expected:    genlErr + ": attribute 1 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: a SecY cipher suite narrower than a u64 is an error",
			body:        cat(hdr, rtattr(MacsecAttrSecY, rtattr(MacsecSecYAttrCipherSuite, le32(1)))),
			expected:    genlErr + ": attribute 4 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: an SA counter wider than a u32 is an error",
			body:        sa(rtattr(MacsecSAAttrStats, rtattr(1, u64(1)))),
			expected:    genlErr + ": attribute 1 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: a SecY counter narrower than a u64 is an error",
			body:        cat(hdr, rtattr(MacsecAttrSecYStats, rtattr(3, le32(1)))),
			expected:    genlErr + ": attribute 3 needs an unflagged 8-byte scalar",
		},
		{
			description: "negative: an ifindex wider than a u32 is an error",
			body:        cat(hdr, rtattr(MacsecAttrIfindex, u64(4))),
			expected:    genlErr + ": attribute 1 needs an unflagged 4-byte scalar",
		},
		{
			description: "negative: an offload type wider than a u8 is an error",
			body:        cat(hdr, rtattr(MacsecAttrOffload, rtattr(MacsecOffloadAttrType, le32(0)))),
			expected:    genlErr + ": attribute 1 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a network-order SA nest is an error",
			body:        cat(hdr, rtattr(MacsecAttrSAConfig|0x4000, nil)),
			expected:    genlErr + ": network-order nested attribute",
		},
		{
			description: "negative: a network-order list entry is an error",
			body:        cat(hdr, rtattr(MacsecAttrRxscList, rtattr(1|0x4000, nil))),
			expected:    genlErr + ": network-order nested attribute",
		},
		{
			description: "negative: a list entry longer than its nest is an error",
			body:        cat(hdr, rtattr(MacsecAttrTxsaList, rtattr(1, rtattrOverrunning(MacsecSAAttrAN, 40)))),
			expected:    genlErr + ": attribute length 40",
		},
		{
			description: "negative: a malformed SA in a channel's list is an error",
			body:        cat(hdr, rtattr(MacsecAttrRxscList, rtattr(1, rtattr(MacsecRxscAttrSAList, rtattr(1, rtattr(MacsecSAAttrAN, le32(0))))))),
			expected:    genlErr + ": attribute 1 needs an unflagged 1-byte scalar",
		},
		{
			description: "negative: a repeated SA number inside the nest is an error",
			body:        sa(rtattr(MacsecSAAttrAN, []byte{0}), rtattr(MacsecSAAttrAN, []byte{1})),
			expected:    genlErr + ": duplicate attribute 1",
		},
		{
			description: "negative: an attribute longer than the body is an error",
			body:        cat(hdr, rtattrOverrunning(MacsecAttrIfindex, 40)),
			expected:    genlErr + ": attribute length 40",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := macsecRender(ParseMacsec(tt.body)); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseMacsecCorpus decodes every macsec message in the committed
// corpus.
//
// go test ./pkg/xtcpnl/ -run TestParseMacsecCorpus
func TestParseMacsecCorpus(t *testing.T) {
	// tally decodes every macsec message of command cmd, ip's requests or
	// the kernel's answers, and counts the fields they carry.
	tally := func(cmd uint8, request bool) string {
		var n, errs, ifindex, rxsc, sa, secy, txsas, rxscs, stats int
		count := func(ok bool, c *int) {
			if ok {
				*c++
			}
		}
		for _, c := range corpusAll(t) {
			m := c.m
			if c.family != MacsecGenlName || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request || len(m.body) == 0 || m.body[0] != cmd {
				continue
			}
			n++
			s, err := ParseMacsec(m.body)
			if err != nil {
				errs++
				continue
			}
			count(s.Ifindex != nil, &ifindex)
			count(s.RxSC != nil, &rxsc)
			count(s.SA != nil, &sa)
			count(s.SecY != nil && s.SecY.SCI != nil, &secy)
			txsas += len(s.TxSAs)
			rxscs += len(s.RxSCs)
			count(s.TxscStats != nil && s.SecYStats != nil, &stats)
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(ifindex) + " ifindex, " +
			strconv.Itoa(rxsc) + " rxsc, " + strconv.Itoa(sa) + " sa, " + strconv.Itoa(secy) + " secy, " +
			strconv.Itoa(txsas) + " txsas, " + strconv.Itoa(rxscs) + " rxscs, " + strconv.Itoa(stats) + " stats"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's GET_TXSC requests decode, every one a dump of the genl header alone", got: tally(MacsecCmdGetTxsc, true), expected: "16 decoded, 0 errors, 0 ifindex, 0 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "positive: every SecY the kernel dumped decodes, with its counters", got: tally(MacsecCmdGetTxsc, false), expected: "17 decoded, 0 errors, 17 ifindex, 0 rxsc, 0 sa, 17 secy, 4 txsas, 6 rxscs, 17 stats"},
		{description: "positive: ip's ADD_TXSA requests decode, each with its SA", got: tally(MacsecCmdAddTxsa, true), expected: "7 decoded, 0 errors, 7 ifindex, 0 rxsc, 7 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "positive: ip's UPD_TXSA and DEL_TXSA requests decode", got: tally(MacsecCmdUpdTxsa, true) + "|" + tally(MacsecCmdDelTxsa, true), expected: "1 decoded, 0 errors, 1 ifindex, 0 rxsc, 1 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|3 decoded, 0 errors, 3 ifindex, 0 rxsc, 3 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "positive: ip's ADD_RXSC, UPD_RXSC and DEL_RXSC requests decode, each with its channel", got: tally(MacsecCmdAddRxsc, true) + "|" + tally(MacsecCmdUpdRxsc, true) + "|" + tally(MacsecCmdDelRxsc, true), expected: "3 decoded, 0 errors, 3 ifindex, 3 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|1 decoded, 0 errors, 1 ifindex, 1 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|2 decoded, 0 errors, 2 ifindex, 2 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "positive: ip's ADD_RXSA, UPD_RXSA and DEL_RXSA requests decode, each with its channel and SA", got: tally(MacsecCmdAddRxsa, true) + "|" + tally(MacsecCmdUpdRxsa, true) + "|" + tally(MacsecCmdDelRxsa, true), expected: "2 decoded, 0 errors, 2 ifindex, 2 rxsc, 2 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|1 decoded, 0 errors, 1 ifindex, 1 rxsc, 1 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|2 decoded, 0 errors, 2 ifindex, 2 rxsc, 2 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "positive: ip's UPD_OFFLOAD requests decode", got: tally(MacsecCmdUpdOffload, true), expected: "2 decoded, 0 errors, 2 ifindex, 0 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
		{description: "negative: the kernel answers no command but GET_TXSC", got: tally(MacsecCmdAddTxsa, false) + "|" + tally(MacsecCmdUpdOffload, false), expected: "0 decoded, 0 errors, 0 ifindex, 0 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats|0 decoded, 0 errors, 0 ifindex, 0 rxsc, 0 sa, 0 secy, 0 txsas, 0 rxscs, 0 stats"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
