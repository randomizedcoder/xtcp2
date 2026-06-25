package xtcp

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// mountInfoDir is the path checkMountInfo scans for a namespace's bind
// mount. Made a var (was const) so tests can redirect to a tempfile and
// drive the os.Open error branch.
var mountInfoDir = "/proc/self/mountinfo"

// netNamespaceInstance runs as a goroutine, and moves the thread
// into a network namespace, opens a netlink socket, and passes
// the socketFD back to the creator of this goroutine
// then this goroutine blocks, waiting to be canceled
// https://pkg.go.dev/github.com/vishvananda/netns#GetFromName
// https://pkg.go.dev/github.com/vishvananda/netns#GetFromPath
// https://tip.golang.org/doc/go1.10#runtime
func (x *XTCP) netNamespaceInstance(nsCtx context.Context, nsCancel context.CancelFunc, nsName *string) {

	startTime := time.Now()
	x.pC.WithLabelValues("netNamespaceInstance", "start", "counter").Inc()
	defer x.pC.WithLabelValues("netNamespaceInstance", "end", "counter").Inc()
	defer func() {
		x.pH.WithLabelValues("netNamespaceInstance", "complete", "counter").Observe(time.Since(startTime).Seconds())
		if x.debugLevel > 10 {
			log.Printf("netNamespaceInstance complete: %s after seconds:%0.3f", *nsName, time.Since(startTime).Seconds())
		}
	}()

	if x.debugLevel > 10 {
		log.Printf("netNamespaceInstance: %s", *nsName)
	}

	runtime.LockOSThread()

	// CRITICAL: snapshot the calling thread's original netns BEFORE the
	// retry loop's `setns` calls, then restore it on the way out via
	// defer. Without this, the M returned to Go's scheduler after
	// UnlockOSThread carries the modified kernel netns indefinitely.
	//
	// Earlier this function used an unconditional `defer
	// runtime.UnlockOSThread()` paired with a best-effort Setns restore.
	// Under nsTest churn at 250 ms cadence, the restore Setns kept
	// failing with EPERM — likely because the kernel rejected setns into
	// a netns whose original userns context had been altered by all the
	// intervening ns operations on this thread. The runtime then dutifully
	// recycled the *tainted* M, but discovered the netns mismatch on the
	// next syscall and was forced to spin up a fresh M. Over 1 h 45 min
	// we accumulated >2000 OS threads and crashed with
	// `fatal error: thread exhaustion`.
	//
	// The reliable fix is to make UnlockOSThread *conditional on the
	// restore succeeding*. If restore fails we leave the goroutine
	// holding the lock — when this function returns the Go runtime
	// terminates the OS thread instead of reusing it (documented
	// behavior of runtime.LockOSThread). The cost is one OS thread
	// creation per failed restore (~10 µs) instead of an unbounded
	// accumulation of tainted Ms.
	origNs, errOrig := os.Open("/proc/thread-self/ns/net")
	if errOrig != nil {
		x.pC.WithLabelValues("netNamespaceInstance", "snapshotOrigNs", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("netNamespaceInstance snapshot original netns err: %v", errOrig)
		}
		// No origNs → can't restore → keep the lock and let the runtime
		// terminate this thread when the goroutine exits.
	} else {
		defer func() {
			if cerr := origNs.Close(); cerr != nil {
				log.Printf("netNamespaceInstance: origNs close: %v", cerr)
			}
		}()
		defer func() {
			if rerr := restoreNsSetns(int(origNs.Fd()), unix.CLONE_NEWNET); rerr != nil {
				x.pC.WithLabelValues("netNamespaceInstance", "restoreNs", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("netNamespaceInstance restore-netns err: %v (keeping thread locked → runtime will terminate it)", rerr)
				}
				// Skip UnlockOSThread on failure — see top-of-function
				// comment. Goroutine exits with the lock still held; Go
				// runtime terminates the thread.
				return
			}
			x.pC.WithLabelValues("netNamespaceInstance", "restoreNs", "count").Inc()
			runtime.UnlockOSThread() //nolint:forbidigo // safe: only called after Setns restore returned nil; tainted-M case takes the early `return` above.
		}()
	}

	// if x.debugLevel > 10 {
	// 	log.Printf("netNamespaceInstance after LockOSThread: %s", ns.name)
	// }

	fd := x.openAndSetNSWithRetries(nsName)

	// If the namespace was deleted during the (possibly slow, retrying) setns
	// above, nsDelete has already called cancel() — reachable because nsAdd
	// stored it before this goroutine launched. Abort before opening a netlink
	// socket we'd immediately close; the deferred restore + UnlockOSThread
	// still run, so the OS thread is released/terminated cleanly instead of
	// this goroutine blocking forever on <-nsCtx.Done() (the leak that
	// exhausted the SetMaxThreads cap under churn).
	if nsCtx.Err() != nil {
		x.pC.WithLabelValues("netNamespaceInstance", "abortedDuringInit", "count").Inc()
		x.closeFD(fd)
		return
	}

	// if x.debugLevel > 10 {
	// 	log.Printf("netNamespaceInstance after unix.Setns: %s", ns.name)
	// }

	// https://godoc.org/golang.org/x/sys/unix#Socket
	socketFD, err := syscall.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM, unix.NETLINK_INET_DIAG)
	if err != nil {
		x.pC.WithLabelValues("netNamespaceInstance", "Socket", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("netNamespaceInstance syscall.Socket err: %v", err)
		}
		// Don't leak fd: openAndSetNSWithRetries returned a netns fd
		// we no longer need now that this namespace's setup is
		// abandoned.
		x.closeFD(fd)
		return
	}

	defer x.closeSocket(socketFD)

	// https://godoc.org/golang.org/x/sys/unix#Bind
	// https://godoc.org/golang.org/x/sys/unix#SockaddrNetlink
	err = unix.Bind(socketFD, &unix.SockaddrNetlink{Family: syscall.AF_NETLINK})
	if err != nil {
		// Demoted from log.Fatalf: a per-namespace Bind failure used
		// to kill the entire daemon (and every other namespace's
		// goroutine + the gRPC services + the poller). Count it,
		// release the fd we opened to setns, and return so the
		// surrounding nsAdd path can move on.
		x.pC.WithLabelValues("netNamespaceInstance", "Bind", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("netNamespaceInstance unix.Bind err: %v", err)
		}
		x.closeFD(fd)
		return
	}

	// The per-ns context + cancel were created in nsAdd and stored in nsMap
	// before this goroutine launched, so nsDelete can always reach cancel()
	// (see nsAdd). createNetlinkersAndStore fills in the socketFD and starts
	// the netlinkers; it no-ops if the namespace was already deleted during
	// the setns/socket init above.
	x.createNetlinkersAndStore(nsCtx, nsCancel, nsName, socketFD)

	x.pH.WithLabelValues("netNamespaceInstance", "store", "counter").Observe(time.Since(startTime).Seconds())

	x.closeFD(fd)

	// block waiting for done (per-ns ctx, not parent — see comment above)
	<-nsCtx.Done()

	x.pC.WithLabelValues("netNamespaceInstance", "ctx.Done", "count").Inc()
}

func (x *XTCP) closeSocket(socketFD int) {
	if err := unix.Close(socketFD); err != nil {
		x.pC.WithLabelValues("netNamespaceInstance", "closeSocketFD", "error").Inc()
		return
	}
	x.pC.WithLabelValues("netNamespaceInstance", "closeSocketFD", "count").Inc()
}

func (x *XTCP) closeFD(fd int) {
	if err := unix.Close(fd); err != nil {
		x.pC.WithLabelValues("netNamespaceInstance", "closeFd", "error").Inc()
		return
	}
	x.pC.WithLabelValues("netNamespaceInstance", "closeFd", "count").Inc()
}

func (x *XTCP) openDefaultNetLinkSocket(ctx context.Context) {

	// https://godoc.org/golang.org/x/sys/unix#Socket
	socketFD, err := syscall.Socket(
		unix.AF_NETLINK,
		unix.SOCK_DGRAM,
		unix.NETLINK_INET_DIAG,
	)
	if err != nil {
		log.Fatalf("openDefaultNetLinkSocket unix.Socket %s", err)
	}

	// Bind the socket
	// https://godoc.org/golang.org/x/sys/unix#Bind
	// https://godoc.org/golang.org/x/sys/unix#SockaddrNetlink
	err = unix.Bind(socketFD, &unix.SockaddrNetlink{Family: syscall.AF_NETLINK})
	if err != nil {
		log.Fatalf("openDefaultNetLinkSocket unix.Bind %s", err)
	}

	df := "default"
	nsCtx, nsCancel := context.WithCancel(ctx)
	x.createNetlinkersAndStore(nsCtx, nsCancel, &df, socketFD)

	if x.debugLevel > 10 {
		log.Printf("openDefaultNetLinkSocket default net namespace netlink socket stored")
	}
}

const (
	maxRetriesCst = 10
)

// backoffFactorNs is the base unit for the exponential backoff in
// openAndSetNSWithRetries / checkMountInfoWithRetries, stored as
// nanoseconds in an atomic so tests can shrink it without racing with
// concurrently running production-path tests (nsAdd → checkMountInfo
// reads this while a test mutates it; the previous plain-var version
// tripped the race detector). Production code never mutates it.
var backoffFactorNs atomic.Int64

func init() {
	backoffFactorNs.Store(int64(10 * time.Millisecond))
}

// backoffFactor returns the current backoff base duration. Wraps the
// atomic load so callers don't need to convert ns → time.Duration each
// time they need the value.
func backoffFactor() time.Duration {
	return time.Duration(backoffFactorNs.Load())
}

// openAndSetnsSyscalls is the seam that the test suite swaps for a
// fake. Default points at the real unix.* calls. NOT for production
// reconfiguration — only init_test.go (build-tag _test) flips it.
var openAndSetnsSyscalls = openAndSetnsSyscallsT{
	open:  unix.Open,
	setns: unix.Setns,
	close: unix.Close,
}

type openAndSetnsSyscallsT struct {
	open  func(path string, flag int, perm uint32) (int, error)
	setns func(fd int, nstype int) error
	close func(fd int) error
}

// restoreNsSetns is the seam used by netNamespaceInstance's deferred
// restore. Same signature as unix.Setns; tests swap it to force
// restore failures and exercise the tainted-M code path without
// needing real CAP_SYS_ADMIN or live network namespaces.
var restoreNsSetns = unix.Setns

// attemptOpenAndSetns is one iteration of the retry loop. Returns:
//   - fd: the fd returned by Open. -1 on Open failure. On Setns failure
//     the fd has already been closed inside this helper, so the caller
//     must not close it again (Linux reuses fd numbers under load —
//     double-close lands on an unrelated socket).
//   - errOpen, errSetns: at most one is non-nil. errOpen != nil means
//     the iteration failed before any state was created. errSetns != nil
//     means the fd existed briefly and was closed.
func (x *XTCP) attemptOpenAndSetns(nsName *string) (fd int, errOpen, errSetns error) {
	fd, errOpen = openAndSetnsSyscalls.open(*nsName, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if errOpen != nil {
		x.pC.WithLabelValues("openAndSetNSWithRetries", "open", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("openAndSetNSWithRetries unix.Open err: %v", errOpen)
		}
		return fd, errOpen, nil
	}
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries after unix.Open: %s", *nsName)
	}

	// https://pkg.go.dev/golang.org/x/sys/unix#Setns
	// https://www.man7.org/linux/man-pages/man2/setns.2.html
	errSetns = openAndSetnsSyscalls.setns(fd, unix.CLONE_NEWNET)
	if errSetns == nil {
		return fd, nil, nil
	}
	x.pC.WithLabelValues("openAndSetNSWithRetries", "Setns", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries unix.Setns err: %v", errSetns)
	}
	if errC := openAndSetnsSyscalls.close(fd); errC != nil {
		x.pC.WithLabelValues("openAndSetNSWithRetries", "close", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("openAndSetNSWithRetries unix.Close errC: %v", errC)
		}
	}
	return fd, nil, errSetns
}

// backoffSleep sleeps 2^attempt * backoffFactor() between Setns retries.
// Skips attempt<=0 (no point sleeping before the first retry) and
// attempt>=maxRetriesCst (loop is about to terminate). Public visibility
// to allow benchmarks to drive it directly without the surrounding loop.
func (x *XTCP) backoffSleep(attempt int) {
	if attempt <= 0 || attempt >= maxRetriesCst {
		return
	}
	backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * backoffFactor()
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries  %d < %d, sleeping: %0.3f", attempt, maxRetriesCst, backoffDuration.Seconds())
	}
	time.Sleep(backoffDuration)
}

// openAndSetNSWithRetries
// opens the /run/netns/X directory, and then tries to run
// SetNs on it.  When ns.new == true, there is a slight
// sleep because it seems to take a moment for the kernel to
// recognize a new netns
//
// beware of bugs:
// https://github.com/iproute2/iproute2/blob/413cf4f03a9b6a219c94b86f41d67992b0a14b82/ip/ipnetns.c#L801
// https://bugs.debiax.org/cgi-bin/bugreport.cgi?bug=949235
//
// The body was previously a 17-cyclo retry loop that mixed Open + Setns +
// close-on-fail + backoff inline. The Open+Setns+close-on-fail step is
// now attemptOpenAndSetns; the backoff is backoffSleep. The remaining
// shell (gocyclo 6) is the orchestration: mount-info check, retry loop,
// success/exhaust return.
func (x *XTCP) openAndSetNSWithRetries(nsName *string) (fd int) {

	// https://www.man7.org/linux/man-pages/man2/opex.2.html
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries nsFullName: %s", *nsName)
	}

	found, err := x.checkMountInfoWithRetries(nsName)
	if err != nil || !found {
		// Named return fd is zero-valued = 0 = stdin. Returning that
		// would let the caller's closeFD(fd) close stdin on the next
		// line. Return -1 (invalid-fd sentinel) so closeFD errors out
		// cleanly via EBADF instead.
		return -1
	}

	for attempt := 0; attempt < maxRetriesCst; attempt++ {
		attemptFD, errOpen, errSetns := x.attemptOpenAndSetns(nsName)
		if errOpen != nil {
			// attemptFD is -1 on Open failure on Linux; pass it
			// through so the caller's closeFD path stays consistent.
			return attemptFD
		}
		if errSetns == nil {
			return attemptFD
		}
		x.backoffSleep(attempt)
	}

	x.pC.WithLabelValues("openAndSetNSWithRetries", "SetnsAfterRetries", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries unable to Setns:%s", *nsName)
	}
	// At this point the most recent Setns attempt's fd has already been
	// closed inside attemptOpenAndSetns. Returning that fd would let
	// the caller's deferred closeFD double-close it — and since Linux
	// reuses fd numbers, the second close could land on whatever
	// unrelated socket got that number in the meantime. Return -1 so
	// closeFD's Close errors out cleanly via EBADF + its counter, but
	// no real fd gets clobbered.
	return -1
}

// checkMountInfoWithRetries is a retry look with exponential backoff around checkMountInfo
func (x *XTCP) checkMountInfoWithRetries(nsName *string) (found bool, err error) {

	for attempt := 0; attempt < maxRetriesCst; attempt++ {

		exists, errC := x.checkMountInfo(nsName)
		if errC != nil {
			err = errC
			continue
		}

		if exists {
			found = true
			break
		}

		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * backoffFactor()
		if x.debugLevel > 10 {
			log.Printf("openAndSetNSWithRetries  %d < %d, sleeping: %0.3f", attempt, maxRetriesCst, backoffDuration.Seconds())
		}
		time.Sleep(backoffDuration)
	}

	return found, err
}

// checkMountInfo read proc mountinfo to check if the namespace is fully mounted
// this is to allow us to check if the network namespace is ready
// for us to open a netlink socket
// mountInfoDir = "/proc/self/mountinfo"
// https://www.man7.org/linux/man-pages/man5/proc_pid_mountinfo.5.html
func (x *XTCP) checkMountInfo(nsName *string) (bool, error) {

	x.pC.WithLabelValues("checkMountInfo", "start", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("checkMountInfo start: %s", *nsName)
	}

	file, err := os.Open(mountInfoDir)
	if err != nil {
		x.pC.WithLabelValues("checkMountInfo", "os.Open", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("checkMountInfo os.Open error: %v", err)
		}
		return false, fmt.Errorf("failed to open /proc/self/mountinfo: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, *nsName) {
			x.pC.WithLabelValues("checkMountInfo", "found", "count").Inc()
			if x.debugLevel > 10 {
				log.Printf("checkMountInfo found: %s", *nsName)
			}
			return true, nil
		}
	}
	x.pC.WithLabelValues("checkMountInfo", "notFound", "count").Inc()

	if err = scanner.Err(); err != nil {
		x.pC.WithLabelValues("checkMountInfo", "scanner.Err", "error").Inc()
		return false, fmt.Errorf("error reading /proc/self/mountinfo: %w", err)
	}

	return false, nil
}

// setSocketTimeoutViaSyscall sets a socket read timeout
// https://www.man7.org/linux/man-pages/man3/setsockopt.3p.html
// doing this so that netlinkers can close on their own, otherwise they will
// never stop being blocked on the read to the socketFD
func (x *XTCP) setSocketTimeoutViaSyscall(timeout int64, socketFD int) {

	if timeout == 0 {
		return
	}

	// timeout is in milliseconds. Decompose into seconds + leftover
	// microseconds so any value works. Previously the >=1000 branch
	// set only tv.Sec = timeout/1000 (dropping sub-second remainders:
	// 1500ms → 1s, 2500ms → 2s, etc). Match the matching xtcpnl helper.
	var tv syscall.Timeval
	tv.Sec = timeout / 1000
	tv.Usec = (timeout % 1000) * 1000

	// https://godoc.org/golang.org/x/sys/unix#SetsockoptTimeval
	err := syscall.SetsockoptTimeval(socketFD, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	if err != nil {
		// Demoted from log.Fatalf: this is called per-namespace from
		// createNetlinkersAndStore. A SO_RCVTIMEO setsockopt failure on
		// one namespace's fd shouldn't tear down the whole daemon (every
		// gRPC service, every other namespace's netlinkers, the poller).
		// Without the timeout the netlinker can't observe ctx
		// cancellation between recv calls — record that in a counter so
		// the operator can see the affected namespace can't shut down
		// cleanly, but keep the rest of the daemon running.
		x.pC.WithLabelValues("setSocketTimeoutViaSyscall", "SetsockoptTimeval", "error").Inc()
		if x.debugLevel > 10 {
			log.Printf("setSocketTimeoutViaSyscall SetsockoptTimeval err: %v", err)
		}
	}
}
