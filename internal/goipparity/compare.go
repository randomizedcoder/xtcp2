package goipparity

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The comparator half of the parity harness.
//
// # What this does not do
//
// It does not capture. The guest's xtcp2-nlcap already knows how to make a
// namespace, open a capture with a datagram floor, close the window with an
// `ss -x` NETLINK_SOCK_DIAG sentinel, and discard a short capture — and the
// measurements behind that are not reproducible by rewriting it in Go. In
// particular libpcap's TPACKET_V3 block-retire timer swallowed 16 of 18
// captures until --immediate-mode went in, which is the sort of fact that gets
// lost in a reimplementation and then costs a day to rediscover.
//
// So the shell captures and this compares. That division is also what keeps
// the plan's Risk 8 honest: cmd/goip-parity never opens a socket, so no
// refactor can put netlink traffic inside a capture window and attribute it to
// goip. TestNoSocketCalls enforces it rather than trusting the comment.
//
// # What a directory looks like
//
// One flat directory per run, with six files per command:
//
//	<slug>.ip_a.pcap  <slug>.goip.pcap  <slug>.ip_b.pcap
//	<slug>.ip_a.out   <slug>.goip.out   <slug>.ip_b.out
//
// A command whose files are absent is MISSING and fails. That is deliberate
// and it is the difference between a gate and a decoration: a silently skipped
// command reads exactly like a passing one, so the only safe default is to
// treat an absent capture as a failure and make the driver say why.

// Status is one command's outcome.
type Status string

// The five outcomes. Strings rather than an int enum because they are printed
// into a report a shell greps.
const (
	// StatusPass is a clean comparison: no findings at all, suppressed or
	// otherwise, on either half.
	StatusPass Status = "PASS"
	// StatusWarn is a comparison that produced findings for a command that is
	// not yet in the allowlist's gated_commands.
	//
	// This exists because the alternative is a lie. The gate-one-at-a-time
	// discipline means an ungated command's findings must not end the build —
	// that is the whole point of gated_commands, and it is what lets a command
	// be captured and reported before its divergences have been explained. But
	// labeling such a command PASS is indefensible: a measured run with the L1
	// transaction count differing, ip=1 against goip=2, printed
	// "GOIP_PARITY_PASS link_show" with the divergence on the very next line.
	// That is the failure mode the whole harness exists to prevent, reproduced
	// inside the harness's own report.
	//
	// So the verdict and the report are separated: WARN does not fail the run
	// (Failed returns false for it), and it does not claim parity either.
	StatusWarn Status = "WARN"
	// StatusFail is a comparison that produced gating findings, or a capture
	// that was unusable.
	StatusFail Status = "FAIL"
	// StatusSkip is a command goip does not implement yet. Distinct from
	// MISSING on purpose: the same distinction internal/goip draws between
	// ErrNotImplemented and ErrUnknownObject, and for the same reason — "not
	// there yet" and "something is wrong" need different reactions.
	StatusSkip Status = "SKIP"
	// StatusMissing is an implemented command whose files were not all there.
	StatusMissing Status = "MISSING"
)

// Result is one command's comparison.
type Result struct {
	// Command is the table row.
	Command Command
	// Status is the outcome.
	Status Status
	// Netlink is the wire comparison. Zero on SKIP and MISSING.
	Netlink nlparity.Report
	// Stdout is the structural render comparison. Zero on SKIP and MISSING.
	Stdout StdoutReport
	// Absent lists the files that were expected and not found, on MISSING.
	Absent []string
	// Err is a load or parse failure.
	Err error
}

// Failed reports whether this result should fail the run.
func (r Result) Failed() bool {
	return r.Status == StatusFail || r.Status == StatusMissing
}

// ErrNoCommands is returned when a directory holds nothing the table knows
// about, which almost always means the driver wrote somewhere else.
var ErrNoCommands = errors.New("goipparity: no command produced a comparison")

// CompareDir compares every command in the table against the captures in dir.
//
// al may be nil, which suppresses nothing and gates nothing.
func CompareDir(dir string, al *nlparity.Allowlist) []Result {
	cmds := Commands()
	out := make([]Result, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, compareOne(dir, c, al))
	}
	return out
}

// compareOne is one command's comparison, including the decision about which
// status it gets.
func compareOne(dir string, c Command, al *nlparity.Allowlist) Result {
	r := Result{Command: c, Status: StatusPass}

	if !c.Implemented {
		r.Status = StatusSkip
		// A goip capture for a command goip cannot run means the driver ran
		// something the table says is unimplemented, so one of the two is
		// stale. Reported as a failure rather than ignored: the table is what
		// decides whether a command is compared, and a stale table is how a
		// command stops being compared without anyone noticing.
		if p := filepath.Join(dir, c.CaptureName(SideGoip)); fileExists(p) {
			r.Status = StatusFail
			r.Err = fmt.Errorf("%q is marked unimplemented but %s exists; the "+
				"command table and the capture driver disagree", c.Name,
				c.CaptureName(SideGoip))
		}
		return r
	}

	// Every file, named before any of them is read, so the report can list
	// all the absentees rather than only the first.
	var absent []string
	for _, side := range Sides() {
		for _, name := range []string{c.CaptureName(side), c.StdoutName(side)} {
			if !fileExists(filepath.Join(dir, name)) {
				absent = append(absent, name)
			}
		}
	}
	if len(absent) != 0 {
		r.Status = StatusMissing
		r.Absent = absent
		return r
	}

	segs := map[string]nlparity.Segmentation{}
	for _, side := range Sides() {
		path := filepath.Join(dir, c.CaptureName(side))
		// Not named `cap`: that shadows a builtin, which gocritic flags and
		// which would make a later `cap(x)` in this function mean something
		// surprising.
		capture, err := nlparity.ParseRouteCaptureFile(path)
		if err != nil {
			r.Status = StatusFail
			r.Err = fmt.Errorf("%s: %w", c.CaptureName(side), err)
			return r
		}
		segs[side] = nlparity.SegmentCapture(capture)
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

	// D_control is the diff between the two REFERENCE captures. Taken before
	// the test diff and passed into Compare, which subtracts it — a difference
	// visible across a window containing the goip run is by construction not
	// attributable to goip.
	control := nlparity.Diff(c.Name, segs[SideIPA], segs[SideIPB])
	r.Netlink = nlparity.Compare(c.Name, segs[SideIPA], segs[SideGoip], control, al)
	r.Stdout = CompareStdoutWithControl(
		c.Name, texts[SideIPA], texts[SideGoip], texts[SideIPB], al)

	// nlparity.Report.Failed already folds gating in, and fails regardless of
	// it on an unusable capture or a hygiene finding. The stdout half does
	// not: StdoutReport.Failed means "there are findings", and whether that
	// ends the build is decided here, once, so the gate-one-at-a-time
	// discipline lives in a single place rather than in two halves that could
	// disagree about which commands it covers.
	//
	// WARN is decided after FAIL and only if FAIL was not reached, so a gated
	// command with findings is never downgraded to a warning.
	gated := al != nil && al.IsGated(c.Name)
	switch {
	case r.Netlink.Failed() || (gated && r.Stdout.Failed()):
		r.Status = StatusFail
	case len(r.Netlink.Findings) != 0 || len(r.Stdout.Findings) != 0:
		r.Status = StatusWarn
	}
	return r
}

// fileExists is a presence probe that does not care why a stat failed.
//
// A permission error and an absence are both "this run cannot read the file",
// and the caller's next move is the same either way: report it as missing and
// fail. Distinguishing them would produce two messages with one remedy.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Render writes the report and returns whether the run passed.
//
// The sentinels are the plan's, and they are the interface the guest driver
// greps — an expect script matching GOIP_PARITY_OVERALL_PASS is a great deal
// more robust than one parsing counts out of prose.
func Render(w io.Writer, results []Result) bool {
	var (
		failed       bool
		controlTotal int
		hygiene      int
		warned       int
		// compared counts the commands that actually got a comparison, so a
		// run that compared nothing can be told apart from a clean one. Counted
		// in this loop rather than a second pass, because a second pass over
		// the same slice is a second thing to keep in step with the statuses.
		compared int
	)

	// Indexed rather than ranged by value: Result embeds two reports and is
	// several hundred bytes, which gocritic's rangeValCopy is right to object to.
	for i := range results {
		r := &results[i]
		c := &r.Command
		switch r.Status {
		case StatusSkip:
			fmt.Fprintf(w, "GOIP_PARITY_SKIP %s (%s): goip does not implement it yet\n",
				c.Slug, c.Name)
		case StatusMissing:
			sort.Strings(r.Absent)
			fmt.Fprintf(w, "GOIP_PARITY_MISSING %s (%s): %v\n", c.Slug, c.Name, r.Absent)
		default:
			fmt.Fprintf(w, "GOIP_PARITY_%s %s (%s)\n", r.Status, c.Slug, c.Name)
		}

		if r.Err != nil {
			fmt.Fprintf(w, "  error: %v\n", r.Err)
		}
		if r.Status == StatusSkip || r.Status == StatusMissing {
			if r.Failed() {
				failed = true
			}
			continue
		}

		compared++
		controlTotal += r.Netlink.ControlSize + r.Stdout.ControlSize
		hygiene += len(r.Netlink.Hygiene)
		if r.Status == StatusWarn {
			warned++
		}

		// The L1 sentinel is printed whether or not it differs, because "both
		// sides ran 2 transactions" is the statement that makes a clean
		// report believable.
		fmt.Fprintf(w, "  txns: ip=%d goip=%d  pids: ip=%v goip=%v  control: nl=%d stdout=%d\n",
			r.Netlink.RefTxns, r.Netlink.SubTxns,
			r.Netlink.RefPids, r.Netlink.SubPids,
			r.Netlink.ControlSize, r.Stdout.ControlSize)

		// No "hygiene: " prefix here, unlike the lines below: a hygiene
		// divergence renders its own level as the word `hygiene`, so a prefix
		// would print it twice.
		for _, d := range r.Netlink.Hygiene {
			fmt.Fprintf(w, "  %s\n", d)
		}
		for _, d := range r.Netlink.Findings {
			fmt.Fprintf(w, "  finding: %s\n", d)
		}
		for _, d := range r.Stdout.Findings {
			fmt.Fprintf(w, "  finding: %s\n", d)
		}
		// Suppressed findings are printed, not hidden. An allowlist nobody
		// reads is an allowlist that accumulates, and a large control is the
		// plan's signal to distrust the whole run.
		for _, d := range r.Netlink.ControlSuppressed {
			fmt.Fprintf(w, "  control-suppressed: %s\n", d)
		}
		for _, d := range r.Stdout.ControlSuppressed {
			fmt.Fprintf(w, "  control-suppressed: %s\n", d)
		}
		for _, d := range r.Netlink.AllowSuppressed {
			fmt.Fprintf(w, "  allow-suppressed: %s\n", d)
		}
		for _, d := range r.Stdout.AllowSuppressed {
			fmt.Fprintf(w, "  allow-suppressed: %s\n", d)
		}

		if r.Failed() {
			failed = true
		}
	}

	// A run that compared nothing is a failed run. Without this an empty
	// directory reports every sentinel green, which is the worst possible
	// output: it is indistinguishable from a passing run and it means the
	// driver wrote its captures somewhere else.
	if compared == 0 {
		fmt.Fprintf(w, "GOIP_PARITY_NOTHING_COMPARED: %v\n", ErrNoCommands)
		failed = true
	}

	if hygiene == 0 {
		fmt.Fprintln(w, "GOIP_PARITY_HYGIENE_PASS")
	} else {
		fmt.Fprintf(w, "GOIP_PARITY_HYGIENE_FAIL %d\n", hygiene)
	}
	if controlTotal == 0 {
		fmt.Fprintln(w, "GOIP_PARITY_CONTROL_CLEAN")
	} else {
		fmt.Fprintf(w, "GOIP_PARITY_CONTROL_NOISY %d\n", controlTotal)
	}
	// Warnings get a sentinel of their own so the guest driver can surface
	// "passed, with N ungated divergences" without parsing the per-command
	// lines. A run that is OVERALL_PASS and has warnings is exactly the state
	// gated_commands is supposed to be walked out of, and it should be visible
	// from the last four lines of the log rather than only from the middle.
	if warned == 0 {
		fmt.Fprintln(w, "GOIP_PARITY_UNGATED_CLEAN")
	} else {
		fmt.Fprintf(w, "GOIP_PARITY_UNGATED_DIVERGENCES %d\n", warned)
	}
	if failed {
		fmt.Fprintln(w, "GOIP_PARITY_OVERALL_FAIL")
		return false
	}
	fmt.Fprintln(w, "GOIP_PARITY_OVERALL_PASS")
	return true
}
