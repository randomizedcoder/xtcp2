package main

import (
	"context"
	cryptoRand "crypto/rand"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	baseNamespaceName = "ns"
	initialNamespaces = 1000

	sleepDefaultDuration = 100 * time.Millisecond
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stderr))
}

// runMain wires flag parsing + the churn loop. Extracted so tests can
// drive it with a cancellable ctx + synthetic args, without actually
// shelling out to `ip netns` for the full 1000-namespace initial fill.
func runMain(ctx context.Context, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("nsTest", flag.ContinueOnError)
	fs.SetOutput(stderr)
	sleep := fs.Duration("sleep", sleepDefaultDuration, "sleep duration")
	initialCount := fs.Int("initial", initialNamespaces, "initial namespace count (for tests; production keeps the 1000 default)")
	// -traffic: legacy "one brief TIME_WAIT pair per ns" mode. Kept for
	// backward compat with old soak invocations. Prefer -conns for new
	// soak runs — persistent connections give xtcp2's per-namespace
	// poll real ESTABLISHED sockets with varied TCP_INFO statistics.
	traffic := fs.Bool("traffic", false, "after `ip netns add`, inject one brief loopback TCP exchange (TIME_WAIT pair) per ns")
	// -conns N: open N persistent loopback connections per ns with
	// varied io profiles (payload size + send cadence) so the per-ns
	// poll readout has 2N ESTABLISHED sockets with different segs/
	// bytes/rtt statistics. Connections close cleanly when the ns is
	// removed by the churn loop (per-ns context cancel).
	conns := fs.Int("conns", 0, "open this many persistent loopback TCP connections per ns with varied io profiles; 0 disables")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Initial-fill phase: create `*initialCount` namespaces.
	for i := 0; i < *initialCount; i++ {
		if ctx.Err() != nil {
			return 0
		}
		ns := namespaceName(i)
		createNamespace(ctx, ns)
		if *traffic {
			injectLoopbackTraffic(ns)
		}
		if *conns > 0 {
			startPersistentTraffic(ctx, ns, *conns)
		}
	}

	// Churn loop: alternately create+remove one namespace per tick.
	return churn(ctx, *initialCount, *sleep, *traffic, *conns)
}

// churn is the production-mode forever loop: add one namespace and
// remove the oldest each iteration, sleeping `sleep` between rounds.
// Returns 0 on ctx cancel.
func churn(ctx context.Context, initial int, sleep time.Duration, traffic bool, conns int) int {
	j := 0
	for {
		if ctx.Err() != nil {
			return 0
		}
		newNamespace := namespaceName(j + initial)
		createNamespace(ctx, newNamespace)
		if traffic {
			injectLoopbackTraffic(newNamespace)
		}
		if conns > 0 {
			startPersistentTraffic(ctx, newNamespace, conns)
		}
		log.Printf("Added namespace: %s\n", newNamespace)

		oldestNamespace := namespaceName(j)
		// Stop the persistent traffic in the ns we're about to delete,
		// so its goroutines close their conns cleanly *before* the
		// kernel reaps the ns. Otherwise the io goroutines see EBADF /
		// EPIPE and surface noise.
		if conns > 0 {
			stopPersistentTraffic(oldestNamespace)
		}
		removeNamespace(ctx, oldestNamespace)
		log.Printf("Removed namespace: %s\n", oldestNamespace)

		j++
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(sleep):
		}
	}
}

// injectLoopbackTraffic enters the named netns, brings up lo, opens
// one loopback TCP connection (listener + dialer in-process), exchanges
// a payload, and closes — leaving a TIME_WAIT pair visible to
// inet_diag for ~60 s. The net effect is that every namespace nsTest
// creates carries socket state during its lifetime, instead of being
// socket-empty as `ip netns add` leaves them.
//
// Runs on a LockOSThread'd goroutine so setns affects only this
// thread; we restore the original netns before returning so the
// outer process keeps polling /run/netns from the host's ns.
//
// Errors are logged but non-fatal — the surrounding churn loop must
// keep running regardless of a single ns's setup failing.
func injectLoopbackTraffic(nsName string) {
	runtime.LockOSThread()
	// NB: NO unconditional defer UnlockOSThread — same pattern as
	// xtcp2's netNamespaceInstance. If the Setns restore fails the
	// goroutine exits with the lock held and the Go runtime
	// terminates the OS thread instead of recycling a tainted M.

	// Snapshot the calling thread's netns so we can restore it.
	origNs, err := os.Open("/proc/thread-self/ns/net")
	if err != nil {
		log.Printf("injectLoopbackTraffic %s: open orig ns: %v", nsName, err)
		return
	}
	defer origNs.Close()
	defer func() {
		if rerr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); rerr != nil {
			log.Printf("injectLoopbackTraffic %s: restore ns: %v (keeping thread locked → runtime will terminate it)", nsName, rerr)
			return
		}
		runtime.UnlockOSThread()
	}()

	// Open the target netns and setns into it.
	target, err := os.Open("/run/netns/" + nsName)
	if err != nil {
		// Race: ns may have been deleted between createNamespace
		// and here. Not actionable; skip.
		return
	}
	defer target.Close()
	if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
		log.Printf("injectLoopbackTraffic %s: setns: %v", nsName, err)
		return
	}

	// Bring up lo so 127.0.0.1 is routable. Shelling out is slower
	// than a direct SIOCSIFFLAGS ioctl, but at the soak's churn rate
	// (~10/s) the cost is negligible and the code is much simpler.
	if err := exec.Command("ip", "link", "set", "lo", "up").Run(); err != nil {
		log.Printf("injectLoopbackTraffic %s: ip link set lo up: %v", nsName, err)
		return
	}

	// Open a TCP listener + dialer pair. Listen on a random port so
	// we don't clash with anything else inside the ns. Exchange one
	// payload, close. The kernel keeps TIME_WAIT entries for ~60s
	// per Linux's default tcp_fin_timeout/timewait — well within the
	// ~20s ns lifetime under the soak's 100 ms churn cadence.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("injectLoopbackTraffic %s: listen: %v", nsName, err)
		return
	}
	defer listener.Close()
	addr := listener.Addr().String()

	// Accept the connection in a goroutine so the dialer can connect.
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		c, aerr := listener.Accept()
		if aerr != nil {
			return
		}
		// Drain a few bytes so the connection actually flows + the
		// kernel records segs-in/out (visible via inet_diag's TCPInfo).
		var buf [16]byte
		_, _ = c.Read(buf[:]) //nolint:errcheck // best-effort drain
		c.Close()
	}()

	// Dial + send. 200 ms total timeout so a setns race or other
	// per-ns flake can't stall the whole churn loop.
	dialer := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		log.Printf("injectLoopbackTraffic %s: dial: %v", nsName, err)
		return
	}
	_, _ = conn.Write([]byte("xtcp2-soak\n")) //nolint:errcheck // best-effort
	conn.Close()

	select {
	case <-acceptDone:
	case <-time.After(200 * time.Millisecond):
	}
}

// nsTrafficState tracks the lifecycle of one ns's persistent-connection
// generator. The cancel function tears down the io goroutines; done
// closes when every io goroutine has returned, so stopPersistentTraffic
// can wait for a clean shutdown before removeNamespace runs.
type nsTrafficState struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// nsTrafficStates: ns name → state. Stored separately from the churn
// loop's local counter so churn() doesn't have to thread per-ns state
// through every call site.
var nsTrafficStates sync.Map

// trafficPayloadSizes / trafficSendIntervals: the cross product
// determines per-connection io profile diversity. Each ns gets `conns`
// connections; conn N picks profile (N % len(sizes), (N / len(sizes))
// % len(intervals)) so consecutive conns differ in BOTH dimensions and
// the TCP_INFO populations xtcp2 sees have a real spread.
var trafficPayloadSizes = []int{
	16,
	256,
	4096,
	16384,
	65536,
}

var trafficSendIntervals = []time.Duration{
	1 * time.Millisecond,
	10 * time.Millisecond,
	100 * time.Millisecond,
	500 * time.Millisecond,
}

// startPersistentTraffic enters nsName, opens `count` listener+dialer
// pairs on loopback, hands the resulting conns to io goroutines with
// varied per-conn profiles, and registers a per-ns cancel so churn()
// can tear it down before deleting the ns. Non-fatal on errors — a
// failure to bring up some ns's traffic must not stop the wider churn.
func startPersistentTraffic(parentCtx context.Context, nsName string, count int) {
	nsCtx, cancel := context.WithCancel(parentCtx)
	done := make(chan struct{})
	nsTrafficStates.Store(nsName, &nsTrafficState{cancel: cancel, done: done})

	go runPersistentTraffic(nsCtx, nsName, count, done)
}

// stopPersistentTraffic signals the per-ns generator to shut down and
// waits briefly for io goroutines to close their sockets. Called by
// churn() immediately before removeNamespace.
func stopPersistentTraffic(nsName string) {
	v, ok := nsTrafficStates.LoadAndDelete(nsName)
	if !ok {
		return
	}
	state, _ := v.(*nsTrafficState)
	state.cancel()
	// Bounded wait: io goroutines may be in mid-Read/Write when the
	// cancel fires. Closing the connection from the runner side
	// (done by runPersistentTraffic) unblocks them.
	select {
	case <-state.done:
	case <-time.After(2 * time.Second):
		log.Printf("stopPersistentTraffic %s: 2s drain timeout — proceeding with ns delete anyway", nsName)
	}
}

// runPersistentTraffic is the per-ns generator goroutine. Lifecycle:
//  1. Enter the ns on a LockOSThread'd goroutine.
//  2. Bring lo UP.
//  3. Open `count` listener+dialer pairs; collect server and client
//     conns into a slice.
//  4. Setns back to host ns (conditional UnlockOSThread on success;
//     keep lock held on failure so the runtime terminates the
//     tainted OS thread — same pattern as xtcp2's netNamespaceInstance).
//  5. Spawn 2 io goroutines per pair (echo server + varied client).
//     These don't need to be in the ns; the sockets carry their netns
//     identity once opened.
//  6. Wait for ns ctx cancel; close all conns to unblock io
//     goroutines; wait for them; close `done`.
func runPersistentTraffic(nsCtx context.Context, nsName string, count int, done chan struct{}) {
	defer close(done)

	runtime.LockOSThread()
	origNs, err := os.Open("/proc/thread-self/ns/net")
	if err != nil {
		log.Printf("runPersistentTraffic %s: open orig ns: %v", nsName, err)
		return
	}
	defer origNs.Close()
	restoredOK := false
	defer func() {
		if !restoredOK {
			// Keep the lock held — Go runtime terminates this thread
			// rather than recycling an M with a non-host netns.
			return
		}
		runtime.UnlockOSThread()
	}()

	target, err := os.Open("/run/netns/" + nsName)
	if err != nil {
		// Race: ns deleted between createNamespace and here.
		_ = unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET)
		restoredOK = true
		return
	}
	defer target.Close()
	if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
		log.Printf("runPersistentTraffic %s: setns: %v", nsName, err)
		_ = unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET)
		restoredOK = true
		return
	}

	if err := exec.Command("ip", "link", "set", "lo", "up").Run(); err != nil {
		log.Printf("runPersistentTraffic %s: ip link set lo up: %v", nsName, err)
		// Try to restore + return
		if unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET) == nil {
			restoredOK = true
		}
		return
	}

	type pair struct {
		server  net.Conn
		client  net.Conn
		profile int
	}
	pairs := make([]pair, 0, count)

	// Open all pairs. A single listener per port is sufficient; we
	// dial back immediately and Close the listener once the accepted
	// conn is in hand so the kernel can reuse the port for the next
	// pair.
	// Generous dial timeout: under init-fill load (200 ns × 100 conns
	// = 20k near-simultaneous socket() + connect()), the kernel's
	// loopback path gets congested even though the SYN never leaves
	// the box. 2s gives plenty of headroom; steady-state churn
	// (one new ns / 100 ms) doesn't come anywhere near this.
	const dialTimeout = 2 * time.Second
	for i := 0; i < count; i++ {
		l, lerr := net.Listen("tcp", "127.0.0.1:0")
		if lerr != nil {
			// Listen failures are rare and usually mean fd exhaustion
			// or netns going away — surface once per ns, then break.
			log.Printf("runPersistentTraffic %s: listen %d: %v", nsName, i, lerr)
			break
		}
		addr := l.Addr().String()
		acceptCh := make(chan net.Conn, 1)
		go func() {
			c, aerr := l.Accept()
			if aerr != nil {
				acceptCh <- nil
				return
			}
			acceptCh <- c
		}()
		dialer := net.Dialer{Timeout: dialTimeout}
		client, derr := dialer.Dial("tcp", addr)
		if derr != nil {
			// Dial failures during init-burst are noisy by design —
			// 100 conns × 200 ns kicks off ~20k connect() in one go
			// and the kernel sheds some load. Silent retry-or-skip
			// keeps the journal readable. Steady-state churn doesn't
			// hit this path.
			l.Close()
			continue
		}
		server := <-acceptCh
		_ = l.Close() // listener no longer needed; accept returned
		if server == nil {
			client.Close()
			continue
		}
		pairs = append(pairs, pair{server: server, client: client, profile: i})
	}

	// Restore the host netns + conditionally unlock the OS thread.
	if rerr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); rerr != nil {
		log.Printf("runPersistentTraffic %s: restore ns: %v (keeping thread locked → runtime will terminate it)", nsName, rerr)
	} else {
		restoredOK = true
	}

	if len(pairs) == 0 {
		return
	}

	// Spawn io goroutines. These do NOT need to be on a LockOSThread'd
	// thread — the sockets are already in the right netns; reading and
	// writing them just touches kernel fds.
	var wg sync.WaitGroup
	for _, p := range pairs {
		wg.Add(2)
		go func(p pair) { defer wg.Done(); runEchoServer(nsCtx, p.server) }(p)
		go func(p pair) { defer wg.Done(); runVariedClient(nsCtx, p.client, p.profile) }(p)
	}

	<-nsCtx.Done()
	// Close all sockets so blocked Read/Write calls return.
	for _, p := range pairs {
		_ = p.server.Close()
		_ = p.client.Close()
	}
	wg.Wait()
}

// runEchoServer drains whatever the client sends and writes it back.
// Returns on ctx cancel (the connection is closed by the parent
// goroutine, which unblocks Read).
func runEchoServer(_ context.Context, c net.Conn) {
	defer c.Close()
	buf := make([]byte, 64*1024)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		if _, werr := c.Write(buf[:n]); werr != nil {
			return
		}
	}
}

// runVariedClient drives a single connection with a profile-dependent
// payload size + send cadence. profileIdx is the per-conn index inside
// the ns; consecutive conns get different sizes AND intervals so the
// inet_diag readout shows real spread in TCPInfo segs/bytes/rtt.
func runVariedClient(ctx context.Context, c net.Conn, profileIdx int) {
	defer c.Close()

	payloadSize := trafficPayloadSizes[profileIdx%len(trafficPayloadSizes)]
	sendInterval := trafficSendIntervals[(profileIdx/len(trafficPayloadSizes))%len(trafficSendIntervals)]

	payload := make([]byte, payloadSize)
	if _, err := cryptoRand.Read(payload); err != nil {
		// Fall back to math/rand if /dev/urandom is unhappy. Doesn't
		// matter cryptographically; we just want bytes.
		rng := rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec // not security-relevant
		for i := range payload {
			payload[i] = byte(rng.Intn(256))
		}
	}
	readBuf := make([]byte, payloadSize)

	ticker := time.NewTicker(sendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if _, err := c.Write(payload); err != nil {
			return
		}
		if _, err := io.ReadFull(c, readBuf); err != nil {
			return
		}
	}
}

func namespaceName(index int) string {
	return fmt.Sprintf("%s%d", baseNamespaceName, index)
}

func createNamespace(ctx context.Context, name string) {

	log.Printf("createNamespace: ip netns add %s", name)
	cmd := exec.CommandContext(ctx, "ip", "netns", "add", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to create namespace %s: %v", name, err)
	}

}

func removeNamespace(ctx context.Context, name string) {
	log.Printf("removeNamespace: ip netns del %s", name)
	cmd := exec.CommandContext(ctx, "ip", "netns", "del", name)
	if err := cmd.Run(); err != nil {
		log.Printf("Failed to remove namespace %s: %v", name, err)
	}
}
