package goip

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

const neighDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh.pcap"

// neighDumpPortid is the portid `ip neigh show` used during the capture.
// Replay filters replies on it, so a wrong value yields an empty listing
// rather than an error.
const neighDumpPortid = "894"

const neighSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

// TestNeighShowMatchesCapturedSidecars replays the committed RTM_GETNEIGH dump
// and compares goip's output with the `ip neigh show` sidecars captured
// alongside it.
//
// The comparison target is `ip_neigh`, the plain form, not `ip_neigh_n`. The
// `_n` suffix in this corpus means `ip -d` (nix/capture-netlink-fixtures.nix:389
// says so, and nix/microvms/mkVm.nix:1928 writes it with `ip -d neigh show`),
// and goip has no -d surface. The two files happen to be byte-identical here,
// because `-d` adds nothing to a neighbor line the way it adds `unicast`,
// `table main` and `scope global` to a route line — but asserting against the
// one goip actually implements is what keeps that an observation rather than a
// dependency.
//
// go test ./internal/goip/ -run TestNeighShowMatchesCapturedSidecars
func TestNeighShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		sidecar     string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
	}{
		{
			description: "positive: `neigh show` reproduces ip_neigh line for line",
			args:        []string{"neigh", "show"},
			sidecar:     "ip_neigh",
		},
		{
			// `ip neigh` with no verb lists, so a bare object must not be a
			// usage error.
			description: "positive: a bare `neigh` is a show",
			args:        []string{"neigh"},
			sidecar:     "ip_neigh",
		},
		{
			description: "positive: `neigh list` is the same listing as `neigh show`",
			args:        []string{"neigh", "list"},
			sidecar:     "ip_neigh",
		},
		{
			// The `-d` variant, asserted because it IS identical for this
			// corpus: if a future capture makes it differ, this row fails and
			// says so rather than leaving the claim in a comment.
			description: "boundary: ip_neigh_n is identical to ip_neigh, because -d adds no neighbor token",
			args:        []string{"neigh", "show"},
			sidecar:     "ip_neigh_n",
		},
		{
			description:    "positive: `-json neigh show` reproduces ip_neigh_json's keys and values",
			args:           []string{"-json", "neigh", "show"},
			sidecar:        "ip_neigh_json",
			jsonEquivalent: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(neighSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", neighDumpPcap)
			t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if !tc.jsonEquivalent {
				if !bytes.Equal(stdout.Bytes(), want) {
					t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
				}
				return
			}
			assertJSONEntriesEqual(t, stdout.Bytes(), want)
		})
	}
}

// assertJSONEntriesEqual compares two `ip -j`-shaped arrays of objects key by
// key in both directions, so a missing key and an extra key are both reported.
func assertJSONEntriesEqual(t *testing.T, gotRaw, wantRaw []byte) {
	t.Helper()
	var got, want []map[string]any
	if err := json.Unmarshal(gotRaw, &got); err != nil {
		t.Fatalf("goip JSON: %v (%s)", err, gotRaw)
	}
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatalf("sidecar JSON: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		for k, wv := range want[i] {
			gv, ok := got[i][k]
			if !ok {
				t.Errorf("entry %d missing key %q", i, k)
				continue
			}
			if !jsonEqual(gv, wv) {
				t.Errorf("entry %d key %q = %#v, want %#v", i, k, gv, wv)
			}
		}
		for k := range got[i] {
			if _, ok := want[i][k]; !ok {
				t.Errorf("entry %d has extra key %q", i, k)
			}
		}
	}
}

// jsonEqual compares two decoded JSON values structurally.
//
// Arrays are walked elementwise so a failure names the entry that differs;
// everything else falls through to reflect.DeepEqual. The fallback is not
// decoration: `==` on an `any` holding a map PANICS, and route rendering
// produces maps — `via` is a nested object, and `metrics` and `nexthops` are
// arrays of them — where neighbor rendering produces only strings.
func jsonEqual(a, b any) bool {
	as, aok := a.([]any)
	bs, bok := b.([]any)
	if aok != bok {
		return false
	}
	if !aok {
		return reflect.DeepEqual(a, b)
	}
	if len(as) != len(bs) {
		return false
	}
	for i := range as {
		if !jsonEqual(as[i], bs[i]) {
			return false
		}
	}
	return true
}

// TestRunNeighArgs covers the CLI surface: the verbs runNeigh accepts, and the
// selectors it refuses rather than silently ignores. Ignoring a selector would
// be worse than refusing it, because the parity harness drives `ip` and `goip`
// with the same argv and would then be comparing two different questions.
//
// go test ./internal/goip/ -run TestRunNeighArgs
func TestRunNeighArgs(t *testing.T) {
	const firstEntry = "192.0.2.50 dev goip0 "

	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `neigh show` renders the first captured entry",
			args:             []string{"neigh", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// "n" reaches neigh, and the verb abbreviates through matches() too.
			description:      "positive: `n s` abbreviates both the object and the verb",
			args:             []string{"n", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// The kernel's own spelling, and the one the object table registers.
			description:      "positive: `neighbour show` spells the object out",
			args:             []string{"neighbour", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			description:      "negative: an unknown neigh verb is refused",
			args:             []string{"neigh", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// A write verb must stay unreachable: goip is read-only, and `add`
			// reaching a handler at all would be the wrong kind of bug.
			description:      "negative: the write verb `add` is refused",
			args:             []string{"neigh", "add"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// The selector exists in `ip` and not here. Refusing it names the
			// gap; ignoring it would list every neighbor and look like success.
			description:      "negative: `neigh show dev goip0` is refused, not ignored",
			args:             []string{"neigh", "show", "dev", "goip0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "boundary: a bare `neigh` lists rather than erroring",
			args:             []string{"neigh"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstEntry,
		},
		{
			// runNeigh rejects args[1] before it sends anything, so a second
			// argument fails even when the first one is a valid verb.
			description:      "boundary: one extra argument after a valid verb is refused",
			args:             []string{"neigh", "show", "nud"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// Options precede the object in iproute2, so this is a selector
			// named "-4", not a family option.
			description:      "corner: an option after the object is not an option",
			args:             []string{"neigh", "show", "-4"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", neighDumpPcap)
			t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.wantCode {
				t.Errorf("Run(%q) = %d, want %d; stderr: %s", tc.args, code, tc.wantCode, stderr.String())
			}
			if tc.wantStdoutPrefix != "" && !strings.HasPrefix(stdout.String(), tc.wantStdoutPrefix) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tc.wantStdoutPrefix)
			}
			if tc.wantStderrSubstr != "" && !strings.Contains(stderr.String(), tc.wantStderrSubstr) {
				t.Errorf("stderr = %q, want to contain %q", stderr.String(), tc.wantStderrSubstr)
			}
		})
	}
}

// TestNeighShowEntryCount pins the entry count against the sidecar, which is
// what makes runNeigh's one filtering rule observable: iproute2's default state
// filter omits NUD_NOARP entries, and runNeigh reproduces that. If the filter
// ever dropped a wanted entry — or stopped dropping an unwanted one — the count
// moves even when every rendered line still looks right.
//
// go test ./internal/goip/ -run TestNeighShowEntryCount
func TestNeighShowEntryCount(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantLines   int
	}{
		{
			description: "positive: the listing has exactly the sidecar's four entries",
			args:        []string{"neigh", "show"},
			wantLines:   4,
		},
		{
			description: "boundary: `neigh list` yields the same count as `neigh show`",
			args:        []string{"neigh", "list"},
			wantLines:   4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", neighDumpPcap)
			t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if got := strings.Count(stdout.String(), "\n"); got != tc.wantLines {
				t.Errorf("lines = %d, want %d; output %q", got, tc.wantLines, stdout.String())
			}
		})
	}
}
