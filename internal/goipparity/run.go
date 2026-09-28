package goipparity

import (
	"flag"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// Exit codes, matching internal/goip's so the two binaries mean the same thing
// by the same number.
const (
	// ExitOK is a clean run.
	ExitOK = 0
	// ExitUsage is a bad invocation.
	ExitUsage = 1
	// ExitFailure is a run that completed and found something.
	ExitFailure = 2
)

const usage = `Usage: goip-parity SUBCOMMAND [OPTIONS]

  commands            print the command table, tab-separated, for a capture
                      driver to iterate: slug, floor, implemented, needs-dev,
                      args, name
  compare -dir DIR    compare the ip/goip/ip capture triples in DIR

goip-parity is the comparator half of the netlink parity harness. It reads
capture files and prints a report; it never opens a socket, which is what
keeps a comparator from putting traffic inside a capture window. The capturing
is the guest's xtcp2-nlcap.
`

// Run is the whole program, minus os.Args and os.Exit.
//
// The same shape as internal/goip.Run and for the same reason: taking the
// writers and returning an int is what lets a test assert on stdout, stderr
// and the exit code in one table row. cmd/goip-parity is a dozen lines over
// this.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}

	switch args[0] {
	case "commands":
		return runCommands(args[1:], stdout, stderr)
	case "compare":
		return runCompare(args[1:], stdout, stderr)
	case "-h", "-help", "--help", "help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "Subcommand %q is unknown.\n\n%s", args[0], usage)
		return ExitUsage
	}
}

// runCommands prints the table for the capture driver.
//
// It exists so the driver does not carry its own copy of the command list.
// One list, printed by the thing that also reads the resulting files, is what
// makes a command that stopped being captured show up as MISSING rather than
// as nothing at all.
func runCommands(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goip-parity commands", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "commands takes no arguments, got %v\n", fs.Args())
		return ExitUsage
	}
	for _, c := range Commands() {
		fmt.Fprintln(stdout, c.String())
	}
	return ExitOK
}

// runCompare compares a directory of triples and prints the report.
func runCompare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("goip-parity compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "directory holding the capture triples")
	noAllowlist := fs.Bool("no-allowlist", false,
		"ignore the embedded allowlist, so every accepted divergence is reported")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "compare takes no positional arguments, got %v\n", fs.Args())
		return ExitUsage
	}
	if *dir == "" {
		fmt.Fprintf(stderr, "compare needs -dir\n\n%s", usage)
		return ExitUsage
	}

	var al *nlparity.Allowlist
	if !*noAllowlist {
		loaded, err := nlparity.EmbeddedAllowlist()
		if err != nil {
			// Not a warning-and-continue: without the allowlist the run would
			// report the accepted version skews as failures, and a report
			// whose findings depend on whether a file parsed is worse than no
			// report.
			fmt.Fprintf(stderr, "the embedded allowlist does not load: %v\n", err)
			return ExitFailure
		}
		al = loaded
	}

	if Render(stdout, CompareDir(*dir, al)) {
		return ExitOK
	}
	return ExitFailure
}
