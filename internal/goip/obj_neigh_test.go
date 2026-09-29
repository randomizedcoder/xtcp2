package goip

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

const neighDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh.pcap"

// neighDevPcap is `ip neigh show dev goip0`. Its replies are the same set as
// neighDumpPcap's — the interesting difference is 8 bytes on the second
// request, which only internal/goip/req can see.
const neighDevPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh_dev.pcap"

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
		// pcap overrides neighDumpPcap for a row whose command has a capture
		// of its own.
		pcap    string
		sidecar string
		// jsonEquivalent compares decoded JSON instead of raw bytes, because
		// `ip -j -p` pretty-prints and goip emits one compact line.
		jsonEquivalent bool
		// unordered compares the lines as a multiset. See the row that sets
		// it for why a neighbor listing needs this and a link listing does
		// not.
		unordered bool
		// noPortid replays the whole file instead of filtering on
		// neighDumpPortid, which is the right thing for a capture taken in
		// the quiet microVM and the wrong thing for the older ones.
		noPortid bool
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
		{
			// `neigh show dev goip0`, against its own capture. Compared as a
			// multiset of lines rather than byte for byte, and the reason is
			// measured rather than defensive: the run that produced this
			// sidecar emitted 192.0.2.52, .51, .50 — kernel hash order —
			// where the committed ip_neigh from an earlier run emitted them
			// ascending. Same topology, same command, different order, which
			// is exactly what model.SortNeighbors' doc says to expect.
			//
			// goip normalizes that order and `ip` does not, so byte equality
			// on a neighbor listing is a property of which run the fixture
			// came from. internal/goipparity takes the same position for the
			// same reason — FacetLines is a multiset and stdout.go:164 says
			// it is "blind to a reordering" — so this row matches the gate
			// rather than being stricter than it.
			//
			// The four rows above ARE byte-exact, and they pass because the
			// capture they cite happens to be ascending. That is worth
			// knowing rather than relying on: a re-capture of
			// netlink_route_getneigh.pcap can break them without anything
			// being wrong, and the fix then is to bring them here.
			description: "positive: `neigh show dev` reproduces ip_neigh_dev, which is ip_neigh minus one token per line",
			args:        []string{"neigh", "show", "dev", "goip0"},
			pcap:        neighDevPcap,
			sidecar:     "ip_neigh_dev",
			unordered:   true,
			// neighDumpPortid belongs to the capture the rows above replay.
			// This file is from a later run with its own portid, and it is
			// single-command clean, so the filter is unnecessary here and
			// would hide every reply — the same call the route captures make
			// (obj_route_test.go:26-39).
			noPortid: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(neighSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			pcap := tc.pcap
			if pcap == "" {
				pcap = neighDumpPcap
			}
			t.Setenv("GOIP_REPLAY", pcap)
			if !tc.noPortid {
				t.Setenv("GOIP_REPLAY_PORTID", neighDumpPortid)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			switch {
			case tc.jsonEquivalent:
				assertJSONEntriesEqual(t, stdout.Bytes(), want)
			case tc.unordered:
				assertSameLines(t, stdout.String(), string(want))
			default:
				if !bytes.Equal(stdout.Bytes(), want) {
					t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
				}
			}
		})
	}
}

// assertSameLines compares two listings as multisets of lines, reporting the
// symmetric difference in both directions so a missing line and an extra one
// are distinguishable.
//
// A multiset and not a set: two identical lines are two entries, and a
// renderer that emitted one of a duplicated pair would otherwise pass.
func assertSameLines(t *testing.T, got, want string) {
	t.Helper()
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
			m[line]++
		}
		return m
	}
	g, w := count(got), count(want)
	for line, n := range w {
		if g[line] != n {
			t.Errorf("line %q appears %d times, want %d", line, g[line], n)
		}
	}
	for line, n := range g {
		if _, ok := w[line]; !ok {
			t.Errorf("unexpected line %q, %d times", line, n)
		}
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
			// The selector, replayed against the BARE command's capture. That
			// works — and is worth doing here rather than only against the
			// dev capture — because the replay answers any RTM_GETNEIGH with
			// the recorded bodies, so what this row exercises is the
			// client-side half: print_neigh's :331 index test and the :415
			// token suppression. The request half needs real bytes and is
			// TestTierANeighShowDevRequests' job.
			description:      "positive: `neigh show dev goip0` lists, with no dev token on the line",
			args:             []string{"neigh", "show", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "192.0.2.50 lladdr ",
		},
		{
			// Resolution is from ll_init_map's cache, so a name absent from
			// the link dump fails locally. ExitFailure and not ExitUsage: the
			// selector IS implemented, so this is a bad command line rather
			// than a missing feature.
			description:      "negative: a device not in the link dump is an error, not an empty listing",
			args:             []string{"neigh", "show", "dev", "nosuch0"},
			wantCode:         ExitFailure,
			wantStderrSubstr: `cannot find device "nosuch0"`,
		},
		{
			// The empty name is a real command line, and it must reach the
			// resolver rather than being read as "no device given" — the same
			// distinction routeSelectors.DevSet exists for.
			description:      "corner: `dev \"\"` resolves nothing and fails, rather than listing everything",
			args:             []string{"neigh", "show", "dev", ""},
			wantCode:         ExitFailure,
			wantStderrSubstr: `cannot find device ""`,
		},
		{
			// duparg (ip/ipneigh.c:526-527). This is where neigh parts company
			// with route, whose `dev`/`oif` pair assigns one variable and lets
			// the last one win.
			description:      "negative: a second `dev` is duparg, not last-wins",
			args:             []string{"neigh", "show", "dev", "goip0", "dev", "lo"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "duplicate",
		},
		{
			// NEXT_ARG() on an exhausted argv.
			description:      "boundary: a trailing `dev` with no value is refused",
			args:             []string{"neigh", "show", "dev"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "missing its value",
		},
		{
			// strcmp, not matches(): `d` is not an abbreviation of `dev`
			// here. In `ip` it falls to the else-arm and is read as a
			// destination prefix; goip refuses it, which is the honest
			// version of not implementing `to PREFIX`.
			description:      "negative: `d` does not abbreviate `dev`",
			args:             []string{"neigh", "show", "d", "goip0"},
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

// runNeighWith drives runNeigh against an arbitrary Source, which is what lets
// the transaction-shape test below count what Run() hides.
func runNeighWith(t *testing.T, src Source, family uint8, args []string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	c := &runCtx{
		src:    src,
		lltab:  NewLLTab(),
		out:    &out,
		errOut: io.Discard,
		family: family,
	}
	err := runNeigh(c, args)
	return out.String(), err
}

// TestNeighShowDevTransactionShape is the assertion this whole increment
// exists for, and it is an assertion of ABSENCE: `neigh show dev NAME` sends
// two dumps and NO single-get, where every other `dev NAME` form in goip sends
// at least one.
//
// do_show_or_flush calls ll_init_map(&rth) at ip/ipneigh.c:597 before it
// resolves the name at :600, so ll_name_to_index's ll_get_by_name hits the
// cache that dump just filled and ll_link_get is never reached
// (lib/ll_map.c:354-359). A goip that reached for LinkByName here — the
// obvious thing to do, and what route, link and addr all correctly do — would
// produce byte-identical output and one extra transaction.
//
// The source is routeReplay, borrowed from obj_route_test.go rather than
// duplicated: it counts dumps and records every single-get, which is exactly
// the pair of numbers at issue. Its Talk arm is the one that must never fire.
//
// Every row replays netlink_route_getneigh.pcap, the BARE command's capture.
// The replay answers whatever is asked, so what varies between rows is only
// what goip asks and what it prints — the request bytes are Tier A's subject,
// in internal/goip/req.
//
// go test ./internal/goip/ -run TestNeighShowDevTransactionShape
func TestNeighShowDevTransactionShape(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		wantDumps   int
		wantErr     bool
		// wantEmpty asserts the output is exactly empty, which no substring
		// test can do.
		wantEmpty bool
		// wantStdoutHas and wantStdoutLacks assert one token's presence or
		// absence rather than the whole rendering, which the sidecar test
		// above already pins.
		wantStdoutHas   []string
		wantStdoutLacks []string
	}{
		{
			description:   "positive: the bare form is two dumps and no single-get",
			args:          []string{"show"},
			wantDumps:     2,
			wantStdoutHas: []string{"192.0.2.50 dev goip0 "},
		},
		{
			// The delta, and there is none to count. Same dump count, same
			// empty get list — contrast TestRouteShowDevTransactionShape,
			// where the equivalent row has one get the bare form does not.
			description:     "positive: `dev NAME` costs the same two dumps and still no single-get",
			args:            []string{"show", "dev", "goip0"},
			wantDumps:       2,
			wantStdoutHas:   []string{"192.0.2.50 lladdr "},
			wantStdoutLacks: []string{"dev goip0"},
		},
		{
			// The client-side half of the filter (ip/ipneigh.c:331), which
			// the replay makes load-bearing: it answers the filtered request
			// with the whole recorded dump, so without the test every entry
			// would print. On a live socket the kernel would have filtered
			// already and this would be belt and braces, exactly as it is in
			// `ip`.
			description: "boundary: a device with no neighbors prints nothing, and still costs two dumps",
			args:        []string{"show", "dev", "lo"},
			wantDumps:   2,
			wantEmpty:   true,
		},
		{
			// Resolution is from the cache, so failure is local and costs
			// only the link dump. `ip` would send ll_link_get here and pay a
			// third transaction; goip stops, which is the position routeShow
			// takes for the same reason.
			description: "negative: an unresolvable device fails after the link dump, with no second dump",
			args:        []string{"show", "dev", "nosuch0"},
			wantDumps:   1,
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newRouteReplay(t, neighDumpPcap)
			got, err := runNeighWith(t, src, unix.AF_UNSPEC, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("runNeigh(%q) = %q, want error", tc.args, got)
				}
			} else if err != nil {
				t.Fatalf("runNeigh(%q): %v", tc.args, err)
			}
			if src.dumps != tc.wantDumps {
				t.Errorf("dumps = %d, want %d", src.dumps, tc.wantDumps)
			}
			if len(src.gets) != 0 {
				t.Errorf("single-gets = %v, want none: ll_init_map already filled the cache", src.gets)
			}
			if tc.wantEmpty && got != "" {
				t.Errorf("stdout = %q, want empty", got)
			}
			for _, want := range tc.wantStdoutHas {
				if !strings.Contains(got, want) {
					t.Errorf("stdout = %q, want it to contain %q", got, want)
				}
			}
			for _, unwanted := range tc.wantStdoutLacks {
				if strings.Contains(got, unwanted) {
					t.Errorf("stdout = %q, want it NOT to contain %q", got, unwanted)
				}
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
