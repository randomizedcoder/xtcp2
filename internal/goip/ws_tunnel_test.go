package goip

import (
	"bytes"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

// The ws/tunnel and ws/tuntap spaces (goip 8C) replayed through goip's own
// entry point, argv as ip's: each row's tnlsnap run, its netlink capture
// (netlink_route_tnlsys.pcap) as GOIP_REPLAY and its sidecar (tnlsys.out)
// as GOIP_TNLSYS, so the netlink and the ioctl, sysfs and /proc answers are
// one run's. goip's stdout and exit status must be that run's, byte for
// byte; and the row's own run (ip.out) printed the same, but for -d,
// whose pids are each run's own.

const wsTunnelRoot = "../../pkg/xtcpnl/testdata/7_3_1/ws"

// wsTunnelRow is one row of a tunnel space: its command and the tnlsnap
// run it replays.
type wsTunnelRow struct {
	dir string
	row wsRowFile
	run tnlsys.Run
}

func wsTunnelRows(t *testing.T, space string) []wsTunnelRow {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(wsTunnelRoot, space, "*"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(dirs)
	out := make([]wsTunnelRow, 0, len(dirs))
	for _, d := range dirs {
		r := wsTunnelRow{dir: d}
		if err := json.Unmarshal([]byte(mustReadFile(t, filepath.Join(d, "cmd.json"))), &r.row); err != nil {
			t.Fatal(err)
		}
		var s struct {
			Run *tnlsys.Run `json:"run"`
		}
		if err := json.Unmarshal([]byte(mustReadFile(t, filepath.Join(d, "tnlsys.out"))), &s); err != nil {
			t.Fatal(err)
		}
		if s.Run == nil {
			t.Fatalf("%s: tnlsys.out has no run", d)
		}
		r.run = *s.Run
		out = append(out, r)
	}
	return out
}

// wsTunnelReplay runs goip over the row's tnlsnap run.
func wsTunnelReplay(t *testing.T, r *wsTunnelRow) (rc int, stdout, stderr string) {
	t.Helper()
	t.Setenv("GOIP_REPLAY", filepath.Join(r.dir, "netlink_route_tnlsys.pcap"))
	t.Setenv("GOIP_REPLAY_PORTID", "")
	t.Setenv("GOIP_TNLSYS", filepath.Join(r.dir, "tnlsys.out"))
	var o, e bytes.Buffer
	rc = run(strings.Fields(r.row.Cmd)[1:], strings.NewReader(""), &o, &e)
	return rc, o.String(), e.String()
}

// wsTunnelSkip is why goip does not replay a row, or "": a batch, which
// goip refuses; -pretty, which goip implements for no object; and a write,
// an ioctl or TUNSETIFF goip does not make (it refuses each with exit 1,
// where ip exits 255 for some argument errors first).
func wsTunnelSkip(cmd string) string {
	f := strings.Fields(cmd)
	switch {
	case strings.Contains(cmd, "-batch"):
		return "batch"
	case slices.Contains(f, "-p"):
		return "pretty"
	case slices.ContainsFunc(f, func(w string) bool {
		return w == "add" || w == "del" || w == "change" || w == "prl" || w == "6rd"
	}):
		return "write"
	}
	return ""
}

// TestWsTunnelReplay replays every ws/tunnel and ws/tuntap row.
//
// go test ./internal/goip/ -run TestWsTunnelReplay
func TestWsTunnelReplay(t *testing.T) {
	tests := []struct {
		description string
		space       string
		rows        int // on disk
		skipped     map[string]int
	}{
		{"positive: every ip tunnel and ip -6 tunnel read row", "tunnel", 171, map[string]int{"batch": 8, "pretty": 2, "write": 4}},
		{"positive: every ip tuntap read row", "tuntap", 46, map[string]int{"batch": 4, "write": 4}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			rows := wsTunnelRows(t, tc.space)
			if len(rows) != tc.rows {
				t.Errorf("%d rows on disk, want %d", len(rows), tc.rows)
			}
			skipped := map[string]int{}
			for i := range rows {
				r := &rows[i]
				if why := wsTunnelSkip(r.row.Cmd); why != "" {
					skipped[why]++
					continue
				}
				t.Run(filepath.Base(r.dir), func(t *testing.T) {
					rc, stdout, stderr := wsTunnelReplay(t, r)
					if rc != r.run.RC || rc != r.row.Result.RC {
						t.Fatalf("%s: exit %d, ip's %d (row %d)\nstderr %q\nip's %q", r.row.Cmd, rc, r.run.RC,
							r.row.Result.RC, stderr, r.run.Stderr)
					}
					if stdout != r.run.Stdout {
						t.Errorf("%s: stdout\n got %q\nwant %q", r.row.Cmd, stdout, r.run.Stdout)
					}
					if !strings.Contains(r.row.Cmd, "-d") {
						if ipOut := mustReadFile(t, filepath.Join(r.dir, "ip.out")); ipOut != r.run.Stdout {
							t.Errorf("%s: the row's run printed %q, its tnlsnap run %q", r.row.Cmd, ipOut, r.run.Stdout)
						}
					}
					if r.run.Stderr == "" && rc == 0 && stderr != "" {
						t.Errorf("%s: stderr %q, ip's empty", r.row.Cmd, stderr)
					}
				})
			}
			if !maps.Equal(skipped, tc.skipped) {
				t.Errorf("skipped %v, want %v", skipped, tc.skipped)
			}
		})
	}
}
