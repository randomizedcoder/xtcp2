package cgroupid

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

const hex64 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestParseLeaf(t *testing.T) {
	cases := []struct {
		name        string
		wantID      string
		wantRuntime string
	}{
		{"docker-" + hex64 + ".scope", hex64, "docker"},
		{"cri-containerd-" + hex64 + ".scope", hex64, "containerd"},
		{"crio-" + hex64 + ".scope", hex64, "crio"},
		{"libpod-" + hex64 + ".scope", hex64, "podman"},
		{hex64, hex64, "cgroupfs"},
		// Non-container components resolve to nothing.
		{"system.slice", "", ""},
		{"kubepods.slice", "", ""},
		{"init.scope", "", ""},
		{"user@1000.service", "", ""},
		{"docker-nothex.scope", "", ""},
		{"docker-" + hex64[:63] + ".scope", "", ""}, // wrong length
		{hex64[:63], "", ""},                        // bare, wrong length
		{"", "", ""},
	}
	for _, tc := range cases {
		gotID, gotRT := parseLeaf(tc.name)
		if gotID != tc.wantID || gotRT != tc.wantRuntime {
			t.Errorf("parseLeaf(%q) = (%q,%q), want (%q,%q)", tc.name, gotID, gotRT, tc.wantID, tc.wantRuntime)
		}
	}
}

func TestParseContainerFromPath(t *testing.T) {
	// A socket in a sub-cgroup of the container scope still resolves to the
	// container (deepest-match wins, ancestor container found by scanning up).
	rel := filepath.Join("system.slice", "docker-"+hex64+".scope", "init")
	id, rt := parseContainerFromPath(rel)
	if id != hex64 || rt != "docker" {
		t.Fatalf("parseContainerFromPath(%q) = (%q,%q), want (%q,docker)", rel, id, rt, hex64)
	}
	// A path with no container component resolves to nothing.
	if id, _ := parseContainerFromPath(filepath.Join("system.slice", "sshd.service")); id != "" {
		t.Fatalf("expected empty container id, got %q", id)
	}
}

func TestResolverRebuildAndResolve(t *testing.T) {
	root := t.TempDir()
	// Mimic a systemd-driver cgroup2 tree with one docker container scope.
	scope := filepath.Join(root, "system.slice", "docker-"+hex64+".scope")
	if err := os.MkdirAll(scope, 0o755); err != nil {
		t.Fatal(err)
	}
	// Also a non-container cgroup that must NOT resolve.
	other := filepath.Join(root, "system.slice", "sshd.service")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	scopeIno := inodeOf(t, scope)
	otherIno := inodeOf(t, other)

	r := New(root)

	if id, rt := r.Resolve(scopeIno); id != hex64 || rt != "docker" {
		t.Errorf("Resolve(scope) = (%q,%q), want (%q,docker)", id, rt, hex64)
	}
	if id, _ := r.Resolve(otherIno); id != "" {
		t.Errorf("Resolve(non-container) = %q, want empty", id)
	}
	if id, _ := r.Resolve(0xdeadbeef); id != "" {
		t.Errorf("Resolve(unknown) = %q, want empty", id)
	}
}

func inodeOf(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("no syscall.Stat_t for %s", path)
	}
	return st.Ino
}
