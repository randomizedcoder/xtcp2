package nlparity

// The segmenter: deciding who asked what, before anything is compared.
//
// # Attribution before normalization, and why that order is the whole trick
//
// The obvious way to compare two captures is to canonicalize both — zero the
// fields that legitimately differ — and then diff. That cannot work here,
// because the fields you would have to zero are the only fields that say which
// socket a message belongs to, and nlmon captures the whole host rather than
// one process. Canonicalizing first destroys the evidence.
//
// The committed fixtures are the proof. pkg/xtcpnl/testdata/7_1_8's
// netlink_route_getaddr.pcap holds 251 NETLINK_ROUTE messages across six
// distinct port ids and seventeen sequence numbers: two separate `ip` runs, 143
// messages from an unrelated process, and 20-byte RTM_GETADDRs built on
// rtgenmsg that iproute2 never sends. Zero the pids and it is one
// indistinguishable pile.
//
// So: segment in capture order using seq and pid, bind each transaction to the
// pid that answered it, and only then hand the result to a differ that may
// canonicalize freely.
//
// # Why requests cannot be attributed directly, and replies can
//
// A request carries nlmsg_pid == 0. The kernel fills the peer port id on the
// way back, not on the way out, so the request side of every transaction in
// every capture looks identical no matter which socket sent it. The reply is
// what identifies the socket, which means attribution runs backwards: a
// request opens a transaction with an unknown owner, and the first reply to
// reach it BINDS it.
//
// nlmsg_seq cannot substitute. rtnl_open seeds it from time(NULL)
// (iproute2 lib/libnetlink.c:249), so two sockets opened in the same second
// start from the same number — and they do: all eleven side RTM_GETLINK
// single-gets in netlink_route_getroute.pcap share seq 1789012355 with the
// primary route dump, which came from a different socket. Keying on seq alone
// merges them.
//
// # The three things that are reported rather than dropped
//
// A comparator that silently discards what it cannot attribute reports green
// on a capture it did not understand. So:
//
//   - A non-request with pid == 0 is a multicast notification. In a read-only
//     cut nothing subscribes to a group, so its presence means the capture
//     window caught someone else's event — a capture-hygiene failure, recorded
//     in Notifications. (pid == 0 alone does not mean notification: six of the
//     requests in the bulk getaddr fixture carry pid 0 too, which is why the
//     test is on NLM_F_REQUEST first.)
//   - A reply with no open transaction to attach to is an Orphan. It is the
//     signature of a capture that started mid-transaction.
//   - A reply that could belong to either of two open transactions marks the
//     one it was attached to Ambiguous. The attachment still happens, because
//     dropping it would understate the reply count, but Ambiguous excludes the
//     transaction's reply side from gating.

import (
	"golang.org/x/sys/unix"
)

// TxnState is how a transaction ended, or that it did not.
type TxnState uint8

const (
	// TxnOpen is a transaction with no terminator in the capture. Either the
	// capture window closed early or the tool never read the rest.
	TxnOpen TxnState = iota
	// TxnClosedByDone is the normal end of a multipart dump.
	TxnClosedByDone
	// TxnClosedByError is NLMSG_ERROR, which ends the transaction whether the
	// errno is an error or the zero of an ACK.
	TxnClosedByError
	// TxnClosedBySingleReply is a non-multipart reply, which is the whole
	// answer: `ip link show dev X` gets one RTM_NEWLINK with neither
	// NLM_F_MULTI nor a following NLMSG_DONE. This is the state that makes
	// xtcpnl.DumpRtnetlink hang and TalkRtnetlink necessary.
	TxnClosedBySingleReply
)

// String names the state for a report.
func (s TxnState) String() string {
	switch s {
	case TxnOpen:
		return "open"
	case TxnClosedByDone:
		return "closed-by-done"
	case TxnClosedByError:
		return "closed-by-error"
	case TxnClosedBySingleReply:
		return "closed-by-single-reply"
	default:
		return "unknown"
	}
}

// Txn is one request and the replies attributed to it.
type Txn struct {
	// Request is the message that opened it. Always a request, always
	// nlmsg_pid 0.
	Request Msg
	// Replies are the attributed replies in capture order, including the
	// terminating NLMSG_DONE or NLMSG_ERROR.
	Replies []Msg
	// Seq is the request's nlmsg_seq, the coarse key.
	Seq uint32
	// Pid is the port id the first attributed reply carried, and so the socket
	// this transaction belongs to. Zero means unbound: no reply arrived, so
	// the owner is unknown. It is NOT a pid of 0 — a reply cannot have one.
	Pid uint32
	// State is how it ended.
	State TxnState
	// Ambiguous records that at least one reply could have belonged to another
	// open transaction with the same seq. Set on the transaction the reply was
	// attached to. A differ must not gate on an ambiguous transaction's
	// replies; its request is still fully comparable, since a request is
	// tool-controlled and byte-deterministic.
	Ambiguous bool
}

// Closed reports whether the transaction has a terminator.
func (t Txn) Closed() bool { return t.State != TxnOpen }

// Segmentation is a whole capture resolved into transactions plus everything
// that did not fit into one.
type Segmentation struct {
	// Txns in the order their requests appeared.
	Txns []Txn
	// Notifications are non-request messages with pid 0 — multicast events
	// from the kernel that nothing in a read-only cut subscribed to.
	Notifications []Msg
	// Orphans are replies that found no open transaction with their seq.
	Orphans []Msg
}

// Pids returns the distinct port ids that answered, sorted ascending, with
// unbound transactions contributing nothing.
//
// This is the sentinel the plan calls for: a clean capture of one command has
// exactly one, and the six in the bulk getaddr fixture are the reason
// attribution exists at all.
func (s Segmentation) Pids() []uint32 {
	seen := make(map[uint32]bool, len(s.Txns))
	for i := range s.Txns {
		if s.Txns[i].Pid != 0 {
			seen[s.Txns[i].Pid] = true
		}
	}
	out := make([]uint32, 0, len(seen))
	for pid := range seen {
		out = append(out, pid)
	}
	// Insertion sort: this is at most a handful of elements and importing sort
	// for it would be the larger cost.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// TxnsForPid returns the transactions bound to one port id, in order. This is
// how a comparator isolates the tool's own traffic from a host capture.
func (s Segmentation) TxnsForPid(pid uint32) []Txn {
	var out []Txn
	for i := range s.Txns {
		if s.Txns[i].Pid == pid {
			out = append(out, s.Txns[i])
		}
	}
	return out
}

// Clean reports whether the segmentation has nothing to complain about: no
// notifications, no orphans, no ambiguous transaction, and every transaction
// closed.
//
// It is deliberately all four at once. Each one individually means the
// capture is not a faithful record of one tool running one command, and a
// caller that wants to know which should read the fields.
func (s Segmentation) Clean() bool {
	if len(s.Notifications) != 0 || len(s.Orphans) != 0 {
		return false
	}
	for i := range s.Txns {
		if s.Txns[i].Ambiguous || !s.Txns[i].Closed() {
			return false
		}
	}
	return true
}

// Segment resolves a message stream, in capture order, into transactions.
//
// The caller passes Capture.Msgs() for a real capture; the parameter is a
// slice rather than a Capture so a table can hand it a constructed stream
// without building a pcap around it.
//
// # The attachment rule, in the order the alternatives are tried
//
// For a reply carrying (seq, pid), among the still-open transactions whose Seq
// equals seq:
//
//  1. one already bound to that exact pid — attach, unambiguous. This is what
//     every reply after the first in a multipart dump takes.
//  2. otherwise the most recent unbound one — attach and bind it. This is the
//     first reply of a dump, the only point at which a transaction learns its
//     owner.
//  3. if more than one unbound candidate existed at step 2, the choice was a
//     guess: attach to the most recent and mark it Ambiguous.
//  4. nothing matched — an Orphan.
//
// Most recent, not first, because netlink is answered in order per socket: a
// later request's reply cannot precede an earlier one's on the same socket, so
// when two sockets collide on a seq the newer open transaction is the better
// guess. Step 3 exists because "better guess" is not "known".
func Segment(msgs []Msg) Segmentation {
	var s Segmentation

	// openIdx holds indices into s.Txns for transactions not yet closed, in
	// the order they were opened. Linear scans over it are fine: a single
	// command's capture has single digits of concurrent transactions, and the
	// dirtiest committed fixture has seventeen seqs in total.
	var openIdx []int

	closeTxn := func(slot int, state TxnState) {
		s.Txns[slot].State = state
		for i, idx := range openIdx {
			if idx == slot {
				openIdx = append(openIdx[:i], openIdx[i+1:]...)
				break
			}
		}
	}

	for _, m := range msgs {
		if m.IsRequest() {
			s.Txns = append(s.Txns, Txn{Request: m, Seq: m.Hdr.Seq, State: TxnOpen})
			openIdx = append(openIdx, len(s.Txns)-1)
			continue
		}

		// Non-request. pid 0 means nobody asked: a multicast notification.
		// Checked after IsRequest, because a request also carries pid 0 and
		// testing pid first would classify every request as a notification.
		if m.Hdr.Pid == 0 {
			s.Notifications = append(s.Notifications, m)
			continue
		}

		// Walk openIdx backwards so "most recent" falls out of the first hit.
		slot := -1
		unbound := -1
		unboundCount := 0
		for i := len(openIdx) - 1; i >= 0; i-- {
			idx := openIdx[i]
			if s.Txns[idx].Seq != m.Hdr.Seq {
				continue
			}
			if s.Txns[idx].Pid == m.Hdr.Pid {
				slot = idx
				break
			}
			if s.Txns[idx].Pid == 0 {
				unboundCount++
				if unbound < 0 {
					unbound = idx
				}
			}
		}

		ambiguous := false
		switch {
		case slot >= 0:
			// Rule 1: an exact pid match. Nothing to guess.
		case unbound >= 0:
			slot = unbound
			s.Txns[slot].Pid = m.Hdr.Pid
			// Rule 3: the bind was a choice between candidates, not a
			// deduction.
			ambiguous = unboundCount > 1
		default:
			s.Orphans = append(s.Orphans, m)
			continue
		}

		s.Txns[slot].Replies = append(s.Txns[slot].Replies, m)
		if ambiguous {
			s.Txns[slot].Ambiguous = true
		}

		switch {
		case m.Hdr.Type == uint16(unix.NLMSG_DONE):
			closeTxn(slot, TxnClosedByDone)
		case m.Hdr.Type == uint16(unix.NLMSG_ERROR):
			closeTxn(slot, TxnClosedByError)
		case !m.IsMulti():
			// A reply with NLM_F_MULTI clear is the whole answer. Closing here
			// is what lets a single-get transaction be complete without the
			// NLMSG_DONE that never comes.
			closeTxn(slot, TxnClosedBySingleReply)
		}
	}

	return s
}

// SegmentCapture is Segment over a parsed capture, which is the only thing a
// real caller wants.
func SegmentCapture(c Capture) Segmentation { return Segment(c.Msgs()) }
