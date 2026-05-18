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
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/proto"
)

const (
	portCst = 13000

	packetBufferSizeCst = 4096
)

var (
	// Passed by "go build -ldflags" for the show version
	commit string
	date   string
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + ListenUDP + runReceiver. Extracted so tests
// can drive it with a cancellable ctx + a captured stderr (instead of
// subprocessing). Returns the process exit code.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("udp_receiver_server", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", portCst, "UDP listen port")
	version := fs.Bool("version", false, "show version")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *version {
		fmt.Fprintf(stdout, "commit:%s\tdate(UTC):%s\n", commit, date)
		return 0
	}

	addr := net.UDPAddr{
		Port: *port,
		IP:   net.ParseIP("127.0.0.1"),
	}
	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		fmt.Fprintf(stderr, "ListenUDP: %v\n", err)
		return 1
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // demo server teardown

	if err := runReceiver(ctx, conn); err != nil {
		fmt.Fprintf(stderr, "runReceiver: %v\n", err)
		return 1
	}
	return 0
}

// ErrDecode wraps proto.Unmarshal errors so callers can distinguish them from
// I/O errors.
var ErrDecode = errors.New("proto decode")

// runReceiver loops Read+proto.Unmarshal on conn until ctx is canceled or
// the conn is closed. Each successfully-decoded record is printed; decode
// errors are returned (matching the original panic-on-decode behavior more
// gracefully). Extracted from main() so tests can drive it with a pair of
// in-process UDP sockets.
func runReceiver(ctx context.Context, conn *net.UDPConn) error {
	packetBufferPool := sync.Pool{
		New: func() any {
			b := make([]byte, packetBufferSizeCst)
			return &b
		},
	}
	xtcpRecordPool := sync.Pool{
		New: func() any {
			return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}

	packetBuffer, _ := packetBufferPool.Get().(*[]byte) //nolint:errcheck // pool.Get returns the type from pool.New
	defer packetBufferPool.Put(packetBuffer)
	xtcpRecord, _ := xtcpRecordPool.Get().(*xtcp_flat_record.Envelope_XtcpFlatRecord) //nolint:errcheck // pool.Get returns the type from pool.New
	defer xtcpRecordPool.Put(xtcpRecord)

	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		n, _, err := conn.ReadFromUDP(*packetBuffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("Error reading from UDP socket: %v", err)
			return err
		}

		if uerr := proto.Unmarshal((*packetBuffer)[:n], xtcpRecord); uerr != nil {
			return fmt.Errorf("%w: %v", ErrDecode, uerr)
		}

		fmt.Printf("Received i:%d, n:%d %v\n", i, n, xtcpRecord)
	}
}
