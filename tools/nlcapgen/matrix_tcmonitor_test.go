package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcMonitorSpace pins the tc monitor rows (gotc 14C): `tc monitor` live
// under xtcp2-nl-tcmon over four fixtures, `tc monitor file` over generated
// records, a batch, and the refusals. Rows are keyed by their cmd value's
// name.
//
// go test ./tools/nlcapgen/ -run TestTcMonitorSpace
func TestTcMonitorSpace(t *testing.T) {
	rows, err := matrixRows(families["tcmonitor"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 27 {
		t.Errorf("%d rows, want 27 (12 live, 8 offline, 7 negatives)", len(rows))
	}
	byDesc := map[string]row{}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcmonitor/") {
			t.Errorf("%s: not in tcmonitor/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcMonitorModules, ",") {
			t.Errorf("%s: initns %t serial %t proto %q modules %v", r.ID, r.InitNS, r.Serial, r.Proto, r.Modules)
		}
		for _, m := range r.Modules {
			if !tdcGuestModules[m] {
				t.Errorf("%s: module %s is not one the guest has", r.ID, m)
			}
		}
		_, name, _ := strings.Cut(r.Desc, "cmd=")
		name, _, _ = strings.Cut(name, ": ")
		byDesc[name] = r
	}

	tests := []struct {
		description string
		name        string // the cmd value
		expected    string // kind rc refused-by | live | until count
	}{
		// Positive.
		{"positive: the qdisc lifecycle (MO6, K19, K20)", "qdisc:live:monitor", "positive 0 none | live | 1"},
		{"positive: the class lifecycle (MO8)", "class:live:monitor", "positive 0 none | live | 1"},
		{"positive: the filter and chain lifecycle (MO7, K22)", "filter:live:monitor", "positive 0 none | live | 1"},
		{"positive: a filter with two actions (bind 1)", "filteracts:live:monitor", "positive 0 none | live | 1"},
		{"positive: the action lifecycle (MO9)", "actions:live:monitor", "positive 0 none | live | 1"},
		{"positive: -s", "stats:live:-s_monitor", "positive 0 none | live | 1"},
		{"positive: -d", "details:live:-d_monitor", "positive 0 none | live | 1"},
		{"positive: a file of every object's event (MO12)", "file:monitor_file_m.bin", "positive 0 none | file | 0"},
		// Negative.
		{"negative: a 2040-op filter's event is truncated (MO5)", "big:live:monitor", "negative 2 ack-then-fail | live | 0"},
		{"negative: help (MO1)", "ng:bare:monitor_help", "negative 255 iproute2 | file | 0"},
		{"negative: an unknown word (MO14)", "ng:bare:monitor_bogus", "negative 255 iproute2 | file | 0"},
		{"negative: file with no value (MO14)", "ng:bare:monitor_file", "negative 255 iproute2 | file | 0"},
		{"negative: no such file (MO13)", "ng:bare:monitor_file_/nonexistent", "negative 255 iproute2 | file | 0"},
		{"negative: a truncated file (MO13)", "ng:trunc:monitor_file_m.bin", "negative 255 iproute2 | file | 0"},
		{"negative: a malformed file (MO13)", "ng:malformed:monitor_file_m.bin", "negative 255 iproute2 | file | 0"},
		// Boundary.
		{"boundary: a 2038-op filter's event fits in 16 KiB", "under:live:monitor", "boundary 0 none | live | 1"},
		{"boundary: an empty file", "empty:monitor_file_m.bin", "boundary 0 none | file | 0"},
		// Corner.
		{"corner: -t", "stamp:live:-t_monitor", "corner 0 none | live | 1"},
		{"corner: -tshort", "stampshort:live:-tshort_monitor", "corner 0 none | live | 1"},
		{"corner: -j is text (MO4)", "json:live:-j_monitor", "corner 0 none | live | 1"},
		{"corner: a renamed device keeps its old name (MO10)", "rename:live:monitor", "corner 0 none | live | 1"},
		{"corner: -j over a file (MO4)", "file:-j_monitor_file_m.bin", "corner 0 none | file | 0"},
		{"corner: stamps on non-events, and an unknown type (MO2, MO3)", "nonevents:-t_monitor_file_m.bin", "corner 0 none | file | 0"},
		{"corner: a batch's statics carry into the file (MO11)", "file:batch:show-then-file", "corner 0 none | file | 0"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byDesc[tc.name]
			if !ok {
				t.Fatalf("no row %q", tc.name)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			until := 0
			if u := r.Axes["until"]; u != "" {
				until = len(strings.Split(u, "\n"))
			}
			got := fmt.Sprintf("%s %d %s | %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Axes["mode"], until)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	objs := map[string]bool{}
	all := new(strings.Builder)
	for _, r := range rows {
		live := r.Axes["mode"] == "live"
		// A live monitor runs only under the helper, which stops it with
		// SIGTERM; nothing else starts one.
		if live != strings.HasPrefix(r.Cmd, "xtcp2-nl-tcmon ") {
			t.Errorf("%s: mode %q but cmd %q", r.ID, r.Axes["mode"], r.Cmd)
		}
		if !live && !strings.HasPrefix(r.Cmd, "tc ") {
			t.Errorf("%s: an offline row's cmd is not tc: %q", r.ID, r.Cmd)
		}
		if live && r.Axes["trigger"] == "" {
			t.Errorf("%s: a live row with no trigger", r.ID)
		}
		for _, fam := range []string{"qdisc", "class", "filter", "chain", "actions"} {
			if strings.Contains(r.Axes["trigger"], "tc "+fam+" ") {
				objs[fam] = true
			}
		}
		all.WriteString(r.Cmd + "\n" + r.Axes["trigger"] + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, fam := range []string{"qdisc", "class", "filter", "chain", "actions"} {
		if !objs[fam] {
			t.Errorf("no trigger makes a %s event", fam)
		}
	}
	// Each event type: NEW and DEL of qdisc, class, filter, chain and action.
	for _, s := range []string{"tc qdisc add", "tc qdisc del", "tc class add", "tc class del", "tc filter add",
		"tc filter del", "tc chain add", "tc chain del", "tc actions add", "tc actions del", "tc actions flush",
		"tc qdisc replace", "tc filter replace", "tc filter change", "tc actions replace", "ip link set d0 name d1",
		"printf '2040'", "printf '2038'"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row, trigger or fixture has %q", s)
		}
	}
	// cBPF only.
	if ebpf := regexp.MustCompile(`\b(obj|object-file|object-pinned)\b`); ebpf.MatchString(all.String()) {
		t.Errorf("a row loads eBPF: %q", ebpf.FindString(all.String()))
	}
	// The event file holds six records of four objects; the non-event file
	// four records.
	for _, f := range []struct {
		data []byte
		n    int
	}{{monEvents(), 6}, {monNonEvents(), 4}} {
		if got := countRecs(f.data); got != f.n {
			t.Errorf("%d records, want %d", got, f.n)
		}
		if !strings.Contains(all.String(), base64.StdEncoding.EncodeToString(f.data)) {
			t.Error("a record file no row writes")
		}
	}

	// Determinism: the same seed writes the same bytes, record files and all.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcmonitor"](), matrixOpts{Seed: 1})
		if err != nil {
			t.Fatal(err)
		}
		if err := writeRows(buf, nil, rows); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two runs with seed 1 differ")
	}
}

// countRecs walks rtnl_from_file records by their length fields.
func countRecs(b []byte) int {
	n := 0
	for len(b) >= 16 {
		l := int(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
		l = (l + 3) &^ 3
		if l < 16 || l > len(b) {
			return -1
		}
		b = b[l:]
		n++
	}
	return n
}
