package render

import (
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// xfrmA4 is an IPv4 xfrm_address_t.
func xfrmA4(a, b, c, d byte) xtcpnl.XfrmAddress {
	return xtcpnl.XfrmAddress{a, b, c, d}
}

func xfrmP32(v uint32) *uint32 { return &v }
func xfrmP64(v uint64) *uint64 { return &v }
func xfrmP8(v uint8) *uint8    { return &v }

// xfrmBaseSA is ESP in transport mode from 192.0.2.1 to 192.0.2.2, SPI
// 0x100, reqid 1, with no selector, no lifetime and no flags.
func xfrmBaseSA() xtcpnl.XfrmUsersaInfo {
	return xtcpnl.XfrmUsersaInfo{
		ID:     xtcpnl.XfrmID{Daddr: xfrmA4(192, 0, 2, 2), SPI: 0x100, Proto: unix.IPPROTO_ESP},
		Saddr:  xfrmA4(192, 0, 2, 1),
		Reqid:  1,
		Family: unix.AF_INET,
	}
}

// TestXfrmStateInfo is xfrm_state_info_print over decoded SAs, every line it
// can print.
//
// go test ./internal/goip/render/ -run TestXfrmStateInfo
func TestXfrmStateInfo(t *testing.T) {
	const (
		head = "src 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000100 reqid 1 mode transport\n"
		rw   = "\treplay-window 0 \n"
	)
	inf := ^uint64(0)
	stats := xfrmBaseSA()
	stats.Lft = xtcpnl.XfrmLifetimeCfg{SoftByteLimit: inf, HardByteLimit: inf, SoftPacketLimit: inf,
		HardPacketLimit: 10, HardAddExpiresSeconds: 3600}
	stats.Curlft = xtcpnl.XfrmLifetimeCur{Bytes: 5, Packets: 1, AddTime: 86400}
	stats.Stats = xtcpnl.XfrmStats{ReplayWindow: 1, Replay: 2, IntegrityFailed: 3}
	stats.Seq = 10
	flags := xfrmBaseSA()
	flags.Flags = 0x41
	tunnel := xfrmBaseSA()
	tunnel.Mode, tunnel.ID.SPI, tunnel.ID.Proto = 1, 0, unix.IPPROTO_IPIP
	zeroSPI := xfrmBaseSA()
	zeroSPI.ID.SPI = 0
	odd := xfrmBaseSA()
	odd.Mode, odd.Family = 9, unix.AF_UNSPEC
	sel := xfrmBaseSA()
	sel.Sel = xtcpnl.XfrmSelector{Saddr: xfrmA4(192, 0, 2, 0), PrefixlenS: 24, Ifindex: 7, User: 4}
	icmp := sel
	icmp.Sel.Proto, icmp.Sel.Ifindex, icmp.Sel.Sport, icmp.Sel.SportMask = unix.IPPROTO_ICMP, 0, 8, 0xffff
	icmp.Sel.Dport, icmp.Sel.DportMask = 3, 0xffff
	gre := icmp
	gre.Sel.Proto = unix.IPPROTO_GRE
	mh := icmp
	mh.Sel.Proto = unix.IPPROTO_MH
	tcp := icmp
	tcp.Sel.Proto, tcp.Sel.SportMask = unix.IPPROTO_TCP, 0
	key := []byte{1, 2}
	algo := func(name string, bits, icv uint32) *xtcpnl.XfrmAlgo {
		return &xtcpnl.XfrmAlgo{Name: name, KeyBits: bits, ICVTruncBits: icv, Key: key}
	}
	base := xfrmBaseSA()
	// sframe is the -s frame around a row's own lines.
	sframe := func(mid string) string {
		return "src 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000100(256) reqid 1(0x00000001) mode transport\n" +
			"\treplay-window 0 seq 0x00000000 flag  (0x00000000)\n" + mid +
			"\tlifetime config:\n\t  limit: soft 0(bytes), hard 0(bytes)\n\t  limit: soft 0(packets), hard 0(packets)\n" +
			"\t  expire add: soft 0(sec), hard 0(sec)\n\t  expire use: soft 0(sec), hard 0(sec)\n" +
			"\tlifetime current:\n\t  0(bytes), 0(packets)\n\t  add - use -\n" +
			"\tstats:\n\t  replay-window 0 replay 0 failed 0\n"
	}
	tests := []struct {
		description string
		info        xtcpnl.XfrmUsersaInfo
		attrs       xtcpnl.XfrmAttrs
		opt         XfrmOpt
		expected    string
	}{
		{"positive: the identity and replay window alone", base, xtcpnl.XfrmAttrs{}, XfrmOpt{}, head + rw},
		{"positive: -s, the SPI's and reqid's other bases, seq, flags and their mask, lifetimes and stats", stats,
			xtcpnl.XfrmAttrs{ExtraFlags: xfrmP32(0x11)}, XfrmOpt{Stats: true, Zone: time.UTC},
			"src 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000100(256) reqid 1(0x00000001) mode transport\n" +
				"\treplay-window 0 seq 0x00000010 flag extra_flag dont-encap-dscp 10 (0x00000000)\n" +
				"\tlifetime config:\n\t  limit: soft (INF)(bytes), hard (INF)(bytes)\n" +
				"\t  limit: soft (INF)(packets), hard 10(packets)\n" +
				"\t  expire add: soft 0(sec), hard 3600(sec)\n\t  expire use: soft 0(sec), hard 0(sec)\n" +
				"\tlifetime current:\n\t  5(bytes), 1(packets)\n\t  add 1970-01-02 00:00:00 use -\n" +
				"\tstats:\n\t  replay-window 1 replay 2 failed 3\n"},
		{"positive: flags by name, a space while bits remain", flags, xtcpnl.XfrmAttrs{}, XfrmOpt{},
			head + "\treplay-window 0 flag noecn align4\n"},
		{"boundary: an SPI of 0 on ESP printed, IPsec forcing it", zeroSPI, xtcpnl.XfrmAttrs{}, XfrmOpt{},
			"src 192.0.2.1 dst 192.0.2.2\n\tproto esp spi 0x00000000 reqid 1 mode transport\n" + rw},
		{"corner: an SPI of 0 on IPIP not printed, tunnel mode", tunnel, xtcpnl.XfrmAttrs{}, XfrmOpt{},
			"src 192.0.2.1 dst 192.0.2.2\n\tproto 4 reqid 1 mode tunnel\n" + rw},
		{"corner: a family neither, ???, a mode past the table", odd, xtcpnl.XfrmAttrs{}, XfrmOpt{},
			"src ??? dst ???\n\tproto esp spi 0x00000100 reqid 1 mode 9\n" + rw},
		{"positive: auth then enc then comp, keys in hex, comp's none", base, xtcpnl.XfrmAttrs{
			AlgAuth: algo("hmac(sha1)", 16, 0), AlgCrypt: algo("cbc(aes)", 16, 0), AlgComp: algo("deflate", 0, 0)},
			XfrmOpt{}, head + rw + "\tauth hmac(sha1) 0x0102\n\tenc cbc(aes) 0x0102\n\tcomp deflate \n"},
		{"positive: auth-trunc wins over auth, its truncation after", base, xtcpnl.XfrmAttrs{
			AlgAuth: algo("hmac(sha1)", 16, 0), AlgAuthTrunc: algo("hmac(sha256)", 16, 96)}, XfrmOpt{},
			head + rw + "\tauth-trunc hmac(sha256) 0x0102 96\n"},
		{"positive: aead and its ICV length", base, xtcpnl.XfrmAttrs{AlgAEAD: algo("rfc4106(gcm(aes))", 16, 128)},
			XfrmOpt{}, head + rw + "\taead rfc4106(gcm(aes)) 0x0102 128\n"},
		{"positive: nokeys hides them", base, xtcpnl.XfrmAttrs{AlgAEAD: algo("rfc4106(gcm(aes))", 16, 128)},
			XfrmOpt{Nokeys: true}, head + rw + "\taead rfc4106(gcm(aes)) <<Keys hidden>> 128\n"},
		{"boundary: a key of 12 bits prints alg_key_len / 8 bytes, -s its bits", base,
			xtcpnl.XfrmAttrs{AlgCrypt: algo("cbc(aes)", 12, 0)}, XfrmOpt{Stats: true, Zone: time.UTC},
			sframe("\tenc cbc(aes) 0x01 (12 bits)\n")},
		{"positive: a mark and the output mark with its mask", base, xtcpnl.XfrmAttrs{
			Mark: &xtcpnl.XfrmMark{V: 5, M: 0xff}, SetMark: xfrmP32(7), SetMarkMask: xfrmP32(0xf)}, XfrmOpt{},
			head + rw + "\tmark 0x5/0xff output-mark 0x7/0xf\n"},
		{"boundary: a zero mark is %#x's 0", base, xtcpnl.XfrmAttrs{Mark: &xtcpnl.XfrmMark{}}, XfrmOpt{},
			head + rw + "\tmark 0/0 \n"},
		{"positive: the output mark alone", base, xtcpnl.XfrmAttrs{SetMark: xfrmP32(7)}, XfrmOpt{},
			head + rw + "\toutput-mark 0x7\n"},
		{"positive: ESP in UDP, and a type past the names", base, xtcpnl.XfrmAttrs{
			Encap: &xtcpnl.XfrmEncapTmpl{Type: 2, Sport: 4500, Dport: 4501}}, XfrmOpt{},
			head + rw + "\tencap type espinudp sport 4500 dport 4501 addr 0.0.0.0\n"},
		{"corner: an encapsulation type it does not name", base, xtcpnl.XfrmAttrs{
			Encap: &xtcpnl.XfrmEncapTmpl{Type: 9}}, XfrmOpt{}, head + rw + "\tencap type 9 sport 0 dport 0 addr 0.0.0.0\n"},
		{"positive: care-of address, last use, replay state", base, xtcpnl.XfrmAttrs{
			CoAddr: func() *xtcpnl.XfrmAddress { a := xfrmA4(192, 0, 2, 9); return &a }(), LastUsed: xfrmP64(0),
			ReplayVal: &xtcpnl.XfrmReplayState{OSeq: 2, Seq: 1, Bitmap: 3}}, XfrmOpt{},
			head + rw + "\tcoa 192.0.2.9\n\tlastused -\n\tanti-replay context: seq 0x1, oseq 0x2, bitmap 0x00000003\n"},
		{"positive: the ESN replay state, its bitmap from the last word", base, xtcpnl.XfrmAttrs{
			ReplayESNVal: &xtcpnl.XfrmReplayStateESN{Seq: 1, OSeq: 2, ReplayWindow: 64, Bmp: []uint32{1, 2}}},
			XfrmOpt{}, head + rw + "\tanti-replay esn context:\n\t seq-hi 0x0, seq 0x1, oseq-hi 0x0, oseq 0x2\n" +
				"\t replay_window 64, bitmap-length 2\n\t 00000002 00000001 \n"},
		{"positive: offload, interface id, padding, direction and CPU", base, xtcpnl.XfrmAttrs{
			OffloadDev: &xtcpnl.XfrmUserOffload{Ifindex: 7, Flags: 6}, IfID: xfrmP32(0x10), TFCPad: xfrmP32(5),
			SADir: xfrmP8(1), SAPcpu: xfrmP32(2)}, XfrmOpt{IndexName: func(int32) string { return "e7" }},
			head + rw + "\tcrypto offload parameters: dev e7 dir in mode packet\n\tif_id 0x10\n\ttfcpad 5\n" +
				"\tdir in\n\tpcpu-num 2\n"},
		{"corner: an outbound crypto offload, interface id 0, a direction past the names", base, xtcpnl.XfrmAttrs{
			OffloadDev: &xtcpnl.XfrmUserOffload{Ifindex: 7}, IfID: xfrmP32(0), SADir: xfrmP8(3)},
			XfrmOpt{IndexName: func(int32) string { return "e7" }},
			head + rw + "\tcrypto offload parameters: dev e7 dir out mode crypto\n\tif_id 0\n\tdir other (3)\n"},
		{"positive: a selector in the SA's family, its device", sel, xtcpnl.XfrmAttrs{},
			XfrmOpt{IndexName: func(int32) string { return "e7" }},
			head + rw + "\tsel src 192.0.2.0/24 dst 0.0.0.0/0 dev e7 \n"},
		{"positive: -s adds the selector's uid", sel, xtcpnl.XfrmAttrs{}, XfrmOpt{Stats: true, Zone: time.UTC,
			IndexName: func(int32) string { return "e7" }},
			sframe("\tsel src 192.0.2.0/24 dst 0.0.0.0/0 dev e7 uid 4\n")},
		{"positive: ICMP's type and code", icmp, xtcpnl.XfrmAttrs{}, XfrmOpt{ProtoName: func(uint8) string {
			return "icmp"
		}}, head + rw + "\tsel src 192.0.2.0/24 dst 0.0.0.0/0 proto icmp type 8 code 3 \n"},
		{"positive: GRE's key", gre, xtcpnl.XfrmAttrs{}, XfrmOpt{ProtoName: func(uint8) string { return "gre" }},
			head + rw + "\tsel src 192.0.2.0/24 dst 0.0.0.0/0 proto gre key 524291 \n"},
		{"corner: MH's type, its dport only under -s", mh, xtcpnl.XfrmAttrs{},
			XfrmOpt{ProtoName: func(uint8) string { return "mobility-header" }},
			head + rw + "\tsel src 192.0.2.0/24 dst 0.0.0.0/0 proto mobility-header type 8 \n"},
		{"positive: TCP's dport alone when only it has a mask", tcp, xtcpnl.XfrmAttrs{},
			XfrmOpt{ProtoName: func(uint8) string { return "tcp" }},
			head + rw + "\tsel src 192.0.2.0/24 dst 0.0.0.0/0 proto tcp dport 3 \n"},
		{"positive: a security context", base, xtcpnl.XfrmAttrs{SecCtx: &xtcpnl.XfrmSecCtx{Ctx: []byte("abc")}},
			XfrmOpt{}, head + rw + "\tsecurity context abc \n"},
		{"positive: a template, its level use", base, xtcpnl.XfrmAttrs{Tmpl: []xtcpnl.XfrmUserTmpl{{
			ID: xtcpnl.XfrmID{Daddr: xfrmA4(192, 0, 2, 2), Proto: unix.IPPROTO_ESP}, Family: unix.AF_INET,
			Optional: 1}}}, XfrmOpt{},
			head + rw + "\ttmpl src 0.0.0.0 dst 192.0.2.2\n\t\tproto esp reqid 0 mode transport\n\t\tlevel use \n"},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmStateInfo(&tc.info, &tc.attrs, &tc.opt); got != tc.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tc.expected)
			}
		})
	}
}

// TestXfrmSADInfo is print_sadinfo.
//
// go test ./internal/goip/render/ -run TestXfrmSADInfo
func TestXfrmSADInfo(t *testing.T) {
	hinfo := &xtcpnl.XfrmHInfo{Count: 8, MaxCount: 1048576}
	tests := []struct {
		description string
		info        xtcpnl.XfrmInfo
		stats       bool
		expected    string
	}{
		{"positive: the count", xtcpnl.XfrmInfo{SADCnt: xfrmP32(2), SADHInfo: hinfo}, false, "\t SAD count 2\n"},
		{"positive: -s, the buckets", xtcpnl.XfrmInfo{SADCnt: xfrmP32(2), SADHInfo: hinfo}, true,
			"\t SAD count 2 (buckets count 8 Max 1048576)\n"},
		{"boundary: -s with no hash information", xtcpnl.XfrmInfo{SADCnt: xfrmP32(0)}, true, "\t SAD count 0\n"},
		{"negative: no count", xtcpnl.XfrmInfo{SADHInfo: hinfo}, true, "BAD SAD info returned\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmSADInfo(&tc.info, tc.stats); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestXfrmProtoName is strxf_xfrmproto and xfrm_xfrmproto_getbyname.
//
// go test ./internal/goip/render/ -run TestXfrmProtoName
func TestXfrmProtoName(t *testing.T) {
	tests := []struct {
		description string
		proto       uint8
		name        string
	}{
		{"positive: esp", unix.IPPROTO_ESP, "esp"},
		{"positive: ah", unix.IPPROTO_AH, "ah"},
		{"positive: comp", unix.IPPROTO_COMP, "comp"},
		{"positive: route2", unix.IPPROTO_ROUTING, "route2"},
		{"positive: hao", unix.IPPROTO_DSTOPTS, "hao"},
		{"boundary: ipsec-any, 255", 255, "ipsec-any"},
		{"negative: a number with no name", unix.IPPROTO_TCP, "6"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmProtoName(tc.proto); got != tc.name {
				t.Errorf("name %q, want %q", got, tc.name)
			}
			want := int(tc.proto)
			if strings.ContainsAny(tc.name[:1], "0123456789") {
				want = -1
			}
			if got := XfrmProtoByName(tc.name); got != want {
				t.Errorf("by name %d, want %d", got, want)
			}
		})
	}
	if XfrmProtoByName("ESP") != -1 {
		t.Error("ESP by strcmp is no protocol")
	}
}

// TestXfrmPolicyInfo is xfrm_policy_info_print over decoded policies, every
// line it can print.
//
// go test ./internal/goip/render/ -run TestXfrmPolicyInfo
func TestXfrmPolicyInfo(t *testing.T) {
	base := xtcpnl.XfrmUserpolicyInfo{Sel: xtcpnl.XfrmSelector{Saddr: xfrmA4(192, 0, 2, 1),
		Daddr: xfrmA4(192, 0, 2, 2), PrefixlenS: 32, PrefixlenD: 32, Family: unix.AF_INET}, Dir: 1}
	const sel = "src 192.0.2.1/32 dst 192.0.2.2/32 \n"
	with := func(f func(p *xtcpnl.XfrmUserpolicyInfo)) xtcpnl.XfrmUserpolicyInfo {
		p := base
		f(&p)
		return p
	}
	tmpl := xtcpnl.XfrmUserTmpl{ID: xtcpnl.XfrmID{Daddr: xfrmA4(192, 0, 2, 2), Proto: unix.IPPROTO_ESP},
		Saddr: xfrmA4(192, 0, 2, 1), Family: unix.AF_INET, Reqid: 1}
	inf := ^uint64(0)
	tests := []struct {
		description string
		info        xtcpnl.XfrmUserpolicyInfo
		attrs       xtcpnl.XfrmAttrs
		stats       bool
		family      uint8
		expected    string
	}{
		{description: "positive: out, allow, no template", info: base,
			expected: sel + "\tdir out priority 0 \n"},
		{description: "positive: the corpus's P1, a template", info: base,
			attrs: xtcpnl.XfrmAttrs{Tmpl: []xtcpnl.XfrmUserTmpl{tmpl}},
			expected: sel + "\tdir out priority 0 \n\ttmpl src 192.0.2.1 dst 192.0.2.2\n" +
				"\t\tproto esp reqid 1 mode transport\n"},
		{description: "positive: in, block, priority 10", info: with(func(p *xtcpnl.XfrmUserpolicyInfo) {
			p.Dir, p.Action, p.Priority = 0, 1, 10
		}), expected: sel + "\tdir in action block priority 10 \n"},
		{description: "positive: fwd", info: with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Dir = 2 }),
			expected: sel + "\tdir fwd priority 0 \n"},
		{description: "corner: a socket policy's direction, offset by XFRM_POLICY_MAX",
			info:     with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Dir = 4 }),
			expected: sel + "\tsocket out priority 0 \n"},
		{description: "boundary: XFRM_POLICY_MAX itself, a socket's in",
			info:     with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Dir = 3 }),
			expected: sel + "\tsocket in priority 0 \n"},
		{description: "boundary: a direction past fwd, as a number", info: with(func(p *xtcpnl.XfrmUserpolicyInfo) {
			p.Dir = 6
		}), expected: sel + "\tsocket 3 priority 0 \n"},
		{description: "corner: an action ip does not know, as a number",
			info:     with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Action = 7 }),
			expected: sel + "\tdir out action 7 priority 0 \n"},
		{description: "positive: ptype sub, and a ptype past it as a number", info: base,
			attrs: xtcpnl.XfrmAttrs{PolicyType: xfrmP8(1)}, expected: sel + "\tdir out priority 0 ptype sub \n"},
		{description: "corner: ptype 5", info: base, attrs: xtcpnl.XfrmAttrs{PolicyType: xfrmP8(5)},
			expected: sel + "\tdir out priority 0 ptype 5 \n"},
		{description: "positive: flags localok and icmp, then a bit past them in hex",
			info:     with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Flags = 0x13 }),
			expected: sel + "\tdir out priority 0 flag localok icmp 10\n"},
		{description: "positive: a security context, after the selector", info: base,
			attrs:    xtcpnl.XfrmAttrs{SecCtx: &xtcpnl.XfrmSecCtx{Ctx: []byte("c")}},
			expected: sel + "\tsecurity context c \n\tdir out priority 0 \n"},
		{description: "positive: -s, the action, index, share, flags, mask and lifetimes",
			info: with(func(p *xtcpnl.XfrmUserpolicyInfo) {
				p.Index, p.Share, p.Flags = 9, 1, 1
				p.Lft = xtcpnl.XfrmLifetimeCfg{SoftByteLimit: inf, HardByteLimit: inf, SoftPacketLimit: inf,
					HardPacketLimit: inf}
			}), stats: true,
			expected: "src 192.0.2.1/32 dst 192.0.2.2/32 uid 0\n\tdir out action allow index 9 priority 0 " +
				"share session flag localok (0x00000001)\n" +
				"\tlifetime config:\n" +
				"\t  limit: soft (INF)(bytes), hard (INF)(bytes)\n" +
				"\t  limit: soft (INF)(packets), hard (INF)(packets)\n" +
				"\t  expire add: soft 0(sec), hard 0(sec)\n" +
				"\t  expire use: soft 0(sec), hard 0(sec)\n" +
				"\tlifetime current:\n" +
				"\t  0(bytes), 0(packets)\n" +
				"\t  add - use -\n"},
		{description: "corner: a selector with no family takes preferred_family's",
			info: with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Sel.Family = unix.AF_UNSPEC }), family: unix.AF_INET,
			expected: sel + "\tdir out priority 0 \n"},
		{description: "boundary: no family anywhere, rt_addr_n2a's ???",
			info:     with(func(p *xtcpnl.XfrmUserpolicyInfo) { p.Sel.Family = unix.AF_UNSPEC }),
			expected: "src ???/32 dst ???/32 \n\tdir out priority 0 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			o := XfrmOpt{Stats: tc.stats, Family: tc.family, Zone: time.UTC}
			if got := XfrmPolicyInfo(&tc.info, &tc.attrs, &o); got != tc.expected {
				t.Errorf("got\n%q\nwant\n%q", got, tc.expected)
			}
		})
	}
}

// TestXfrmSPDInfo is print_spdinfo: the counts, -s's socket counts, and -s
// -s's buckets and thresholds, each when present.
//
// go test ./internal/goip/render/ -run TestXfrmSPDInfo
func TestXfrmSPDInfo(t *testing.T) {
	si := &xtcpnl.XfrmSPDInfo{In: 1, Out: 2, Fwd: 3, InS: 4, OutS: 5, FwdS: 6}
	full := xtcpnl.XfrmInfo{SPDInfo: si, SPDHInfo: &xtcpnl.XfrmHInfo{Count: 7, MaxCount: 1048576},
		IPv4HThresh: &xtcpnl.XfrmSPDHThresh{LBits: 32, RBits: 32},
		IPv6HThresh: &xtcpnl.XfrmSPDHThresh{LBits: 128, RBits: 128}}
	tests := []struct {
		description string
		info        xtcpnl.XfrmInfo
		stats       int
		expected    string
	}{
		{"positive: the counts", full, 0, "\t SPD IN  1 OUT 2 FWD 3\n"},
		{"positive: -s, the socket counts", full, 1, "\t SPD IN  1 OUT 2 FWD 3 (Sock: IN 4 OUT 5 FWD 6)\n"},
		{"positive: -s -s, the buckets and thresholds", full, 2,
			"\t SPD IN  1 OUT 2 FWD 3 (Sock: IN 4 OUT 5 FWD 6)\n\t SPD buckets: count 7 Max 1048576\n" +
				"\t SPD IPv4 thresholds: local 32 remote 32\n\t SPD IPv6 thresholds: local 128 remote 128\n"},
		{"boundary: -s -s with only the IPv6 thresholds", xtcpnl.XfrmInfo{SPDInfo: si,
			IPv6HThresh: full.IPv6HThresh}, 2,
			"\t SPD IN  1 OUT 2 FWD 3 (Sock: IN 4 OUT 5 FWD 6)\n\t SPD IPv6 thresholds: local 128 remote 128\n"},
		{"negative: no XFRMA_SPD_INFO, its line unended", xtcpnl.XfrmInfo{}, 0, "\t SPD"},
		{"corner: -s -s with no XFRMA_SPD_INFO, the buckets after the bare SPD",
			xtcpnl.XfrmInfo{SPDHInfo: full.SPDHInfo}, 2, "\t SPD\t SPD buckets: count 7 Max 1048576\n"},
		{"corner: a count past int32 prints negative, as %d of a u32 does",
			xtcpnl.XfrmInfo{SPDInfo: &xtcpnl.XfrmSPDInfo{In: 0xffffffff}}, 0, "\t SPD IN  -1 OUT 0 FWD 0\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmSPDInfo(&tc.info, tc.stats); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestXfrmPolicyDefault is xfrm_policy_default_print: each direction's
// action by name, and unknown past accept.
//
// go test ./internal/goip/render/ -run TestXfrmPolicyDefault
func TestXfrmPolicyDefault(t *testing.T) {
	tests := []struct {
		description string
		d           xtcpnl.XfrmUserpolicyDefault
		expected    string
	}{
		{"positive: the kernel's default, accept everywhere", xtcpnl.XfrmUserpolicyDefault{In: 2, Fwd: 2, Out: 2},
			"Default policies:\n in:  accept\n fwd: accept\n out: accept\n"},
		{"positive: in and fwd blocked", xtcpnl.XfrmUserpolicyDefault{In: 1, Fwd: 1, Out: 2},
			"Default policies:\n in:  block\n fwd: block\n out: accept\n"},
		{"boundary: unspec", xtcpnl.XfrmUserpolicyDefault{},
			"Default policies:\n in:  unspec\n fwd: unspec\n out: unspec\n"},
		{"corner: past accept is unknown", xtcpnl.XfrmUserpolicyDefault{In: 3, Fwd: 255, Out: 0},
			"Default policies:\n in:  unknown\n fwd: unknown\n out: unspec\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmPolicyDefault(&tc.d); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
