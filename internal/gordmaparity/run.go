package gordmaparity

import (
	"flag"
	"fmt"
	"io"
)

// Exit codes, gordma's and goip-parity's.
const (
	ExitOK      = 0
	ExitUsage   = 1
	ExitFailure = 2
)

const usage = `Usage: gordma-parity SUBCOMMAND [OPTIONS]

  commands            print the command table, tab-separated, for the capture
                      driver: slug, phase, implemented, kind, args
  compare -dir DIR    compare the rdma/gordma/rdma strace triples in DIR

gordma-parity is the comparator half of gordma's live parity run. It reads
capture files and prints a report; it never opens a socket.
`

// Run is the whole program, minus os.Args and os.Exit.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "commands":
		if len(args) != 1 {
			fmt.Fprintf(stderr, "commands takes no arguments, got %v\n", args[1:])
			return ExitUsage
		}
		for _, c := range Commands() {
			fmt.Fprintln(stdout, c.String())
		}
		return ExitOK
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

func runCompare(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gordma-parity compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "directory holding the strace triples")
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
	if Render(stdout, CompareDir(*dir)) {
		return ExitOK
	}
	return ExitFailure
}
