package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/rtnlmon"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// go test ./tools/rtnlmon-probe/ -run TestProbeOutput
func TestProbeOutput(t *testing.T) {
	v4 := []byte{192, 0, 2, 1}
	for _, row := range []struct {
		description string
		event       any
		snapshot    *rtnlmon.Snapshot
		reason      rtnlmon.Reason
		expected    string
	}{
		{
			description: "positive: a link event",
			event:       xtcpnl.LinkEvent{Action: xtcpnl.EventActionAdd, Link: xtcpnl.LinkInfo{Index: 5, Name: "rtnl0", Flags: unix.IFF_UP}},
			expected:    "RTNLMON_EVENT link add ifindex=5 ifname=rtnl0 up\n",
		},
		{
			description: "positive: an address event prints IFA_LOCAL",
			event:       xtcpnl.AddrEvent{Action: xtcpnl.EventActionDel, Addr: xtcpnl.AddrInfo{Family: unix.AF_INET, Prefixlen: 32, Index: 5, Local: v4, Address: []byte{10, 0, 0, 1}}},
			expected:    "RTNLMON_EVENT addr del ifindex=5 addr=192.0.2.1/32\n",
		},
		{
			description: "positive: a snapshot",
			snapshot:    &rtnlmon.Snapshot{Links: make([]xtcpnl.LinkInfo, 2), Addrs: []xtcpnl.AddrInfo{{Family: unix.AF_INET, Prefixlen: 24, Index: 1, Address: v4}}},
			reason:      rtnlmon.ReasonOverrun,
			expected:    "RTNLMON_SNAPSHOT overrun links=2 addrs=1 routes=0 neighs=0\nRTNLMON_SNAPSHOT_ADDR overrun ifindex=1 addr=192.0.2.1/24\n",
		},
		{
			description: "boundary: a default route",
			event:       xtcpnl.RouteEvent{Action: xtcpnl.EventActionAdd, Route: xtcpnl.RouteInfo{Family: unix.AF_INET6, Table: 254, Oif: 2}},
			expected:    "RTNLMON_EVENT route add table=254 dst=::/0 oif=2\n",
		},
		{
			description: "corner: a link that is down",
			event:       xtcpnl.LinkEvent{Action: xtcpnl.EventActionDel, Link: xtcpnl.LinkInfo{Index: 5, Name: "rtnl0"}},
			expected:    "RTNLMON_EVENT link del ifindex=5 ifname=rtnl0 down\n",
		},
		{
			description: "corner: a neighbor",
			event:       xtcpnl.NeighEvent{Action: xtcpnl.EventActionAdd, Neigh: xtcpnl.NeighInfo{Family: unix.AF_INET, Ifindex: 3, Dst: v4}},
			expected:    "RTNLMON_EVENT neigh add ifindex=3 dst=192.0.2.1\n",
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			var out bytes.Buffer
			p := &printer{w: &out}
			var err error
			if row.snapshot != nil {
				err = p.Snapshot(*row.snapshot, row.reason)
			} else {
				err = p.Event(row.event)
			}
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != row.expected {
				t.Fatalf("%q, expected %q", out.String(), row.expected)
			}
		})
	}
}

// go test ./tools/rtnlmon-probe/ -run TestPrinterControl
func TestPrinterControl(t *testing.T) {
	link := xtcpnl.LinkEvent{Action: xtcpnl.EventActionAdd}
	t.Run("positive: -count ends Run after N events", func(t *testing.T) {
		p := &printer{w: &bytes.Buffer{}, count: 2}
		if err := p.Event(link); err != nil {
			t.Fatal(err)
		}
		if err := p.Event(link); !errors.Is(err, errCountReached) {
			t.Fatalf("err=%v, expected errCountReached", err)
		}
	})
	t.Run("positive: the ready file is written on the start snapshot only", func(t *testing.T) {
		ready := filepath.Join(t.TempDir(), "ready")
		p := &printer{w: &bytes.Buffer{}, ready: ready}
		if err := p.Snapshot(rtnlmon.Snapshot{}, rtnlmon.ReasonOverrun); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(ready); err == nil {
			t.Fatal("an overrun snapshot wrote the ready file")
		}
		if err := p.Snapshot(rtnlmon.Snapshot{}, rtnlmon.ReasonStart); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(ready); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("corner: the pause file holds the Handler until it is removed", func(t *testing.T) {
		pause := filepath.Join(t.TempDir(), "pause")
		if err := os.WriteFile(pause, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		p := &printer{w: &bytes.Buffer{}, pause: pause, poll: time.Millisecond}
		done := make(chan error, 1)
		go func() { done <- p.Event(link) }()
		select {
		case <-done:
			t.Fatal("Event returned while paused")
		case <-time.After(30 * time.Millisecond):
		}
		if err := os.Remove(pause); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

// go test ./tools/rtnlmon-probe/ -run TestRunFlags
func TestRunFlags(t *testing.T) {
	for _, row := range []struct {
		description string
		args        []string
		expected    int
		stderr      string
	}{
		{description: "negative: unknown group", args: []string{"-groups", "frob"}, expected: 1, stderr: `unknown group "frob"`},
		{description: "negative: an unknown flag", args: []string{"-frob"}, expected: 1, stderr: "flag provided but not defined"},
		{description: "boundary: an empty group list", args: []string{"-groups", ""}, expected: 1, stderr: `unknown group ""`},
		{description: "corner: help", args: []string{"-h"}, expected: 0, stderr: "-groups"},
	} {
		t.Run(row.description, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if rc := run(row.args, &stdout, &stderr); rc != row.expected {
				t.Fatalf("rc=%d, expected %d; stderr %q", rc, row.expected, stderr.String())
			}
			if !strings.Contains(stderr.String(), row.stderr) {
				t.Fatalf("stderr %q lacks %q", stderr.String(), row.stderr)
			}
		})
	}
}

// go test ./tools/rtnlmon-probe/ -run TestParseGroups
func TestParseGroups(t *testing.T) {
	for _, row := range []struct {
		description string
		in          string
		expected    []rtnlmon.Group
	}{
		{description: "positive: link and both address families", in: "link,addr", expected: []rtnlmon.Group{rtnlmon.GroupLink, rtnlmon.GroupIPv4Addr, rtnlmon.GroupIPv6Addr}},
		{description: "positive: one family", in: "route6", expected: []rtnlmon.Group{rtnlmon.GroupIPv6Route}},
		{description: "boundary: spaces around words", in: " neigh , link ", expected: []rtnlmon.Group{rtnlmon.GroupNeigh, rtnlmon.GroupLink}},
		{description: "corner: a trailing comma is an empty word", in: "link,", expected: nil},
	} {
		t.Run(row.description, func(t *testing.T) {
			got, err := parseGroups(row.in)
			if row.expected == nil {
				if err == nil {
					t.Fatalf("%v, expected an error", got)
				}
				return
			}
			if err != nil || len(got) != len(row.expected) {
				t.Fatalf("%v %v, expected %v", got, err, row.expected)
			}
			for i := range got {
				if got[i] != row.expected[i] {
					t.Fatalf("%v, expected %v", got, row.expected)
				}
			}
		})
	}
}
