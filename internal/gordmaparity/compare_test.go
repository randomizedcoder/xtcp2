package gordmaparity

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func lookup(t *testing.T, slug string) Command {
	t.Helper()
	for _, c := range Commands() {
		if c.Slug == slug {
			return c
		}
	}
	t.Fatalf("no row %s", slug)
	return Command{}
}

// fixture is a run directory for slug in which all three sides are the 7.1.4
// corpus capture and the replay printed what rdma did: a clean row.
func fixture(t *testing.T, slug string) string {
	t.Helper()
	dir := t.TempDir()
	fixtureInto(t, dir, slug)
	return dir
}

// fixtureInto writes slug's clean row into dir; a not-implemented row gets
// rdma's sides only, as the driver leaves it.
func fixtureInto(t *testing.T, dir, slug string) {
	t.Helper()
	c := lookup(t, slug)
	for _, ext := range []string{"strace", "out", "err", "rc"} {
		b, err := os.ReadFile(filepath.Join(corpusDir, slug+"."+ext))
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range Sides() {
			put(t, dir, c.FileName(s, ext), string(b))
		}
		if ext == "strace" {
			continue
		}
		if ext == "rc" && c.Kind == KindMonitor {
			b = []byte("0\n") // a replayed monitor ends on its own
		}
		put(t, dir, c.FileName(SideReplay, ext), string(b))
	}
	if !c.Implemented {
		for _, ext := range []string{"strace", "out", "err", "rc"} {
			if err := os.Remove(filepath.Join(dir, c.FileName(SideGordma, ext))); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func put(t *testing.T, dir, name, s string) {
	t.Helper()
	// os.Root keeps the write inside dir whatever name holds.
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := root.WriteFile(name, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func corpusFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpusDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestCompare runs one row against a corpus fixture with one thing changed.
func TestCompare(t *testing.T) {
	type expect struct {
		status  Status
		finding string // a substring of the findings, or of the hygiene lines
		noisy   int
		overall bool
	}
	tests := []struct {
		description string
		slug        string
		mutate      func(t *testing.T, dir string)
		expected    expect
	}{
		{description: "positive: three identical sides", slug: "dev_show",
			expected: expect{status: StatusPass, overall: true}},
		{description: "positive: the monitor's SIGTERM status and its device index", slug: "monitor_add",
			mutate: func(t *testing.T, dir string) {
				put(t, dir, "monitor_add.gordma.out", strings.ReplaceAll(corpusFile(t, "monitor_add.out"), "dev 1 ", "dev 2 "))
				put(t, dir, "monitor_add.rdma_b.out", strings.ReplaceAll(corpusFile(t, "monitor_add.out"), "dev 1 ", "dev 3 "))
			}, expected: expect{status: StatusPass, overall: true}},
		{description: "negative: gordma's request differs", slug: "dev_show",
			mutate: func(t *testing.T, dir string) {
				put(t, dir, "dev_show.gordma.strace", corpusFile(t, "link_show.strace"))
			},
			expected: expect{status: StatusFail, finding: "L1 request 1 differs"}},
		{description: "negative: gordma's stdout differs by one byte", slug: "dev_show",
			mutate: func(t *testing.T, dir string) {
				put(t, dir, "dev_show.gordma.out", strings.Replace(corpusFile(t, "dev_show.out"), "ca", "cb", 1))
			}, expected: expect{status: StatusFail, finding: "L2 stdout differs"}},
		{description: "negative: gordma's rc differs", slug: "dev_missing",
			mutate:   func(t *testing.T, dir string) { put(t, dir, "dev_missing.gordma.rc", "0\n") },
			expected: expect{status: StatusFail, finding: "L2 rc differs"}},
		{description: "negative: the replay differs", slug: "res_qp",
			mutate:   func(t *testing.T, dir string) { put(t, dir, "res_qp.replay.out", "") },
			expected: expect{status: StatusFail, finding: "L3 replay stdout differs"}},
		{description: "negative: a file is missing", slug: "dev_show",
			mutate: func(t *testing.T, dir string) {
				if err := os.Remove(filepath.Join(dir, "dev_show.gordma.err")); err != nil {
					t.Fatal(err)
				}
			}, expected: expect{status: StatusMissing}},
		{description: "negative: a not-implemented row that gordma was run for", slug: "res_qp_raw",
			mutate: func(t *testing.T, dir string) {
				put(t, dir, "res_qp_raw.gordma.strace", corpusFile(t, "res_qp_raw.strace"))
			},
			expected: expect{status: StatusFail, finding: "not implemented, but gordma was run"}},
		{description: "boundary: a not-implemented row with no gordma capture is skipped", slug: "res_qp_raw",
			expected: expect{status: StatusSkip}},
		{description: "boundary: an empty dump", slug: "res_mr_empty",
			expected: expect{status: StatusPass, overall: true}},
		{description: "corner: gordma matches rdma_b where the two rdma runs differ", slug: "res_show_busy",
			mutate: func(t *testing.T, dir string) {
				changed := strings.Replace(corpusFile(t, "res_show_busy.out"), "pd ", "pd 9", 1)
				put(t, dir, "res_show_busy.gordma.out", changed)
				put(t, dir, "res_show_busy.rdma_b.out", changed)
			}, expected: expect{status: StatusPass, noisy: 1, overall: true}},
		{description: "corner: a capture cut mid-dump", slug: "dev_show",
			mutate: func(t *testing.T, dir string) {
				s := corpusFile(t, "dev_show.strace")
				put(t, dir, "dev_show.gordma.strace", s[:strings.LastIndex(s, " | ")])
			}, expected: expect{status: StatusFail, finding: "dev_show.gordma.strace: nlstrace: the hex dump"}},
		{description: "corner: a transaction with no end", slug: "dev_show",
			mutate: func(t *testing.T, dir string) {
				// Drop the last call, the dump's NLMSG_DONE, and its hex.
				s := corpusFile(t, "dev_show.strace")
				lines := strings.SplitAfter(s, "\n")
				last := 0
				for i, l := range lines {
					if strings.Contains(l, "recvmsg(") || strings.Contains(l, "recvfrom(") {
						last = i
					}
				}
				put(t, dir, "dev_show.gordma.strace", strings.Join(lines[:last], ""))
			}, expected: expect{status: StatusFail, finding: "has no NLMSG_DONE or NLMSG_ERROR"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := fixture(t, tc.slug)
			if tc.mutate != nil {
				tc.mutate(t, dir)
			}
			r := Compare(dir, []Command{lookup(t, tc.slug)})
			got := r.Results[0]
			all := strings.Join(append(append([]string{}, got.Findings...), got.Hygiene...), "\n")
			if got.Status != tc.expected.status || got.Noisy != tc.expected.noisy || r.Pass() != tc.expected.overall ||
				(tc.expected.finding == "") != (all == "") || !strings.Contains(all, tc.expected.finding) {
				t.Fatalf("status %s noisy %d overall %v\n%s\nwant %+v", got.Status, got.Noisy, r.Pass(), all, tc.expected)
			}
		})
	}
	t.Run("boundary: an empty directory compares nothing", func(t *testing.T) {
		r := CompareDir(t.TempDir())
		if r.Compared() != 0 || r.Pass() || r.Results[0].Status != StatusMissing {
			t.Fatalf("compared %d, pass %v, first %s", r.Compared(), r.Pass(), r.Results[0].Status)
		}
	})
	t.Run("positive: every corpus row compares clean against itself", func(t *testing.T) {
		for _, c := range Commands() {
			if !c.Implemented {
				continue
			}
			r := Compare(fixture(t, c.Slug), []Command{c})
			if x := r.Results[0]; x.Status != StatusPass {
				t.Errorf("%s: %s %v %v", c.Slug, x.Status, x.Findings, x.Hygiene)
			}
		}
	})
}

var overallRe = regexp.MustCompile(`(?m)^GORDMA_PARITY_OVERALL_PASS$`)

func TestRender(t *testing.T) {
	pass := Result{Command: Command{Slug: "dev_show"}, Status: StatusPass}
	fail := Result{Command: Command{Slug: "link_show"}, Status: StatusFail, Findings: []string{"L2 stdout differs"}}
	skip := Result{Command: Command{Slug: "res_qp_raw"}, Status: StatusSkip}
	noisy := Result{Command: Command{Slug: "res_show"}, Status: StatusPass, Noisy: 2}
	tests := []struct {
		description string
		in          Report
		expected    string
		pass        bool
	}{
		{description: "positive: an all-pass run", in: Report{Results: []Result{pass, skip}}, pass: true,
			expected: "GORDMA_PARITY_PASS dev_show\nGORDMA_PARITY_SKIP res_qp_raw (not implemented)\n" +
				"GORDMA_PARITY_HYGIENE_PASS 0\nGORDMA_PARITY_CONTROL_CLEAN 0\nGORDMA_PARITY_OVERALL_PASS\n"},
		{description: "negative: one failure", in: Report{Results: []Result{pass, fail}},
			expected: "GORDMA_PARITY_PASS dev_show\nGORDMA_PARITY_FAIL link_show\n  finding: L2 stdout differs\n" +
				"GORDMA_PARITY_HYGIENE_PASS 0\nGORDMA_PARITY_CONTROL_CLEAN 0\nGORDMA_PARITY_OVERALL_FAIL\n"},
		{description: "negative: a hygiene failure", in: Report{Results: []Result{{Command: Command{Slug: "x"}, Status: StatusFail,
			Hygiene: []string{"x.gordma.strace: short"}}}},
			expected: "GORDMA_PARITY_FAIL x\n  hygiene: x.gordma.strace: short\n" +
				"GORDMA_PARITY_HYGIENE_FAIL 1\nGORDMA_PARITY_CONTROL_CLEAN 0\nGORDMA_PARITY_OVERALL_FAIL\n"},
		{description: "boundary: skips only", in: Report{Results: []Result{skip}},
			expected: "GORDMA_PARITY_SKIP res_qp_raw (not implemented)\nGORDMA_PARITY_NOTHING_COMPARED\n" +
				"GORDMA_PARITY_HYGIENE_PASS 0\nGORDMA_PARITY_CONTROL_CLEAN 0\nGORDMA_PARITY_OVERALL_FAIL\n"},
		{description: "corner: control noise passes and is counted", in: Report{Results: []Result{noisy}}, pass: true,
			expected: "GORDMA_PARITY_PASS res_show\n  control-suppressed: 2\n" +
				"GORDMA_PARITY_HYGIENE_PASS 0\nGORDMA_PARITY_CONTROL_NOISY 2\nGORDMA_PARITY_OVERALL_PASS\n"},
		{description: "corner: a missing row names its files", in: Report{Results: []Result{pass,
			{Command: Command{Slug: "res_qp"}, Status: StatusMissing, Missing: []string{"res_qp.gordma.out"}}}},
			expected: "GORDMA_PARITY_PASS dev_show\nGORDMA_PARITY_MISSING res_qp: res_qp.gordma.out\n" +
				"GORDMA_PARITY_HYGIENE_PASS 0\nGORDMA_PARITY_CONTROL_CLEAN 0\nGORDMA_PARITY_OVERALL_FAIL\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b bytes.Buffer
			got := Render(&b, tc.in)
			if got != tc.pass || b.String() != tc.expected {
				t.Fatalf("pass %v\n%s\nwant %v\n%s", got, b.String(), tc.pass, tc.expected)
			}
			// The host runner's grep finds the verdict line once on a pass
			// and never otherwise.
			if n := len(overallRe.FindAllString(b.String(), -1)); n != map[bool]int{true: 1, false: 0}[tc.pass] {
				t.Fatalf("%d OVERALL_PASS lines", n)
			}
		})
	}
}

func TestRunCLI(t *testing.T) {
	good := fixture(t, "dev_show")
	full := t.TempDir()
	for _, c := range Commands() {
		fixtureInto(t, full, c.Slug)
	}
	tests := []struct {
		description string
		args        []string
		expected    int
		stdout      string // a prefix
		stderr      string // a prefix
	}{
		{description: "positive: commands", args: []string{"commands"}, expected: ExitOK, stdout: "sys_show\tidle\tyes\tplain\tsystem show\n"},
		{description: "positive: help", args: []string{"--help"}, expected: ExitOK, stdout: "Usage: gordma-parity"},
		{description: "positive: compare on a whole clean run", args: []string{"compare", "-dir", full},
			expected: ExitOK, stdout: "GORDMA_PARITY_PASS sys_show\n"},
		{description: "negative: compare on a directory with missing rows", args: []string{"compare", "-dir", good},
			expected: ExitFailure, stdout: "GORDMA_PARITY_MISSING sys_show"},
		{description: "negative: an unknown subcommand", args: []string{"frob"}, expected: ExitUsage, stderr: "Subcommand \"frob\" is unknown."},
		{description: "negative: commands with an argument", args: []string{"commands", "x"}, expected: ExitUsage, stderr: "commands takes no arguments"},
		{description: "boundary: no arguments", args: nil, expected: ExitUsage, stderr: "Usage: gordma-parity"},
		{description: "corner: compare without -dir", args: []string{"compare"}, expected: ExitUsage, stderr: "compare needs -dir"},
		{description: "corner: compare with a positional argument", args: []string{"compare", "-dir", good, "x"},
			expected: ExitUsage, stderr: "compare takes no positional arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			rc := Run(tc.args, &stdout, &stderr)
			if rc != tc.expected || !strings.HasPrefix(stdout.String(), tc.stdout) || !strings.HasPrefix(stderr.String(), tc.stderr) {
				t.Fatalf("rc %d\nstdout %q\nstderr %q", rc, stdout.String(), stderr.String())
			}
		})
	}
	t.Run("positive: commands prints one line per row", func(t *testing.T) {
		var stdout bytes.Buffer
		Run([]string{"commands"}, &stdout, &bytes.Buffer{})
		if n := strings.Count(stdout.String(), "\n"); n != len(Commands()) {
			t.Fatalf("%d lines", n)
		}
	})
}
