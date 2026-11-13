package xtcp

import (
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The socket-count gate. Before each poll, every tracked namespace's
// sockstat says roughly how many records its dump will return. When the
// total is over -maxRecordsPerPoll, planPoll gives each namespace a max-min
// fair share of the budget and degrades only the namespaces over it:
//
//  1. pollStates: stop asking for TIME_WAIT and SYN_RECV/NEW_SYN_RECV.
//     The kernel drops them before building a record (tcp_diag.c, the
//     idiag_states check), so the dump is about sockstat's inuse.
//  2. pollSample: also split the namespace's sockets into K port shards and
//     ask for shard (pollingLoops mod K) each poll, with inet_diag bytecode
//     (sport in R_k) OR (dport in R_k). Every socket's dport is in exactly
//     one range, so K consecutive polls see every socket at least once.
//
// Port ops need no CAP_NET_ADMIN (only MARK_COND does; inet_diag.c
// inet_diag_bc_audit), so the shard filter works wherever xtcp2 does.

type pollLevel uint8

const (
	pollFull pollLevel = iota
	pollStates
	pollSample
)

func (l pollLevel) String() string {
	switch l {
	case pollStates:
		return "states"
	case pollSample:
		return "sample"
	default:
		return "full"
	}
}

// maxShardsCst bounds K. With a share of s records, K = ceil(inuse/s); a
// tiny share against a huge namespace would otherwise ask for thousands of
// one-port shards. Past this the namespace's per-poll dump can exceed its
// share; the per-namespace record cap (-maxRecordsPerNetns) still holds.
const maxShardsCst = 1024

// statesDegradeMask is the idiag_states bits pollStates clears.
const statesDegradeMask = 1<<3 | 1<<6 | 1<<12 // TCP_SYN_RECV, TCP_TIME_WAIT, TCP_NEW_SYN_RECV

// nsPlan is what one namespace is asked for this poll.
type nsPlan struct {
	level   pollLevel
	shard   uint32 // k, for pollSample
	shards  uint32 // K, for pollSample
	planned uint64 // records expected
	unknown bool   // the sockstat count was unknown
}

// pollPlanner holds planPoll's scratch so a poll allocates nothing once
// warmed up. Owned by the poller goroutine.
type pollPlanner struct {
	demand []uint64
	sorted []uint64
	plans  []nsPlan
}

// fairShare is the max-min fair share of budget over the demands in
// sorted (ascending): demands at or under the share are met in full, and
// the rest get the share. limited is false when every demand fits.
func fairShare(sorted []uint64, budget uint64) (share uint64, limited bool) {
	remaining := budget
	for i, d := range sorted {
		n := uint64(len(sorted) - i)
		if d <= remaining/n {
			remaining -= d
			continue
		}
		return remaining / n, true
	}
	if len(sorted) == 0 {
		return budget, false
	}
	return sorted[len(sorted)-1], false
}

// plan returns each namespace's plan, parallel to counts. budget 0 turns
// the gate off. An unknown count is taken as unknownAs (the per-namespace
// record cap; budget when that is off too), so a namespace whose sockstat
// can't be read is assumed heavy rather than free.
func (p *pollPlanner) plan(counts []sockstatCount, budget, unknownAs, pollingLoops uint64) []nsPlan {
	n := len(counts)
	p.plans = slices.Grow(p.plans[:0], n)[:n]
	if budget == 0 {
		clear(p.plans)
		return p.plans
	}
	if unknownAs == 0 {
		unknownAs = budget
	}
	p.demand = slices.Grow(p.demand[:0], n)[:n]
	for i, c := range counts {
		if c.known {
			p.demand[i] = c.total()
		} else {
			p.demand[i] = unknownAs
		}
	}
	p.sorted = append(p.sorted[:0], p.demand...)
	slices.Sort(p.sorted)
	share, limited := fairShare(p.sorted, budget)

	for i, c := range counts {
		d := p.demand[i]
		pl := nsPlan{level: pollFull, planned: d, unknown: !c.known}
		if limited && d > share {
			pl = degrade(c, d, share, pollingLoops)
			pl.unknown = !c.known
		}
		p.plans[i] = pl
	}
	return p.plans
}

// degrade picks the lightest level that fits a namespace into share.
func degrade(c sockstatCount, demand, share, pollingLoops uint64) nsPlan {
	if c.known && c.inuse <= share {
		return nsPlan{level: pollStates, planned: c.inuse}
	}
	base := demand
	if c.known {
		base = c.inuse
	}
	s := max(share, 1)
	k := min((base+s-1)/s, maxShardsCst)
	if k < 2 {
		k = 2 // inuse > share >= 1, so at least two shards
	}
	return nsPlan{
		level:   pollSample,
		shard:   uint32(pollingLoops % k),
		shards:  uint32(k),
		planned: (base + k - 1) / k,
	}
}

// shardRange is the k-th of K equal slices of the port space 0..65535.
func shardRange(k, shards uint32) (lo, hi uint16) {
	const space = 1 << 16
	l := uint64(k) * space / uint64(shards)
	h := uint64(k+1)*space/uint64(shards) - 1
	return uint16(l), uint16(h)
}

// shardBytecode is inet_diag bytecode for (sport in [lo,hi]) OR
// (dport in [lo,hi]), shaped as iproute2's ssfilter builds an OR (a; JMP;
// b) so every jump target is on the yes-chain, as the kernel's valid_cc
// requires:
//
//	 0 S_GE lo  yes 8  no 20  ┐ sport range; a miss goes to the dport range
//	 8 S_LE hi  yes 8  no 12  ┘
//	16 JMP      yes 4  no 20    sport matched: jump to the end (accept)
//	20 D_GE lo  yes 8  no 20  ┐ dport range; a miss jumps past the end
//	28 D_LE hi  yes 8  no 12  ┘ (reject)
func shardBytecode(lo, hi uint16) []byte {
	op := func(b []byte, code, yes uint8, no uint16) []byte {
		b = append(b, code, yes)
		return binary.LittleEndian.AppendUint16(b, no)
	}
	port := func(b []byte, p uint16) []byte { return op(b, 0, 0, p) }
	bc := make([]byte, 0, 36)
	bc = port(op(bc, xtcpnl.InetDiagBcSGe, 8, 20), lo)
	bc = port(op(bc, xtcpnl.InetDiagBcSLe, 8, 12), hi)
	bc = op(bc, xtcpnl.InetDiagBcJmp, 4, 20)
	bc = port(op(bc, xtcpnl.InetDiagBcDGe, 8, 20), lo)
	bc = port(op(bc, xtcpnl.InetDiagBcDLe, 8, 12), hi)
	return bc
}

// nlattrHdrSize is struct nlattr.
const nlattrHdrSize = 4

// maxCachedShardRequests bounds the shard request cache; past it the cache
// is cleared (K changes rarely, so this only trims stale sizes).
const maxCachedShardRequests = 4 * maxShardsCst

// pollRequests builds and caches the request each level sends. full is the
// startup request (x.nlRequest), shared, not copied.
type pollRequests struct {
	full   *[]byte
	states *[]byte
	shards map[uint64]*[]byte // shards<<32 | shard
}

func newPollRequests(full *[]byte) *pollRequests {
	st := slices.Clone(*full)
	states := binary.LittleEndian.Uint32(st[20:24]) &^ statesDegradeMask
	binary.LittleEndian.PutUint32(st[20:24], states)
	return &pollRequests{full: full, states: &st, shards: make(map[uint64]*[]byte)}
}

// get returns the request for pl, building and auditing a shard request
// the first time it is needed.
func (r *pollRequests) get(pl nsPlan) (*[]byte, error) {
	switch pl.level {
	case pollFull:
		return r.full, nil
	case pollStates:
		return r.states, nil
	}
	key := uint64(pl.shards)<<32 | uint64(pl.shard)
	if req, ok := r.shards[key]; ok {
		return req, nil
	}
	lo, hi := shardRange(pl.shard, pl.shards)
	bc := shardBytecode(lo, hi)
	if err := xtcpnl.InetDiagBcAudit(bc, false); err != nil {
		return nil, fmt.Errorf("shard %d/%d bytecode: %w", pl.shard, pl.shards, err)
	}
	req := make([]byte, 0, len(*r.states)+nlattrHdrSize+len(bc))
	req = append(req, *r.states...)
	req = binary.LittleEndian.AppendUint16(req, uint16(nlattrHdrSize+len(bc)))
	req = binary.LittleEndian.AppendUint16(req, xtcpnl.InetDiagReqBytecode)
	req = append(req, bc...)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req))) // nlmsg_len
	if len(r.shards) >= maxCachedShardRequests {
		clear(r.shards)
	}
	r.shards[key] = &req
	return &req, nil
}
