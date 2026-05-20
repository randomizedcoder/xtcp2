// quality-report
//
// Aggregates findings from every static-analysis tool in the xtcp2 repo
// into a single markdown report.
//
// The orchestration shell (see nix/quality-report/default.nix) runs each
// tool with `|| true` and writes the per-tool raw output into a directory.
// This program reads that directory, parses each tool's output (JSON for
// golangci-lint and gosec, text for the rest), normalises everything into
// a uniform Finding shape, and emits markdown on stdout.
//
// Invariants:
//   - Never exits non-zero on findings — the report itself is the result.
//   - Exit code 0 on success, 2 only on a structural failure (missing
//     -raw-dir, malformed required file).
//   - Missing optional tool outputs render as "not run" rows in the
//     tool-status table; they don't fail the report.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// Tool / rule / severity strings reused throughout the aggregator. Kept
// as named constants so a typo in a Tool field or Severity check
// stays a compile error rather than a silent mismatch.
const (
	toolGofmt = "gofmt"
	toolGosec = "gosec"

	ruleFormat = "format"

	severityWarning = "warning"
	severityInfo    = "info"
	severityError   = "error"

	testActionFail = "fail"
	testActionPass = "pass"
	testActionSkip = "skip"

	goSecNixPath = "nix/checks/go-sec.nix"
)

// linterTier maps a golangci-lint linter name to the lowest config tier
// in which it is enabled. Source of truth: the `linters:` blocks of
// .golangci-quick.yml, .golangci.yml, .golangci-comprehensive.yml.
//
// Used to attribute every finding to its earliest-tier origin, so the
// Tier 0/1/2 counts don't triple-count findings shared across tiers.
var linterTier = map[string]int{
	// Tier 0 (.golangci-quick.yml)
	"govet":       0,
	"errcheck":    0,
	"ineffassign": 0,
	"unused":      0,
	"staticcheck": 0,
	toolGofmt:     0,
	"goimports":   0,
	"typecheck":   0,
	// Tier 1 (.golangci.yml)
	toolGosec:       1,
	"gocritic":      1,
	"revive":        1,
	"noctx":         1,
	"contextcheck":  1,
	"durationcheck": 1,
	// Tier 2 (.golangci-comprehensive.yml)
	"exhaustive": 2,
	"prealloc":   2,
	"gocyclo":    2,
	"funlen":     2,
	"goconst":    2,
	"dupl":       2,
	"unconvert":  2,
	"nakedret":   2,
	"misspell":   2,
}

// Finding is the normalised shape across every tool.
type Finding struct {
	Tool     string // "golangci-lint-quick", "gosec", "netlink-audit", ...
	Tier     int    // 0/1/2 for golangci-lint, 0 otherwise
	Rule     string // linter name or rule id (e.g. "errcheck", "G104")
	Severity string // "error" | "warning" | "info" | "" (unset)
	File     string // repo-relative path
	Line     int
	Column   int
	Message  string
}

// ToolStatus records how each tool ran: ran-clean, ran-with-findings,
// crashed, was skipped (e.g. network-restricted).
type ToolStatus struct {
	Name      string
	ExitCode  int
	Findings  int
	RuntimeS  int    // seconds
	Note      string // "skipped: network-restricted", "crash: <msg>", ""
	Available bool   // false if the raw file was missing
}

// TestResult captures `go test -json` event data, aggregated per-package.
type TestResult struct {
	Package  string
	Test     string // empty for package-level summary
	Action   string // pass, fail, skip
	Elapsed  float64
	Output   string
	Preexist bool // matched in known-failures.txt
}

// ConfigExclusion is one row of the configuration-audit table.
type ConfigExclusion struct {
	Source    string // filename
	Rule      string // rule id or path glob
	Scope     string // path filter, linter scope, etc.
	Justified bool
	Note      string // recovered comment / justification
}

// reportInput is everything the templater needs.
type reportInput struct {
	Generated      time.Time
	Versions       map[string]string
	CommitSHA      string
	Branch         string
	Findings       []Finding
	Status         []ToolStatus
	Tests          []TestResult
	KnownFailures  map[string]bool
	Exclusions     []ConfigExclusion
	GofmtFiles     []string
	NixfmtFiles    []string
	CliHelpResults []CliHelpResult
	Coverage       Coverage
}

// CliHelpResult is one cmd binary's -help smoke result.
type CliHelpResult struct {
	Binary   string
	ExitCode int
	Bytes    int
	OK       bool
}

// Coverage holds the parsed `go test -coverprofile` summary: the overall
// statement-coverage percentage plus a per-package breakdown averaged
// from `go tool cover -func` output.
type Coverage struct {
	// Total is the "total: (statements) NN.N%" line from `go tool cover -func`.
	Total float64
	// PerPackage maps "pkg/xtcp" / "tools/quality-report" / "cmd/xtcp2" →
	// average function coverage within that package. Sourced from the TSV
	// the orchestrator produces.
	PerPackage map[string]float64
	// Available is false when the coverage profile or summary was missing.
	Available bool
}

// CoverageThreshold is the per-package floor the plan targets. Packages
// below this surface as findings in the report.
const CoverageThreshold = 90.0

// countBelowThreshold reports how many packages fall under
// CoverageThreshold. Used as the "Findings" count for the go test -cover
// tool-status row.
func countBelowThreshold(cov Coverage) int {
	n := 0
	for _, pct := range cov.PerPackage {
		if pct < CoverageThreshold {
			n++
		}
	}
	return n
}

// coverageFindings emits one Finding per package below CoverageThreshold
// so the per-package gaps surface in the executive summary's tier
// rollup. Each finding lands in tier 0 with severity warning — the
// linter set chose tier 0 for coverage gaps because they're widely
// considered must-fix and the tooling already aggregates tier 0 in the
// top-line counts.
func coverageFindings(cov Coverage) []Finding {
	if !cov.Available {
		return nil
	}
	var findings []Finding
	for pkg, pct := range cov.PerPackage {
		if pct >= CoverageThreshold {
			continue
		}
		findings = append(findings, Finding{
			Tool:     "go-test-cover",
			Rule:     "below-90pct",
			Severity: severityWarning,
			File:     pkg,
			Message:  fmtSprintf("package coverage %.1f%% < %.0f%%", pct, CoverageThreshold),
		})
	}
	return findings
}

// fmtSprintf is a tiny indirection so the import set stays tight.
func fmtSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + report assembly + emit. Extracted from main
// so tests can drive it with synthetic args + capture buffers (instead of
// subprocessing). Returns the process exit code.
// ingestCtx bundles the read-only inputs shared by every per-tool
// ingestion step in runMain. Passing one struct instead of 5 params
// per helper keeps the call sites short.
type ingestCtx struct {
	rawDir    string
	repoRoot  string
	runtimes  map[string]int
	exitCodes map[string]int
	known     map[string]bool
}

func runMain(args []string, stdout, stderr io.Writer) int {
	rawDir, repoRoot, knownFile, baselineFile, maxDropAbs, coverageOut, parseErr := parseRunMainFlags(args, stderr)
	if parseErr != 0 {
		return parseErr
	}

	// When -coverage-out is set, regenerate coverage-func.out +
	// coverage-per-package.tsv inside rawDir from the supplied profile
	// before ingestCoverage runs. This lets the update-quality-report
	// wrapper merge host + microvm profiles into one .out and re-run
	// the aggregator without re-building the entire Nix derivation.
	if coverageOut != "" {
		if err := regenerateCoverageArtifacts(rawDir, coverageOut); err != nil {
			fmt.Fprintf(stderr, "quality-report: regen coverage artifacts: %v\n", err)
			return 2
		}
	}

	ctx := &ingestCtx{
		rawDir:    rawDir,
		repoRoot:  repoRoot,
		runtimes:  readRuntimes(filepath.Join(rawDir, "runtimes.txt")),
		exitCodes: readExitCodes(filepath.Join(rawDir, "exit-codes.txt")),
		known:     loadKnownFailures(knownFile),
	}

	in := reportInput{
		Generated:     time.Now().UTC(),
		Versions:      readKVFile(filepath.Join(rawDir, "versions.txt")),
		CommitSHA:     strings.TrimSpace(readFile(filepath.Join(rawDir, "commit.txt"))),
		Branch:        strings.TrimSpace(readFile(filepath.Join(rawDir, "branch.txt"))),
		KnownFailures: ctx.known,
	}

	ctx.ingestGolangciTiers(&in)
	ctx.ingestGosec(&in)
	ctx.ingestGoVet(&in)
	ctx.ingestFormatter(&in, toolGofmt, "gofmt.out", toolGofmt, &in.GofmtFiles)
	ctx.ingestFormatter(&in, "nix-fmt", "nix-fmt.out", "nixfmt", &in.NixfmtFiles)
	ctx.ingestCustomAudits(&in)
	ctx.ingestGoTest(&in)
	ctx.ingestCliHelpSmoke(&in)
	ctx.ingestCoverage(&in)

	// Configuration audit — parse the .golangci*.yml exclusion sections.
	in.Exclusions = parseExclusions(repoRoot)

	if err := emit(stdout, in); err != nil {
		fmt.Fprintf(stderr, "quality-report: emit: %v\n", err)
		return 2
	}

	// Coverage ratchet: refuse to land a report whose Total has dropped
	// by more than maxDropAbs absolute points from the operator-set
	// baseline in baselineFile. Skipped when the baseline file is
	// missing (first run on a new branch) or coverage data is absent.
	if baselineFile != "" && in.Coverage.Available {
		if msg, breached := evaluateCoverageRatchet(baselineFile, in.Coverage.Total, maxDropAbs); breached {
			fmt.Fprintf(stderr, "quality-report: coverage ratchet: %s\n", msg)
			return 3
		}
	}
	return 0
}

// evaluateCoverageRatchet reads the recorded baseline from baselineFile
// and compares it to current. Returns (msg, true) when the absolute
// drop exceeds maxDropAbs; (string, false) otherwise. Missing or
// unparseable baseline is treated as "no baseline" → pass.
func evaluateCoverageRatchet(baselineFile string, current, maxDropAbs float64) (string, bool) {
	baseline, ok := readCoverageBaseline(baselineFile)
	if !ok {
		return "", false
	}
	drop := baseline - current
	if drop <= maxDropAbs {
		return "", false
	}
	return fmt.Sprintf("coverage dropped from %.2f%% (baseline %s) to %.2f%% (current); drop %.2f%% > allowed %.2f%%",
		baseline, baselineFile, current, drop, maxDropAbs), true
}

// readCoverageBaseline reads a single float from baselineFile,
// tolerating whitespace and a trailing percent sign. Returns ok=false
// on read or parse error so the caller can skip the check.
func readCoverageBaseline(path string) (float64, bool) {
	if path == "" {
		return 0, false
	}
	b, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	s = strings.TrimSuffix(s, "%")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseRunMainFlags isolates the flag-parsing block from the rest of
// runMain so the orchestration is easier to test. Returns (rawDir,
// repoRoot, knownFailuresFile, baselineFile, maxDropAbs, exitCode).
// exitCode==0 means continue; otherwise caller returns it.
func parseRunMainFlags(args []string, stderr io.Writer) (string, string, string, string, float64, string, int) {
	fset := flag.NewFlagSet("quality-report", flag.ContinueOnError)
	fset.SetOutput(stderr)
	rawDir := fset.String("raw-dir", "", "directory with per-tool raw outputs")
	repoRoot := fset.String("repo-root", ".", "repo root (used to relativise paths)")
	knownFile := fset.String("known-failures", "", "file listing pre-existing test failures (Package/Test per line)")
	baselineFile := fset.String("coverage-baseline", "", "path to coverage baseline file (single float, e.g. \"73.5\"); empty disables the ratchet")
	maxDropAbs := fset.Float64("coverage-max-drop", 0.5, "max allowed absolute drop in Total coverage from baseline (percentage points)")
	coverageOut := fset.String("coverage-out", "", "path to a Go coverage profile to use; when set, regenerates <raw-dir>/coverage-func.out + coverage-per-package.tsv from it before ingesting. Lets the update-quality-report wrapper merge host + microvm profiles.")
	if err := fset.Parse(args); err != nil {
		return "", "", "", "", 0, "", 2
	}
	if *rawDir == "" {
		fmt.Fprintln(stderr, "quality-report: -raw-dir is required")
		return "", "", "", "", 0, "", 2
	}
	return *rawDir, *repoRoot, *knownFile, *baselineFile, *maxDropAbs, *coverageOut, 0
}

// regenerateCoverageArtifacts re-derives <rawDir>/coverage-func.out +
// coverage-per-package.tsv from the supplied coverage profile. Used by
// the update-quality-report --with-microvm path: after merging host +
// microvm coverage into one profile, this lets the aggregator re-run
// without rebuilding the entire Nix derivation.
//
// coverage-func.out is produced by shelling out to `go tool cover -func`
// (we can't easily reimplement that without parsing the full profile
// representation Go uses internally). The per-package TSV is built by
// directly parsing the atomic-mode profile in Go, mirroring the awk in
// nix/quality-report/default.nix.
func regenerateCoverageArtifacts(rawDir, profile string) error {
	if _, err := os.Stat(profile); err != nil {
		return fmt.Errorf("profile %q not readable: %w", profile, err)
	}
	// Run `go tool cover -func=<profile>` and capture stdout into
	// <rawDir>/coverage-func.out.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	funcOut, err := exec.CommandContext(ctx, "go", "tool", "cover", "-func="+profile).Output()
	if err != nil {
		return fmt.Errorf("go tool cover -func: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "coverage-func.out"), funcOut, 0o600); err != nil {
		return fmt.Errorf("write coverage-func.out: %w", err)
	}
	// Per-package TSV: aggregate statement coverage per package directory
	// from the atomic-mode profile.
	tsv, err := buildPerPackageTSV(profile)
	if err != nil {
		return fmt.Errorf("buildPerPackageTSV: %w", err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "coverage-per-package.tsv"), []byte(tsv), 0o600); err != nil {
		return fmt.Errorf("write coverage-per-package.tsv: %w", err)
	}
	return nil
}

// buildPerPackageTSV parses a Go coverage profile and emits a TSV of
// `<pkg>\t<percent>` lines, one per package directory under the repo
// module path. Mirrors the awk in nix/quality-report/default.nix: atomic-
// mode profiles can repeat the same block across test binaries, so we
// dedupe per file:range and keep the max count seen, then aggregate
// statements per package directory.
func buildPerPackageTSV(profile string) (string, error) {
	f, err := os.Open(profile) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	const modulePrefix = "github.com/randomizedcoder/xtcp2/"
	seenStmt := map[string]int{} // key → numStmt
	seenMaxCount := map[string]int{}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "mode:") {
			continue
		}
		// `path:range numStmt count`
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		if !strings.HasPrefix(fields[0], modulePrefix) {
			continue
		}
		numStmt, e1 := strconv.Atoi(fields[1])
		count, e2 := strconv.Atoi(fields[2])
		if e1 != nil || e2 != nil {
			continue
		}
		k := fields[0]
		if _, ok := seenStmt[k]; !ok {
			seenStmt[k] = numStmt
		}
		if count > seenMaxCount[k] {
			seenMaxCount[k] = count
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Aggregate per package directory: derive package from "path:range"
	// by stripping the `:range` suffix then the `/<file>.go` filename.
	type pkgAgg struct {
		tot int
		hit int
	}
	pkgs := map[string]*pkgAgg{}
	for k, numStmt := range seenStmt {
		path := k
		if i := strings.IndexByte(path, ':'); i >= 0 {
			path = path[:i]
		}
		path = strings.TrimPrefix(path, modulePrefix)
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			path = path[:i]
		}
		a, ok := pkgs[path]
		if !ok {
			a = &pkgAgg{}
			pkgs[path] = a
		}
		a.tot += numStmt
		if seenMaxCount[k] > 0 {
			a.hit += numStmt
		}
	}

	// Emit sorted TSV.
	keys := make([]string, 0, len(pkgs))
	for p := range pkgs {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, p := range keys {
		a := pkgs[p]
		pct := 0.0
		if a.tot > 0 {
			pct = 100.0 * float64(a.hit) / float64(a.tot)
		}
		fmt.Fprintf(&b, "%s\t%.1f\n", p, pct)
	}
	return b.String(), nil
}

// ingestGolangciTiers ingests findings from the comprehensive tier
// (strict superset of standard ⊇ quick, so we never count one finding
// in two tiers). Records runtime + exit code per tier so the Tool
// Status table reflects that all three ran.
func (c *ingestCtx) ingestGolangciTiers(in *reportInput) {
	tiers := []struct {
		name string
		t    int
	}{
		{"golangci-comprehensive", 2},
		{"golangci-standard", 1},
		{"golangci-quick", 0},
	}
	var golangciFindings []Finding
	var golangciSource string
	for _, tier := range tiers {
		path := filepath.Join(c.rawDir, tier.name+".json")
		fs, ok := parseGolangci(path, "golangci-lint", tier.t, c.repoRoot)
		if ok && len(fs) > 0 && golangciSource == "" {
			golangciFindings = fs
			golangciSource = tier.name
		}
		in.Status = append(in.Status, ToolStatus{
			Name:      "golangci-lint (" + strings.TrimPrefix(tier.name, "golangci-") + ")",
			ExitCode:  c.exitCodes[tier.name],
			Findings:  len(fs),
			RuntimeS:  c.runtimes[tier.name],
			Available: ok,
		})
	}
	for i := range golangciFindings {
		if t, ok := linterTier[golangciFindings[i].Rule]; ok {
			golangciFindings[i].Tier = t
		}
	}
	in.Findings = append(in.Findings, golangciFindings...)
}

// ingestGosec ingests the single gosec.json report.
func (c *ingestCtx) ingestGosec(in *reportInput) {
	path := filepath.Join(c.rawDir, "gosec.json")
	fs, ok := parseGosec(path, c.repoRoot)
	in.Findings = append(in.Findings, fs...)
	in.Status = append(in.Status, ToolStatus{
		Name:      toolGosec,
		ExitCode:  c.exitCodes[toolGosec],
		Findings:  len(fs),
		RuntimeS:  c.runtimes[toolGosec],
		Available: ok,
	})
}

// ingestGoVet ingests govet.out (line-per-finding output).
func (c *ingestCtx) ingestGoVet(in *reportInput) {
	path := filepath.Join(c.rawDir, "govet.out")
	fs, ok := parseLineFindings(path, "go-vet", 0, "")
	in.Findings = append(in.Findings, fs...)
	in.Status = append(in.Status, ToolStatus{
		Name:      "go vet",
		ExitCode:  c.exitCodes["govet"],
		Findings:  len(fs),
		RuntimeS:  c.runtimes["govet"],
		Available: ok,
	})
}

// ingestFormatter ingests one of the formatter file-list reports
// (gofmt / nix-fmt). exitCodeKey + statusName are split so gofmt can
// reuse the toolGofmt const and nix-fmt can use exitCodes["nix-fmt"]
// + status name "nixfmt".
func (c *ingestCtx) ingestFormatter(in *reportInput, exitCodeKey, fileName, statusName string, kept *[]string) {
	files := readLines(filepath.Join(c.rawDir, fileName))
	var rows []string
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f != "" {
			rows = append(rows, f)
		}
	}
	*kept = rows
	for _, f := range rows {
		in.Findings = append(in.Findings, Finding{
			Tool:     statusName,
			Rule:     ruleFormat,
			Severity: severityWarning,
			File:     f,
			Message:  "file not formatted",
		})
	}
	in.Status = append(in.Status, ToolStatus{
		Name:      statusName,
		ExitCode:  c.exitCodes[exitCodeKey],
		Findings:  len(rows),
		RuntimeS:  c.runtimes[exitCodeKey],
		Available: fileExists(filepath.Join(c.rawDir, fileName)),
	})
}

// ingestCustomAudits ingests the four xtcp2-specific AST audits.
func (c *ingestCtx) ingestCustomAudits(in *reportInput) {
	for _, a := range []string{"netlink-audit", "iouring-audit", "metrics-audit", "proto-field-audit"} {
		path := filepath.Join(c.rawDir, a+".out")
		fs, ok := parseAuditOutput(path, a)
		in.Findings = append(in.Findings, fs...)
		in.Status = append(in.Status, ToolStatus{
			Name:      a,
			ExitCode:  c.exitCodes[a],
			Findings:  len(fs),
			RuntimeS:  c.runtimes[a],
			Available: ok,
		})
	}
}

// ingestGoTest ingests the JSON test results + buckets failures into
// "new" vs "pre-existing" per the known-failures allowlist.
func (c *ingestCtx) ingestGoTest(in *reportInput) {
	path := filepath.Join(c.rawDir, "gotest.json")
	results, ok := parseGoTest(path, c.known)
	in.Tests = results
	failing := 0
	preexist := 0
	for _, r := range results {
		if r.Action != testActionFail {
			continue
		}
		if r.Preexist {
			preexist++
			continue
		}
		failing++
	}
	in.Status = append(in.Status, ToolStatus{
		Name:      "go test",
		ExitCode:  c.exitCodes["gotest"],
		Findings:  failing + preexist,
		RuntimeS:  c.runtimes["gotest"],
		Available: ok,
	})
}

// ingestCliHelpSmoke ingests the per-binary -h smoke matrix when one
// exists. In the quality-report sandbox the cmd binaries aren't on
// PATH, so this is usually empty; the matrix is covered separately by
// `nix flake check`.
func (c *ingestCtx) ingestCliHelpSmoke(in *reportInput) {
	path := filepath.Join(c.rawDir, "cli-help-smoke.out")
	results, ok := parseCliHelpSmoke(path)
	if !ok || len(results) == 0 {
		return
	}
	in.CliHelpResults = results
	failing := 0
	for _, r := range results {
		if !r.OK {
			failing++
		}
	}
	in.Status = append(in.Status, ToolStatus{
		Name:      "cli-help-smoke",
		ExitCode:  c.exitCodes["cli-help-smoke"],
		Findings:  failing,
		RuntimeS:  c.runtimes["cli-help-smoke"],
		Available: true,
	})
}

// ingestCoverage parses coverage-func.out + coverage-per-package.tsv
// (when present) and surfaces each below-90% package as a tier-0
// Finding so it bubbles up in the executive summary's tier rollup.
func (c *ingestCtx) ingestCoverage(in *reportInput) {
	in.Coverage = parseCoverage(c.rawDir)
	if !in.Coverage.Available {
		return
	}
	in.Status = append(in.Status, ToolStatus{
		Name:      "go test -cover",
		ExitCode:  c.exitCodes["coverage"],
		Findings:  countBelowThreshold(in.Coverage),
		RuntimeS:  c.runtimes["coverage"],
		Available: true,
	})
	in.Findings = append(in.Findings, coverageFindings(in.Coverage)...)
}

// ─── parsers ───────────────────────────────────────────────────────────────

type golangciJSON struct {
	Issues []struct {
		FromLinter  string   `json:"FromLinter"`
		Text        string   `json:"Text"`
		Severity    string   `json:"Severity"`
		SourceLines []string `json:"SourceLines"`
		Pos         struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
			Column   int    `json:"Column"`
		} `json:"Pos"`
	} `json:"Issues"`
}

func parseGolangci(path, tool string, tier int, repoRoot string) ([]Finding, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	// golangci-lint can emit some garbage on stderr that may leak through;
	// find the first '{' to be defensive.
	if i := bytesIndex(b, []byte("{")); i > 0 {
		b = b[i:]
	}
	if len(b) == 0 {
		return nil, true
	}
	var j golangciJSON
	if err = json.Unmarshal(b, &j); err != nil {
		// Tolerate: emit a single synthetic finding noting the parse failure.
		return []Finding{{
			Tool:     tool,
			Tier:     tier,
			Rule:     "internal/parse-error",
			Severity: severityInfo,
			Message:  fmt.Sprintf("could not parse JSON: %v", err),
		}}, true
	}
	out := make([]Finding, 0, len(j.Issues))
	for _, is := range j.Issues {
		out = append(out, Finding{
			Tool:     tool,
			Tier:     tier,
			Rule:     is.FromLinter,
			Severity: is.Severity,
			File:     relpath(is.Pos.Filename, repoRoot),
			Line:     is.Pos.Line,
			Column:   is.Pos.Column,
			Message:  is.Text,
		})
	}
	return out, true
}

type gosecJSON struct {
	Issues []struct {
		Severity   string `json:"severity"`
		Confidence string `json:"confidence"`
		CWE        struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"cwe"`
		RuleID  string `json:"rule_id"`
		Details string `json:"details"`
		File    string `json:"file"`
		Line    string `json:"line"`
		Column  string `json:"column"`
	} `json:"Issues"`
}

func parseGosec(path, repoRoot string) ([]Finding, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if i := bytesIndex(b, []byte("{")); i > 0 {
		b = b[i:]
	}
	if len(b) == 0 {
		return nil, true
	}
	var j gosecJSON
	if err = json.Unmarshal(b, &j); err != nil {
		return []Finding{{
			Tool:     toolGosec,
			Rule:     "internal/parse-error",
			Severity: severityInfo,
			Message:  fmt.Sprintf("could not parse JSON: %v", err),
		}}, true
	}
	out := make([]Finding, 0, len(j.Issues))
	for i := range j.Issues {
		is := &j.Issues[i]
		ln := atoiOr0(strings.SplitN(is.Line, "-", 2)[0])
		col := atoiOr0(is.Column)
		msg := is.Details
		if is.CWE.ID != "" {
			msg = fmt.Sprintf("%s (CWE-%s)", msg, is.CWE.ID)
		}
		out = append(out, Finding{
			Tool:     toolGosec,
			Rule:     is.RuleID,
			Severity: strings.ToLower(is.Severity),
			File:     relpath(is.File, repoRoot),
			Line:     ln,
			Column:   col,
			Message:  msg,
		})
	}
	return out, true
}

var (
	reFileLineCol = regexp.MustCompile(`^([^:\s][^:]*):(\d+):(\d+):\s*(.+)$`)
	reFileLine    = regexp.MustCompile(`^([^:\s][^:]*):(\d+):\s*(.+)$`)
)

// parseLineFindings parses tools that emit lines like `file:line:col: msg`
// or `file:line: msg`. Skips header/footer noise.
func parseLineFindings(path, tool string, tier int, defaultRule string) ([]Finding, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var out []Finding
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		// go vet pads with "# package\n" headers; skip those + obvious noise.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := reFileLineCol.FindStringSubmatch(line); m != nil {
			ln := atoiOr0(m[2])
			col := atoiOr0(m[3])
			out = append(out, Finding{
				Tool: tool, Tier: tier, Rule: defaultRule,
				Severity: severityWarning,
				File:     m[1], Line: ln, Column: col, Message: m[4],
			})
			continue
		}
		if m := reFileLine.FindStringSubmatch(line); m != nil {
			ln := atoiOr0(m[2])
			out = append(out, Finding{
				Tool: tool, Tier: tier, Rule: defaultRule,
				Severity: severityWarning,
				File:     m[1], Line: ln, Message: m[3],
			})
			continue
		}
	}
	return out, true
}

// parseAuditOutput handles the four custom audit tools whose stdout
// resembles `file:line:col: msg` or just a summary line.
func parseAuditOutput(path, tool string) ([]Finding, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var out []Finding
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		// Audit-summary lines like "proto-field-audit: 123 fields scanned" or
		// "proto-field-audit: no findings" are informational, not findings.
		if strings.HasPrefix(line, tool+":") {
			continue
		}
		if m := reFileLineCol.FindStringSubmatch(line); m != nil {
			ln := atoiOr0(m[2])
			col := atoiOr0(m[3])
			out = append(out, Finding{
				Tool: tool, Severity: severityWarning,
				File: m[1], Line: ln, Column: col, Message: m[4],
			})
			continue
		}
		if m := reFileLine.FindStringSubmatch(line); m != nil {
			ln := atoiOr0(m[2])
			out = append(out, Finding{
				Tool: tool, Severity: severityWarning,
				File: m[1], Line: ln, Message: m[3],
			})
			continue
		}
		// Anything else: keep as a free-form finding for visibility.
		out = append(out, Finding{
			Tool: tool, Severity: severityInfo, Message: line,
		})
	}
	return out, true
}

// parseGoTest reads the `go test -json` event stream.
// goTestEvent mirrors the JSON record `go test -json` emits per state
// transition. Lifted to package scope so the per-event helpers don't
// need a copy of the anonymous struct.
type goTestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

// applyTestEvent mutates results/failOutput based on one event from
// `go test -json`. Each `go test` Action ends up as one transition on
// either the per-test TestResult or its accumulated output buffer.
// Extracted so the surrounding decoder loop becomes flat.
func applyTestEvent(results map[string]*TestResult, failOutput map[string]string, e goTestEvent, known map[string]bool) {
	if e.Action == "" {
		return
	}
	key := e.Package + "/" + e.Test
	switch e.Action {
	case "run":
		results[key] = &TestResult{Package: e.Package, Test: e.Test}
	case "output":
		if e.Test != "" {
			failOutput[key] += e.Output
		}
	case testActionPass, testActionFail, testActionSkip:
		recordTerminalAction(results, failOutput, key, e, known)
	}
}

// recordTerminalAction sets the Action/Elapsed on the per-test entry
// and (for failures) attaches accumulated output + the known-failures
// classification. Pulled out of applyTestEvent so each helper is
// linear top-to-bottom.
func recordTerminalAction(results map[string]*TestResult, failOutput map[string]string, key string, e goTestEvent, known map[string]bool) {
	r := results[key]
	if r == nil {
		r = &TestResult{Package: e.Package, Test: e.Test}
		results[key] = r
	}
	r.Action = e.Action
	r.Elapsed = e.Elapsed
	if e.Action != testActionFail {
		return
	}
	r.Output = failOutput[key]
	name := e.Package
	if e.Test != "" {
		name += "." + e.Test
	}
	r.Preexist = known[name] || known[e.Test]
}

// finalizeTestResults converts the per-key map into a sorted slice for
// the template. Sorted by (Package, Test) ascending so the report's
// failing-tests list is deterministic across runs.
func finalizeTestResults(results map[string]*TestResult) []TestResult {
	out := make([]TestResult, 0, len(results))
	for _, r := range results {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Package != out[j].Package {
			return out[i].Package < out[j].Package
		}
		return out[i].Test < out[j].Test
	})
	return out
}

func parseGoTest(path string, known map[string]bool) ([]TestResult, bool) {
	f, err := os.Open(path) //nolint:gosec // test report path is operator-supplied via -raw-dir flag
	if err != nil {
		return nil, false
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only file; close error is non-actionable

	failOutput := map[string]string{} // pkg/test -> accumulated output
	results := map[string]*TestResult{}
	dec := json.NewDecoder(f)
	for {
		var e goTestEvent
		derr := dec.Decode(&e)
		if derr == io.EOF {
			break
		}
		if derr != nil {
			// Some Go test output mixes JSON with stderr; skip non-JSON tokens.
			continue
		}
		applyTestEvent(results, failOutput, e, known)
	}
	return finalizeTestResults(results), true
}

func parseCliHelpSmoke(path string) ([]CliHelpResult, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var out []CliHelpResult
	scanner := bufio.NewScanner(f)
	// Lines: `<binary> <rc> <bytes>`
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		rc := atoiOr0(fields[1])
		bytes := atoiOr0(fields[2])
		out = append(out, CliHelpResult{
			Binary:   fields[0],
			ExitCode: rc,
			Bytes:    bytes,
			OK:       rc <= 2 && bytes > 0,
		})
	}
	return out, true
}

// parseExclusions reads .golangci*.yml and extracts each exclude rule + its
// preceding comment as justification text. Lightweight regex-based — full
// YAML parsing would force a yaml dep for marginal value.
var reExcludeKey = regexp.MustCompile(`^\s*-\s+(path|linters|text):\s*(.+)$`)

func parseExclusions(repoRoot string) []ConfigExclusion {
	var out []ConfigExclusion
	cfgs := []string{
		".golangci-quick.yml",
		".golangci.yml",
		".golangci-comprehensive.yml",
	}
	for _, cfg := range cfgs {
		path := filepath.Join(repoRoot, cfg)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var lastComment string
		inExcludeRules := false
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				if c := strings.TrimSpace(strings.TrimPrefix(trimmed, "#")); c != "" {
					lastComment = c
				}
				continue
			}
			if strings.HasPrefix(trimmed, "exclude-rules:") {
				inExcludeRules = true
				continue
			}
			if inExcludeRules && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
				inExcludeRules = false
			}
			if !inExcludeRules {
				lastComment = ""
				continue
			}
			if m := reExcludeKey.FindStringSubmatch(line); m != nil {
				out = append(out, ConfigExclusion{
					Source:    cfg,
					Rule:      m[2],
					Scope:     m[1],
					Justified: lastComment != "",
					Note:      lastComment,
				})
				lastComment = ""
			}
		}
		_ = f.Close()
	}
	// gosec exclusions — hardcoded in nix/checks/go-sec.nix.
	out = append(out,
		ConfigExclusion{Source: goSecNixPath, Rule: "G103", Scope: toolGosec, Justified: true,
			Note: "unsafe pointers: required by pkg/io_uring (giouring wraps liburing SQE/CQE with unsafe.Pointer)"},
		ConfigExclusion{Source: goSecNixPath, Rule: "G115", Scope: toolGosec, Justified: true,
			Note: "integer overflow conversions: netlink length fields + io_uring batch indices, all bounds-checked"},
		ConfigExclusion{Source: goSecNixPath, Rule: "G204", Scope: toolGosec, Justified: true,
			Note: "subprocess with variable: cmd/ns + cmd/nsTest invoke `ip netns exec ...` by design"},
		ConfigExclusion{Source: goSecNixPath, Rule: "G304", Scope: toolGosec, Justified: true,
			Note: "file path from variable: register_schema reads .proto paths from CLI"},
	)
	return out
}

// parseCoverage reads the artifacts produced by the orchestrator's
// coverage post-processing block:
//
//   - <rawDir>/coverage-func.out — `go tool cover -func` output; the
//     last line is `total:\t(statements)\tNN.N%`.
//   - <rawDir>/coverage-per-package.tsv — `<pkg>\t<percent>` per row,
//     emitted by the awk pass over coverage-func.out.
//
// Either file missing is fine; the report renders "n/a" instead of a
// percentage in that case.
func parseCoverage(rawDir string) Coverage {
	cov := Coverage{PerPackage: map[string]float64{}}

	funcPath := filepath.Join(rawDir, "coverage-func.out")
	if data, err := os.ReadFile(funcPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "total:") {
				continue
			}
			// `total:\t(statements)\tNN.N%`
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				pct := strings.TrimSuffix(fields[len(fields)-1], "%")
				if v, perr := strconv.ParseFloat(pct, 64); perr == nil {
					cov.Total = v
					cov.Available = true
				}
			}
		}
	}

	tsvPath := filepath.Join(rawDir, "coverage-per-package.tsv")
	if data, err := os.ReadFile(tsvPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) != 2 {
				continue
			}
			if v, perr := strconv.ParseFloat(parts[1], 64); perr == nil {
				cov.PerPackage[parts[0]] = v
				cov.Available = true
			}
		}
	}
	return cov
}

// ─── helpers ───────────────────────────────────────────────────────────────

func loadKnownFailures(path string) map[string]bool {
	out := map[string]bool{}
	if path == "" {
		return out
	}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out
}

func readFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func readLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

func readKVFile(path string) map[string]string {
	out := map[string]string{}
	for _, line := range readLines(path) {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}

func readRuntimes(path string) map[string]int {
	out := map[string]int{}
	for k, v := range readKVFile(path) {
		out[k] = atoiOr0(v)
	}
	return out
}

func readExitCodes(path string) map[string]int {
	return readRuntimes(path) // same shape: key=int
}

// atoiOr0 is a best-effort parse for already-validated numeric strings
// (regex digit captures, golangci-lint JSON column fields, runtime KV
// values, etc.). Anywhere the upstream guarantees parseability, the
// error is uninteresting.
func atoiOr0(s string) int {
	n, _ := strconv.Atoi(s) //nolint:errcheck // best-effort parse of pre-validated digits
	return n
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func bytesIndex(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j, c := range needle {
			if haystack[i+j] != c {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func relpath(p, root string) string {
	if root == "" {
		return p
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return p
	}
	if r, rerr := filepath.Rel(abs, p); rerr == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}

// ─── aggregation ───────────────────────────────────────────────────────────

type linterAgg struct {
	Linter  string
	Tool    string
	Count   int
	Samples []Finding
	Files   map[string]int
}

func aggregateByLinter(findings []Finding) []linterAgg {
	m := map[string]*linterAgg{}
	for _, f := range findings {
		key := f.Tool + "::" + f.Rule
		if _, ok := m[key]; !ok {
			m[key] = &linterAgg{
				Linter: f.Rule,
				Tool:   f.Tool,
				Files:  map[string]int{},
			}
		}
		a := m[key]
		a.Count++
		a.Files[f.File]++
		if len(a.Samples) < 3 {
			a.Samples = append(a.Samples, f)
		}
	}
	out := make([]linterAgg, 0, len(m))
	for _, a := range m {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Linter < out[j].Linter
	})
	return out
}

type fileAgg struct {
	File  string
	Count int
	Top   []string // top rules in this file
}

func aggregateByFile(findings []Finding) []fileAgg {
	m := map[string]map[string]int{}
	for _, f := range findings {
		if f.File == "" {
			continue
		}
		if m[f.File] == nil {
			m[f.File] = map[string]int{}
		}
		m[f.File][f.Rule]++
	}
	out := make([]fileAgg, 0, len(m))
	for file, rules := range m {
		total := 0
		type rc struct {
			r string
			c int
		}
		rcs := make([]rc, 0, len(rules))
		for r, c := range rules {
			total += c
			rcs = append(rcs, rc{r, c})
		}
		sort.Slice(rcs, func(i, j int) bool { return rcs[i].c > rcs[j].c })
		top := []string{}
		for i := 0; i < len(rcs) && i < 3; i++ {
			top = append(top, fmt.Sprintf("%s×%d", rcs[i].r, rcs[i].c))
		}
		out = append(out, fileAgg{File: file, Count: total, Top: top})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].File < out[j].File
	})
	return out
}

// ─── markdown emission ─────────────────────────────────────────────────────

const tmpl = `# xtcp2 code-quality report

Generated: {{.Generated.Format "2006-01-02T15:04:05Z"}}{{if .CommitSHA}}
Commit: ` + "`{{.CommitSHA}}`" + `{{end}}{{if .Branch}}
Branch: ` + "`{{.Branch}}`" + `{{end}}

Tool versions: {{range $k, $v := .Versions}}{{$k}}={{$v}}; {{end}}

This report is generated by ` + "`tools/quality-report`" + ` and refreshed via
` + "`nix run .#update-quality-report`" + `. Every section is regenerated end-to-end on
each refresh; section anchors are stable so ` + "`git diff docs/quality-report.md`" + `
between commits reveals exactly what changed.

---

## 1. Executive summary

| Metric | Value |
|---|---|
| Total findings | {{.TotalFindings}} |
| Findings (Tier 0) | {{.TierCounts.T0}} |
| Findings (Tier 1) | {{.TierCounts.T1}} |
| Findings (Tier 2) | {{.TierCounts.T2}} |
| Findings (non-tiered) | {{.TierCounts.NT}} |
| Files with at least one finding | {{.FilesAffected}} |
| Test failures (new) | {{.NewTestFails}} |
| Test failures (pre-existing) | {{.PreexistTestFails}} |
| Config exclusions reviewed | {{len .Exclusions}} |

{{if .HasErrSeverity}}**At least one finding has severity "error" — must fix.**

{{end}}---

## 2. Tool status

| Tool | Status | Findings | Runtime |
|---|---|---|---|
{{range .Status}}| {{.Name}} | {{statusLabel .}} | {{.Findings}} | {{.RuntimeS}}s |
{{end}}

---

## 3. Tier rollup

| Tier | Linters | Findings | Quick-fixable¹ |
|---|---|---|---|
| 0 (` + "`lint-quick`" + `) | govet, errcheck, ineffassign, unused, staticcheck | {{.TierCounts.T0}} | {{.QuickFixable.T0}} |
| 1 (` + "`lint`" + ` / CI) | Tier 0 + gosec, gocritic, revive, noctx, contextcheck, durationcheck | {{.TierCounts.T1}} | {{.QuickFixable.T1}} |
| 2 (` + "`lint-comprehensive`" + `) | Tier 1 + exhaustive, prealloc, gocyclo, funlen, goconst, dupl, unconvert, nakedret, misspell | {{.TierCounts.T2}} | {{.QuickFixable.T2}} |

¹ Quick-fixable = produced by a linter that supports ` + "`golangci-lint run --fix`" + ` (gofmt, goimports, misspell, unconvert, …).

---

## 4. Hotspot files (top 10)

{{if .Hotspots}}| File | Findings | Top rules |
|---|---|---|
{{range .Hotspots}}| ` + "`{{.File}}`" + ` | {{.Count}} | {{join .Top ", "}} |
{{end}}{{else}}*No file-attributed findings.*
{{end}}

---

## 5. Findings by linter

{{range .ByLinter}}### {{.Tool}} / {{.Linter}} — {{.Count}}

{{range .Samples}}- ` + "`{{.File}}{{if .Line}}:{{.Line}}{{end}}`" + `: {{.Message}}
{{end}}
{{end}}{{if not .ByLinter}}*No linter findings.*

{{end}}---

## 6. Custom audits

{{range .Audits}}### {{.Tool}} — {{.Count}}

{{if .Samples}}{{range .Samples}}- {{if .File}}` + "`{{.File}}{{if .Line}}:{{.Line}}{{end}}`" + `: {{end}}{{.Message}}
{{end}}{{else}}*No findings.*
{{end}}
{{end}}{{if not .Audits}}*No audit findings.*

{{end}}---

## 7. Security (gosec)

{{if .Gosec}}{{range .Gosec}}- **{{if .Severity}}{{.Severity}}{{else}}?{{end}}** ` + "`{{.Rule}}`" + ` at ` + "`{{.File}}:{{.Line}}`" + ` — {{.Message}}
{{end}}{{else}}*No security findings.*

{{end}}

---

## 8. Test results

{{if .TestStats.Total}}| Status | Count |
|---|---|
| Pass | {{.TestStats.Pass}} |
| Fail (new) | {{.TestStats.FailNew}} |
| Fail (pre-existing) | {{.TestStats.FailPre}} |
| Skip | {{.TestStats.Skip}} |

{{if .FailingTests}}**Failures:**

{{range .FailingTests}}- {{if .Preexist}}🟡 **PRE-EXISTING** {{else}}🔴 {{end}}` + "`{{.Package}}`" + `{{if .Test}} / ` + "`{{.Test}}`" + `{{end}}
{{end}}{{end}}{{else}}*Tests did not run in the report sandbox.*
{{end}}

---

## 9. CLI ` + "`-help`" + ` smoke

{{if .CliHelpResults}}| Binary | Exit code | Output bytes | Status |
|---|---|---|---|
{{range .CliHelpResults}}| ` + "`{{.Binary}}`" + ` | {{.ExitCode}} | {{.Bytes}} | {{if .OK}}OK{{else}}**FAIL**{{end}} |
{{end}}{{else}}*Not run.*
{{end}}

---

## 10. Format checks

{{if .GofmtFiles}}**` + "`gofmt`" + ` would reformat ({{len .GofmtFiles}} file{{if ne (len .GofmtFiles) 1}}s{{end}}):**

{{range .GofmtFiles}}- ` + "`{{.}}`" + `
{{end}}{{else}}` + "`gofmt`" + `: clean.

{{end}}{{if .NixfmtFiles}}**` + "`nixfmt`" + ` would reformat ({{len .NixfmtFiles}} file{{if ne (len .NixfmtFiles) 1}}s{{end}}):**

{{range .NixfmtFiles}}- ` + "`{{.}}`" + `
{{end}}{{else}}` + "`nixfmt`" + `: clean.

{{end}}---

## 11. Configuration audit

Every linter exclusion in the repo, with the recovered justification from
the adjacent YAML comment. Rows with no justification need review.

| Source | Rule | Scope | Justification |
|---|---|---|---|
{{range .Exclusions}}| ` + "`{{.Source}}`" + ` | ` + "`{{.Rule}}`" + ` | {{.Scope}} | {{if .Justified}}{{.Note}}{{else}}**(missing)**{{end}} |
{{end}}

---

## 12. Recommendations

{{range .Recommendations}}- {{.}}
{{end}}

---

## 13. Test coverage

{{if .Coverage.Available}}**Overall:** {{printf "%.1f" .Coverage.Total}}% of statements (target: {{printf "%.0f" .CoverageThreshold}}% per package).

{{if .CoverageRows}}| Package | Coverage | Status |
|---|---|---|
{{range .CoverageRows}}| ` + "`{{.Pkg}}`" + ` | {{printf "%.1f" .Pct}}% | {{if .Below}}🔴 below {{printf "%.0f" $.CoverageThreshold}}%{{else}}🟢 OK{{end}} |
{{end}}
{{end}}{{else}}*Coverage profile not available — go test did not run or produced no profile.*
{{end}}
`

type tierCounts struct {
	T0, T1, T2, NT int
}

type quickFixableCounts struct {
	T0, T1, T2 int
}

type testStats struct {
	Total, Pass, FailNew, FailPre, Skip int
}

type renderInput struct {
	reportInput
	TotalFindings     int
	FilesAffected     int
	NewTestFails      int
	PreexistTestFails int
	HasErrSeverity    bool
	TierCounts        tierCounts
	QuickFixable      quickFixableCounts
	Hotspots          []fileAgg
	ByLinter          []linterAgg
	Audits            []linterAgg
	Gosec             []Finding
	TestStats         testStats
	FailingTests      []TestResult
	Recommendations   []string
	CoverageRows      []coverageRow
	CoverageThreshold float64
}

// coverageRow renders one package's coverage percentage with a flag for
// whether it sits below the CoverageThreshold.
type coverageRow struct {
	Pkg   string
	Pct   float64
	Below bool
}

// emit assembles the renderInput from in (counts, splits, sort, test
// stats, coverage rows, recommendations) and renders the markdown
// template to w.
//
// Previously a single 100+ line function with cyclo 27 mixing all of
// the above concerns inline. Each phase is now a helper below; emit
// is the orchestrator (cyclo 3).
func emit(w io.Writer, in reportInput) error {
	r := renderInput{reportInput: in}
	r.TotalFindings = len(in.Findings)

	r.FilesAffected = accumulateFindingCounts(&r, in.Findings)
	r.Hotspots = topHotspots(in.Findings, 10)

	linter, audit, gosec := splitFindingsByTool(in.Findings)
	r.ByLinter = aggregateByLinter(linter)
	r.Audits = aggregateByLinter(audit)
	r.Gosec = gosec
	sortGosecBySeverityFileLine(r.Gosec)

	accumulateTestStats(&r, in.Tests)

	r.CoverageThreshold = CoverageThreshold
	r.CoverageRows = buildCoverageRows(in.Coverage)

	r.Recommendations = synthRecommendations(r)

	t := template.Must(template.New("report").
		Funcs(template.FuncMap{
			"statusLabel": statusLabel,
			"join":        strings.Join,
		}).
		Parse(tmpl))
	return t.Execute(w, r)
}

// accumulateFindingCounts walks findings once and increments the
// per-tier counts + the quick-fixable per-tier counts on r, sets
// HasErrSeverity, and returns the number of distinct files touched.
// Single pass; replaces a nested-switch monolith inside emit.
func accumulateFindingCounts(r *renderInput, findings []Finding) int {
	files := map[string]bool{}
	for _, f := range findings {
		if f.File != "" {
			files[f.File] = true
		}
		bumpTierCount(&r.TierCounts, f)
		if f.Severity == severityError {
			r.HasErrSeverity = true
		}
		if isQuickFixableRule(f.Rule) {
			bumpQuickFixable(&r.QuickFixable, f.Tier)
		}
	}
	return len(files)
}

// bumpTierCount increments the appropriate tier counter for a single
// finding. Tier 0 splits into T0 (golangci) vs NT (non-tiered tool).
func bumpTierCount(tc *tierCounts, f Finding) {
	switch f.Tier {
	case 0:
		if isTieredTool(f.Tool) {
			tc.T0++
			return
		}
		tc.NT++
	case 1:
		tc.T1++
	case 2:
		tc.T2++
	}
}

// bumpQuickFixable increments the per-tier quick-fixable counter.
// Takes *quickFixableCounts (not *tierCounts) because the QuickFixable
// counter is a distinct type — they share field names but distinct
// types prevent accidentally passing one where the other is expected.
func bumpQuickFixable(qf *quickFixableCounts, tier int) {
	switch tier {
	case 0:
		qf.T0++
	case 1:
		qf.T1++
	case 2:
		qf.T2++
	}
}

// topHotspots returns the top-N file-aggregated entries from
// aggregateByFile. Always returns at most n elements.
func topHotspots(findings []Finding, n int) []fileAgg {
	out := aggregateByFile(findings)
	if len(out) > n {
		return out[:n]
	}
	return out
}

// splitFindingsByTool buckets findings into (linter, audit, gosec) for
// the corresponding sections of the markdown report. Audit tool names
// are hard-coded here so a future audit must be added to both this
// switch and the ingestCustomAudits loop — the symmetry is intentional.
func splitFindingsByTool(findings []Finding) (linter, audit, gosec []Finding) {
	for _, f := range findings {
		switch f.Tool {
		case "netlink-audit", "iouring-audit", "metrics-audit", "proto-field-audit":
			audit = append(audit, f)
		case toolGosec:
			gosec = append(gosec, f)
		default:
			linter = append(linter, f)
		}
	}
	return linter, audit, gosec
}

// sortGosecBySeverityFileLine sorts gosec findings by (severity rank
// asc, file asc, line asc). Mutates the slice in place — same semantics
// as the previous inline sort.Slice.
func sortGosecBySeverityFileLine(gosec []Finding) {
	sort.Slice(gosec, func(i, j int) bool {
		if gosec[i].Severity != gosec[j].Severity {
			return severityOrder(gosec[i].Severity) < severityOrder(gosec[j].Severity)
		}
		if gosec[i].File != gosec[j].File {
			return gosec[i].File < gosec[j].File
		}
		return gosec[i].Line < gosec[j].Line
	})
}

// accumulateTestStats walks tests once and increments Pass/Fail/Skip
// counters on r.TestStats + appends per-test failures with a non-empty
// test name to r.FailingTests. PreexistTestFails + NewTestFails are
// the flat counters surfaced in the executive summary.
func accumulateTestStats(r *renderInput, tests []TestResult) {
	for _, t := range tests {
		r.TestStats.Total++
		switch t.Action {
		case testActionPass:
			r.TestStats.Pass++
		case testActionFail:
			recordTestFailure(r, t)
		case testActionSkip:
			r.TestStats.Skip++
		}
	}
}

// recordTestFailure splits one failing test into pre-existing vs new
// and appends to FailingTests when the entry is at test (not package)
// granularity. Extracted from the nested switch in accumulateTestStats
// so the surrounding switch reads top-to-bottom.
func recordTestFailure(r *renderInput, t TestResult) {
	if t.Preexist {
		r.TestStats.FailPre++
		r.PreexistTestFails++
	} else {
		r.TestStats.FailNew++
		r.NewTestFails++
	}
	if t.Test != "" {
		r.FailingTests = append(r.FailingTests, t)
	}
}

// buildCoverageRows turns the (unordered) per-package coverage map into
// a sorted slice of coverageRow records. Marks each row Below=true when
// the percentage is under CoverageThreshold. Returns nil when coverage
// data isn't available.
func buildCoverageRows(cov Coverage) []coverageRow {
	if !cov.Available {
		return nil
	}
	pkgs := make([]string, 0, len(cov.PerPackage))
	for p := range cov.PerPackage {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	rows := make([]coverageRow, 0, len(pkgs))
	for _, p := range pkgs {
		pct := cov.PerPackage[p]
		rows = append(rows, coverageRow{
			Pkg:   p,
			Pct:   pct,
			Below: pct < CoverageThreshold,
		})
	}
	return rows
}

func statusLabel(s ToolStatus) string {
	if !s.Available {
		return "not run"
	}
	if s.ExitCode == 0 && s.Findings == 0 {
		return "clean"
	}
	if s.Findings > 0 {
		return "findings"
	}
	return fmt.Sprintf("exit %d", s.ExitCode)
}

func isTieredTool(tool string) bool {
	return strings.HasPrefix(tool, "golangci-")
}

func isQuickFixableRule(rule string) bool {
	switch rule {
	case toolGofmt, "goimports", "misspell", "unconvert", ruleFormat:
		return true
	}
	return false
}

func severityOrder(s string) int {
	switch strings.ToLower(s) {
	case "high", severityError:
		return 0
	case "medium", severityWarning:
		return 1
	case "low", severityInfo:
		return 2
	}
	return 3
}

func synthRecommendations(r renderInput) []string {
	var recs []string
	if r.HasErrSeverity {
		recs = append(recs, "Address error-severity findings first — they're blockers in any tier they appear in.")
	}
	// Top linter
	if len(r.ByLinter) > 0 && r.ByLinter[0].Count > 0 {
		top := r.ByLinter[0]
		// Guard against div-by-zero / Inf. r.TotalFindings should always
		// be >= top.Count when ByLinter is populated, but a defensive
		// check keeps the report from rendering "+Inf%% of total" if a
		// future caller passes inconsistent aggregates.
		var share float64
		if r.TotalFindings > 0 {
			share = float64(top.Count) / float64(r.TotalFindings) * 100
		}
		recs = append(recs, fmt.Sprintf("Top contributor: **%s/%s** with %d findings (%.0f%% of total). Concentrate effort here for the biggest quality win.", top.Tool, top.Linter, top.Count, share))
	}
	// Quick-fix call out
	totalQF := r.QuickFixable.T0 + r.QuickFixable.T1 + r.QuickFixable.T2
	if totalQF > 0 {
		recs = append(recs, fmt.Sprintf("Run `lint-fix` (or `golangci-lint run --fix`) to auto-resolve ~%d quick-fixable findings before manual review.", totalQF))
	}
	// Hotspot file
	if len(r.Hotspots) > 0 {
		top := r.Hotspots[0]
		recs = append(recs, fmt.Sprintf("Hotspot file: `%s` carries %d findings (%s). Refactor here before touching adjacent code.", top.File, top.Count, strings.Join(top.Top, ", ")))
	}
	// Unjustified exclusions
	missing := 0
	for _, e := range r.Exclusions {
		if !e.Justified {
			missing++
		}
	}
	if missing > 0 {
		recs = append(recs, fmt.Sprintf("%d linter exclusion(s) have no justification comment — review whether they're still needed.", missing))
	}
	// Pre-existing failures
	if r.PreexistTestFails > 0 {
		recs = append(recs, fmt.Sprintf("%d pre-existing test failure(s) tracked via `tools/quality-report/known-failures.txt`. Schedule a focused fix-up; today they're masking real regression signal.", r.PreexistTestFails))
	}
	// Format hygiene
	if len(r.GofmtFiles) > 0 || len(r.NixfmtFiles) > 0 {
		recs = append(recs, "Format files are out of sync — run `gofmt -w .` and `nixfmt **/*.nix` to bring formatting back to baseline.")
	}
	if len(recs) == 0 {
		recs = append(recs, "No specific recommendations — the codebase is clean across every tier the report measures.")
	}
	return recs
}
