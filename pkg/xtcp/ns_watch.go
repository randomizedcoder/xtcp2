package xtcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
	fsnotify "gopkg.in/fsnotify.v1"
)

// watchNsNamespace sets up inotify to track namespaces being added
// and removed, and then with inotify in place, this function also calls
// discoverNamespaces() to read all the existing name spaces from "/run/netns/"
//
// if running in a k8s environment, an alterantive approach would be to get
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
	defer watcher.Close()

	if err := watcher.Add(netNsDir); err != nil {
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

			//nsName := filepath.Base(event.Name)
			//nsName := netNsDir + event.Name
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

		case err, ok := <-watcher.Errors:
			x.pC.WithLabelValues("watchNamespaces", "error", "error").Inc()
			if !ok {
				x.pC.WithLabelValues("watchNamespaces", "watcherCloseErr", "counter").Inc()
				return fmt.Errorf("watchNamespaces %s error channel closed", netNsDir)
			}
			if x.debugLevel > 10 {
				log.Printf("Watcher error: %v", err)
			}
		}
	}

	return nil
}

// checkDirectoryExists checks if a directory exists
func checkDirectoryExists(dir string) bool {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return false
	}
	return info.IsDir()
}

// createNetworkNamespace creates a Linux network namespace
// and binds it to a name in /run/netns
// this is a pure go implmentation
// this is essentially what "ip netnsd add ns1" does under the hood
func (x *XTCP) createNetworkNamespace(netnsDir string, newNetNSName string) error {

	if err := os.MkdirAll(netnsDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", netnsDir, err)
	}

	// Create the network namespace using CLONE_NEWNET
	if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
		return fmt.Errorf("failed to create network namespace: %w", err)
	}

	// Open a file descriptor for the new namespace
	fd, err := os.Create(filepath.Join(netnsDir, newNetNSName))
	if err != nil {
		return fmt.Errorf("failed to create namespace file: %w", err)
	}
	defer fd.Close()

	// Use syscall to bind the namespace to the file
	if err := syscall.Mount("/proc/self/ns/net", fd.Name(), "none", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("failed to bind namespace: %w", err)
	}

	if x.debugLevel > 10 {
		log.Printf("createNetworkNamespace bindmount complete")
	}

	return nil
}
