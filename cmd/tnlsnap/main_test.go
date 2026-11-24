package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

const helperEnv = "TNLSNAP_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		fmt.Print("helper out")
		os.Exit(4)
	}
	os.Exit(m.Run())
}

// self is the test binary re-run as the helper TestMain turns it into.
func self() *exec.Cmd {
	return &exec.Cmd{Path: os.Args[0], Args: []string{os.Args[0], "-test.run=^$"}}
}

func TestRunUsage(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantRC      int
		wantErr     string
	}{
		{"negative: no arguments", nil, 2, "usage: tnlsnap -- IP-ARG..."},
		{"negative: arguments without --", []string{"tunnel", "show"}, 2, "usage"},
		{"boundary: -- with no ip arguments", []string{"--"}, 2, "usage"},
		{"negative: no ip on PATH", []string{"--", "tunnel"}, 1, "tnlsnap: exec: \"ip\""},
	}
	t.Setenv("PATH", t.TempDir())
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errb bytes.Buffer
			if rc := run(tc.args, &out, &errb); rc != tc.wantRC {
				t.Errorf("rc = %d, want %d", rc, tc.wantRC)
			}
			if !strings.Contains(errb.String(), tc.wantErr) || out.Len() != 0 {
				t.Errorf("stderr = %q, stdout = %q", errb.String(), out.String())
			}
		})
	}
}

func TestSnapshot(t *testing.T) {
	t.Setenv(helperEnv, "1")
	sys, proc := t.TempDir(), t.TempDir()
	for name, v := range map[string]string{
		"devices/virtual/net/tap0/tun_flags": "0x1002\n",
		"devices/virtual/net/tap0/owner":     "-1\n",
		"devices/virtual/net/tap0/group":     "100\n",
		"devices/virtual/net/d0/ifindex":     "2\n",
		"class/net/bonding_masters":          "\n",
	} {
		p := filepath.Join(sys, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"tap0", "d0"} {
		if err := os.Symlink("../../devices/virtual/net/"+d, filepath.Join(sys, "class/net", d)); err != nil {
			t.Fatal(err)
		}
	}
	b, err := snapshot(self(), tnlsys.Live{SysRoot: sys, ProcRoot: proc}, true)
	if err != nil {
		t.Fatal(err)
	}
	noWalk, err := snapshot(self(), tnlsys.Live{SysRoot: sys, ProcRoot: proc}, false)
	if err != nil {
		t.Fatal(err)
	}
	nw, err := tnlsys.Parse(noWalk)
	if err != nil {
		t.Fatal(err)
	}
	s, err := tnlsys.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	fds, fdErr := s.TunFDs()
	tests := []struct {
		description string
		got, want   any
	}{
		{"positive: the traced run's status", s.Run.RC, 4},
		{"positive: the traced run's stdout is kept, not passed through", s.Run.Stdout, "helper out"},
		{"positive: tap0's three props", len(s.Props), 3},
		{"positive: group read", s.Props[2].Value, "100\n"},
		{"negative: no ioctls from a command that makes none", len(s.Ioctls), 0},
		{"corner: d0 and bonding_masters are not tun devices", s.Props[0].Dev, "tap0"},
		{"boundary: an empty /proc is a recorded empty walk", fmt.Sprint(len(fds), fdErr), "0 <nil>"},
		{"corner: no walk asked, none recorded", nw.Fds == nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
	t.Run("negative: a missing sysfs is an error", func(t *testing.T) {
		if _, err := snapshot(self(), tnlsys.Live{SysRoot: filepath.Join(sys, "nope"), ProcRoot: proc}, true); err == nil {
			t.Error("no error")
		}
	})
}

func TestRunIP(t *testing.T) {
	bin := t.TempDir()
	if err := os.Symlink(os.Args[0], filepath.Join(bin, "ip")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv(helperEnv, "1")
	// An empty sysfs: the host's need not exist (the nix sandbox has none).
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "class", "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	saved := liveSys
	liveSys = tnlsys.Live{SysRoot: sys, ProcRoot: t.TempDir()}
	t.Cleanup(func() { liveSys = saved })
	var out, errb bytes.Buffer
	rc := run([]string{"--", "-j", "tunnel", "show"}, &out, &errb)
	s, err := tnlsys.Parse(out.Bytes())
	if err != nil {
		t.Fatalf("rc %d, stderr %q: %v", rc, errb.String(), err)
	}
	tests := []struct {
		description string
		got, want   any
	}{
		{"positive: tnlsnap exits 0 whatever ip exits", rc, 0},
		{"positive: ip's status is recorded", s.Run.RC, 4},
		{"positive: argv is ip and the arguments", fmt.Sprint(s.Run.Argv), "[ip -j tunnel show]"},
		{"negative: nothing on stderr", errb.String(), ""},
		{"corner: no -d, no /proc walk", s.Fds == nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

// TestWalksProc pins when tnlsnap records the /proc walk: when ip would
// run show_processes.
func TestWalksProc(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    bool
	}{
		{"positive: -d tuntap", []string{"-d", "tuntap", "show"}, true},
		{"positive: -details after -j, tap", []string{"-j", "-details", "tap"}, true},
		{"positive: --det", []string{"--det", "tuntap"}, true},
		{"positive: -d on a batch, whose lines may be tuntap", []string{"-d", "-batch", "b.txt"}, true},
		{"positive: tunt is tuntap", []string{"-d", "tunt"}, true},
		{"negative: no -d", []string{"-j", "tuntap", "show"}, false},
		{"negative: -d tunnel", []string{"-d", "tunnel", "show"}, false},
		{"negative: tun is tunnel, first in the table", []string{"-d", "tun"}, false},
		{"negative: t is tunnel", []string{"-d", "t"}, false},
		{"negative: -d after the object is an argument", []string{"tuntap", "show", "-d"}, false},
		{"negative: a batch without -d", []string{"-batch", "-"}, false},
		{"negative: an unknown option, ip exits", []string{"-d", "-bogus", "tuntap"}, false},
		{"negative: -detailsx is no option", []string{"-detailsx", "tuntap"}, false},
		{"boundary: no arguments", nil, false},
		{"boundary: -d and nothing else", []string{"-d"}, false},
		{"corner: -netns takes a value", []string{"-d", "-netns", "tuntap", "link"}, false},
		{"corner: -n is -netns", []string{"-d", "-n", "x", "tap"}, true},
		{"corner: -- ends the options", []string{"-d", "--", "tuntap"}, true},
		{"corner: -color=never", []string{"-color=never", "-d", "tuntap"}, true},
		{"corner: -de is a prefix of -details", []string{"-de", "tuntap"}, true},
		{"corner: -d is -details, not -Version or -force", []string{"-d", "tap"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := walksProc(tc.args); got != tc.expected {
				t.Errorf("walksProc(%q) = %t, want %t", tc.args, got, tc.expected)
			}
		})
	}
}
