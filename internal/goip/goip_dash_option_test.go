package goip

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunDashConventions covers the two things `ip` does to an option string
// before it looks at it, both of which goip did not do.
//
// # `--` ends the options
//
// ip/ip.c:192-195, and it is the FIRST test in the loop, before the `opt[0] !=
// '-'` break. It consumes the `--` and stops parsing, so `ip -- link show` is
// `link show`. goip reported it as an unknown option.
//
// # one leading dash comes off when the second character is also a dash
//
// ip/ip.c:198-199, `if (opt[1] == '-') opt++`. So `--json` is `-json` and
// every long option works with either one dash or two. goip rejected the
// two-dash spelling, and the reason is worth stating because it is not a
// missing case in a switch: matchesPrefix("--json", "-json") is false on
// LENGTH alone — the argument is longer than the pattern — so it fails before
// a single character is compared. No amount of adding patterns fixes it.
//
// Exactly one dash is removed. `---json` becomes `--json`, which matches
// nothing and stays an error; a strings.TrimLeft would have accepted it, and
// the corner row below is what says so.
//
// # What the rows assert, and what they cannot
//
// Every row passes options and NO object, so Run parses, fails the "no object"
// check and prints usage without opening a socket — the same shape as
// TestRunFamilyOption, for the same reason: a row must not be able to fail for
// a reason belonging to a dump. "Accepted" therefore means "was not reported
// as an unknown option", which is exactly the property under test.
//
// go test ./internal/goip/ -run TestRunDashConventions
func TestRunDashConventions(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		// wantAccepted is false for a row the parser must reject with the
		// unknown-option message.
		wantAccepted bool
	}{
		{
			description:  "positive: -json is accepted, the spelling that always worked",
			args:         []string{"-json"},
			wantAccepted: true,
		},
		{
			description:  "positive: --json is the same option, because ip/ip.c:198-199 strips one dash",
			args:         []string{"--json"},
			wantAccepted: true,
		},
		{
			description:  "positive: --j abbreviates after the strip, since what remains goes through matches()",
			args:         []string{"--j"},
			wantAccepted: true,
		},
		{
			description:  "positive: --stats, so the strip is not special-cased to one option",
			args:         []string{"--stats"},
			wantAccepted: true,
		},
		{
			// -4 is compared with strcmp, so the strip has to happen BEFORE
			// the comparison for this to work at all.
			description:  "positive: --4 reaches the strcmp family option after the strip",
			args:         []string{"--4"},
			wantAccepted: true,
		},
		{
			description:  "boundary: a bare -- is consumed and ends the options",
			args:         []string{"--"},
			wantAccepted: true,
		},
		{
			description:  "positive: -- after a real option keeps that option's effect",
			args:         []string{"-json", "--"},
			wantAccepted: true,
		},
		{
			// The corner a TrimLeft would get wrong: one dash off leaves
			// "--json", which is not an option.
			description:  "corner: ---json loses exactly one dash and is still unknown",
			args:         []string{"---json"},
			wantAccepted: false,
		},
		{
			description:  "negative: --bogus is unknown after the strip, not accepted by it",
			args:         []string{"--bogus"},
			wantAccepted: false,
		},
		{
			// A bare "-" is NOT an end-of-options marker in either tool, and
			// neither rejects it — but they accept it for different reasons,
			// which is a pre-existing divergence this row records rather than
			// fixes.
			//
			// `ip` falls through the strip (opt[1] is NUL, not '-') into the
			// matches() chain, where "-" is a prefix of the FIRST entry,
			// "-loops" (ip/ip.c:200), so `ip -` is `-loops` with no argument
			// and dies on missarg("loop count"). goip has no -loops, so "-"
			// reaches matchesPrefix(a, "-json") and sets json instead.
			//
			// Both exit non-zero and neither says "unknown"; only the
			// diagnostic differs, and matching it exactly would mean
			// implementing -loops purely to fail on it.
			description:  "corner: a bare - is accepted by both, but as a different option in each",
			args:         []string{"-"},
			wantAccepted: true,
		},
	}

	const unknown = "is unknown"

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != ExitUsage {
				t.Fatalf("Run(%q) = %d, want %d (no object, so usage)\nstderr: %s",
					tt.args, code, ExitUsage, stderr.String())
			}
			gotUnknown := strings.Contains(stderr.String(), unknown)
			if gotUnknown == tt.wantAccepted {
				t.Errorf("Run(%q): unknown-option message present = %v, want accepted = %v\nstderr: %s",
					tt.args, gotUnknown, tt.wantAccepted, stderr.String())
			}
		})
	}
}

// TestRunDashConventionsReachTheObject is the half the rows above cannot
// assert: that a stripped or terminated option list still produces the SAME
// LISTING, not merely the same acceptance.
//
// Every row in TestRunDashConventions stops at "no object, so usage", which
// proves the parser did not reject the spelling and nothing more. A `--` that
// was recognized but not CONSUMED would pass every one of those rows and then
// hand "--" to the object lookup as the object name. These rows replay a real
// capture and compare against the committed sidecar, so the whole path has to
// work.
//
// go test ./internal/goip/ -run TestRunDashConventionsReachTheObject
func TestRunDashConventionsReachTheObject(t *testing.T) {
	tests := []struct {
		description string
		args        []string
	}{
		{
			description: "positive: `-- link show` runs link show, so -- is consumed and not passed on",
			args:        []string{"--", "link", "show"},
		},
		{
			// An option, then the terminator, then the object. `link show`
			// overwrites preferred_family with AF_PACKET internally
			// (ip/ipaddress.c:2416) so -4 cannot change this output — which
			// is the point: the row isolates `--` being consumed in the
			// middle of a list rather than at its head.
			description: "boundary: `-4 -- link show` keeps the option and still ends the list",
			args:        []string{"-4", "--", "link", "show"},
		},
		{
			description: "boundary: the two-dash spelling composes with the terminator",
			args:        []string{"--4", "--", "link", "show"},
		},
	}

	// The reference: the plain form, over the same replay.
	want := runLinkShowCapture(t, []string{"link", "show"})

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := runLinkShowCapture(t, tt.args); got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}

	// `--json` and `-json` must agree with each other, which is the actual
	// claim for the long spelling.
	one := runLinkShowCapture(t, []string{"-json", "link", "show"})
	two := runLinkShowCapture(t, []string{"--json", "link", "show"})
	if one != two {
		t.Errorf("-json and --json differ:\n -json: %q\n--json: %q", one, two)
	}
	if one == want {
		t.Error("-json produced the text form; the row would pass for the wrong reason")
	}
}

// runLinkShowCapture replays the committed clean link dump through Run and
// returns stdout, failing the test on any non-OK exit.
func runLinkShowCapture(t *testing.T, args []string) string {
	t.Helper()

	t.Setenv("GOIP_REPLAY", guestDumpsDir+"netlink_route_getlink.pcap")
	var stdout, stderr bytes.Buffer
	if code := Run(args, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(%q) = %d, stderr=%s", args, code, stderr.String())
	}
	return stdout.String()
}

// TestLinkShowDevKeywordIsExact covers the one selector keyword in iproute2
// that does NOT abbreviate.
//
// ip/ipaddress.c:2241 compares it with strcmp, so "d" is not "dev": it fails
// the comparison, falls through to the else branch and becomes filter_dev
// itself, and the NEXT argument then trips duparg2("dev", …). `ip link show d
// lo` is therefore an error, and goip accepted it as `dev lo` until this test
// existed — strictly more permissive than the thing it mirrors, which is the
// one direction a coverage oracle must not drift in.
//
// go test ./internal/goip/ -run TestLinkShowDevKeywordIsExact
func TestLinkShowDevKeywordIsExact(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantOK      bool
	}{
		{
			description: "positive: the exact keyword selects the device",
			args:        []string{"link", "show", "dev", "goip0"},
			wantOK:      true,
		},
		{
			description: "negative: `d goip0` is not `dev goip0`, because the comparison is strcmp",
			args:        []string{"link", "show", "d", "goip0"},
			wantOK:      false,
		},
		{
			description: "negative: `de goip0` likewise",
			args:        []string{"link", "show", "de", "goip0"},
			wantOK:      false,
		},
		{
			description: "boundary: `deva goip0` is longer than the keyword and was never at risk",
			args:        []string{"link", "show", "deva", "goip0"},
			wantOK:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", guestDumpsDir+"netlink_route_getlink_dev.pcap")
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if gotOK := code == ExitOK; gotOK != tt.wantOK {
				t.Errorf("Run(%q) = %d (ok=%v), want ok=%v\nstdout: %q\nstderr: %s",
					tt.args, code, gotOK, tt.wantOK, stdout.String(), stderr.String())
			}
		})
	}
}

// TestRunDashStripInErrorMessage pins which spelling the diagnostic echoes.
//
// `ip` prints the STRIPPED pointer — the fprintf at ip/ip.c:294-296 passes
// `opt`, which `opt++` has already advanced — so `ip --bogus` complains about
// `"-bogus"` and not `"--bogus"`. goip gets this right by construction because
// it reassigns the loop variable rather than a copy, and a future refactor
// that kept the original around for the message would silently diverge.
//
// go test ./internal/goip/ -run TestRunDashStripInErrorMessage
func TestRunDashStripInErrorMessage(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		wantQuoted  string
	}{
		{
			description: "positive: a two-dash unknown option is echoed with one dash",
			arg:         "--bogus",
			wantQuoted:  `"-bogus"`,
		},
		{
			description: "positive: a one-dash unknown option is echoed unchanged",
			arg:         "-bogus",
			wantQuoted:  `"-bogus"`,
		},
		{
			description: "corner: three dashes lose one, so the message shows two",
			arg:         "---bogus",
			wantQuoted:  `"--bogus"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run([]string{tt.arg}, &stdout, &stderr); code != ExitUsage {
				t.Fatalf("Run(%q) = %d, want %d", tt.arg, code, ExitUsage)
			}
			if !strings.Contains(stderr.String(), tt.wantQuoted) {
				t.Errorf("stderr = %q, want it to name %s", stderr.String(), tt.wantQuoted)
			}
		})
	}
}
