package goip

import (
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
)

// The `ip link set … inet` tests (docs/netlink/goip-link-kinds.md,
// "KG0-T01 `inet` in goip's keyword chain"), against KC1's rows of the
// 7.3.0 rtnl corpus, which ran `ip link set dev d0 inet …` with d0 at
// ifindex 3.

// useLLMap720 makes the index-cache lookups 7.2.0's, which captured the
// 7.3.0 corpus, for the rest of t (main_test.go defaults to 7.1.0's).
func useLLMap720(t *testing.T) {
	t.Helper()
	prev := req.LLMapNameOnly
	req.LLMapNameOnly = true
	t.Cleanup(func() { req.LLMapNameOnly = prev })
}

// TestErrLinkUsageExitCode pins exitCodeOf's arm order: a link usage
// refusal wraps ErrInvalidArgument and still exits 255.
func TestErrLinkUsageExitCode(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{description: "positive: a link usage refusal", err: linkUsage(incompleteErr()), expected: ExitMinusOne},
		{description: "negative: a plain argument error", err: incompleteErr(), expected: ExitUsage},
		{description: "corner: wrapped again", err: errors.Join(linkUsage(invargErr("x", "y"))), expected: ExitMinusOne},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exitCodeOf = %d, want %d", got, tc.expected)
			}
		})
	}
}
