package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/tools/internal/sockopt"
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

	// networkCst "tcp" lets Go pick the family from -connect. tcp4 and tcp6
	// pin it.
	networkCst = "tcp"

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

// closeMode is how a client ends its connection after the exchange.
type closeMode int

const (
	// keepOpen exchanges a message every -sleep until an error (today's
	// behavior).
	keepOpen closeMode = iota
	// closeAfter closes after one exchange, leaving the socket in TIME_WAIT.
	closeAfter
	// halfClose shuts down writing after one exchange and holds the socket
	// until the peer closes: FIN_WAIT2 here, CLOSE_WAIT at a -holdEOF
	// server.
	halfClose
)

// clientConfig is the parsed command line.
type clientConfig struct {
	count      int
	connect    string
	sleep      time.Duration
	startsleep time.Duration
	wto        time.Duration
	rto        time.Duration
	dialr      int
	pads       int
	srcaddr    string
	iface      string
	network    string
	// dialTimeout is the first dial attempt's timeout; each retry after a
	// timeout waits one more of it.
	dialTimeout time.Duration
	bulk        sockopt.Size
	mode        closeMode
	opts        sockopt.Options
}

// parseFlags reads the command line into a clientConfig. Every error is
// also printed to stderr (flag errors with usage).
func parseFlags(args []string, stderr io.Writer) (clientConfig, error) {
	var c clientConfig
	fs := flag.NewFlagSet("tcp_client", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&c.count, "count", countCst, "count")
	fs.StringVar(&c.connect, "connect", connectCst, "connect")
	fs.DurationVar(&c.sleep, "sleep", sleepCst, "sleep between writes")
	fs.DurationVar(&c.startsleep, "startsleep", startsleepCst, "sleep between client starts")
	fs.DurationVar(&c.wto, "wto", writeTimeoutCst, "write time out")
	fs.DurationVar(&c.rto, "rto", readTimeoutCst, "read time out")
	fs.IntVar(&c.dialr, "dialr", dialRetryCst, "dial retries")
	fs.IntVar(&c.pads, "pads", padSizeCst, "pad size")
	fs.StringVar(&c.srcaddr, "srcaddr", srcaddrCst, "bind each connection's source IP (net.Dialer.LocalAddr); empty = kernel default")
	fs.StringVar(&c.iface, "iface", ifaceCst, "bind each connection to this interface via SO_BINDTODEVICE; empty = kernel default")
	fs.StringVar(&c.network, "network", networkCst, "tcp (family from -connect), tcp4 or tcp6")
	fs.DurationVar(&c.dialTimeout, "dialTimeout", dialTimeoutCst, "first dial attempt's timeout; longer than the kernel's SYN retries (about 2m) holds a SYN_SENT socket to an unanswering peer")
	fs.Var(&c.bulk, "bulk", "send this many bytes (e.g. 4MiB) and read the echo back before the first message; 0 = none")
	closeFlag := fs.Bool("close", false, "close after one exchange (the socket goes to TIME_WAIT)")
	halfFlag := fs.Bool("halfclose", false, "shutdown(SHUT_WR) after one exchange and hold the socket until the peer closes (FIN_WAIT2)")
	c.opts.Register(fs)
	if err := fs.Parse(args); err != nil {
		return clientConfig{}, err
	}
	err := c.opts.Validate()
	if c.dialTimeout <= 0 {
		err = errors.Join(err, fmt.Errorf("-dialTimeout %s must be positive", c.dialTimeout))
	}
	switch {
	case *closeFlag && *halfFlag:
		err = errors.Join(err, errors.New("-close and -halfclose are exclusive"))
	case *closeFlag:
		c.mode = closeAfter
	case *halfFlag:
		c.mode = halfClose
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return clientConfig{}, err
	}
	return c, nil
}

// runMain wires flag parsing + client fan-out. Extracted so tests can drive
// it with synthetic args (and count=0 makes the function a pure no-op fan-out).
func runMain(args []string, stderr io.Writer) int {
	c, err := parseFlags(args, stderr)
	if err != nil {
		return 2
	}
	if _, _, err := dialTarget(c.network, c.connect, c.srcaddr, startPort+c.count-1); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	d, err := newDialer(c.srcaddr, c.iface, c.opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// dialFailures counts clients that never established a connection. A
	// client that does connect only returns on a read/write error (or after
	// -close / -halfclose), so a non-zero count at the end means part of the
	// requested population was never created (bad -iface, unreachable peer,
	// …). Exit 1 in that case so a systemd Restart=on-failure unit retries
	// instead of sitting "active (exited)" with no sockets behind it.
	var dialFailures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < c.count; i++ {
		wg.Add(1)
		go client(&wg, &dialFailures, d, c, startPort+i)
		time.Sleep(c.startsleep)
	}
	wg.Wait()
	if n := dialFailures.Load(); n > 0 {
		fmt.Fprintf(stderr, "tcp_client: %d of %d clients never connected\n", n, c.count)
		return 1
	}
	return 0
}

// newDialer builds the net.Dialer used for every connection, applying the
// optional source-address bind (LocalAddr), interface bind (SO_BINDTODEVICE)
// and socket options. All are unset by default, yielding a zero-value Dialer
// identical to the previous behavior. A non-empty but unparseable srcaddr is
// a hard error so a misconfigured load container fails loudly rather than
// silently binding nothing.
func newDialer(srcaddr, iface string, opts sockopt.Options) (net.Dialer, error) {
	var d net.Dialer
	if srcaddr != "" {
		ip := net.ParseIP(srcaddr)
		if ip == nil {
			return net.Dialer{}, fmt.Errorf("invalid -srcaddr %q", srcaddr)
		}
		d.LocalAddr = &net.TCPAddr{IP: ip}
	}
	var bind func(string, string, syscall.RawConn) error
	if iface != "" {
		name := iface
		bind = func(_, _ string, c syscall.RawConn) error {
			var operr error
			if cerr := c.Control(func(fd uintptr) {
				operr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
			}); cerr != nil {
				return cerr
			}
			return operr
		}
	}
	if opts != (sockopt.Options{}) {
		d.Control = sockopt.Chain(bind, opts.Control)
	} else if bind != nil {
		d.Control = bind
	}
	return d, nil
}

func client(wg *sync.WaitGroup, dialFailures *atomic.Int32, d net.Dialer, c clientConfig, port int) {
	defer wg.Done()

	conn, err := dialWithRetryDialer(d, c.network, c.connect, port, c.dialr, c.dialTimeout)
	if err != nil {
		log.Printf("dialWithRetry: %v", err)
		dialFailures.Add(1)
		return
	}
	if err := session(conn, c, port); err != nil {
		log.Printf("session port=%d: %v", port, err)
	}
}

// session runs one connection: the optional bulk transfer, then message
// exchanges ended by c.mode. It closes conn.
func session(conn net.Conn, c clientConfig, port int) error {
	defer func() { _ = conn.Close() }()

	if _, err := bulk(conn, int64(c.bulk)); err != nil {
		return err
	}
	buf := buildMessage(port, c.pads)
	reply := make([]byte, readBufferSizeCst)
	for i := 0; ; i++ {
		if err := clientOnce(conn, buf, reply, c.wto, c.rto); err != nil {
			if errors.Is(err, ErrTimeout) {
				continue
			}
			return fmt.Errorf("clientOnce i=%d: %w", i, err)
		}
		fmt.Printf("received from server i:%d : [%s]\n", i, string(reply))
		switch c.mode {
		case closeAfter:
			return nil
		case halfClose:
			return holdHalfClosed(conn)
		}
		time.Sleep(c.sleep)
	}
}

// holdHalfClosed shuts down writing and reads until the peer closes.
func holdHalfClosed(conn net.Conn) error {
	tc, ok := conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("halfclose: %T is not TCP", conn)
	}
	if err := tc.CloseWrite(); err != nil {
		return fmt.Errorf("halfclose: %w", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return fmt.Errorf("halfclose: %w", err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		return fmt.Errorf("halfclose: read: %w", err)
	}
	return nil
}

// bulk writes n bytes and reads the echo back, returning the bytes read.
// The deadlines -wto/-rto set are for the small messages, so bulk clears
// them.
func bulk(conn net.Conn, n int64) (int64, error) {
	if n <= 0 {
		return 0, nil
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return 0, fmt.Errorf("bulk: %w", err)
	}
	werr := make(chan error, 1)
	go func() {
		_, err := io.CopyN(conn, zeroReader{}, n)
		werr <- err
	}()
	got, err := io.CopyN(io.Discard, conn, n)
	if err != nil {
		return got, fmt.Errorf("bulk: read %d of %d: %w", got, n, err)
	}
	if err := <-werr; err != nil {
		return got, fmt.Errorf("bulk: write: %w", err)
	}
	return got, nil
}

// zeroReader reads zeros.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// dialTarget returns the network and address a client dials for -network,
// -connect, -srcaddr and one port.
func dialTarget(network, host, srcaddr string, port int) (string, string, error) {
	if err := checkFamily(network, "-connect", host); err != nil {
		return "", "", err
	}
	if srcaddr != "" {
		if err := checkFamily(network, "-srcaddr", srcaddr); err != nil {
			return "", "", err
		}
	}
	if port < 1 || port > 65535 {
		return "", "", fmt.Errorf("port %d is outside 1-65535", port)
	}
	return network, net.JoinHostPort(host, strconv.Itoa(port)), nil
}

// checkFamily rejects a network other than tcp, tcp4 and tcp6, and an
// address literal of the other family for tcp4 or tcp6 (a host name is
// left to the resolver).
func checkFamily(network, flag, host string) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("-network: %q is not tcp, tcp4 or tcp6", network)
	}
	a, err := netip.ParseAddr(host)
	if err != nil || network == "tcp" {
		return nil
	}
	if network == "tcp4" && !a.Is4() {
		return fmt.Errorf("%s %s is not an IPv4 address, as -network tcp4 needs", flag, host)
	}
	if network == "tcp6" && a.Is4() {
		return fmt.Errorf("%s %s is not an IPv6 address, as -network tcp6 needs", flag, host)
	}
	return nil
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
	return dialWithRetryDialer(net.Dialer{}, networkCst, bind, port, attempts, baseTimeout)
}

// dialWithRetryDialer is dialWithRetry with a caller-supplied base Dialer (so the
// source-address / interface binds from newDialer are applied). It overrides only
// the per-attempt Timeout, leaving LocalAddr/Control intact across retries.
func dialWithRetryDialer(base net.Dialer, network, bind string, port, attempts int, baseTimeout time.Duration) (net.Conn, error) {
	network, addr, err := dialTarget(network, bind, "", port)
	if err != nil {
		return nil, err
	}
	if attempts <= 0 {
		return nil, fmt.Errorf("dial %s: attempts must be > 0, got %d", addr, attempts)
	}
	timeout := baseTimeout
	var lastErr error
	for r := 0; r < attempts; r++ {
		dialer := base
		dialer.Timeout = timeout
		dialCtx, cancel := context.WithTimeout(context.Background(), timeout)
		conn, err := dialer.DialContext(dialCtx, network, addr)
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
