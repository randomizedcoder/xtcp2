//go:build linux && rdma && cgo && ionic_vm

package rdmaevents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestIonicVerbsEvents runs inside test-linkmonitor-ionic-vm against the
// in-tree ionic_rdma driver: each row changes the RDMA device's netdev and
// checks which async events the real verbs context delivers through Open and
// Run. PORT_ACTIVE and PORT_ERR both arrive as Refresh for port 1, and
// GID_CHANGE as Topology for port 1.
func TestIonicVerbsEvents(t *testing.T) {
	netdev, device := os.Getenv("LINKMONITOR_IONIC_NETDEV"), os.Getenv("LINKMONITOR_IONIC_RDMA")
	if netdev == "" || device == "" {
		t.Skip("LINKMONITOR_IONIC_NETDEV and LINKMONITOR_IONIC_RDMA are set by the ionic VM")
	}
	root := "/sys/class/infiniband"
	hardware, err := filepath.EvalSymlinks(filepath.Join(root, device, "device"))
	if err != nil {
		t.Fatal(err)
	}
	const dummy = "lmevdummy0"
	ip(t, "link", "add", dummy, "type", "dummy")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", dummy).Run() })
	t.Cleanup(func() {
		// Leave the port as the following VM steps expect it.
		_ = exec.Command("ip", "link", "set", netdev, "up").Run()
		_ = waitPortState(root, device, "ACTIVE", 30*time.Second)
	})

	ctx, cancel := context.WithCancel(t.Context())
	source, err := Open(ctx, NewProvider(root), []Identity{{Name: device, Hardware: hardware}})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	events := make(chan Event, Limit)
	done := make(chan error, 1)
	go func() {
		done <- source.Run(ctx, func(e Event) bool {
			select {
			case events <- e:
			default:
				t.Errorf("event buffer full; dropped %+v", e)
			}
			return true
		})
	}()
	t.Cleanup(func() {
		cancel()
		_ = source.Close()
		if err := <-done; err != nil && ctx.Err() == nil {
			t.Errorf("Run: %v", err)
		}
	})

	refresh := Event{Device: device, Port: 1, Kind: Refresh}
	topology := Event{Device: device, Port: 1, Kind: Topology}
	for _, row := range []struct {
		name, category, description, expected string
		commands                              [][]string
		// state is the RDMA port state to wait for after the commands.
		state string
		// want is the exact sequence of Refresh events, in order.
		want []Event
		// topology allows Topology events; needTopology requires one.
		// ionic_rdma keeps its GIDs across down and up, so link rows
		// allow none.
		topology, needTopology bool
	}{
		{"down", "positive", "administrative down of the ionic netdev", "one Refresh for port 1 (PORT_ERR)",
			[][]string{{"link", "set", netdev, "down"}}, "DOWN", []Event{refresh}, false, false},
		{"up", "positive", "administrative up of the ionic netdev", "one Refresh for port 1 (PORT_ACTIVE)",
			[][]string{{"link", "set", netdev, "up"}}, "ACTIVE", []Event{refresh}, false, false},
		{"gid", "positive", "an IPv6 address added to the ionic netdev", "a Topology for port 1 (GID_CHANGE) and no Refresh",
			[][]string{{"-6", "addr", "add", "2001:db8:1::1/64", "dev", netdev, "nodad"}}, "ACTIVE", nil, true, true},
		{"gid-del", "positive", "the same IPv6 address removed", "a Topology for port 1 (GID_CHANGE) and no Refresh",
			[][]string{{"-6", "addr", "del", "2001:db8:1::1/64", "dev", netdev}}, "ACTIVE", nil, true, true},
		{"unrelated", "negative", "down and up of an unrelated dummy netdev", "no event for the ionic device",
			[][]string{{"link", "set", dummy, "up"}, {"link", "set", dummy, "down"}}, "ACTIVE", nil, false, false},
		{"flap", "boundary", "down then up with no pause between them", "two Refresh for port 1, in order",
			[][]string{{"link", "set", netdev, "down"}, {"link", "set", netdev, "up"}}, "ACTIVE", []Event{refresh, refresh}, false, false},
		{"down-again", "corner", "a second down while already down", "exactly one Refresh: the core only reports a state change",
			[][]string{{"link", "set", netdev, "down"}, {"link", "set", netdev, "down"}}, "DOWN", []Event{refresh}, false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			drain(events, 500*time.Millisecond)
			for _, command := range row.commands {
				ip(t, command...)
			}
			if err := waitPortState(root, device, row.state, 30*time.Second); err != nil {
				t.Fatal(err)
			}
			got := collect(events, 2*time.Second)
			t.Logf("observed %v", got)
			var refreshes []Event
			topologies := 0
			for _, e := range got {
				switch {
				case e == refresh:
					refreshes = append(refreshes, e)
				case e == topology && row.topology:
					topologies++
				default:
					t.Errorf("unexpected event %+v", e)
				}
			}
			if !slices.Equal(refreshes, row.want) {
				t.Errorf("Refresh events %v, want %v", refreshes, row.want)
			}
			if row.needTopology && topologies == 0 {
				t.Error("no Topology event")
			}
		})
	}
}

func ip(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// waitPortState waits for ports/1/state to read "N: STATE".
func waitPortState(root, device, state string, timeout time.Duration) error {
	path := filepath.Join(root, device, "ports", "1", "state")
	var text string
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text = strings.TrimSpace(string(b))
		if _, name, _ := strings.Cut(text, ": "); name == state {
			return nil
		}
	}
	return fmt.Errorf("%s is %q after %v, want %s", path, text, timeout, state)
}

// drain discards events until none has arrived for quiet.
func drain(events <-chan Event, quiet time.Duration) {
	collect(events, quiet)
}

// collect returns the events delivered until none has arrived for quiet.
func collect(events <-chan Event, quiet time.Duration) []Event {
	var got []Event
	for {
		select {
		case e := <-events:
			got = append(got, e)
		case <-time.After(quiet):
			return got
		}
	}
}
