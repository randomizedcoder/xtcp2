package xtcp

import (
	"cmp"
	"log"
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

// admitNamespaces picks which of the untracked live namespaces reconcile may
// add, given tracked (the namespaces tracked after its deletes) and limit
// (-maxNetns, 0 for no cap). Lower pids go first, so long-running workloads
// keep their slots ahead of a burst of new namespaces, and xtcp2's own
// namespace (self) is always admitted. It returns the admitted identities
// and how many were left out.
func admitNamespaces(candidates []nsIdentity, tracked int, limit uint32, self uint64) (admit []nsIdentity, dropped int) {
	if limit == 0 {
		return candidates, 0
	}
	slices.SortFunc(candidates, func(a, b nsIdentity) int {
		return cmp.Or(cmp.Compare(a.pid, b.pid), cmp.Compare(a.inode, b.inode))
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
