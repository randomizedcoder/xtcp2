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
//     matching shardKey(sport, dport) in R_k. The K ranges partition the
//     port space, so K consecutive polls see every socket exactly once.
//     Sockets that share both ports (differing only by address) share a
//     shard; -maxRecordsPerNetns still bounds that.
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

// shardRange is shard k of K's range of shard keys. Keys are almost all
// ephemeral ports (>= ephemeralLoCst), so the ranges split 32768..65535
// evenly and shard 0 also takes every key below it (listeners, whose dport
// is 0, and the rare socket with no ephemeral side).
func shardRange(k, shards uint32) (lo, hi uint16) {
	const span = 1<<16 - ephemeralLoCst
	l := ephemeralLoCst + uint64(k)*span/uint64(shards)
	h := ephemeralLoCst + uint64(k+1)*span/uint64(shards) - 1
	if k == 0 {
		l = 0
	}
	return uint16(l), uint16(h)
}

// ephemeralLoCst splits the port space for sharding: a socket's shard key
// is its sport when sport >= ephemeralLoCst (a client-side ephemeral port,
// or a listener's accepted socket seen from the client) and its dport
// otherwise (a server-side socket: dport is the peer's ephemeral port).
// It covers Linux's default ip_local_port_range (32768-60999) and IANA's
// (49152-65535). Keying on the varying side spreads a busy listener's
// sockets over every shard, where keying on either port would put them all
// in the shard holding the listener's port. The split only affects the
// spread: the shards partition every socket either way.
const ephemeralLoCst = 32768

// shardKey is the port shardBytecode filters on.
func shardKey(sport, dport uint16) uint16 {
	if sport >= ephemeralLoCst {
		return sport
	}
	return dport
}

// shardBytecode is inet_diag bytecode for shardKey(sport, dport) in
// [lo,hi]:
//
//	(sport >= 32768 AND sport >= lo AND sport <= hi) OR
//	(sport <= 32767 AND dport >= lo AND dport <= hi)
//
// shaped as iproute2's ssfilter builds an OR (a; JMP; b) so every jump
// target is on the yes-chain, as the kernel's valid_cc requires:
//
//	 0 S_GE 32768 yes 8 no 28  ┐ a: a miss goes to b (offset 28)
//	 8 S_GE lo    yes 8 no 20  │
//	16 S_LE hi    yes 8 no 12  ┘
//	24 JMP        yes 4 no 28    a matched: jump to the end (accept)
//	28 S_LE 32767 yes 8 no 28  ┐ b: a miss jumps past the end (reject)
//	36 D_GE lo    yes 8 no 20  │
//	44 D_LE hi    yes 8 no 12  ┘ falling off the end accepts
func shardBytecode(lo, hi uint16) []byte {
	op := func(b []byte, code, yes uint8, no uint16) []byte {
		b = append(b, code, yes)
		return binary.LittleEndian.AppendUint16(b, no)
	}
	port := func(b []byte, p uint16) []byte { return op(b, 0, 0, p) }
	bc := make([]byte, 0, shardBytecodeLen)
	bc = port(op(bc, xtcpnl.InetDiagBcSGe, 8, 28), ephemeralLoCst)
	bc = port(op(bc, xtcpnl.InetDiagBcSGe, 8, 20), lo)
	bc = port(op(bc, xtcpnl.InetDiagBcSLe, 8, 12), hi)
	bc = op(bc, xtcpnl.InetDiagBcJmp, 4, 28)
	bc = port(op(bc, xtcpnl.InetDiagBcSLe, 8, 28), ephemeralLoCst-1)
	bc = port(op(bc, xtcpnl.InetDiagBcDGe, 8, 20), lo)
	bc = port(op(bc, xtcpnl.InetDiagBcDLe, 8, 12), hi)
	return bc
}

// shardBytecodeLen is shardBytecode's length: six port ops and a JMP.
const shardBytecodeLen = 6*2*xtcpnl.InetDiagBcOpSizeCst + xtcpnl.InetDiagBcOpSizeCst

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
