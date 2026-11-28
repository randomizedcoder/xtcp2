package trace

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// TestThreadGone is the tracer's rule for a thread that vanishes in a
// stop: ESRCH, however wrapped, skips the thread; any other error stops
// the trace.
//
// go test ./internal/tnlsys/trace/ -run TestThreadGone
func TestThreadGone(t *testing.T) {
	tests := []struct {
		description string
		err         error
		expected    bool
	}{
		{"positive: ESRCH from PTRACE_GETREGS", unix.ESRCH, true},
		{"positive: ESRCH wrapped as syscallStop wraps it", fmt.Errorf("tnlsys: trace: regs: %w", unix.ESRCH), true},
		{"negative: EIO is a real failure", fmt.Errorf("tnlsys: trace: peek: %w", unix.EIO), false},
		{"negative: a non-errno error", errors.New("tnlsys: trace: short read"), false},
		{"boundary: nil", nil, false},
		{"corner: ESRCH joined with another error", errors.Join(unix.EPERM, unix.ESRCH), true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := threadGone(tc.err); got != tc.expected {
				t.Errorf("threadGone(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}
