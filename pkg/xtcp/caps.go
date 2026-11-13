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

// admitLimits are the admission caps; 0 turns one off.
type admitLimits struct {
	maxNetns    uint32 // -maxNetns: tracked namespaces
	maxPerOwner uint32 // -maxNetnsPerOwner: tracked namespaces per owner
	maxAdds     uint32 // -maxNetnsAddsPerReconcile: adds per reconcile
	self        uint64 // xtcp2's own namespace, always admitted
}

func (l admitLimits) any() bool { return l.maxNetns != 0 || l.maxPerOwner != 0 || l.maxAdds != 0 }

// admitDrops counts the candidates each cap left out.
type admitDrops struct {
	maxNetns, ownerQuota, addRate int
}

func (d admitDrops) total() int { return d.maxNetns + d.ownerQuota + d.addRate }

// admitNamespaces applies the admission caps to the live, untracked
// candidates, given tracked namespaces already holding slots and owners,
// the tracked count per owner (updated in place; nil when the owner quota
// is off). A tracked namespace keeps its slot until it dies: evicting by
// any key a tenant can influence (a pid wraps; a namespace's scanned pid
// changes) would let it push monitored workloads out. Waiting candidates
// are considered in the order reconcile first saw them (waiting,
// maintained by queueWaiting), then by pid and inode, and xtcp2's own
// namespace always gets in. A candidate is left out by -maxNetns when
// every slot is taken, by the add rate once this reconcile has added
// maxAdds, and by the owner quota when its owner (the outermost container
// id; see nsdiscover.Resolver.ContainerID) has maxPerOwner tracked.
// Namespaces with no pid (bind mounts only) have no owner and are exempt
// from the quota: tenants can't create host bind mounts. It returns the
// candidates to add and what each cap left out.
func admitNamespaces(candidates []nsIdentity, tracked int, owners map[string]int, lim admitLimits, waiting map[uint64]uint64) (admit []nsIdentity, drops admitDrops) {
	if !lim.any() {
		return candidates, drops
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
	adds := 0
	for _, id := range candidates {
		if id.inode == lim.self {
			admit = append(admit, id)
			tracked++
			adds++
			continue
		}
		switch {
		case lim.maxNetns != 0 && tracked >= int(lim.maxNetns):
			drops.maxNetns++
		case lim.maxAdds != 0 && adds >= int(lim.maxAdds):
			drops.addRate++
		case lim.maxPerOwner != 0 && id.pid > 0 && owners[id.owner] >= int(lim.maxPerOwner):
			drops.ownerQuota++
		default:
			admit = append(admit, id)
			tracked++
			adds++
			if lim.maxPerOwner != 0 && id.pid > 0 {
				owners[id.owner]++
			}
		}
	}
	return admit, drops
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
