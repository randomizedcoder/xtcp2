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

// nsRanked is a namespace in admitNamespaces' ranking, and whether it is
// already tracked.
type nsRanked struct {
	id      nsIdentity
	tracked bool
}

// admitNamespaces applies -maxNetns (limit, 0 for no cap): the namespaces
// tracked are the limit lowest-pid live ones, xtcp2's own (self) always
// among them. Lower pids are older processes, so a burst of new namespaces
// cannot displace long-running workloads; bind-mounted namespaces (pid 0)
// were created by the host or the container runtime and rank first.
//
// candidates are live and untracked; tracked are live and tracked, with
// their current pid. It returns the candidates to add, the tracked
// namespaces to evict because lower-pid ones displace them, and how many
// candidates stay out. The ranking is total and depends only on the live
// set, so an evicted namespace loses the same comparison on every later
// reconcile: the tracked set converges, it does not churn.
func admitNamespaces(candidates, tracked []nsIdentity, limit uint32, self uint64) (admit, evict []nsIdentity, dropped int) {
	if limit == 0 || len(candidates) == 0 {
		return candidates, nil, 0
	}
	all := make([]nsRanked, 0, len(candidates)+len(tracked))
	for _, id := range tracked {
		all = append(all, nsRanked{id: id, tracked: true})
	}
	for _, id := range candidates {
		all = append(all, nsRanked{id: id})
	}
	slices.SortFunc(all, func(a, b nsRanked) int {
		if (a.id.inode == self) != (b.id.inode == self) {
			if a.id.inode == self {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(a.id.pid, b.id.pid), cmp.Compare(a.id.inode, b.id.inode))
	})
	for i, r := range all {
		keep := i < int(limit) || r.id.inode == self
		switch {
		case keep && !r.tracked:
			admit = append(admit, r.id)
		case !keep && r.tracked:
			evict = append(evict, r.id)
		case !keep:
			dropped++
		}
	}
	return admit, evict, dropped
}
