//go:build goip_write

package goip

import (
	"fmt"
	"testing"
)

// TestMacsecExitCodes maps each macsec error to ip's exit status.
//
// go test ./internal/goip/ -run TestMacsecExitCodes
func TestMacsecExitCodes(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    int
	}{
		{"positive: usage is exit(-1)", macsecUsage(), ExitMinusOne},
		{"positive: invarg wrapped as usage is 255, not ErrInvalidArgument's 1",
			macsecInvarg(invargErr("expected pn", "x")), ExitMinusOne},
		{"positive: a missing device is -1, exit 1", fmt.Errorf("%w: Device", ErrMacsecRefused), ExitUsage},
		{"positive: a terminated dump is exit(1)", fmt.Errorf("%w: x", ErrMacsecDumpTerminated), ExitUsage},
		{"corner: an unwrapped error is 2", fmt.Errorf("other"), ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := exitCodeOf(tc.err); got != tc.expected {
				t.Errorf("exit %d, want %d", got, tc.expected)
			}
		})
	}
}
