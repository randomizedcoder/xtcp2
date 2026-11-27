//go:build gotc_write

package gotc

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The tdc corpus's own commands are tc's request oracle for writes: each
// row's ip.pcap holds what tc sent for cmd and the kernel's answer, ip.out,
// ip.err and ip.rc what tc printed. A qdisc or class write of a kind gotc
// parses must match all of it, request bytes included; a write of a kind
// whose parser waits for increment 18 or 19 is refused, counted by kind.

// tdcWriteSkips pins the refusals by kind.
var tdcWriteSkips = map[string]int{
	"cake": 21, "cake_mq": 22, "cbs": 11, "choke": 7, "codel": 13, "drr": 3, "dualpi2": 12, "etf": 4, "ets": 49,
	"fq_pie": 2, "gred": 7, "hfsc": 9, "hhf": 16, "mqprio": 4, "multiq": 4, "pie": 2, "plug": 9, "qfq": 10, "red": 9,
	"sfb": 12, "skbprio": 4, "taprio": 7,
}

// tdcWriteKnown are the rows gotc cannot match, each with why.
var tdcWriteKnown = map[string]string{}

// tdcWritesSent pins how many tdc writes gotc sends exactly.
const tdcWritesSent = 173

var writeKindRefused = regexp.MustCompile(`not implemented: tc (?:qdisc|class) write of kind (\S+)`)

type tdcCmdRow struct {
	ID  string `json:"id"`
	Cmd string `json:"cmd"`
}

// TestGotcTdcWrites replays every tdc qdisc and class write.
//
// go test -tags gotc_write ./internal/gotc/ -run TestGotcTdcWrites
func TestGotcTdcWrites(t *testing.T) {
	roots, err := filepath.Glob(tdcGlob)
	if err != nil || len(roots) == 0 {
		t.Fatalf("no tdc corpus: %v", err)
	}
	pschedContent = readFile(t, pschedRow)
	var dirs []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Name() == "ip.pcap" {
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
	sent := 0
	for _, dir := range dirs {
		var row tdcCmdRow
		if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "cmd.json"))), &row); err != nil {
			t.Fatal(err)
		}
		args := strings.Fields(row.Cmd)
		if len(args) < 3 || args[0] != "tc" || strings.ContainsAny(row.Cmd, "|;&<>$'\"") {
			continue
		}
		obj := tcShowObject(args[1:])
		i := slices.Index(args, obj)
		if i+1 >= len(args) {
			continue
		}
		verb := args[i+1]
		qdisc := obj == "qdisc" && writeVerb(verb, "add", "change", "replace", "link", "delete")
		class := obj == "class" && writeVerb(verb, "add", "change", "replace", "delete")
		if !qdisc && !class {
			continue
		}
		src, err := NewReplaySource(filepath.Join(dir, "ip.pcap"))
		if err != nil {
			t.Fatal(err)
		}
		var out, errb bytes.Buffer
		rc := run(config{src: src, sys: testSys(t, dir)}, args[1:], &out, &errb)
		if m := writeKindRefused.FindStringSubmatch(errb.String()); m != nil {
			skips[m[1]]++
			continue
		}
		wantRC, err := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(dir, "ip.rc"))))
		if err != nil {
			t.Fatal(err)
		}
		got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: src.Pending()}
		want := result{stdout: readFile(t, filepath.Join(dir, "ip.out")), stderr: readFile(t, filepath.Join(dir, "ip.err")), rc: wantRC}
		if got != want {
			if _, ok := tdcWriteKnown[row.ID]; ok {
				continue
			}
			t.Errorf("%s: %s\n got  %#v\n want %#v", row.ID, row.Cmd, got, want)
			continue
		}
		if _, ok := tdcWriteKnown[row.ID]; ok {
			t.Errorf("%s now matches; drop it from tdcWriteKnown", row.ID)
		}
		sent++
	}
	t.Logf("matched %d tdc writes; refused by kind %v", sent, skips)
	if sent != tdcWritesSent {
		t.Errorf("matched %d tdc writes, want %d", sent, tdcWritesSent)
	}
	for k, n := range skips {
		if tdcWriteSkips[k] != n {
			t.Errorf("kind %s refused %d times, want %d", k, n, tdcWriteSkips[k])
		}
	}
	for k, n := range tdcWriteSkips {
		if skips[k] != n {
			t.Errorf("kind %s refused %d times, want %d", k, skips[k], n)
		}
	}
}
