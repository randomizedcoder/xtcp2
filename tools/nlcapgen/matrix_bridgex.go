package main

import (
	"strconv"
	"strings"
)

// The bridge extras space (gobridge 6X-C): what the 6C space left out of
// bridge(8)'s read-only surface. Its rows go to their own corpus directory
// (ws/bridgex), so 6C's row ids stay as captured.
//
//   - -batch FILE, -batch - and -force (bridge/bridge.c batch, lib/utils.c
//     do_batch, getcmdline, makeargs). A batch runs every line through
//     do_cmd in one process. A line whose command returns non-zero prints
//     "Command failed FILE:LINE" and ends the batch, unless -force is
//     given. A command that calls exit() (usage, duparg, a failed dump,
//     makeargs' own refusals) ends the process whatever -force says.
//     ll_init_map dumps the links once per process, not once per line.
//   - -family, -4 and -6: only fdb's address parsing reads
//     preferred_family, so for these shows they parse and do nothing.
//   - -netns: only its refusals can be captured. Once bridge has moved to
//     another namespace, the row namespace's nlmon sees none of its
//     traffic; the switch itself is checked live (6X-L).
//   - the multicast router ports of `vlan global show`, with -s their
//     timer and type (bridge/mdb.c br_print_router_port_stats), which 6C's
//     brglob fixture (snooping off) never had.
//
// The batch file is written by the row's last pre step, which runs in the
// row's directory, so the file is kept beside the capture as b.txt.
//
// The rows pin the bugs this space adds (B28-B33 in gobridge.md): the
// per-object filter statics are never reset between batch lines (B28), so
// a second `vid` is a duparg exit (B29); a failed dump's exit(1) ends a
// -force batch (B30); a batch whose last line wants a continuation ends with
// rc 0 (B31); words after `-batch FILE` are ignored (B32); and `-netns`
// with no name opens "(null)" (B33).
//
// Determinism: as for 6C. The router-port timers of the temp router count
// down, so they may differ between batches.

// shq is s as one sh word, in single quotes.
func shq(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// batchFile is a pre step writing lines, one per line, to b.txt.
func batchFile(lines ...string) string {
	q := make([]string, len(lines))
	for i, l := range lines {
		q[i] = shq(l)
	}
	return "printf '%s\\n' " + strings.Join(q, " ") + " > b.txt"
}

// batchWords is a pre step writing one line to b.txt: words, then n more
// words "x". makeargs refuses a line of more than MAX_ARGS-1 (511) words.
func batchWords(words string, n int) string {
	return "{ printf '%s' " + shq(words) + "; i=0; while [ $i -lt " + strconv.Itoa(n) +
		" ]; do printf ' x'; i=$((i+1)); done; echo; } > b.txt"
}

// withArg is val with its words replaced, for a row whose words bb's
// shape doesn't cover.
func (val value) withArg(words string) value {
	val.Arg = words
	val.Facts["words"] = words
	return val
}

// brRtrFix is br0 with multicast VLAN snooping and router ports on its
// VLANs: d1 is a permanent router port on vid 10 (its port-wide setting
// left at the default, temp-query); d2 is a temp router port on vid 10
// (a timer runs) and a permanent one on vid 30, with its port-wide setting
// permanent. The kernel reports each port's type from its port-wide
// setting, not the VLAN's (K3), which the -s rows show.
var brRtrFix = append(clonePre(brFix),
	"ip link set br0 type bridge mcast_vlan_snooping 1",
	"bridge vlan add dev d2 vid 10",
	"bridge link set dev d2 mcast_router 2",
	"bridge vlan set dev d1 vid 10 mcast_router 2",
	"bridge vlan set dev d2 vid 10 mcast_router 3",
	"bridge vlan set dev d2 vid 30 mcast_router 2",
)

// bb is a batch value: bridge's options and -batch b.txt (or, with
// stdin, -batch - with b.txt on stdin), in fixture fix, with b.txt written
// by the last pre step.
func bb(kind brKind, fix, name, opts string, stdin bool, file string, pre []string) value {
	words := "-batch b.txt"
	if stdin {
		words = "-batch - < b.txt"
	}
	words = strings.TrimSpace(opts + " " + words)
	val := bc(kind, fix, words, append(clonePre(pre), file))
	val.Name = fix + ":batch:" + name
	val.Facts["batch"] = name
	return val
}

// bbBad is a refused batch: as bb, plain only.
func bbBad(fix, name, opts string, stdin bool, file string, pre []string, exp expect, why string) badValue {
	val := bb(brPos, fix, name, opts, stdin, file, pre)
	val.Name = "ng:" + val.Name
	return badValue{Axis: "cmd", Value: val, Expect: exp, Why: why}
}

// bridgexSpace: bridge's batch mode, its family and netns options, and
// router ports, each positive value plain and -j.
func bridgexSpace() *space {
	tool := expect{RC: intp(255), RefusedBy: refIproute2}
	tool1 := expect{RC: intp(1), RefusedBy: refIproute2}
	ack := expect{RC: intp(255), RefusedBy: refAckThenFail}
	ack1 := expect{RC: intp(1), RefusedBy: refAckThenFail}
	two := batchFile("link show", "vlan show")
	sp := &space{
		Name: "bridgex",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// Batch: one ll_init_map, then each line's own dump.
				bb(brPos, "br", "two", "", false, two, brFix),
				bb(brPos, "br", "stdin", "", true, two, brFix),
				bb(brPos, "brmst", "mst", "", false, batchFile("mst show", "vlan global show"), brMstFix),
				// getcmdline: a comment, a blank line, a continuation
				// joined to the next line, a trailing comment.
				bb(brCorner, "br", "comments", "", false,
					batchFile("# the ports", "", `link \`, "show dev d1 # one port", "vlan show dev d2"), brFix),
				// makeargs: a quoted word is one word without its quotes.
				bb(brCorner, "br", "quoted", "", false,
					batchFile(`link show dev "d1"`, `vlan show dev 'd2'`), brFix),
				// -force with nothing failing changes nothing.
				bb(brPos, "br", "force", "-force", false, two, brFix),
				// B28: each object's filters are file statics, set by a
				// line and never cleared, so the next line inherits them.
				bb(brCorner, "br", "sticky-vlan-dev", "", false, batchFile("vlan show dev d1", "vlan show"), brFix),
				bb(brCorner, "br", "sticky-global-dev", "", false,
					batchFile("vlan show dev d1", "vlan global show"), brFix),
				bb(brCorner, "br", "sticky-link-dev", "", false, batchFile("link show dev d1", "link show"), brFix),
				bb(brCorner, "br", "sticky-vid", "", false, batchFile("vlan show vid 10", "vlan show"), brFix),
				bb(brCorner, "brmst", "sticky-mst-dev", "", false, batchFile("mst show dev d1", "mst show"), brMstFix),
				// B31: the last line asks for a continuation that never
				// comes; the batch ends there with rc 0.
				bb(brCorner, "br", "no-continuation", "", false, batchFile("link show", `vlan show \`), brFix),
				// B32: words after -batch FILE are ignored.
				bb(brCorner, "br", "extra-words", "", false, batchFile("vlan show dev d1"), brFix).
					withArg("-batch b.txt link show"),
				// makeargs' limit: 511 words is the most a line holds.
				bb(brBoundary, "br", "511-words", "", false, batchWords("link show", 509), brFix),
				// -family, -4 and -6 parse and change nothing here.
				bc(brPos, "br", "-family inet link show", brFix),
				bc(brPos, "br", "-family inet6 vlan show", brFix),
				bc(brCorner, "br", "-4 vlan show", brFix),
				bc(brCorner, "br", "-6 link show", brFix),
				bc(brPos, "br", "-force link show", brFix),
				// brrtr: router ports per VLAN, -s adds timer and type.
				bc(brPos, "brrtr", "vlan global show", brRtrFix),
				bc(brPos, "brrtr", "-s vlan global show", brRtrFix),
				bc(brPos, "brrtr", "-s vlan global show dev br0 vid 10", brRtrFix),
				bc(brCorner, "brrtr", "-s -o vlan global show", brRtrFix), // oneline: \ for each newline
			}},
		},
		Bad: []badValue{
			// Batch failures.
			bbBad("br", "fail-stops", "", false, batchFile("badobj", "link show"), brFix, tool1,
				"an unknown object returns -1: Command failed b.txt:1, and the batch stops before any request"),
			bbBad("br", "fail-force", "-force", false, batchFile("badobj", "link show"), brFix, ack1,
				"with -force the batch goes on to link show, and still exits 1"),
			bbBad("br", "fail-force-stdin", "-force", true, batchFile("badobj", "link show"), brFix, ack1,
				"from stdin the failure is reported as -:1"),
			bbBad("br", "nodev-force", "-force", false, batchFile("vlan show dev nosuch", "vlan show"), brFix,
				expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				"nodev returns -1 and -force goes on; the next line is unfiltered, since filter_index was set to 0"),
			bbBad("br", "mst-force", "-force", false, batchFile("mst show", "link show"), brFix, ack1,
				"mst show returns -1 on B10's abort, so -force goes on to link show"),
			bbBad("br", "sticky-vid-duparg", "-force", false, batchFile("vlan show vid 10", "vlan show vid 20"), brFix, ack,
				"the first vid is still set, so the second line is a duparg, which exits 255 through -force (B29)"),
			bbBad("br", "dump-exit-force", "-force", false, batchFile("vni show dev d1", "link show"), brFix,
				expect{RC: intp(1), Errno: "EINVAL", Extack: "The device is not a vxlan device", RefusedBy: refKernel},
				"a failed vni dump calls exit(1), ending the batch whatever -force says (B30)"),
			bbBad("br", "help-force", "-force", false, batchFile("link help", "link show"), brFix, ack,
				"link help's usage calls exit(-1), ending the batch"),
			bbBad("br", "unterminated", "", false, batchFile(`link show dev "d1`, "link show"), brFix, tool1,
				"makeargs: an unterminated quote exits 1 before the line runs"),
			bbBad("br", "512-words", "", false, batchWords("link show", 510), brFix, tool1,
				"makeargs: a 512th word exits 1"),
			// Options.
			brBad("bare", "-batch nosuch", nil, tool1, "the batch file cannot be opened"),
			brBad("bare", "-batch", nil, tool, "-batch with no file prints the usage"),
			brBad("bare", "-netns nosuch link show", nil, tool, "no such named namespace"),
			brBad("bare", "-netns", nil, tool,
				"main's NEXT_ARG counts argv[0], so -netns with no name opens \"(null)\" (B33)"),
			brBad("bare", "-family", nil, tool, "-family with no value prints the usage"),
			brBad("bare", "-family help", nil, tool, "-family help prints the usage"),
			brBad("bare", "-family ipx link show", nil, tool, "an unknown family is an invalid argument"),
		},
	}
	sp.Build = bridgeSpace().Build
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
