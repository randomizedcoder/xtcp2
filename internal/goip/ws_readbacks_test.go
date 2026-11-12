package goip

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
)

// O9, the readback differential (goip-parity-next.md, NX7): every workbench
// row's readbacks, `ip … show` after the row's command, are replayed through
// goip over the row's own dump capture. goip must print what ip printed and
// send what ip sent. Every readback that does not is listed in
// testdata/ws-readbacks.txt with the first word of ip's output goip lacks,
// or goip's refusal. A new divergence and a fixed one both fail until the
// file is regenerated:
//
//	go test ./internal/goip/ -run TestWsReadbacks -update-ws-readbacks
//
// TestWsEncapReadbacks and TestWsV730Readbacks hold the same readbacks to
// finer rules for their spaces; this one is the whole corpus.

var updateWsReadbacks = flag.Bool("update-ws-readbacks", false, "rewrite testdata/ws-readbacks.txt")

const wsReadbacksGolden = "testdata/ws-readbacks.txt"

var wsDigits = regexp.MustCompile(`\b[0-9]+\b`)

// wsReadbackFirstGap is the first word of ip's first differing line that
// goip's line lacks, or why goip's output differs if not by a word: goip's
// refusal, its requests, or whitespace alone.
func wsReadbackFirstGap(r wsReadback) string {
	if r.rc != ExitOK && r.want != "" {
		msg := strings.TrimSpace(r.stderr)
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			msg = msg[:i] // drop the ErrNotImplemented suffix all refusals share
		}
		if i := strings.Index(msg, ": "); i >= 0 {
			msg = msg[i+2:] // and the "goip: " prefix
		}
		msg = wsDigits.ReplaceAllString(msg, "N") // one line per refusal, not per rule
		return fmt.Sprintf("refuses %q", msg)
	}
	if r.stdout == r.want {
		if !r.sameReqs {
			return "requests"
		}
		return ""
	}
	got, want := strings.Split(r.stdout, "\n"), strings.Split(r.want, "\n")
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	var g, w string
	if i < len(got) {
		g = got[i]
	}
	if i < len(want) {
		w = want[i]
	}
	have := map[string]bool{}
	for _, f := range strings.Fields(g) {
		have[f] = true
	}
	for _, f := range strings.Fields(w) {
		if !have[f] {
			return fmt.Sprintf("lacks %q", f)
		}
	}
	if len(strings.Fields(g)) > len(strings.Fields(w)) {
		return "adds words"
	}
	return "spacing"
}

func TestWsReadbacks(t *testing.T) {
	useLLMap720(t)
	useQlen720(t)
	t.Cleanup(func() { useWsO5Release("") })
	var report []string
	head := make([]string, 0, len(wsO5Spaces))
	gaps := map[string]int{}
	for _, sp := range wsO5Spaces {
		useWsO5Release(sp)
		ents, err := os.ReadDir(wsO5SpacePath(sp))
		if err != nil {
			t.Fatal(err)
		}
		same, differ := 0, 0
		for _, e := range ents {
			dir := filepath.Join(wsO5SpacePath(sp), e.Name())
			b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
			if err != nil {
				t.Fatal(err)
			}
			var row struct {
				Readback []struct{ Cmd, Name string } `json:"readback"`
			}
			if err := json.Unmarshal(b, &row); err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			for _, rb := range row.Readback {
				gap := wsReadbackFirstGap(wsReadbackRun(t, dir, rb.Name, rb.Cmd))
				if gap == "" {
					same++
					continue
				}
				differ++
				gaps[gap]++
				report = append(report, fmt.Sprintf("%s/%s/%s %s | %s", sp, e.Name(), rb.Name, gap, rb.Cmd))
			}
		}
		head = append(head, fmt.Sprintf("# %s: %d same, %d differ", sp, same, differ))
	}
	keys := make([]string, 0, len(gaps))
	for k := range gaps {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	head = slices.Grow(head, len(keys)+len(report))
	for _, k := range keys {
		head = append(head, fmt.Sprintf("# gap: %d readbacks: %s", gaps[k], k))
	}
	got := strings.Join(append(head, report...), "\n") + "\n"
	if *updateWsReadbacks {
		if err := os.WriteFile(wsReadbacksGolden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(wsReadbacksGolden)
	if err != nil {
		t.Fatalf("%v (run with -update-ws-readbacks to write it)", err)
	}
	for _, l := range wsO5GoldenDiff(string(want), got) {
		t.Error(l)
	}
}

// useQlen720 sets render.RenderQlenZero, as the workbench's iproute2 7.2.0
// prints a present txqlen of 0; main_test.go clears it for the 7.1.x
// fixtures.
func useQlen720(t *testing.T) {
	t.Helper()
	prev := render.RenderQlenZero
	render.RenderQlenZero = true
	t.Cleanup(func() { render.RenderQlenZero = prev })
}

// TestWsReadbackFirstGap checks how a differing readback is named.
func TestWsReadbackFirstGap(t *testing.T) {
	tests := []struct {
		description string
		r           wsReadback
		expected    string
	}{
		{description: "positive: the same output and requests", r: wsReadback{stdout: "a b\n", want: "a b\n", sameReqs: true}},
		{description: "positive: a word ip prints and goip lacks", r: wsReadback{stdout: "x\n1 lladdr STALE \n", want: "x\n1 lladdr STALE proto static \n", sameReqs: true},
			expected: `lacks "proto"`},
		{description: "negative: goip refuses", r: wsReadback{rc: ExitUsage, stderr: "goip: rule pref 0: iproute2 renders FRA_DSCP: object recognized but not implemented by goip\n", want: "0:\tfrom all\n"},
			expected: `refuses "rule pref N: iproute2 renders FRA_DSCP"`},
		{description: "negative: the same output from other requests", r: wsReadback{stdout: "a\n", want: "a\n"}, expected: "requests"},
		{description: "boundary: a refusal where ip printed nothing is the match", r: wsReadback{rc: ExitUsage, sameReqs: true}},
		{description: "corner: goip prints a word more", r: wsReadback{stdout: "a b c\n", want: "a b\n", sameReqs: true}, expected: "adds words"},
		{description: "corner: only the spacing differs", r: wsReadback{stdout: "a  b\n", want: "a b\n", sameReqs: true}, expected: "spacing"},
		{description: "corner: ip prints a line goip lacks", r: wsReadback{stdout: "a\n", want: "a\n    alias hello\n", sameReqs: true},
			expected: `lacks "alias"`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := wsReadbackFirstGap(tc.r); got != tc.expected {
				t.Errorf("got %q, expected %q", got, tc.expected)
			}
		})
	}
}
