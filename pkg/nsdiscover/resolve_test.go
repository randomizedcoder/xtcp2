package nsdiscover

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseContainerID(t *testing.T) {
	const id = "3f2a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708" // 64 hex
	tests := []struct {
		name   string
		cgroup string
		want   string
	}{
		{"docker cgroup v2", "0::/system.slice/docker-" + id + ".scope\n", id},
		{"containerd", "0::/kubepods/besteffort/pod123/cri-containerd-" + id + ".scope\n", id},
		{"k8s bare id", "12:pids:/kubepods/burstable/podXYZ/" + id + "\n", id},
		{"none", "0::/system.slice/sshd.service\n", ""},
		{"too short (48 hex)", "0::/x/" + id[:48] + "\n", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseContainerID([]byte(tt.cgroup)); got != tt.want {
				t.Fatalf("parseContainerID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolver_Name_bindMount(t *testing.T) {
	netnsDir := t.TempDir()
	// Create a "named" namespace file and learn its inode.
	p := filepath.Join(netnsDir, "myns")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	if err := unix.Stat(p, &st); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(t.TempDir(), []string{netnsDir})
	r.Refresh()
	if got := r.Name(st.Ino, 0); got != "myns" {
		t.Fatalf("Name(bind-mounted) = %q, want %q", got, "myns")
	}
}

func TestResolver_Name_cgroupFallback(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 64 hex
	procRoot := t.TempDir()
	pid := 4242
	cg := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(cg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cg, "cgroup"), []byte("0::/system.slice/docker-"+id+".scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := NewResolver(procRoot, nil)
	r.Refresh()
	// Unknown inode (no bind mount) → container id from cgroup.
	if got := r.Name(999999, pid); got != id {
		t.Fatalf("Name(cgroup) = %q, want the container id", got)
	}
}

func TestResolver_Name_syntheticFallback(t *testing.T) {
	r := NewResolver(t.TempDir(), nil) // empty procRoot: no cgroup for any pid
	r.Refresh()
	if got := r.Name(4026531840, 7); got != "netns:[4026531840]" {
		t.Fatalf("Name(fallback) = %q, want netns:[4026531840]", got)
	}
}
