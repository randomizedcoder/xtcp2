package main

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestEvFamily holds the ev rows to the plan's shape rules
// (goip-parity-events.md EC0-T02): one monitor-helper invocation per row,
// the canary rule (maddress named unless the groups are all), a floor for
// every row, and a trigger and at least one until-ERE, so the window
// closes on the monitor seeing its trigger's effect, not on the helper's
// exit.
func TestEvFamily(t *testing.T) {
	rows := evFamily()
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	// The quoted regions accept \\-escapes: the nsid rows' triggers carry
	// \\$n and \\" for the helper's inner shell (a row's cmd runs through
	// its own shell first, V381).
	ipmon := regexp.MustCompile(`^xtcp2-nl-ipmon c0 "((?:[^"\\]|\\.)*)" "((?:[^"\\]|\\.)*)"( "(.*)")?$`)
	for _, r := range rows {
		if r.Proto != "" {
			t.Errorf("%s: proto %q set; an event row captures on route", r.ID, r.Proto)
		}
		if !r.Serial {
			t.Errorf("%s: not serial; a concurrent row's netns churn answers the monitor's init probe EINVAL at random (V380)", r.ID)
		}
		if r.Floor < 1 {
			t.Errorf("%s: floor %d; an event row's pcap holds at least the canary", r.ID, r.Floor)
		}
		if r.Expect.RC == nil {
			t.Errorf("%s: no expected rc", r.ID)
		} else if got := *r.Expect.RC; got != 0 &&
			!(strings.HasPrefix(r.Desc, "negative:") && got == 1) {
			t.Errorf("%s: expect rc %d; a monitor that never subscribes is the helper's rc 1, a row to discard, and only a negative row's deliberate failure may expect 1", r.ID, got)
		}
		m := ipmon.FindStringSubmatch(r.Cmd)
		if m == nil {
			t.Errorf("%s: cmd %q is not one xtcp2-nl-ipmon invocation", r.ID, r.Cmd)
			continue
		}
		args, trigger, untils := m[1], m[2], m[4]
		if !strings.HasPrefix(args, "monitor ") {
			t.Errorf("%s: ip-args %q do not start a monitor", r.ID, args)
		}
		// The canary rule: maddress is named so the multicast join that
		// proves the subscription is one of the groups, unless the groups
		// are all, which has it already (any object named replaces all;
		// ip/ipmonitor.c's do_ipmonitor).
		if !strings.HasSuffix(args, " maddress") && args != "monitor all" {
			t.Errorf("%s: ip-args %q name no maddress; the canary's events would not print", r.ID, args)
		}
		if strings.TrimSpace(trigger) == "" {
			t.Errorf("%s: no trigger", r.ID)
		}
		if untils == "" {
			t.Errorf("%s: cmd %q carries no until-ERE", r.ID, r.Cmd)
		}
	}
}

// TestEvSeedsAreTheCorpusRows pins the ports: each seed row's command and
// floor equal the measured row it came from (the .exp corpus's row, and
// EC1's probe floors), so a change here is a decision, not a drift.
func TestEvSeedsAreTheCorpusRows(t *testing.T) {
	allCmd := `xtcp2-nl-ipmon c0 "monitor all" "` + evTriggerAll + `"`
	for _, re := range evUntilAll {
		allCmd += ` "` + re + `"`
	}
	cases := []struct {
		id, cmd, floor string
	}{
		{"ev/nsid-del", `xtcp2-nl-ipmon c0 "monitor nsid maddress" "ip netns add c1del && ip netns set c1del 7 && ip netns del c1del" "^nsid 7 " "^Deleted nsid 7 "`, "16"},
		{"ev/anycast-del", `xtcp2-nl-ipmon c0 "monitor acaddress maddress" "ip link set d0 up; ip -6 addr add 2001:db8:1::1/64 dev d0 nodad; ip -6 addr del 2001:db8:1::1/64 dev d0" "any 2001:db8:1:: " "Deleted .*any 2001:db8:1:: "`, "25"},
		{"ev/prefix", `xtcp2-nl-ipmon c0 "monitor prefix maddress" "` + evRASend + `" "prefix 2001:db8:5::/64"`, "11"},
		{"ev/monitor-all", allCmd, "48"},
	}
	byID := map[string]row{}
	for _, r := range evFamily() {
		byID[r.ID] = r
	}
	for _, c := range cases {
		r, ok := byID[c.id]
		if !ok {
			t.Errorf("%s: no such row", c.id)
			continue
		}
		if r.Cmd != c.cmd {
			t.Errorf("%s:\n got %q\nwant %q", c.id, r.Cmd, c.cmd)
		}
		if got := strconv.Itoa(r.Floor); got != c.floor {
			t.Errorf("%s: floor %s, want %s", c.id, got, c.floor)
		}
	}
}
