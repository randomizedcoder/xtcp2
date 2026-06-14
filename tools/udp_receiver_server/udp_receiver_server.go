package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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

	port := flag.Int("port", portCst, "UDP listen port")

	version := flag.Bool("version", false, "show version")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *version {
		log.Println("commit:", commit, "\tdate(UTC):", date)
		os.Exit(0)
	}

	addr := net.UDPAddr{
		Port: *port,
		IP:   net.ParseIP("0.0.0.0"),
	}

	conn, err := net.ListenUDP("udp", &addr)
	if err != nil {
		log.Fatalf("Error listening on UDP socket: %v", err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // demo server teardown

	log.Printf("Listening for UDP packets on 0.0.0.0:%d", *port)

	if err := runReceiver(context.Background(), conn); err != nil {
		log.Fatalf("runReceiver: %v", err)
	}
}

// ErrDecode wraps proto.Unmarshal errors so callers can distinguish them from
// I/O errors.
var ErrDecode = errors.New("proto decode")

// runReceiver loops Read+proto.Unmarshal on conn until ctx is cancelled or
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
