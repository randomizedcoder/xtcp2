package main

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// TestTcAction2Space pins the second tc actions rows (gotc 13C): `actions
// list/get` of pedit, tunnel_key, ife, ct, gate and bpf, and `filter show`
// of bpf filters and of a matchall holding all six, over six fixtures,
// their batches and their refusals. Rows are keyed by command, fixture and
// batch file.
//
// go test ./tools/nlcapgen/ -run TestTcAction2Space
func TestTcAction2Space(t *testing.T) {
	rows, err := matrixRows(families["tcaction2"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 105 {
		t.Errorf("%d rows, want 105 (49 commands plain and -j, 7 negatives)", len(rows))
	}
	byKey := map[string]row{}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "tcaction2/") {
			t.Errorf("%s: not in tcaction2/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != strings.Join(tcAction2Modules, ",") {
			t.Errorf("%s: initns %t serial %t proto %q modules %v", r.ID, r.InitNS, r.Serial, r.Proto, r.Modules)
		}
		for _, m := range r.Modules {
			if !tdcGuestModules[m] {
				t.Errorf("%s: module %s is not one the guest has", r.ID, m)
			}
		}
		if _, dup := byKey[key(r)]; dup {
			t.Errorf("%s: %q twice", r.ID, key(r))
		}
		byKey[key(r)] = r
	}

	tests := []struct {
		description string
		key         string // cmd|fix|batch
		expected    string // kind rc refused-by errno | pre count
	}{
		// Positive.
		{"positive: pedit list (AC42, AC44)", "tc actions ls action pedit|acped|", "positive 0 none  | 6"},
		{"positive: pedit htypes in JSON (AC43)", "tc -j actions get action pedit index 3|acped|", "positive 0 none  | 6"},
		{"positive: tunnel_key v4 with flags (AC49)", "tc -j actions get action tunnel_key index 2|actk|", "positive 0 none  | 7"},
		{"positive: tunnel_key geneve options", "tc actions get action tunnel_key index 3|actk|", "positive 0 none  | 7"},
		{"positive: ife (AC51, AC53)", "tc -j actions ls action ife|acife|", "positive 0 none  | 5"},
		{"positive: ct nat range", "tc actions get action ct index 5|acct|", "positive 0 none  | 10"},
		{"positive: gate schedule (AC59)", "tc -j actions get action gate index 1|acgate|", "positive 0 none  | 3"},
		{"positive: bpf action", "tc actions ls action bpf|acbpf|", "positive 0 none  | 9"},
		{"positive: bpf filters and all six kinds at bind 1 (AC66)", "tc filter show dev d0 ingress|acbpf|", "positive 0 none  | 9"},
		{"positive: a batch of lists then a filter show", "tc -batch b.txt|acbpf|ls-then-filter", "positive 0 none  | 10"},
		// Negative.
		{"negative: no such pedit", "tc actions get action pedit index 999|acped|", "negative 255 kernel ENOENT | 6"},
		{"negative: no such tunnel_key", "tc actions get action tunnel_key index 999|actk|", "negative 255 kernel ENOENT | 7"},
		{"negative: no such ife", "tc actions get action ife index 999|acife|", "negative 255 kernel ENOENT | 5"},
		{"negative: no such ct", "tc actions get action ct index 999|acct|", "negative 255 kernel ENOENT | 10"},
		{"negative: no such gate", "tc actions get action gate index 999|acgate|", "negative 255 kernel ENOENT | 3"},
		{"negative: no such bpf", "tc actions get action bpf index 999|acbpf|", "negative 255 kernel ENOENT | 9"},
		{"negative: a write", "tc actions delete action pedit index 9|bare|", "negative 255 kernel ENOENT | 0"},
		// Boundary.
		{"boundary: no pedit actions", "tc actions ls action pedit|bare|", "boundary 0 none  | 0"},
		{"boundary: gate wildcards", "tc actions get action gate index 2|acgate|", "boundary 0 none  | 3"},
		{"boundary: a short ct label", "tc -j actions get action ct index 9|acct|", "boundary 0 none  | 10"},
		// Corner.
		{"corner: pedit under -o (AC39)", "tc -o actions ls action pedit|acped|", "corner 0 none  | 6"},
		{"corner: erspan v1 (AC48)", "tc -j actions get action tunnel_key index 5|actk|", "corner 0 none  | 7"},
		{"corner: erspan v2 (AC48)", "tc -j actions get action tunnel_key index 6|actk|", "corner 0 none  | 7"},
		{"corner: ife dst only (AC52)", "tc actions get action ife index 4|acife|", "corner 0 none  | 5"},
		{"corner: a bare ct nat (AC57)", "tc -j actions get action ct index 7|acct|", "corner 0 none  | 10"},
		{"corner: the ftp helper (AC56)", "tc actions get action ct index 8|acct|", "corner 0 none  | 10"},
		{"corner: gate under -o (AC61)", "tc -o actions get action gate index 1|acgate|", "corner 0 none  | 3"},
		{"corner: a terse bpf dump", "tc -br actions ls action bpf|acbpf|", "corner 0 none  | 9"},
		{"corner: get then list in a batch", "tc -batch b.txt|acct|get-then-ls", "corner 0 none  | 11"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no row %q", tc.key)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	write := regexp.MustCompile(`\b(add|del|delete|change|replace|flush)\b`)
	fixtures := map[string]bool{}
	reads := map[string]bool{}
	for k, r := range byKey {
		fixtures[r.Axes["fix"]] = true
		// Writes appear only as refusals.
		if write.MatchString(r.Cmd) && !strings.HasPrefix(r.Desc, "negative:") {
			t.Errorf("%s: %q writes but is not a negative", r.ID, r.Cmd)
		}
		reads[r.Cmd] = true
		// Every positive command runs plain and -j.
		if strings.HasPrefix(r.Desc, "negative:") || strings.HasPrefix(r.Cmd, "tc -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "tc ", "tc -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}
	for _, fix := range []string{"bare", "acped", "actk", "acife", "acct", "acgate", "acbpf"} {
		if !fixtures[fix] {
			t.Errorf("fixture %s has no row", fix)
		}
	}
	// Each kind is listed and got, and sits in the matchall a filter show
	// prints at bind 1.
	for _, kind := range []string{"pedit", "tunnel_key", "ife", "ct", "gate", "bpf"} {
		listed, got := false, false
		for cmd := range reads {
			listed = listed || strings.HasSuffix(cmd, "ls action "+kind)
			got = got || strings.Contains(cmd, "get action "+kind+" index")
		}
		if !listed || !got {
			t.Errorf("kind %s: listed %t, got %t", kind, listed, got)
		}
		if !strings.Contains(acbpfFix[len(acbpfFix)-1], "action "+kind+" ") {
			t.Errorf("kind %s is not in the bind-1 matchall", kind)
		}
	}
	all := new(strings.Builder)
	for _, r := range rows {
		all.WriteString(r.Cmd + "\n")
		for _, p := range r.Pre {
			all.WriteString(p.Cmd + "\n")
		}
	}
	for _, s := range []string{"pedit ex munge ip ttl add 1", "munge udp sport", "retain 0xff00", "nocsum nofrag",
		"geneve_opts 0102:80:00880022,0103:81:00a000b0", "vxlan_opts", "erspan_opts 1:", "erspan_opts 2:",
		"use mark 7", "allow prio", "ife decode", "commit force", "ct clear", "nat src addr", "ct nat index 7",
		"helper ipv4-tcp-ftp", "sched-entry close 100000000ns -1 -1", "cycle-time-ext", "bpf bytecode",
		"classid 1:1", "da skip_hw", "police rate"} {
		if !strings.Contains(all.String(), s) {
			t.Errorf("no row or fixture has %q", s)
		}
	}
	// cBPF only: an eBPF object's info comes from bpf(2), which gotc refuses.
	if ebpf := regexp.MustCompile(`\b(obj|object-file|object-pinned|fd)\b`); ebpf.MatchString(all.String()) {
		t.Errorf("a row loads eBPF: %q", ebpf.FindString(all.String()))
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	for _, buf := range []*bytes.Buffer{&a, &b} {
		rows, err := matrixRows(families["tcaction2"](), matrixOpts{Seed: 1})
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
