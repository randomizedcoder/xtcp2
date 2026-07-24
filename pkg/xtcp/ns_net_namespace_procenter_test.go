package xtcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestProcNsPath pins the /proc/<pid>/ns/net handle format.
func TestProcNsPath(t *testing.T) {
	if got := procNsPath(1234); got != "/proc/1234/ns/net" {
		t.Fatalf("procNsPath(1234) = %q, want /proc/1234/ns/net", got)
	}
}

// TestOpenAndSetNSWithRetries_checkMountFalseSkipsGate verifies that entering a
// namespace by a /proc/<pid>/ns/net handle (checkMount=false) skips the
// mount-info readiness gate entirely and enters via open()+setns() on that path.
// The gate is pointed at an empty mountinfo so it WOULD fail if consulted.
func TestOpenAndSetNSWithRetries_checkMountFalseSkipsGate(t *testing.T) {
	// An empty mountinfo → checkMountInfoWithRetries would return (false, nil),
	// which would short-circuit to -1 if the gate ran.
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(tmp, []byte(""), 0o600); err != nil {
		t.Fatalf("seed mountinfo: %v", err)
	}
	orig := mountInfoDir
	mountInfoDir = tmp
	t.Cleanup(func() { mountInfoDir = orig })

	const fakeFD = 7
	var gotPath string
	fake := openAndSetnsSyscallsT{
		open: func(path string, _ int, _ uint32) (int, error) {
			gotPath = path
			return fakeFD, nil
		},
		setns: func(_ int, _ int) error { return nil },
		close: func(_ int) error { return nil },
	}

	x := newTestXTCP(t, "null:")
	var fd int
	withFakeSyscalls(t, fake, func() {
		p := procNsPath(1234)
		fd = x.openAndSetNSWithRetries(&p, false)
	})

	if fd != fakeFD {
		t.Fatalf("fd = %d, want %d (must enter despite an unsatisfiable mount-info gate)", fd, fakeFD)
	}
	if gotPath != "/proc/1234/ns/net" {
		t.Fatalf("open() path = %q, want /proc/1234/ns/net", gotPath)
	}
}

// TestOpenAndSetNSWithRetries_checkMountTrueStillGates is the paired negative:
// with checkMount=true and an empty mountinfo, the gate short-circuits to -1 and
// open() is never called — proving the gate still applies to the bind-mount path.
func TestOpenAndSetNSWithRetries_checkMountTrueStillGates(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "mountinfo")
	if err := os.WriteFile(tmp, []byte(""), 0o600); err != nil {
		t.Fatalf("seed mountinfo: %v", err)
	}
	orig := mountInfoDir
	mountInfoDir = tmp
	t.Cleanup(func() { mountInfoDir = orig })
	withShortBackoff(t)

	opened := false
	fake := openAndSetnsSyscallsT{
		open:  func(string, int, uint32) (int, error) { opened = true; return 9, nil },
		setns: func(int, int) error { return nil },
		close: func(int) error { return nil },
	}

	x := newTestXTCP(t, "null:")
	var fd int
	withFakeSyscalls(t, fake, func() {
		ns := "/run/netns/never_mounted"
		fd = x.openAndSetNSWithRetries(&ns, true)
	})

	if fd != -1 {
		t.Fatalf("fd = %d, want -1 (gate must short-circuit on empty mountinfo)", fd)
	}
	if opened {
		t.Fatal("open() was called, but the mount-info gate should have short-circuited first")
	}
}
