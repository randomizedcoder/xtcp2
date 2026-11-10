package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestV730Family holds both versions' rows to the probe batches (V017):
// the same ids and commands, and each version's rc.
func TestV730Family(t *testing.T) {
	at720, at730 := v730Family("7.2.0"), v730Family("7.3.0")
	for _, rows := range [][]row{at720, at730} {
		if err := checkRows(rows); err != nil {
			t.Fatal(err)
		}
	}
	if len(at720) != len(at730) {
		t.Fatalf("%d rows at 7.2.0, %d at 7.3.0", len(at720), len(at730))
	}
	by720, by730 := map[string]row{}, map[string]row{}
	for i := range at720 {
		if at720[i].ID != at730[i].ID || at720[i].Cmd != at730[i].Cmd {
			t.Errorf("row %d: %s %q at 7.2.0, %s %q at 7.3.0", i, at720[i].ID, at720[i].Cmd, at730[i].ID, at730[i].Cmd)
		}
		by720[at720[i].ID], by730[at730[i].ID] = at720[i], at730[i]
	}
	exp := func(e expect) string { return fmt.Sprintf("%d %s %s", *e.RC, e.RefusedBy, e.Errno) }
	cases := []struct {
		description string
		id          string
		expected    string // 7.2.0's rc, refuser and errno | 7.3.0's
	}{
		{"positive: lacp_strict on is new", "v730bond/on", "1 iproute2  | 0 none "},
		{"positive: a plain bond works at both", "v730bond/plain", "0 none  | 0 none "},
		{"negative: lacp_strict outside 802.3ad", "v730bond/active-backup", "1 iproute2  | 2 kernel EACCES"},
		{"negative: lacp_strict x", "v730bond/x", "1 iproute2  | 1 iproute2 "},
		{"positive: dst_port 4789 is new", "v730nh/4789", "255 iproute2  | 0 none "},
		{"boundary: dst_port 0", "v730nh/0", "255 iproute2  | 2 kernel ERANGE"},
		{"boundary: dst_port 65536", "v730nh/65536", "255 iproute2  | 255 iproute2 "},
		{"negative: dst_port without fdb", "v730nh/nofdb", "255 ack-then-fail  | 2 kernel EINVAL"},
		{"positive: seg6 lookup is new", "v730seg6/100", "255 iproute2  | 0 none "},
		{"boundary: seg6 lookup 0", "v730seg6/0", "255 iproute2  | 255 iproute2 "},
		{"positive: ovpn mode mp is new", "v730ovpn/mp", "1 iproute2  | 0 none "},
		{"positive: maddress show at both", "v730maddr/all", "0 none  | 0 none "},
		{"corner: maddress show of no such device", "v730maddr/nosuch", "0 none  | 0 kernel-ignored ENODEV"},
		{"negative: a non-hex octet at both", "v730lladdr/zz", "1 iproute2  | 1 iproute2 "},
		{"corner: a seven-octet neighbor address at both", "v730lladdr/neigh-seven", "0 none  | 0 none "},
		{"corner: macaddr set then bcqueuelen at both", "v730lladdr/macvlan-set", "0 none  | 0 none "},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			a, ok := by720[tc.id]
			if !ok {
				t.Fatalf("no row %s", tc.id)
			}
			if got := exp(a.Expect) + " | " + exp(by730[tc.id].Expect); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
	props := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: a bond row loads bonding, an ovpn row ovpn", func(r row) bool {
			return (strings.HasPrefix(r.ID, "v730bond/") == (len(r.Modules) == 1 && r.Modules[0] == "bonding")) &&
				(strings.HasPrefix(r.ID, "v730ovpn/") == (len(r.Modules) == 1 && r.Modules[0] == "ovpn")) ||
				len(r.Modules) == 0 && !strings.HasPrefix(r.ID, "v730bond/") && !strings.HasPrefix(r.ID, "v730ovpn/")
		}},
		{"positive: no setup step uses a 7.3.0 word", func(r row) bool {
			for _, p := range r.Pre {
				for _, w := range []string{"lacp_strict", "dst_port", "lookup", "mode p2p", "mode mp"} {
					if strings.Contains(p.Cmd, w) {
						return false
					}
				}
			}
			return true
		}},
		{"positive: a maddress row reads nothing back, as it is the show", func(r row) bool {
			return !strings.HasPrefix(r.ID, "v730maddr/") || len(r.Readback) == 0
		}},
		{"corner: the empty argument keeps its quotes", func(r row) bool {
			return r.ID != "v730lladdr/empty" || strings.HasSuffix(r.Cmd, " address ''")
		}},
		{"positive: a refusal's desc is negative, a success's positive", func(r row) bool {
			return strings.HasPrefix(r.Desc, "negative:") == (*r.Expect.RC != 0)
		}},
	}
	for _, p := range props {
		t.Run(p.description, func(t *testing.T) {
			for _, r := range append(at720, at730...) {
				if !p.check(r) {
					t.Errorf("%s: %s", r.ID, r.Cmd)
				}
			}
		})
	}
	if len(at730) != 42 {
		t.Errorf("%d rows, want 42", len(at730))
	}
}
