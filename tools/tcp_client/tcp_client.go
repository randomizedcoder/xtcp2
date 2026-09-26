package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	startPort = 4000

	countCst = 10

	connectCst = "0.0.0.0"

	// srcaddrCst / ifaceCst are empty by default so the client behaves exactly as
	// before: the kernel picks the source address and egress interface by route.
	// When set they bind each connection's source, which xtcp2 reads back as the
	// socket's local address / bound interface (idiag_if) for interface-name
	// enrichment testing.
	srcaddrCst = ""
	ifaceCst   = ""

	writeTimeoutCst = 100 * time.Millisecond
	readTimeoutCst  = 100 * time.Millisecond

	sleepCst = 2 * time.Second

	startsleepCst = 50 * time.Millisecond

	// had to increase this when creating 10k+ sockets
	dialTimeoutCst = 1000 * time.Millisecond

	dialRetryCst = 10

	readBufferSizeCst = 3000
	padSizeCst        = 2048
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stderr))
}

// runMain wires flag parsing + client fan-out. Extracted so tests can drive
// it with synthetic args (and count=0 makes the function a pure no-op fan-out).
func runMain(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("tcp_client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	count := fs.Int("count", countCst, "count")
	connect := fs.String("connect", connectCst, "connect")
	sleep := fs.Duration("sleep", sleepCst, "sleep between writes")
	startsleep := fs.Duration("startsleep", startsleepCst, "sleep between client starts")
	wto := fs.Duration("wto", writeTimeoutCst, "write time out")
	rto := fs.Duration("rto", readTimeoutCst, "read time out")
	dialr := fs.Int("dialr", dialRetryCst, "dial retries")
	pads := fs.Int("pads", padSizeCst, "pad size")
	srcaddr := fs.String("srcaddr", srcaddrCst, "bind each connection's source IP (net.Dialer.LocalAddr); empty = kernel default")
	iface := fs.String("iface", ifaceCst, "bind each connection to this interface via SO_BINDTODEVICE; empty = kernel default")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	d, err := newDialer(*srcaddr, *iface)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// dialFailures counts clients that never established a connection. A
	// client that does connect only returns on a read/write error, so a
	// non-zero count at the end means part of the requested population was
	// never created (bad -iface, unreachable peer, …). Exit 1 in that case so
	// a systemd Restart=on-failure unit retries instead of sitting "active
	// (exited)" with no sockets behind it.
	var dialFailures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < *count; i++ {
		wg.Add(1)
		go client(&wg, &dialFailures, d, *connect, startPort+i, *sleep, *wto, *rto, *dialr, *pads)
		time.Sleep(*startsleep)
	}
	wg.Wait()
	if n := dialFailures.Load(); n > 0 {
		fmt.Fprintf(stderr, "tcp_client: %d of %d clients never connected\n", n, *count)
		return 1
	}
	return 0
}

// newDialer builds the net.Dialer used for every connection, applying the
// optional source-address bind (LocalAddr) and interface bind (SO_BINDTODEVICE).
// Both are empty by default, yielding a zero-value Dialer identical to the
// previous behavior. A non-empty but unparseable srcaddr is a hard error so a
// misconfigured load container fails loudly rather than silently binding nothing.
func newDialer(srcaddr, iface string) (net.Dialer, error) {
	var d net.Dialer
	if srcaddr != "" {
		ip := net.ParseIP(srcaddr)
		if ip == nil {
			return net.Dialer{}, fmt.Errorf("invalid -srcaddr %q", srcaddr)
		}
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	if iface != "" {
		name := iface
		d.Control = func(_, _ string, c syscall.RawConn) error {
			var operr error
			if cerr := c.Control(func(fd uintptr) {
				operr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
			}); cerr != nil {
				return cerr
			}
			return operr
		}
	}
	return d, nil
}

func client(wg *sync.WaitGroup,
	dialFailures *atomic.Int32,
	d net.Dialer,
	bind string,
	port int,
	sleep time.Duration,
	wto time.Duration,
	rto time.Duration,
	dialr int,
	pads int,
) {

	defer wg.Done()

	buf := buildMessage(port, pads)
	reply := make([]byte, readBufferSizeCst)

	conn, err := dialWithRetryDialer(d, bind, port, dialr, dialTimeoutCst)
	if err != nil {
		log.Printf("dialWithRetry: %v", err)
		dialFailures.Add(1)
		return
	}

	defer func() {
		if cerr := conn.Close(); cerr != nil {
			log.Printf("client: conn close: %v", cerr)
		}
	}()

	for i := 0; ; i++ {
		if err := clientOnce(conn, buf, reply, wto, rto); err != nil {
			if errors.Is(err, ErrTimeout) {
				continue
			}
			log.Printf("clientOnce i=%d: %v", i, err)
			return
		}
		fmt.Printf("received from server i:%d : [%s]\n", i, string(reply))
		time.Sleep(sleep)
	}
}

// ErrTimeout is the sentinel returned by clientOnce when the underlying
// Read/Write deadline fires, signaling "retry next iteration".
var ErrTimeout = errors.New("net deadline")

// buildMessage assembles the per-client send buffer: "clientPORT" + pads of
// zero padding, sized so the receiver tells us apart by port.
func buildMessage(port, pads int) []byte {
	msg := fmt.Appendf(nil, "client%d", port)
	pad := make([]byte, pads)
	return slices.Concat(msg, pad)
}

// dialWithRetry retries dial up to `attempts` times with linearly-increasing
// timeout. Returns the first successful conn or the last non-timeout error.
//
// The previous loop bound was `for r := 1; r < attempts; r++` — one short
// of the advertised count (attempts=10 ran 9 iterations). With attempts=1
// the loop didn't run at all, lastErr stayed nil, and the function
// returned a nonsense "dial X:Y: %!w(<nil>)" error. Loop r := 0..attempts-1
// matches the comment, and if attempts <= 0 we report it instead of
// pretending we ran a loop.
func dialWithRetry(bind string, port, attempts int, baseTimeout time.Duration) (net.Conn, error) {
	return dialWithRetryDialer(net.Dialer{}, bind, port, attempts, baseTimeout)
}

// dialWithRetryDialer is dialWithRetry with a caller-supplied base Dialer (so the
// source-address / interface binds from newDialer are applied). It overrides only
// the per-attempt Timeout, leaving LocalAddr/Control intact across retries.
func dialWithRetryDialer(base net.Dialer, bind string, port, attempts int, baseTimeout time.Duration) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", bind, port)
	if attempts <= 0 {
		return nil, fmt.Errorf("dial %s: attempts must be > 0, got %d", addr, attempts)
	}
	timeout := baseTimeout
	var lastErr error
	for r := 0; r < attempts; r++ {
		dialer := base
		dialer.Timeout = timeout
		dialCtx, cancel := context.WithTimeout(context.Background(), timeout)
		conn, err := dialer.DialContext(dialCtx, "tcp", addr)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			timeout = baseTimeout + (baseTimeout * time.Duration(r+1))
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("dial %s: %w", addr, lastErr)
}

// clientOnce performs one write+read round-trip against the open conn,
// applying separate write/read deadlines. Returns ErrTimeout on a deadline
// hit (caller decides whether to retry) or the underlying I/O error.
func clientOnce(conn net.Conn, buf, reply []byte, wto, rto time.Duration) error {
	if derr := conn.SetWriteDeadline(time.Now().Add(wto)); derr != nil {
		return fmt.Errorf("set write deadline: %w", derr)
	}
	if _, err := conn.Write(buf); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ErrTimeout
		}
		return fmt.Errorf("write: %w", err)
	}

	if derr := conn.SetReadDeadline(time.Now().Add(rto)); derr != nil {
		return fmt.Errorf("set read deadline: %w", derr)
	}
	if _, err := conn.Read(reply); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ErrTimeout
		}
		return fmt.Errorf("read: %w", err)
	}
	return nil
}
