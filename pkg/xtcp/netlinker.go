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

// recvOneFromKernel performs a single syscall.Recvfrom into buf and
// classifies the result. retry=true means the caller should skip the
// rest of the iteration (timeout / non-fatal recv error). Returning
// retry=true with n=0 is the normal flow on SO_RCVTIMEO expiry — the
// outer loop then re-checks ctx and tries again.
func (x *XTCP) recvOneFromKernel(fd int, buf []byte) (n int, retry bool) {
	startTime := time.Now()
	// https://www.man7.org/linux/man-pages/man3/recvfrom.3p.html
	n, _, err := syscall.Recvfrom(fd, buf, 0)
	x.pH.WithLabelValues("Netlinker", "Recvfrom", "count").Observe(time.Since(startTime).Seconds())

	if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
		x.pC.WithLabelValues("Netlinker", "Timeout", "count").Inc()
		return 0, true
	}
	if err != nil {
		x.pC.WithLabelValues("Netlinker", "nerr", "count").Inc()
		return 0, true
	}
	return n, false
}

// captureToFileIfEnabled writes the packet to disk when the WriteFiles
// budget is positive. Returns the updated budget — 0 means "stop trying
// for the rest of this goroutine's life" (a write error disables
// captures permanently rather than killing the daemon).
func (x *XTCP) captureToFileIfEnabled(wf uint32, packet []byte, id uint32) uint32 {
	if wf == 0 {
		return wf
	}
	now := time.Now()
	// Capture only the n bytes Recvfrom actually filled. Writing the
	// raw *packetBuffer here used to dump the full pool-buffer size
	// (e.g. 8 KiB), trailing the real packet with stale bytes from a
	// previous Recvfrom — pcap-like consumers parsed garbage past
	// the real end of the message.
	err := os.WriteFile(
		x.config.CapturePath+"netlink."+now.Format(time.RFC3339Nano),
		packet,
		writeFilesPermissionsCst)
	if err == nil {
		return wf - 1
	}
	// Diagnostic capture-to-file is a side feature; a disk-full /
	// EACCES / etc. here must NOT take down the daemon (and every
	// other netlinker for every other namespace with it). Stop
	// capturing further packets and count the failure.
	x.pC.WithLabelValues("Netlinker", "WriteFile", "error").Inc()
	if x.debugLevel > 10 {
		log.Printf("Netlinker %d WriteFile err (disabling further captures): %v", id, err)
	}
	return 0
}

// maybeForceGC fires runtime.GC every forceGCModulesCst packets. The
// previous body fired on packets==0 too, forcing a GC before any work
// was done; skip the zero case so it fires every Nth packet AFTER the
// first, not on startup.
func (x *XTCP) maybeForceGC(packets int) {
	if packets <= 0 || packets%forceGCModulesCst != 0 {
		return
	}
	x.pC.WithLabelValues("Netlinker", "runtime.GC()", "count").Inc()
	runtime.GC()
}

// logRecvDebug emits the "after Recvfrom" debug log line, looking up
// the namespace name from fdToNsMap. Extracted from a 5-line if-ok-else
// block that appeared verbatim at two call sites.
func (x *XTCP) logRecvDebug(id uint32, packets, n, fd int) {
	if x.debugLevel <= 100 {
		return
	}
	if ns, ok := x.fdToNsMap.Load(fd); ok {
		nsStr, _ := ns.(string) //nolint:errcheck // fdToNsMap Store sites all use string
		log.Printf("Netlinker %d Recvfrom packets:%d, n:%d, fd:%d ns:%s", id, packets, n, fd, nsStr)
		return
	}
	log.Printf("Netlinker %d Recvfrom packets:%d, n:%d, fd:%d Unknown FD!!", id, packets, n, fd)
}

// logProcessedDebug emits the "after Deserialize" debug log line with
// the same ns-lookup pattern as logRecvDebug.
func (x *XTCP) logProcessedDebug(id uint32, packets, n int, p uint64, fd int) {
	if x.debugLevel <= 100 {
		return
	}
	if ns, ok := x.fdToNsMap.Load(fd); ok {
		nsStr, _ := ns.(string) //nolint:errcheck // fdToNsMap Store sites all use string
		log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d ns:%s", id, packets, n, p, fd, nsStr)
		return
	}
	log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d", id, packets, n, p, fd)
}

// netlinkerSyscall is the original synchronous path: one syscall.Recvfrom
// per netlink response packet, inline call to Deserialize, packet buffer
// reused from packetBufferPool. The SO_RCVTIMEO set by
// setSocketTimeoutViaSyscall caps Recvfrom blocking time so the loop can
// poll ctx for cancel.
//
// The body was previously a 17-cyclo monolith that mixed recv + error
// classification + debug logging + on-disk capture + GC bookkeeping +
// pool teardown inline. The bulk has moved into the helpers above;
// what remains is the receive-decode-loop skeleton (gocyclo 5).
func (x *XTCP) netlinkerSyscall(ctx context.Context, wg *sync.WaitGroup, nsName *string, fd int, id uint32) {

	defer wg.Done()

	if x.debugLevel > 10 {
		log.Printf("Netlinker %d started ns:%s fd:%d", id, *nsName, fd)
	}

	wf := x.config.WriteFiles

	packetBuffer, _ := x.packetBufferPool.Get().(*[]byte) //nolint:errcheck // pool.New returns *[]byte

	for packets, netlinkerDone := 0, false; !netlinkerDone; packets++ {

		x.pC.WithLabelValues("Netlinker", "RecvfromCalls", "count").Inc()

		if netlinkerDone = x.checkDoneNonBlocking(ctx); netlinkerDone {
			continue
		}

		// keep in mind that via SetSocketTimeoutViaSyscall, the setsocket option
		// has set a read timeout to x.config.NLTimeout milliseconds, so
		// Recvfrom will not block forever, allowing Netlinkers to be shutdown
		n, retry := x.recvOneFromKernel(fd, *packetBuffer)
		if retry {
			continue
		}

		x.logRecvDebug(id, packets, n, fd)

		x.pC.WithLabelValues("Netlinker", "packets", "count").Inc()
		x.pC.WithLabelValues("Netlinker", "n", "count").Add(float64(n))

		wf = x.captureToFileIfEnabled(wf, (*packetBuffer)[:n], id)

		b := (*packetBuffer)[0:n]

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

		x.logProcessedDebug(id, packets, n, p, fd)

		x.maybeForceGC(packets)
	}

	// Restore the slice header to full capacity before returning it to
	// the pool. (*packetBuffer)[:0] would Put a zero-length slice — a
	// later Get from a fresh netlinker would call syscall.Recvfrom on
	// it, which panics on &p[0] when len(p)==0. iouringPrefillRecvs
	// (netlinker_iouring.go) already restores cap on Get as a defensive
	// measure, but the syscall path is the producer of these buffers
	// and must Put them in usable shape.
	*packetBuffer = (*packetBuffer)[:cap(*packetBuffer)]
	x.packetBufferPool.Put(packetBuffer)

	x.pC.WithLabelValues("Netlinker", "complete", "count").Inc()

}
