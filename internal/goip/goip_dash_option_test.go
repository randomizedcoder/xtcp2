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
			description:  "positive: -brief is a known option",
			args:         []string{"-brief"},
			wantAccepted: true,
		},
		{
			description:  "positive: -oneline is a known option",
			args:         []string{"-oneline"},
			wantAccepted: true,
		},
		{
			// ip tests -batch (ip/ip.c:254) before -brief (:260), so `-b` is
			// batch and the shortest brief spelling is two letters.
			description:  "boundary: -br is the shortest brief, because -b is ip's -batch",
			args:         []string{"-br"},
			wantAccepted: true,
		},
		{
			description:  "negative: -b is ip's -batch, which goip lacks, so it is unknown and not brief",
			args:         []string{"-b"},
			wantAccepted: false,
		},
		{
			description:  "boundary: -o is the shortest oneline, since nothing ahead of it begins with -o",
			args:         []string{"-o"},
			wantAccepted: true,
		},
		{
			description:  "corner: --br strips one dash and is brief",
			args:         []string{"--br"},
			wantAccepted: true,
		},
		{
			description:  "corner: -bri is still brief",
			args:         []string{"-bri"},
			wantAccepted: true,
		},
		{
			description:  "negative: -brieff is longer than the option and is unknown",
			args:         []string{"-brieff"},
			wantAccepted: false,
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

// TestLinkShowBareDevice covers the final arm of ipaddr_list_flush_or_save
// (ip/ipaddress.c): `ip link show goip0` is `ip link show dev goip0`, unless
// the word is one of the loop's keywords, a flag filter or an abbreviation of
// help, none of which goip's link show implements.
//
// go test ./internal/goip/ -run TestLinkShowBareDevice
func TestLinkShowBareDevice(t *testing.T) {
	run := func(args ...string) (int, string) {
		t.Setenv("GOIP_REPLAY", guestDumpsDir+"netlink_route_getlink_dev.pcap")
		var stdout, stderr bytes.Buffer
		code := Run(args, &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	_, want := run("link", "show", "dev", "goip0")
	tests := []struct {
		name, category, description string
		args                        []string
		expected                    string // "dev" is the `dev goip0` output; otherwise a substring of the error
	}{
		{"bare_name", "positive", "a bare name is the device", []string{"link", "show", "goip0"}, "dev"},
		{"bare_name_json", "positive", "and under -j", []string{"-j", "link", "show", "goip0"}, ""},
		{"keyword_up", "negative", "`up` is a keyword, not a device", []string{"link", "show", "up"}, "not implemented"},
		{"flag_filter", "negative", "`dynamic` is a flag filter", []string{"link", "show", "dynamic"}, "not implemented"},
		{"help_abbrev", "corner", "`he` abbreviates help", []string{"link", "show", "he"}, "not implemented"},
		{"dev_alone", "boundary", "`dev` with no name is not a device named dev", []string{"link", "show", "dev"}, "Command line is not complete"},
		{"two_bare", "negative", "a second name is ip's duparg2", []string{"link", "show", "goip0", "goip0"}, `either "dev" is duplicate, or "goip0" is garbage`},
	}
	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			code, out := run(tc.args...)
			switch tc.expected {
			case "dev":
				if code != ExitOK || out != want {
					t.Errorf("%s: Run(%q) = %d %q, want the `dev goip0` output %q", tc.description, tc.args, code, out, want)
				}
			case "":
				if code != ExitOK {
					t.Errorf("%s: Run(%q) = %d %q, want ok", tc.description, tc.args, code, out)
				}
			default:
				if code == ExitOK || !strings.Contains(out, tc.expected) {
					t.Errorf("%s: Run(%q) = %d %q, want an error containing %q", tc.description, tc.args, code, out, tc.expected)
				}
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

// TestRunLoopsOption is `-loops`, the first entry in ip's option chain
// (ip/ip.c:200-205). It takes the next argument as its count whatever it
// is, and with none it dies on missarg, exit(-1). Being first, it owns every
// prefix of itself, a bare `-` included: `ip -` is -loops with no count, and
// so is `goip -`. The count's effect is TestAddrFlushLoops'.
//
// go test ./internal/goip/ -run TestRunLoopsOption
func TestRunLoopsOption(t *testing.T) {
	type result struct {
		code   int
		stderr string
	}
	const missing = `Error: argument "loop count" is required`
	tests := []struct {
		description string
		args        []string
		expected    result
	}{
		{description: "positive: -loops takes a count, then usage for no object",
			args: []string{"-loops", "3"}, expected: result{code: ExitUsage, stderr: "Usage:"}},
		{description: "corner: -l is -loops",
			args: []string{"-l", "3"}, expected: result{code: ExitUsage, stderr: "Usage:"}},
		{description: "corner: --loops strips one dash",
			args: []string{"--loops", "3"}, expected: result{code: ExitUsage, stderr: "Usage:"}},
		{description: "corner: the count is taken even when it looks like an option",
			args: []string{"-loops", "-4"}, expected: result{code: ExitUsage, stderr: "Usage:"}},
		{description: "negative: -loops with no count is missarg's exit 255",
			args: []string{"-loops"}, expected: result{code: ExitMinusOne, stderr: missing}},
		{description: "corner: a bare - is -loops with no count, as in ip",
			args: []string{"-"}, expected: result{code: ExitMinusOne, stderr: missing}},
		{description: "negative: -loopss is longer than the option and is unknown",
			args: []string{"-loopss", "3"}, expected: result{code: ExitUsage, stderr: "is unknown"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := result{code: Run(tc.args, &stdout, &stderr)}
			if strings.Contains(stderr.String(), tc.expected.stderr) {
				got.stderr = tc.expected.stderr
			} else {
				got.stderr = stderr.String()
			}
			if got != tc.expected {
				t.Errorf("Run(%q) = %+v, want %+v", tc.args, got, tc.expected)
			}
		})
	}
}
