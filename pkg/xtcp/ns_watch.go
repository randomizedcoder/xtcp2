package xtcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
	fsnotify "gopkg.in/fsnotify.v1"
)

// watchNsNamespace sets up inotify to track namespaces being added
// and removed, and then with inotify in place, this function also calls
// discoverNamespaces() to read all the existing name spaces from "/run/netns/"
//
// if running in a k8s environment, an alternative approach would be to get
// events, like pod create/detele, from the API, but this would make
// xtcp specific to k8s, rather than more generic
func (x *XTCP) watchNsNamespace(ctx context.Context, wg *sync.WaitGroup, netNsDir string) error {

	defer wg.Done()

	x.pC.WithLabelValues("watchNamespaces", "start", "count").Inc()
	defer x.pC.WithLabelValues("watchNamespaces", "complete", "count").Inc()

	if err := x.ensureNetNSDir(netNsDir); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer func() {
		if cerr := watcher.Close(); cerr != nil {
			log.Printf("watchNsNamespace: watcher close: %v", cerr)
		}
	}()

	if err = watcher.Add(netNsDir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", netNsDir, err)
	}

	if x.debugLevel > 10 {
		log.Printf("Watching directory: %s", netNsDir)
	}

	for {
		x.pC.WithLabelValues("watchNamespaces", "for", "counter").Inc()
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.Events:
			if e := x.dispatchNsFsEvent(ctx, netNsDir, event, ok); e != nil {
				return e
			}
		case werr, ok := <-watcher.Errors:
			if e := x.handleNsWatcherErr(netNsDir, werr, ok); e != nil {
				return e
			}
		}
	}
}

// ensureNetNSDir creates linuxNetNSDirCst when missing. No-op for any
// other dir (e.g. tests' tempdirs, which the caller is responsible for
// creating). The previous body had this conditional inline as a triple-
// nested if; lifting it cuts watchNsNamespace's gocyclo by 3.
func (x *XTCP) ensureNetNSDir(netNsDir string) error {
	if netNsDir != linuxNetNSDirCst {
		return nil
	}
	if checkDirectoryExists(netNsDir) {
		return nil
	}
	if x.debugLevel > 10 {
		log.Printf("watchNamespaces %s no network namespace exists. Creating: %s", linuxNetNSDirCst, xtcpNSName)
	}
	return x.createNetworkNamespace(netNsDir, xtcpNSName)
}

// dispatchNsFsEvent handles one fsnotify.Event from watcher.Events.
// Returns nil to continue the watch loop, non-nil when the event
// channel itself has closed (caller propagates as the loop error).
func (x *XTCP) dispatchNsFsEvent(ctx context.Context, netNsDir string, event fsnotify.Event, ok bool) error {
	x.pC.WithLabelValues("watchNamespaces", "event", "counter").Inc()
	if !ok {
		x.pC.WithLabelValues("watchNamespaces", "watcherClose", "counter").Inc()
		return fmt.Errorf("watcher event channel closed")
	}
	nsName := event.Name
	if x.debugLevel > 10 {
		log.Printf("watchNamespaces %s event.Name: %v event.Op.String: %s nsName:%s", netNsDir, event.Name, event.Op.String(), nsName)
	}
	if event.Op&fsnotify.Create == fsnotify.Create {
		x.nsAdd(ctx, &nsName)
		return nil
	}
	if event.Op&fsnotify.Remove == fsnotify.Remove {
		x.nsDelete(&nsName)
	}
	return nil
}

// handleNsWatcherErr handles one error from watcher.Errors. Same return
// contract as dispatchNsFsEvent: non-nil only when the error channel
// has closed.
func (x *XTCP) handleNsWatcherErr(netNsDir string, werr error, ok bool) error {
	x.pC.WithLabelValues("watchNamespaces", "error", "error").Inc()
	if !ok {
		x.pC.WithLabelValues("watchNamespaces", "watcherCloseErr", "counter").Inc()
		return fmt.Errorf("watchNamespaces %s error channel closed", netNsDir)
	}
	if x.debugLevel > 10 {
		log.Printf("Watcher error: %v", werr)
	}
	return nil
}

// checkDirectoryExists checks if a directory exists.
//
// The previous body only special-cased ErrNotExist, then unconditionally
// dereferenced info — a non-not-exist Stat error (EACCES on a permission-
// restricted mount, EIO on a flaky filesystem) leaves info==nil and the
// info.IsDir() call panics. Treat any Stat error as "no" and only
// dereference info on the success path.
func checkDirectoryExists(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// createNetworkNamespace creates a Linux network namespace
// and binds it to a name in /run/netns
// this is a pure go implementation
// this is essentially what "ip netns add ns1" does under the hood
//
// Threading: unix.Unshare(CLONE_NEWNET) changes the calling OS THREAD's
// network namespace, but Go's scheduler can migrate the goroutine to a
// different thread at any syscall yield point. If migration happens
// between Unshare and the subsequent bind-mount, /proc/self/ns/net
// resolves to the wrong thread's namespace — silently creating a
// bind-mount pointing into the original (host) netns rather than the
// freshly-unshared one. Lock the OS thread for the duration so the
// goroutine can't migrate mid-sequence. We restore the original netns
// before returning so the caller's subsequent syscalls execute in the
// host's namespace, not the new one.
func (x *XTCP) createNetworkNamespace(netnsDir string, newNetNSName string) error {

	// #nosec G301 -- /run/netns is a system-managed namespace dir; 0755 is the standard `ip netns add` permission
	if err := os.MkdirAll(netnsDir, 0755); err != nil { //nolint:gosec // mirrored by the #nosec annotation above for the standalone gosec run
		return fmt.Errorf("failed to create directory %s: %w", netnsDir, err)
	}

	runtime.LockOSThread()
	// NB: NO `defer runtime.UnlockOSThread()` here on purpose. See the
	// matching pattern in netNamespaceInstance: if the deferred
	// restore-Setns fails, we *must not* unlock — handing a tainted M
	// back to Go's scheduler leaks OS threads up to SetMaxThreads. On
	// restore failure the goroutine exits with the lock still held;
	// Go's runtime then terminates the OS thread (documented
	// LockOSThread behavior) rather than recycling a tainted M.

	// Snapshot the calling thread's current netns so we can restore
	// after the unshare+bind-mount. Otherwise this goroutine's thread
	// stays in the new netns and the caller (watchNsNamespace) ends up
	// running its fsnotify loop in a different network namespace.
	origNs, errOrig := os.Open("/proc/thread-self/ns/net")
	if errOrig != nil {
		// snapshotOrigNs failed → can't restore → leave the lock held
		// so the runtime terminates this thread on goroutine exit
		// rather than recycling a thread that's about to be unshared
		// into a new netns with no way back.
		return fmt.Errorf("failed to snapshot original netns: %w", errOrig)
	}
	defer func() {
		if cerr := origNs.Close(); cerr != nil {
			log.Printf("createNetworkNamespace: origNs close: %v", cerr)
		}
	}()
	defer func() {
		// Restore on the way out; conditionally unlock only if the
		// restore actually succeeded.
		if rerr := restoreNsSetns(int(origNs.Fd()), unix.CLONE_NEWNET); rerr != nil {
			if x.debugLevel > 10 {
				log.Printf("createNetworkNamespace restore-netns err: %v (keeping thread locked → runtime will terminate it)", rerr)
			}
			return // skip UnlockOSThread → runtime terminates the OS thread
		}
		runtime.UnlockOSThread() //nolint:forbidigo // safe: only fires after Setns restore returned nil.
	}()

	// Create the network namespace using CLONE_NEWNET. Affects the
	// pinned thread only.
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("failed to create network namespace: %w", err)
	}

	// Open a file descriptor for the new namespace
	fd, err := os.Create(filepath.Join(netnsDir, newNetNSName))
	if err != nil {
		return fmt.Errorf("failed to create namespace file: %w", err)
	}
	defer fd.Close()

	// Bind-mount /proc/thread-self/ns/net (NOT /proc/self/ns/net) so
	// we explicitly reference the thread we unshared, not whichever
	// thread the runtime happens to schedule us on. The LockOSThread
	// above guarantees they are the same, but using thread-self makes
	// that assumption explicit at the syscall level.
	if err = syscall.Mount("/proc/thread-self/ns/net", fd.Name(), "none", syscall.MS_BIND, ""); err != nil {
		// Mount failed → the os.Create above leaves an empty,
		// non-bind-mounted file at fd.Name() that another process /
		// later retry would observe as "namespace exists but isn't a
		// real netns bind". Best-effort remove so the next attempt
		// starts clean. We're already on the error path; Remove err is
		// non-actionable.
		if rerr := os.Remove(fd.Name()); rerr != nil {
			log.Printf("createNetworkNamespace: cleanup remove %s after mount failure: %v", fd.Name(), rerr)
		}
		return fmt.Errorf("failed to bind namespace: %w", err)
	}

	if x.debugLevel > 10 {
		log.Printf("createNetworkNamespace bindmount complete")
	}

	return nil
}
