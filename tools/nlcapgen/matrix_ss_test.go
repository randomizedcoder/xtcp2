package main

import (
	"strings"
	"testing"
)

// ssPick builds an ss row from value names by axis, the other axes taking
// their first value.
func ssPick(kv ...string) row {
	sp := ssSpace()
	names := f(kv...)
	return sp.Build(sp.assign(names))
}

func TestBuildSs(t *testing.T) {
	tests := []struct {
		description string
		pick        []string
		cmd         string
		states      string
		pred        string
		bug         string
	}{
		{description: "positive: the default tcp row", pick: nil, cmd: "ss -n -t", states: "15159"},
		{description: "positive: udp, both families, keeps only SS_ESTABLISHED", pick: []string{"proto", "udp"},
			cmd: "ss -n -u", states: "2"},
		{description: "positive: raw, both families, keeps only SS_ESTABLISHED", pick: []string{"proto", "raw"},
			cmd: "ss -n -w", states: "2"},
		{description: "boundary: udp with -4 ORs in the family's SS_CONN", pick: []string{"proto", "udp", "family", "4"},
			cmd: "ss -n -u -4", states: "15159"},
		{description: "positive: a state option replaces the defaults", pick: []string{"proto", "udp", "state", "all"},
			cmd: "ss -n -u -a", states: "16383"},
		{description: "positive: -l is LISTEN and CLOSE", pick: []string{"state", "listen"},
			cmd: "ss -n -t -l", states: "1152"},
		{description: "positive: options, then states, then the filter", pick: []string{"ext", "mem", "state", "established", "filter", "or"},
			cmd: "ss -n -t -m state established sport = :22 or sport = :80", states: "2", pred: "or(sport=22,sport=80)"},
		{description: "corner: a cgroup filter mounts cgroup2 first", pick: []string{"filter", "cgroup"},
			cmd: "mount -t cgroup2 cgroup2 /sys/fs/cgroup && ss -n -t cgroup /sys/fs/cgroup", states: "15159", pred: "cgroup=/sys/fs/cgroup"},
		{description: "corner: localhost over both families shows the bug", pick: []string{"filter", "localhost"},
			cmd: "ss -n -t src localhost", states: "15159", pred: "src=localhost", bug: "ss-scond-first-addr"},
		{description: "corner: localhost with -4 is one address, no bug", pick: []string{"family", "4", "filter", "localhost"},
			cmd: "ss -n -t -4 src localhost", states: "15159", pred: "src=127.0.0.1/32"},
		{description: "corner: localhost with -6", pick: []string{"family", "6", "filter", "localhost"},
			cmd: "ss -n -t -6 src localhost", states: "15159", pred: "src=::1/128"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := ssPick(tc.pick...)
			if r.Cmd != tc.cmd || r.Axes["states"] != tc.states || r.Axes["pred"] != tc.pred || r.Axes["bug"] != tc.bug {
				t.Errorf("got %q states %s pred %q bug %q\nwant %q states %s pred %q bug %q",
					r.Cmd, r.Axes["states"], r.Axes["pred"], r.Axes["bug"], tc.cmd, tc.states, tc.pred, tc.bug)
			}
			if r.Proto != "sock_diag" || len(r.Pre) != 1 || r.Pre[0].Cmd != ssSetup || len(r.Modules) != len(ssModules) {
				t.Errorf("proto %q, pre %v, modules %v", r.Proto, r.Pre, r.Modules)
			}
		})
	}
}

func TestSsFamilyExcl(t *testing.T) {
	sp := ssSpace()
	excl := ssFamilyExcl()
	if len(excl) != 9 {
		t.Fatalf("%d exclusions, want 9", len(excl))
	}
	for i, e := range excl {
		if _, err := sp.pair(e); err != nil {
			t.Errorf("exclusion %d: %v", i, err)
		}
		if i > 0 && excl[i-1].A+excl[i-1].B >= e.A+e.B {
			t.Errorf("exclusion %d out of order: %s %s", i, e.A, e.B)
		}
		if e.Expect == nil || *e.Expect.RC != 1 || e.Expect.RefusedBy != "iproute2" {
			t.Errorf("exclusion %d: expect %+v", i, e.Expect)
		}
	}
}

func TestRowValidateProto(t *testing.T) {
	tests := []struct {
		description string
		proto       string
		expected    bool // valid
	}{
		{description: "positive: route by default", proto: "", expected: true},
		{description: "positive: sock_diag", proto: "sock_diag", expected: true},
		{description: "boundary: route named", proto: "route", expected: true},
		{description: "negative: an unknown protocol", proto: "inet_diag"},
		{description: "corner: the case matters", proto: "SOCK_DIAG"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := row{ID: "ss/x", Desc: "positive: x", Cmd: "ss", Proto: tc.proto}
			if err := r.validate(); (err == nil) != tc.expected {
				t.Errorf("err %v, want valid %v", err, tc.expected)
			}
		})
	}
}

func TestMatrixSsRows(t *testing.T) {
	rows, err := matrixRows(families["ss"](), matrixOpts{Seed: 1, Random: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	v6Filter := map[string]bool{"[::1]": true, "[2001:db8::/32]": true, "[::/0]": true}
	cases := []struct {
		description string
		check       func(r *row) bool
	}{
		{"positive: every row runs ss -n on sock_diag", func(r *row) bool {
			return strings.Contains(r.Cmd, "ss -n ") && r.Proto == "sock_diag" && strings.HasPrefix(r.ID, "ss/")
		}},
		{"positive: every refusal is ss's own, exit 1 or 255", func(r *row) bool {
			rc := *r.Expect.RC
			return rc == 0 || ((rc == 1 || rc == 255) && r.Expect.RefusedBy == "iproute2")
		}},
		{"positive: every accepted row carries the facts the oracle reads", func(r *row) bool {
			return *r.Expect.RC != 0 || (r.Axes["proto"] != "" && r.Axes["families"] != "" && r.Axes["states"] != "" && r.Axes["ext"] != "")
		}},
		{"negative: no accepted -4 row filters on an IPv6 address", func(r *row) bool {
			if *r.Expect.RC != 0 || r.Axes["families"] != "2" {
				return true
			}
			for a := range v6Filter {
				if strings.Contains(r.Cmd, a) {
					return false
				}
			}
			return true
		}},
		{"corner: a bug fact only on a both-family localhost row or a port bug", func(r *row) bool {
			switch r.Axes["bug"] {
			case "", "ss-port-u16-wrap", "ss-port-minus-one-any":
				return true
			case "ss-scond-first-addr":
				return r.Axes["families"] == "2,10"
			}
			return false
		}},
		{"corner: every cgroup row mounts cgroup2 first", func(r *row) bool {
			return !strings.Contains(r.Cmd, " cgroup ") || strings.HasPrefix(r.Cmd, "mount -t cgroup2 ")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for i := range rows {
				if !tc.check(&rows[i]) {
					t.Errorf("%s: %s", rows[i].ID, rows[i].Cmd)
				}
			}
		})
	}
}
