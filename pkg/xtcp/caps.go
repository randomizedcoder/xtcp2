package xtcp

import (
	"cmp"
	"log"
	"math"
	"slices"
)

// The caps on what a tenant can cost the collector: -maxDumpBytes and
// -maxRecordsPerNetns bound one namespace's dump per poll, -maxNetns bounds
// the namespaces tracked. Each drop is counted, so a namespace past a cap
// costs a metric rather than the collector's CPU or memory. 0 turns a cap off.

// Reasons for xtcp_dump_dropped_total.
const (
	dumpDropBytesCst   = "bytes"
	dumpDropRecordsCst = "records"
)

// Reasons for xtcp_netns_dropped_total.
const (
	netnsDropMaxNetnsCst   = "max_netns"
	netnsDropOwnerQuotaCst = "owner_quota"
	netnsDropAddRateCst    = "add_rate"
)

// dumpTruncated counts a dump cut short by the cap named by reason, and logs
// it. Callers make it once per dump, so it is at most one line per
// namespace per poll.
func (x *XTCP) dumpTruncated(reason string, fd int, limit uint64) {
	if x.pDumpDropped != nil {
		x.pDumpDropped.WithLabelValues(reason).Inc()
	}
	inode, _ := x.fdToNsMap.Load(fd)
	log.Printf("dump truncated: fd:%d inode:%v cap:%s=%d; the rest of this poll's dump is dropped", fd, inode, reason, limit)
}

// admitNamespaces applies -maxNetns (limit, 0 for no cap) to the live,
// untracked candidates, given tracked namespaces already holding slots. A
// tracked namespace keeps its slot until it dies: evicting by any key a
// tenant can influence (a pid wraps; a namespace's scanned pid changes)
// would let it push monitored workloads out. Waiting candidates are
// admitted in the order reconcile first saw them (waiting, maintained by
// queueWaiting), then by pid and inode, and xtcp2's own namespace (self)
// always. It returns the candidates to add and how many stay out.
func admitNamespaces(candidates []nsIdentity, tracked int, limit uint32, self uint64, waiting map[uint64]uint64) (admit []nsIdentity, dropped int) {
	if limit == 0 {
		return candidates, 0
	}
	// A candidate not yet queued arrived this reconcile: behind every
	// waiting one.
	place := func(inode uint64) uint64 {
		if s, ok := waiting[inode]; ok {
			return s
		}
		return math.MaxUint64
	}
	slices.SortFunc(candidates, func(a, b nsIdentity) int {
		return cmp.Or(cmp.Compare(place(a.inode), place(b.inode)),
			cmp.Compare(a.pid, b.pid), cmp.Compare(a.inode, b.inode))
	})
	admit = candidates[:0:0]
	for _, id := range candidates {
		if id.inode == self || tracked < int(limit) {
			admit = append(admit, id)
			tracked++
			continue
		}
		dropped++
	}
	return admit, dropped
}

// queueWaiting updates the -maxNetns queue after a reconcile: a candidate
// left out keeps its place, or joins at the back with the next sequence;
// one admitted or gone leaves the queue. Candidates first seen in the same
// reconcile share a sequence and are ordered by pid. It returns the queue
// and the sequence counter, both owned by reconcile.
func queueWaiting(waiting map[uint64]uint64, seq uint64, candidates, admit []nsIdentity) (map[uint64]uint64, uint64) {
	if len(candidates) == len(admit) {
		clear(waiting)
		return waiting, seq
	}
	admitted := make(map[uint64]struct{}, len(admit))
	for _, id := range admit {
		admitted[id.inode] = struct{}{}
	}
	next := make(map[uint64]uint64, len(candidates)-len(admit))
	seq++
	for _, id := range candidates {
		if _, in := admitted[id.inode]; in {
			continue
		}
		if s, ok := waiting[id.inode]; ok {
			next[id.inode] = s
		} else {
			next[id.inode] = seq
		}
	}
	return next, seq
}
