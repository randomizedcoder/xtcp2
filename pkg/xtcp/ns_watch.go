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

	if netNsDir == linuxNetNSDirCst {
		if !checkDirectoryExists(netNsDir) {
			if x.debugLevel > 10 {
				log.Printf("watchNamespaces %s no network namespace exists. Creating: %s", linuxNetNSDirCst, xtcpNSName)
			}
			if err := x.createNetworkNamespace(netNsDir, xtcpNSName); err != nil {
				return err
			}
		}
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer func() { _ = watcher.Close() }() //nolint:errcheck // watcher.Close teardown; err non-actionable

	if err = watcher.Add(netNsDir); err != nil {
		return fmt.Errorf("failed to watch directory %s: %w", netNsDir, err)
	}

	if x.debugLevel > 10 {
		log.Printf("Watching directory: %s", netNsDir)
	}

breakPoint:
	for {
		x.pC.WithLabelValues("watchNamespaces", "for", "counter").Inc()

		select {

		case <-ctx.Done():
			break breakPoint

		case event, ok := <-watcher.Events:
			x.pC.WithLabelValues("watchNamespaces", "event", "counter").Inc()
			if !ok {
				x.pC.WithLabelValues("watchNamespaces", "watcherClose", "counter").Inc()
				return fmt.Errorf("watcher event channel closed")
			}

			// nsName := filepath.Base(event.Name)
			// nsName := netNsDir + event.Name
			nsName := event.Name

			if x.debugLevel > 10 {
				log.Printf("watchNamespaces %s event.Name: %v event.Op.String: %s nsName:%s", netNsDir, event.Name, event.Op.String(), nsName)
			}

			if event.Op&fsnotify.Create == fsnotify.Create {
				x.nsAdd(ctx, &nsName)
				continue
			}

			if event.Op&fsnotify.Remove == fsnotify.Remove {
				x.nsDelete(&nsName)
				continue
			}

		case werr, ok := <-watcher.Errors:
			x.pC.WithLabelValues("watchNamespaces", "error", "error").Inc()
			if !ok {
				x.pC.WithLabelValues("watchNamespaces", "watcherCloseErr", "counter").Inc()
				return fmt.Errorf("watchNamespaces %s error channel closed", netNsDir)
			}
			if x.debugLevel > 10 {
				log.Printf("Watcher error: %v", werr)
			}
		}
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
	defer runtime.UnlockOSThread()

	// Snapshot the calling thread's current netns so we can restore
	// after the unshare+bind-mount. Otherwise this goroutine's thread
	// stays in the new netns and the caller (watchNsNamespace) ends up
	// running its fsnotify loop in a different network namespace.
	origNs, errOrig := os.Open("/proc/thread-self/ns/net")
	if errOrig != nil {
		return fmt.Errorf("failed to snapshot original netns: %w", errOrig)
	}
	defer func() { _ = origNs.Close() }() //nolint:errcheck // restore-only fd
	defer func() {
		// Restore on the way out; if Setns fails the goroutine is
		// already pinned to this (modified) thread, so the failure
		// surfaces in the surrounding LockOSThread scope. We log
		// instead of returning because the primary work is done.
		if rerr := unix.Setns(int(origNs.Fd()), unix.CLONE_NEWNET); rerr != nil {
			if x.debugLevel > 10 {
				log.Printf("createNetworkNamespace restore-netns err: %v", rerr)
			}
		}
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
		return fmt.Errorf("failed to bind namespace: %w", err)
	}

	if x.debugLevel > 10 {
		log.Printf("createNetworkNamespace bindmount complete")
	}

	return nil
}
