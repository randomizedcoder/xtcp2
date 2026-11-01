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
//	matrix generate a family's rows from its grammar matrix (matrix.go):
//	      each-choice, pairwise, boundary and seeded random rows, then the
//	      negatives its exclusions and bad values name
//	kselftest mine the ip commands of the kernel's fib selftests
//	      (tools/testing/selftests/net/fib_*.sh) into rows (kselftest.go);
//	      lines it can't mine are written to -skipped with the reason
//	coverage report, per message family, which attributes of the kernel's
//	      policy table some request in the corpus sent (coverage.go)
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
		fmt.Fprintln(stderr, "       nlcapgen matrix -family NAME -seed N -random N -out ROWS")
		fmt.Fprintln(stderr, "       nlcapgen kselftest -src DIR -out ROWS -skipped FILE")
		fmt.Fprintln(stderr, "       nlcapgen coverage -linux DIR -ws DIR -out FILE")
		return 2
	}
	switch args[0] {
	case "tdc":
		return runTdc(args[1:], stdout, stderr)
	case "matrix":
		return runMatrix(args[1:], stdout, stderr)
	case "kselftest":
		return runKselftest(args[1:], stdout, stderr)
	case "coverage":
		return runCoverage(args[1:], stdout, stderr)
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

// families maps a rows file's family to the spaces it holds.
var families = map[string]func() []*space{
	"route":    func() []*space { return []*space{route4Space(), route6Space()} },
	"rule":     func() []*space { return []*space{rule4Space(), rule6Space()} },
	"nexthop":  func() []*space { return []*space{nh4Space(), nh6Space(), nhGroupSpace()} },
	"addr":     func() []*space { return []*space{addr4Space(), addr6Space()} },
	"neigh":    func() []*space { return []*space{neigh4Space(), neigh6Space()} },
	"link":     func() []*space { return []*space{linkSetSpace(), linkAddSpace()} },
	"linkattr": func() []*space { return []*space{linkAttrSpace(), linkPDSpace(), linkPropSpace()} },
	"routeget": func() []*space { return []*space{routeGet4Space(), routeGet6Space()} },
	"fdb":      func() []*space { return []*space{fdbBrSpace(), fdbVxSpace()} },
}

func runMatrix(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("matrix", flag.ContinueOnError)
	fs.SetOutput(stderr)
	family := fs.String("family", "", "the family to generate: route, rule, nexthop, addr, neigh, link, linkattr, routeget, fdb")
	seed := fs.Uint64("seed", 1, "the random rows' seed, recorded in their ids")
	random := fs.Int("random", 0, "random rows per space")
	out := fs.String("out", "", "rows file to write")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	spaces, ok := families[*family]
	if !ok || *out == "" || *random < 0 {
		fmt.Fprintln(stderr, "nlcapgen matrix: -family (route, rule, nexthop, addr, neigh, link, linkattr, routeget, fdb) and -out are required, -random must be >= 0")
		return 2
	}
	rows, err := matrixRows(spaces(), matrixOpts{Seed: *seed, Random: *random})
	if err != nil {
		fmt.Fprintln(stderr, "nlcapgen matrix:", err)
		return 1
	}
	header := []string{
		fmt.Sprintf("Generated by `nlcapgen matrix -family %s -seed %d -random %d`.", *family, *seed, *random),
		"Do not edit; regenerate. tools/nlcapgen/matrix.go explains the strategies.",
		fmt.Sprintf("%d rows.", len(rows)),
	}
	if err := writeFile(*out, func(w io.Writer) error { return writeRows(w, header, rows) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen matrix:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d rows\n", len(rows))
	return 0
}

// matrixRows generates every space's rows, in order.
func matrixRows(spaces []*space, opts matrixOpts) ([]row, error) {
	var rows []row
	for _, sp := range spaces {
		r, err := generate(sp, opts)
		if err != nil {
			return nil, err
		}
		rows = append(rows, r...)
	}
	return rows, nil
}

func runKselftest(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kselftest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	src := fs.String("src", "", "the tools/testing/selftests/net directory of a Linux tree")
	out := fs.String("out", "", "rows file to write")
	skipped := fs.String("skipped", "", "file to write the lines it can't mine to")
	rev := fs.String("rev", "", "the Linux tree's revision, recorded in the header")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *src == "" || *out == "" || *skipped == "" {
		fmt.Fprintln(stderr, "nlcapgen kselftest: -src, -out and -skipped are required")
		return 2
	}
	rows, skips, err := mineKselftests(os.DirFS(*src))
	if err != nil {
		fmt.Fprintln(stderr, "nlcapgen kselftest:", err)
		return 1
	}
	header := []string{
		fmt.Sprintf("Generated by `nlcapgen kselftest` from the fib selftests of Linux %s.", *rev),
		"Do not edit; regenerate. tools/nlcapgen/kselftest.go explains the mining.",
		fmt.Sprintf("%d rows, %d lines skipped.", len(rows), len(skips)),
	}
	if err := writeFile(*out, func(w io.Writer) error { return writeRows(w, header, rows) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen kselftest:", err)
		return 1
	}
	if err := writeFile(*skipped, func(w io.Writer) error { return writeSkips(w, header, skips) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen kselftest:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d rows, %d skipped\n", len(rows), len(skips))
	return 0
}

func runCoverage(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	fs.SetOutput(stderr)
	linux := fs.String("linux", "", "a Linux tree")
	ws := fs.String("ws", "", "the workbench corpus of one kernel version (pkg/xtcpnl/testdata/<kver>/ws)")
	out := fs.String("out", "", "report file to write")
	rev := fs.String("rev", "", "the Linux tree's revision, recorded in the header")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *linux == "" || *ws == "" || *out == "" {
		fmt.Fprintln(stderr, "nlcapgen coverage: -linux, -ws and -out are required")
		return 2
	}
	pols, err := loadPolicies(os.DirFS(*linux))
	if err != nil {
		fmt.Fprintln(stderr, "nlcapgen coverage:", err)
		return 1
	}
	t, files, err := tallyCorpus(os.DirFS(*ws))
	if err != nil {
		fmt.Fprintln(stderr, "nlcapgen coverage:", err)
		return 1
	}
	header := []string{
		fmt.Sprintf("Generated by `nlcapgen coverage` from %d captures, against the policy tables of Linux %s.", files, *rev),
		"Do not edit; regenerate. tools/nlcapgen/coverage.go explains the count.",
	}
	if err := writeFile(*out, func(w io.Writer) error { return writeCoverage(w, header, pols, t) }); err != nil {
		fmt.Fprintln(stderr, "nlcapgen coverage:", err)
		return 1
	}
	fmt.Fprintf(stdout, "%d captures\n", files)
	return 0
}
