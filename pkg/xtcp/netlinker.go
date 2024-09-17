// Package netlinker is the netlinker go routine of the xtcp package
//
// Netlinker recieves netlink packets from the kernel and passes
// to the worker queue
package xtcp

import (
	"context"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	writeFilesPermissionsCst = 0644

	forceGCModulesCst = 1000
)

func (x *XTCP) Netlinker(ctx context.Context, wg *sync.WaitGroup, id int) {

	defer wg.Done()

	if x.debugLevel > 10 {
		log.Printf("Netlinker %d started", id)
	}

	wf := *x.config.WriteFiles

	packetBuffer := x.packetBufferPool.Get().(*[]byte)

	for packets, netlinkerDone := 0, false; !netlinkerDone; packets++ {

		x.pC.WithLabelValues("Netlinker", "RecvfromCalls", "count").Inc()

		// keep in mind that via SetSocketTimeoutViaSyscall, the setsocket option
		// has set a read timeout to x.config.NLTimeout milliseconds

		startTime := time.Now()
		// // https://www.man7.org/linux/man-pages/man3/recvfrom.3p.html
		n, _, err := syscall.Recvfrom(x.socketFD, *packetBuffer, 0)

		x.pH.WithLabelValues("Netlinker", "Recvfrom", "count").Observe(time.Since(startTime).Seconds())

		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			x.pC.WithLabelValues("Netlinker", "Timeout", "count").Inc()
			netlinkerDone = x.CheckDoneNonBlocking(ctx)
			continue
		}
		if err != nil {
			x.pC.WithLabelValues("Netlinker", "nerr", "count").Inc()
			continue
		}

		if x.debugLevel > 10 {
			log.Printf("Netlinker %d Recvfrom packets:%d", id, packets)
		}

		x.pC.WithLabelValues("Netlinker", "packets", "count").Inc()
		x.pC.WithLabelValues("Netlinker", "n", "count").Add(float64(n))

		if wf > 0 {
			now := time.Now()
			err := os.WriteFile(
				*x.config.CapturePath+now.Format(time.RFC3339Nano),
				(*packetBuffer)[0:n],
				writeFilesPermissionsCst)
			if err != nil {
				log.Fatal(err)
			}
			wf--
		}

		b := (*(packetBuffer))[0:n]

		p, errD := x.Deserialize(
			ctx,
			DeserializeArgs{
				ctx:            ctx,
				NLPacket:       &b,
				xtcpRecordPool: &x.xtcpRecordPool,
				nlhPool:        &x.nlhPool,
				rtaPool:        &x.rtaPool,
				pC:             x.pC,
				pH:             x.pH,
				id:             id,
			})
		if errD != nil {
			x.pC.WithLabelValues("Netlinker", "ParseNLPacket", "error").Inc()
			continue
		}
		x.pC.WithLabelValues("Netlinker", "p", "count").Add(float64(p))

		if x.debugLevel > 10 {
			log.Printf("Netlinker %d packets:%d, n:%d, p:%d", id, packets, n, p)
		}

		netlinkerDone = x.CheckDoneNonBlocking(ctx)

		if packets%forceGCModulesCst == 0 {
			runtime.GC()
		}
	}

	x.packetBufferPool.Put(packetBuffer)

	x.pC.WithLabelValues("Netlinker", "complete", "count").Inc()

}

// IOURing notes

// https://pkg.go.dev/github.com/iceber/iouring-go@v0.0.0-20230403020409-002cfd2e2a90#Recv
//prep := iouring.Recv(x.socketFD, *packetBuffer, 0)

// if _, err := x.iour.SubmitRequest(prep, x.resulter); err != nil {
// 	log.Panicf("submit read request error: %v", err)
// }
// var n int
// for read := false, !read; {
// 	result := <-resulter
// 	switch result.Opcode() {

// 	case iouring.OpRead:
// 		x.pC.WithLabelValues("Netlinker", "resultOpRead", "count").Inc()
// 		n := result.ReturnValue0().(int)
// 		buf, _ := result.GetRequestBuffer()
// 		content := buf[:num]

// 	case iouring.OpWrite:
// 		x.pC.WithLabelValues("Netlinker", "resultOpWrite", "count").Inc()

// 	}
// }

// select {
// case x.packetCh <- p:
// 	x.pC.WithLabelValues("Netlinker", "packetsSent", "count").Inc()
// default:
// 	blockedStartTime := time.Now()
// 	x.packetCh <- p
// 	blockedEndTime := time.Now()
// 	x.pC.WithLabelValues("Netlinker", "blockedCh", "error").Inc()
// 	x.pH.WithLabelValues("Netlinker", "blocked", "error").Observe(blockedEndTime.Sub(blockedStartTime).Seconds())
// }
