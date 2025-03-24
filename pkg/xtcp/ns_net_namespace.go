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
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	mountInfoDir = "/proc/self/mountinfo"
)

// netNamespaceInstance runs as a goroutine, and moves the thread
// into a network namespace, opens a netlink socket, and passes
// the socketFD back to the creator of this goroutine
// then this goroutine blocks, waiting to be cancelled
// https://pkg.go.dev/github.com/vishvananda/netns#GetFromName
// https://pkg.go.dev/github.com/vishvananda/netns#GetFromPath
// https://tip.golang.org/doc/go1.10#runtime
func (x *XTCP) netNamespaceInstance(ctx context.Context, nsName *string) {

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
	defer runtime.UnlockOSThread()

	// if x.debugLevel > 10 {
	// 	log.Printf("netNamespaceInstance after LockOSThread: %s", ns.name)
	// }

	fd := x.openAndSetNSWithRetries(nsName)

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
		//log.Fatalf("netNamespaceInstance unix.Socket %s", err)
		return
	}

	defer x.closeSocket(socketFD)

	// https://godoc.org/golang.org/x/sys/unix#Bind
	// https://godoc.org/golang.org/x/sys/unix#SockaddrNetlink
	err = unix.Bind(socketFD, &unix.SockaddrNetlink{Family: syscall.AF_NETLINK})
	if err != nil {
		if x.debugLevel > 10 {
			log.Printf("netNamespaceInstance unix.Bind err: %v", err)
		}
		log.Fatalf("netNamespaceInstance unix.Bind %s", err)
	}

	x.createNetlinkersAndStore(ctx, nsName, socketFD)

	x.pH.WithLabelValues("netNamespaceInstance", "store", "counter").Observe(time.Since(startTime).Seconds())

	x.closeFD(fd)

	// block waiting for done
	<-ctx.Done()

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
	x.createNetlinkersAndStore(ctx, &df, socketFD)

	if x.debugLevel > 10 {
		log.Printf("openDefaultNetLinkSocket default net namespace netlink socket stored")
	}
}

const (
	maxRetriesCst    = 10
	backoffFactorCst = 10 * time.Millisecond
)

// openAndSetNSWithRetries
// opens the /run/netns/X directory, and then tries to run
// SetNs on it.  When ns.new == true, there is a slight
// sleep because it seems to take a moment for the kernel to
// recognize a new netns
//
// beware of bugs:
// https://github.com/iproute2/iproute2/blob/413cf4f03a9b6a219c94b86f41d67992b0a14b82/ip/ipnetns.c#L801
// https://bugs.debiax.org/cgi-bin/bugreport.cgi?bug=949235
func (x *XTCP) openAndSetNSWithRetries(nsName *string) (fd int) {

	// https://www.man7.org/linux/man-pages/man2/opex.2.html
	//nsFullName := netnsDir + *ns.name
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries nsFullName: %s", *nsName)
	}

	found, err := x.checkMountInfoWithRetries(nsName)
	if err != nil || !found {
		return
	}

	for attempt := 0; attempt < maxRetriesCst; attempt++ {

		var err error
		fd, err = unix.Open(*nsName, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			x.pC.WithLabelValues("openAndSetNSWithRetries", "open", "error").Inc()
			if x.debugLevel > 10 {
				log.Printf("openAndSetNSWithRetries unix.Open err: %v", err)
			}
			return fd
		}

		if x.debugLevel > 10 {
			log.Printf("openAndSetNSWithRetries after unix.Open: %s", *nsName)
		}

		// https://pkg.go.dev/golang.org/x/sys/unix#Setns
		// https://cs.opensource.google/go/x/sys/+/refs/tags/v0.28.0:unix/zsyscall_linux.go;l=1533
		// https://www.man7.org/linux/man-pages/man2/setns.2.html
		errS := unix.Setns(fd, unix.CLONE_NEWNET)

		if errS != nil {

			x.pC.WithLabelValues("openAndSetNSWithRetries", "Setns", "error").Inc()
			if x.debugLevel > 10 {
				log.Printf("openAndSetNSWithRetries unix.Setns err: %v", errS)
			}

			errC := unix.Close(fd)
			if errC != nil {
				x.pC.WithLabelValues("openAndSetNSWithRetries", "close", "error").Inc()
				if x.debugLevel > 10 {
					log.Printf("openAndSetNSWithRetries unix.Close errC: %v", errC)
				}
			}

			if attempt > 0 {
				if attempt < maxRetriesCst {
					backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * backoffFactorCst
					if x.debugLevel > 10 {
						log.Printf("openAndSetNSWithRetries  %d < %d, sleeping: %0.3f", attempt, maxRetriesCst, backoffDuration.Seconds())
					}
					time.Sleep(backoffDuration)
				}
			}
		}

		// SUCCESS PATH
		if errS == nil {
			return fd
		}
	}

	x.pC.WithLabelValues("openAndSetNSWithRetries", "SetnsAfterRetries", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("openAndSetNSWithRetries unable to Setns:%s", *nsName)
	}
	return fd
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

		backoffDuration := time.Duration(math.Pow(2, float64(attempt))) * backoffFactorCst
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

	if err := scanner.Err(); err != nil {
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

	var tv syscall.Timeval
	if timeout >= 1000 {
		// seconds
		tv.Sec = timeout / 1000
	} else {
		// milliseconds
		tv.Usec = timeout * 1000 // microsecond or 1 millionth of a second.  1 milliseconds = 1000 micro
	}

	// https://godoc.org/golang.org/x/sys/unix#SetsockoptTimeval
	err := syscall.SetsockoptTimeval(socketFD, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv)
	if err != nil {
		log.Fatalf("SetSocketTimeoutViaSyscall SetsockopttimeSpec %s", err)
	}
}
