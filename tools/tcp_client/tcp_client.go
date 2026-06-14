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
	"time"
)

const (
	startPort = 4000

	countCst = 10

	connectCst = "0.0.0.0"

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
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var wg sync.WaitGroup
	for i := 0; i < *count; i++ {
		wg.Add(1)
		go client(&wg, *connect, startPort+i, *sleep, *wto, *rto, *dialr, *pads)
		time.Sleep(*startsleep)
	}
	wg.Wait()
	return 0
}

func client(wg *sync.WaitGroup,
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

	conn, err := dialWithRetry(bind, port, dialr, dialTimeoutCst)
	if err != nil {
		log.Printf("dialWithRetry: %v", err)
		return
	}

	defer func() { _ = conn.Close() }() //nolint:errcheck // demo client teardown

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
func dialWithRetry(bind string, port, attempts int, baseTimeout time.Duration) (net.Conn, error) {
	timeout := baseTimeout
	addr := fmt.Sprintf("%s:%d", bind, port)
	var lastErr error
	for r := 1; r < attempts; r++ {
		dialer := net.Dialer{Timeout: timeout}
		dialCtx, cancel := context.WithTimeout(context.Background(), timeout)
		conn, err := dialer.DialContext(dialCtx, "tcp", addr)
		cancel()
		if err == nil {
			return conn, nil
		}
		lastErr = err
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			timeout = baseTimeout + (baseTimeout * time.Duration(r))
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
	_ = conn.SetWriteDeadline(time.Now().Add(wto)) //nolint:errcheck // deadline err surfaces on next Write
	if _, err := conn.Write(buf); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ErrTimeout
		}
		return fmt.Errorf("write: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(rto)) //nolint:errcheck // deadline err surfaces on next Read
	if _, err := conn.Read(reply); err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return ErrTimeout
		}
		return fmt.Errorf("read: %w", err)
	}
	return nil
}
