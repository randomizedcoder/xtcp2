package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

const (
	baseNamespaceName = "ns"
	initialNamespaces = 1000
	namespaceDir      = "/run/netns"

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
	// -traffic: after each `ip netns add`, enter the new ns + bring lo
	// UP + open one quick loopback TCP exchange. Leaves a TIME_WAIT
	// pair visible to xtcp2's per-namespace inet_diag poll for the
	// ns's lifetime. Off by default so existing soak callers that
	// don't want this overhead aren't affected.
	traffic := fs.Bool("traffic", false, "after `ip netns add`, inject one loopback TCP connection inside the new ns so its inet_diag readout has sockets to report")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Initial-fill phase: create `*initialCount` namespaces.
	for i := 0; i < *initialCount; i++ {
		if ctx.Err() != nil {
			return 0
		}
		createNamespace(ctx, namespaceName(i))
		if *traffic {
			injectLoopbackTraffic(namespaceName(i))
		}
	}

	// Churn loop: alternately create+remove one namespace per tick.
	return churn(ctx, *initialCount, *sleep, *traffic)
}

// churn is the production-mode forever loop: add one namespace and
// remove the oldest each iteration, sleeping `sleep` between rounds.
// Returns 0 on ctx cancel.
func churn(ctx context.Context, initial int, sleep time.Duration, traffic bool) int {
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
		log.Printf("Added namespace: %s\n", newNamespace)

		oldestNamespace := namespaceName(j)
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
