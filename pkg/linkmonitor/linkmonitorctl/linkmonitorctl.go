// Package linkmonitorctl is the shared command-line client for
// linkmonitor.v1.LinkMonitorService. Both `go-link-monitor ctl` and
// `xtcp2ctl link` dispatch to Run; each host owns dialing and credentials, and
// uses Access to pick the credential before it dials.
package linkmonitorctl

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	exitOK    = 0
	exitRPC   = 1
	exitUsage = 2

	cmdStatus     = "status"
	cmdInterfaces = "interfaces"
	cmdCollectors = "collectors"
	cmdRebaseline = "rebaseline"
	cmdResync     = "resync"

	formatText = "text"
	formatJSON = "json"
	unknown    = "unknown"
)

// errUsage marks a usage error already reported to stderr.
var errUsage = errors.New("usage")

type command struct {
	summary string
	access  xtcp_auth.Access
	run     runFunc
}

// runFunc performs one RPC and returns the response with its text renderer.
type runFunc func(context.Context, pb.LinkMonitorServiceClient, *invocation) (proto.Message, func(io.Writer) error, error)

var commands = map[string]command{
	cmdStatus:     {"Health, link counts, baseline and control progress", xtcp_auth.Access_ACCESS_READ, runStatus},
	cmdInterfaces: {"Per-interface state and policy checks", xtcp_auth.Access_ACCESS_READ, runInterfaces},
	cmdCollectors: {"Per-interface and host collector diagnostics", xtcp_auth.Access_ACCESS_READ, runCollectors},
	cmdRebaseline: {"Reconcile and durably replace the expected link count", xtcp_auth.Access_ACCESS_ADMIN, runRebaseline},
	cmdResync:     {"Reconcile inventory against the kernel", xtcp_auth.Access_ACCESS_ADMIN, runResync},
}

// commandOrder fixes the usage listing.
var commandOrder = []string{cmdStatus, cmdInterfaces, cmdCollectors, cmdRebaseline, cmdResync}

// Access reports the access class of the subcommand named by args[0]. Unknown
// or missing subcommands report ACCESS_READ; Run rejects them before any RPC.
func Access(args []string) xtcp_auth.Access {
	if len(args) > 0 {
		if c, ok := commands[args[0]]; ok {
			return c.access
		}
	}
	return xtcp_auth.Access_ACCESS_READ
}

// Usage writes the subcommand summary. prog prefixes every example, for
// example "go-link-monitor ctl" or "xtcp2ctl link".
func Usage(w io.Writer, prog string) {
	fmt.Fprintf(w, "Usage: %s <command> [flags]\n\nCommands:\n", prog)
	for _, name := range commandOrder {
		access := "read"
		if commands[name].access == xtcp_auth.Access_ACCESS_ADMIN {
			access = "admin"
		}
		fmt.Fprintf(w, "  %-11s %-5s  %s\n", name, access, commands[name].summary)
	}
	fmt.Fprintf(w, `
Flags (all commands):
  -format text|json   output format (default text; json is protojson)
interfaces, collectors:
  -name NAME          exact interface name; repeat for more (default all)
interfaces:
  -eligible-only      only interfaces in the physical-link inventory
rebaseline, resync:
  -reason TEXT        required; recorded in the daemon's audit log (<=512 bytes)
  -wait DURATION      wait for completion, up to 60s (default 0: return on acceptance)

Run '%s <command> -h' for per-command flags.
`, prog)
}

// invocation holds one subcommand's parsed flags.
type invocation struct {
	format       string
	names        []string
	eligibleOnly bool
	reason       string
	wait         time.Duration
}

type nameList struct{ names *[]string }

func (n nameList) String() string {
	if n.names == nil {
		return ""
	}
	return strings.Join(*n.names, ",")
}

func (n nameList) Set(v string) error {
	*n.names = append(*n.names, v)
	return nil
}

// Run executes one subcommand over conn and returns the process exit code:
// 0 on success, 1 for an RPC failure and 2 for a usage error.
func Run(ctx context.Context, conn grpc.ClientConnInterface, prog string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		Usage(stderr, prog)
		return exitUsage
	}
	name := args[0]
	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(stderr, "%s: unknown command %q\n\n", prog, name)
		Usage(stderr, prog)
		return exitUsage
	}
	inv, err := parse(prog+" "+name, name, args[1:], stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	msg, render, err := cmd.run(ctx, pb.NewLinkMonitorServiceClient(conn), inv)
	if err != nil {
		if errors.Is(err, errUsage) {
			return exitUsage
		}
		s, _ := status.FromError(err)
		fmt.Fprintf(stderr, "%s %s: %s: %s\n", prog, name, s.Code(), s.Message())
		return exitRPC
	}
	if inv.format == formatJSON {
		out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(msg)
		if err != nil {
			fmt.Fprintf(stderr, "%s %s: encode: %v\n", prog, name, err)
			return exitRPC
		}
		fmt.Fprintln(stdout, string(out))
		return exitOK
	}
	if err := render(stdout); err != nil {
		fmt.Fprintf(stderr, "%s %s: write: %v\n", prog, name, err)
		return exitRPC
	}
	return exitOK
}

func parse(prog, name string, args []string, stderr io.Writer) (*invocation, error) {
	inv := &invocation{}
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&inv.format, "format", formatText, "output format: text or json")
	switch name {
	case cmdInterfaces, cmdCollectors:
		fs.Var(nameList{&inv.names}, "name", "exact interface name; repeat for more")
		if name == cmdInterfaces {
			fs.BoolVar(&inv.eligibleOnly, "eligible-only", false, "only interfaces in the physical-link inventory")
		}
	case cmdRebaseline, cmdResync:
		fs.StringVar(&inv.reason, "reason", "", "why; recorded in the audit log (required)")
		fs.DurationVar(&inv.wait, "wait", 0, "wait for completion, up to 60s")
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	switch {
	case fs.NArg() != 0:
		fmt.Fprintf(stderr, "%s: unexpected arguments %q\n", prog, fs.Args())
		return nil, errUsage
	case inv.format != formatText && inv.format != formatJSON:
		fmt.Fprintf(stderr, "%s: -format must be text or json, not %q\n", prog, inv.format)
		return nil, errUsage
	case (name == cmdRebaseline || name == cmdResync) && strings.TrimSpace(inv.reason) == "":
		fmt.Fprintf(stderr, "%s: -reason is required\n", prog)
		return nil, errUsage
	case inv.wait < 0:
		fmt.Fprintf(stderr, "%s: -wait must not be negative\n", prog)
		return nil, errUsage
	}
	return inv, nil
}

func runStatus(ctx context.Context, c pb.LinkMonitorServiceClient, _ *invocation) (proto.Message, func(io.Writer) error, error) {
	resp, err := c.GetStatus(ctx, &pb.GetStatusRequest{})
	return resp, func(w io.Writer) error { return renderStatus(w, resp) }, err
}

func renderStatus(w io.Writer, r *pb.GetStatusResponse) error {
	h := r.GetHealth()
	c := r.GetCounts()
	fmt.Fprintf(w, "health:      running=%t ready=%t collection_healthy=%t baseline_ready=%t\n",
		h.GetRunning(), h.GetReady(), h.GetCollectionHealthy(), h.GetBaselineReady())
	fmt.Fprintf(w, "links:       current=%s expected=%s delta=%s\n",
		optUint(c.Current), optUint(c.Expected), optInt(c.Delta))
	fmt.Fprintf(w, "baseline:    recorded_at=%s write_errors=%d\n", stamp(r.GetBaselineRecordedAt()), r.GetBaselineWriteErrors())
	fmt.Fprintf(w, "last_resync: %s\n", stamp(r.GetLastSuccessfulResync()))
	fmt.Fprintf(w, "controls:    resync %d/%d rebaseline %d/%d (completed/requested)\n",
		r.GetResync().GetCompleted(), r.GetResync().GetRequested(),
		r.GetRebaseline().GetCompleted(), r.GetRebaseline().GetRequested())
	_, err := fmt.Fprintf(w, "snapshot:    %d\n", r.GetSnapshotVersion())
	return err
}

func runInterfaces(ctx context.Context, c pb.LinkMonitorServiceClient, inv *invocation) (proto.Message, func(io.Writer) error, error) {
	resp, err := c.ListInterfaces(ctx, &pb.ListInterfacesRequest{Names: inv.names, EligibleOnly: inv.eligibleOnly})
	return resp, func(w io.Writer) error { return renderInterfaces(w, resp) }, err
}

func renderInterfaces(w io.Writer, r *pb.ListInterfacesResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tUP\tELIGIBILITY\tSPEED\tWIDTH\tDUPLEX\tRDMA\tSPEED_EXCEPTION\tUP/DOWN")
	for _, i := range r.GetInterfaces() {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%t\t%d/%d\n", i.GetName(), optBool(i.Up),
			enumText(i.GetEligibility().String(), "ELIGIBILITY_"),
			enumText(i.GetMaximumSpeed().String(), "CHECK_STATUS_"), enumText(i.GetMaximumWidth().String(), "CHECK_STATUS_"),
			enumText(i.GetFullDuplex().String(), "CHECK_STATUS_"), enumText(i.GetRdmaReadiness().String(), "CHECK_STATUS_"),
			i.GetMaximumSpeedException(), i.GetUpTransitions(), i.GetDownTransitions())
	}
	return tw.Flush()
}

func runCollectors(ctx context.Context, c pb.LinkMonitorServiceClient, inv *invocation) (proto.Message, func(io.Writer) error, error) {
	resp, err := c.ListCollectors(ctx, &pb.ListCollectorsRequest{Names: inv.names})
	return resp, func(w io.Writer) error { return renderCollectors(w, resp) }, err
}

func renderCollectors(w io.Writer, r *pb.ListCollectorsResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "INTERFACE\tCOLLECTOR\tSUPPORT\tSUCCESS\tFRESH\tLAST_SUCCESS\tDURATION\tERROR\tDISCONTINUITIES")
	for _, c := range r.GetCollectors() {
		iface := c.GetInterface()
		if iface == "" {
			iface = "(host)"
		}
		duration := unknown
		if c.Duration != nil {
			duration = c.GetDuration().AsDuration().String()
		}
		reason := c.GetErrorReason()
		if reason == "" {
			reason = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\t%s\t%s\t%s\t%d\n", iface, c.GetName(),
			enumText(c.GetSupport().String(), "SUPPORT_"), optBool(c.Success), c.GetFresh(),
			stamp(c.GetLastSuccess()), duration, reason, c.GetDiscontinuities())
	}
	return tw.Flush()
}

func runRebaseline(ctx context.Context, c pb.LinkMonitorServiceClient, inv *invocation) (proto.Message, func(io.Writer) error, error) {
	resp, err := c.Rebaseline(ctx, &pb.RebaselineRequest{Reason: inv.reason, Wait: waitDuration(inv.wait)})
	return resp, func(w io.Writer) error { return renderRebaseline(w, resp) }, err
}

func renderRebaseline(w io.Writer, r *pb.RebaselineResponse) error {
	if !r.GetCompleted() {
		_, err := fmt.Fprintf(w, "ok: rebaseline generation %d accepted\n", r.GetGeneration())
		return err
	}
	_, err := fmt.Fprintf(w, "ok: rebaseline generation %d completed; expected=%s write_errors=%d\n",
		r.GetGeneration(), optUint(r.Expected), r.GetBaselineWriteErrors())
	return err
}

func runResync(ctx context.Context, c pb.LinkMonitorServiceClient, inv *invocation) (proto.Message, func(io.Writer) error, error) {
	resp, err := c.Resync(ctx, &pb.ResyncRequest{Reason: inv.reason, Wait: waitDuration(inv.wait)})
	return resp, func(w io.Writer) error { return renderResync(w, resp) }, err
}

func renderResync(w io.Writer, r *pb.ResyncResponse) error {
	if !r.GetCompleted() {
		_, err := fmt.Fprintf(w, "ok: resync generation %d accepted\n", r.GetGeneration())
		return err
	}
	_, err := fmt.Fprintf(w, "ok: resync generation %d completed; last_resync=%s\n", r.GetGeneration(), stamp(r.GetLastSuccessfulResync()))
	return err
}

func waitDuration(d time.Duration) *durationpb.Duration {
	if d == 0 {
		return nil
	}
	return durationpb.New(d)
}

func optUint(v *uint64) string {
	if v == nil {
		return unknown
	}
	return fmt.Sprint(*v)
}

func optInt(v *int64) string {
	if v == nil {
		return unknown
	}
	return fmt.Sprintf("%+d", *v)
}

func optBool(v *bool) string {
	if v == nil {
		return unknown
	}
	return fmt.Sprint(*v)
}

func stamp(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return unknown
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

// enumText turns CHECK_STATUS_NOT_APPLICABLE into not_applicable and any
// UNSPECIFIED value into unknown.
func enumText(name, prefix string) string {
	v := strings.ToLower(strings.TrimPrefix(name, prefix))
	if v == "unspecified" {
		return unknown
	}
	return v
}
