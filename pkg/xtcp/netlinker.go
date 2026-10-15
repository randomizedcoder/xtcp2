// Netlinker is the per-socket code that receives netlink packets from the
// kernel and feeds the deserializer. The function-pointer x.Netlinker
// (registered in pkg/xtcp/init_netlinkers.go) is one of:
//
//	netlinkerSyscall  — the synchronous syscall.Recvfrom path: one
//	                    receiver per socket plus a pool of decoders.
//	netlinkerIoUring  — the opt-in io_uring path: one ring per socket
//	                    with a multishot recv, feeding the same decoders.
//
// Selection happens at init time from config.IoUring. Same dispatch
// pattern as Marshaller/Destination (sync.Map of closures + chosen
// function pointer on XTCP).

package xtcp

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	writeFilesPermissionsCst = 0644
	forceGCModulesCst        = 1000
	nlmsgAlignToCst          = 4
)

// NetlinkerFunc runs the netlinkers for one fd until ctx is canceled,
// calling wg.Done when they have all exited. netlinkers is
// config.Netlinkers: the decoder count for the syscall variant, the ring
// goroutine count for io_uring. The chosen variant is stored in
// x.Netlinker (sync.Map dispatch — see init_netlinkers.go) and called once
// per socket from ns_createNetlinkersAndStore.go. nsName is the
// best-effort namespace name (by pointer, shared) and inode is its stable
// nsfs identity; both are stamped onto every record.
type NetlinkerFunc func(ctx context.Context, wg *sync.WaitGroup, nsName *string, inode uint64, fd int, netlinkers uint32)

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
		if inode, okI := ns.(uint64); okI {
			log.Printf("Netlinker %d Recvfrom packets:%d, n:%d, fd:%d inode:%d", id, packets, n, fd, inode)
			return
		}
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
		if inode, okI := ns.(uint64); okI {
			log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d inode:%d", id, packets, n, p, fd, inode)
			return
		}
	}
	log.Printf("Netlinker %d packets:%d, n:%d, p:%d, fd:%d", id, packets, n, p, fd)
}

// netlinkerSyscall is the synchronous path for one socket: a single
// receiver goroutine (this one) reads the socket with syscall.Recvfrom and
// hands each packet to `netlinkers` decoder goroutines, which deserialize
// in parallel. The SO_RCVTIMEO set by setSocketTimeoutViaSyscall caps
// Recvfrom blocking time so the receiver can poll ctx for cancel.
//
// Why one receiver: the kernel builds a dump one packet at a time per
// socket (netlink_dump under nl_cb_mutex), so several readers on one fd
// don't receive any faster; decoding is the CPU-bound part, and that stays
// parallel. One receiver also sees the packets in the kernel's order,
// which is what lets the dump's end be detected correctly: see dumpState.
// Earlier, `netlinkers` goroutines each did Recvfrom + Deserialize, and
// the one that read the 20-byte NLMSG_DONE packet signaled done while its
// siblings were still decoding the dump's last data packets, so the
// poll-end flush dropped their records
// (docs/investigations/envelope-post-flush-drop.md).
func (x *XTCP) netlinkerSyscall(ctx context.Context, wg *sync.WaitGroup, nsName *string, inode uint64, fd int, netlinkers uint32) {

	defer wg.Done()

	if x.debugLevel > 10 {
		log.Printf("Netlinker started ns:%s fd:%d decoders:%d", *nsName, fd, netlinkers)
	}

	packetCh, decodersWG := x.startDecoders(ctx, nsName, inode, fd, netlinkers, "Netlinker")

	x.netlinkReceiver(ctx, packetCh, fd)

	decodersWG.Wait()

	x.pC.WithLabelValues("Netlinker", "complete", "count").Inc()
}

// startDecoders starts max(netlinkers, 1) netlinkDecoder goroutines for fd
// and returns the channel the receiver feeds them through. The decoders
// exit once the receiver closes it; wait on the WaitGroup for that. label
// is the metrics function label, "Netlinker" or "NetlinkerIoUring".
func (x *XTCP) startDecoders(ctx context.Context, nsName *string, inode uint64, fd int, netlinkers uint32, label string) (chan dumpPacket, *sync.WaitGroup) {
	decoders := max(netlinkers, 1)
	packetCh := make(chan dumpPacket, decoders)

	decodersWG := new(sync.WaitGroup)
	for id := range decoders {
		decodersWG.Add(1)
		go x.netlinkDecoder(ctx, decodersWG, packetCh, nsName, inode, fd, id, label)
	}
	return packetCh, decodersWG
}

// dumpState tracks one dump on one socket, so the done signal goes out
// only after the last of its packets has been decoded, by whichever
// decoder finishes last.
//
// The receiver numbers packets in the order the kernel delivered them and,
// when it sees NLMSG_DONE in the dump's k-th packet, stores total = k
// before handing that packet to a decoder. Each decoder increments decoded
// after deserializing a packet; the one whose increment reaches total
// signals done. That increment is the last of the dump's k increments, and
// the done packet's own increment (which follows the store of total) is
// among them, so the last one always sees total. Before the store, total
// is 0 and decoded is at least 1, so nothing can signal early. signaled
// makes the signal exactly-once regardless.
//
// The count can't come from the kernel: every message of a dump, NLMSG_DONE
// included, carries the request's nlmsg_seq, not a per-message number.
type dumpState struct {
	total    atomic.Uint64
	decoded  atomic.Uint64
	signaled atomic.Bool
}

// dumpPacket is one received packet on its way from the receiver to a
// decoder. buf comes from packetBufferPool; the decoder returns it.
type dumpPacket struct {
	buf  *[]byte
	n    int
	dump *dumpState
}

// dumpTracker is a receiver's position in the current dump. Only the
// receiver goroutine uses it.
type dumpTracker struct {
	dump    *dumpState
	packets uint64
}

func newDumpTracker() *dumpTracker {
	return &dumpTracker{dump: new(dumpState)}
}

// assign numbers packet b, the next one received, and returns the dump it
// belongs to. If b holds NLMSG_DONE it stores the dump's total before
// returning, so before b is handed to a decoder, and the next packet starts
// a new dump.
func (t *dumpTracker) assign(b []byte) *dumpState {
	t.packets++
	d := t.dump
	if packetHasDone(b) {
		d.total.Store(t.packets)
		t.dump, t.packets = new(dumpState), 0
	}
	return d
}

// netlinkReceiver reads packets from fd until ctx is canceled, numbering
// them per dump (see dumpState) and sending them to the decoders. It
// closes packetCh on return, which stops the decoders once they've drained
// it.
func (x *XTCP) netlinkReceiver(ctx context.Context, packetCh chan<- dumpPacket, fd int) {

	defer close(packetCh)

	wf := x.config.WriteFiles

	tracker := newDumpTracker()

	for packets := 0; ; packets++ {

		x.pC.WithLabelValues("Netlinker", "RecvfromCalls", "count").Inc()

		if x.checkDoneNonBlocking(ctx) {
			return
		}

		packetBuffer := x.packetBufferPool.Get()

		// keep in mind that via SetSocketTimeoutViaSyscall, the setsocket option
		// has set a read timeout to x.config.NLTimeout milliseconds, so
		// Recvfrom will not block forever, allowing Netlinkers to be shutdown
		n, retry := x.recvOneFromKernel(fd, *packetBuffer)
		if retry {
			x.putPacketBuffer(packetBuffer)
			continue
		}

		x.logRecvDebug(0, packets, n, fd)

		x.pC.WithLabelValues("Netlinker", "packets", "count").Inc()
		x.pC.WithLabelValues("Netlinker", "n", "count").Add(float64(n))

		wf = x.captureToFileIfEnabled(wf, (*packetBuffer)[:n], 0)

		packetCh <- dumpPacket{buf: packetBuffer, n: n, dump: tracker.assign((*packetBuffer)[:n])}

		x.maybeForceGC(packets)
	}
}

// netlinkDecoder deserializes packets from packetCh until it's closed,
// then exits. id is stamped on the records as NetlinkerId; label is the
// metrics function label.
func (x *XTCP) netlinkDecoder(ctx context.Context, wg *sync.WaitGroup, packetCh <-chan dumpPacket, nsName *string, inode uint64, fd int, id uint32, label string) {

	defer wg.Done()

	packets := 0
	for p := range packetCh {

		b := (*p.buf)[:p.n]

		if x.decodeHook != nil {
			x.decodeHook(fd, b)
		}

		records, errD := x.Deserialize(
			ctx,
			DeserializeArgs{
				ns:             nsName,
				fd:             fd,
				inode:          inode,
				NLPacket:       &b,
				xtcpRecordPool: &x.xtcpRecordPool,
				nlhPool:        &x.nlhPool,
				rtaPool:        &x.rtaPool,
				pC:             x.pC,
				pH:             x.pH,
				id:             id,
			})
		if errD != nil {
			x.pC.WithLabelValues(label, "ParseNLPacket", "error").Inc()
		} else {
			x.pC.WithLabelValues(label, "p", "count").Add(float64(records))
			x.logProcessedDebug(id, packets, p.n, records, fd)
		}

		x.putPacketBuffer(p.buf)

		// Counted even when Deserialize failed: the packet is finished
		// with either way, and the dump's done must not wait on it.
		x.packetDecoded(p.dump, fd)
		packets++
	}
}

// packetDecoded counts one decoded packet of dump d and signals done for
// fd if it was the dump's last (see dumpState).
func (x *XTCP) packetDecoded(d *dumpState, fd int) {
	if d.decoded.Add(1) == d.total.Load() && d.signaled.CompareAndSwap(false, true) {
		x.signalNetlinkerDone(fd)
	}
}

// packetHasDone reports whether a netlink packet contains an NLMSG_DONE
// message, by walking the message headers without decoding the bodies.
// A header whose nlmsg_len is shorter than a header stops the walk, like
// Deserialize, so a malformed packet can't loop forever.
func packetHasDone(b []byte) bool {
	for off := 0; off+xtcpnl.NlMsgHdrSizeCst <= len(b); {
		if binary.LittleEndian.Uint16(b[off+4:off+6]) == xtcpnl.NlMsgHdrTypeDoneCst {
			return true
		}
		l := int(binary.LittleEndian.Uint32(b[off : off+4]))
		if l < xtcpnl.NlMsgHdrSizeCst {
			return false
		}
		off += nlmsgAlign(l)
	}
	return false
}

// nlmsgAlign rounds a netlink message length up to NLMSG_ALIGNTO (4).
func nlmsgAlign(l int) int {
	return (l + nlmsgAlignToCst - 1) &^ (nlmsgAlignToCst - 1)
}

// putPacketBuffer restores the slice header to full capacity before
// returning it to the pool. (*packetBuffer)[:0] would Put a zero-length
// slice — a later Get would call syscall.Recvfrom on it, which panics on
// &p[0] when len(p)==0. handleRecvCQE (netlinker_iouring.go) also
// restores cap on Get as a defensive measure.
func (x *XTCP) putPacketBuffer(packetBuffer *[]byte) {
	*packetBuffer = (*packetBuffer)[:cap(*packetBuffer)]
	x.packetBufferPool.Put(packetBuffer)
}
