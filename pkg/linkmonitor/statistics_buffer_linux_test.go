package linkmonitor

import (
	"errors"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestStatisticBuffer(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		size                                         int
		invalid                                      bool
	}{
		{"small", "positive", "bounded small payload", "mapping reusable and zeroed", 40, false},
		{"limit", "boundary", "maximum strings payload", "mapping reusable and zeroed", maximumStatisticBuffer, false},
		{"oversize", "negative", "one byte beyond limit", "reject before mapping", maximumStatisticBuffer + 1, true},
		{"short", "negative", "no complete command", "reject before mapping", 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			b := &statisticBuffer{}
			t.Cleanup(func() {
				if err := b.Close(); err != nil {
					t.Error(err)
				}
			})
			data, err := b.bytes(tc.size)
			if (err != nil) != tc.invalid {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err != nil {
				if b.mapping != nil {
					t.Fatal("allocated invalid size")
				}
				return
			}
			data[0], data[len(data)-1] = 1, 2
			again, err := b.bytes(tc.size)
			if err != nil || &data[0] != &again[0] || again[0] != 0 || again[len(again)-1] != 0 {
				t.Fatal(tc.expectedOutcome)
			}
			assertStatisticGuard(t, data)
		})
	}
}

func assertStatisticGuard(t *testing.T, data []byte) {
	t.Helper()
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := errors.Join(unix.Close(pipe[0]), unix.Close(pipe[1])); err != nil {
			t.Error(err)
		}
	})
	if _, err := unix.Write(pipe[1], []byte{1}); err != nil {
		t.Fatal(err)
	}
	// The pointer belongs to an mmap, not a Go allocation. An actual kernel
	// copy must fail at the byte immediately beyond the exposed request slice.
	guard := unsafe.Add(unsafe.Pointer(&data[0]), len(data))
	_, _, errno := unix.Syscall(unix.SYS_READ, uintptr(pipe[0]), uintptr(guard), 1)
	runtime.KeepAlive(data)
	if errno != unix.EFAULT {
		t.Fatalf("guard copy errno=%v", errno)
	}
}
