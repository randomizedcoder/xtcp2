//go:build linux && (rdma_vm || ionic_vm)

package linkmonitor

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmanative"
	"golang.org/x/sys/unix"
)

// The recorded event test runs in test-linkmonitor-rdma-vm against rxe and in
// test-linkmonitor-ionic-vm against ionic_rdma. It opens the monitor's own
// RDMA event source, openRDMAEvents (the netlink port source joined with the
// NLDEV monitor), and checks what it delivers for each trigger against what
// the cgo verbs backend delivered for the same trigger in R1-T05's
// differential runs (GO-NATIVE-RDMA-STATUS.md RV043-RV049), before R4-T02
// removed it.
//
// The guest sets LINKMONITOR_EVENTS_NETDEV and LINKMONITOR_EVENTS_RDMA, and
// optionally LINKMONITOR_EVENTS_PEER (a veth peer whose down drops the
// netdev's carrier), LINKMONITOR_EVENTS_REMOVE (a shell command that removes
// the device while it is held) and LINKMONITOR_EVENTS_FAULTS=1.

type eventsFixture struct {
	netdev, device, peer string
	ifindex              uint32
	set                  rdmaEventSet
}

func eventsEnv(t *testing.T) eventsFixture {
	t.Helper()
	f := eventsFixture{
		netdev: os.Getenv("LINKMONITOR_EVENTS_NETDEV"),
		device: os.Getenv("LINKMONITOR_EVENTS_RDMA"),
		peer:   os.Getenv("LINKMONITOR_EVENTS_PEER"),
	}
	if f.netdev == "" || f.device == "" {
		t.Fatal("LINKMONITOR_EVENTS_NETDEV and LINKMONITOR_EVENTS_RDMA are required")
	}
	iface, err := net.InterfaceByName(f.netdev)
	if err != nil {
		t.Fatal(err)
	}
	f.ifindex = uint32(iface.Index)
	c, err := linuxio.NewClient(unix.NETLINK_RDMA)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := c.DumpRDMADevices(t.Context())
	if err := errors.Join(err, c.Close()); err != nil {
		t.Fatal(err)
	}
	// The kernel's first RDMA device has NLDEV index 0.
	var index uint32
	found := false
	for _, d := range devices.Values {
		if d.Name == f.device {
			index, found = d.Index, true
		}
	}
	if !found {
		t.Fatalf("no NLDEV index for %s", f.device)
	}
	ports, err := rdmanative.NewPortMap([]rdmanative.Binding{{Device: f.device, Port: 1, Netdev: f.ifindex, DevIndex: index, HasDevIndex: true}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.set = rdmaEventSet{ports: ports}
	return f
}

// eventSide is a running source and what it has delivered.
type eventSide struct {
	events chan rdmaevents.Event
	done   chan error
}

func openSide(t *testing.T, f eventsFixture) (*eventSide, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := &lifecycleSession{}
	source, err := s.openRDMAEvents(ctx, f.set)
	if err != nil {
		cancel()
		return nil, err
	}
	side := &eventSide{events: make(chan rdmaevents.Event, rdmaevents.Limit), done: make(chan error, 1)}
	go func() {
		side.done <- source.Run(ctx, func(e rdmaevents.Event) bool {
			select {
			case side.events <- e:
			default:
				t.Errorf("event buffer full; dropped %+v", e)
			}
			return true
		})
	}()
	t.Cleanup(func() {
		cancel()
		if err := errors.Join(source.Close(), ignoreCanceled(<-side.done)); err != nil {
			t.Error(err)
		}
	})
	return side, nil
}

func mustOpenSide(t *testing.T, f eventsFixture) *eventSide {
	t.Helper()
	side, err := openSide(t, f)
	if err != nil {
		t.Fatal(err)
	}
	return side
}

// ignoreCanceled keeps the errors a closed source reports for itself.
func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, rdmaevents.ErrLost) {
		return nil
	}
	return err
}

// tally is a row's events, by what the schedule does with them.
type tally struct {
	refresh, topology, hints, fatal int
	other                           []rdmaevents.Event
}

func (got *tally) add(device string, e rdmaevents.Event) {
	switch {
	case e.Kind == rdmaevents.Topology && e.Device == "":
		got.hints++ // an NLDEV notification: a resync hint, not a port event
	case e.Device != device:
		got.other = append(got.other, e)
	case e.Kind == rdmaevents.Refresh && e.Port == 1:
		got.refresh++
	case e.Kind == rdmaevents.Topology && e.Port == 1:
		got.topology++
	case e.Kind == rdmaevents.Fatal:
		got.fatal++
	default:
		got.other = append(got.other, e)
	}
}

// collect takes every event already delivered, then waits until none arrives
// for quiet. Queued events always win over the timer, so quiet 0 is a drain.
func (s *eventSide) collect(device string, quiet time.Duration) tally {
	var got tally
	for {
		select {
		case e := <-s.events:
			got.add(device, e)
			continue
		default:
		}
		select {
		case e := <-s.events:
			got.add(device, e)
		case <-time.After(quiet):
			if len(s.events) == 0 {
				return got
			}
		}
	}
}

// running fails the test if the source has returned.
func (s *eventSide) running(t *testing.T) {
	t.Helper()
	select {
	case err := <-s.done:
		s.done <- err
		t.Fatalf("source ended: %v", err)
	default:
	}
}

// eventRow is one trigger and what the source must deliver for it: what the
// verbs backend delivered for it in R1-T05, where the two agreed.
type eventRow struct {
	name, category, description, expected string
	commands                              [][]string
	state                                 string
	// refresh is the exact Refresh count for port 1. Verbs gave the same, or
	// twice as many on kernels where rxe dispatches PORT_ACTIVE from both the
	// driver and the core (rxe_dup).
	refresh int
	// topology requires a Topology, as verbs' GID_CHANGE; quiet allows none.
	// Otherwise a Topology is allowed: an address change on the netdev can
	// come with a link change (rxe's IPv6 link-local on a flap) that verbs'
	// GID cache absorbed. An extra Topology is a resync, never a missed change.
	topology, quiet bool
}

func (r eventRow) check(got tally) error {
	var errs []error
	if len(got.other) > 0 {
		errs = append(errs, errors.New("events for another device or port"))
	}
	if got.refresh != r.refresh {
		errs = append(errs, errors.New("Refresh count differs from the row"))
	}
	if r.topology && got.topology == 0 {
		errs = append(errs, errors.New("no Topology"))
	}
	if r.quiet && got.topology > 0 {
		errs = append(errs, errors.New("Topology for a row that must be quiet"))
	}
	if got.fatal > 0 {
		errs = append(errs, errors.New("Fatal without a removal"))
	}
	return errors.Join(errs...)
}

func runIP(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// waitRDMAPortState waits for ports/1/state to read "N: STATE".
func waitRDMAPortState(t *testing.T, device, state string) {
	t.Helper()
	path := filepath.Join(defaultRDMARoot, device, "ports", "1", "state")
	var text string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text = strings.TrimSpace(string(b))
		if _, name, _ := strings.Cut(text, ": "); name == state {
			return
		}
	}
	t.Fatalf("%s is %q, want %s", path, text, state)
}

// TestRDMAEventsRecorded is the differential table's link and address rows;
// rerun after the device returns, it is the rescan row.
func TestRDMAEventsRecorded(t *testing.T) {
	f := eventsEnv(t)
	const dummy = "lmdiffdummy0"
	runIP(t, "link", "add", dummy, "type", "dummy")
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", dummy).Run() })
	t.Cleanup(func() {
		_ = exec.Command("ip", "link", "set", f.netdev, "up").Run()
		if f.peer != "" {
			_ = exec.Command("ip", "link", "set", f.peer, "up").Run()
		}
	})
	side := mustOpenSide(t, f)
	rows := []eventRow{
		{"down", "positive", "ip link set NETDEV down", "one Refresh for port 1, as verbs' PORT_ERR",
			[][]string{{"link", "set", f.netdev, "down"}}, "DOWN", 1, false, false},
		{"up", "positive", "ip link set NETDEV up", "one Refresh for port 1, as verbs' PORT_ACTIVE",
			[][]string{{"link", "set", f.netdev, "up"}}, "ACTIVE", 1, false, false},
		{"gid_add", "positive", "an IPv6 address added to the netdev", "a Topology, as verbs' GID_CHANGE; no Refresh",
			[][]string{{"-6", "addr", "add", "2001:db8:1::1/64", "dev", f.netdev, "nodad"}}, "ACTIVE", 0, true, false},
		{"gid_del", "positive", "the same address deleted", "a Topology, as verbs' GID_CHANGE; no Refresh",
			[][]string{{"-6", "addr", "del", "2001:db8:1::1/64", "dev", f.netdev}}, "ACTIVE", 0, true, false},
		{"unrelated", "negative", "a dummy netdev with no RDMA device goes up and down", "no event, as from verbs",
			[][]string{{"link", "set", dummy, "up"}, {"link", "set", dummy, "down"}}, "ACTIVE", 0, false, true},
		{"flap", "boundary", "down then up with no pause", "two Refresh, as verbs",
			[][]string{{"link", "set", f.netdev, "down"}, {"link", "set", f.netdev, "up"}}, "ACTIVE", 2, false, false},
		{"down_again", "corner", "a second down while already down", "one Refresh: only a state change is reported",
			[][]string{{"link", "set", f.netdev, "down"}, {"link", "set", f.netdev, "down"}}, "DOWN", 1, false, false},
		{"restore", "corner", "up after down_again", "one Refresh, as verbs",
			[][]string{{"link", "set", f.netdev, "up"}}, "ACTIVE", 1, false, false},
	}
	if f.peer != "" {
		rows = append(rows,
			eventRow{"carrier_only", "boundary", "the veth peer goes down: carrier drops with the netdev administratively up",
				"one Refresh, as verbs", [][]string{{"link", "set", f.peer, "down"}}, "DOWN", 1, false, false},
			eventRow{"carrier_back", "boundary", "the veth peer comes back", "one Refresh, as verbs",
				[][]string{{"link", "set", f.peer, "up"}}, "ACTIVE", 1, false, false})
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			side.collect(f.device, 500*time.Millisecond)
			for _, command := range row.commands {
				runIP(t, command...)
			}
			waitRDMAPortState(t, f.device, row.state)
			got := side.collect(f.device, 2*time.Second)
			t.Logf("netlink %+v", got)
			side.running(t)
			if err := row.check(got); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestRDMAEventsRemove is the pci_remove row and the removed_mid_read fault:
// the guest's LINKMONITOR_EVENTS_REMOVE removes the device while the source
// holds it, and the NLDEV UNREGISTER of a device the port map holds is Fatal,
// as verbs' DEVICE_FATAL was. The source holds no uverbs file, so removal
// never waits on it, even on a driver without disassociate_ucontext (rxe).
func TestRDMAEventsRemove(t *testing.T) {
	f := eventsEnv(t)
	remove := os.Getenv("LINKMONITOR_EVENTS_REMOVE")
	if remove == "" {
		t.Fatal("LINKMONITOR_EVENTS_REMOVE is required")
	}
	t.Logf("corner: %s while the source holds %s; expected: Fatal for the device", remove, f.device)
	side := mustOpenSide(t, f)
	side.collect(f.device, 500*time.Millisecond)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "bash", "-c", remove).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", remove, err, out)
	}
	deadline := time.After(30 * time.Second)
	for fatal := false; !fatal; {
		select {
		case e := <-side.events:
			t.Logf("%+v", e)
			fatal = e.Kind == rdmaevents.Fatal && e.Device == f.device
		case err := <-side.done:
			// Run ended; put the result back for the cleanup.
			side.done <- err
			t.Fatalf("Run ended before a Fatal: %v", err)
		case <-deadline:
			t.Fatalf("no Fatal for %s", f.device)
		}
	}
}

// TestRDMAEventsFaults is the faults table's rows. The source holds no
// uverbs node, so uverbs_denied, which left verbs unable to open the device,
// does not affect it; that is the C2b difference the backend exists for.
func TestRDMAEventsFaults(t *testing.T) {
	f := eventsEnv(t)
	node := uverbsNode(t, f.device)
	info, err := os.Stat(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name, category, description, expected string
		run                                   func(t *testing.T) error
	}{
		{"uverbs_denied", "negative", "the uverbs node mode 000 and the opener without CAP_DAC_OVERRIDE",
			"opens and reports a flap; verbs could not open the device", func(t *testing.T) error {
				if err := os.Chmod(node, 0); err != nil {
					return err
				}
				defer func() { _ = os.Chmod(node, info.Mode().Perm()) }()
				var side *eventSide
				if err := withoutDACOverride(func() (err error) {
					side, err = openSide(t, f)
					return err
				}); err != nil {
					return err
				}
				return flapped(t, f, side)
			}},
		{"recover_after_fault", "positive", "the node's mode restored", "opens and reports the same flap", func(t *testing.T) error {
			return flapped(t, f, mustOpenSide(t, f))
		}},
		{"namespace", "negative", "the source opened in a new network namespace",
			"opens; the netdev is not in that namespace, so a flap reports no port event", func(t *testing.T) error {
				side, err := inNewNetns(func() (*eventSide, error) { return openSide(t, f) })
				if err != nil {
					return err
				}
				side.collect(f.device, 500*time.Millisecond)
				runIP(t, "link", "set", f.netdev, "down")
				runIP(t, "link", "set", f.netdev, "up")
				waitRDMAPortState(t, f.device, "ACTIVE")
				if got := side.collect(f.device, 2*time.Second); got.refresh+got.topology+got.fatal > 0 {
					return errors.New("events from another namespace's netdev")
				}
				return nil
			}},
		{"empty_queue", "boundary", "nothing happens", "no event, as from verbs", func(t *testing.T) error {
			side := mustOpenSide(t, f)
			side.collect(f.device, 500*time.Millisecond)
			if got := side.collect(f.device, 2*time.Second); got.refresh+got.topology+got.fatal+len(got.other) > 0 {
				return errors.New("an event with nothing queued")
			}
			return nil
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			if err := row.run(t); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// flapped takes the netdev down and up and requires two Refresh from s.
func flapped(t *testing.T, f eventsFixture, s *eventSide) error {
	t.Helper()
	s.collect(f.device, 500*time.Millisecond)
	runIP(t, "link", "set", f.netdev, "down")
	waitRDMAPortState(t, f.device, "DOWN")
	runIP(t, "link", "set", f.netdev, "up")
	waitRDMAPortState(t, f.device, "ACTIVE")
	if got := s.collect(f.device, 2*time.Second); got.refresh < 2 {
		return errors.New("a flap without two Refresh")
	}
	return nil
}

// uverbsNode is the device's /dev/infiniband/uverbsN.
func uverbsNode(t *testing.T, device string) string {
	t.Helper()
	// infiniband_verbs/uverbsN/ibdev names the device, virtual ones included.
	matches, err := filepath.Glob("/sys/class/infiniband_verbs/uverbs*/ibdev")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		b, err := os.ReadFile(m)
		if err == nil && strings.TrimSpace(string(b)) == device {
			return filepath.Join("/dev/infiniband", filepath.Base(filepath.Dir(m)))
		}
	}
	t.Fatalf("no uverbs node for %s in %v", device, matches)
	return ""
}

// withoutDACOverride runs fn on a thread whose effective set lacks
// CAP_DAC_OVERRIDE and CAP_DAC_READ_SEARCH, so root's opens obey file modes.
// Capabilities are per thread, and the sockets fn opens are opened on the
// calling thread. The goroutine keeps its thread locked if the set cannot be
// restored, so the thread exits with it rather than returning to the
// scheduler.
func withoutDACOverride(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
		var saved, reduced [2]unix.CapUserData
		if err := unix.Capget(&header, &saved[0]); err != nil {
			done <- err
			return
		}
		reduced = saved
		reduced[0].Effective &^= 1<<unix.CAP_DAC_OVERRIDE | 1<<unix.CAP_DAC_READ_SEARCH
		if err := unix.Capset(&header, &reduced[0]); err != nil {
			done <- err
			return
		}
		err := fn()
		if restore := unix.Capset(&header, &saved[0]); restore != nil {
			done <- errors.Join(err, restore)
			return
		}
		runtime.UnlockOSThread()
		done <- err
	}()
	return <-done
}

// inNewNetns runs open on a thread moved to a new network namespace. The
// sockets it opens stay in that namespace; the thread is never unlocked, so
// it exits with the goroutine.
func inNewNetns(open func() (*eventSide, error)) (*eventSide, error) {
	type result struct {
		side *eventSide
		err  error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			done <- result{err: err}
			return
		}
		side, err := open()
		done <- result{side, err}
	}()
	r := <-done
	return r.side, r.err
}
