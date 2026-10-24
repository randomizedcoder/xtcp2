package goip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// TestRunMonitorFile is `ip monitor file FILE` through Run, over a file of
// one address on c0 and the monitor_all capture's link dump for its names:
// the -t and -ts stamps, label, dev, and the refusals' exits.
//
// go test ./internal/goip/ -run TestRunMonitorFile
func TestRunMonitorFile(t *testing.T) {
	const now = "2026-10-22T18:11:35Z"
	at, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatal(err)
	}
	stamp := "Timestamp: " + at.Local().Format(asctimeLayoutCst) + " 0 usec\n"
	short := "[" + at.Local().Format("2006-01-02T15:04:05") + ".000000] "
	file := filepath.Join(t.TempDir(), "rtmon")
	body := monAddrBody(3)
	if err := os.WriteFile(file, monFileRecord(unix.RTM_NEWADDR, body, uint32(16+len(body))), 0o600); err != nil {
		t.Fatal(err)
	}
	short16 := filepath.Join(t.TempDir(), "short")
	if err := os.WriteFile(short16, make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	const addr = "3: c0    inet 10.0.0.1/24 scope global \n"
	type result struct {
		rc             int
		stdout, stderr string
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: the address, named from the dump", "monitor file " + file, result{stdout: addr}},
		{"positive: -t stamps it", "-t monitor file " + file, result{stdout: stamp + addr}},
		{"positive: -timestamp in full", "-timestamp monitor file " + file, result{stdout: stamp + addr}},
		{"positive: -ts stamps it on the line, before the label", "-ts monitor label file " + file,
			result{stdout: short + "[ADDR]" + addr}},
		{"positive: -tshort in full", "-tshort monitor file " + file, result{stdout: short + addr}},
		{"positive: all labels it", "monitor all file " + file, result{stdout: "[ADDR]" + addr}},
		{"negative: dev d0 drops it", "monitor dev d0 file " + file, result{}},
		{"positive: dev c0 keeps it", "monitor file " + file + " dev c0", result{stdout: addr}},
		{"negative: dev of no device exits 255", "monitor dev x9 file " + file,
			result{rc: ExitMinusOne, stderr: "goip: monitor: invalid argument: Error: argument \"x9\" is wrong: Device does not exist\n"}},
		{"negative: an unknown argument exits 255", "monitor bogus",
			result{rc: ExitMinusOne, stderr: "goip: monitor: Argument \"bogus\" is unknown, try \"ip monitor help\"\n"}},
		{"negative: no such file exits 255", "monitor file " + file + ".none",
			result{rc: ExitMinusOne, stderr: "goip: monitor: Cannot fopen: no such file or directory\n"}},
		{"corner: -j still prints text", "-j monitor file " + file, result{stdout: addr}},
		{"negative: a truncated file exits 1, rtnl_from_file's -1 negated", "monitor file " + short16,
			result{rc: ExitUsage, stderr: "goip: monitor file: rtnl-from_file: truncated message\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", routeRtnlDir+"monitor_all/ip.pcap")
			t.Setenv("GOIP_NOW", now)
			var stdout, stderr bytes.Buffer
			rc := Run(strings.Fields(tc.args), &stdout, &stderr)
			got := result{rc: rc, stdout: stdout.String(), stderr: stderr.String()}
			if strings.HasPrefix(got.stderr, strings.TrimSuffix(tc.expected.stderr, "\n")) {
				got.stderr = tc.expected.stderr
			}
			if got != tc.expected {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

// TestRunMonitorSources is runMonitor's choice of source: a source that
// cannot subscribe is not implemented, and a refused subscription exits 1.
//
// go test ./internal/goip/ -run TestRunMonitorSources
func TestRunMonitorSources(t *testing.T) {
	tests := []struct {
		description string
		src         Source
		expected    error
	}{
		{"negative: a dump-only source cannot monitor", &mruleFake{}, ErrNotImplemented},
		{"negative: a refused subscription is setup's exit 1", &refusingMonitor{}, ErrMonitorSetup},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errOut bytes.Buffer
			c := newRunCtx(nil, &out, &errOut)
			c.src = tc.src
			err := runMonitor(c, nil)
			if !errors.Is(err, tc.expected) {
				t.Fatalf("err %v, want %v", err, tc.expected)
			}
			if errors.Is(err, ErrMonitorSetup) && exitCodeOf(err) != ExitUsage {
				t.Errorf("exit %d, want 1", exitCodeOf(err))
			}
		})
	}
}

// refusingMonitor is a source whose subscription fails, as a bind would.
type refusingMonitor struct{ mruleFake }

func (*refusingMonitor) Subscribe(monitorSub) (MonitorConn, error) {
	return nil, ErrMonitorSetup
}

// TestNetlinkSubscribe opens the monitor's own socket with the groups
// `ip monitor all` binds and adds, and reads back NETLINK_LISTEN_ALL_NSID,
// -1 for a subscription refused with EPERM: the option needs
// CAP_NET_BROADCAST, which a test not run as root lacks, and `ip` exits 1.
//
// go test ./internal/goip/ -run TestNetlinkSubscribe
func TestNetlinkSubscribe(t *testing.T) {
	s, err := OpenNetlink()
	if err != nil {
		t.Skipf("no NETLINK_ROUTE socket here: %v", err)
	}
	t.Cleanup(func() {
		if cerr := s.Close(); cerr != nil {
			t.Errorf("Close: %v", cerr)
		}
	})
	allNsid := -1
	if os.Geteuid() == 0 {
		allNsid = 1
	}
	tests := []struct {
		description string
		sub         monitorSub
		expected    int
	}{
		{"positive: all, no all-nsid", monitorGroups(0, unix.AF_UNSPEC, false), 0},
		{"positive: all-nsid is set as root, else refused", monitorGroups(ipmonLink, unix.AF_UNSPEC, true), allNsid},
		{"corner: a group the kernel lacks, EINVAL, is tolerated though needed", monitorSub{
			adds: []monitorAdd{{group: 1 << 20, needed: true, errMsg: "Failed to add x group to list"}}}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			conn, err := s.Subscribe(tc.sub)
			if errors.Is(err, unix.EPERM) && errors.Is(err, ErrMonitorSetup) {
				if tc.expected != -1 {
					t.Errorf("Subscribe: %v, want %d", err, tc.expected)
				}
				return
			}
			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			nc, ok := conn.(*netlinkMonitorConn)
			if !ok {
				t.Fatalf("conn is %T", conn)
			}
			got, gerr := unix.GetsockoptInt(nc.fd, unix.SOL_NETLINK, unix.NETLINK_LISTEN_ALL_NSID)
			if gerr != nil {
				t.Fatalf("getsockopt: %v", gerr)
			}
			if got != tc.expected {
				t.Errorf("NETLINK_LISTEN_ALL_NSID = %d, want %d", got, tc.expected)
			}
			if err := conn.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
			if err := conn.Close(); err != nil {
				t.Errorf("a second Close: %v", err)
			}
		})
	}
}
