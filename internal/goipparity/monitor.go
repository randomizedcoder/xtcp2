//go:build goip_write

package goipparity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The monitor rows' comparison — the EG1 row kind (docs/netlink/
// goip-parity-events.md, EG1-T01). A monitor row is the inverse of a dump
// row: the notification is the point, so the driver records no pcap and
// applies no datagram floor — the until-EREs are the row's own floor, the
// helper's own rc — and this reads the three sides' stdout through the same
// facet machinery the dumps' stdout half uses, after collapsing the one
// noise class that machinery cannot see for itself.
//
// # The three noise classes (V391's measurements)
//
//  1. The canary retry's pair. The helper joins 224.0.0.250 on the canary
//     device until the monitor prints it, deleting and rejoining while it
//     does not, and whichever side the race hits prints the extra `Deleted …`
//     and re-join blocks. The retry count is the race itself, so it cannot be
//     a finding: collapseCanary keeps each side's FIRST canary block — the
//     join, the readiness signal, whose rendered text IS compared — and
//     drops the retry's twins.
//  2. The random veth MAC and its derived addresses. Not this file's
//     problem, and deliberately so: the rows' Setup pins the veth MACs (the
//     corpus's own device rule, the addr corpus's 02:00:00:00:d0:00 dummy),
//     so the MAC facet and the solicited-node multicast line are comparable
//     as they stand. A goip that prints a MAC the row did not pin is a
//     finding, which is the point of pinning.
//  3. The carrier transient. The trigger's `down; up` settles
//     asynchronously, so the first stanza after the up prints `state
//     LOWERLAYERDOWN` with `NO-CARRIER` when the carrier had not returned and
//     `state UP` when it had. That is D_control: when the two reference
//     captures catch each shape, the state finding is in the control and
//     SubtractStdout removes it. When the controls agree and goip caught the
//     other shape, the finding stands — the plan's own boundary, "the goip
//     row's shape is accepted while it lies inside the controls' own
//     disagreement" — and the row goes to the findings rule rather than to a
//     suppression, because state is a Value-class keyword and a suppression
//     would also hide the real state render bug the corpus exists to catch.
//
// # The floor, which is not a datagram count
//
// The helper exits 1 when the canary never printed or an until-ERE never
// matched, and that rc is the row's floor. But it also exits 1 when the
// trigger itself failed — the refusal rows, whose expect is the helper's own
// rc on every side — so the rc's value cannot be the rule. Two are, both
// read here:
//
//   - the helper's own complaint, on any side: `xtcp2-nl-ipmon: …` on
//     stderr is a canary that never printed or an ERE that never matched,
//     and the row is a FAIL naming it. The trigger's own stderr is not the
//     helper's complaint and lands in the same .err unflagged.
//   - the three sides' rc must agree, whatever the value: the kernel
//     refusing goip's trigger but not `ip`'s, or the reverse, is a real
//     divergence even when both stdouts are only canary blocks.

// canaryAddrCst is the helper's canary (netlink-capture.nix's ipmon): the
// group whose join makes the monitor's subscription observable. Named here so
// collapseCanary and the helper cannot drift apart silently.
const canaryAddrCst = "224.0.0.250"

// monitorComplaintCst is the prefix the helper puts on its own complaints
// (the canary that never printed, an until-ERE that never matched), which is
// the floor the plan gives the row. A trigger's own failure is not the
// helper's complaint, which is what makes the refusal rows legal.
const monitorComplaintCst = "xtcp2-nl-ipmon: "

// ErrMonitorFloor is a monitor row whose helper complained on stderr: the
// canary never printed, or an until-ERE never matched.
var ErrMonitorFloor = fmt.Errorf("goipparity: monitor row floor")

// ErrMonitorSide is a monitor row whose three sides' helper rc disagree.
var ErrMonitorSide = fmt.Errorf("goiparity: monitor row exit status")

// collapseCanary keeps the last canary block and drops the rest.
//
// A block is a stanza line — column zero, `N: …` or `Deleted N: …` — plus
// the indented lines that follow it, which is how the monitor's own output
// is shaped. A canary block is one whose stanza line names the canary
// address; only the helper's canary join and its retry ever print one, and
// the last is the readiness join: the helper joins the canary, waits one
// second for the monitor to print it, and deletes and rejoins it when it
// has not, so the join the loop broke on is always the last canary block,
// the shapes the race prints being the join alone (a monitor subscribed in
// time, V391's no-retry run) and the Deleted pair (the retry — the last
// block is the re-join, whose stanza the first run's WARN held absent).
// The earlier twins are dropped, not the join: the join's own rendered text
// is the comparison's to make, and a goip that printed the canary block
// differently must be a finding.
func collapseCanary(text string) string {
	lines := strings.Split(text, "\n")
	last := -1
	for i := 0; i < len(lines); i++ {
		if isCanaryStanza(lines[i]) {
			last = i
		}
	}
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if !isCanaryStanza(lines[i]) {
			out = append(out, lines[i])
			continue
		}
		// A canary block: itself and its continuation lines. The last
		// block's stay and every earlier one's go.
		j := i
		for j+1 < len(lines) && isContinuation(lines[j+1]) {
			j++
		}
		if i == last {
			for ; i <= j; i++ {
				out = append(out, lines[i])
			}
			i--
		} else {
			i = j
		}
	}
	return strings.Join(out, "\n")
}

// isCanaryStanza reports whether the line is a canary block's stanza line.
// The `mcast ` form is print_addrinfo's, on both families (`inet mcast` and
// `inet6 mcast`), and only the helper's canary ever carries this address.
// A stanza opens at column zero, which is what tells the join's own line
// apart from a continuation line that happened to name the address.
func isCanaryStanza(line string) bool {
	return !isContinuation(line) &&
		strings.Contains(line, " mcast "+canaryAddrCst+" ")
}

// isContinuation reports whether the line belongs to the stanza above it: a
// stanza opens at column zero and its lines are indented, which is the
// monitor's own format.
func isContinuation(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// CompareMonitorWithControl is the monitor rows' stdout comparison: the
// canary retry collapsed on each side, then the dumps' own comparison — the
// facets, D_control, the allowlist — unchanged.
func CompareMonitorWithControl(
	command, ipA, goip, ipB string, al *nlparity.Allowlist,
) StdoutReport {
	return CompareStdoutWithControl(command,
		collapseCanary(ipA), collapseCanary(goip), collapseCanary(ipB), al)
}

// compareMonitorRow is one monitor row's whole comparison: the floor, then
// the stdout. The netlink half is zero by construction — there is no pcap to
// parse — and the status is decided exactly as a read row decides it.
func compareMonitorRow(dir string, c Command, al *nlparity.Allowlist) Result {
	r := Result{Command: c, Status: StatusPass, MonitorRC: ""}

	rc := map[string]string{}
	for _, side := range Sides() {
		b, err := os.ReadFile(filepath.Join(dir, c.RCName(side)))
		if err != nil {
			r.Status = StatusFail
			r.Err = fmt.Errorf("%s: %w", c.RCName(side), err)
			return r
		}
		rc[side] = strings.TrimSpace(string(b))
	}
	r.MonitorRC = rc[SideGoip]

	// The floor: the helper's own complaint, on any side. Read line by line
	// rather than as a prefix so the report names the complaint itself,
	// which is what the operator needs to know.
	for _, side := range Sides() {
		b, err := os.ReadFile(filepath.Join(dir, c.ErrName(side)))
		if err != nil {
			r.Status = StatusFail
			r.Err = fmt.Errorf("%s: %w", c.ErrName(side), err)
			return r
		}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, monitorComplaintCst) {
				r.Status = StatusFail
				r.Err = fmt.Errorf("%w: %s: %s", ErrMonitorFloor, side, line)
				return r
			}
		}
	}

	// The refusal rows' own rule: the helper's rc is the row's expect, so
	// the value is the row's to choose and only the agreement is this's to
	// check. A kernel that refused one tool's trigger and not the other's is
	// a divergence no stdout comparison would name — both stdouts would be
	// canary blocks alone.
	if rc[SideIPA] != rc[SideGoip] || rc[SideIPA] != rc[SideIPB] {
		r.Status = StatusFail
		r.Err = fmt.Errorf("%w: ip_a=%s goip=%s ip_b=%s",
			ErrMonitorSide, rc[SideIPA], rc[SideGoip], rc[SideIPB])
		return r
	}

	texts := map[string]string{}
	for _, side := range Sides() {
		b, err := os.ReadFile(filepath.Join(dir, c.StdoutName(side)))
		if err != nil {
			r.Status = StatusFail
			r.Err = fmt.Errorf("%s: %w", c.StdoutName(side), err)
			return r
		}
		texts[side] = string(b)
	}
	r.Stdout = CompareMonitorWithControl(
		c.Name, texts[SideIPA], texts[SideGoip], texts[SideIPB], al)

	gated := al != nil && al.IsGated(c.Name)
	switch {
	case gated && r.Stdout.Failed():
		r.Status = StatusFail
	case len(r.Stdout.Findings) != 0:
		r.Status = StatusWarn
	}
	return r
}
