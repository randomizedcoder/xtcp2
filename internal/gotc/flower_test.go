package gotc

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func be16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func be64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

// geneveOptB is one GENEVE option: CLASS, TYPE, DATA.
func geneveOptB(class uint16, typ uint8, data []byte) []byte {
	return cat(attr(xtcpnl.TcaFlowerKeyEncOptGeneveClass, be16(class)),
		attr(xtcpnl.TcaFlowerKeyEncOptGeneveType, []byte{typ}),
		attr(xtcpnl.TcaFlowerKeyEncOptGeneveData, data))
}

// encOptsB is a key and mask pair of TCA_FLOWER_KEY_ENC_OPTS nests, each
// holding one kind's nest.
func encOptsB(kind uint16, key, mask []byte) []byte {
	return cat(nest(xtcpnl.TcaFlowerKeyEncOpts, nest(kind, key)),
		nest(xtcpnl.TcaFlowerKeyEncOptsMask, nest(kind, mask)))
}

// TestGotcFlowerUnit runs flower_print_opt and flower_parse_opt's get on
// scripted replies: the leads (FL*) on crafted options the kernel would
// not send, and the forms the corpus already pins, each in isolation.
//
// go test ./internal/gotc/ -run TestGotcFlowerUnit
func TestGotcFlowerUnit(t *testing.T) {
	d0 := rep(linkMsg(2, "d0"))
	fl := func(proto uint16, opts ...[]byte) Reply {
		return rep(fmsg(tcBody(2, 1, tcHClsactIngress, finfo(1, proto),
			kind("flower"), attr(xtcpnl.TcaChain, le32(0)), nest(xtcpnl.TcaOptions, cat(opts...)))), done())
	}
	ip := attr(xtcpnl.TcaFlowerKeyEthType, be16(ethPIP))
	ip6 := attr(xtcpnl.TcaFlowerKeyEthType, be16(ethPIPv6))
	tcp := attr(xtcpnl.TcaFlowerKeyIPProto, []byte{ipprotoTCP})
	show := []string{"filter", "show", "dev", "d0", "ingress"}
	showJ := []string{"-j", "filter", "show", "dev", "d0", "ingress"}
	head := "filter protocol ip pref 1 flower chain 0 handle 0x1 "
	head6 := "filter protocol ipv6 pref 1 flower chain 0 handle 0x1 "
	headJ := `[{"protocol":"ip","pref":1,"kind":"flower","chain":0,"options":{"handle":1,"keys":{`
	get := func(more ...string) []string {
		return append([]string{"filter", "get", "dev", "d0", "ingress", "protocol", "ip", "pref", "1"}, more...)
	}
	usage := flowerUsage

	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    result
	}{
		// Positive.
		{
			description: "positive: L3/L4 text",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, ip, tcp, attr(xtcpnl.TcaFlowerKeyIPv4Dst, []byte{10, 0, 0, 1}),
				attr(xtcpnl.TcaFlowerKeyIPv4DstMask, []byte{255, 255, 255, 255}), attr(xtcpnl.TcaFlowerKeyTCPDst, be16(80)))},
			expected: result{stdout: head + "\n  eth_type ipv4\n  ip_proto tcp\n  dst_ip 10.0.0.1\n  dst_port 80\n"},
		},
		{
			description: "positive: a masked MAC prints its prefix length",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyEthDst, []byte{0xaa, 0xbb, 0xcc, 0, 0, 0}),
				attr(xtcpnl.TcaFlowerKeyEthDstMask, []byte{0xff, 0xff, 0xff, 0, 0, 0}))},
			expected: result{stdout: head + "\n  dst_mac aa:bb:cc:00:00:00/24\n"},
		},
		{
			description: "positive: two geneve options, key/mask pairs joined by commas",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsGeneve,
				cat(geneveOptB(0x0102, 0x80, []byte{0x11, 0x22, 0x33, 0x44}), geneveOptB(0x0103, 0x81, []byte{1, 2, 3, 4})),
				cat(geneveOptB(0xffff, 0xff, []byte{0xff, 0xff, 0xff, 0xff}), geneveOptB(0x0fff, 0xff, []byte{0xff, 0, 0xff, 0}))))},
			expected: result{stdout: head + "\n  geneve_opts 0102:80:11223344/ffff:ff:ffffffff,0103:81:01020304/0fff:ff:ff00ff00\n"},
		},
		{
			description: "positive: geneve in JSON: class, type and the data bytes; no text form",
			args:        showJ,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsGeneve,
				geneveOptB(0x0102, 0x80, []byte{0x11, 0x22, 0x33, 0x44}), geneveOptB(0xffff, 0xff, []byte{0xff, 0xff, 0xff, 0xff})))},
			expected: result{stdout: headJ + `"geneve_opt_key":[{"class":258,"type":128,"data":[17,34,51,68]}],` +
				`"geneve_opt_mask":[{"class":65535,"type":255,"data":[255,255,255,255]}]}}}]` + "\n"},
		},
		{
			description: "positive: the -br terse dump: flags, then the terse gact",
			args:        []string{"-br", "filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerFlags, le32(xtcpnl.TcaClsFlagsNotInHw)),
				nest(xtcpnl.TcaFlowerAct, nest(1, attr(xtcpnl.TcaActKind, str0("gact")))))},
			expected: result{stdout: head + "\n  not_in_hw\n\taction order 1: gact \n"},
		},
		{
			description: "positive: ip_flags, each mask bit as name or noname",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyFlags, be32(xtcpnl.TcaFlowerKeyFlagsIsFragment)),
				attr(xtcpnl.TcaFlowerKeyFlagsMask, be32(xtcpnl.TcaFlowerKeyFlagsIsFragment|xtcpnl.TcaFlowerKeyFlagsFragIsFirst)))},
			expected: result{stdout: head + "\n  ip_flags frag/nofirstfrag\n"},
		},
		{
			description: "positive: ct_state in tc's table order",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyCtState, le16(xtcpnl.TcaFlowerKeyCtFlagsTracked|xtcpnl.TcaFlowerKeyCtFlagsEstablished)),
				attr(xtcpnl.TcaFlowerKeyCtStateMask, le16(xtcpnl.TcaFlowerKeyCtFlagsTracked|xtcpnl.TcaFlowerKeyCtFlagsEstablished|xtcpnl.TcaFlowerKeyCtFlagsNew)))},
			expected: result{stdout: head + "\n  ct_state +trk-new+est\n"},
		},
		{
			description: "positive: a get sends FLAGS 0 and ETH_TYPE and prints the reply",
			args:        get("handle", "1", "flower"),
			replies:     []Reply{d0, rep(fmsg(tcBody(2, 1, tcHClsactIngress, finfo(1, ethPIP), kind("flower"), attr(xtcpnl.TcaChain, le32(0)), nest(xtcpnl.TcaOptions, ip))))},
			expected:    result{stdout: "filter ingress protocol ip pref 1 flower chain 0 handle 0x1 \n  eth_type ipv4\n"},
		},
		// Negative.
		{
			description: "negative: get's handle is get_u32, so 1junk is illegal (FL16)",
			args:        get("handle", "1junk", "flower"),
			replies:     []Reply{d0},
			expected:    result{stderr: "Illegal \"handle\"\n", rc: 1},
		},
		{
			description: "negative: a match key on a get is refused by name",
			args:        get("handle", "1", "flower", "dst_ip", "1.2.3.4"),
			replies:     []Reply{d0},
			expected:    result{stderr: notImpl + "flower dst_ip in a get\n", rc: 255},
		},
		{
			description: "negative: a key prefix is the key it matches",
			args:        get("handle", "1", "flower", "enc_k", "1"),
			replies:     []Reply{d0},
			expected:    result{stderr: notImpl + "flower enc_key_id in a get\n", rc: 255},
		},
		{
			description: "negative: strict keywords need the whole word",
			args:        get("handle", "1", "flower", "sp"),
			replies:     []Reply{d0},
			expected:    result{stderr: "What is \"sp\"?\n" + usage, rc: 1},
		},
		{
			description: "negative: help is explain() alone",
			args:        get("handle", "1", "flower", "help"),
			replies:     []Reply{d0},
			expected:    result{stderr: usage, rc: 1},
		},
		{
			description: "corner: a pedit with no options in a flower prints its kind alone",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, nest(xtcpnl.TcaFlowerAct, nest(1, attr(xtcpnl.TcaActKind, str0("pedit")))))},
			expected:    result{stdout: head + "\n\taction order 1:  pedit \n"},
		},
		{
			description: "negative: a truncated geneve option is refused where C reads NULL (FL10)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsGeneve,
				attr(xtcpnl.TcaFlowerKeyEncOptGeneveClass, be16(1)), geneveOptB(1, 1, le32(1))))},
			expected: result{stdout: head, stderr: notImpl + "flower xtcpnl: flower option attribute missing: geneve type (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: a mask nest without the key's kind is malloc garbage in C (FL11)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, nest(xtcpnl.TcaFlowerKeyEncOpts, nest(xtcpnl.TcaFlowerKeyEncOptsVxlan, attr(1, le32(5)))),
				nest(xtcpnl.TcaFlowerKeyEncOptsMask, nest(xtcpnl.TcaFlowerKeyEncOptsGeneve, geneveOptB(1, 1, le32(1)))))},
			expected: result{stdout: head, stderr: notImpl + "flower vxlan opts mask (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: ct_label without its mask (FL12)",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyCtLabels, make([]byte, 16)))},
			expected:    result{stdout: head, stderr: notImpl + "flower ct_label mask (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: a flags mask without its key (FL14)",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyFlagsMask, be32(1)))},
			expected:    result{stdout: head, stderr: notImpl + "flower ip_flags key (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: an LSE without NLA_F_NESTED: C's message says expecting 0x0 (FL8)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, nest(xtcpnl.TcaFlowerKeyMplsOpts, attr(xtcpnl.TcaFlowerKeyMplsOptsLse,
				attr(xtcpnl.TcaFlowerKeyMplsOptLseDepth, []byte{1}))))},
			expected: result{stdout: head + "\n  mpls\n", stderr: "rta_type 0x1, expecting 0x0 (0x1 & 0x8000)\n"},
		},
		// Boundary.
		{
			description: "boundary: a pfcp seid past 2^32 keeps its low 32 bits in JSON (FL2)",
			args:        showJ,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsPfcp,
				cat(attr(1, []byte{1}), attr(2, be64(1<<32+1))), cat(attr(1, []byte{0xff}), attr(2, be64(^uint64(0))))))},
			expected: result{stdout: headJ + `"pfcp_opt_key":[{"type":1,"seid":1}],"pfcp_opt_mask":[{"type":255,"seid":4294967295}]}}}]` + "\n"},
		},
		{
			description: "boundary: pfcp text is %02x:%llx, whole (FL18)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsPfcp,
				cat(attr(1, []byte{1}), attr(2, be64(1<<32+1))), cat(attr(1, []byte{0xff}), attr(2, be64(^uint64(0))))))},
			expected: result{stdout: head + "\n  pfcp_opts 01:100000001/ff:ffffffffffffffff\n"},
		},
		{
			description: "boundary: a non-contiguous IPv4 mask prints as an address",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, ip, attr(xtcpnl.TcaFlowerKeyIPv4Src, []byte{10, 0, 1, 0}),
				attr(xtcpnl.TcaFlowerKeyIPv4SrcMask, []byte{255, 0, 255, 0}))},
			expected: result{stdout: head + "\n  eth_type ipv4\n  src_ip 10.0.1.0/255.0.255.0\n"},
		},
		{
			description: "boundary: classid 1:ffe0 prints hw_tc 0, the major dropped (FL3)",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerClassid, le32(0x1ffe0)))},
			expected:    result{stdout: head + "hw_tc 0 \n"},
		},
		{
			description: "boundary: classid 1:ffef is the last hw_tc",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerClassid, le32(0x1ffef)))},
			expected:    result{stdout: head + "hw_tc 15 \n"},
		},
		{
			description: "boundary: classid 1:fff0 is a classid again",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerClassid, le32(0x1fff0)))},
			expected:    result{stdout: head + "classid 1:fff0 \n"},
		},
		{
			description: "boundary: the largest handle",
			args:        get("handle", "0xffffffff", "flower"),
			replies:     []Reply{d0, rep(fmsg(tcBody(2, 0xffffffff, tcHClsactIngress, finfo(1, ethPIP), kind("flower"), attr(xtcpnl.TcaChain, le32(0)), nest(xtcpnl.TcaOptions))))},
			expected:    result{stdout: "filter ingress protocol ip pref 1 flower chain 0 handle 0xffffffff \n"},
		},
		// Corner.
		{
			description: "corner: gtp prints no JSON at all (FL1)",
			args:        showJ,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsGtp,
				cat(attr(1, []byte{1}), attr(2, []byte{9})), cat(attr(1, []byte{0xff}), attr(2, []byte{0x3f}))))},
			expected: result{stdout: headJ + `}}}]` + "\n"},
		},
		{
			description: "corner: a port range is an object under the port's key in JSON (FL6)",
			args:        showJ,
			replies: []Reply{d0, fl(ethPIP, tcp, attr(xtcpnl.TcaFlowerKeyPortDstMin, be16(10)),
				attr(xtcpnl.TcaFlowerKeyPortDstMax, be16(20)))},
			expected: result{stdout: headJ + `"ip_proto":"tcp","dst_port":{"start":10,"end":20}}}}]` + "\n"},
		},
		{
			description: "corner: a port range in text",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, tcp, attr(xtcpnl.TcaFlowerKeyPortSrcMin, be16(10)),
				attr(xtcpnl.TcaFlowerKeyPortSrcMax, be16(20)))},
			expected: result{stdout: head + "\n  ip_proto tcp\n  src_port 10-20\n"},
		},
		{
			description: "corner: tcp_flags' mask has no 0x (FL4)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyTCPFlags, be16(2)),
				attr(xtcpnl.TcaFlowerKeyTCPFlagsMask, be16(0x12)))},
			expected: result{stdout: head + "\n  tcp_flags 0x2/12\n"},
		},
		{
			description: "corner: an ICMP mask is decimal (FL5)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, ip, attr(xtcpnl.TcaFlowerKeyIPProto, []byte{ipprotoICMP}),
				attr(xtcpnl.TcaFlowerKeyICMPv4Type, []byte{8}), attr(xtcpnl.TcaFlowerKeyICMPv4TypeMsk, []byte{15}))},
			expected: result{stdout: head + "\n  eth_type ipv4\n  ip_proto icmp\n  icmp_type 8/15\n"},
		},
		{
			description: "corner: an unnamed eth_type is %04x with no 0x (FL7)",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyEthType, be16(0x88b5)))},
			expected:    result{stdout: head + "\n  eth_type 88b5\n"},
		},
		{
			description: "corner: IPv6 in glibc's inet_ntop forms",
			args:        show,
			replies: []Reply{d0, fl(ethPIPv6, ip6,
				attr(xtcpnl.TcaFlowerKeyIPv6Dst, append(make([]byte, 10), 0xff, 0xff, 1, 2, 3, 4)),
				attr(xtcpnl.TcaFlowerKeyIPv6DstMask, bytes.Repeat([]byte{0xff}, 16)),
				attr(xtcpnl.TcaFlowerKeyIPv6Src, append(make([]byte, 12), 0, 2, 0, 3)),
				attr(xtcpnl.TcaFlowerKeyIPv6SrcMask, append(bytes.Repeat([]byte{0xff}, 8), make([]byte, 8)...)))},
			expected: result{stdout: head6 + "\n  eth_type ipv6\n  dst_ip ::ffff:1.2.3.4\n  src_ip ::0.2.0.3/64\n"},
		},
		{
			description: "corner: in_hw_count follows in_hw after one space (FL19)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerFlags, le32(xtcpnl.TcaClsFlagsInHw)),
				attr(xtcpnl.TcaFlowerInHwCount, le32(2)))},
			expected: result{stdout: head + "\n  in_hw in_hw_count 2\n"},
		},
		{
			description: "corner: an unnamed ppp_proto, and an MPLS LSE deficit on stderr (FL9)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyPppProto, be16(0x1234)),
				nest(xtcpnl.TcaFlowerKeyMplsOpts, nest(xtcpnl.TcaFlowerKeyMplsOptsLse, attr(xtcpnl.TcaFlowerKeyMplsOptLseLabel, le32(100))), []byte{8, 0}))},
			expected: result{stdout: head + "\n  mpls\n    lse label 100\n  ppp_proto [4660]\n", stderr: "!!!Deficit 2, rta_len=8\n"},
		},
		{
			description: "corner: an MPLS_OPTS nest without NLA_F_NESTED prints nothing",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyMplsOpts, nest(xtcpnl.TcaFlowerKeyMplsOptsLse,
				attr(xtcpnl.TcaFlowerKeyMplsOptLseLabel, le32(100)))))},
			expected: result{stdout: head + "\n"},
		},
		{
			description: "corner: arp_op names reply only under a full mask; rarp's name",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyEthType, be16(ethPRARP)),
				attr(xtcpnl.TcaFlowerKeyArpOp, []byte{arpopReply}), attr(xtcpnl.TcaFlowerKeyArpOpMask, []byte{0xff}),
				attr(xtcpnl.TcaFlowerKeyArpSha, []byte{1, 2, 3, 4, 5, 6}), attr(xtcpnl.TcaFlowerKeyArpShaMask, []byte{0xff, 0, 0xff, 0, 0, 0}))},
			expected: result{stdout: head + "\n  eth_type rarp\n  arp_op reply\n  arp_sha 01:02:03:04:05:06/ff:00:ff:00:00:00\n"},
		},
		{
			description: "corner: a masked arp_op is a number",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyArpOp, []byte{arpopRequest}),
				attr(xtcpnl.TcaFlowerKeyArpOpMask, []byte{0x0f}))},
			expected: result{stdout: head + "\n  arp_op 1/15\n"},
		},
		{
			description: "negative: enc_opts with no mask nest: C parses NULL",
			args:        show,
			replies:     []Reply{d0, fl(ethPIP, nest(xtcpnl.TcaFlowerKeyEncOpts, nest(xtcpnl.TcaFlowerKeyEncOptsVxlan, attr(1, le32(5)))))},
			expected:    result{stdout: head, stderr: notImpl + "flower enc_opts mask (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: ct_state whose mask has no bit tc knows (FL13)",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, attr(xtcpnl.TcaFlowerKeyCtState, le16(0)),
				attr(xtcpnl.TcaFlowerKeyCtStateMask, le16(0x8000)))},
			expected: result{stdout: head, stderr: notImpl + "flower ct_state with no known mask bit (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "corner: erspan v2 text: ver:index:dir:hwid",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, encOptsB(xtcpnl.TcaFlowerKeyEncOptsErspan,
				cat(attr(1, []byte{2}), attr(3, []byte{1}), attr(4, []byte{7})), cat(attr(1, []byte{0xff}), attr(3, []byte{1}), attr(4, []byte{0x3f}))))},
			expected: result{stdout: head + "\n  erspan_opts 2:0:1:7/255:0:1:63\n"},
		},
		{
			description: "corner: the mpls and cfm text forms",
			args:        show,
			replies: []Reply{d0, fl(ethPIP, nest(xtcpnl.TcaFlowerKeyMplsOpts, nest(xtcpnl.TcaFlowerKeyMplsOptsLse,
				attr(xtcpnl.TcaFlowerKeyMplsOptLseDepth, []byte{1}), attr(xtcpnl.TcaFlowerKeyMplsOptLseLabel, le32(100)),
				attr(xtcpnl.TcaFlowerKeyMplsOptLseTTL, []byte{64}))),
				nest(xtcpnl.TcaFlowerKeyCfm, attr(xtcpnl.TcaFlowerKeyCfmMdLevel, []byte{3}), attr(xtcpnl.TcaFlowerKeyCfmOpcode, []byte{1})))},
			expected: result{stdout: head + "\n  mpls\n    lse depth 1 label 100 ttl 64\n  cfm mdl 3 op 1\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			rc := run(config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestGotcFlowerGetRequest holds the get's TCA_OPTIONS to what tc sends:
// FLAGS 0, and ETH_TYPE unless the protocol is all (FL17).
//
// go test ./internal/gotc/ -run TestGotcFlowerGetRequest
func TestGotcFlowerGetRequest(t *testing.T) {
	tests := []struct {
		description string
		proto       string
		expected    string // the request's tail, hex
	}{
		{"positive: protocol ip adds ETH_TYPE", "ip", "1400020008001600000000000600080008000000"},
		{"corner: protocol all sends FLAGS alone", "all", "0c0002000800160000000000"},
		{"boundary: an unnamed protocol", "0x88b5", "14000200080016000000000006000800" + "88b50000"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: []Reply{rep(linkMsg(2, "d0")), rep(nlErr(unix.ENOENT))}}
			var out, errb bytes.Buffer
			run(config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)},
				[]string{"filter", "get", "dev", "d0", "ingress", "protocol", tc.proto, "pref", "1", "handle", "1", "flower"}, &out, &errb)
			last := src.reqs[len(src.reqs)-1]
			want := hexDecode(t, tc.expected)
			if !bytes.HasSuffix(last, want) {
				t.Errorf("request % x does not end in % x", last, want)
			}
		})
	}
}

// TestInetNtop6 holds inetNtop6 to glibc's inet_ntop.
//
// go test ./internal/gotc/ -run TestInetNtop6
func TestInetNtop6(t *testing.T) {
	w := func(words ...uint16) []byte {
		b := make([]byte, 0, 16)
		for _, x := range words {
			b = binary.BigEndian.AppendUint16(b, x)
		}
		return b
	}
	tests := []struct {
		description string
		addr        []byte
		expected    string
	}{
		{"positive: a documentation prefix", w(0x2001, 0xdb8, 0, 0, 0, 0, 0, 1), "2001:db8::1"},
		{"positive: trailing zeros", w(0x2001, 0xdb8, 0, 0, 0, 0, 0, 0), "2001:db8::"},
		{"boundary: all zeros", w(0, 0, 0, 0, 0, 0, 0, 0), "::"},
		{"boundary: all ones", w(0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff, 0xffff), "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"},
		{"boundary: loopback", w(0, 0, 0, 0, 0, 0, 0, 1), "::1"},
		{"corner: IPv4-mapped", w(0, 0, 0, 0, 0, 0xffff, 0x0102, 0x0304), "::ffff:1.2.3.4"},
		{"corner: IPv4-compatible, where netip prints ::2:3", w(0, 0, 0, 0, 0, 0, 2, 3), "::0.2.0.3"},
		{"corner: a single zero word is not compressed", w(1, 0, 2, 3, 4, 5, 6, 7), "1:0:2:3:4:5:6:7"},
		{"corner: the first of two equal runs", w(1, 0, 0, 2, 3, 0, 0, 4), "1::2:3:0:0:4"},
		{"corner: the longer later run", w(1, 0, 0, 2, 0, 0, 0, 3), "1:0:0:2::3"},
		{"negative: ::ffff with a nonzero word 4 is not mapped", w(0, 0, 0, 0, 1, 0xffff, 0x0102, 0x0304), "::1:ffff:102:304"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := inetNtop6(tc.addr); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestMaskBits holds maskBits to f_flower.c __mask_bits.
//
// go test ./internal/gotc/ -run TestMaskBits
func TestMaskBits(t *testing.T) {
	tests := []struct {
		description string
		mask        []byte
		expected    int
	}{
		{"positive: /24", []byte{255, 255, 255, 0}, 24},
		{"boundary: all ones", []byte{255, 255, 255, 255}, 32},
		{"boundary: /1", []byte{0x80, 0, 0, 0}, 1},
		{"negative: a hole", []byte{255, 0, 255, 0}, -1},
		{"corner: all zeros is -1, not 0", []byte{0, 0, 0, 0}, -1},
		{"corner: a clear first bit is -1", []byte{0x7f, 0xff, 0xff, 0xff}, -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := maskBits(tc.mask); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

// hexDecode is hex.DecodeString, failing the test on a bad string.
func hexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
