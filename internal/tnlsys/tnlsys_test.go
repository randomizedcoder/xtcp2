package tnlsys

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func parm(name string, fill byte) []byte {
	b := bytes.Repeat([]byte{fill}, IPTunnelParmSize)
	copy(b, name+"\x00")
	return b
}

// fixture is a snapshot built the way Trace builds one.
func fixture() *Snapshot {
	var s Snapshot
	s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "gre1", parm("", 0), parm("gre1", 1), nil, 0)
	s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "gre0", parm("", 0), parm("gre0", 2), nil, 0)
	s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "gre0", parm("", 7), parm("gre9", 3), nil, 0)
	s.AddIoctl(unix.AF_INET6, SIOCGETTUNNEL, "gre1", parm("", 0), parm("gre1", 4), nil, 0)
	s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "gone0", parm("", 0), nil, nil, unix.ENODEV)
	s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "ip6gre1", parm("", 0), parm("ip6gre1", 5), []byte{9, 9}, 0)
	s.AddIoctl(unix.AF_INET, SIOCGETPRL, "isa1", make([]byte, 16*TunnelPRLSize), bytes.Repeat([]byte{6}, 16*TunnelPRLSize), nil, 0)
	s.Props = []PropRec{
		{Dev: "tap0", Prop: "tun_flags", Value: "0x1002\n"},
		{Dev: "tap0", Prop: "owner", Value: "-1\n"},
		{Dev: "tap1", Prop: "group", Errno: unix.ENOENT},
	}
	fds := []TunFD{{Pid: 10, FD: 3, FdInfo: "iff:\ttap0\n", Comm: "holder\n"}}
	s.Fds = &fds
	return &s
}

// roundTrip is s through its JSON form.
func roundTrip(t *testing.T, s *Snapshot) *Snapshot {
	t.Helper()
	b, err := s.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse(%s): %v", b, err)
	}
	return got
}

func TestTnlsys(t *testing.T) {
	type ask struct {
		family int
		req    uint32
		name   string
		in     []byte
	}
	tests := []struct {
		description string
		snap        func(t *testing.T) *Snapshot
		ask         ask
		wantOut     []byte
		wantSpill   []byte
		wantErr     error
	}{
		{
			description: "positive: an ioctl after a JSON round trip answers its recorded output",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gre1", parm("", 0)},
			wantOut:     parm("gre1", 1),
		},
		{
			description: "positive: a fallback device answers per input bytes",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gre0", parm("", 7)},
			wantOut:     parm("gre9", 3),
		},
		{
			description: "positive: the same fallback device with zero input answers itself",
			snap:        func(t *testing.T) *Snapshot { return fixture() },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gre0", parm("", 0)},
			wantOut:     parm("gre0", 2),
		},
		{
			description: "positive: the kernel's write past the argument comes back as spill",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "ip6gre1", parm("", 0)},
			wantOut:     parm("ip6gre1", 5),
			wantSpill:   []byte{9, 9},
		},
		{
			description: "positive: a PRL read of 16 routers",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET, SIOCGETPRL, "isa1", make([]byte, 16*TunnelPRLSize)},
			wantOut:     bytes.Repeat([]byte{6}, 16*TunnelPRLSize),
		},
		{
			description: "negative: a recorded errno comes back as itself, the argument untouched",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gone0", parm("", 0)},
			wantOut:     parm("", 0),
			wantErr:     unix.ENODEV,
		},
		{
			description: "negative: input bytes never recorded are not recorded, not ENODEV",
			snap:        func(t *testing.T) *Snapshot { return fixture() },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gre1", parm("", 1)},
			wantOut:     parm("", 1),
			wantErr:     ErrNotRecorded,
		},
		{
			description: "negative: an unknown device is not recorded",
			snap:        func(t *testing.T) *Snapshot { return fixture() },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "nosuch", parm("", 0)},
			wantOut:     parm("", 0),
			wantErr:     ErrNotRecorded,
		},
		{
			description: "boundary: an empty snapshot answers nothing",
			snap: func(t *testing.T) *Snapshot {
				s, err := Parse([]byte("{}"))
				if err != nil {
					t.Fatal(err)
				}
				return s
			},
			ask:     ask{unix.AF_INET, SIOCGETTUNNEL, "gre1", parm("", 0)},
			wantOut: parm("", 0),
			wantErr: ErrNotRecorded,
		},
		{
			description: "boundary: an argument one byte short of the recorded input misses",
			snap:        func(t *testing.T) *Snapshot { return fixture() },
			ask:         ask{unix.AF_INET, SIOCGETTUNNEL, "gre1", parm("", 0)[:IPTunnelParmSize-1]},
			wantOut:     parm("", 0)[:IPTunnelParmSize-1],
			wantErr:     ErrNotRecorded,
		},
		{
			description: "corner: v4 and v6 under one name are kept apart",
			snap:        func(t *testing.T) *Snapshot { return roundTrip(t, fixture()) },
			ask:         ask{unix.AF_INET6, SIOCGETTUNNEL, "gre1", parm("", 0)},
			wantOut:     parm("gre1", 4),
		},
		{
			description: "corner: a name longer than IFNAMSIZ-1 is cut as strlcpy cuts it",
			snap: func(t *testing.T) *Snapshot {
				var s Snapshot
				s.AddIoctl(unix.AF_INET, SIOCGETTUNNEL, "abcdefghijklmnopq", parm("", 0), parm("x", 1), nil, 0)
				return roundTrip(t, &s)
			},
			ask:     ask{unix.AF_INET, SIOCGETTUNNEL, "abcdefghijklmnoXYZ", parm("", 0)},
			wantOut: parm("x", 1),
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s := tc.snap(t)
			arg := bytes.Clone(tc.ask.in)
			spill, err := s.Ioctl(tc.ask.family, tc.ask.req, tc.ask.name, arg)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !bytes.Equal(arg, tc.wantOut) {
				t.Errorf("arg = %x\nwant  %x", arg, tc.wantOut)
			}
			if !bytes.Equal(spill, tc.wantSpill) {
				t.Errorf("spill = %x, want %x", spill, tc.wantSpill)
			}
		})
	}
}

func TestSnapshotProps(t *testing.T) {
	tests := []struct {
		description string
		dev, prop   string
		want        string
		wantErr     error
	}{
		{"positive: a recorded value", "tap0", "tun_flags", "0x1002\n", nil},
		{"positive: owner -1 is kept as text", "tap0", "owner", "-1\n", nil},
		{"negative: a recorded open failure is its errno", "tap1", "group", "", unix.ENOENT},
		{"negative: an unrecorded prop", "tap0", "group", "", ErrNotRecorded},
		{"corner: an unrecorded device", "tap9", "owner", "", ErrNotRecorded},
	}
	s := roundTrip(t, fixture())
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := s.ReadProp(tc.dev, tc.prop)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSnapshotTunFDs(t *testing.T) {
	empty := []TunFD{}
	tests := []struct {
		description string
		fds         *[]TunFD
		want        []TunFD
		wantErr     error
	}{
		{"positive: the recorded walk", fixture().Fds, []TunFD{{Pid: 10, FD: 3, FdInfo: "iff:\ttap0\n", Comm: "holder\n"}}, nil},
		{"boundary: a recorded walk that found nothing", &empty, []TunFD{}, nil},
		{"negative: no walk recorded", nil, nil, ErrNotRecorded},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s := roundTrip(t, &Snapshot{Fds: tc.fds})
			got, err := s.TunFDs()
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("fds = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseRefuses(t *testing.T) {
	tests := []struct {
		description string
		json        string
		want        string
	}{
		{"negative: malformed hex in", `{"ioctls":[{"family":2,"req":"SIOCGETTUNNEL","name":"x","in":"zz"}]}`, "in: encoding/hex"},
		{"negative: odd-length hex out", `{"ioctls":[{"family":2,"req":"SIOCGETTUNNEL","name":"x","in":"00","out":"0"}]}`, "out: encoding/hex"},
		{"negative: an unknown request", `{"ioctls":[{"family":2,"req":"SIOCADDTUNNEL","name":"x","in":""}]}`, `unknown request "SIOCADDTUNNEL"`},
		{"negative: out longer than in", `{"ioctls":[{"family":2,"req":"SIOCGET6RD","name":"x","in":"00","out":"0000"}]}`, "out is 2 bytes, in 1"},
		{"negative: a negative errno", `{"props":[{"dev":"a","prop":"b","errno":-1}]}`, "cannot unmarshal"},
		{"negative: an unknown field", `{"ioctl":[]}`, "unknown field"},
		{"negative: trailing data", `{} {}`, "trailing data"},
		{"boundary: not JSON", ``, "EOF"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := Parse([]byte(tc.json))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestIfNameAndArgSize(t *testing.T) {
	tests := []struct {
		description string
		got, want   any
	}{
		{"positive: a short name is kept", IfName("gre1"), "gre1"},
		{"boundary: fifteen bytes are kept", IfName("abcdefghijklmno"), "abcdefghijklmno"},
		{"boundary: sixteen bytes lose the last", IfName("abcdefghijklmnop"), "abcdefghijklmno"},
		{"corner: a NUL ends the name", IfName("ab\x00cd"), "ab"},
		{"positive: v4 parm size", ArgSize(unix.AF_INET, SIOCGETTUNNEL), IPTunnelParmSize},
		{"positive: v6 parm size", ArgSize(unix.AF_INET6, SIOCGETTUNNEL), IP6TnlParm2Size},
		{"positive: PRL buffer is 16 routers", ArgSize(unix.AF_INET, SIOCGETPRL), 256},
		{"positive: 6rd size", ArgSize(unix.AF_INET, SIOCGET6RD), Tunnel6RDSize},
		{"negative: an unknown request has no size", ArgSize(unix.AF_INET, 0x89F1), 0},
		{"negative: an unknown request is named in hex", ReqName(0x89F1), "0x89f1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}

func TestLiveIoctl(t *testing.T) {
	tests := []struct {
		description string
		family      int
		req         uint32
		name        string
		arg         []byte
		wantErr     error
		wantSocket  bool
	}{
		{"negative: no such device", unix.AF_INET, SIOCGETTUNNEL, "nosuch0", parm("", 0), unix.ENODEV, false},
		{"negative: a write request is refused before any socket", unix.AF_INET, 0x89F1, "lo", parm("", 0), ErrRequest, false},
		{"boundary: an empty argument is refused", unix.AF_INET, SIOCGETTUNNEL, "lo", nil, ErrRequest, false},
		{"boundary: an argument over 64 KiB is refused", unix.AF_INET, SIOCGETPRL, "lo", make([]byte, MaxArg+1), ErrRequest, false},
		{"corner: a family with no sockets fails at socket(2)", 9999, SIOCGETTUNNEL, "lo", parm("", 0), unix.EAFNOSUPPORT, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			in := bytes.Clone(tc.arg)
			spill, err := Live{}.Ioctl(tc.family, tc.req, tc.name, tc.arg)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			var se *SocketError
			if errors.As(err, &se) != tc.wantSocket {
				t.Errorf("SocketError = %v, want %v", errors.As(err, &se), tc.wantSocket)
			}
			if spill != nil || !bytes.Equal(in, tc.arg) {
				t.Errorf("a failed ioctl changed its argument or spilled: %x", spill)
			}
		})
	}
}

func TestSpillOf(t *testing.T) {
	fill := bytes.Repeat([]byte{SpillFill}, 4)
	tests := []struct {
		description string
		window      []byte
		want        []byte
	}{
		{"positive: an untouched window is no spill", fill, nil},
		{"positive: a written prefix is the spill", []byte{0, 6, SpillFill, SpillFill}, []byte{0, 6}},
		{"positive: the whole window written", []byte{1, 2, 3, 4}, []byte{1, 2, 3, 4}},
		{"boundary: an empty window", nil, nil},
		{"boundary: one byte written", []byte{0, SpillFill, SpillFill, SpillFill}, []byte{0}},
		{"corner: a written SpillFill inside the reply is kept", []byte{SpillFill, 0, SpillFill, SpillFill}, []byte{SpillFill, 0}},
		{"corner: the last byte written", []byte{SpillFill, SpillFill, SpillFill, 1}, []byte{SpillFill, SpillFill, SpillFill, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := SpillOf(tc.window); !bytes.Equal(got, tc.want) {
				t.Errorf("SpillOf = %x, want %x", got, tc.want)
			}
		})
	}
}

// writeTree makes files (content) and symlinks ("-> target") under root.
func writeTree(t *testing.T, root string, tree map[string]string) {
	t.Helper()
	for name, v := range tree {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if target, ok := strings.CutPrefix(v, "-> "); ok {
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveReadProp(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"devices/virtual/net/tap0/tun_flags": "0x1002\n",
		"devices/virtual/net/tap0/owner":     "-1\n",
		"class/net/tap0":                     "-> ../../devices/virtual/net/tap0",
		"class/net/esc":                      "-> ../../../",
	})
	tests := []struct {
		description string
		dev, prop   string
		want        string
		wantErr     error
	}{
		{"positive: through the class/net symlink", "tap0", "tun_flags", "0x1002\n", nil},
		{"positive: owner", "tap0", "owner", "-1\n", nil},
		{"negative: a missing prop", "tap0", "group", "", unix.ENOENT},
		{"negative: a missing device", "tap9", "owner", "", unix.ENOENT},
		{"corner: a dev with a slash never leaves class/net", "../../devices/virtual/net/tap0", "owner", "", unix.ENOENT},
		{"corner: a dev of .. is refused", "..", "owner", "", unix.ENOENT},
		{"corner: a symlink out of the root is refused", "esc", "owner", "", nil},
	}
	l := Live{SysRoot: root}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := l.ReadProp(tc.dev, tc.prop)
			if tc.wantErr == nil && tc.want == "" {
				if err == nil {
					t.Fatalf("read %q out of the root", got)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("value = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLiveTunFDs(t *testing.T) {
	root := t.TempDir()
	self := fmt.Sprint(os.Getpid())
	writeTree(t, root, map[string]string{
		"100/fd/3":         "-> /dev/net/tun",
		"100/fd/4":         "-> /dev/null",
		"100/fdinfo/3":     "pos:\t0\niff:\ttap0\n",
		"100/comm":         "holder\n",
		"102/fd/5":         "-> /dev/net/tun",
		"102/fdinfo/5":     "iff:\ttun1\n",
		"103/fd/6":         "-> /dev/net/tun",
		"104/fd/7":         "-> /dev/net/tunnel",
		"12x/fd/8":         "-> /dev/net/tun",
		"12x/fdinfo/8":     "iff:\ttap0\n",
		"self/fd/9":        "-> /dev/net/tun",
		self + "/fd/1":     "-> /dev/net/tun",
		self + "/fdinfo/1": "iff:\ttap0\n",
		"100/fd/x1":        "-> /dev/net/tun",
		"105/fd/2a":        "-> /dev/net/tun",
		"105/fdinfo/2":     "iff:\tq\n",
		"105/comm":         "a\n",
	})
	got, err := Live{ProcRoot: root}.TunFDs()
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b TunFD) int { return a.Pid*100 + a.FD - b.Pid*100 - b.FD })
	tests := []struct {
		description string
		want        TunFD
	}{
		{"corner: a name with leading digits scans as sscanf's %d", TunFD{Pid: 12, FD: 8, FdInfo: "iff:\ttap0\n", NoComm: true}},
		{"positive: a tun descriptor with fdinfo and comm", TunFD{Pid: 100, FD: 3, FdInfo: "pos:\t0\niff:\ttap0\n", Comm: "holder\n"}},
		{"negative: no comm file is NoComm", TunFD{Pid: 102, FD: 5, FdInfo: "iff:\ttun1\n", NoComm: true}},
		{"corner: an fd name with leading digits reads fdinfo by the number", TunFD{Pid: 105, FD: 2, FdInfo: "iff:\tq\n", Comm: "a\n"}},
	}
	for i, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if i >= len(got) || got[i] != tc.want {
				t.Errorf("walk = %+v, want %+v at %d", got, tc.want, i)
			}
		})
	}
	t.Run("boundary: only those four; no fdinfo, another device, non-numeric names and self are skipped", func(t *testing.T) {
		if len(got) != len(tests) {
			t.Errorf("walk = %+v", got)
		}
	})
	t.Run("negative: a missing root is an error", func(t *testing.T) {
		if _, err := (Live{ProcRoot: filepath.Join(root, "nope")}).TunFDs(); err == nil {
			t.Error("no error")
		}
	})
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	b, err := fixture().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "tnlsys.out")
	if err := os.WriteFile(good, b, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		path        string
		wantIoctls  int
		wantErr     bool
	}{
		{"positive: a marshaled snapshot loads whole", good, len(fixture().Ioctls), false},
		{"negative: a missing file", filepath.Join(dir, "nope"), 0, true},
		{"corner: a directory is not a snapshot", dir, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			s, err := Load(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, want error %v", err, tc.wantErr)
			}
			if err == nil && len(s.Ioctls) != tc.wantIoctls {
				t.Errorf("ioctls = %d, want %d", len(s.Ioctls), tc.wantIoctls)
			}
		})
	}
}

func TestSocketError(t *testing.T) {
	e := &SocketError{Err: unix.EAFNOSUPPORT}
	tests := []struct {
		description string
		got, want   any
	}{
		{"positive: the message names socket", e.Error(), "tnlsys: socket: " + unix.EAFNOSUPPORT.Error()},
		{"positive: it unwraps to the errno", errors.Is(e, unix.EAFNOSUPPORT), true},
		{"negative: not another errno", errors.Is(e, unix.ENODEV), false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v", tc.got, tc.want)
			}
		})
	}
}
