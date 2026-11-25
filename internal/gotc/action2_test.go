package gotc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The crafted options of increment 13's kinds, in the kernel's dump order
// (net/sched/act_<kind>.c), tc_gen index 1, action pipe.

// peditKeyB is a struct tc_pedit_key: mask and val as the wire has them
// (tc prints their ntohl), off in host order.
func peditKeyB(mask, val, off uint32) []byte {
	return cat(be32(mask), be32(val), le32(off), le32(0), le32(0), le32(0))
}

// peditSelB is a tc_pedit_sel of nkeys, then keys.
func peditSelB(bind, nkeys uint32, keys ...[]byte) []byte {
	return cat(genB(1, tcActPipeCst, 1, bind), []byte{byte(nkeys), 0, 0, 0}, cat(keys...))
}

// keyExB is one TCA_PEDIT_KEY_EX, without NLA_F_NESTED: tc compares the
// raw type (pedit_keys_ex_getattr).
func keyExB(htype, cmd uint16) []byte {
	return attr(xtcpnl.TcaPeditKeyEx, cat(attr(xtcpnl.TcaPeditKeyExHtype, le16(htype)),
		attr(xtcpnl.TcaPeditKeyExCmd, le16(cmd))))
}

// insnB is a struct sock_filter.
func insnB(code uint16, jt, jf uint8, k uint32) []byte {
	return cat(le16(code), []byte{jt, jf}, le32(k))
}

// tkParms is a tunnel_key's PARMS with t_action set.
func tkParms() []byte {
	return attr(xtcpnl.TcaTunnelKeyParms, cat(genB(1, tcActPipeCst, 1, 0), le32(xtcpnl.TcaTunnelKeyActSet)))
}

func tkAddrs() []byte {
	return cat(attr(xtcpnl.TcaTunnelKeyEncIPv4Src, []byte{10, 0, 0, 1}), attr(xtcpnl.TcaTunnelKeyEncIPv4Dst, []byte{10, 0, 0, 2}))
}

// gateEntryB is one TCA_GATE_ONE_ENTRY.
func gateEntryB(index uint32, open bool, ipv, maxOctets int32) []byte {
	e := attr(xtcpnl.TcaGateEntryIndex, le32(index))
	if open {
		e = cat(e, attr(xtcpnl.TcaGateEntryGate, nil))
	}
	return attr(xtcpnl.TcaGateOneEntry, cat(e, attr(xtcpnl.TcaGateEntryInterval, le32(100000000)),
		attr(xtcpnl.TcaGateEntryMaxOctets, le32(uint32(maxOctets))), attr(xtcpnl.TcaGateEntryIpv, le32(uint32(ipv)))))
}

const tcActPipeCst = 3

// TestGotcAction2Unit runs the printers of pedit, tunnel_key, ife, ct, gate
// and bpf, and the bpf filter's, on scripted replies: each row's stdout,
// stderr and exit code, or, where only a part is pinned (has), strings
// stdout or stderr must hold ("!" for one they must not).
//
// go test ./internal/gotc/ -run TestGotcAction2Unit
func TestGotcAction2Unit(t *testing.T) {
	ls := func(k string) []string { return []string{"actions", "ls", "action", k} }
	get := func(k string) []string { return []string{"actions", "get", "action", k, "index", "1"} }
	one := func(order uint16, k string, opts []byte, extra ...[]byte) Reply {
		if order == 0 {
			return rep(actMsg(1, actOrder(0, k, opts, extra...)), done())
		}
		return rep(actMsg(-1, actOrder(order, k, opts, extra...)))
	}
	pedit2 := cat(attr(xtcpnl.TcaPeditParmsEx, peditSelB(0, 2, peditKeyB(0x00ffffff, 0x01000000, 8),
		peditKeyB(0xffff0000, 0x50, 0))), attr(xtcpnl.TcaPeditKeysEx, cat(keyExB(xtcpnl.PeditHtypeIP4, xtcpnl.PeditCmdAdd),
		keyExB(xtcpnl.PeditHtypeTCP, xtcpnl.PeditCmdSet))))
	geneve := attr(xtcpnl.TcaTunnelKeyEncOpts, attr(xtcpnl.TcaTunnelKeyEncOptsGeneve, cat(
		attr(xtcpnl.TcaTunnelKeyEncOptGeneveClass, be16(0x0102)), attr(xtcpnl.TcaTunnelKeyEncOptGeneveType, []byte{0x80}),
		attr(xtcpnl.TcaTunnelKeyEncOptGeneveData, be32(0x00880022)),
		attr(xtcpnl.TcaTunnelKeyEncOptGeneveClass, be16(0x0103)), attr(xtcpnl.TcaTunnelKeyEncOptGeneveType, []byte{0x81}),
		attr(xtcpnl.TcaTunnelKeyEncOptGeneveData, be32(0x00a000b0)))))
	ifeParms := func(flags uint16) []byte {
		return attr(xtcpnl.TcaIfeParms, cat(genB(1, tcActPipeCst, 1, 0), le16(flags), []byte{0, 0}))
	}
	ife := cat(ifeParms(xtcpnl.IfeEncode), attr(xtcpnl.TcaIfeDmac, []byte{2, 0, 0, 0, 0, 1}),
		attr(xtcpnl.TcaIfeType, le16(0xED3E)), attr(xtcpnl.TcaIfeMetalst, attr(xtcpnl.IfeMetaSkbmark, le32(7))))
	ctParms := attr(xtcpnl.TcaCtParms, genB(1, tcActPipeCst, 1, 0))
	ctNat := cat(ctParms, attr(xtcpnl.TcaCtAction, le16(xtcpnl.TcaCtActCommit|xtcpnl.TcaCtActNat|xtcpnl.TcaCtActNatSrc)),
		attr(xtcpnl.TcaCtZone, le16(0)), attr(xtcpnl.TcaCtNatIPv4Min, []byte{10, 0, 0, 1}),
		attr(xtcpnl.TcaCtNatIPv4Max, []byte{10, 0, 0, 9}), attr(xtcpnl.TcaCtNatPortMin, be16(1000)),
		attr(xtcpnl.TcaCtNatPortMax, be16(2000)))
	ctHelper := func(index uint32, family byte) []byte {
		return cat(attr(xtcpnl.TcaCtParms, genB(index, tcActPipeCst, 1, 0)), attr(xtcpnl.TcaCtHelperName, str0("ftp")),
			attr(xtcpnl.TcaCtHelperFamily, []byte{family}), attr(xtcpnl.TcaCtHelperProto, []byte{unix.IPPROTO_TCP}))
	}
	gateTop := cat(attr(xtcpnl.TcaGateParms, genB(1, tcActPipeCst, 1, 0)), attr(xtcpnl.TcaGateBaseTime, le64(200000000000)),
		attr(xtcpnl.TcaGateCycleTime, le64(300000000)), attr(xtcpnl.TcaGateCycleTimeExt, le64(1000)),
		attr(xtcpnl.TcaGateClockid, le32(11)), attr(xtcpnl.TcaGateFlags, le32(0)), attr(xtcpnl.TcaGatePriority, le32(2)))
	gate3 := cat(gateTop, attr(xtcpnl.TcaGateEntryList, cat(gateEntryB(0, true, 3, 8000), gateEntryB(1, false, -1, -1),
		gateEntryB(2, true, 1, -1))))
	actBpf := func(n uint16, insns ...[]byte) []byte {
		return cat(attr(xtcpnl.TcaActBpfParms, genB(1, tcActPipeCst, 1, 0)), attr(xtcpnl.TcaActBpfOpsLen, le16(n)),
			attr(xtcpnl.TcaActBpfOps, cat(insns...)))
	}
	d0 := rep(linkMsg(2, "d0"))
	bpfFilter := func(opts ...[]byte) Reply {
		return rep(fmsg(tcBody(2, 1, tcHClsactIngress, finfo(1, ethPIP), kind("bpf"), attr(xtcpnl.TcaChain, le32(0)),
			nest(xtcpnl.TcaOptions, opts...))), done())
	}
	skipHw := attr(xtcpnl.TcaActFlags, cat(le32(xtcpnl.TcaActFlagsSkipHw), le32(xtcpnl.TcaActFlagsSkipHw)))
	bound := rep(fmsg(tcBody(2, 1, tcHClsactIngress, finfo(3, 0x0003), kind("matchall"), attr(xtcpnl.TcaChain, le32(0)),
		nest(xtcpnl.TcaOptions, nest(xtcpnl.TcaMatchallAct,
			actOrder(1, "pedit", attr(xtcpnl.TcaPeditParms, peditSelB(1, 1, peditKeyB(0, 1, 0))), skipHw),
			actOrder(2, "ct", cat(attr(xtcpnl.TcaCtParms, genB(1, tcActPipeCst, 1, 1)), attr(xtcpnl.TcaCtZone, le16(0)))),
			actOrder(3, "gate", attr(xtcpnl.TcaGateParms, genB(1, tcActPipeCst, 1, 1))))))), done())

	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    result
		has         []string // when set: rc is expected.rc, and these hold
	}{
		// Positive.
		{
			description: "positive: pedit ex with 2 keys (AC42-AC44)",
			args:        ls("pedit"),
			replies:     []Reply{one(0, "pedit", pedit2)},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  pedit action pipe keys 2\n \t index 1 ref 1 bind 0" +
				"\n\t key #0  at ipv4+8: add 01000000 mask 00ffffff\n\t key #1  at tcp+0: val 00000050 mask ffff0000\n"},
		},
		{
			description: "positive: tunnel_key set with 2 geneve options",
			args:        get("tunnel_key"),
			replies: []Reply{one(1, "tunnel_key", cat(tkParms(), tkAddrs(), geneve,
				attr(xtcpnl.TcaTunnelKeyNoCsum, []byte{0})))},
			expected: result{stdout: "total acts 0\n\n\taction order 1: tunnel_key  set\n\tsrc_ip 10.0.0.1\n\tdst_ip 10.0.0.2" +
				"\n\tgeneve_opts 0102:80:00880022,0103:81:00a000b0\n\tcsum pipe\n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: ife encode, type and use mark (AC51, AC52)",
			args:        ls("ife"),
			replies:     []Reply{one(0, "ife", ife)},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ife encode action pipe type 0XED3E \n" +
				"\tuse mark 7 dst 02:00:00:00:00:01 \n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: ct commit with a src nat range and ports",
			args:        get("ct"),
			replies:     []Reply{one(1, "ct", ctNat)},
			expected: result{stdout: "total acts 0\n\n\taction order 1: ct commit zone 0 nat src addr 10.0.0.1-10.0.0.9" +
				" port 1000-2000 pipe\n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: gate with 3 entries (AC59-AC61)",
			args:        get("gate"),
			replies:     []Reply{one(1, "gate", gate3)},
			expected: result{stdout: "total acts 0\n\n\taction order 1: \n\tpriority 2       \tclockid TAI\tflags 0\n" +
				"\tbase-time 200s\tcycle-time 300ms\tcycle-time-ext 1us\n\tschedule:\n" +
				"\t number    0\tgate-state open \tinterval 100ms\t ipv 3         \t max-octets 8000b\n" +
				"\t number    1\tgate-state close \tinterval 100ms\t ipv wildcard\t max-octets wildcard\n" +
				"\t number    2\tgate-state open \tinterval 100ms\t ipv 1         \t max-octets wildcard\n" +
				"\tpipe\n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "positive: a cBPF action (AC65)",
			args:        ls("bpf"),
			replies:     []Reply{one(0, "bpf", actBpf(2, insnB(6, 0, 0, 0), insnB(6, 0, 0, 1)))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: bpf bytecode '2,6 0 0 0,6 0 0 1' default-action pipe\n" +
				"\t index 1 ref 1 bind 0\n "},
		},
		{
			description: "positive: a bpf filter with da and a classid (AC66)",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, bpfFilter(attr(xtcpnl.TcaBpfClassid, le32(0x10001)), attr(xtcpnl.TcaBpfOpsLen, le16(1)),
				attr(xtcpnl.TcaBpfOps, insnB(6, 0, 0, 0)), attr(xtcpnl.TcaBpfFlags, le32(xtcpnl.TcaBpfFlagActDirect)),
				attr(xtcpnl.TcaBpfFlagsGen, le32(xtcpnl.TcaClsFlagsNotInHw)))},
			expected: result{stdout: "filter protocol ip pref 1 bpf chain 0 handle 0x1 flowid 1:1 direct-action not_in_hw" +
				" bytecode '1,6 0 0 0'\n"},
		},
		// Negative.
		{
			description: "negative: a pedit PARMS of 4 bytes is refused (AC40)",
			args:        ls("pedit"),
			replies:     []Reply{one(0, "pedit", attr(xtcpnl.TcaPeditParms, le32(1)))},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  pedit ",
				stderr: notImpl + "pedit parameters of 4 bytes (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: a pedit ex with too few KEY_EX is tc's Netlink error",
			args:        ls("pedit"),
			replies: []Reply{one(0, "pedit", cat(attr(xtcpnl.TcaPeditParmsEx, peditSelB(0, 2, peditKeyB(0, 1, 0),
				peditKeyB(0, 1, 4))), attr(xtcpnl.TcaPeditKeysEx, keyExB(0, 0))))},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  pedit ", stderr: "Netlink error\nError printing action\n"},
		},
		{
			description: "negative: a geneve option without CLASS is refused (AC46)",
			args:        ls("tunnel_key"),
			replies: []Reply{one(0, "tunnel_key", cat(tkParms(), attr(xtcpnl.TcaTunnelKeyEncOpts,
				attr(xtcpnl.TcaTunnelKeyEncOptsGeneve, cat(attr(xtcpnl.TcaTunnelKeyEncOptGeneveType, []byte{1}),
					attr(xtcpnl.TcaTunnelKeyEncOptGeneveData, be32(1)))))))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: tunnel_key ",
				stderr: notImpl + "tunnel_key geneve options its walk cannot step through (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "negative: a bpf action with an eBPF ID is refused",
			args:        ls("bpf"),
			replies: []Reply{one(0, "bpf", cat(attr(xtcpnl.TcaActBpfParms, genB(1, tcActPipeCst, 1, 0)),
				attr(xtcpnl.TcaActBpfName, str0("prog")), attr(xtcpnl.TcaActBpfID, le32(5))))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: bpf ", stderr: notImpl + "eBPF program info via bpf(2)\n",
				rc: 255},
		},
		{
			description: "negative: a bpf filter with an eBPF ID is refused",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, bpfFilter(attr(xtcpnl.TcaBpfID, le32(5)))},
			expected:    result{stdout: "filter protocol ip pref 1 bpf chain 0 ", stderr: notImpl + "eBPF program info via bpf(2)\n", rc: 255},
		},
		{
			description: "negative: ct without PARMS prints tc's bracketed note to stdout (AC58)",
			args:        ls("ct"),
			replies:     []Reply{one(0, "ct", attr(xtcpnl.TcaCtZone, le16(0)))},
			expected:    result{stdout: "total acts 1\n\n\taction order 0: ct[NULL ct parameters]", stderr: "Error printing action\n"},
		},
		// Boundary.
		{
			description: "boundary: pedit of no keys prints tc's note in text (AC41)",
			args:        ls("pedit"),
			replies:     []Reply{one(0, "pedit", attr(xtcpnl.TcaPeditParms, peditSelB(0, 0)))},
			expected: result{stdout: "total acts 1\n\n\taction order 0:  pedit action pipe keys 0\n \t index 1 ref 1 bind 0" +
				"\npedit 1 keys 0 is not LEGIT\n"},
		},
		{
			description: "boundary: pedit of no keys under -j is refused (AC41)",
			args:        append([]string{"-j"}, ls("pedit")...),
			replies:     []Reply{one(0, "pedit", attr(xtcpnl.TcaPeditParms, peditSelB(0, 0)))},
			expected:    result{rc: 255},
			has:         []string{notImpl + "pedit of no keys (tc writes its note into the JSON)"},
		},
		{
			description: "boundary: gate wildcards in text and -1 in JSON (K11)",
			args:        append([]string{"-j"}, get("gate")...),
			replies: []Reply{one(1, "gate", cat(attr(xtcpnl.TcaGateParms, genB(1, tcActPipeCst, 1, 0)),
				attr(xtcpnl.TcaGateEntryList, gateEntryB(0, false, -1, -1))))},
			has: []string{`"priority":-1,"clockid":"invalid","base_time":0`,
				`"schedule":[{"number":0,"gate_state":"close","interval":100000000,"ipv":-1,"max_octets":-1}]`},
		},
		{
			description: "boundary: OPS_LEN one past the ops is refused (AC63)",
			args:        ls("bpf"),
			replies:     []Reply{one(0, "bpf", actBpf(3, insnB(6, 0, 0, 0), insnB(6, 0, 0, 1)))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: bpf ",
				stderr: notImpl + "bpf bytecode of 3 instructions past its ops (tc reads it unchecked)\n", rc: 255},
		},
		{
			description: "boundary: a helper name past helper[32] is cut to 31 bytes",
			args:        ls("ct"),
			replies: []Reply{one(0, "ct", cat(ctParms, attr(xtcpnl.TcaCtHelperName, str0("abcdefghijklmnopqrstuvwxyz")),
				attr(xtcpnl.TcaCtHelperFamily, []byte{unix.AF_INET}), attr(xtcpnl.TcaCtHelperProto, []byte{unix.IPPROTO_UDP})))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ct helper ipv4-udp-abcdefghijklmnopqrstuv pipe\n" +
				"\t index 1 ref 1 bind 0\n"},
		},
		// Corner.
		{
			description: "corner: -j tunnel_key csum is a string, nofrag a bool (AC49)",
			args:        append([]string{"-j"}, get("tunnel_key")...),
			replies: []Reply{one(1, "tunnel_key", cat(tkParms(), tkAddrs(), attr(xtcpnl.TcaTunnelKeyNoCsum, []byte{0}),
				attr(xtcpnl.TcaTunnelKeyNoFrag, nil)))},
			has: []string{`"kind":"tunnel_key","mode":"set","src_ip":"10.0.0.1","dst_ip":"10.0.0.2","flag":"csum","nofrag":true`},
		},
		{
			description: "corner: a t_action other than set or unset prints no mode (AC50)",
			args:        ls("tunnel_key"),
			replies: []Reply{one(0, "tunnel_key", attr(xtcpnl.TcaTunnelKeyParms,
				cat(genB(1, tcActPipeCst, 1, 0), le32(3))))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: tunnel_key  pipe\n\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "corner: -j erspan v2 carries index 0 (AC48)",
			args:        append([]string{"-j"}, get("tunnel_key")...),
			replies: []Reply{one(1, "tunnel_key", cat(tkParms(), attr(xtcpnl.TcaTunnelKeyEncOpts, attr(xtcpnl.TcaTunnelKeyEncOptsErspan,
				cat(attr(xtcpnl.TcaTunnelKeyEncOptErspanVer, []byte{2}), attr(xtcpnl.TcaTunnelKeyEncOptErspanDir, []byte{1}),
					attr(xtcpnl.TcaTunnelKeyEncOptErspanHwid, []byte{7}))))))},
			has: []string{`"erspan_opts":[{"ver":2,"index":0,"dir":1,"hwid":7}]`},
		},
		{
			description: "corner: -j gate has no kind (AC59)",
			args:        append([]string{"-j"}, get("gate")...),
			replies:     []Reply{one(1, "gate", gate3)},
			has:         []string{`{"order":1,"priority":2,"clockid":"TAI","flags":"0","base_time":200000000000`, `!"kind"`},
		},
		{
			description: "corner: -o keeps pedit's and gate's hard newlines (AC39, AC61)",
			args:        append([]string{"-o"}, ls("pedit")...),
			replies:     []Reply{one(0, "pedit", pedit2)},
			has:         []string{"keys 2\n \t index 1", "\n\t key #1  at tcp+0: val 00000050 mask ffff0000\\"},
		},
		{
			description: "corner: -o gate",
			args:        append([]string{"-o"}, get("gate")...),
			replies:     []Reply{one(1, "gate", gate3)},
			has:         []string{"action order 1: \n\tpriority 2       ", "cycle-time-ext 1us\\\tschedule:\\\t number    0"},
		},
		{
			description: "corner: ct helper family 10 and 7 print ipv6, 2 prints ipv4 (AC56)",
			args:        ls("ct"),
			replies: []Reply{rep(actMsg(3, actOrder(0, "ct", ctHelper(1, unix.AF_INET6)), actOrder(1, "ct", ctHelper(2, 7)),
				actOrder(2, "ct", ctHelper(3, unix.AF_INET))), done())},
			expected: result{stdout: "total acts 3\n\n\taction order 0: ct helper ipv6-tcp-ftp pipe\n\t index 1 ref 1 bind 0\n" +
				"\n\taction order 1: ct helper ipv6-tcp-ftp pipe\n\t index 2 ref 1 bind 0\n" +
				"\n\taction order 2: ct helper ipv4-tcp-ftp pipe\n\t index 3 ref 1 bind 0\n"},
		},
		{
			description: "corner: -N names the helper's protocol ipproto-6",
			args:        append([]string{"-N"}, ls("ct")...),
			replies:     []Reply{one(0, "ct", ctHelper(1, unix.AF_INET))},
			has:         []string{" helper ipv4-ipproto-6-ftp pipe"},
		},
		{
			description: "corner: ife with a dst and no type gets no break: has_optional is tested first (AC52)",
			args:        ls("ife"),
			replies:     []Reply{one(0, "ife", cat(ifeParms(xtcpnl.IfeEncode), attr(xtcpnl.TcaIfeDmac, []byte{2, 0, 0, 0, 0, 1})))},
			expected: result{stdout: "total acts 1\n\n\taction order 0: ife encode action pipe dst 02:00:00:00:00:01 \n" +
				"\t index 1 ref 1 bind 0\n"},
		},
		{
			description: "corner: -j ife allow mark is a string, use mark 7 a number (AC53)",
			args:        append([]string{"-j"}, ls("ife")...),
			replies: []Reply{rep(actMsg(2, actOrder(0, "ife", ife), actOrder(1, "ife", cat(ifeParms(xtcpnl.IfeEncode),
				attr(xtcpnl.TcaIfeType, le16(0xED3E)), attr(xtcpnl.TcaIfeMetalst, attr(xtcpnl.IfeMetaSkbmark, nil))))), done())},
			has: []string{`"type":"0xed3e","mark":7,"dst":"02:00:00:00:00:01"`, `"type":"0xed3e","mark":"allow","index":1`},
		},
		{
			description: "corner: a matchall holding pedit, ct and gate (bind 1) prints no skip_hw",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, bound},
			expected: result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n" +
				"\taction order 1:  pedit action pipe keys 1\n \t index 1 ref 1 bind 1\n\t key #0  at 0: val 00000001 mask 00000000\n" +
				"\n\taction order 2: ct zone 0 pipe\n\t index 1 ref 1 bind 1\n" +
				"\n\taction order 3: \n\tpriority wildcard\tclockid invalid\n\tbase-time 0ns\tcycle-time 0ns\tcycle-time-ext 0ns" +
				"\tpipe\n\t index 1 ref 1 bind 1\n\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if tc.has == nil {
				if got != tc.expected {
					t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
				}
				return
			}
			if got.rc != tc.expected.rc {
				t.Errorf("tc %q: rc %d, want %d (%#v)", tc.args, got.rc, tc.expected.rc, got)
			}
			both := got.stdout + got.stderr
			for _, s := range tc.has {
				if neg, ok := strings.CutPrefix(s, "!"); ok {
					if strings.Contains(both, neg) {
						t.Errorf("tc %q: holds %q:\n%s", tc.args, neg, both)
					}
				} else if !strings.Contains(both, s) {
					t.Errorf("tc %q: lacks %q:\n%s", tc.args, s, both)
				}
			}
		})
	}
}
