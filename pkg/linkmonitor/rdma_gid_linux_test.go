package linkmonitor

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestEmptyGIDSlot(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		err                                          error
		want                                         bool
	}{
		{"empty", "positive", "kernel empty GID slot", "ignore absent association", syscall.EINVAL, true},
		{"removed", "corner", "GID entry vanished", "ignore absent association", os.ErrNotExist, true},
		{"wrapped", "positive", "sysfs read wraps empty-slot errno", "preserve empty-slot classification", errors.Join(&os.PathError{Op: "read", Path: "ndevs/1", Err: syscall.EINVAL}), true},
		{"denied", "negative", "GID read denied", "propagate permission failure", syscall.EACCES, false},
		{"io", "negative", "sysfs read failed", "propagate I/O failure", syscall.EIO, false},
		{"canceled", "boundary", "empty slot and canceled read", "do not hide cancellation", errors.Join(syscall.EINVAL, context.Canceled), false},
		{"close", "corner", "empty slot and close failure", "do not hide cleanup error", errors.Join(syscall.EINVAL, syscall.EIO), false},
		{"same errno", "corner", "read and close both return EINVAL", "multiple operation failures remain failures", errors.Join(syscall.EINVAL, syscall.EINVAL), false},
		{"success", "boundary", "populated slot", "process value normally", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if emptyGIDSlot(tc.err) != tc.want {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
