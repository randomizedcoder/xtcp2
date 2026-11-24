package xtcp

import (
	"encoding/binary"
	"log"

	"golang.org/x/sys/unix"
)

// pollGate is the socket-count gate's per-poller state: the sockstat
// reader, the planner and the request cache. It is created on the first
// gated poll and used only by the poller goroutine.
type pollGate struct {
	reader  sockstatReader
	planner pollPlanner
	reqs    *pollRequests
	reqs6   *pollRequests // nil: AF_INET only

	pairs   []nsPollFDs
	ready   []nsPollFDs
	statFDs []int // each ready namespace's sockstat, then its sockstat6
	counts  []sockstatCount
	sendTo  []nsSend

	rangeBuf []byte // a sampled namespace's ip_local_port_range

	// polls counts gated polls; it picks each sampled namespace's shard.
	// Not pollingLoops: that advances on every poller event (ticks, poll
	// requests, one netlinkerDone per namespace), so a poll could step it
	// by a multiple of K and dump the same shard forever.
	polls uint64
}

// nsSend is one namespace's socket and the requests planned for it: req,
// and req6, its AF_INET6 twin (nil: AF_INET only), sent after req's dump.
type nsSend struct {
	fd   int
	req  *[]byte
	req6 *[]byte
}

// newSockstatRingReader is the seam tests swap to make ring setup fail.
var newSockstatRingReader = func(batch, workers uint32) (sockstatReader, error) {
	return newRingSockstatReader(batch, workers)
}

// fdsPerNetns is what one tracked namespace holds open for its life: its
// netlink socket and the origNs snapshot its goroutine restores on exit
// (netNamespaceInstance), plus its sockstat, sockstat6 and
// ip_local_port_range fds with the gate on.
func fdsPerNetns(gate bool) uint64 {
	if gate {
		return 5
	}
	return 2
}

// fdLimitMarginCst covers everything else: listeners, destinations,
// enrichers, setns fds in flight.
const fdLimitMarginCst = 256

// getNofile is the seam tests swap for the RLIMIT_NOFILE soft limit.
var getNofile = func() (uint64, error) {
	var r unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &r); err != nil {
		return 0, err
	}
	return r.Cur, nil
}

// checkFDLimit warns at startup when RLIMIT_NOFILE can't hold -maxNetns
// namespaces' fds: past it, opening a namespace fails (counted per
// namespace) long before the cap is reached. It returns the fds needed, or
// 0 when the limit is enough or -maxNetns is off.
func (x *XTCP) checkFDLimit() (need uint64) {
	n := uint64(x.config.GetMaxNetns())
	if n == 0 {
		return 0
	}
	need = n*fdsPerNetns(x.gateEnabled()) + fdLimitMarginCst
	soft, err := getNofile()
	if err != nil || soft >= need {
		return 0
	}
	x.pC.WithLabelValues("init", "nofile", "low").Inc()
	log.Printf("RLIMIT_NOFILE %d is below the %d fds -maxNetns=%d needs (%d per namespace + %d); raise LimitNOFILE / ulimit -n",
		soft, need, n, fdsPerNetns(x.gateEnabled()), fdLimitMarginCst)
	return need
}

// gateEnabled reports whether -maxRecordsPerPoll is set.
func (x *XTCP) gateEnabled() bool {
	return x.config.GetMaxRecordsPerPoll() > 0
}

// newPollGate picks the sockstat reader as the netlinkers pick theirs: the
// io_uring one with -ioUring, falling back to pread if the ring can't be
// set up (counted).
func (x *XTCP) newPollGate() *pollGate {
	g := &pollGate{reqs: newPollRequests(x.nlRequest), rangeBuf: make([]byte, portRangeBufSize)}
	if x.nlRequest6 != nil {
		g.reqs6 = newPollRequests(x.nlRequest6)
	}
	if x.config.GetIoUring() {
		r, err := newSockstatRingReader(x.config.GetIoUringSockstatBatchSize(), x.config.GetIoUringSockstatWorkers())
		if err == nil {
			g.reader = r
			return g
		}
		x.pC.WithLabelValues("poller", "sockstatRing", "fallback").Inc()
		log.Printf("sockstat io_uring reader unavailable, using pread: %v", err)
	}
	g.reader = newSyscallSockstatReader()
	return g
}

// closeGate releases the gate's reader (the ring's goroutine and thread).
func (x *XTCP) closeGate() {
	if x.gate != nil {
		x.gate.reader.close()
		x.gate = nil
	}
}

// planGatedPoll reads every ready namespace's sockstat, plans the poll,
// and returns each ready socket with the request to send it.
func (x *XTCP) planGatedPoll() []nsSend {
	if x.gate == nil {
		x.gate = x.newPollGate()
	}
	g := x.gate
	g.pairs = getPollFDs(x.nsMap, g.pairs)
	g.ready = g.ready[:0]
	g.statFDs = g.statFDs[:0]
	for _, p := range g.pairs {
		// Skip slots reserved by nsAdd whose socket isn't open yet.
		if p.socketFD < 0 {
			continue
		}
		g.ready = append(g.ready, p)
		g.statFDs = append(g.statFDs, p.stat.acquire())
	}
	// The sockstat6 fds follow, read in the same batch; held under the
	// same read locks.
	for _, p := range g.ready {
		g.statFDs = append(g.statFDs, p.stat.sockstat6FD())
	}
	// Hold every handle's read lock until planning is done (sockstat and,
	// for sampled namespaces, port-range reads), so a
	// namespace torn down meanwhile can't close (and the kernel reuse) an
	// fd the reader is about to use. A teardown waits at most this read.
	defer func() {
		for _, p := range g.ready {
			p.stat.release()
		}
	}()
	if cap(g.counts) < len(g.statFDs) {
		g.counts = make([]sockstatCount, len(g.statFDs))
	}
	g.counts = g.counts[:len(g.statFDs)]

	failed, err := g.reader.read(g.statFDs, g.counts)
	if err != nil {
		// The ring is unusable: fall back to pread for this and later polls.
		x.pC.WithLabelValues("poller", "sockstatRing", "fallback").Inc()
		log.Printf("sockstat io_uring reader failed, using pread: %v", err)
		g.reader.close()
		g.reader = newSyscallSockstatReader()
		if failed, err = g.reader.read(g.statFDs, g.counts); err != nil {
			log.Printf("sockstat pread reader: %v", err) // never: pread has no reader-level error
		}
	}
	if failed > 0 {
		x.pC.WithLabelValues("poller", "sockstat", "error").Add(float64(failed))
	}

	g.counts = addSockstat6(g.counts, g.statFDs[len(g.ready):])

	plans := g.planner.plan(g.counts, x.config.GetMaxRecordsPerPoll(), x.config.GetMaxRecordsPerNetns(), g.polls)
	g.polls++
	g.sendTo = g.sendTo[:0]
	var total uint64
	var unknown, states, sample int
	for i, pl := range plans {
		total += g.planner.demand[i]
		if pl.unknown {
			unknown++
		}
		pr := defaultPortRange
		if pl.level == pollSample {
			// Read under the handle's read lock, still held (see above).
			if r, ok := g.ready[i].stat.portRange(g.rangeBuf); ok {
				pr = r
			} else {
				x.pC.WithLabelValues("poller", "portRange", "error").Inc()
			}
		}
		req, err := g.reqs.get(pl, pr)
		if err != nil {
			// Can't happen for the fixed shard shape (it is audited); if it
			// does, a full request is still bounded by the receiver caps.
			x.pC.WithLabelValues("poller", "planPoll", "requestError").Inc()
			log.Printf("planPoll: %v; sending the full request", err)
			req = g.reqs.full
			pl.level = pollFull
		}
		switch pl.level {
		case pollFull:
		case pollStates:
			states++
		case pollSample:
			sample++
		}
		g.sendTo = append(g.sendTo, nsSend{fd: g.ready[i].socketFD, req: req, req6: g.request6(pl, pr)})
	}
	x.recordGate(total, unknown, states, sample)
	return g.sendTo
}

// addSockstat6 folds counts' second half, each namespace's sockstat6, into
// its first half and returns the first half. fds6 are the sockstat6 fds: one
// never opened (no IPv6) adds nothing; one that couldn't be read leaves the
// namespace's count unknown, as a failed sockstat read does.
func addSockstat6(counts []sockstatCount, fds6 []int) []sockstatCount {
	n := len(fds6)
	for i, fd := range fds6 {
		c6 := counts[n+i]
		switch {
		case fd <= 0:
		case c6.known:
			counts[i].inuse += c6.inuse
		default:
			counts[i].known = false
		}
	}
	return counts[:n]
}

// request6 is the AF_INET6 request at pl's level, nil when there is no
// AF_INET6 dump. Its shard request is built like the AF_INET one (the
// bytecode matches ports only), so it can't fail where that one didn't;
// if it does, the full request is sent, bounded by the receiver caps.
func (g *pollGate) request6(pl nsPlan, pr portRange) *[]byte {
	if g.reqs6 == nil {
		return nil
	}
	req, err := g.reqs6.get(pl, pr)
	if err != nil {
		return g.reqs6.full
	}
	return req
}

// recordGate publishes one gated poll's counts and logs a poll that
// degraded anything (one line per poll).
func (x *XTCP) recordGate(total uint64, unknown, states, sample int) {
	if x.pPollSockets != nil {
		x.pPollSockets.Set(float64(total))
	}
	if unknown > 0 {
		x.pC.WithLabelValues("poller", "planPoll", "unknownCount").Add(float64(unknown))
	}
	if states == 0 && sample == 0 {
		return
	}
	if x.pPollDegraded != nil {
		x.pPollDegraded.WithLabelValues(pollStates.String()).Add(float64(states))
		x.pPollDegraded.WithLabelValues(pollSample.String()).Add(float64(sample))
	}
	log.Printf("poll degraded by -maxRecordsPerPoll=%d: sockets:%d states:%d sample:%d unknown:%d",
		x.config.GetMaxRecordsPerPoll(), total, states, sample, unknown)
}

// setRequestSeq stamps this poll's sequence number into req.
func (x *XTCP) setRequestSeq(req *[]byte, pollingLoops uint64) {
	binary.LittleEndian.PutUint32((*req)[8:12], x.config.NlmsgSeq+uint32(pollingLoops))
}
