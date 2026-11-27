package gotc

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The tdc corpus's verify readbacks are tc's own oracle: each row of
// pkg/xtcpnl/testdata/*/ws/tdc ran a tc command, then a readback whose
// nlmon capture is netlink_route_verify.pcap and whose stdout is
// verify.out. A readback that is a `tc qdisc`, `tc class`, `tc filter` or
// `tc chain` read must render to verify.out byte for byte; one whose dump
// holds a kind gotc does not print is refused, and counted by kind.

const tdcGlob = "../../pkg/xtcpnl/testdata/*/ws/tdc"

// readbackSkips pins how many readbacks each out-of-slice kind takes away,
// so a decoder that starts (or stops) refusing one shows.
var readbackSkips = map[string]int{
	"cake": 19, "cake_mq": 22, "choke": 7, "codel": 13, "dualpi2": 14, "fq_pie": 2, "gred": 6,
	"hhf": 14, "pie": 1, "plug": 11, "red": 10, "sfb": 13, "skbprio": 4,
}

// readbackRendered pins how many readbacks render exactly: 216 qdisc and
// class shows, 273 filter and chain shows and gets, and 398 `tc actions`
// lists and gets (increment 12's 887), then increment 13's 138: 5 bpf
// filter shows and 133 readbacks holding pedit, tunnel_key, ife, ct, gate
// or bpf, in filters and in `tc actions`. None is eBPF. Increment 15's
// nine kinds add 89 (two of them taprio under -d with a size table); 4
// more readbacks of those kinds also hold plug, and are now plug's.
const readbackRendered = 1114

// kindRefused is a refusal by name: a qdisc kind is its bare name, the
// filter path's are prefixed (filter:, action:, ematch:).
var kindRefused = regexp.MustCompile(`not implemented: (?:(qdisc|filter|action) kind|(ematch)) "([^"]+)"`)

// refusedKey is a kindRefused match's readbackSkips key.
func refusedKey(m []string) string {
	switch {
	case m[1] == "qdisc":
		return m[3]
	case m[2] != "":
		return m[2] + ":" + m[3]
	}
	return m[1] + ":" + m[3]
}

type tdcRow struct {
	ID       string `json:"id"`
	Readback []struct {
		Name string `json:"name"`
		Cmd  string `json:"cmd"`
	} `json:"readback"`
}

// tcShowObject is the object word of a tc command line, after its
// options: qdisc, class, filter, chain or actions (by prefix, as tc
// matches it) for the readbacks gotc renders.
func tcShowObject(args []string) string {
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// TestGotcReadbacks replays every in-slice tdc qdisc and class readback.
//
// go test ./internal/gotc/ -run TestGotcReadbacks
func TestGotcReadbacks(t *testing.T) {
	roots, err := filepath.Glob(tdcGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 {
		t.Fatal("no tdc corpus")
	}
	pschedContent = readFile(t, pschedRow)
	var dirs []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Name() == "netlink_route_verify.pcap" {
				dirs = append(dirs, filepath.Dir(p))
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(dirs)
	skips := map[string]int{}
	rendered := 0
	for _, dir := range dirs {
		var row tdcRow
		if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "cmd.json"))), &row); err != nil {
			t.Fatal(err)
		}
		var cmd string
		for _, rb := range row.Readback {
			if rb.Name == "verify" {
				cmd = rb.Cmd
			}
		}
		args := strings.Fields(cmd)
		if len(args) < 2 || args[0] != "tc" || strings.ContainsAny(cmd, "|;&<>") {
			continue
		}
		if obj := tcShowObject(args[1:]); obj != "qdisc" && obj != "class" && obj != "filter" && obj != "chain" &&
			!matches(obj, "actions") {
			continue
		}
		src, err := NewReplaySource(filepath.Join(dir, "netlink_route_verify.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		run(config{src: src, sys: testSys(t, dir)}, args[1:], &out, &errb)
		if m := kindRefused.FindStringSubmatch(errb.String()); m != nil {
			skips[refusedKey(m)]++
			continue
		}
		want := readFile(t, filepath.Join(dir, "verify.out"))
		if out.String() != want || src.Pending() != 0 {
			t.Errorf("%s: %s\n got  %q\n want %q\n stderr %q pending %d", row.ID, cmd, out.String(), want, errb.String(), src.Pending())
			continue
		}
		rendered++
	}
	t.Logf("rendered %d readbacks exactly; refused by kind %v", rendered, skips)
	if rendered != readbackRendered {
		t.Errorf("rendered %d, pinned %d", rendered, readbackRendered)
	}
	for k, n := range skips {
		if readbackSkips[k] != n {
			t.Errorf("kind %s refused %d readbacks, pinned %d", k, n, readbackSkips[k])
		}
	}
	for k, n := range readbackSkips {
		if _, ok := skips[k]; !ok {
			t.Errorf("kind %s pinned at %d refusals, saw none", k, n)
		}
	}
}
