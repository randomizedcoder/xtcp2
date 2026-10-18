//go:build linux && rdma && cgo && (rdma_vm || ionic_vm)

package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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

// The differential event test (GO-NATIVE-RDMA-ASSESSMENT.md "Differential
// event test", R1-T05) runs in test-linkmonitor-rdma-vm against rxe and in
// test-linkmonitor-ionic-vm against ionic_rdma. It opens the monitor's own
// RDMA event source, openRDMAEvents, once per backend, both at once, so each
// side is the port source joined with the NLDEV monitor exactly as the
// monitor runs it, and compares what each delivers for the same trigger.
//
// The guest sets LINKMONITOR_EVENTS_NETDEV and LINKMONITOR_EVENTS_RDMA, and
// optionally LINKMONITOR_EVENTS_PEER (a veth peer whose down drops the
// netdev's carrier), LINKMONITOR_EVENTS_REMOVE (a shell command that removes
// the device while it is held) and LINKMONITOR_EVENTS_FAULTS=1.

type eventsFixture struct {
	netdev, device, peer string
	ifindex              uint32
	set                  rdmaEventSet
	// root is the RDMA sysfs class root the verbs side verifies identity
	// against; empty for the real one.
	root string
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
	hardware, err := filepath.EvalSymlinks(filepath.Join(defaultRDMARoot, f.device, "device"))
	if errors.Is(err, fs.ErrNotExist) {
		// rxe is virtual: it has no device link, the monitor's inventory
		// excludes it, and the verbs provider refuses an identity without
		// hardware. A class root holding only a device link lets the verbs
		// side open it; libibverbs still finds the device by name.
		f.root = t.TempDir()
		hardware = t.TempDir()
		if err := os.Mkdir(filepath.Join(f.root, f.device), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(hardware, filepath.Join(f.root, f.device, "device")); err != nil {
			t.Fatal(err)
		}
		hardware, err = filepath.EvalSymlinks(filepath.Join(f.root, f.device, "device"))
	}
	if err != nil {
		t.Fatal(err)
	}
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
	f.set = rdmaEventSet{ids: []rdmaevents.Identity{{Name: f.device, Hardware: hardware}}, ports: ports}
	return f
}

// eventSide is one backend's running source and what it has delivered.
type eventSide struct {
	backend RDMAEventsBackend
	events  chan rdmaevents.Event
	done    chan error
	// covered is false when a device could not be opened: the source runs
	// and reports it as Fatal, as the monitor's worker expects.
	covered bool
}

func openSide(t *testing.T, backend RDMAEventsBackend, f eventsFixture) (*eventSide, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	s := &lifecycleSession{rdmaEventsBackend: backend, rdmaRoot: f.root}
	source, err := s.openRDMAEvents(ctx, f.set)
	if err != nil {
		cancel()
		return nil, err
	}
	side := &eventSide{backend: backend, events: make(chan rdmaevents.Event, rdmaevents.Limit), done: make(chan error, 1),
		covered: source.(interface{ Covered() bool }).Covered()}
	go func() {
		side.done <- source.Run(ctx, func(e rdmaevents.Event) bool {
			select {
			case side.events <- e:
			default:
				t.Errorf("%s: event buffer full; dropped %+v", backend, e)
			}
			return true
		})
	}()
	t.Cleanup(func() {
		cancel()
		if err := errors.Join(source.Close(), ignoreCanceled(<-side.done)); err != nil {
			t.Errorf("%s: %v", backend, err)
		}
	})
	return side, nil
}

// ignoreCanceled keeps the errors a closed source reports for itself.
func ignoreCanceled(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, rdmaevents.ErrLost) {
		return nil
	}
	return err
}

func openSides(t *testing.T, f eventsFixture) [2]*eventSide {
	t.Helper()
	var sides [2]*eventSide
	for i, backend := range []RDMAEventsBackend{RDMAEventsVerbs, RDMAEventsNetlink} {
		side, err := openSide(t, backend, f)
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		sides[i] = side
	}
	return sides
}

// tally is one side's events for a row, by what the schedule does with them.
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

// running fails the test if the side's source has returned.
func (s *eventSide) running(t *testing.T) {
	t.Helper()
	select {
	case err := <-s.done:
		s.done <- err
		t.Fatalf("%s: source ended: %v", s.backend, err)
	default:
	}
}

// diffRow is one trigger and what each side must deliver for it.
type diffRow struct {
	name, category, description, expected string
	commands                              [][]string
	state                                 string
	// refresh is the netlink side's exact Refresh count. Verbs may deliver
	// up to twice as many: rxe dispatches PORT_ACTIVE from the driver and
	// the core on some kernels (rxe_dup).
	refresh int
	// topology requires a Topology from both sides; quiet allows none from
	// either. Otherwise the netlink side may add Topology, as an address
	// change on the netdev can come with a link change the GID cache absorbs.
	topology, quiet bool
}

func (r diffRow) check(verbs, netlink tally) error {
	var errs []error
	if len(verbs.other)+len(netlink.other) > 0 {
		errs = append(errs, errors.New("events for another device or port"))
	}
	if netlink.refresh != r.refresh || verbs.refresh < r.refresh || verbs.refresh > 2*r.refresh {
		errs = append(errs, errors.New("Refresh counts differ from the row"))
	}
	if r.topology && (verbs.topology == 0 || netlink.topology == 0) {
		errs = append(errs, errors.New("no Topology from one side"))
	}
	if r.quiet && verbs.topology+netlink.topology > 0 {
		errs = append(errs, errors.New("Topology for a row that must be quiet"))
	}
	if !r.topology && !r.quiet && netlink.topology < verbs.topology {
		errs = append(errs, errors.New("verbs reported a Topology netlink did not"))
	}
	if verbs.fatal+netlink.fatal > 0 {
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

// TestDifferentialRDMAEvents is the differential table's link and address
// rows; rerun after the device returns, it is the rescan row.
func TestDifferentialRDMAEvents(t *testing.T) {
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
	sides := openSides(t, f)
	rows := []diffRow{
		{"down", "positive", "ip link set NETDEV down", "one Refresh for port 1 from both",
			[][]string{{"link", "set", f.netdev, "down"}}, "DOWN", 1, false, false},
		{"up", "positive", "ip link set NETDEV up", "one Refresh for port 1 from both",
			[][]string{{"link", "set", f.netdev, "up"}}, "ACTIVE", 1, false, false},
		{"gid_add", "positive", "an IPv6 address added to the netdev", "a Topology from both, no Refresh",
			[][]string{{"-6", "addr", "add", "2001:db8:1::1/64", "dev", f.netdev, "nodad"}}, "ACTIVE", 0, true, false},
		{"gid_del", "positive", "the same address deleted", "a Topology from both, no Refresh",
			[][]string{{"-6", "addr", "del", "2001:db8:1::1/64", "dev", f.netdev}}, "ACTIVE", 0, true, false},
		{"unrelated", "negative", "a dummy netdev with no RDMA device goes up and down", "no event from either",
			[][]string{{"link", "set", dummy, "up"}, {"link", "set", dummy, "down"}}, "ACTIVE", 0, false, true},
		{"flap", "boundary", "down then up with no pause", "two Refresh from both",
			[][]string{{"link", "set", f.netdev, "down"}, {"link", "set", f.netdev, "up"}}, "ACTIVE", 2, false, false},
		{"down_again", "corner", "a second down while already down", "one Refresh: only a state change is reported",
			[][]string{{"link", "set", f.netdev, "down"}, {"link", "set", f.netdev, "down"}}, "DOWN", 1, false, false},
		{"restore", "corner", "up after down_again", "one Refresh from both",
			[][]string{{"link", "set", f.netdev, "up"}}, "ACTIVE", 1, false, false},
	}
	if f.peer != "" {
		rows = append(rows,
			diffRow{"carrier_only", "boundary", "the veth peer goes down: carrier drops with the netdev administratively up",
				"one Refresh from both", [][]string{{"link", "set", f.peer, "down"}}, "DOWN", 1, false, false},
			diffRow{"carrier_back", "boundary", "the veth peer comes back", "one Refresh from both",
				[][]string{{"link", "set", f.peer, "up"}}, "ACTIVE", 1, false, false})
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", row.category, row.description, row.expected)
			for _, s := range sides {
				s.collect(f.device, 500*time.Millisecond)
			}
			for _, command := range row.commands {
				runIP(t, command...)
			}
			waitRDMAPortState(t, f.device, row.state)
			verbs, netlink := sides[0].collect(f.device, 2*time.Second), sides[1].collect(f.device, 0)
			t.Logf("verbs %+v, netlink %+v", verbs, netlink)
			for _, s := range sides {
				s.running(t)
			}
			if err := row.check(verbs, netlink); err != nil {
				t.Error(err)
			}
		})
	}
}

// TestDifferentialRDMARemove is the pci_remove row and the removed_mid_read
// fault: the guest's LINKMONITOR_EVENTS_REMOVE removes the device while the
// sides named by LINKMONITOR_EVENTS_REMOVE_BACKENDS (default both) hold it.
// Verbs reports DEVICE_FATAL from the async fd; netlink reports the NLDEV
// UNREGISTER of a device its port map holds as Fatal. A driver without
// disassociate_ucontext (rxe) delivers no DEVICE_FATAL: unregister waits for
// every uverbs file to close, so a verbs side holding one blocks the removal.
func TestDifferentialRDMARemove(t *testing.T) {
	f := eventsEnv(t)
	remove := os.Getenv("LINKMONITOR_EVENTS_REMOVE")
	if remove == "" {
		t.Fatal("LINKMONITOR_EVENTS_REMOVE is required")
	}
	backends := []RDMAEventsBackend{RDMAEventsVerbs, RDMAEventsNetlink}
	if names := os.Getenv("LINKMONITOR_EVENTS_REMOVE_BACKENDS"); names != "" {
		backends = backends[:0]
		for name := range strings.SplitSeq(names, ",") {
			backend := RDMAEventsBackend(name)
			if backend != RDMAEventsVerbs && backend != RDMAEventsNetlink {
				t.Fatalf("LINKMONITOR_EVENTS_REMOVE_BACKENDS: %q is not verbs or netlink", name)
			}
			backends = append(backends, backend)
		}
	}
	t.Logf("corner: %s while %v hold %s; expected: Fatal for the device from each", remove, backends, f.device)
	var sides []*eventSide
	for _, backend := range backends {
		side, err := openSide(t, backend, f)
		if err != nil {
			t.Fatalf("%s: %v", backend, err)
		}
		sides = append(sides, side)
	}
	for _, s := range sides {
		s.collect(f.device, 500*time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "bash", "-c", remove).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", remove, err, out)
	}
	for _, s := range sides {
		deadline := time.After(30 * time.Second)
		for fatal := false; !fatal; {
			select {
			case e := <-s.events:
				t.Logf("%s: %+v", s.backend, e)
				fatal = e.Kind == rdmaevents.Fatal && e.Device == f.device
			case err := <-s.done:
				// Run ended; put the result back for the cleanup.
				s.done <- err
				t.Fatalf("%s: Run ended before a Fatal: %v", s.backend, err)
			case <-deadline:
				t.Fatalf("%s: no Fatal for %s", s.backend, f.device)
			}
		}
	}
}

// TestDifferentialRDMAFaults is the faults table's rows that apply to both
// sides. The netlink side holds no uverbs node, so uverbs_denied affects
// verbs only; that is the C2b difference the backend exists for.
func TestDifferentialRDMAFaults(t *testing.T) {
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
			"verbs cannot open the device: not covered, a Fatal for it; netlink opens and reports a flap", func(t *testing.T) error {
				if err := os.Chmod(node, 0); err != nil {
					return err
				}
				defer func() { _ = os.Chmod(node, info.Mode().Perm()) }()
				var verbs *eventSide
				if err := withoutDACOverride(func() (err error) {
					verbs, err = openSide(t, RDMAEventsVerbs, f)
					return err
				}); err != nil {
					return err
				}
				if got := verbs.collect(f.device, 2*time.Second); verbs.covered || got.fatal != 1 {
					return fmt.Errorf("verbs opened a node it may not read: covered %v, %+v", verbs.covered, got)
				}
				netlink, err := openSide(t, RDMAEventsNetlink, f)
				if err != nil {
					return err
				}
				return flapped(t, f, netlink)
			}},
		{"recover_after_fault", "positive", "the node's mode restored", "both open and report the same flap", func(t *testing.T) error {
			sides := openSides(t, f)
			return errors.Join(flapped(t, f, sides[0]), flapped(t, f, sides[1]))
		}},
		{"namespace", "negative", "the netlink side opened in a new network namespace",
			"opens; the netdev is not in that namespace, so a flap reports no port event", func(t *testing.T) error {
				side, err := inNewNetns(func() (*eventSide, error) { return openSide(t, RDMAEventsNetlink, f) })
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
		{"empty_queue", "boundary", "nothing happens", "neither side delivers an event", func(t *testing.T) error {
			sides := openSides(t, f)
			for _, s := range sides {
				s.collect(f.device, 500*time.Millisecond)
			}
			for _, s := range sides {
				if got := s.collect(f.device, 2*time.Second); got.refresh+got.topology+got.fatal+len(got.other) > 0 {
					return errors.New(string(s.backend) + ": an event with nothing queued")
				}
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
		return errors.New(string(s.backend) + ": a flap without two Refresh")
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
// Capabilities are per thread, and cgo calls run on the calling thread. The
// goroutine keeps its thread locked if the set cannot be restored, so the
// thread exits with it rather than returning to the scheduler.
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
