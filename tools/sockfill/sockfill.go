// sockfill holds TCP sockets open in its network namespace, for tests that
// need a namespace with a known, large socket table (the socket-scale microVM).
//
// Modes:
//
//	-pairs N   open N loopback connections (2N sockets: the client end and
//	           the accepted end), print SOCKFILL_READY, and hold them until
//	           SIGTERM/SIGINT. Clients are spread over ceil(N/-perPort)
//	           listener ports so the ephemeral range doesn't run out; set
//	           net.ipv4.ip_local_port_range wide enough for -perPort.
//	-listen A  listen on A and never accept (a SYN flood target: half-open
//	           connections stay in SYN_RECV). The backlog is the namespace's
//	           net.core.somaxconn.
//
// Keepalive is off on every connection, so the sockets stay idle and
// established for as long as they are held.
//
// On exit every socket is closed with SO_LINGER 0, so it is reset rather than
// left in TIME_WAIT: TIME_WAIT sockets stay in the host-wide TCP table and
// would skew the next measurement.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultPerPort    = 50000
	defaultListenBase = 2000
	defaultDialers    = 128
	fdMargin          = 256

	// noKeepAlive turns off the TCP keepalive Go enables by default (15s
	// idle, 15s interval, 9 probes). At 500k connections that is ~33k
	// probes a second of background traffic; probes lost while the host is
	// busy (a full dump of the table) reset idle connections to CLOSE.
	noKeepAlive = -1
)

// listenersNeeded is how many listener ports N client connections need when
// one port can take perPort of them.
func listenersNeeded(pairs, perPort int) int {
	if pairs <= 0 || perPort <= 0 {
		return 0
	}
	return (pairs + perPort - 1) / perPort
}

// fdsNeeded is the RLIMIT_NOFILE pairs mode needs: two fds per pair, one per
// listener, and a margin.
func fdsNeeded(pairs, perPort int) uint64 {
	return uint64(2*pairs+listenersNeeded(pairs, perPort)) + fdMargin
}

// raiseNofile lifts the soft (and, if needed, hard) RLIMIT_NOFILE to need.
func raiseNofile(need uint64) error {
	var r unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &r); err != nil {
		return err
	}
	if r.Cur >= need {
		return nil
	}
	r.Cur = need
	if r.Max < need {
		r.Max = need
	}
	return unix.Setrlimit(unix.RLIMIT_NOFILE, &r)
}

// filler holds every socket it opened.
type filler struct {
	mu        sync.Mutex
	listeners []net.Listener
	conns     []*net.TCPConn
}

func (f *filler) add(c net.Conn) {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		closeQuiet(c)
		return
	}
	f.mu.Lock()
	f.conns = append(f.conns, tc)
	f.mu.Unlock()
}

// count is the number of connection sockets held (both ends).
func (f *filler) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.conns)
}

// fill opens pairs loopback connections on host, over listener ports from
// base. It returns once every client is connected and every connection
// accepted.
func (f *filler) fill(ctx context.Context, host string, base, pairs, perPort, dialers int) error {
	n := listenersNeeded(pairs, perPort)
	addrs := make([]string, 0, n)
	accepted := make(chan error, n)
	for i := 0; i < n; i++ {
		lc := net.ListenConfig{KeepAlive: noKeepAlive}
		l, err := lc.Listen(ctx, "tcp4", net.JoinHostPort(host, strconv.Itoa(base+i)))
		if err != nil {
			return fmt.Errorf("listen %d: %w", base+i, err)
		}
		f.listeners = append(f.listeners, l)
		addrs = append(addrs, l.Addr().String())
		want := min(perPort, pairs-i*perPort)
		go func() {
			for j := 0; j < want; j++ {
				c, err := l.Accept()
				if err != nil {
					accepted <- err
					return
				}
				f.add(c)
			}
			accepted <- nil
		}()
	}

	jobs := make(chan string)
	errs := make(chan error, dialers)
	var wg sync.WaitGroup
	for w := 0; w < dialers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := net.Dialer{KeepAlive: noKeepAlive}
			for a := range jobs {
				c, err := d.DialContext(ctx, "tcp4", a)
				if err != nil {
					errs <- fmt.Errorf("dial %s: %w", a, err)
					return
				}
				f.add(c)
			}
		}()
	}
	var dialErr error
feed:
	for i := 0; i < pairs; i++ {
		select {
		case jobs <- addrs[i/perPort]:
		case dialErr = <-errs:
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if dialErr == nil {
		select {
		case dialErr = <-errs:
		default:
		}
	}
	if dialErr != nil {
		return dialErr
	}
	for i := 0; i < n; i++ {
		if err := <-accepted; err != nil {
			return fmt.Errorf("accept: %w", err)
		}
	}
	return nil
}

// closeQuiet closes c; a close error on teardown changes nothing here.
func closeQuiet(c io.Closer) { _ = c.Close() }

// closeAll resets every connection (SO_LINGER 0: no TIME_WAIT) and closes
// the listeners.
func (f *filler) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		if err := c.SetLinger(0); err != nil {
			fmt.Fprintf(os.Stderr, "sockfill: SO_LINGER: %v\n", err)
		}
		closeQuiet(c)
	}
	f.conns = nil
	for _, l := range f.listeners {
		closeQuiet(l)
	}
	f.listeners = nil
}

func main() {
	pairs := flag.Int("pairs", 0, "loopback connections to open (2 sockets each)")
	perPort := flag.Int("perPort", defaultPerPort, "connections per listener port; must fit the ephemeral port range")
	base := flag.Int("listenBase", defaultListenBase, "first listener port (keep it below the ephemeral range)")
	host := flag.String("host", "127.0.0.1", "loopback address to use")
	dialers := flag.Int("dialers", defaultDialers, "concurrent dialers")
	listen := flag.String("listen", "", "listen on this address and never accept (SYN flood target)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	err := run(ctx, *pairs, *perPort, *base, *dialers, *host, *listen)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "SOCKFILL_ERR %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, pairs, perPort, base, dialers int, host, listen string) error {
	f := &filler{}
	defer f.closeAll()
	start := time.Now()
	switch {
	case listen != "":
		lc := net.ListenConfig{KeepAlive: noKeepAlive}
		l, err := lc.Listen(ctx, "tcp4", listen)
		if err != nil {
			return err
		}
		f.listeners = append(f.listeners, l)
		fmt.Printf("SOCKFILL_READY listen=%s\n", l.Addr())
	case pairs > 0:
		if perPort <= 0 || dialers <= 0 {
			return errors.New("-perPort and -dialers must be positive")
		}
		if err := raiseNofile(fdsNeeded(pairs, perPort)); err != nil {
			return fmt.Errorf("RLIMIT_NOFILE %d: %w", fdsNeeded(pairs, perPort), err)
		}
		if err := f.fill(ctx, host, base, pairs, perPort, dialers); err != nil {
			return err
		}
		fmt.Printf("SOCKFILL_READY pairs=%d sockets=%d listeners=%d elapsed=%s\n",
			pairs, f.count(), len(f.listeners), time.Since(start).Round(time.Millisecond))
	default:
		return errors.New("one of -pairs or -listen is required")
	}
	<-ctx.Done()
	return nil
}
