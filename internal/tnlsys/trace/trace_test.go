package trace

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"golang.org/x/sys/unix"
)

// helperEnv makes the test binary, run as a child, the command TestTrace
// traces.
const helperEnv = "TNLSYS_TRACE_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(traceHelper())
	}
	os.Exit(m.Run())
}

// traceHelper makes two tunnel ioctls and one other, writes to both
// streams and exits 3.
func traceHelper() int {
	var l tnlsys.Live
	a := make([]byte, tnlsys.IPTunnelParmSize)
	copy(a, "lo")
	_, err1 := l.Ioctl(unix.AF_INET, tnlsys.SIOCGETTUNNEL, "lo", a)
	b := make([]byte, tnlsys.Tunnel6RDSize)
	_, err2 := l.Ioctl(unix.AF_INET, tnlsys.SIOCGET6RD, "nosuch0", b)
	fmt.Printf("%v|%v\n", err1, err2)
	fmt.Fprint(os.Stderr, "helper stderr")
	return 3
}

func TestTrace(t *testing.T) {
	t.Setenv(helperEnv, "1")
	run, recs, err := Trace(&exec.Cmd{Path: os.Args[0], Args: []string{os.Args[0], "-test.run=^$"}})
	if errors.Is(err, unix.EPERM) {
		t.Skipf("ptrace not permitted here: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	in := make([]byte, tnlsys.IPTunnelParmSize)
	copy(in, "lo")
	tests := []struct {
		description string
		got, want   any
	}{
		{"positive: the exit status", run.RC, 3},
		{"positive: stdout is the helper's", run.Stdout, fmt.Sprintf("%v|%v\n", unix.EOPNOTSUPP, unix.ENODEV)},
		{"positive: stderr is the helper's", run.Stderr, "helper stderr"},
		{"positive: two tunnel ioctls, nothing else", len(recs), 2},
		{"positive: the first's family from its socket", recField(recs, 0, "family"), unix.AF_INET},
		{"positive: the first's input as passed", recField(recs, 0, "in"), hex.EncodeToString(in)},
		{"negative: the first failed EOPNOTSUPP, no output", recField(recs, 0, "errno+out"), fmt.Sprint(unix.EOPNOTSUPP) + "|"},
		{"negative: the second is ENODEV for its device", recField(recs, 1, "name+errno"), "nosuch0|" + fmt.Sprint(unix.ENODEV)},
		{"boundary: the 6rd input is 24 bytes", recField(recs, 1, "in"), strings.Repeat("00", tnlsys.Tunnel6RDSize)},
		{"corner: the argv is kept", len(run.Argv), 2},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %v, want %v (recs %+v)", tc.got, tc.want, recs)
			}
		})
	}
}

func recField(recs []tnlsys.IoctlRec, i int, f string) any {
	if i >= len(recs) {
		return nil
	}
	r := recs[i]
	switch f {
	case "family":
		return r.Family
	case "in":
		return r.In
	case "errno+out":
		return fmt.Sprint(r.Errno) + "|" + r.Out
	case "name+errno":
		return r.Name + "|" + fmt.Sprint(r.Errno)
	}
	return nil
}
