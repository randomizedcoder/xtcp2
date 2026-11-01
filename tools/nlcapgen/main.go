// nlcapgen
//
// Generates rows files for the nl_mon capture workbench
// (nix/microvms/workbench/, docs/netlink/coverage-status.md "Workbench and
// generated rows"). A row is one command to run in its own namespace with
// the netlink messages it exchanges captured, and what the run must show.
// The workbench runs them; pkg/xtcpnl/xtcpnl_ws_corpus_test.go holds every
// capture to its row.
//
// Subcommands:
//
//	tdc   convert the kernel's tc selftests (tools/testing/selftests/
//	      tc-testing/tc-tests/**/*.json) into rows; cases the workbench
//	      cannot run are written to -skipped with the reason
//
// Output is deterministic: the same input tree gives the same bytes, so a
// regenerated rows file diffs cleanly against the committed one.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: nlcapgen tdc -src DIR -out ROWS -skipped FILE")
		return 2
	}
	switch args[0] {
	case "tdc":
		return runTdc(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "nlcapgen: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runTdc(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tdc", flag.ContinueOnError)
	fs.SetOutput(stderr)
	src := fs.String("src", "", "the tc-tests directory of a Linux tree")
	out := fs.String("out", "", "rows file to write")
	skipped := fs.String("skipped", "", "file to write the skipped cases to")
	rev := fs.String("rev", "", "the Linux tree's revision, recorded in the header")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *src == "" || *out == "" || *skipped == "" {
		fmt.Fprintln(stderr, "nlcapgen tdc: -src, -out and -skipped are required")
		return 2
	}
	rows, skips, err := convertTdcTree(os.DirFS(*src))
	if err != nil {
		fmt.Fprintln(stderr, "nlcapgen tdc:", err)
		return 1
	}
	header := tdcHeader(*rev, len(rows), len(skips))
	if err := writeFile(*out, func(w io.Writer) error { return writeRows(w, header, rows) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen tdc:", err)
		return 1
	}
	if err := writeFile(*skipped, func(w io.Writer) error { return writeSkips(w, header, skips) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen tdc:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d rows, %d skipped\n", len(rows), len(skips))
	return 0
}

func writeFile(path string, fill func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := fill(f); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}
