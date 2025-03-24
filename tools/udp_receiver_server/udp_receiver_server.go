package main

import (
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
	defer conn.Close()

	log.Printf("Listening for UDP packets on 0.0.0.0:%d", *port)

	packetBufferPool := sync.Pool{
		New: func() interface{} {
			b := make([]byte, packetBufferSizeCst)
			return &b
		},
	}

	xtcpRecordPool := sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}

	packetBuffer := packetBufferPool.Get().(*[]byte)
	defer packetBufferPool.Put(packetBuffer)

	xtcpRecord := xtcpRecordPool.Get().(*xtcp_flat_record.Envelope_XtcpFlatRecord)
	defer xtcpRecordPool.Put(xtcpRecord)

	for i := 0; ; i++ {
		n, _, err := conn.ReadFromUDP(*packetBuffer)
		if err != nil {
			log.Printf("Error reading from UDP socket: %v", err)
			continue
		}

		err = proto.Unmarshal((*packetBuffer)[:n], xtcpRecord)
		if err != nil {
			panic(err)
		}

		//fmt.Printf("Received i:%d, %d bytes from %s: %s\n", i, n, remoteAddr, string((*packetBuffer)[:n]))
		fmt.Printf("Received i:%d, n:%d %v\n", i, n, xtcpRecord)
	}

}
