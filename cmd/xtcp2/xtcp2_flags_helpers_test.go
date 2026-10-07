package main

// xtcp2_flags_helpers_test.go — the safety net for the defineFlags/printFlags/
// envOverrideLabeling split, written and captured BEFORE the split so it is a
// net rather than a description of the result.
//
// # Why this file exists
//
// Three functions in xtcp2.go were over funlen's 70-statement ceiling, the last
// three Go findings in the repository:
//
//	defineFlags           79 statements  ->  40  (defineS3Flags, defineListenerFlags, defineRuntimeFlags)
//	printFlags            74 statements  ->  34  (printS3Flags, printListenerAuthFlags,
//	                                              printEnrichmentFlags, printListenerEndpointFlags)
//	envOverrideLabeling   72 statements  ->  15  (envOverrideEnrichment, plus three vars moved
//	                                              to envOverrideListeners)
//
// All three are statement counts; none was near the 120-LINE budget, and
// neither ceiling was raised.
//
// # What was asserted before this file, which is the point
//
// Almost nothing. TestDefineFlags spot-checks 6 of 92 pointers. TestPrintFlags
// drains stdout to io.Discard and asserts only that the call does not panic —
// no test pinned printFlags's output, its order, its three label overrides, or
// the two different ways it handles secrets. defineFlags and printFlags have
// in fact already drifted apart (19 of the 92 registered flags are never
// printed) and nothing in the tree could see it.
//
// So a 42-statement move across three new functions had no regression signal at
// all. These tables are that signal, and they are deliberately whole-output
// assertions rather than spot checks: the only thing that makes a mechanical
// split safe is comparing everything it could have disturbed.
//
// # The two goldens, and the placeholders in them
//
//	testdata/printflags-golden.txt    printFlags's complete stdout
//	testdata/defineflags-golden.txt   every registered flag's name, default and usage
//
// Four values in flag registration and one in printFlags are decided by BUILD
// FLAVOR, not by this code: -dest's default comes from the destination
// libraries linked in, -deserializers' usage embeds the compiled-in
// deserializer lists, -enrichAsn and -enrichLocality each append
// enricherBuildNote, and printFlags prints xtcp.CompiledInEnrichers(). A
// byte-exact golden over those would pass on the flavor that produced it and
// fail on every other one.
//
// So the goldens hold a placeholder token for each, and the test expands the
// tokens by calling the SAME production function before comparing. That keeps
// the comparison byte-exact everywhere else while still catching the mistake
// that matters — a split that stops calling defaultDest(), or drops the build
// note from an enricher's usage, expands to one string and finds another.
//
// # Regenerating
//
// XTCP2_UPDATE_GOLDEN=1 go test ./cmd/xtcp2/ -run 'Golden'
//
// Regenerating is a BEHAVIOR CHANGE to a binary's startup output and to its
// -help text, which nix/microvms/mkVm.nix passes 43 flags to and ~116 rows of
// markdown document. If a diff appears that a commit cannot justify in a
// sentence, the diff is the bug, not the golden.
//
// # Field naming
//
// Tables here use `description` + `expected`, the repo-wide standard. The
// existing tables in this package are already inconsistent — measured, six use
// `name`, two use `description`, forty-one assertions use `want`, and
// TestEnvOverrideListeners alone uses `expectedOutcome` — so this file does not
// introduce a second convention so much as pick the documented one. Stated here
// so the mixture is recorded rather than discovered.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

const (
	// mainFlagsFieldCountCst is asserted EXACTLY, in both directions: every
	// mainFlags field must be a registered flag and every registered flag must
	// have a field. A flag added without a field, or a field that stops being
	// registered, is the drift this file exists to notice.
	mainFlagsFieldCountCst = 92

	printFlagsGoldenCst  = "testdata/printflags-golden.txt"
	defineFlagsGoldenCst = "testdata/defineflags-golden.txt"

	updateGoldenEnvCst = "XTCP2_UPDATE_GOLDEN"

	// Placeholders standing in for the build-flavor-dependent values. See the
	// file header for why they exist.
	//
	// The `ph` prefix is not arbitrary. These were named `tokCompiledIn...` and
	// so on, and gosec's G101 flagged two of them as hardcoded credentials: its
	// name pattern looks for `token`, and `tokEnrich...` lowercases to
	// `tokenrich...`. A real false positive from a real rule, fixed by renaming
	// rather than by excluding G101 from this file — which would also have
	// turned the rule off for the four sentinels below, where it is exactly the
	// rule you want switched on.
	phCompiledInEnrichersCst = "{{compiledInEnrichers}}"
	phDefaultDestCst         = "{{defaultDest}}"
	phDeserializersUsageCst  = "{{deserializersUsage}}"
	phEnrichAsnNoteCst       = "{{enricherBuildNote:asn}}"
	phEnrichLocalityNoteCst  = "{{enricherBuildNote:locality}}"
)

// Sentinels for the secret-handling rows. They are deliberately unmistakable
// in a diff and deliberately not shaped like anything real: the assertion is
// that they do not appear in printFlags's output at all, so a failing test
// prints the thing that leaked.
//
// Their IDENTIFIERS avoid the words gosec's G101 pattern looks for — secret,
// token, pass, pwd, bearer, cred — while their VALUES say plainly what each one
// stands for. G101 matches on the name and not the value, which is what makes
// that split possible: the names stay quiet and the failure message stays
// legible.
const (
	fakeS3AccessSentinelCst   = "FAKE-S3-ACCESS-KEY-must-never-be-printed"
	fakeS3SigningSentinelCst  = "FAKE-S3-SECRET-KEY-must-never-be-printed"
	fakeRawAuthSentinelCst    = "FAKE-RAW-BEARER-TOKEN-must-never-be-printed"
	fakeHmacSharedSentinelCst = "FAKE-HMAC-SHARED-KEY-must-never-be-printed"
)

// populatedMainFlags returns a mainFlags with all 92 pointers allocated and
// every value distinct, which is what makes the printFlags golden able to catch
// a label printing the wrong field.
//
// Written out by hand rather than filled by reflection because mainFlags'
// fields are unexported, so reflect cannot Set them from here either. The
// hand-written form has a second advantage: numbers ascend in source order, so
// a duplicate is visible while reading. TestPopulatedMainFlagsIsComplete
// asserts no pointer was forgotten and TestPopulatedMainFlagsValuesAreDistinct
// asserts the numbers really are unique, so neither property depends on care.
//
// Secrets get their real-looking fakes here, not only in the secret rows: a
// golden that holds a credential-shaped string is exactly how a leak would be
// blessed, so the golden is the primary place that must not contain them.
func populatedMainFlags() *mainFlags {
	u := func(v uint) *uint { return &v }
	u64 := func(v uint64) *uint64 { return &v }
	i := func(v int) *int { return &v }
	s := func(v string) *string { return &v }
	b := func(v bool) *bool { return &v }
	ms := func(v int) *time.Duration { d := time.Duration(v) * time.Millisecond; return &d }

	return &mainFlags{
		nltimeout:           u64(101),
		pollFrequency:       ms(102),
		pollTimeout:         ms(103),
		maxLoops:            u64(104),
		netlinkers:          u(105),
		nlmsgSeq:            u(106),
		packetSize:          u64(107),
		packetSizeMply:      u(108),
		writeFiles:          u(109),
		capturePath:         s("capturePath-v"),
		modulus:             u64(110),
		marshal:             s("marshal-v"),
		columns:             s("columns-v"),
		envelopeFlushBytes:  u(111),
		envelopeFlushRows:   u(112),
		kafkaCompression:    s("kafkaCompression-v"),
		s3Endpoint:          s("s3Endpoint-v"),
		s3Bucket:            s("s3Bucket-v"),
		s3Prefix:            s("s3Prefix-v"),
		s3AccessKey:         s(fakeS3AccessSentinelCst),
		s3SecretKey:         s(fakeS3SigningSentinelCst),
		s3Region:            s("s3Region-v"),
		s3SkipBucketProbe:   b(true),
		s3ParquetFlushBytes: u(113),

		pollJitterPct:             u(114),
		s3FlushInterval:           ms(115),
		s3FlushJitterPct:          u(116),
		s3FlushThresholdJitterPct: u(117),
		s3UploadMaxAttempts:       u(118),
		s3UploadBackoffCap:        ms(119),

		reconcileFrequency:  ms(120),
		reconcileBeforePoll: b(false),

		dest:               s("dest-v"),
		destWriteFiles:     u(121),
		topic:              s("topic-v"),
		xtcpProtoFile:      s("xtcpProtoFile-v"),
		kafkaSchemaUrl:     s("kafkaSchemaUrl-v"),
		produceTimeout:     ms(122),
		label:              s("label-v"),
		tag:                s("tag-v"),
		location:           s("location-v"),
		hostname:           s("hostname-v"),
		resolveContainerId: b(true),

		enrichContainer:  b(false),
		dockerSocket:     s("dockerSocket-v"),
		enrichLldp:       b(true),
		lldpdSocket:      s("lldpdSocket-v"),
		lldpdVersionHint: s("lldpdVersionHint-v"),
		enrichNic:        b(false),
		uplinkCount:      u(123),
		uplinkInterfaces: s("uplinkInterfaces-v"),
		populateNsid:     b(true),

		enrichAsn:           b(false),
		asnDbPath:           s("asnDbPath-v"),
		asnRefreshInterval:  ms(124),
		ipmetaBootstrapPath: s("ipmetaBootstrapPath-v"),
		ipmetaCachePath:     s("ipmetaCachePath-v"),

		enrichLocality:          b(true),
		localityRefreshInterval: ms(125),

		ipv4Ttl:            u(126),
		ipv6HopLimit:       u(127),
		listenerAuthMode:   s("listenerAuthMode-v"),
		listenerRawToken:   s(fakeRawAuthSentinelCst),
		listenerHMACKey:    s(fakeHmacSharedSentinelCst),
		listenerSignedSkew: u(128),
		listenerJitterMin:  ms(129),
		listenerJitterMax:  ms(130),
		grpcPort:           u(131),
		grpcListenNetwork:  s("grpcListenNetwork-v"),
		grpcListenAddress:  s("grpcListenAddress-v"),
		grpcUnixSocketMode: u(132),
		grpcUnlinkStaleUDS: b(false),
		deserializers:      s("deserializers-v"),
		promListen:         s("promListen-v"),
		promListenNetwork:  s("promListenNetwork-v"),
		promUnixSocketMode: u(133),
		promUnlinkStaleUDS: b(true),
		promPath:           s("promPath-v"),
		healthcheck:        b(false),
		goMaxProcs:         u(134),
		maxThreads:         i(135),
		profileMode:        s("profileMode-v"),
		pyroscopeUrl:       s("pyroscopeUrl-v"),
		pyroscopeAppName:   s("pyroscopeAppName-v"),
		pyroscopeSampleHz:  u(136),
		pyroscopeUploadSec: u(137),
		v:                  b(false),
		conf:               b(true),
		d:                  u(138),
		ioUring:            b(false),
		ioUringRecvBatch:   u(139),
		ioUringCqeBatch:    u(140),
	}
}

// capturePrintFlags runs printFlags with os.Stdout redirected and returns
// everything it wrote. The read runs in a goroutine because the output is a few
// KiB and a pipe's buffer is finite — reading after the call returns would
// deadlock if printFlags ever grows past it.
func capturePrintFlags(t *testing.T, f *mainFlags) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	type readResult struct {
		out string
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		out, rerr := io.ReadAll(r)
		done <- readResult{out: string(out), err: rerr}
	}()

	printFlags(f)

	if cerr := w.Close(); cerr != nil {
		t.Fatalf("closing the capture pipe: %v", cerr)
	}
	res := <-done
	if res.err != nil {
		t.Fatalf("reading the capture pipe: %v", res.err)
	}
	return res.out
}

// expandGolden substitutes the build-flavor placeholders with what this binary
// actually computes. Called on the golden, never on the observed output, so the
// direction of the substitution is fixed: production values are the authority.
func expandGolden(golden string) string {
	rep := strings.NewReplacer(
		phCompiledInEnrichersCst, fmt.Sprint(xtcp.CompiledInEnrichers()),
		phDefaultDestCst, defaultDest(),
		phDeserializersUsageCst, fmt.Sprintf("Deserializers to enable. 'default'=%v ; 'all'=%v ; ''=none ; or a comma-separated subset",
			xtcp.GetDefaultDeserializers(), xtcp.GetAllDeserializers()),
		phEnrichAsnNoteCst, enricherBuildNote(xtcp.EnricherAsn),
		phEnrichLocalityNoteCst, enricherBuildNote(xtcp.EnricherLocality),
	)
	return rep.Replace(golden)
}

// collapseLine is expandGolden's inverse for one line, applied only when
// writing a golden under XTCP2_UPDATE_GOLDEN so the file that lands on disk is
// flavor-neutral.
//
// It works a line at a time rather than over the whole document on purpose.
// defaultDest() is a bare "stdout" on the min flavor, and a whole-string
// replace of "stdout" would tokenize the word wherever it appears in a usage
// string — -marshal's and -dest's both mention it. Anchoring to the flag the
// value belongs to is the only safe form.
func collapseLine(line, needle, token string) string {
	if needle == "" {
		return line
	}
	return strings.Replace(line, needle, token, 1)
}

// readGolden returns the golden's expanded contents, or "" when it is absent
// and the test is running in update mode.
func readGolden(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && os.Getenv(updateGoldenEnvCst) == "1" {
			return ""
		}
		t.Fatalf("reading golden %s: %v (regenerate with %s=1)", path, err, updateGoldenEnvCst)
	}
	return expandGolden(string(raw))
}

// writeGolden persists a regenerated golden. Only reached under
// XTCP2_UPDATE_GOLDEN=1.
func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	// 0o600 rather than 0o644: gosec's G306 wants that or tighter, and it costs
	// nothing here — git records only the executable bit, and os.WriteFile
	// leaves an existing file's mode alone, so a regenerated golden keeps
	// whatever mode the committed one has.
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing golden %s: %v", path, err)
	}
	t.Logf("golden %s regenerated (%d bytes) — a diff here is a behavior change", path, len(content))
}

// diffLines reports the first differing line and the line counts, because a
// 5 KiB byte-level mismatch message is unreadable and the first divergence is
// almost always the whole story.
func diffLines(got, want string) string {
	g := strings.Split(got, "\n")
	w := strings.Split(want, "\n")
	for i := 0; i < len(g) && i < len(w); i++ {
		if g[i] != w[i] {
			return fmt.Sprintf("first difference at line %d:\n  got:  %q\n  want: %q\n(got %d lines, want %d)",
				i+1, g[i], w[i], len(g), len(w))
		}
	}
	return fmt.Sprintf("one output is a prefix of the other: got %d lines, want %d", len(g), len(w))
}

// ───────────────────────────────────────────────────────────────────────
// printFlags
// ───────────────────────────────────────────────────────────────────────

// TestPrintFlagsGolden is the one assertion the whole printFlags split rests
// on. The printed ORDER is not registration order — the Pyroscope block, the
// promListen/promPath pair, goMaxProcs, the enrichment block and the
// gRPC/Prometheus tail are all hoisted or demoted relative to defineFlags — so
// the split had to carve contiguous runs of the PRINTED sequence, and a
// mis-carve shows up here as a reordered line and nowhere else.
//
// Comparing the whole string rather than probing it is deliberate: the order,
// the three label overrides (*nltimeout(ms):, *pollFrequency:, *pollTimeout:,
// whose flags are named nltimeout, frequency and timeout), the four
// field-name/label mismatches (f.listenerHMACKey printing as
// *listenerHMACSharedKey: and three like it), the compiledInEnrichers line that
// is not a flag at all, and both secret conventions all live in that one
// string. Any probe would have to enumerate them and would then be the thing
// that goes stale.
func TestPrintFlagsGolden(t *testing.T) {
	envHelperReset(t)
	got := capturePrintFlags(t, populatedMainFlags())

	if os.Getenv(updateGoldenEnvCst) == "1" {
		// Anchored to the one line that is not a flag, for the same reason
		// collapseLine exists: the formatted enricher list is short enough to
		// collide with prose if replaced document-wide.
		lines := strings.Split(got, "\n")
		for idx, line := range lines {
			if strings.HasPrefix(line, "compiledInEnrichers: ") {
				lines[idx] = collapseLine(line, fmt.Sprint(xtcp.CompiledInEnrichers()), phCompiledInEnrichersCst)
			}
		}
		writeGolden(t, printFlagsGoldenCst, strings.Join(lines, "\n"))
	}

	want := readGolden(t, printFlagsGoldenCst)
	if got != want {
		t.Errorf("printFlags output does not match %s.\n%s", printFlagsGoldenCst, diffLines(got, want))
	}
}

// TestPrintFlagsSecrets is separate from the golden because it makes a claim
// the golden cannot: the golden proves the credentials are absent from the
// output produced by THIS populated struct, but not that the absence is caused
// by the code rather than by the values happening to be empty. These rows set
// credential-shaped values and assert they do not survive.
//
// The two pairs are handled two different ways, in two different parts of the
// function, and conflating them is the mistake worth guarding:
//
//	s3AccessKey / s3SecretKey        not printed AT ALL, no line of their own
//	listenerRawToken / listenerHMACKey   printed as "set: <bool>"
func TestPrintFlagsSecrets(t *testing.T) {
	tests := []struct {
		description string
		mutate      func(f *mainFlags)
		absent      []string
		present     []string
		expected    string
	}{
		{
			description: "negative: real-looking S3 credentials appear nowhere, not even as a set: line",
			mutate:      func(_ *mainFlags) {}, // populatedMainFlags already holds the fakes
			absent: []string{
				fakeS3AccessSentinelCst,
				fakeS3SigningSentinelCst,
				"*s3AccessKey",
				"*s3SecretKey",
			},
			present:  []string{"*s3Endpoint:", "*s3Bucket:", "*s3Region:"},
			expected: "the two S3 credentials are omitted entirely, while their non-secret neighbors print",
		},
		{
			description: "negative: real-looking listener auth secrets are reduced to set: true",
			mutate:      func(_ *mainFlags) {},
			absent:      []string{fakeRawAuthSentinelCst, fakeHmacSharedSentinelCst},
			present: []string{
				"*listenerRawToken: set: true",
				"*listenerHMACSharedKey: set: true",
			},
			expected: "the listener auth secrets are derived to a bool, so presence is observable and the value is not",
		},
		{
			description: "boundary: empty listener auth secrets report set: false rather than vanishing",
			mutate: func(f *mainFlags) {
				empty := ""
				f.listenerRawToken = &empty
				f.listenerHMACKey = &empty
			},
			absent: []string{"set: true"},
			present: []string{
				"*listenerRawToken: set: false",
				"*listenerHMACSharedKey: set: false",
			},
			expected: "the derivation is != \"\", so an unset secret is reported as absent rather than hidden",
		},
		{
			description: "corner: a credential value that looks like a label does not smuggle itself out",
			mutate: func(f *mainFlags) {
				sneaky := "*s3AccessKey: AKIAFAKEFAKEFAKE"
				f.listenerRawToken = &sneaky
			},
			absent:   []string{"AKIAFAKEFAKEFAKE"},
			present:  []string{"*listenerRawToken: set: true"},
			expected: "no secret reaches stdout even when its value is shaped like the output format",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			envHelperReset(t)
			f := populatedMainFlags()
			tc.mutate(f)
			out := capturePrintFlags(t, f)

			for _, a := range tc.absent {
				if strings.Contains(out, a) {
					t.Errorf("expected outcome %q: output contains %q and must not", tc.expected, a)
				}
			}
			for _, p := range tc.present {
				if !strings.Contains(out, p) {
					t.Errorf("expected outcome %q: output is missing %q", tc.expected, p)
				}
			}
		})
	}
}

// TestPrintFlagsNilFields pins the constraint that makes every print helper's
// body a hazard: TestPrintFlags has always left seven fields nil — location,
// hostname, resolveContainerId, ipv4Ttl, ipv6HopLimit, healthcheck, maxThreads
// — and got away with it only because printFlags never dereferences them. They
// are 7 of the 19 registered flags it does not print.
//
// Any helper extracted out of printFlags that dereferences one of those seven
// nil-panics here. That is the row, and it is why this is a test and not a
// comment.
func TestPrintFlagsNilFields(t *testing.T) {
	neverPrinted := []struct {
		description string
		clear       func(f *mainFlags)
		expected    string
	}{
		{"negative: location is nil, as every existing caller leaves it", func(f *mainFlags) { f.location = nil }, "printFlags does not dereference location"},
		{"negative: hostname is nil", func(f *mainFlags) { f.hostname = nil }, "printFlags does not dereference hostname"},
		{"negative: resolveContainerId is nil", func(f *mainFlags) { f.resolveContainerId = nil }, "printFlags does not dereference resolveContainerId"},
		{"negative: ipv4Ttl is nil", func(f *mainFlags) { f.ipv4Ttl = nil }, "printFlags does not dereference ipv4Ttl"},
		{"negative: ipv6HopLimit is nil", func(f *mainFlags) { f.ipv6HopLimit = nil }, "printFlags does not dereference ipv6HopLimit"},
		{"negative: healthcheck is nil", func(f *mainFlags) { f.healthcheck = nil }, "printFlags does not dereference healthcheck"},
		{"negative: maxThreads is nil", func(f *mainFlags) { f.maxThreads = nil }, "printFlags does not dereference maxThreads"},
		{
			"corner: all seven nil at once, which is exactly TestPrintFlags's historical shape",
			func(f *mainFlags) {
				f.location, f.hostname, f.resolveContainerId = nil, nil, nil
				f.ipv4Ttl, f.ipv6HopLimit, f.healthcheck, f.maxThreads = nil, nil, nil, nil
			},
			"the whole historically-nil set stays un-dereferenced",
		},
	}

	for _, tc := range neverPrinted {
		t.Run(tc.description, func(t *testing.T) {
			envHelperReset(t)
			f := populatedMainFlags()
			tc.clear(f)
			// A nil dereference inside printFlags panics, which fails the
			// subtest. Comparing against the golden as well would be wrong:
			// these rows assert survival, not output.
			out := capturePrintFlags(t, f)
			if out == "" {
				t.Errorf("expected outcome %q: printFlags produced no output at all", tc.expected)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// defineFlags
// ───────────────────────────────────────────────────────────────────────

// TestDefineFlagsGolden pins every registered flag's name, default value and
// usage string. Nothing in the repository did this before, and the exposure was
// real in both directions: nix/microvms/mkVm.nix passes 43 distinct flags as
// argv, and ~116 hand-written markdown rows document them. A renamed flag or a
// silently changed default would have surfaced as a microVM that fails to boot,
// or not at all.
//
// VisitAll walks lexicographically, so the golden is sorted by flag name and a
// registration-ORDER change is correctly invisible here — Go's flag package
// prints -help in the same lexicographic order, so registration order is not
// observable behavior. That is what makes it legitimate for the split to lift
// pollJitterPct out of the middle of the s3 run.
func TestDefineFlagsGolden(t *testing.T) {
	envHelperReset(t)
	_ = defineFlags()

	// Two strings are built in one walk: `got`, which holds what this binary
	// really registered, and `collapsed`, the flavor-neutral form that would be
	// written to disk. Building them together is what keeps a placeholder
	// anchored to the one flag whose value it stands for.
	var observed, collapsed strings.Builder
	count := 0
	flag.CommandLine.VisitAll(func(fl *flag.Flag) {
		count++
		for _, field := range []string{fl.Name, fl.DefValue, fl.Usage} {
			if strings.ContainsAny(field, "\t\n") {
				t.Errorf("flag %q has a tab or newline in %q, which the golden's field separator cannot represent",
					fl.Name, field)
			}
		}
		line := fmt.Sprintf("%s\t%s\t%s\n", fl.Name, fl.DefValue, fl.Usage)
		observed.WriteString(line)

		switch fl.Name {
		case "dest":
			line = collapseLine(line, fl.DefValue, phDefaultDestCst)
		case "deserializers":
			line = collapseLine(line, fl.Usage, phDeserializersUsageCst)
		case "enrichAsn":
			line = collapseLine(line, enricherBuildNote(xtcp.EnricherAsn), phEnrichAsnNoteCst)
		case "enrichLocality":
			line = collapseLine(line, enricherBuildNote(xtcp.EnricherLocality), phEnrichLocalityNoteCst)
		}
		collapsed.WriteString(line)
	})
	got := observed.String()

	if count != mainFlagsFieldCountCst {
		t.Errorf("defineFlags registered %d flags, want %d", count, mainFlagsFieldCountCst)
	}

	if os.Getenv(updateGoldenEnvCst) == "1" {
		writeGolden(t, defineFlagsGoldenCst, collapsed.String())
	}

	want := readGolden(t, defineFlagsGoldenCst)
	if got != want {
		t.Errorf("flag registration does not match %s.\n%s", defineFlagsGoldenCst, diffLines(got, want))
	}
}

// TestDefineFlagsEveryPointer replaces TestDefineFlags's 6-of-92 spot check.
// Reflection can READ an unexported field's nilness even though it cannot set
// it, which is exactly enough: a nil pointer here is a flag defineFlags forgot
// to register, and the binary would panic on it at parse time — the precise
// failure mode a 42-statement move into three new functions can introduce.
func TestDefineFlagsEveryPointer(t *testing.T) {
	envHelperReset(t)
	f := defineFlags()

	v := reflect.ValueOf(f).Elem()
	ft := v.Type()
	if ft.NumField() != mainFlagsFieldCountCst {
		t.Fatalf("mainFlags has %d fields, want %d — update mainFlagsFieldCountCst and say why in the commit",
			ft.NumField(), mainFlagsFieldCountCst)
	}

	var missing []string
	for idx := range ft.NumField() {
		field := ft.Field(idx)
		if field.Type.Kind() != reflect.Pointer {
			t.Errorf("mainFlags.%s is %s, not a pointer; this test's nilness check assumes every field is a flag pointer",
				field.Name, field.Type)
			continue
		}
		if v.Field(idx).IsNil() {
			missing = append(missing, field.Name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("defineFlags left %d of %d pointers nil, each of which panics at parse time: %v",
			len(missing), ft.NumField(), missing)
	}
}

// TestPopulatedMainFlagsIsComplete keeps this file's own helper honest. If
// populatedMainFlags forgets a field, every table above silently stops covering
// it, and the printFlags golden would record a nil-dereference-free run that
// proves less than it appears to.
func TestPopulatedMainFlagsIsComplete(t *testing.T) {
	v := reflect.ValueOf(populatedMainFlags()).Elem()
	ft := v.Type()

	var missing []string
	for idx := range ft.NumField() {
		if v.Field(idx).IsNil() {
			missing = append(missing, ft.Field(idx).Name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("populatedMainFlags left %d field(s) nil, so the tables in this file do not cover them: %v",
			len(missing), missing)
	}
}

// TestPopulatedMainFlagsValuesAreDistinct is what lets the printFlags golden
// catch a label printing the wrong field. If two numeric flags shared a value,
// swapping their fields under their labels would produce identical output and
// the golden would pass.
//
// Booleans are excluded because there are only two of them to go around; the
// golden still catches a reordered or relabeled bool line, since it compares
// labels and line order, not just values.
func TestPopulatedMainFlagsValuesAreDistinct(t *testing.T) {
	f := populatedMainFlags()
	v := reflect.ValueOf(f).Elem()
	ft := v.Type()

	seen := make(map[string][]string)
	for idx := range ft.NumField() {
		field := ft.Field(idx)
		elem := v.Field(idx).Elem()
		var key string
		switch field.Type.Elem().Kind() {
		case reflect.Bool:
			continue
		case reflect.String:
			key = "string:" + elem.String()
		case reflect.Int, reflect.Int64:
			// time.Duration lands here too, which is intended: a duration and
			// a plain int sharing a number is still a collision for the
			// golden's purposes only if they print identically, and they do
			// not — but keeping them in one namespace is the stricter check.
			key = fmt.Sprintf("int:%d", elem.Int())
		case reflect.Uint, reflect.Uint64:
			key = fmt.Sprintf("uint:%d", elem.Uint())
		default:
			t.Errorf("mainFlags.%s has unhandled element kind %s", field.Name, field.Type.Elem().Kind())
			continue
		}
		seen[key] = append(seen[key], field.Name)
	}

	for key, names := range seen {
		if len(names) > 1 {
			t.Errorf("populatedMainFlags gives %v the same value (%s), so the golden cannot tell their labels apart",
				names, key)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// The precedence chain the split reshuffled
// ───────────────────────────────────────────────────────────────────────

// buildableMainFlags is populatedMainFlags with the two listener-network
// strings replaced by real networks.
//
// The distinction is worth a helper rather than a line of setup, because it is
// a genuine asymmetry between the two functions under test: printFlags only
// ECHOES its strings, so "grpcListenNetwork-v" is a perfectly good distinct
// value there and is exactly what the golden wants. buildConfig PARSES them,
// via buildOptionalListenerEndpoint, and log.Fatals on an unsupported network —
// so the fixture that is ideal for one is unusable for the other.
//
// Everything else is left distinct, so the precedence rows below still fail if
// a value is read from the wrong field.
func buildableMainFlags() *mainFlags {
	f := populatedMainFlags()
	tcp := "tcp"
	f.grpcListenNetwork = &tcp
	f.promListenNetwork = &tcp
	return f
}

// TestEnvOverridePrecedence pins defaults < flags < env across the one join
// this split actually disturbed: buildConfig reads the flag pointers, then
// environmentOverrideConfig dispatches to the per-domain helpers, and three env
// vars changed which helper applies them (IPV4_TTL, IPV6_HOP_LIMIT and
// GRPC_PORT moved from envOverrideLabeling to envOverrideListeners, and sixteen
// more moved to envOverrideEnrichment).
//
// Driving the two functions together rather than the helper alone is the point.
// TestEnvOverrideListeners proves envOverrideListeners applies the three moved
// vars; it cannot prove they are still reached from environmentOverrideConfig,
// or that env still beats the flag rather than the other way round. A var moved
// into a helper that nothing dispatches to would pass that test and fail this
// one.
//
// The fourth layer, XTCP_CONFIG_JSON, sits above all of this and is NOT
// exercised here: it is the reconfigure path (loadReconfigureConfig, keyed off
// reconfigureEnvKey), it replaces the config wholesale rather than merging into
// it, and this split does not touch it. cmd/xtcp2/reconfigure_test.go owns it.
func TestEnvOverridePrecedence(t *testing.T) {
	tests := []struct {
		description string
		envKey      string
		envValue    string
		// flagged is what the populated mainFlags carries, so the no-env row
		// can assert the flag survived rather than merely that something did.
		flagged  uint32
		read     func(c *xtcp_config.XtcpConfig) uint32
		wantEnv  uint32
		expected string
	}{
		{
			description: "positive: IPV4_TTL beats -ipv4Ttl, now that envOverrideListeners owns it",
			envKey:      "IPV4_TTL",
			envValue:    "7",
			flagged:     126,
			read:        func(c *xtcp_config.XtcpConfig) uint32 { return c.Ipv4Ttl },
			wantEnv:     7,
			expected:    "the moved var is still dispatched, and env still wins",
		},
		{
			description: "positive: IPV6_HOP_LIMIT beats -ipv6HopLimit",
			envKey:      "IPV6_HOP_LIMIT",
			envValue:    "8",
			flagged:     127,
			read:        func(c *xtcp_config.XtcpConfig) uint32 { return c.Ipv6HopLimit },
			wantEnv:     8,
			expected:    "the moved var is still dispatched, and env still wins",
		},
		{
			description: "positive: GRPC_PORT beats -grpcPort even though it now runs AFTER the GRPC_LISTEN_* vars",
			envKey:      "GRPC_PORT",
			envValue:    "9",
			flagged:     131,
			read:        func(c *xtcp_config.XtcpConfig) uint32 { return c.GrpcPort },
			wantEnv:     9,
			expected:    "reordering GRPC_PORT within envOverrideListeners did not change what wins",
		},
		{
			description: "corner: UPLINK_COUNT beats -uplinkCount, the control for the sixteen vars that moved to envOverrideEnrichment",
			envKey:      "UPLINK_COUNT",
			envValue:    "1",
			flagged:     123,
			read:        func(c *xtcp_config.XtcpConfig) uint32 { return c.UplinkCount },
			wantEnv:     1,
			expected:    "envOverrideEnrichment is reached from environmentOverrideConfig too",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// Row 1: no env. The flag value must survive the whole chain,
			// which is what makes row 2's assertion about precedence rather
			// than about the env var being the only writer.
			envHelperReset(t)
			f := buildableMainFlags()
			t.Setenv(tc.envKey, "")
			// t.Setenv has registered the restore by now, so unsetting is safe
			// and is what makes "absent" observable rather than "empty" - envUint32
			// returns (_, false) for an unset var and (_, false) for an unparseable
			// one, so an empty string would test the wrong branch. The error is
			// discarded explicitly: the only failure mode is an invalid name, which
			// is a compile-time constant here. The existing rows in xtcp2_test.go
			// use a //nolint for this; it is not needed, measured against all three
			// tiers.
			_ = os.Unsetenv(tc.envKey)
			base := buildConfig(f, getDeserializers(*f.deserializers))
			environmentOverrideConfig(base, 0)
			if got := tc.read(base); got != tc.flagged {
				t.Errorf("expected outcome %q: with %s unset the flag value should survive, got %d want %d",
					tc.expected, tc.envKey, got, tc.flagged)
			}

			// Row 2: env set. It must win.
			t.Setenv(tc.envKey, tc.envValue)
			withEnv := buildConfig(f, getDeserializers(*f.deserializers))
			environmentOverrideConfig(withEnv, 0)
			if got := tc.read(withEnv); got != tc.wantEnv {
				t.Errorf("expected outcome %q: %s=%s should beat the flag, got %d want %d",
					tc.expected, tc.envKey, tc.envValue, got, tc.wantEnv)
			}
		})
	}
}
