package misc

import (
	"io"
	"os"
	"runtime"
	"strings"
	"testing"
)

// DieIfNotLinux is a no-op on linux (which the CI / Nix sandbox is).
// It calls log.Fatal on any other GOOS. We can only test the no-op
// branch.
func TestDieIfNotLinux_onLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("only meaningful on linux")
	}
	DieIfNotLinux() // should return without exiting
}

func TestGetHostname(t *testing.T) {
	got := GetHostname()
	if got == "" {
		t.Error("GetHostname returned empty string")
	}
}

func TestByteToMegabyte(t *testing.T) {
	cases := []struct {
		b    uint64
		want uint64
	}{
		{0, 0},
		{1 << 20, 1},
		{2 << 20, 2},
		{1024*1024*1024 + 1, 1024}, // 1 GiB → 1024 MiB (integer division)
		{(1 << 20) - 1, 0},         // just-under a MiB rounds to 0
	}
	for _, tc := range cases {
		if got := byteToMegabyte(tc.b); got != tc.want {
			t.Errorf("byteToMegabyte(%d) = %d, want %d", tc.b, got, tc.want)
		}
	}
}

func TestPrintMemUsage(t *testing.T) {
	// Capture stdout for the duration of the call so the call itself
	// doesn't pollute the test log; verify it produced some output.
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })
	var out strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&out, r) //nolint:errcheck // test plumbing
		close(done)
	}()
	PrintMemUsage()
	_ = w.Close() //nolint:errcheck // test plumbing
	<-done
	if !strings.Contains(out.String(), "Alloc") {
		t.Errorf("PrintMemUsage should print Alloc; got %q", out.String())
	}
}
