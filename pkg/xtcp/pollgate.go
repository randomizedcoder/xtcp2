package xtcp

import (
	"encoding/binary"
	"log"
)

// pollGate is the socket-count gate's per-poller state: the sockstat
// reader, the planner and the request cache. It is created on the first
// gated poll and used only by the poller goroutine.
type pollGate struct {
	reader  sockstatReader
	planner pollPlanner
	reqs    *pollRequests

	pairs   []nsPollFDs
	ready   []nsPollFDs
	statFDs []int
	counts  []sockstatCount
	sendTo  []nsSend
}

// nsSend is one namespace's socket and the request planned for it.
type nsSend struct {
	fd  int
	req *[]byte
}

// newSockstatRingReader is the seam tests swap to make ring setup fail.
var newSockstatRingReader = func(batch, workers uint32) (sockstatReader, error) {
	return newRingSockstatReader(batch, workers)
}

// gateEnabled reports whether -maxRecordsPerPoll is set.
func (x *XTCP) gateEnabled() bool {
	return x.config.GetMaxRecordsPerPoll() > 0
}

// newPollGate picks the sockstat reader as the netlinkers pick theirs: the
// io_uring one with -ioUring, falling back to pread if the ring can't be
// set up (counted).
func (x *XTCP) newPollGate() *pollGate {
	g := &pollGate{reqs: newPollRequests(x.nlRequest)}
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
func (x *XTCP) planGatedPoll(pollingLoops uint64) []nsSend {
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
	// Hold every handle's read lock until the reads are done, so a
	// namespace torn down meanwhile can't close (and the kernel reuse) an
	// fd the reader is about to use. A teardown waits at most this read.
	defer func() {
		for _, p := range g.ready {
			p.stat.release()
		}
	}()
	if cap(g.counts) < len(g.ready) {
		g.counts = make([]sockstatCount, len(g.ready))
	}
	g.counts = g.counts[:len(g.ready)]

	failed, err := g.reader.read(g.statFDs, g.counts)
	if err != nil {
		// The ring is unusable: fall back to pread for this and later polls.
		x.pC.WithLabelValues("poller", "sockstatRing", "fallback").Inc()
		log.Printf("sockstat io_uring reader failed, using pread: %v", err)
		g.reader.close()
		g.reader = newSyscallSockstatReader()
		failed, _ = g.reader.read(g.statFDs, g.counts)
	}
	if failed > 0 {
		x.pC.WithLabelValues("poller", "sockstat", "error").Add(float64(failed))
	}

	plans := g.planner.plan(g.counts, x.config.GetMaxRecordsPerPoll(), x.config.GetMaxRecordsPerNetns(), pollingLoops)
	g.sendTo = g.sendTo[:0]
	var total uint64
	var unknown, states, sample int
	for i, pl := range plans {
		total += g.planner.demand[i]
		if pl.unknown {
			unknown++
		}
		req, err := g.reqs.get(pl)
		if err != nil {
			// Can't happen for the fixed shard shape (it is audited); if it
			// does, a full request is still bounded by the receiver caps.
			x.pC.WithLabelValues("poller", "planPoll", "requestError").Inc()
			log.Printf("planPoll: %v; sending the full request", err)
			req = g.reqs.full
			pl.level = pollFull
		}
		switch pl.level {
		case pollStates:
			states++
		case pollSample:
			sample++
		}
		g.sendTo = append(g.sendTo, nsSend{fd: g.ready[i].socketFD, req: req})
	}
	x.recordGate(total, unknown, states, sample)
	return g.sendTo
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
