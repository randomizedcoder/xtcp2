package netlink

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func liveConn(t *testing.T, protocol int) *Conn {
	t.Helper()
	c, err := Open(context.Background(), protocol, nil)
	if err != nil {
		t.Fatalf("open real netlink socket: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("close real socket: %v", err)
		}
	})
	return c
}

func waitLive(t *testing.T, ready <-chan struct{}) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("reader did not reach EAGAIN")
	}
}

func signalIdle(c *Conn, ready chan<- struct{}) {
	recv := c.recv
	signaled := false
	c.recv = func(fd int, b, oob []byte, flags int) (int, int, int, unix.Sockaddr, error) {
		n, o, f, sa, err := recv(fd, b, oob, flags)
		if errors.Is(err, unix.EAGAIN) && !signaled {
			signaled = true
			ready <- struct{}{}
		}
		return n, o, f, sa, err
	}
}

func TestLiveSocketFlagsAndRoundTrip(t *testing.T) {
	c := liveConn(t, unix.NETLINK_ROUTE)
	var flags, fdFlags int
	var controlErr error
	if err := c.raw.Control(func(fd uintptr) {
		flags, controlErr = unix.FcntlInt(fd, unix.F_GETFL, 0)
		if controlErr == nil {
			fdFlags, controlErr = unix.FcntlInt(fd, unix.F_GETFD, 0)
		}
	}); err != nil || controlErr != nil {
		t.Fatalf("fcntl: %v %v", err, controlErr)
	}
	if flags&unix.O_NONBLOCK == 0 || fdFlags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("flags=%x fdFlags=%x", flags, fdFlags)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := xtcpnl.BuildDumpLinkRequestExt(unix.AF_UNSPEC, 0, 123)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Send(ctx, request); err != nil {
		t.Fatal(err)
	}
	done := errors.New("test dump complete")
	links := 0
	for {
		_, err = c.Receive(ctx, func(data []byte) error {
			return xtcpnl.WalkNetlinkEnvelopes(data, func(e xtcpnl.NetlinkEnvelope) error {
				if e.Header.Seq != 123 {
					return fmt.Errorf("unexpected sequence %d", e.Header.Seq)
				}
				if e.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
					return xtcpnl.ErrDumpInterrupted
				}
				switch e.Control {
				case xtcpnl.NetlinkDone:
					if e.Code != 0 {
						return fmt.Errorf("DONE code %d", e.Code)
					}
					return done
				case xtcpnl.NetlinkData:
					if e.Header.Type != unix.RTM_NEWLINK {
						return fmt.Errorf("unexpected type %d", e.Header.Type)
					}
					if _, parseErr := xtcpnl.ParseMonitorLink(e.Body, xtcpnl.MonitorLinkRequirements{Name: true}); parseErr != nil {
						return parseErr
					}
					links++
				case xtcpnl.NetlinkACK, xtcpnl.NetlinkNoop:
					return nil
				case xtcpnl.NetlinkError, xtcpnl.NetlinkOverrun:
					return fmt.Errorf("unexpected control %d, code %d", e.Control, e.Code)
				}
				return nil
			})
		})
		if errors.Is(err, done) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if links == 0 {
		t.Fatal("kernel dump omitted all links, including loopback")
	}
	t.Logf("read-only real kernel dump: %d links, authenticated recvmsg, successful DONE", links)
}

func TestLiveIdleCancellation(t *testing.T) {
	for _, row := range []struct {
		description   string
		explicitClose bool
		want          error
	}{
		{"cancel context closes and wakes idle reader", false, context.Canceled},
		{"explicit Close wakes idle reader", true, os.ErrClosed},
	} {
		t.Run(row.description, func(t *testing.T) {
			c := liveConn(t, unix.NETLINK_ROUTE)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan struct{}, 1)
			signalIdle(c, ready)
			result := make(chan error, 1)
			go func() {
				_, err := c.Receive(ctx, func([]byte) error { return errors.New("unexpected data on unsubscribed socket") })
				result <- err
			}()
			waitLive(t, ready)
			if row.explicitClose {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel()
			}
			select {
			case err := <-result:
				if !errors.Is(err, row.want) {
					t.Fatalf("error=%v want=%v", err, row.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("idle read did not wake")
			}
			if err := c.Send(context.Background(), []byte{1}); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("retired socket reusable: %v", err)
			}
		})
	}
}

func TestLiveDeadlineRetiresSocket(t *testing.T) {
	c := liveConn(t, unix.NETLINK_GENERIC)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Receive(ctx, func([]byte) error { return errors.New("unexpected unsolicited generic message") })
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("deadline error=%v", err)
	}
	if err := c.Send(context.Background(), []byte{1}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("deadline socket reusable: %v", err)
	}
}

func TestLiveIdleThreadBound(t *testing.T) {
	if os.Getenv("GO_LINK_MONITOR_POLLER_TEST_CHILD") == "1" {
		checkIdleThreadBound(t)
		return
	}
	// A subprocess bounds GOMAXPROCS without changing concurrent package tests.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestLiveIdleThreadBound$", "-test.v")
	cmd.Env = append(os.Environ(), "GO_LINK_MONITOR_POLLER_TEST_CHILD=1", "GOMAXPROCS=2")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("idle-thread subprocess: %v\n%s", err, out)
	}
	t.Log(string(out))
}

func procCount(t *testing.T, threads bool) int {
	t.Helper()
	if !threads {
		entries, err := os.ReadDir("/proc/self/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(entries)
	}
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "Threads:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "Threads:")))
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("Threads missing from proc status")
	return 0
}

func checkIdleThreadBound(t *testing.T) {
	const count = 64
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Initialize the runtime poller before measuring descriptors/threads.
	warm := liveConn(t, unix.NETLINK_ROUTE)
	if err := warm.Close(); err != nil {
		t.Fatal(err)
	}
	beforeFD, beforeThreads := procCount(t, false), procCount(t, true)
	ready, result := make(chan struct{}, count), make(chan error, count)
	conns := make([]*Conn, 0, count)
	for range count {
		c := liveConn(t, unix.NETLINK_ROUTE)
		conns = append(conns, c)
		signalIdle(c, ready)
		go func() {
			_, err := c.Receive(ctx, func([]byte) error { return errors.New("unexpected data") })
			result <- err
		}()
	}
	for range count {
		waitLive(t, ready)
	}
	afterThreads := procCount(t, true)
	if afterThreads-beforeThreads >= count/2 {
		t.Fatalf("idle readers consumed threads: before=%d after=%d sockets=%d", beforeThreads, afterThreads, count)
	}
	cancel()
	for range count {
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("shutdown: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("idle reader leaked")
		}
	}
	for _, c := range conns {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	afterFD := procCount(t, false)
	if afterFD != beforeFD {
		t.Fatalf("descriptor leak: before=%d after=%d", beforeFD, afterFD)
	}
	t.Logf("%d idle readers, GOMAXPROCS=2: threads %d -> %d; fds %d -> %d after close", count, beforeThreads, afterThreads, beforeFD, afterFD)
}
