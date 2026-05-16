// Netlinker is the per-namespace goroutine that receives netlink packets
// from the kernel and feeds the deserializer. The function-pointer
// x.Netlinker (registered in pkg/xtcp/init_netlinkers.go) is one of:
//
//	netlinkerSyscall  — the original synchronous syscall.Recvfrom path.
//	netlinkerIoUring  — opt-in io_uring path with batched recvmsg SQEs.
//
// Selection happens at init time from config.IoUring. Same dispatch
// pattern as Marshaller/Destination (sync.Map of closures + chosen
// function pointer on XTCP).

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
	forceGCModulesCst        = 1000
)

// NetlinkerFunc is the signature of a per-fd netlinker goroutine. The
// chosen variant is stored in x.Netlinker (sync.Map dispatch — see
// init_netlinkers.go) and called from ns_createNetlinkersAndStore.go.
type NetlinkerFunc func(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32)

// netlinkerSyscall is the original synchronous path: one syscall.Recvfrom
// per netlink response packet, inline call to Deserialize, packet buffer
// reused from packetBufferPool. The SO_RCVTIMEO set by
// setSocketTimeoutViaSyscall caps Recvfrom blocking time so the loop can
// poll ctx for cancel.
func (x *XTCP) netlinkerSyscall(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32) {

	defer wg.Done()

	if x.debugLevel > 10 {
		log.Printf("Netlinker %d started ns:%s fd:%d", id, *nsName, fd)
	}

	wf := x.config.WriteFiles

	packetBuffer := x.packetBufferPool.Get().(*[]byte)

	for packets, netlinkerDone := 0, false; !netlinkerDone; packets++ {

		x.pC.WithLabelValues("Netlinker", "RecvfromCalls", "count").Inc()

		if netlinkerDone = x.checkDoneNonBlocking(ctx); netlinkerDone {
			continue
		}

		// keep in mind that via SetSocketTimeoutViaSyscall, the setsocket option
		// has set a read timeout to x.config.NLTimeout milliseconds, so
		// Recvfrom will not block forever, allowing Netlinkers to be shutdown

		startTime := time.Now()
		// https://www.man7.org/linux/man-pages/man3/recvfrom.3p.html
		n, _, err := syscall.Recvfrom(fd, *packetBuffer, 0)

		x.pH.WithLabelValues("Netlinker", "Recvfrom", "count").Observe(time.Since(startTime).Seconds())

		if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
			x.pC.WithLabelValues("Netlinker", "Timeout", "count").Inc()
			continue
		}
		if err != nil {
			x.pC.WithLabelValues("Netlinker", "nerr", "count").Inc()
			continue
		}

		if x.debugLevel > 100 {
			if ns, ok := x.fdToNsMap.Load(fd); ok {
				log.Printf("Netlinker %d Recvfrom packets:%d, n:%d, fd:%d ns:%s", id, packets, n, fd, ns.(string))
			} else {
				log.Printf("Netlinker %d Recvfrom packets:%d, n:%d, fd:%d Unknown FD!!", id, packets, n, fd)
			}
		}

		x.pC.WithLabelValues("Netlinker", "packets", "count").Inc()
		x.pC.WithLabelValues("Netlinker", "n", "count").Add(float64(n))

		if wf > 0 {
			now := time.Now()
			err = os.WriteFile(
				x.config.CapturePath+"netlink."+now.Format(time.RFC3339Nano),
				*(packetBuffer),
				writeFilesPermissionsCst)
			if err != nil {
				wg.Done()      // release the WG explicitly; log.Fatal skips the deferred Done
				log.Fatal(err) //nolint:gocritic // exitAfterDefer: deferred wg.Done() is released explicitly above
			}
			wf--
		}

		b := (*(packetBuffer))[0:n]

		p, errD := x.Deserialize(
			ctx,
			DeserializeArgs{
				ns:             nsName,
				fd:             fd,
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

		if x.debugLevel > 100 {
			if ns, ok := x.fdToNsMap.Load(fd); ok {
				log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d ns:%s", id, packets, n, p, fd, ns.(string))
			} else {
				log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d", id, packets, n, p, fd)
			}
		}

		if packets%forceGCModulesCst == 0 {
			x.pC.WithLabelValues("Netlinker", "runtime.GC()", "count").Inc()
			runtime.GC()
		}
	}

	*packetBuffer = (*packetBuffer)[:0]
	x.packetBufferPool.Put(packetBuffer)

	x.pC.WithLabelValues("Netlinker", "complete", "count").Inc()

}
