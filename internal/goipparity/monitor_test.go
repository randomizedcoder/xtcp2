//go:build goip_write

package goipparity

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The monitor comparison's rows (EG1-T01). The texts are the shapes the
// corpus's ev rows print — the carrier row's own .out is the model — with
// the pinned MACs the rows' Setup writes, so the MAC facet is stable here
// the way it is on a run.

// monJoinBlock is the canary join's block, print_addrinfo's shape.
const monJoinBlock = "3: c0    inet mcast 224.0.0.250 scope global \n" +
	"       valid_lft forever preferred_lft forever\n"

// monDeletedBlock is the canary's deletion's block, print_addrinfo's
// `Deleted` shape — one block, the .out's retry shape's first.
const monDeletedBlock = "Deleted 3: c0    inet mcast 224.0.0.250 scope global \n" +
	"       valid_lft forever preferred_lft forever\n"

// monText renders one side's monitor output: `retries` canary
// deletions, the canary join (the block the helper's loop broke on, so the
// LAST canary block the race ever prints), then the trigger's events with
// the first post-up stanza in `upState` — "UP" for the settled shape,
// "LOWERLAYERDOWN" for the carrier transient the plan's noise class 3
// names, whose own flag set carries NO-CARRIER as the measured shape does.
// The retry's own order is the V391 record's and the live run's: the
// deletion the never-printed join's deletion prints, then the re-join
// the loop's next iteration prints, the join last.
func monText(upState string, retries int) string {
	var b strings.Builder
	for i := 0; i < retries; i++ {
		b.WriteString(monDeletedBlock)
	}
	b.WriteString(monJoinBlock)
	b.WriteString("4: r0p@r0: <BROADCAST,MULTICAST> mtu 1500 qdisc noqueue " +
		"state DOWN group default \n" +
		"    link/ether 02:00:00:00:10:01 brd ff:ff:ff:ff:ff:ff\n")
	b.WriteString("Deleted 4: r0p    inet mcast 224.0.0.1 scope global \n" +
		"       valid_lft forever preferred_lft forever\n")
	b.WriteString("5: r0@r0p: <NO-CARRIER,BROADCAST,MULTICAST,UP,M-DOWN> " +
		"mtu 1500 qdisc noqueue state LOWERLAYERDOWN group default \n" +
		"    link/ether 02:00:00:00:10:00 brd ff:ff:ff:ff:ff:ff\n")
	upFlags := "<BROADCAST,MULTICAST,UP,LOWER_UP>"
	if upState == "LOWERLAYERDOWN" {
		upFlags = "<NO-CARRIER,BROADCAST,MULTICAST,UP,M-DOWN>"
	}
	b.WriteString("4: r0p@r0: " + upFlags + " mtu 1500 " +
		"qdisc noqueue state " + upState + " group default \n" +
		"    link/ether 02:00:00:00:10:01 brd ff:ff:ff:ff:ff:ff\n")
	return b.String()
}

// TestCollapseCanary holds the retry's collapse to its rule: the join stays,
// the twins go, everything else is untouched.
func TestCollapseCanary(t *testing.T) {
	tests := []struct {
		description string
		in, want    string
	}{
		{
			description: "positive: the join the loop broke on stays as it is",
			in:          monText("UP", 0),
			want:        monText("UP", 0),
		},
		{
			description: "positive: the retry's deletion before the join collapses",
			in:          monText("UP", 1),
			want:        monText("UP", 0),
		},
		{
			description: "positive: two retries collapse to the one join",
			in:          monText("LOWERLAYERDOWN", 2),
			want:        monText("LOWERLAYERDOWN", 0),
		},
		{
			description: "corner: the Deleted prefix alone is not the canary's — " +
				"the trigger's own Deleted block stays",
			in: "Deleted 4: r0p    inet mcast 224.0.0.1 scope global \n" +
				"       valid_lft forever preferred_lft forever\n",
			want: "Deleted 4: r0p    inet mcast 224.0.0.1 scope global \n" +
				"       valid_lft forever preferred_lft forever\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := collapseCanary(tt.in); got != tt.want {
				t.Errorf("collapseCanary:\ngot  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestCompareMonitorWithControl holds the monitor stdout comparison's rules
// over the three noise classes: the retry collapsed, the pinned MACs
// compared as they stand, the transient bounded by the controls' own
// disagreement.
func TestCompareMonitorWithControl(t *testing.T) {
	tests := []struct {
		description       string
		ipA, goipTxt, ipB string
		wantFindings      int
	}{
		{
			description: "positive: three identical sides compare clean, canary and all",
			ipA:         monText("UP", 0),
			goipTxt:     monText("UP", 0),
			ipB:         monText("UP", 0),
		},
		{
			description: "positive: the canary retry, on whichever side the race hit " +
				"it, is not a finding",
			ipA:     monText("UP", 0),
			goipTxt: monText("UP", 1),
			ipB:     monText("UP", 2),
		},
		{
			description: "corner: the carrier transient inside the controls' own " +
				"disagreement is D_control, subtracted",
			ipA:     monText("LOWERLAYERDOWN", 0),
			goipTxt: monText("UP", 0),
			ipB:     monText("UP", 0),
		},
		{
			description: "corner: the transient outside the controls' agreement " +
				"is the finding the plan accepts — the goip row's shape " +
				"must lie inside the controls' disagreement",
			ipA:          monText("UP", 0),
			goipTxt:      monText("LOWERLAYERDOWN", 0),
			ipB:          monText("UP", 0),
			wantFindings: 1,
		},
		{
			description: "negative: a stanza goip prints that ip does not is a " +
				"presence finding, unsuppressible",
			ipA: monText("UP", 0),
			goipTxt: monText("UP", 0) + "5: r0@r0p: <BROADCAST,MULTICAST,UP,LOWER_UP> " +
				"mtu 1500 qdisc noqueue state UP group default \n" +
				"    link/ether 02:00:00:00:10:00 brd ff:ff:ff:ff:ff:ff\n",
			ipB: monText("UP", 0),
			// Nine facets move together: the line count, the ifname and the
			// ifindex, the MAC set (the stanza's own address and the brd), and
			// the mtu, qdisc, group, brd and state keywords. One finding per
			// facet is the facet machinery's own rule - a finding names a
			// facet's whole symmetric difference, not one element of it.
			wantFindings: 9,
		},
		{
			description: "negative: a MAC the row did not pin is the finding the " +
				"pin exists to make catchable",
			ipA: monText("UP", 0),
			goipTxt: strings.ReplaceAll(monText("UP", 0), "02:00:00:00:10:01",
				"02:00:00:00:20:01"),
			ipB:          monText("UP", 0),
			wantFindings: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			r := CompareMonitorWithControl("monitor link maddress carrier",
				tt.ipA, tt.goipTxt, tt.ipB, nil)
			if len(r.Findings) != tt.wantFindings {
				t.Errorf("Findings = %d, want %d:\n", len(r.Findings), tt.wantFindings)
				for _, d := range r.Findings {
					t.Errorf("  %s", d)
				}
			}
			if !r.Compared {
				t.Errorf("Compared = false, want true")
			}
		})
	}
}

// writeMonRow writes one monitor row's nine files into dir: each side's
// .out, .rc and .err. The refusal rows' .err holds the trigger's own
// complaint, which is not the helper's and must not fail the floor.
func writeMonRow(t *testing.T, dir string, c Command, out, errText, rc string) {
	t.Helper()
	for _, side := range Sides() {
		if err := os.WriteFile(filepath.Join(dir, c.StdoutName(side)),
			[]byte(out), 0o600); err != nil {
			t.Fatalf("%s: %v", c.StdoutName(side), err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.RCName(side)),
			[]byte(rc), 0o600); err != nil {
			t.Fatalf("%s: %v", c.RCName(side), err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.ErrName(side)),
			[]byte(errText), 0o600); err != nil {
			t.Fatalf("%s: %v", c.ErrName(side), err)
		}
	}
}

// writeCleanMonitorRow writes one monitor row's clean nine files: the
// three sides' identical settled stdout, an empty .err and rc 0 — the row
// TestRender's "every implemented command clean" setup writes for the
// table's monitor rows, the write rows' writeCleanWriteRow shape.
func writeCleanMonitorRow(t *testing.T, dir string, c Command) {
	t.Helper()
	writeMonRow(t, dir, c, monText("UP", 0), "", "0")
}

// monitorRowFor finds the table's carrier row, the EG1-T01 probe's own.
func monitorRowFor(t *testing.T) Command {
	t.Helper()
	c, err := Lookup("monitor link maddress carrier")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	return c
}

// TestCompareMonitorRow holds the floor: the helper's own complaint fails
// the row on any side, the three rcs must agree, and the agreement — not the
// value — is the rule, which is what makes the refusal rows legal.
func TestCompareMonitorRow(t *testing.T) {
	row := monitorRowFor(t)

	t.Run("positive: a clean row passes and names the agreed rc", func(t *testing.T) {
		dir := t.TempDir()
		writeMonRow(t, dir, row, monText("UP", 0), "", "0")
		r := compareMonitorRow(dir, row, nil)
		if r.Status != StatusPass {
			t.Fatalf("Status = %s (%v), want PASS", r.Status, r.Err)
		}
		if r.MonitorRC != "0" {
			t.Errorf("MonitorRC = %q, want 0", r.MonitorRC)
		}
		if len(r.Stdout.Findings) != 0 {
			for _, d := range r.Stdout.Findings {
				t.Errorf("finding: %s", d)
			}
		}
	})

	t.Run("positive: the refusal row's rc 1 on every side is the row's own "+
		"expect, not the comparator's to fail",
		func(t *testing.T) {
			dir := t.TempDir()
			// The trigger's own complaint is not the helper's: the refusal
			// rows land it in the same .err, and the floor must not read it.
			writeMonRow(t, dir, row, monJoinBlock,
				"ip route del 192.0.2.128/32 dev c0: RTNETLINK answers: "+errESRCHCst+"\n",
				"1")
			r := compareMonitorRow(dir, row, nil)
			if r.Status != StatusPass {
				t.Fatalf("Status = %s (%v), want PASS", r.Status, r.Err)
			}
			if r.MonitorRC != "1" {
				t.Errorf("MonitorRC = %q, want 1", r.MonitorRC)
			}
		})

	t.Run("negative: the helper's own complaint is the floor, and fails the "+
		"row naming the side",
		func(t *testing.T) {
			dir := t.TempDir()
			writeMonRow(t, dir, row, monText("UP", 0), "", "0")
			errPath := filepath.Join(dir, row.ErrName(SideGoip))
			if err := os.WriteFile(errPath,
				[]byte("xtcp2-nl-ipmon: no match for r0@r0p: .*state UP\n"), 0o600); err != nil {
				t.Fatalf("%s: %v", errPath, err)
			}
			r := compareMonitorRow(dir, row, nil)
			if r.Status != StatusFail {
				t.Fatalf("Status = %s, want FAIL", r.Status)
			}
			if !strings.Contains(r.Err.Error(), "no match for") {
				t.Errorf("Err = %v, want it naming the complaint", r.Err)
			}
		})

	t.Run("negative: the three sides' rc must agree, whatever the value",
		func(t *testing.T) {
			dir := t.TempDir()
			writeMonRow(t, dir, row, monJoinBlock, "", "0")
			rcPath := filepath.Join(dir, row.RCName(SideGoip))
			if err := os.WriteFile(rcPath, []byte("1"), 0o600); err != nil {
				t.Fatalf("%s: %v", rcPath, err)
			}
			r := compareMonitorRow(dir, row, nil)
			if r.Status != StatusFail {
				t.Fatalf("Status = %s, want FAIL", r.Status)
			}
			if !strings.Contains(r.Err.Error(), "goip=1") {
				t.Errorf("Err = %v, want it naming the sides' rcs", r.Err)
			}
		})

	t.Run("negative: a missing file is MISSING, named", func(t *testing.T) {
		dir := t.TempDir()
		// No files at all: the whole expected set is absent.
		r := compareOne(dir, row, nil)
		if r.Status != StatusMissing {
			t.Fatalf("Status = %s, want MISSING", r.Status)
		}
		want := 3 * 3 // .out, .rc and .err per side, none of them present.
		if len(r.Absent) != want {
			t.Errorf("Absent = %v, want %d names", r.Absent, want)
		}
	})

	t.Run("corner: a gated monitor row with a finding fails, the dump rows' "+
		"own gating rule",
		func(t *testing.T) {
			dir := t.TempDir()
			writeMonRow(t, dir, row, monText("UP", 0), "", "0")
			goipPath := filepath.Join(dir, row.StdoutName(SideGoip))
			if err := os.WriteFile(goipPath,
				[]byte(monText("LOWERLAYERDOWN", 0)), 0o600); err != nil {
				t.Fatalf("%s: %v", goipPath, err)
			}
			al, err := nlparity.LoadAllowlist([]byte(fmt.Sprintf(`{
  "_comment": ["test fixture: internal/goiparity/monitor_test.go"],
  "entries": [],
  "gated_commands": [%q]
}`, row.Name)))
			if err != nil {
				t.Fatalf("LoadAllowlist: %v", err)
			}
			r := compareMonitorRow(dir, row, al)
			if r.Status != StatusFail {
				t.Fatalf("Status = %s, want FAIL (gated, a finding)", r.Status)
			}
		})
}

// errESRCHCst is the kernel's ESRCH spelling, as `ip` renders it on a
// route delete that meets nothing.
const errESRCHCst = "No such process"
