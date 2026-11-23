package main

// The mptcp space (W5-mptcp): `ip mptcp`'s read-only endpoint and limits
// shows over generic netlink's "mptcp_pm" family. ip's side is
// ip/ipmptcp.c (iproute2): do_mptcp resolves the family with
// genl_init_handle (one CTRL_CMD_GETFAMILY) BEFORE it dispatches, so a bad
// sub-command is an ack-then-fail — except `ip mptcp help`, which usage()
// refuses before the family is resolved, with no netlink at all.
//
//   - `ip mptcp endpoint show` (and bare `ip mptcp endpoint`) sends one
//     MPTCP_PM_CMD_GET_ADDR with NLM_F_DUMP; the kernel answers one
//     NLM_F_MULTI message per endpoint, in id order, then NLMSG_DONE.
//   - `ip mptcp endpoint show id N` sends one GET_ADDR (no dump) whose
//     nested MPTCP_PM_ATTR_ADDR carries only the id; an absent id is the
//     kernel's EINVAL "address not found".
//   - `ip mptcp limits show` (and bare `ip mptcp limits`) sends one
//     MPTCP_PM_CMD_GET_LIMITS.
//
// Both endpoint paths call ll_init_map(&rth), so ip also dumps the links on
// its rtnetlink socket to name an endpoint's dev: the dump path does so on
// the first endpoint it prints (an empty dump never does), the `show id`
// path before it sends anything (in mptcp_parse_opt). So the rows capture
// on generic+route: the RTM_GETLINK dump is held alongside the genl
// traffic, and a replay can name the dev from it as ip did. (A limits row
// never touches rtnetlink, so it captures nothing extra.)
//
// The kernel's side is net/mptcp/pm_netlink.c, pm_kernel.c and the
// generated mptcp_pm_gen.c. The family is netnsok=true and its endpoints
// and limits are per-netns (pm_nl_pernet), so each row answers in its own
// fresh namespace: no init-namespace mode, no serial execution.
// mptcp_nl_fill_addr always emits FAMILY (u16), PORT, ID and FLAGS, plus
// IF_IDX when set and the family's ADDR4 or ADDR6; ip prints port only
// when non-zero and dev only when the ifindex resolves.
//
// MPTCP is a bool Kconfig (built in, never a module), so the rows load no
// modules. The topology is a dummy d0 holding the endpoints' addresses, so
// a signal endpoint with a port can bind its listener; it is d0's ifindex
// (2, after lo) that a `dev d0` endpoint carries, fixed across boots.
//
// This space is read-only: the endpoint and limits setup runs in `pre`,
// outside the capture window; no add/change/delete/flush/set is captured,
// and no monitor (it never exits). The clone (internal/gomptcp) replays
// these captures and must reproduce `ip mptcp`'s stdout byte for byte.

// mptcpTopo is the namespace's dummy device and the addresses the
// endpoints use.
var mptcpTopo = []string{
	"ip link add d0 type dummy",
	"ip link set d0 up",
	"ip addr add 192.0.2.1/24 dev d0",
	"ip addr add 192.0.2.2/24 dev d0",
	"ip addr add 192.0.2.3/24 dev d0",
	"ip -6 addr add 2001:db8::1/64 dev d0 nodad",
}

// mptcpEndpoints is the multi fixture's endpoints, one per print branch:
// a plain v4 subflow; a v6 signal bound to d0 (dev, ADDR6); a v4 signal
// with a port and backup (port, two flags in table order); and a v4
// subflow fullmesh. ids are explicit so the dump order (id order) is
// fixed.
var mptcpEndpoints = []string{
	"ip mptcp endpoint add 192.0.2.1 id 1 subflow",
	"ip mptcp endpoint add 2001:db8::1 id 2 dev d0 signal",
	"ip mptcp endpoint add 192.0.2.2 id 3 port 10000 signal backup",
	"ip mptcp endpoint add 192.0.2.3 id 4 subflow fullmesh",
}

// mptcpWith joins setup step lists into one pre.
func mptcpWith(steps ...[]string) []string {
	var pre []string
	for _, s := range steps {
		pre = append(pre, s...)
	}
	return pre
}

// mptcpFix is a fixture value: its name, the `ip mptcp` words after the
// options, and the setup its namespace needs. The same words over
// different setups capture different replies.
func mptcpFix(name, words string, pre []string) value {
	val := v(name, words, f("words", words, "fix", name))
	val.Pre = pre
	return val
}

// mptcpSpace: `ip mptcp` endpoint and limits shows, plain and as JSON.
func mptcpSpace() *space {
	multi := mptcpWith(mptcpTopo, mptcpEndpoints)
	sp := &space{
		Name: "mptcp",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "fix", Values: []value{
				// No endpoints: the dump is NLMSG_DONE alone, and ip
				// never initializes its link map (no RTM_GETLINK).
				mptcpFix("ep-empty", "endpoint show", nil),
				// One v4 subflow, no dev: one reply, and the link
				// dump ll_init_map does on the first print.
				mptcpFix("ep-one", "endpoint show",
					mptcpWith(mptcpTopo, mptcpEndpoints[:1])),
				// Four endpoints covering address families, dev,
				// port and every flag combination ip prints.
				mptcpFix("ep-multi", "endpoint show", multi),
				// A single GET_ADDR by id: the v6 endpoint bound to
				// d0. ip dumps the links before it sends.
				mptcpFix("ep-id", "endpoint show id 2", multi),
				// Bare `endpoint` is a show: do_mptcp calls
				// mptcp_addr_show(0, NULL) when nothing follows.
				mptcpFix("ep-bare", "endpoint", multi),
				// The namespace's default limits.
				mptcpFix("lim-default", "limits show", nil),
				// Limits set in pre, then read back.
				mptcpFix("lim-set", "limits show",
					[]string{"ip mptcp limits set subflows 4 add_addr_accepted 3"}),
				// Bare `limits` is a show (GET_LIMITS).
				mptcpFix("lim-bare", "limits", nil),
				// `limits show subflows 3`: mptcp_parse_limit adds
				// MPTCP_PM_ATTR_SUBFLOWS to the GET_LIMITS request;
				// the kernel's GET_LIMITS policy accepts it and the
				// doit ignores it, so this succeeds and prints the
				// unchanged limits. A set spelled into a read.
				mptcpFix("lim-showarg", "limits show subflows 3", nil),
			}},
		},
		Bad: []badValue{
			// `ip mptcp help`: usage() before genl_init_handle, exit(-1),
			// no netlink at all.
			{Axis: "fix", Value: v("help", "help", nil),
				Expect: expect{RC: intp(255), RefusedBy: refIproute2},
				Why:    "help is refused before the family is resolved"},
			// `ip mptcp badobj`: the family is resolved, then do_mptcp's
			// unknown label exits 255.
			{Axis: "fix", Value: v("badobj", "badobj", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "an unknown object, refused after the family is resolved"},
			// `ip mptcp endpoint junk`: no endpoint sub-command matches,
			// goto unknown, exit 255.
			{Axis: "fix", Value: v("ep-junk", "endpoint junk", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "an unknown endpoint sub-command"},
			// `ip mptcp endpoint show junk`: junk is no flag or keyword,
			// so mptcp_parse_opt hands it to get_addr, which prints "any
			// valid address is expected" and exits 1 itself — the
			// parser's own invarg("unknown argument") is never reached.
			// The link dump (ll_init_map) has already run.
			{Axis: "fix", Value: v("ep-showjunk", "endpoint show junk", nil),
				Expect: expect{RC: intp(1), RefusedBy: refAckThenFail},
				Why:    "show's argument is not an id, flag or address"},
			// `ip mptcp endpoint show id 0`: mptcp_parse_opt refuses id 0
			// for anything but delete ("cannot be 0"), exit 255.
			{Axis: "fix", Value: v("ep-id0", "endpoint show id 0", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "id 0 is only valid for delete"},
			// `ip mptcp endpoint show id 9` with no endpoint 9: the
			// kernel's mptcp_pm_nl_get_addr finds no entry, EINVAL with
			// extack "address not found"; rtnl_talk fails, rc 2.
			{Axis: "fix", Value: v("ep-id-missing", "endpoint show id 9", nil),
				Expect: expect{RC: intp(2), Errno: "EINVAL", Extack: "address not found", RefusedBy: refKernel},
				Why:    "the kernel has no endpoint with that id"},
			// `ip mptcp limits show junk`: mptcp_parse_limit's "unknown
			// limit", exit 255, before GET_LIMITS is sent.
			{Axis: "fix", Value: v("lim-showjunk", "limits show junk", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "limits show's argument is not a limit"},
			// `ip mptcp limits junk`: the limits block has no unknown
			// branch, so control falls to the monitor test (no match)
			// and then unknown, exit 255. (`limits monitor` would fall
			// into the monitor and never exit, so it is not captured.)
			{Axis: "fix", Value: v("lim-junk", "limits junk", nil),
				Expect: expect{RC: intp(255), RefusedBy: refAckThenFail},
				Why:    "an unknown limits sub-command"},
		},
	}
	sp.Build = func(vals []value) row {
		var optArg, fixArg string
		var pre []string
		for i, val := range vals {
			pre = append(pre, val.Pre...)
			switch i {
			case 0:
				optArg = val.Arg
			case 1:
				fixArg = val.Arg
			}
		}
		r := row{
			Cmd:    "ip " + joinArgs(optArg, "mptcp", fixArg),
			Proto:  "generic+route",
			Expect: expect{RC: intp(0), RefusedBy: refNone},
			Axes:   mergeFacts(vals),
		}
		for _, p := range pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
		return r
	}
	// The bad values change the fix axis, so a base with opt=plain keeps
	// the bad words at the end of the line.
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
