package main

import "strings"

// The tipc space (genl-clones 5C): tipc(8)'s read-only commands over
// generic netlink's "TIPCv2" family. tipc's side is iproute2's tipc/: main
// (tipc/tipc.c) parses its options with getopt_long, then resolves the
// family with mnlu_gen_socket_open (one CTRL_CMD_GETFAMILY) BEFORE run_cmd
// dispatches the object. Every refusal after the options is therefore an
// ack-then-fail, and main turns any failure into exit 255; only an unknown
// option, which getopt refuses before the family is resolved, exits 1 with
// no netlink at all. run_cmd matches words by prefix and refuses an
// ambiguous one (tipc/cmdl.c, find_cmd).
//
// The kernel's side is net/tipc/netlink.c (tipc_genl_family, netnsok true)
// and the per-object fill functions. TIPC's state is per network namespace,
// so each row answers in its own fresh namespace. The `peer` fixture alone
// needs a second node: an unnamed namespace of its own (see tipcPeer),
// joined by a veth pair, so peer rows need no serial execution either.
//
// Only link and nametable print JSON; bearer, media, node and socket accept
// -j and print text. Every command is captured plain and -j anyway, so the
// corpus records that.
//
// Determinism: identities are fixed (1001, the peer 1002), so the node
// address, which TIPC hashes from the identity, is too; MACs are fixed.
// Three things are not: TIPC picks socket ports at random
// (net/tipc/socket.c, tipc_sk_insert), so `socket list` and `nametable
// show` vary in port and order; the peer link's counters depend on timing.
// The two-boot comparison allows those rows' replies to differ (V093).
//
// This space is read-only: setup runs in `pre`, outside the capture
// window. The clone (internal/gotipc) replays these captures and must
// reproduce tipc's stdout, stderr and exit status byte for byte.

var tipcModules = []string{"tipc"}

// tipcPeerLink is the unicast link the peer fixture brings up, named by
// TIPC from the two identities and bearer devices.
const tipcPeerLink = "1001:v0-1002:v1"

// tipcIdent gives the row's node a fixed identity; its address is the
// identity's hash, assigned once a bearer is enabled.
const tipcIdent = "tipc node set identity 1001"

// tipcWaitAddr waits up to 5 s for the node address a bearer brings, so a
// row captures the assigned address and its name-table entry, not the
// moment before.
const tipcWaitAddr = `for i in $(seq 50); do [ "$(tipc node get address)" != 00000000 ] && exit 0; sleep 0.1; done; exit 1`

// tipcEth is an eth bearer on a dummy device with a fixed MAC.
var tipcEth = []string{
	tipcIdent,
	"ip link add d0 address 02:00:00:00:00:01 type dummy && ip link set d0 up",
	"tipc bearer enable media eth device d0",
	tipcWaitAddr,
}

// tipcUDP is a udp bearer on a dummy device's address. TIPC refuses
// 127.0.0.1 (ENODEV), so the address is a dummy's.
var tipcUDP = []string{
	tipcIdent,
	"ip link add d1 type dummy && ip addr add 10.9.0.1/24 dev d1 && ip link set d1 up",
	"tipc bearer enable media udp name u0 localip 10.9.0.1",
	tipcWaitAddr,
}

// tipcPeer is a second node, identity 1002, joined to the row by the veth
// pair v0/v1 with eth bearers on both ends. The peer is an unnamed
// namespace held by a background `unshare -n sleep`, named by its pid, all
// in one step: each pre step runs under its own `ip netns exec`, whose
// private mount namespace would hide an `ip netns add` mount from the next
// step (a later `netns NAME` fails EINVAL). The step waits until the sleep
// has its own namespace before moving v1 into it. The link comes up in
// about two seconds; the last step waits up to ten.
var tipcPeer = []string{
	tipcIdent,
	`unshare -n sleep 60 >/dev/null 2>&1 & p=$!; ` +
		`for i in $(seq 50); do [ "$(readlink /proc/$p/ns/net)" != "$(readlink /proc/self/ns/net)" ] && break; sleep 0.1; done; ` +
		`ip link add v0 address 02:00:00:00:00:01 type veth peer name v1 address 02:00:00:00:00:02 netns "$p" && ip link set v0 up && ` +
		`nsenter -t "$p" -n sh -c 'ip link set v1 up && tipc node set identity 1002 && tipc bearer enable media eth device v1'`,
	"tipc bearer enable media eth device v0",
	`for i in $(seq 100); do tipc link list | grep -qx '` + tipcPeerLink + `: up' && exit 0; sleep 0.1; done; exit 1`,
}

// tipcSockProg holds three TIPC sockets for the row: an RDM socket bound
// to {1000,1,1} and a connected stream pair on {2000,1}. A TIPC stream
// connect completes only when the listener accepts, so it runs in a
// thread. It touches its argument when ready, then sleeps past the row.
const tipcSockProg = `import socket as s, sys, threading as th, time; ` +
	`r = s.socket(s.AF_TIPC, s.SOCK_RDM); r.bind((s.TIPC_ADDR_NAMESEQ, 1000, 1, 1, s.TIPC_NODE_SCOPE)); ` +
	`l = s.socket(s.AF_TIPC, s.SOCK_STREAM); l.bind((s.TIPC_ADDR_NAME, 2000, 1, 0)); l.listen(1); ` +
	`c = s.socket(s.AF_TIPC, s.SOCK_STREAM); t = th.Thread(target=c.connect, args=((s.TIPC_ADDR_NAME, 2000, 1, 0),)); ` +
	`t.start(); a, _ = l.accept(); t.join(); open(sys.argv[1], "w").close(); time.sleep(30)`

// tipcSock starts tipcSockProg in the background, its output closed so the
// runner does not wait on it, and waits up to 5 s for it to be ready.
var tipcSock = []string{
	tipcIdent,
	`f=/tmp/tipc-sock.$$; python3 -c '` + tipcSockProg + `' "$f" >/dev/null 2>&1 & ` +
		`for i in $(seq 50); do [ -e "$f" ] && rm -f "$f" && exit 0; sleep 0.1; done; exit 1`,
}

// tc is a command value: its fixture, the tipc words, and the fixture's
// setup. The value's name is the fixture and the words, so every row id
// names what it ran.
func tc(fix, words string, pre []string) value {
	val := v(fix+":"+strings.ReplaceAll(words, " ", "-"), words, f("fix", fix, "words", words))
	val.Pre = pre
	return val
}

// tipcBad is a refused command: its name, words and expectation, after
// the setup pre (nil for none).
func tipcBad(name, words string, pre []string, exp expect, why string) badValue {
	val := v(name, words, nil)
	val.Pre = pre
	return badValue{Axis: "cmd", Value: val, Expect: exp, Why: why}
}

// tipcSpace: tipc's read-only commands over six namespace fixtures, each
// plain and -j.
func tipcSpace() *space {
	ack := expect{RC: intp(255), RefusedBy: refAckThenFail}
	sp := &space{
		Name: "tipc",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// bare: tipc loaded, nothing configured. Address 0, the
				// default netid, empty bearer, node and monitor dumps;
				// the name table holds the topology server's socket.
				tc("bare", "node get address", nil),
				tc("bare", "node get identity", nil),
				tc("bare", "node get netid", nil),
				tc("bare", "node list", nil),
				tc("bare", "bearer list", nil),
				tc("bare", "media list", nil),
				tc("bare", "link list", nil),
				tc("bare", "link monitor summary", nil),
				tc("bare", "nametable show", nil),
				tc("bare", "socket list", nil),
				// A filter no link name holds: an empty stats dump.
				tc("bare", "link statistics show link nosuch", nil),
				// ident: an identity, no bearer, so still address 0.
				tc("ident", "node get identity", []string{tipcIdent}),
				tc("ident", "node get address", []string{tipcIdent}),
				// eth: one eth bearer, the address assigned.
				tc("eth", "node get address", tipcEth),
				tc("eth", "bearer list", tipcEth),
				tc("eth", "bearer get priority media eth device d0", tipcEth),
				tc("eth", "bearer get tolerance media eth device d0", tipcEth),
				tc("eth", "bearer get window media eth device d0", tipcEth),
				tc("eth", "media get priority media eth", tipcEth),
				tc("eth", "media get window media eth", tipcEth),
				tc("eth", "link list", tipcEth),
				tc("eth", "link get broadcast", tipcEth),
				tc("eth", "link statistics show link broadcast-link", tipcEth),
				tc("eth", "link monitor summary", tipcEth),
				tc("eth", "link monitor get threshold", tipcEth),
				tc("eth", "link monitor list", tipcEth),
				tc("eth", "nametable show", tipcEth),
				// udp: one udp bearer, which carries an mtu.
				tc("udp", "bearer list", tipcUDP),
				tc("udp", "bearer get mtu media udp name u0", tipcUDP),
				tc("udp", "bearer get priority media udp name u0", tipcUDP),
				tc("udp", "media get mtu media udp", tipcUDP),
				// peer: a unicast link to a second node.
				tc("peer", "link list", tipcPeer),
				tc("peer", "link get priority link "+tipcPeerLink, tipcPeer),
				tc("peer", "link get tolerance link "+tipcPeerLink, tipcPeer),
				tc("peer", "link get window link "+tipcPeerLink, tipcPeer),
				tc("peer", "link statistics show link "+tipcPeerLink, tipcPeer),
				tc("peer", "link monitor list", tipcPeer),
				tc("peer", "node list", tipcPeer),
				// sock: bound and connected sockets.
				tc("sock", "socket list", tipcSock),
				tc("sock", "nametable show", tipcSock),
			}},
		},
		Bad: []badValue{
			// An unknown option: getopt prints its message and main
			// returns 1, before the family is resolved.
			{Axis: "opt", Value: v("badopt", "-z", nil),
				Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why:    "an unknown option is refused before the family is resolved"},
			tipcBad("none", "", nil, ack, "no object prints the usage after the family is resolved"),
			tipcBad("help", "help", nil, ack, "help is no object: an invalid command"),
			tipcBad("badobj", "badobj", nil, ack, "an unknown object"),
			tipcBad("ambiguous", "n", nil, ack, "n is a prefix of both nametable and node"),
			tipcBad("linkbad", "link badcmd", nil, ack, "an unknown link sub-command"),
			tipcBad("nolink", "link get priority", nil, ack, "link get with no link name"),
			tipcBad("nosuchlink", "link get priority link nosuch", nil,
				expect{RC: intp(255), Errno: "EINVAL", RefusedBy: refKernel},
				"the kernel has no such link"),
			tipcBad("nodebad", "node get badprop", nil, ack, "an unknown node property"),
			tipcBad("nodevice", "bearer get priority media eth", nil, ack, "an eth bearer with no device"),
			tipcBad("ethmtu", "bearer get mtu media eth device d0", tipcEth, ack,
				"mtu is a udp property, refused for eth"),
			tipcBad("nosuchbearer", "bearer get priority media udp name nosuch", nil,
				expect{RC: intp(255), Errno: "EINVAL", Extack: "Bearer not found", RefusedBy: refKernel},
				"the kernel has no such bearer"),
			tipcBad("nosuchmedia", "media get priority media nosuch", nil,
				expect{RC: intp(255), Errno: "EINVAL", Extack: "Media not found", RefusedBy: refKernel},
				"the kernel has no such media"),
		},
	}
	sp.Build = func(vals []value) row {
		var optArg, cmdArg string
		var pre []string
		for i, val := range vals {
			pre = append(pre, val.Pre...)
			switch i {
			case 0:
				optArg = val.Arg
			case 1:
				cmdArg = val.Arg
			}
		}
		facts := mergeFacts(vals)
		r := row{
			Cmd:     strings.TrimSpace("tipc " + joinArgs(optArg, cmdArg)),
			Proto:   "generic",
			Modules: tipcModules,
			Expect:  expect{RC: intp(0), RefusedBy: refNone},
			Axes:    facts,
		}
		// tipc -j link get broadcast dies of SIGSEGV after the kernel's
		// answer: cmd_link_get_bcast_cb prints JSON with no json object
		// opened (DOC030), so the shell reports 128+11.
		if facts["words"] == "link get broadcast" && facts["json"] == "1" {
			r.Expect = expect{RC: intp(139), RefusedBy: refAckThenFail}
		}
		for _, p := range pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
		return r
	}
	// A bad value replaces one axis of the first base row: opt=plain, so a
	// bad command carries no -j, and bare's first command, so a bad option
	// runs `tipc -z node get address`.
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
