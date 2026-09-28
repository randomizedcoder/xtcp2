package goip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
)

// TestRunStatsOption drives the `-s` half of Run's option loop.
//
// # Why every row stops before an object
//
// The rows pass options and no object, so Run parses the options, fails the
// "no object" check and prints usage — without opening a netlink socket or a
// replay source. That makes the table a test of the PARSER and nothing else:
// a row cannot pass or fail for reasons belonging to a dump.
//
// Acceptance is therefore asserted negatively, as the absence of `Option ...
// is unknown`, which is the only thing the parser says about an option it does
// not recognize. That is a real assertion and not a weak one: before `-s` was
// added, every row below except the unknown-option ones failed on it.
//
// go test ./internal/goip/ -run TestRunStatsOption
func TestRunStatsOption(t *testing.T) {
	// `ip` matches options with matches(), an unanchored prefix test, so every
	// non-empty prefix of `-stats` and of `-statistics` is the same option
	// (ip/ip.c:232-234). The rows walk that rule rather than asserting `-s`
	// alone, because a switch written with `a == "-s"` passes a one-row table
	// and diverges from `ip` on the very next argv the harness throws at it.
	tests := []struct {
		description string
		args        []string
		wantCode    int
		// wantStderr, when non-empty, must appear in stderr.
		wantStderr string
		// wantAccepted asserts the option was recognized at all.
		wantAccepted bool
	}{
		{
			description:  "positive: -s is accepted, the short form ip documents",
			args:         []string{"-s"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			description:  "positive: -st is accepted, because matches() is a prefix test and not an equality test",
			args:         []string{"-st"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			description:  "positive: -stats is accepted, the full first spelling",
			args:         []string{"-stats"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			description:  "positive: -statistics is accepted, the full second spelling",
			args:         []string{"-statistics"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			// A prefix of the SECOND spelling only: `-statistic` is not a
			// prefix of `-stats`, so a parser that checked one spelling and
			// not the other rejects this and `ip` does not.
			description:  "boundary: -statistic matches the second spelling only, and must still be accepted",
			args:         []string{"-statistic"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			description:  "positive: -s composes with other options in either order",
			args:         []string{"-s", "-6"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			description:  "positive: and in the other order, since the loop is order-independent",
			args:         []string{"-j", "-s"},
			wantCode:     ExitUsage,
			wantAccepted: true,
		},
		{
			// The decision this feature had to make, asserted rather than
			// left to discovery. `ip -s -s` is a real command with a wider
			// render; goip implements show_stats == 1 and says so.
			description:  "negative: -s -s is refused by name rather than silently rendering the narrow form",
			args:         []string{"-s", "-s"},
			wantCode:     ExitUsage,
			wantStderr:   "not implemented",
			wantAccepted: true,
		},
		{
			description:  "negative: three -s are refused too, and the count is reported",
			args:         []string{"-s", "-s", "-s"},
			wantCode:     ExitUsage,
			wantStderr:   "show_stats = 3",
			wantAccepted: true,
		},
		{
			// The mixed-spelling form, which a count-by-flag-string
			// implementation would get wrong.
			description:  "corner: -s and -stats together are two increments, not one option seen twice",
			args:         []string{"-s", "-stats"},
			wantCode:     ExitUsage,
			wantStderr:   "show_stats = 2",
			wantAccepted: true,
		},
		{
			// The control. Without it, "no unknown-option message" would be
			// satisfied by a parser that never printed one.
			description:  "negative: an option that really is unknown still says so",
			args:         []string{"-sz"},
			wantCode:     ExitUsage,
			wantAccepted: false,
		},
		{
			description:  "negative: the empty-ish -- is not a stats option either",
			args:         []string{"-x"},
			wantCode:     ExitUsage,
			wantAccepted: false,
		},
	}

	const unknown = "is unknown"

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(tt.args, &stdout, &stderr)
			if code != tt.wantCode {
				t.Errorf("Run(%q) = %d, want %d\nstderr: %s",
					tt.args, code, tt.wantCode, stderr.String())
			}
			gotUnknown := strings.Contains(stderr.String(), unknown)
			if gotUnknown == tt.wantAccepted {
				t.Errorf("Run(%q): unknown-option message present = %v, want accepted = %v\nstderr: %s",
					tt.args, gotUnknown, tt.wantAccepted, stderr.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Errorf("Run(%q) stderr lacks %q\nstderr: %s",
					tt.args, tt.wantStderr, stderr.String())
			}
		})
	}
}

// TestRunStatsOptionInUsage keeps the usage text honest about an option that
// now exists. A flag the parser accepts and the help does not mention is the
// kind of drift nothing else here would catch.
//
// go test ./internal/goip/ -run TestRunStatsOptionInUsage
func TestRunStatsOptionInUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-help"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("Run(-help) = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout.String(), "-s[tats]") {
		t.Errorf("usage does not list the -s option:\n%s", stdout.String())
	}
}

// TestLinkExtMask is the one-byte decision, isolated from argv.
//
// go test ./internal/goip/ -run TestLinkExtMask
func TestLinkExtMask(t *testing.T) {
	tests := []struct {
		description string
		showStats   int
		want        uint32
	}{
		{
			description: "positive: no -s sends RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS, so the replies carry no counters",
			showStats:   0,
			want:        req.ExtMaskShow,
		},
		{
			description: "positive: one -s clears RTEXT_FILTER_SKIP_STATS, so the kernel attaches IFLA_STATS and IFLA_STATS64",
			showStats:   1,
			want:        req.ExtMaskStats,
		},
		{
			// Unreachable through Run, which rejects a count above one before
			// any object runs — but the function is total and a later caller
			// that reaches it must not get the plain mask back. `ip -s -s`
			// sends the same request as `ip -s`; only the render differs.
			description: "boundary: a count above one still selects the stats mask, because -s -s differs from -s only in rendering",
			showStats:   2,
			want:        req.ExtMaskStats,
		},
		{
			// Defensive, and cheap: a negative count can only arise from a
			// bug, and `> 0` rather than `!= 0` is what keeps it out of the
			// stats arm.
			description: "corner: a negative count is not -s",
			showStats:   -1,
			want:        req.ExtMaskShow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			c := &runCtx{showStats: tt.showStats}
			if got := c.linkExtMask(); got != tt.want {
				t.Errorf("linkExtMask() = %#x, want %#x", got, tt.want)
			}
		})
	}
}
