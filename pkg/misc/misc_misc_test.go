package misc

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// withFatalf swaps the package-level fatalf for the duration of the test,
// restoring it on cleanup. The capture buffer ends up with the formatted
// message any fatalf calls during the test produced.
func withFatalf(t *testing.T) *strings.Builder {
	t.Helper()
	prev := fatalf
	var captured strings.Builder
	fatalf = func(format string, args ...any) {
		captured.WriteString(strings.TrimSuffix(fmtSprintf(format, args...), "\n"))
		captured.WriteString("\n")
	}
	t.Cleanup(func() { fatalf = prev })
	return &captured
}

// fmtSprintf is a tiny wrapper to keep the fmt import isolated to this helper.
func fmtSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func TestGetHostname(t *testing.T) {
	got := GetHostname()
	if got == "" {
		t.Error("GetHostname returned empty string")
	}
}

func TestGetHostname_error(t *testing.T) {
	prev := hostnameLookup
	hostnameLookup = func() (string, error) { return "", fmt.Errorf("synthetic") }
	t.Cleanup(func() { hostnameLookup = prev })

	captured := withFatalf(t)
	got := GetHostname()
	if got != "" {
		t.Errorf("expected empty string on error; got %q", got)
	}
	if !strings.Contains(captured.String(), "os.Hostname() error") {
		t.Errorf("fatalf not invoked; got %q", captured.String())
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

// ScanFile error branches: bad path (open error) + invalid file (scanner
// error). Both invoke fatalf and return nil instead of exiting.

func TestScanFile_openError(t *testing.T) {
	captured := withFatalf(t)
	got := ScanFile("/no/such/path")
	if got != nil {
		t.Errorf("expected nil on open error; got %v", got)
	}
	if !strings.Contains(captured.String(), "scanFile open file error") {
		t.Errorf("fatalf not invoked with expected message; got %q", captured.String())
	}
}

// ScanFile scanner error: bufio.Scanner errors when a single line
// exceeds bufio.MaxScanTokenSize (64 KiB default). Write a 65 KiB
// no-newline blob into a tempfile to force sc.Err() into the error path.
func TestScanFile_scannerError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "huge.txt")
	// 65 KiB of 'a' without any newline. bufio.Scanner can't handle a
	// single token this large and returns bufio.ErrTooLong.
	blob := strings.Repeat("a", 65*1024)
	if err := os.WriteFile(p, []byte(blob), 0o600); err != nil {
		t.Fatal(err)
	}
	captured := withFatalf(t)
	got := ScanFile(p)
	if got != nil {
		t.Errorf("expected nil on scanner error; got %d lines", len(got))
	}
	if !strings.Contains(captured.String(), "scan file error") {
		t.Errorf("fatalf not invoked with expected message; got %q", captured.String())
	}
}

// ReadFile error branches.

func TestReadFile_openError(t *testing.T) {
	captured := withFatalf(t)
	got := ReadFile("/no/such/path")
	if got != nil {
		t.Errorf("expected nil on open error; got %v", got)
	}
	if !strings.Contains(captured.String(), "open file error") {
		t.Errorf("fatalf not invoked with expected message; got %q", captured.String())
	}
}

// CheckFilePermissions: missing file → fatalf, return false.

func TestCheckFilePermissions_missing(t *testing.T) {
	captured := withFatalf(t)
	got := CheckFilePermissions("/no/such/path", "0644")
	if got {
		t.Error("expected false on missing file")
	}
	if !strings.Contains(captured.String(), "os.Stat error") {
		t.Errorf("fatalf not invoked with expected message; got %q", captured.String())
	}
}

// GetHostname error branch. os.Hostname is hard to force-fail; we exercise
// the fatalf swap by setting GOOS=non-linux via DieIfNotLinux instead.
// DieIfNotLinux on non-linux: fatalf hit + no panic.

func TestDieIfNotLinuxImpl_linux(t *testing.T) {
	dieIfNotLinuxImpl("linux") // no-op, no fatalf
}

func TestDieIfNotLinuxImpl_darwin(t *testing.T) {
	captured := withFatalf(t)
	dieIfNotLinuxImpl("darwin")
	if !strings.Contains(captured.String(), "only designed for linux") {
		t.Errorf("fatalf not invoked; got %q", captured.String())
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
		_, _ = io.Copy(&out, r)
		close(done)
	}()
	PrintMemUsage()
	_ = w.Close()
	<-done
	if !strings.Contains(out.String(), "Alloc") {
		t.Errorf("PrintMemUsage should print Alloc; got %q", out.String())
	}
}
