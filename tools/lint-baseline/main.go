// lint-baseline
//
// Compares each golangci-lint tier's findings against a committed baseline and
// fails when a GATED tier gains one.
//
// # Why this exists
//
// `nix flake check` is red on eight checks at baseline, so its exit code has
// been 1 for longer than any individual finding and cannot announce a new one.
// The tier is not unwatched; its red is unreadable. A check that is GREEN today
// and goes red the moment the finding list changes is the only thing that can
// report a regression. See docs/static-analysis.md.
//
// # Why a finding LIST and not a count
//
// Two of the sixteen fixes in the Tier 0 pass introduced five new findings
// while the total fell from 46 to 30, so a count-based bar would have read a
// regression as progress. Two consequences, and both are load-bearing:
//
//   - Keys drop line:col, so a finding that merely MOVED does not read as new.
//   - The diff runs in BOTH directions, because one direction cannot
//     distinguish "unchanged" from "one finding swapped for another".
//
// # Why not shell comm
//
// The hand recipe in docs/static-analysis.md is ten lines of sed/sort/comm, and
// proto-audit-netlink.nix sets a precedent for set subtraction inside a check.
// Two silent failure modes rule it out. golangci-lint exits 4 on `run.timeout`
// AFTER printing "0 issues", so a timed-out run would bake a falsely-clean
// baseline; and a malformed baseline must fail CLOSED, which readCoverageBaseline
// does not (TODO-SOON.md files that as a bug). Both are cases a table-driven Go
// test pins and a pipeline gets wrong.
//
// Exit codes:
//
//	0 = no added findings in any gated tier. Removals are an improvement and
//	    never fail: a ratchet that blocks a fix is worse than no ratchet.
//	1 = a gated tier gained at least one finding.
//	2 = internal error: bad flags, unreadable input, a tier that is gated but
//	    was never measured, or a baseline that is missing, unparseable,
//	    unsorted, duplicated, or carrying an unsuppressible key. Fails CLOSED —
//	    neither an unusable baseline nor an unchecked gate is a pass.
//	3 = refuse to ratchet: a tier's own golangci exit code was > 1, so the run
//	    is a timeout or a crash rather than a measurement.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// tier names one golangci config. The set is closed: an unknown tier in the
// baseline or on the command line is an error, so a typo cannot quietly create
// a fourth tier that nothing gates.
type tier string

const (
	// tier0 is .golangci-quick.yml, the ~90s pre-commit tier.
	tier0 tier = "tier0"
	// tier1 is .golangci.yml, the gating tier.
	tier1 tier = "tier1"
	// tier2 is .golangci-comprehensive.yml, the only tier enabling gocyclo,
	// funlen, goconst, unconvert and exhaustive.
	tier2 tier = "tier2"
)

// tierChecks maps each tier to the nix check that produces it, so a failure
// message names the command the reader has to run rather than a tier number.
var tierChecks = map[tier]string{
	tier0: "golangci-lint-quick",
	tier1: "golangci-lint",
	tier2: "golangci-lint-comprehensive",
}

// allTiers is iteration order for reports. Explicit rather than a map range so
// output is deterministic without sorting at every print site.
var allTiers = []tier{tier0, tier1, tier2}

// parseErrorLinterCst is the linter name on the synthetic finding emitted when
// a tier's JSON cannot be decoded. It mirrors parseGolangci in
// tools/quality-report/main.go, which tolerates a decode failure rather than
// dropping the tier.
//
// No baseline line may carry it, and that is enforced at load time rather than
// left to review. This is the lint-baseline analog of
// nlparity.DivergenceClass.Suppressible: if a parse error were suppressible,
// a config change that broke the JSON output would be masked by the entry
// written when it first broke, and the bug would arrive with its own cover
// story.
const parseErrorLinterCst = "internal/parse-error"

// keySepCst separates the tier from the key on a baseline line.
//
// A tab, not the " | " that appears inside the key itself. Keys embed a path,
// and a path may legally contain "|"; the key is therefore never split back
// into fields, only compared whole. Splitting the LINE on its first tab is
// unambiguous because a key containing a tab is rejected at load time.
const keySepCst = "\t"

// Load errors. Typed sentinels rather than ad-hoc strings so a test can assert
// WHICH fault fired, following pkg/nlparity/nlparity_allowlist.go.
//
// Unlike nlparity's, these carry no "lint-baseline: " prefix. That package is a
// library whose errors are returned to a caller that prints them; this is a
// package main that prints them itself, at the five Fprintf sites in run and
// runMain. Prefixing here too produced "lint-baseline: lint-baseline: ..." on
// every sentinel path. The prefix stays at the print site because three error
// paths (reading findings, reading and writing the baseline) wrap an os error
// with no sentinel and would otherwise be unattributed — which is also the
// convention in tools/netlink-audit/main.go.
var (
	errBaselineMissing        = errors.New("baseline file does not exist")
	errBaselineBadLine        = errors.New("baseline line is not <tier>\\t<key>")
	errBaselineUnknownTier    = errors.New("baseline line names an unknown tier")
	errBaselineEmptyKey       = errors.New("baseline line has an empty key")
	errBaselineUnsorted       = errors.New("baseline is not sorted")
	errBaselineDuplicate      = errors.New("baseline has a duplicate line")
	errBaselineUnsuppressible = errors.New("baseline line carries the unsuppressible " + parseErrorLinterCst + " linter")
	errFlagBadPair            = errors.New("expected <tier>=<value>")
	errFlagUnknownTier        = errors.New("unknown tier")
	errFlagGatedUnmeasured    = errors.New("tier is gated but has no -findings input")
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// pairFlag collects a repeatable -findings/-exit flag as <tier>=<value>.
//
// Insertion order is retained so a report lists tiers in the order the caller
// supplied them when they are a subset of allTiers.
type pairFlag struct {
	order []tier
	vals  map[tier]string
}

func (p *pairFlag) String() string {
	if p == nil || len(p.order) == 0 {
		return ""
	}
	parts := make([]string, 0, len(p.order))
	for _, t := range p.order {
		parts = append(parts, string(t)+"="+p.vals[t])
	}
	return strings.Join(parts, ",")
}

// Set parses one <tier>=<value>. The value may itself contain "=", so the split
// is on the FIRST separator only: a findings path is a filename and nothing
// stops it carrying one.
func (p *pairFlag) Set(s string) error {
	name, value, found := strings.Cut(s, "=")
	if !found || name == "" || value == "" {
		return fmt.Errorf("%w, got %q", errFlagBadPair, s)
	}
	t := tier(name)
	if _, ok := tierChecks[t]; !ok {
		return fmt.Errorf("%w %q", errFlagUnknownTier, name)
	}
	if p.vals == nil {
		p.vals = make(map[tier]string)
	}
	if _, dup := p.vals[t]; !dup {
		p.order = append(p.order, t)
	}
	p.vals[t] = value
	return nil
}

// options is the parsed command line. A struct rather than eight locals so
// runMain stays a wiring function and the work is testable without flags.
type options struct {
	baselinePath string
	findings     *pairFlag
	exitCodes    *pairFlag
	gated        map[tier]bool
	root         string
	update       bool
}

// runMain wires flag parsing to run. Extracted so tests drive it with
// synthetic args and capture buffers without subprocessing, following
// tools/netlink-audit/main.go.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("lint-baseline", flag.ContinueOnError)
	fs.SetOutput(stderr)
	baselinePath := fs.String("baseline", "docs/lint-baseline.txt", "committed baseline file")
	findings := &pairFlag{}
	fs.Var(findings, "findings", "repeatable <tier>=<golangci json path>")
	exitCodes := &pairFlag{}
	fs.Var(exitCodes, "exit", "repeatable <tier>=<that tier's own golangci exit code>")
	gated := fs.String("gated", "", "comma-separated tiers whose added findings fail the build")
	root := fs.String("root", "", "repo root, used to relativize absolute paths in the JSON")
	update := fs.Bool("update", false, "rewrite the baseline from the supplied findings")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	gatedSet, err := parseGated(*gated)
	if err != nil {
		fmt.Fprintf(stderr, "lint-baseline: %v\n", err)
		return 2
	}
	return run(options{
		baselinePath: *baselinePath,
		findings:     findings,
		exitCodes:    exitCodes,
		gated:        gatedSet,
		root:         *root,
		update:       *update,
	}, stdout, stderr)
}

// parseGated turns the -gated list into a set, rejecting unknown tiers. An
// empty list is valid and means every tier is advisory, which is the honest
// state for a tier whose findings have not been emptied yet.
func parseGated(s string) (map[tier]bool, error) {
	out := make(map[tier]bool)
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, name := range strings.Split(s, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		t := tier(name)
		if _, ok := tierChecks[t]; !ok {
			return nil, fmt.Errorf("%w %q in -gated", errFlagUnknownTier, name)
		}
		out[t] = true
	}
	return out, nil
}

// run is the whole program minus flag parsing. Order of checks is the exit-code
// precedence and is deliberate: an untrustworthy tier run is rejected BEFORE the
// baseline is even read, because the one thing this tool must never do is
// certify a timeout as a clean tree.
func run(o options, stdout, stderr io.Writer) int {
	if rc, bad := refuseToRatchet(o.exitCodes, stderr); bad {
		return rc
	}

	measured, err := collectFindings(o)
	if err != nil {
		fmt.Fprintf(stderr, "lint-baseline: %v\n", err)
		return 2
	}

	if o.update {
		if err := writeBaseline(o.baselinePath, measured); err != nil {
			fmt.Fprintf(stderr, "lint-baseline: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "lint-baseline: wrote %s\n", o.baselinePath)
		return 0
	}

	if err := requireGatedMeasured(o.gated, measured); err != nil {
		fmt.Fprintf(stderr, "lint-baseline: %v\n", err)
		return 2
	}

	base, err := readBaseline(o.baselinePath)
	if err != nil {
		fmt.Fprintf(stderr, "lint-baseline: %v\n", err)
		return 2
	}
	return report(o, base, measured, stdout, stderr)
}

// requireGatedMeasured rejects a gated tier that was given no -findings input.
//
// Without it the tool fails OPEN in the one direction that matters most. An
// absent findings set is an EMPTY findings set, which makes every baseline line
// for that tier read as REMOVED — and removals are an improvement that never
// fail, so the tool would exit 0. The tier would be nominally gated and silently
// unchecked, which is precisely the condition this tool exists to make
// impossible.
//
// Note what this is NOT: it does not require a gated tier to have findings. Zero
// findings is the goal state and gating a clean tier is the whole point. It
// requires that the tier was MEASURED, which collectFindings records by creating
// the map entry even when the list is empty.
//
// The Nix call sites derive the gated list and the findings list from the same
// nix/lint-baseline-measure.nix passthru, so they cannot disagree. This is the
// backstop for a hand invocation, and it is checked only on the comparison path
// because -gated means nothing under -update.
//
// allTiers rather than a map range, so a multi-tier fault reports the same tier
// every time.
func requireGatedMeasured(gated map[tier]bool, measured map[tier][]string) error {
	for _, t := range allTiers {
		if !gated[t] {
			continue
		}
		if _, ok := measured[t]; !ok {
			return fmt.Errorf("%w: %s", errFlagGatedUnmeasured, t)
		}
	}
	return nil
}

// refuseToRatchet reports whether any tier's own exit code says the run was not
// a measurement.
//
// golangci-lint exits 0 clean and 1 with findings; anything above that is the
// tool failing. The case that matters is 4, which it returns on `run.timeout`
// AFTER printing "0 issues." — recorded at nix/quality-report/default.nix. A
// tool that accepted that would write an empty baseline and call the tree
// clean, which is the single worst thing it could do.
func refuseToRatchet(exitCodes *pairFlag, stderr io.Writer) (int, bool) {
	bad := false
	for _, t := range exitCodes.order {
		raw := exitCodes.vals[t]
		code, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(stderr, "lint-baseline: tier %s exit code %q is not a number\n", t, raw)
			return 2, true
		}
		if code > 1 {
			fmt.Fprintf(stderr,
				"lint-baseline: refusing to ratchet: %s (%s) exited %d, which is a tool failure "+
					"rather than a measurement (golangci-lint exits 4 on run.timeout after printing \"0 issues.\")\n",
				t, tierChecks[t], code)
			bad = true
		}
	}
	if bad {
		return 3, true
	}
	return 0, false
}

// collectFindings decodes every supplied tier's JSON into a sorted, deduplicated
// key set.
func collectFindings(o options) (map[tier][]string, error) {
	out := make(map[tier][]string, len(o.findings.order))
	for _, t := range o.findings.order {
		keys, err := parseFindingsFile(o.findings.vals[t], o.root)
		if err != nil {
			return nil, err
		}
		out[t] = keys
	}
	return out, nil
}

// golangciJSON is the subset of golangci-lint's JSON this tool reads.
//
// The shape is copied from golangciJSON in tools/quality-report/main.go rather
// than shared: each tool under tools/ is its own package main with no shared
// internal/ dependency, which is the house pattern (see shouldSkipFile in
// tools/netlink-audit/main.go, kept inline for the same reason). Fourteen lines
// of struct tags is the deliberate cost of that.
//
// Line and Column are decoded and then dropped on purpose — see normalizeKey.
type golangciJSON struct {
	Issues []struct {
		FromLinter string `json:"FromLinter"`
		Text       string `json:"Text"`
		Pos        struct {
			Filename string `json:"Filename"`
			Line     int    `json:"Line"`
			Column   int    `json:"Column"`
		} `json:"Pos"`
	} `json:"Issues"`
}

// parseFindingsFile reads one tier's JSON and returns its normalized keys.
//
// A read failure is an error: the check itself is broken and must not pass. A
// DECODE failure is not — it yields one synthetic parseErrorLinterCst finding,
// mirroring parseGolangci's tolerance. Because no baseline line may carry that
// linter, the synthetic finding can never be suppressed and a gated tier goes
// red, which is the correct outcome for output nobody can read.
func parseFindingsFile(path, root string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading findings %s: %w", path, err)
	}
	// golangci-lint can leak noise onto the stream ahead of the JSON object;
	// find the first '{', exactly as parseGolangci does.
	if i := bytes.IndexByte(raw, '{'); i > 0 {
		raw = raw[i:]
	}
	var decoded golangciJSON
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return []string{normalizeKey(path, fmt.Sprintf("could not parse JSON: %v", err), parseErrorLinterCst)}, nil
	}
	keys := make([]string, 0, len(decoded.Issues))
	for _, issue := range decoded.Issues {
		keys = append(keys, normalizeKey(relpath(issue.Pos.Filename, root), issue.Text, issue.FromLinter))
	}
	return dedupeSorted(keys), nil
}

// normalizeKey builds the one canonical spelling of a finding:
//
//	path | message (linter)
//
// Line and column are deliberately absent. docs/static-analysis.md's recipe
// drops them for a measured reason: otherwise every finding that an unrelated
// edit merely MOVED down the file reads as new, and the diff is noise. The cost
// is that two identical findings in one file collapse to one key, which is
// accepted — the pair is indistinguishable to a reviewer anyway.
func normalizeKey(path, message, linter string) string {
	return fmt.Sprintf("%s | %s (%s)", path, message, linter)
}

// relpath makes an absolute JSON path repo-relative so the baseline does not
// embed the nix store path it was measured in. A relative path is returned
// unchanged, which is the normal case: golangci-lint reports relative to its
// run directory.
func relpath(path, root string) string {
	if root == "" || !filepath.IsAbs(path) {
		return path
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// dedupeSorted sorts and collapses duplicates. Dropping line:col can make two
// distinct issues share a key, and a set is the right model: the question this
// tool answers is "is this finding present", not "how many times".
func dedupeSorted(keys []string) []string {
	slices.Sort(keys)
	return slices.Compact(keys)
}

// baseline is the committed list, indexed per tier.
type baseline struct {
	keys map[tier]map[string]bool
}

// has reports whether a tier's baseline contains a key.
func (b *baseline) has(t tier, key string) bool { return b.keys[t][key] }

// sortedKeys returns one tier's baseline keys in file order (which is sorted
// order, since loading enforces that).
func (b *baseline) sortedKeys(t tier) []string {
	out := make([]string, 0, len(b.keys[t]))
	for k := range b.keys[t] {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// readBaseline loads the committed file, treating a missing one as an error.
//
// Fails closed, following LoadAllowlist rather than readCoverageBaseline: a
// caller handed a partially-valid baseline would have to decide which half to
// trust, and a tool that treats "no baseline" as "nothing to compare" passes
// every build the moment someone deletes the file.
func readBaseline(path string) (*baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", errBaselineMissing, path)
		}
		return nil, fmt.Errorf("reading baseline %s: %w", path, err)
	}
	return loadBaseline(data)
}

// loadBaseline parses and validates the baseline text.
//
// Four conditions are errors rather than warnings, and each closes a way the
// file could stop meaning anything: an unparseable line, an unknown tier, an
// out-of-order line, and a duplicate. Sortedness is enforced so that the diff a
// reviewer reads in a PR is minimal and a line cannot be hidden by being moved;
// duplicates are an error rather than a precedence rule, because nothing in the
// file would say which copy applied.
func loadBaseline(data []byte) (*baseline, error) {
	b := &baseline{keys: make(map[tier]map[string]bool, len(allTiers))}
	for _, t := range allTiers {
		b.keys[t] = make(map[string]bool)
	}
	var prev string
	for lineNo, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		t, key, err := parseBaselineLine(line, lineNo+1)
		if err != nil {
			return nil, err
		}
		if prev != "" {
			if line == prev {
				return nil, fmt.Errorf("%w: line %d: %s", errBaselineDuplicate, lineNo+1, line)
			}
			if line < prev {
				return nil, fmt.Errorf("%w: line %d (%q) sorts before the line above it (%q); run -update",
					errBaselineUnsorted, lineNo+1, line, prev)
			}
		}
		prev = line
		b.keys[t][key] = true
	}
	return b, nil
}

// parseBaselineLine splits one significant line into its tier and key and
// rejects everything a later comparison could not survive.
func parseBaselineLine(line string, lineNo int) (tier, string, error) {
	name, key, found := strings.Cut(line, keySepCst)
	if !found {
		return "", "", fmt.Errorf("%w: line %d: %q", errBaselineBadLine, lineNo, line)
	}
	t := tier(name)
	if _, ok := tierChecks[t]; !ok {
		return "", "", fmt.Errorf("%w: line %d: %q", errBaselineUnknownTier, lineNo, name)
	}
	if key == "" {
		return "", "", fmt.Errorf("%w: line %d", errBaselineEmptyKey, lineNo)
	}
	// A key with a tab in it would re-split differently than it was written,
	// so the one assumption keySepCst rests on is checked rather than assumed.
	if strings.Contains(key, keySepCst) {
		return "", "", fmt.Errorf("%w: line %d: key contains a tab: %q", errBaselineBadLine, lineNo, key)
	}
	if strings.HasSuffix(key, "("+parseErrorLinterCst+")") {
		return "", "", fmt.Errorf("%w: line %d: %q", errBaselineUnsuppressible, lineNo, key)
	}
	return t, key, nil
}

// baselineHeaderCst is written above the generated list. It states the standing
// decision the file encodes, because the file is the artifact a PR diff shows a
// reviewer and it has to explain itself there.
const baselineHeaderCst = `# docs/lint-baseline.txt - the committed golangci-lint finding baseline.
#
# Generated by tools/lint-baseline -update. Reviewed like docs/coverage-baseline.txt:
# it is a standing decision, not a scratch file, and it only ever goes DOWN.
#
# One line per finding, as "<tier>\t<path> | <message> (<linter>)". Line and
# column are deliberately absent, so a finding that an unrelated edit merely
# moved down a file does not read as new.
#
# A tier listed in -gated fails the build when it gains a line. Adding a line to
# a gated tier is therefore a decision to accept a new finding, and the diff is
# where that decision gets reviewed. Deleting a line never fails anything -
# that is a fix landing.
#
# Lines are sorted and unique; both are enforced at load time, so re-run -update
# rather than hand-editing anything but a deletion.
#
`

// writeBaseline rewrites the file from measured findings. The only mutating
// path in the tool, and it is behind -update so a check run can never move the
// bar it is measuring against.
func writeBaseline(path string, measured map[tier][]string) error {
	var buf bytes.Buffer
	buf.WriteString(baselineHeaderCst)
	var lines []string
	for _, t := range allTiers {
		for _, key := range measured[t] {
			lines = append(lines, string(t)+keySepCst+key)
		}
	}
	slices.Sort(lines)
	for _, line := range lines {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("writing baseline %s: %w", path, err)
	}
	return nil
}

// tierDiff is one tier's both-direction comparison.
type tierDiff struct {
	tier    tier
	gated   bool
	added   []string
	removed []string
}

// diffTier compares one tier's measured keys against the baseline in both
// directions.
//
// Both directions are computed even though only `added` can fail, because one
// direction cannot tell "unchanged" from "one finding swapped for another" —
// the case that makes a count useless and the reason this tool exists.
func diffTier(t tier, gated bool, measured []string, base *baseline) tierDiff {
	d := tierDiff{tier: t, gated: gated}
	present := make(map[string]bool, len(measured))
	for _, key := range measured {
		present[key] = true
		if !base.has(t, key) {
			d.added = append(d.added, key)
		}
	}
	for _, key := range base.sortedKeys(t) {
		if !present[key] {
			d.removed = append(d.removed, key)
		}
	}
	return d
}

// report prints every tier's diff and returns the exit code.
func report(o options, base *baseline, measured map[tier][]string, stdout, stderr io.Writer) int {
	failed := false
	for _, t := range o.findings.order {
		d := diffTier(t, o.gated[t], measured[t], base)
		printTierDiff(d, len(measured[t]), stdout)
		if d.gated && len(d.added) > 0 {
			failed = true
		}
	}
	if failed {
		fmt.Fprintln(stderr, "lint-baseline: a gated tier gained findings; fix them or, if they are "+
			"genuinely accepted, add them to the baseline in a reviewed commit")
		return 1
	}
	fmt.Fprintln(stdout, "lint-baseline: no added findings in any gated tier")
	return 0
}

// printTierDiff writes one tier's result. An advisory tier prints exactly the
// same body as a gated one and is labeled rather than silenced: the Tier 1 and
// Tier 2 deltas are the thing the next phase works from, so hiding them until
// they gate would lose the only progress signal there is.
func printTierDiff(d tierDiff, total int, stdout io.Writer) {
	label := "advisory"
	if d.gated {
		label = "GATED"
	}
	fmt.Fprintf(stdout, "\n%s (%s) [%s]: %d finding(s), %d added, %d removed\n",
		d.tier, tierChecks[d.tier], label, total, len(d.added), len(d.removed))
	for _, key := range d.added {
		fmt.Fprintf(stdout, "  + %s\n", key)
	}
	for _, key := range d.removed {
		fmt.Fprintf(stdout, "  - %s\n", key)
	}
}
