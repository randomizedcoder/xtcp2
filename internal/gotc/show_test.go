package gotc

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The shows against scripted replies: the paths the corpus cannot reach
// (compat TCA_STATS, BASIC_HW, an unknown kind's options, dump errors, a
// batch's sticky filters, the second graph) and one row per lead the
// corpus pins, so a mutant that breaks one fails here by name too.
//
// go test ./internal/gotc/ -run TestGotcShow

func attr(t uint16, payload []byte) []byte {
	n, _ := cint.U16(4 + len(payload))
	b := binary.LittleEndian.AppendUint16(nil, n)
	b = binary.LittleEndian.AppendUint16(b, t)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }
func le64(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }
func cat(p ...[]byte) []byte {
	var b []byte
	for _, x := range p {
		b = append(b, x...)
	}
	return b
}
func str0(s string) []byte { return append([]byte(s), 0) }

// tcBody is a tcmsg and its attributes.
func tcBody(ifindex, handle, parent, info uint32, attrs ...[]byte) []byte {
	b := []byte{0, 0, 0, 0}
	for _, v := range []uint32{ifindex, handle, parent, info} {
		b = binary.LittleEndian.AppendUint32(b, v)
	}
	return cat(b, cat(attrs...))
}

func qmsg(body []byte) Msg { return Msg{Type: unix.RTM_NEWQDISC, Flags: unix.NLM_F_MULTI, Body: body} }
func cmsg(body []byte) Msg { return Msg{Type: unix.RTM_NEWTCLASS, Flags: unix.NLM_F_MULTI, Body: body} }

// linkMsg is an RTM_NEWLINK for ll_init_map or ll_link_get.
func linkMsg(index uint32, name string) Msg {
	b := make([]byte, 0, xtcpnl.IfInfomsgSizeCst)
	b = append(b, 0, 0, 1, 0) // family, pad, ARPHRD_ETHER
	b = binary.LittleEndian.AppendUint32(b, index)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0)
	return Msg{Type: unix.RTM_NEWLINK, Flags: unix.NLM_F_MULTI, Body: cat(b, attr(unix.IFLA_IFNAME, str0(name)))}
}

// links is ll_init_map's dump: d0 is ifindex 2.
func links() Reply { return rep(linkMsg(2, "d0"), done()) }

func kind(k string) []byte { return attr(xtcpnl.TcaKind, str0(k)) }

func fqCodelOpts() []byte {
	return attr(xtcpnl.TcaOptions|unix.NLA_F_NESTED, cat(
		attr(xtcpnl.TcaFqCodelTarget, le32(4999)), attr(xtcpnl.TcaFqCodelLimit, le32(10240)),
		attr(xtcpnl.TcaFqCodelInterval, le32(99999)), attr(xtcpnl.TcaFqCodelEcn, le32(1)),
		attr(xtcpnl.TcaFqCodelFlows, le32(1024)), attr(xtcpnl.TcaFqCodelQuantum, le32(1514)),
		attr(xtcpnl.TcaFqCodelMemoryLimit, le32(32<<20)), attr(xtcpnl.TcaFqCodelDropBatchSize, le32(64))))
}

func stats2(parts ...[]byte) []byte { return attr(xtcpnl.TcaStats2|unix.NLA_F_NESTED, cat(parts...)) }

func basic(bytes uint64, pkts uint32) []byte {
	return attr(xtcpnl.TcaStatsBasic, cat(le64(bytes), le32(pkts), le32(0)))
}

func queue(qlen, backlog, drops, requeues, overlimits uint32) []byte {
	return attr(xtcpnl.TcaStatsQueue, cat(le32(qlen), le32(backlog), le32(drops), le32(requeues), le32(overlimits)))
}

func ratespec(rate uint32) []byte {
	return cat([]byte{0, 1}, []byte{0, 0}, []byte{0xff, 0xff}, []byte{0, 0}, le32(rate))
}

// htbClass is an htb class 1:10 under 1:1 with the given quantum, and
// RATE64 when rate64 is non-zero.
func htbClass(quantum uint32, rate64 uint64) []byte {
	parms := cat(ratespec(125000), ratespec(250000), le32(1000000), le32(1000000), le32(quantum), le32(0), le32(2))
	opts := attr(xtcpnl.TcaHtbParms, parms)
	if rate64 != 0 {
		opts = cat(opts, attr(xtcpnl.TcaHtbRate64, le64(rate64)))
	}
	return tcBody(2, 0x10010, 0x10001, 0, kind("htb"), attr(xtcpnl.TcaOptions|unix.NLA_F_NESTED, opts))
}

func TestGotcShow(t *testing.T) {
	fqc := qmsg(tcBody(2, 0, tcHRoot, 2, kind("fq_codel"), fqCodelOpts()))
	tests := []struct {
		description string
		args        []string
		stdin       string
		replies     []Reply
		expected    result
	}{
		{
			description: "positive: the header line, then fq_codel's options",
			args:        []string{"qdisc", "show"},
			replies:     []Reply{links(), rep(fqc, done())},
			expected: result{stdout: "qdisc fq_codel 0: dev d0 root refcnt 2 limit 10240p flows 1024 quantum 1514 " +
				"target 5ms interval 100ms memory_limit 32Mb ecn drop_batch 64 \n"},
		},
		{
			description: "positive: -s's STATS2 lines, requeues twice in text (Q18)",
			args:        []string{"-s", "qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"),
				stats2(basic(10, 1), queue(0, 0, 0, 3, 0)))), done())},
			expected: result{stdout: "qdisc noqueue 0: root \n Sent 10 bytes 1 pkt (dropped 0, overlimits 0 requeues 3) \n" +
				" backlog 0b 0p requeues 3\n"},
		},
		{
			description: "positive: -j -s puts a qdisc's stats beside its kind (Q19)",
			args:        []string{"-j", "-s", "qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"),
				stats2(basic(10, 1)))), done())},
			expected: result{stdout: `[{"kind":"noqueue","handle":"0:","root":true,"options":{},"bytes":10,"packets":1}]` + "\n"},
		},
		{
			description: "positive: an htb class under -d at RATE64",
			args:        []string{"-d", "class", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(cmsg(htbClass(1500, 125000000000)), done())},
			expected: result{stdout: "class htb 1:10 parent 1:1 prio 2 quantum 1500 rate 1Tbit ceil 2Mbit linklayer ethernet " +
				"burst 125000000000b/1mpu 0b cburst 250000b/1mpu 0b level 0 \n"},
		},
		{
			description: "positive: compat TCA_STATS says pkts and dropped (Q17)",
			args:        []string{"-s", "qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"),
				attr(xtcpnl.TcaStats, cat(le64(5), le32(1), le32(2), le32(3), le32(1000), le32(4), le32(6), le32(7), le32(0))))), done())},
			expected: result{stdout: "qdisc noqueue 0: root \n Sent 5 bytes 1 pkts (dropped 2, overlimits 3) \n" +
				" rate 8Kbit 4pps backlog 7b 6p \n"},
		},
		{
			description: "positive: BASIC_HW prints software and hardware lines",
			args:        []string{"-s", "qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"),
				stats2(basic(100, 10), attr(xtcpnl.TcaStatsBasicHw, cat(le64(40), le32(4), le32(0)))))), done())},
			expected: result{stdout: "qdisc noqueue 0: root \n Sent 100 bytes 10 pkt\n Sent software 60 bytes 6 pkt\n" +
				" Sent hardware 40 bytes 4 pkt\n"},
		},
		{
			description: "negative: show dev nosuch",
			args:        []string{"qdisc", "show", "dev", "nosuch"},
			replies:     []Reply{rep(nlErr(unix.ENODEV))},
			expected:    result{stderr: "Cannot find device \"nosuch\"\n", rc: 1},
		},
		{
			description: "negative: show root root is invarg, exit 255",
			args:        []string{"qdisc", "show", "root", "root"},
			expected:    result{stderr: "Error: argument \"root\" is wrong: parent is already specified\n", rc: 255},
		},
		{
			description: "corner: cake with no options prints none",
			args:        []string{"qdisc", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("cake"))), done())},
			expected:    result{stdout: "qdisc cake 0: root \n"},
		},
		{
			description: "negative: an NLMSG_ERROR mid-dump is Dump terminated, rc 1 (Q26)",
			args:        []string{"qdisc", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(nlErr(unix.EINVAL))},
			expected:    result{stderr: "RTNETLINK answers: Invalid argument\nDump terminated\n", rc: 1},
		},
		{
			description: "negative: class show qdisc twice is duparg",
			args:        []string{"class", "show", "qdisc", "1:", "qdisc", "2:"},
			expected:    result{stderr: "Error: duplicate \"qdisc\": \"2:\" is the second value.\n", rc: 255},
		},
		{
			description: "boundary: no qdiscs under -j is an empty array",
			args:        []string{"-j", "qdisc", "show"},
			replies:     []Reply{links(), rep(done())},
			expected:    result{stdout: "[]\n"},
		},
		{
			description: "boundary: netem limit 3000000000 prints negative (Q7)",
			args:        []string{"qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("netem"),
				attr(xtcpnl.TcaOptions, cat(le32(0), le32(3000000000), le32(0), le32(0), le32(0), le32(0))))), done())},
			expected: result{stdout: "qdisc netem 0: root limit -1294967296\n"},
		},
		{
			description: "boundary: htb quantum 3000000000 under -d prints negative (Q11)",
			args:        []string{"-d", "class", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(cmsg(htbClass(3000000000, 0)), done())},
			expected: result{stdout: "class htb 1:10 parent 1:1 prio 2 quantum -1294967296 rate 1Mbit ceil 2Mbit linklayer ethernet " +
				"burst 125000b/1mpu 0b cburst 250000b/1mpu 0b level 0 \n"},
		},
		{
			description: "corner: the dev key goes when dev filters (Q20)",
			args:        []string{"-j", "qdisc", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"))), done())},
			expected:    result{stdout: `[{"kind":"noqueue","handle":"0:","root":true,"options":{}}]` + "\n"},
		},
		{
			description: "corner: a batch's second line keeps the first's dev filter (Q29)",
			args:        []string{"-batch", "-"},
			stdin:       "qdisc show dev d0\nqdisc show\n",
			replies: []Reply{links(), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"))), done()),
				rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("noqueue"))), qmsg(tcBody(3, 0, tcHRoot, 1, kind("noqueue"))), done())},
			expected: result{stdout: "qdisc noqueue 0: root \nqdisc noqueue 0: root \n"},
		},
		{
			description: "corner: -j -g prints [] then the graph's text (Q22)",
			args:        []string{"-j", "-g", "class", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(cmsg(tcBody(2, 0x10001, tcHRoot, 0, kind("htb"))), done())},
			expected:    result{stdout: "[]\n+---(1:1) htb \n\n"},
		},
		{
			description: "corner: -j -r's htb buffer words are bare text in the JSON (Q12)",
			args:        []string{"-j", "-r", "class", "show", "dev", "d0"},
			replies:     []Reply{rep(linkMsg(2, "d0")), rep(cmsg(htbClass(1500, 0)), done())},
			expected: result{stdout: `[{"class":"htb","handle":"1:10","parent":"1:1","prio":2,"rate":125000,"ceil":250000,` +
				`"burst":125000,"cburst":250000buffer [000f4240] cbuffer [000f4240] }]` + "\n"},
		},
		{
			description: "corner: an unknown kind's options are print_noqopt's bare text (Q28)",
			args:        []string{"qdisc", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(qmsg(tcBody(2, 0, tcHRoot, 1, kind("mq"),
				attr(xtcpnl.TcaOptions, le32(1)), attr(xtcpnl.TcaExtWarnMsg, str0("careful")))), done())},
			expected: result{stdout: "qdisc mq 0: root [Unknown qdisc, optlen=4] \ncareful\n"},
		},
		{
			description: "corner: a second -g line in a batch aborts, its buffer lost (Q34)",
			args:        []string{"-g", "-batch", "-"},
			stdin:       "class show dev d0\nclass show dev d0\n",
			replies: []Reply{links(), rep(cmsg(tcBody(2, 0x10001, tcHRoot, 0, kind("htb"))), done()),
				rep(cmsg(tcBody(2, 0x10001, tcHRoot, 0, kind("htb"))), done())},
			expected: result{stderr: "free(): double free detected in tcache 2\n", rc: 134},
		},
		{
			description: "corner: sfq class xstats print allot",
			args:        []string{"-s", "class", "show", "dev", "d0"},
			replies: []Reply{rep(linkMsg(2, "d0")), rep(cmsg(tcBody(2, 0x10001, 0x10000, 0, kind("sfq"),
				stats2(basic(0, 0), attr(xtcpnl.TcaStatsApp, le32(0xfffffffe))))), done())},
			expected: result{stdout: "class sfq 1:1 parent 1: \n Sent 0 bytes 0 pkt\n  allot -2\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(tc.stdin), sys: emptySys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}
