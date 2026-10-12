// Package gordmaparity is the comparator half of gordma's live parity run.
//
// The guest runs each command three times under strace, as `rdma`, gordma,
// then `rdma` again (nix/microvms/scripts/gordma-parity.exp), and then
// gordma once more replaying the first `rdma` capture. CompareDir reads those
// files and reports, per command, whether gordma sent rdma's requests and
// printed rdma's bytes. The second `rdma` run is the control: a difference
// the two `rdma` runs share was the kernel's, not gordma's.
//
// It never opens a socket (TestParityCannotOpenASocket), so it cannot put
// traffic into a capture, and it owns the command table the driver iterates,
// so a command the driver stopped running shows up as MISSING.
package gordmaparity

import (
	"fmt"
	"strings"
)

// Phase is when the driver runs a command: before the holder processes
// start, or while they hold their QPs, CQs, MRs, SRQ and CM ID.
type Phase string

const (
	PhaseIdle Phase = "idle"
	PhaseBusy Phase = "busy"
)

// Kind is how the driver runs a command.
type Kind string

const (
	// KindPlain is one invocation under `xtcp2-nlcap strace`.
	KindPlain Kind = "plain"
	// KindMonitor is `xtcp2-nlcap strace-monitor`: the tool listens while
	// rxe1 is added and deleted, and is stopped by SIGTERM.
	KindMonitor Kind = "monitor"
)

// CQNPlaceholder in Args is replaced by the driver with the first
// user-owned CQ number in res_cq_user's rdma_a output.
const CQNPlaceholder = "<cqn>"

// Sides of a row, in the order the driver runs them.
const (
	SideRdmaA  = "rdma_a"
	SideGordma = "gordma"
	SideRdmaB  = "rdma_b"
	// SideReplay is gordma replaying the rdma_a capture
	// (GORDMA_REPLAY), so it has .out, .err and .rc but no .strace.
	SideReplay = "replay"
)

// Sides are the three captured runs, in order.
func Sides() []string { return []string{SideRdmaA, SideGordma, SideRdmaB} }

// Command is one row: the corpus capture of the same slug
// (pkg/xtcpnl/testdata/<kernel>/rdma, nix/microvms/scripts/capture-rdma.exp)
// ran these Args.
type Command struct {
	Slug        string
	Phase       Phase
	Implemented bool
	Kind        Kind
	Args        []string
}

// String is the driver's line: slug, phase, implemented (yes/no), kind and
// the space-joined args, tab-separated.
func (c Command) String() string {
	impl := "no"
	if c.Implemented {
		impl = "yes"
	}
	return strings.Join([]string{c.Slug, string(c.Phase), impl, string(c.Kind), strings.Join(c.Args, " ")}, "\t")
}

// FileName is <slug>.<side>.<ext>, the name xtcp2-nlcap gives a side's
// file.
func (c Command) FileName(side, ext string) string {
	return fmt.Sprintf("%s.%s.%s", c.Slug, side, ext)
}

func row(slug string, phase Phase, args string) Command {
	return Command{Slug: slug, Phase: phase, Implemented: true, Kind: KindPlain, Args: strings.Fields(args)}
}

// commands is capture-rdma.exp's rows, in its order, with its arguments
// (TestParityCommands checks both against the .exp).
var commands = []Command{
	row("sys_show", PhaseIdle, "system show"),
	row("dev_show", PhaseIdle, "dev show"),
	row("dev_show_one", PhaseIdle, "dev show rxe0"),
	row("dev_show_json", PhaseIdle, "-j dev show"),
	row("dev_missing", PhaseIdle, "dev show nosuch"),
	row("link_show", PhaseIdle, "link show"),
	row("link_show_one", PhaseIdle, "link show rxe0/1"),
	row("link_missing", PhaseIdle, "link show rxe0/9"),
	row("res_show", PhaseIdle, "res show"),
	row("stat_show", PhaseIdle, "statistic show link rxe0/1"),
	row("res_qp", PhaseIdle, "res show qp"),
	row("res_cq", PhaseIdle, "res show cq"),
	row("res_pd", PhaseIdle, "res show pd"),
	row("res_mr_empty", PhaseIdle, "res show mr"),
	row("res_qp_json", PhaseIdle, "-j res show qp"),
	row("res_qp_lqpn", PhaseIdle, "res show qp link rxe0/1 lqpn 1"),
	// gordma refuses -r: it does not decode the _RAW dumps.
	{Slug: "res_qp_raw", Phase: PhaseIdle, Kind: KindPlain, Args: strings.Fields("-r res show qp")},
	row("res_show_busy", PhaseBusy, "res show"),
	row("res_qp_user", PhaseBusy, "res show qp"),
	row("res_cq_user", PhaseBusy, "res show cq"),
	row("res_mr", PhaseBusy, "res show mr"),
	row("res_pd_user", PhaseBusy, "res show pd"),
	row("res_ctx", PhaseBusy, "res show ctx"),
	row("res_srq", PhaseBusy, "res show srq"),
	row("res_cm_id", PhaseBusy, "res show cm_id"),
	row("res_cq_cqn", PhaseBusy, "res show cq cqn "+CQNPlaceholder),
	// After the holders are stopped, as in capture-rdma.exp.
	{Slug: "monitor_add", Phase: PhaseIdle, Implemented: true, Kind: KindMonitor, Args: []string{"monitor"}},
}

// Commands is the table, in the order the driver runs it.
func Commands() []Command { return commands }
