package gotc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/wsreplay"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// actMsg is an RTM_GETACTION reply (a dump's and a get's carry the
// request's type, K5): a zero tcamsg, TCA_ROOT_COUNT when count >= 0, and
// TCA_ACT_TAB holding the order nests.
func actMsg(count int64, orders ...[]byte) Msg {
	body := make([]byte, xtcpnl.TcaMsgSizeCst)
	if count >= 0 {
		body = cat(body, attr(xtcpnl.TcaRootCount, le32(uint32(count))))
	}
	return Msg{Type: unix.RTM_GETACTION, Flags: unix.NLM_F_MULTI,
		Body: cat(body, attr(xtcpnl.TcaRootTab, cat(orders...)))}
}

// actOrder is one order nest: TCA_ACT_KIND, the options nest and extra
// attributes (flags, in_hw_count).
func actOrder(order uint16, k string, opts []byte, extra ...[]byte) []byte {
	parts := [][]byte{attr(xtcpnl.TcaActKind, str0(k))}
	if opts != nil {
		parts = append(parts, nest(xtcpnl.TcaActOptions, opts))
	}
	return nest(order, append(parts, extra...)...)
}

// genB is a tc_gen: index, capab, action, refcnt, bindcnt.
func genB(index, action uint32, ref, bind uint32) []byte {
	return cat(le32(index), le32(0), le32(action), le32(ref), le32(bind))
}

// policeTbf is a struct tc_police with a rate of rate bytes/s, burst ticks
// and mtu.
func policeTbf(index, action, burst, mtu, rate, ref, bind uint32) []byte {
	rs := func(r uint32) []byte { return cat(make([]byte, 8), le32(r)) }
	return cat(le32(index), le32(action), le32(0), le32(burst), le32(mtu), rs(rate), rs(0),
		le32(ref), le32(bind), le32(0))
}

const (
	gactLine     = "gact action pass\n\t random type none pass val 0\n\t index 1 ref 1 bind 1\n"
	tcActOKCst   = 0
	tcActDropCst = 2
)

// TestGotcActionUnit runs do_action's list and get, print_action and the
// kinds' printers on scripted replies.
//
// go test ./internal/gotc/ -run TestGotcActionUnit
func TestGotcActionUnit(t *testing.T) {
	gact := func(order uint16, action uint32) []byte {
		return actOrder(order, "gact", attr(xtcpnl.TcaGactParms, genB(1, action, 1, 1)))
	}
	police := func(order uint16, extra ...[]byte) []byte {
		opts := cat(append([][]byte{attr(xtcpnl.TcaPoliceTbf, policeTbf(1, 1, 0, 2048, 125000, 2, 1))}, extra...)...)
		return actOrder(order, "police", opts)
	}
	skipHw := attr(xtcpnl.TcaActFlags, cat(le32(xtcpnl.TcaActFlagsSkipHw), le32(xtcpnl.TcaActFlagsSkipHw)))
	noPercpu := attr(xtcpnl.TcaActFlags, cat(le32(xtcpnl.TcaActFlagsNoPercpuStats), le32(xtcpnl.TcaActFlagsNoPercpuStats)))
	inHw := attr(xtcpnl.TcaActInHwCount, le32(0))
	d0 := rep(linkMsg(2, "d0"))
	matchall := func(act ...[]byte) Msg {
		return fmsg(tcBody(2, 1, tcHClsactIngress, finfo(3, 0x0003), kind("matchall"),
			attr(xtcpnl.TcaChain, le32(0)), nest(xtcpnl.TcaOptions, nest(xtcpnl.TcaMatchallAct, act...))))
	}
	u32Police := fmsg(tcBody(2, 0x800, tcHClsactIngress, finfo(1, ethPIP), kind("u32"), attr(xtcpnl.TcaChain, le32(0)),
		nest(xtcpnl.TcaOptions, attr(xtcpnl.TcaU32Hash, le32(0x80000000)), nest(xtcpnl.TcaU32Police, attr(xtcpnl.TcaPoliceTbf,
			policeTbf(2, tcActDropCst, 0, 2048, 125000, 1, 1))))))
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    result
	}{
		// Positive.
		{
			description: "positive: a list numbers a dump's actions from 0 (AC12)",
			args:        []string{"actions", "ls", "action", "gact"},
			replies:     []Reply{rep(actMsg(2, gact(0, tcActOKCst), gact(1, tcActOKCst)), done())},
			expected: result{stdout: "total acts 2\n\n\taction order 0: " + gactLine +
				"\n\taction order 1: " + gactLine},
		},
		{
			description: "positive: a get numbers from 1 and prints total acts 0 (AC2)",
			args:        []string{"actions", "get", "action", "gact", "index", "1"},
			replies:     []Reply{rep(actMsg(-1, gact(1, tcActOKCst)))},
			expected:    result{stdout: "total acts 0\n\n\taction order 1: " + gactLine},
		},
		{
			description: "positive: police's text line",
			args:        []string{"actions", "ls", "action", "police"},
			replies:     []Reply{rep(actMsg(1, police(0)), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  police 0x1 rate 1Mbit burst 0b mtu 2Kb " +
				"action reclassify overhead 0b \n\tref 2 bind 1 \n"},
		},
		{
			description: "positive: police inside a u32 filter, refused before",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(u32Police, done())},
			expected: result{stdout: "filter protocol ip pref 1 u32 chain 0 fh :800 order 2048 key ht 800 bkt 0 \n police 0x2 rate 1Mbit burst 0b " +
				"mtu 2Kb action drop overhead 0b \n\tref 1 bind 1 \n\n"},
		},
		{
			description: "positive: unbound (tc actions), skip_hw and not_in_hw print",
			args:        []string{"actions", "ls", "action", "gact"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "gact", attr(xtcpnl.TcaGactParms, genB(1, 0, 1, 1)),
				skipHw, inHw)), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: " + gactLine + "\tskip_hw\n"},
		},
		{
			description: "positive: unbound, in_hw_count 0 is not_in_hw",
			args:        []string{"actions", "ls", "action", "gact"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "gact", attr(xtcpnl.TcaGactParms, genB(1, 0, 1, 1)),
				noPercpu, inHw)), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: " + gactLine + "\tno_percpu\tnot_in_hw\n"},
		},
		{
			description: "positive: bound (a filter's), the same nest prints neither",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, rep(matchall(actOrder(1, "gact", attr(xtcpnl.TcaGactParms, genB(1, 0, 1, 1)),
				skipHw, inHw)), done())},
			expected: result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n" +
				"\taction order 1: " + gactLine + "\n"},
		},
		{
			description: "positive: mirred to a device",
			args:        []string{"actions", "get", "action", "mirred", "index", "1"},
			replies: []Reply{rep(actMsg(-1, actOrder(1, "mirred", attr(xtcpnl.TcaMirredParms,
				cat(genB(1, 4, 1, 0), le32(xtcpnl.TcaEgressRedirCst), le32(2)))))), d0},
			expected: result{stdout: "total acts 0\n\n\taction order 1: mirred (Egress Redirect to device d0) stolen\n" +
				"\tindex 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: skbedit's fields and inheritdsfield",
			args:        []string{"actions", "ls", "action", "skbedit"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "skbedit", cat(attr(xtcpnl.TcaSkbeditParms, genB(3, 3, 1, 0)),
				attr(xtcpnl.TcaSkbeditQueueMapping, le16(2)), attr(xtcpnl.TcaSkbeditPriority, le32(0x10002)),
				attr(xtcpnl.TcaSkbeditMark, le32(7)), attr(xtcpnl.TcaSkbeditMask, le32(0xff)),
				attr(xtcpnl.TcaSkbeditPtype, le16(9)),
				attr(xtcpnl.TcaSkbeditFlags, le64(xtcpnl.SkbeditFInheritDsfield))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: skbedit queue_mapping 2 priority 1:2 mark 7/0xff " +
				"ptype 9 inheritdsfield pipe\n\t index 3 ref 1 bind 0\n"},
		},
		{
			description: "positive: vlan push_eth prints both MACs",
			args:        []string{"actions", "ls", "action", "vlan"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "vlan", cat(attr(xtcpnl.TcaVlanParms, cat(genB(4, 3, 1, 0), le32(5))),
				attr(xtcpnl.TcaVlanPushEthDst, []byte{2, 0, 0, 0, 0, 1}),
				attr(xtcpnl.TcaVlanPushEthSrc, []byte{2, 0, 0, 0, 0, 2})))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: vlan  push_eth dst_mac 02:00:00:00:00:01 " +
				"src_mac 02:00:00:00:00:02 pipe\n\t index 4 ref 1 bind 0\n"},
		},
		{
			description: "positive: mpls push prints protocol, label, tc, bos and ttl",
			args:        []string{"actions", "ls", "action", "mpls"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "mpls", cat(attr(xtcpnl.TcaMplsParms, cat(genB(1, 3, 1, 0), le32(2))),
				attr(xtcpnl.TcaMplsProto, be16(0x8847)), attr(xtcpnl.TcaMplsLabel, le32(20)),
				attr(xtcpnl.TcaMplsTC, []byte{3}), attr(xtcpnl.TcaMplsBos, []byte{1}),
				attr(xtcpnl.TcaMplsTTL, []byte{64})))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: mpls  push protocol mpls_uc label 20 tc 3 " +
				"bos 1 ttl 64 pipe\n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: connmark, sample and simple",
			args:        []string{"actions", "ls", "action", "sample"},
			replies: []Reply{rep(actMsg(3,
				actOrder(0, "connmark", attr(xtcpnl.TcaConnmarkParms, cat(genB(1, 3, 1, 0), le16(5), le16(0)))),
				actOrder(1, "sample", cat(attr(xtcpnl.TcaSampleParms, genB(2, 3, 1, 0)),
					attr(xtcpnl.TcaSampleRate, le32(10)), attr(xtcpnl.TcaSamplePsampleGroup, le32(5)),
					attr(xtcpnl.TcaSampleTruncSize, le32(64)))),
				actOrder(2, "simple", cat(attr(xtcpnl.TcaDefParms, genB(3, 3, 1, 0)),
					attr(xtcpnl.TcaDefData, str0("hello"))))), done())},
			expected: result{stdout: "total acts 3\n\n\taction order 0: connmark zone 5 pipe\n\t index 1 ref 1 bind 0\n" +
				"\n\taction order 1: sample rate 1/10 group 5 trunc_size 64 pipe\n\t index 2 ref 1 bind 0\n" +
				"\n\taction order 2: Simple <hello>\n\t index 3 ref 1 bind 0\n"},
		},
		{
			description: "positive: ctinfo's dscp, its unlabelled statemask (AC28) and cpmark",
			args:        []string{"actions", "ls", "action", "ctinfo"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "ctinfo", cat(attr(xtcpnl.TcaCtinfoAct, genB(2, 3, 1, 0)),
				attr(xtcpnl.TcaCtinfoZone, le16(1)), attr(xtcpnl.TcaCtinfoParmsDscpMask, le32(0xfc000000)),
				attr(xtcpnl.TcaCtinfoParmsDscpStatemask, le32(0)),
				attr(xtcpnl.TcaCtinfoParmsCpmarkMask, le32(0x3f))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ctinfo zone 1 pipe\n\t index 2 ref 1 bind 0" +
				" dscp 0xfc000000 0000000000 cpmark 0x0000003f\n"},
		},
		// Negative.
		{
			description: "negative: a kind tc does not link is bad action, exit 0 (AC6)",
			args:        []string{"actions", "ls", "action", "nosuch"},
			expected:    result{stderr: "bad action nosuch\n"},
		},
		{
			description: "negative: a failed get leaves its words, so do_action meets \"action\" (AC8)",
			args:        []string{"actions", "get", "action", "gact", "index", "999999"},
			replies:     []Reply{rep(nlErr(unix.ENOENT))},
			expected: result{stderr: "RTNETLINK answers: No such file or directory\nWe have an error talking to the kernel\n" +
				"Command \"action\" is unknown, try \"tc actions help\".\n", rc: 255},
		},
		{
			description: "negative: a pedit action (increment 13) in a dump is refused by name",
			args:        []string{"actions", "ls", "action", "pedit"},
			replies:     []Reply{rep(actMsg(1, actOrder(0, "pedit", nil)), done())},
			expected:    result{stdout: "total acts 1\n\n\taction order 0: ", stderr: notImpl + "action kind \"pedit\"\n", rc: 255},
		},
		{
			description: "negative: a write is refused, nothing sent",
			args:        []string{"actions", "add", "action", "gact", "pass"},
			expected:    result{stderr: notImpl + "tc actions add\n", rc: 255},
		},
		{
			description: "negative: a dump reply with no tab ends the list silently, exit 255",
			args:        []string{"actions", "ls", "action", "gact"},
			replies:     []Reply{rep(Msg{Type: unix.RTM_GETACTION, Body: make([]byte, 4)}, done())},
			expected:    result{stdout: "total acts 0\n", rc: 255},
		},
		{
			description: "negative: a get whose reply has no tab is Dump terminated, then AC8's loop",
			args:        []string{"actions", "get", "action", "gact", "index", "1"},
			replies:     []Reply{rep(Msg{Type: unix.RTM_NEWACTION, Body: make([]byte, 4)})},
			expected: result{stdout: "total acts 0\n", stderr: "print_action: NULL kind\nDump terminated\n" +
				"Command \"action\" is unknown, try \"tc actions help\".\n", rc: 255},
		},
		// Boundary.
		{
			description: "boundary: police in JSON has no rates (AC14) and control_action twice (AC15)",
			args:        []string{"-j", "actions", "get", "action", "police", "index", "1"},
			replies:     []Reply{rep(actMsg(-1, police(1, attr(xtcpnl.TcaPoliceResult, le32(3)))))},
			expected: result{stdout: `[{"total acts":0},{"actions":[{"order":1,"kind":"police","index":1,` +
				`"control_action":{"type":"reclassify"},"control_action":{"type":"pipe"},"overhead":0,"ref":2,"bind":1}]}]` + "\n"},
		},
		{
			description: "boundary: a pps-only policer prints a rate of 0 (AC16)",
			args:        []string{"actions", "ls", "action", "police"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "police", cat(
				attr(xtcpnl.TcaPoliceTbf, policeTbf(4, tcActDropCst, 0, 0, 0, 1, 0)),
				attr(xtcpnl.TcaPolicePktrate64, le64(1000)), attr(xtcpnl.TcaPolicePktburst64, le64(0))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  police 0x4 rate 0bit burst 0b mtu 0b " +
				"pkts_rate 1000 pkts_burst 0 action drop overhead 0b \n\tref 1 bind 0 \n"},
		},
		{
			description: "boundary: a kind past 15 bytes is cut to 15 (AC10)",
			args:        []string{"actions", "get", "action", "aaaaaaaaaaaaaaaaaa", "index", "1"},
			expected:    result{stderr: "Error: non existent action: aaaaaaaaaaaaaaa\n", rc: 255},
		},
		{
			description: "boundary: a TCA_ROOT_COUNT past the table's bound is refused",
			args:        []string{"actions", "ls", "action", "gact"},
			replies:     []Reply{rep(actMsg(2000, gact(0, tcActOKCst)), done())},
			expected:    result{stdout: "total acts 2000\n", stderr: notImpl + "an action table of 2000 orders\n", rc: 255},
		},
		{
			description: "boundary: a count of 0 is TCA_ACT_MAX_PRIO's 32 orders",
			args:        []string{"actions", "ls", "action", "gact"},
			replies:     []Reply{rep(actMsg(0, gact(32, tcActOKCst), gact(33, tcActOKCst)), done())},
			expected:    result{stdout: "total acts 0\n\n\taction order 32: " + gactLine},
		},
		// Corner.
		{
			description: "corner: -j gives a total acts object of its own, its key spaced (AC1)",
			args:        []string{"-j", "actions", "ls", "action", "gact"},
			replies:     []Reply{rep(actMsg(1, gact(0, tcActDropCst)), done())},
			expected: result{stdout: `[{"total acts":1},{"actions":[{"order":0,"kind":"gact","control_action":` +
				`{"type":"drop"},"prob":{"random_type":"none","control_action":{"type":"pass"},"val":0},` +
				`"index":1,"ref":1,"bind":1}]}]` + "\n"},
		},
		{
			description: "corner: nat's double space and \"type\" key (AC22, AC23)",
			args:        []string{"actions", "ls", "action", "nat"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "nat", attr(xtcpnl.TcaNatParms, cat(genB(1, 0, 1, 0),
				[]byte{10, 0, 0, 0}, []byte{192, 168, 0, 0}, []byte{255, 0, 255, 0}, le32(1))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  nat egress 10.0.0.0/24 192.168.0.0 pass\n" +
				"\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "corner: nat in JSON keys its kind \"type\" (AC22)",
			args:        []string{"-j", "actions", "ls", "action", "nat"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "nat", attr(xtcpnl.TcaNatParms, cat(genB(1, 0, 1, 0),
				[]byte{10, 0, 0, 0}, []byte{192, 168, 0, 0}, []byte{255, 0, 0, 0}, le32(0))))), done())},
			expected: result{stdout: `[{"total acts":1},{"actions":[{"order":0,"type":"nat","direction":"ingress",` +
				`"old_addr":"10.0.0.0/8","new_addr":"192.168.0.0","control_action":{"type":"pass"},` +
				`"index":1,"ref":1,"bind":0}]}]` + "\n"},
		},
		{
			description: "corner: csum's flags are one JSON string (AC29)",
			args:        []string{"-j", "actions", "ls", "action", "csum"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "csum", attr(xtcpnl.TcaCsumParms, cat(genB(1, 0, 1, 0),
				le32(xtcpnl.TcaCsumUpdateFlagIPv4Hdr|xtcpnl.TcaCsumUpdateFlagTCP))))), done())},
			expected: result{stdout: `[{"total acts":1},{"actions":[{"order":0,"kind":"csum","csum":"iph, tcp",` +
				`"control_action":{"type":"pass"},"index":1,"ref":1,"bind":0}]}]` + "\n"},
		},
		{
			description: "corner: csum with no flags is ?empty",
			args:        []string{"actions", "ls", "action", "csum"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "csum", attr(xtcpnl.TcaCsumParms, cat(genB(1, 0, 1, 0),
				le32(0))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: csum (?empty) action pass\n\tindex 1 ref 1 bind 0\n"},
		},
		{
			description: "corner: skbmod keeps its hard newline under -o (AC24, AC26)",
			args:        []string{"-o", "actions", "ls", "action", "skbmod"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "skbmod", cat(attr(xtcpnl.TcaSkbmodParms,
				cat(genB(2, 3, 1, 0), le32(0), le64(xtcpnl.SkbmodFSwapmac))), attr(xtcpnl.TcaSkbmodEtype, le16(0xbeef))))),
				done())},
			expected: result{stdout: "total acts 1\\\\\taction order 0: skbmod pipe set etype 0xBEEF \\\t swap mac \\" +
				"\t index 2 ref 1 bind 0\n"},
		},
		{
			description: "corner: a 4-byte PARMS is refused (AC21)",
			args:        []string{"actions", "ls", "action", "mirred"},
			replies:     []Reply{rep(actMsg(1, actOrder(0, "mirred", attr(xtcpnl.TcaMirredParms, le32(1)))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: mirred ",
				stderr: notImpl + "mirred parameters of 4 bytes (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "corner: vlan v_action 7 is past tc's table, refused (AC20)",
			args:        []string{"actions", "ls", "action", "vlan"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "vlan", attr(xtcpnl.TcaVlanParms,
				cat(genB(1, 0, 1, 0), le32(7))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: vlan ",
				stderr: notImpl + "vlan action 7 (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "corner: simple's sdata with no NUL is refused (AC21)",
			args:        []string{"actions", "ls", "action", "simple"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "simple", cat(attr(xtcpnl.TcaDefParms, genB(1, 0, 1, 0)),
				attr(xtcpnl.TcaDefData, []byte("abcd"))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ",
				stderr: notImpl + "simple sdata of 4 bytes with no NUL (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "corner: a tm of 2^32 * hz and more prints print_uint's 32 bits (AC13)",
			args:        []string{"-s", "actions", "get", "action", "gact", "index", "1"},
			replies: []Reply{rep(actMsg(-1, actOrder(1, "gact", cat(attr(xtcpnl.TcaGactParms, genB(1, 0, 1, 1)),
				attr(xtcpnl.TcaGactTm, cat(le64(1<<32*userHZ+500), le64(0), le64(0), le64(0)))))))},
			expected: result{stdout: "total acts 0\n\n\taction order 1: gact action pass\n\t random type none pass val 0\n" +
				"\t index 1 ref 1 bind 1 installed 5 sec\n"},
		},
		{
			description: "corner: missing parameters print tc's note and Error printing action",
			args:        []string{"actions", "ls", "action", "mpls"},
			replies:     []Reply{rep(actMsg(1, actOrder(0, "mpls", attr(xtcpnl.TcaMplsTm, make([]byte, 32)))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: mpls ",
				stderr: "[NULL mpls parameters]\nError printing action\n"},
		},
		{
			description: "corner: ctinfo's missing-parameters note goes to stdout, with no newline (AC27)",
			args:        []string{"actions", "ls", "action", "ctinfo"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "ctinfo",
				attr(xtcpnl.TcaCtinfoZone, le16(3)))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ctinfo [NULL ctinfo action parameters]",
				stderr: "Error printing action\n"},
		},
		{
			description: "corner: skbedit ignores QUEUE_MAPPING_MAX and TXQ_SKBHASH, a hash range prints its low end (AC19)",
			args:        []string{"actions", "ls", "action", "skbedit"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "skbedit", cat(attr(xtcpnl.TcaSkbeditParms, genB(1, 3, 1, 0)),
				attr(xtcpnl.TcaSkbeditQueueMapping, le16(0)), attr(xtcpnl.TcaSkbeditQueueMappingMax, le16(3)),
				attr(xtcpnl.TcaSkbeditFlags, le64(0x40))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: skbedit queue_mapping 0 pipe\n" +
				"\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "corner: a 2-byte mirred BLOCKID is read as tc would, MIN-copied (AC17)",
			args:        []string{"actions", "ls", "action", "mirred"},
			replies: []Reply{rep(actMsg(1, actOrder(0, "mirred", cat(attr(xtcpnl.TcaMirredParms,
				cat(genB(1, 4, 1, 0), le32(xtcpnl.TcaEgressRedirCst), le32(0))),
				attr(xtcpnl.TcaMirredBlockid, le16(7))))), done())},
			expected: result{stdout: "total acts 1\n\n\taction order 0: mirred (Egress Redirect to blockid 7) stolen\n" +
				"\tindex 1 ref 1 bind 0\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}

// TestDoActionArgs pins do_action's word handling: the request each
// command line sends (seq aside), or the error and exit code with none.
//
// go test ./internal/gotc/ -run TestDoActionArgs
func TestDoActionArgs(t *testing.T) {
	dump := func(kind string, terse bool, since uint32) []byte {
		b, err := xtcpnl.BuildTcActionDump(kind, terse, since, since != 0, 1)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	get := func(acts ...xtcpnl.TcActRef) []byte {
		b, err := xtcpnl.BuildTcActionGet(acts, 1)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	dumpReply := []Reply{rep(done())}
	getReply := []Reply{rep(actMsg(-1))}
	type want struct {
		req    []byte // the one request sent; nil for none
		stderr string // a prefix of stderr
		rc     int
	}
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    want
	}{
		// Positive.
		{"positive: ls", []string{"ls", "action", "gact"}, dumpReply, want{req: dump("gact", false, 0)}},
		{"positive: list", []string{"list", "action", "gact"}, dumpReply, want{req: dump("gact", false, 0)}},
		{"positive: show", []string{"show", "action", "gact"}, dumpReply, want{req: dump("gact", false, 0)}},
		{"positive: lst", []string{"lst", "action", "gact"}, dumpReply, want{req: dump("gact", false, 0)}},
		{"positive: since sends TIME_DELTA", []string{"ls", "action", "gact", "since", "1000"}, dumpReply,
			want{req: dump("gact", false, 1000)}},
		{"positive: -br sets TERSE_DUMP", []string{"-br", "actions", "ls", "action", "gact"}, dumpReply,
			want{req: dump("gact", true, 0)}},
		{"positive: a get of two actions orders them 1 and 2",
			[]string{"get", "action", "gact", "index", "1", "action", "mirred", "index", "2"}, getReply,
			want{req: get(xtcpnl.TcActRef{Kind: "gact", Index: 1}, xtcpnl.TcActRef{Kind: "mirred", Index: 2})}},
		{"positive: no sub-command does nothing", nil, nil, want{}},
		// Negative.
		{"negative: list with no kind is act_usage", []string{"list", "action"}, nil, want{stderr: "usage: tc actions", rc: 255}},
		{"negative: help is act_usage", []string{"help"}, nil, want{stderr: "usage: tc actions", rc: 255}},
		{"negative: an unknown sub-command", []string{"bogus"}, nil,
			want{stderr: "Command \"bogus\" is unknown, try \"tc actions help\".\n", rc: 255}},
		{"negative: get with no index", []string{"get", "action", "gact"}, nil,
			want{stderr: "Error: no index specified action: gact\n", rc: 255}},
		{"negative: get with a word other than index", []string{"get", "action", "gact", "slot", "1"}, nil,
			want{stderr: "Error: no index specified action: gact\n", rc: 255}},
		{"negative: get's index is base 10", []string{"get", "action", "gact", "index", "0x10"}, nil,
			want{stderr: "Illegal \"index\"\n", rc: 255}},
		{"negative: index with no value", []string{"get", "action", "gact", "index"}, nil,
			want{stderr: "Command line is not complete.", rc: 255}},
		{"negative: get action help returns -1 silently", []string{"get", "action", "help"}, nil, want{rc: 255}},
		{"negative: a since that is not a number", []string{"ls", "action", "gact", "since", "x"}, nil,
			want{stderr: "Error: argument \"x\" is wrong: dump time \"since\" is invalid", rc: 255}},
		{"negative: since with no value", []string{"ls", "action", "gact", "since"}, nil,
			want{stderr: "Command line is not complete.", rc: 255}},
		{"negative: a word after get that is not action is skipped all the same",
			[]string{"get", "gact", "index", "1"}, nil, want{stderr: "Error: non existent action: index\n", rc: 255}},
		// Boundary.
		{"boundary: index 0 sends no INDEX", []string{"get", "action", "gact", "index", "0"}, getReply,
			want{req: get(xtcpnl.TcActRef{Kind: "gact"})}},
		{"boundary: index 0xffffffff", []string{"get", "action", "gact", "index", "4294967295"}, getReply,
			want{req: get(xtcpnl.TcActRef{Kind: "gact", Index: 0xffffffff})}},
		{"boundary: index past 32 bits", []string{"get", "action", "gact", "index", "4294967296"}, nil,
			want{stderr: "Illegal \"index\"\n", rc: 255}},
		{"boundary: since 0 sends no TIME_DELTA", []string{"ls", "action", "gact", "since", "0"}, dumpReply,
			want{req: dump("gact", false, 0)}},
		// Corner.
		{"corner: words around the kind are ignored (AC7)", []string{"list", "foo", "gact", "bar"}, dumpReply,
			want{req: dump("gact", false, 0)}},
		{"corner: l and s are prefixes of list and show", []string{"l", "x", "gact"}, dumpReply,
			want{req: dump("gact", false, 0)}},
		{"corner: s is show", []string{"s", "x", "gact"}, dumpReply, want{req: dump("gact", false, 0)}},
		{"corner: get help sends an empty tab (AC9)", []string{"get", "help"}, getReply, want{req: get()}},
		{"corner: a bare get sends an empty tab (AC9)", []string{"get"}, getReply, want{req: get()}},
		{"corner: flush is refused", []string{"flush", "action", "gact"}, nil,
			want{stderr: notImpl + "tc actions flush\n", rc: 255}},
		{"corner: since is read as base 0", []string{"ls", "action", "gact", "since", "0x10"}, dumpReply,
			want{req: dump("gact", false, 16)}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			args := tc.args
			if len(args) == 0 || !strings.HasPrefix(args[0], "-") {
				args = append([]string{"actions"}, args...)
			}
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			rc := run(cfg, args, &out, &errb)
			if rc != tc.expected.rc || !strings.HasPrefix(errb.String(), tc.expected.stderr) ||
				(tc.expected.stderr == "" && errb.Len() > 0) {
				t.Errorf("tc %q: rc %d stderr %q, want rc %d stderr %q...", args, rc, errb.String(),
					tc.expected.rc, tc.expected.stderr)
			}
			switch {
			case tc.expected.req == nil && len(src.reqs) != 0:
				t.Errorf("tc %q sent %d requests, want none", args, len(src.reqs))
			case tc.expected.req != nil && len(src.reqs) != 1:
				t.Errorf("tc %q sent %d requests, want 1", args, len(src.reqs))
			case tc.expected.req != nil &&
				!bytes.Equal(wsreplay.CanonSeqPID(src.reqs[0]), wsreplay.CanonSeqPID(tc.expected.req)):
				t.Errorf("tc %q request\n got  %x\n want %x", args, src.reqs[0], tc.expected.req)
			}
		})
	}
}
