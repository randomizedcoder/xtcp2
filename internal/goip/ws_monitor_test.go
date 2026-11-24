package goip

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"io"
)

// O10, the monitor replay golden (goip-parity-events.md, EP1-T01): every
// event row's pcap replayed through goip's monitor — the same run the row's
// `.out` holds `ip monitor`'s decoded output of, the capio discipline — and
// held to that `.out`, text and errors. The canary blocks pair with their
// notifications, both ways, the A1-T02 rule TestMonitorMatchesIp holds its
// seven rows to.
//
// The replay is the monitor's own: the row's pcap under GOIP_REPLAY, the
// notifications after the init dump's DONE handed to accept as a socket
// handed them (source_monitor.go's replayMonitorConn), the link cache from
// the recorded dump. The netns names the nsid prints carry are the ones
// `ip` resolved under NETNS_RUN_DIR at capture time, read out of the row's
// own `.out` — a replay cannot ask the host's /run/netns.
//
// Every row that does not print ip's output is listed in
// testdata/ws-monitor.txt with the first word of ip's first differing line
// goip's line lacks, or how else it differs. A new divergence and a fixed
// one both fail until the file is regenerated:
//
//	go test ./internal/goip/ -run TestWsMonitor -update-ws-monitor
func TestWsMonitor(t *testing.T) {
	var report []string
	head := make([]string, 0, 4)
	gaps := map[string]int{}
	same, differ := 0, 0
	roots, err := filepath.Glob("../../pkg/xtcpnl/testdata/*/ws*")
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		ents, err := os.ReadDir(filepath.Join(root, "ev"))
		if err != nil {
			continue // a root whose corpus has no event rows
		}
		// The release knobs: ws-ip730's rows were captured with iproute2
		// 7.3.0, and useWsO5Release keys them by the ip730/ prefix.
		rel := "ws"
		if strings.HasSuffix(root, "-ip730") {
			rel = wsO5IP730Prefix + "ws"
		}
		useWsO5Release(rel)
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		slices.Sort(names)
		for _, name := range names {
			_, gap := wsMonitorRow(t, filepath.Join(root, "ev", name))
			if gap == "" {
				same++
				continue
			}
			differ++
			gaps[gap]++
			report = append(report, fmt.Sprintf("%s/ev/%s %s", filepath.Base(root), name, gap))
		}
	}
	t.Cleanup(func() { useWsO5Release("") })
	head = append(head, fmt.Sprintf("# ev: %d same, %d differ", same, differ))
	keys := make([]string, 0, len(gaps))
	for k := range gaps {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		head = append(head, fmt.Sprintf("# gap: %d rows: %s", gaps[k], k))
	}
	got := strings.Join(append(head, report...), "\n") + "\n"
	if *updateWsMonitor {
		if err := os.WriteFile(wsMonitorGolden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(wsMonitorGolden)
	if err != nil {
		t.Fatalf("%v (run with -update-ws-monitor to write it)", err)
	}
	for _, l := range wsO5GoldenDiff(string(want), got) {
		t.Error(l)
	}
}

var updateWsMonitor = flag.Bool("update-ws-monitor", false, "rewrite testdata/ws-monitor.txt")

const wsMonitorGolden = "testdata/ws-monitor.txt"

// wsMonitorNetnsRe is the nsid print's name, as many rows' nsid lines carry.
var wsMonitorNetnsRe = regexp.MustCompile(`nsid ([0-9]+) \(iproute2 netns name: ([^)]+)\)`)

// wsMonitorRow replays one event row's capture through goip's monitor and
// names the first gap between its output and the row's `.out`.
// wsMonitorRow replays one event row's capture through goip's monitor and
// names the first gap between its output and the row's `.out`.
func wsMonitorRow(t testing.TB, dir string) (string, string) {
	t.Helper()
	want, args := wsMonitorSetup(t, dir)
	var stdout, stderr bytes.Buffer
	if rc := Run(strings.Fields(args), &stdout, &stderr); rc != ExitOK {
		return args, fmt.Sprintf("rc %d %q", rc, strings.TrimSpace(stderr.String()))
	}
	if stderr.Len() != 0 {
		return args, fmt.Sprintf("stderr %q", stderr.String())
	}
	return args, wsMonitorGap(stdout.String(), want)
}

// wsMonitorSetup reads one event row and points the replay machinery at
// it: the monitor arguments out of its cmd, the `.out` to hold goip to,
// the netns names ip resolved under NETNS_RUN_DIR, and
// GOIP_REPLAY/GOIP_NOW. The benchmark hoists it out of its loop.
func wsMonitorSetup(t testing.TB, dir string) (string, string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
	if err != nil {
		t.Fatal(err)
	}
	var row struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	args, ok := wsMonitorArgs(row.Cmd)
	if !ok {
		t.Fatalf("%s: cmd %q is not one xtcp2-nl-ipmon invocation", dir, row.Cmd)
	}
	wantB, err := os.ReadFile(filepath.Join(dir, "ip.out"))
	if err != nil {
		t.Fatal(err)
	}
	want := string(wantB)
	// The netns names ip printed, the ones it resolved under NETNS_RUN_DIR.
	names := map[int32]string{}
	for _, m := range wsMonitorNetnsRe.FindAllStringSubmatch(want, -1) {
		var id int32
		if _, err := fmt.Sscanf(m[1], "%d", &id); err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		names[id] = m[2]
	}
	saved := monitorNetnsName
	monitorNetnsName = func(_ *service.Service, nsid int32) (string, bool) {
		name, ok := names[nsid]
		return name, ok
	}
	t.Cleanup(func() { monitorNetnsName = saved })
	os.Setenv("GOIP_REPLAY", filepath.Join(dir, "ip.pcap"))
	os.Setenv("GOIP_NOW", "2026-11-25T12:00:00Z")
	return want, args
}

// wsMonitorArgs is the ipmon invocation's monitor arguments: the second of
// its double-quoted words, `monitor …` as `ip` takes them.
func wsMonitorArgs(cmd string) (string, bool) {
	q := strings.Split(cmd, `"`)
	if len(q) < 3 || !strings.HasPrefix(cmd, "xtcp2-nl-ipmon ") || !strings.HasPrefix(q[1], "monitor ") {
		return "", false
	}
	return q[1], true
}

// wsMonitorGap is the first word of ip's first differing line goip's line
// lacks, or how else the two outputs differ, O9's rule.
func wsMonitorGap(got, want string) string {
	if got == want {
		return ""
	}
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	i := 0
	for i < len(g) && i < len(w) && g[i] == w[i] {
		i++
	}
	var gl, wl string
	if i < len(g) {
		gl = g[i]
	}
	if i < len(w) {
		wl = w[i]
	}
	have := map[string]bool{}
	for _, f := range strings.Fields(gl) {
		have[f] = true
	}
	for _, f := range strings.Fields(wl) {
		if !have[f] {
			return fmt.Sprintf("lacks %q", f)
		}
	}
	if len(strings.Fields(gl)) > len(strings.Fields(wl)) {
		return "adds words"
	}
	return "spacing"
}

// wsMonitorPicks are the EP1 twelve (goip-parity-events.md, EP1-T03), one
// row per family, the tier's benchmark picks.
var wsMonitorPicks = []string{
	"evlink-carrier", "evaddr-dad", "evroute-autoconf", "evneigh-v4",
	"evnhb-bucket", "evconf-del", "evrule-v4", "evstats-l3",
	"evnsid-live", "evprefix-route", "anycast-del", "evseq-mixed",
}

// BenchmarkWsMonitor replays each of the EP1 twelve through goip's
// monitor, the O10 harness's path, so a render regression that passes the
// text gate still has to explain a step change here.
//
//	go test ./internal/goip/ -bench BenchmarkWsMonitor -benchtime 10x
func BenchmarkWsMonitor(b *testing.B) {
	roots, err := filepath.Glob("../../pkg/xtcpnl/testdata/*/ws/ev")
	if err != nil {
		b.Fatal(err)
	}
	if len(roots) == 0 {
		b.Fatal("no event rows under testdata/*/ws/ev")
	}
	for _, name := range wsMonitorPicks {
		b.Run(name, func(b *testing.B) {
			want, args := wsMonitorSetup(b, filepath.Join(roots[0], name))
			b.ResetTimer()
			var out bytes.Buffer
			for i := 0; i < b.N; i++ {
				out.Reset()
				if rc := Run(strings.Fields(args), &out, io.Discard); rc != ExitOK {
					b.Fatalf("%s: rc %d", args, rc)
				}
				if out.String() != want {
					b.Fatalf("%s: output drifted", args)
				}
			}
		})
	}
}
