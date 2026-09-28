package nlparity

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// FuzzWalkDatagram fuzzes the tolerant walker over arbitrary bytes.
//
// This is the highest-value fuzz target in the package because it is the only
// code here that parses genuinely untrusted input: a pcap file, which in the
// in-guest parity runner is produced by tcpdump and read back without any
// validation of its own.
//
// The invariants asserted are the ones a differ depends on, not just
// "no panic":
//
//  1. every byte is accounted for — the messages plus the tail must sum to the
//     datagram length, or a divergence could hide in the gap;
//  2. no message may overlap its neighbor or run past the end;
//  3. TailFlagged and TailAllZero must stay consistent, because the whole
//     oversend-versus-truncation distinction rests on them.
//
// go test ./pkg/nlparity/ -run FuzzWalkDatagram -fuzz FuzzWalkDatagram
func FuzzWalkDatagram(f *testing.F) {
	ifinfo := make([]byte, xtcpnl.IfInfomsgSizeCst)
	ifinfo[0] = unix.AF_PACKET

	f.Add([]byte(nil))
	f.Add(zeros(16))
	f.Add(concat(getaddrRequest(unix.AF_INET), zeros(128)))
	f.Add(wellFormed(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), ifinfo))
	f.Add(nlmsg(0, 0x03e7, 0, 0, 0, nil))
	f.Add(nlmsg(0xffffffff, uint16(unix.RTM_GETLINK), 0, 0, 0, ifinfo))

	// The real request datagrams, so the corpus starts from traffic a kernel
	// actually produced rather than only from hand-built edges.
	f.Add(routeDatagram(f, tdBulkGetLink, 0))
	f.Add(routeDatagram(f, tdBulkGetRoute, 0))

	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := WalkDatagram(uint16(unix.NETLINK_ROUTE), data)
		if err != nil {
			if !IsBadCapture(err) {
				t.Fatalf("err = %v, want ErrBadHead or ErrShortDatagram", err)
			}
			return
		}

		if d.Len != len(data) {
			t.Fatalf("Len = %d, want %d", d.Len, len(data))
		}

		consumed := 0
		for i, m := range d.Msgs {
			if m.Offset < consumed {
				t.Fatalf("message[%d] at offset %d overlaps the previous message, "+
					"which ended at %d", i, m.Offset, consumed)
			}
			end := m.Offset + int(m.Hdr.Len)
			if end > len(data) {
				t.Fatalf("message[%d] ends at %d, past the %d-byte datagram", i, end, len(data))
			}
			if int(m.Hdr.Len) < xtcpnl.NlMsgHdrSizeCst {
				t.Fatalf("message[%d] nlmsg_len = %d, below a bare header", i, m.Hdr.Len)
			}
			if len(m.Body) != int(m.Hdr.Len)-xtcpnl.NlMsgHdrSizeCst {
				t.Fatalf("message[%d] body = %d bytes, want %d",
					i, len(m.Body), int(m.Hdr.Len)-xtcpnl.NlMsgHdrSizeCst)
			}
			consumed = end
		}

		if d.TailBytes < 0 || d.TailBytes > len(data) {
			t.Fatalf("TailBytes = %d, outside 0..%d", d.TailBytes, len(data))
		}
		// Everything not in a message, and not alignment padding between
		// messages, is the tail. So the tail can never start before the last
		// message ended.
		if len(data)-d.TailBytes < consumed {
			t.Fatalf("the tail starts at %d, before the last message ended at %d: "+
				"bytes went missing", len(data)-d.TailBytes, consumed)
		}

		if d.TailFlagged && d.TailAllZero {
			t.Fatalf("tail is both flagged and all-zero; an all-zero tail is an "+
				"oversend, which is exactly what must not be flagged (%d bytes)",
				d.TailBytes)
		}
		if d.TailFlagged && d.TailBytes < xtcpnl.NlMsgHdrSizeCst {
			t.Fatalf("tail of %d bytes is flagged, but a flag means a full header "+
				"was present and unusable", d.TailBytes)
		}
	})
}

// FuzzSegmentTransactions fuzzes the segmenter.
//
// # Why this fuzzes bytes and not a []Msg
//
// f.Fuzz accepts only scalars and []byte, so the stream cannot be handed over
// directly. That is no loss: a datagram may carry any number of back-to-back
// messages — which is exactly how a multipart dump arrives — so walking one
// fuzzed datagram yields an arbitrary-length message stream, and it yields it
// through the same WalkDatagram a real caller uses. The seeds below include two
// real request datagrams and the real twelve-message reply stream so the corpus
// starts from bytes a kernel produced.
//
// # Why conservation is the invariant worth fuzzing
//
// Segment classifies every message into exactly one of four places: a
// transaction's Request, a transaction's Replies, Notifications, or Orphans.
// The whole reason the last two exist is that a comparator which silently
// discards what it cannot attribute reports green on a capture it did not
// understand — so a message going missing is not a cosmetic bug, it is the
// specific failure mode the design is built to prevent. A count that sums is
// the only cheap proof that nothing was dropped or double-counted.
//
// The rest are the state-machine consistencies a differ relies on: a bound pid
// is never 0 and is shared by every reply in its transaction, a transaction
// cannot be closed or ambiguous without a reply to have closed or confused it,
// and the recorded end state agrees with the message that caused it.
//
// go test ./pkg/nlparity/ -run FuzzSegmentTransactions -fuzz FuzzSegmentTransactions
func FuzzSegmentTransactions(f *testing.F) {
	ifinfo := make([]byte, xtcpnl.IfInfomsgSizeCst)
	ifinfo[0] = unix.AF_PACKET

	const (
		fzSeq  = 1789012355 // the real colliding seq from getroute.pcap
		fzPidA = 106975
		fzPidB = 106943
	)

	f.Add([]byte(nil))

	// The real request datagrams: one transaction each, left open because the
	// capture holds no reply.
	f.Add(routeDatagram(f, tdBulkGetLink, 0))
	f.Add(routeDatagram(f, tdBulkGetRoute, 0))
	// The real extracted reply stream, which is twelve orphans: the request was
	// thrown away when the fixture was cut, so nothing here can be attributed.
	f.Add(routeDatagram(f, tdGetLinkDump, 0))

	// A whole clean transaction: request, two multipart replies, NLMSG_DONE.
	f.Add(concat(
		wellFormedFrom(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), fzSeq, 0, ifinfo),
		wellFormedFrom(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), fzSeq, fzPidA, ifinfo),
		wellFormedFrom(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), fzSeq, fzPidA, ifinfo),
		wellFormedFrom(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), fzSeq, fzPidA, zeros(4)),
	))
	// The seq collision: two sockets open transactions on one seq, then answer
	// in order. This is the seed that reaches the ambiguity branch.
	f.Add(concat(
		wellFormedFrom(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), fzSeq, 0, ifinfo),
		wellFormedFrom(uint16(unix.RTM_GETLINK), uint16(unix.NLM_F_REQUEST), fzSeq, 0, ifinfo),
		wellFormedFrom(uint16(unix.RTM_NEWLINK), 0, fzSeq, fzPidA, ifinfo),
		wellFormedFrom(uint16(unix.RTM_NEWLINK), 0, fzSeq, fzPidB, ifinfo),
	))
	// A notification: no NLM_F_REQUEST, pid 0. Nothing subscribed, so its
	// presence is a capture-hygiene failure rather than a parse failure.
	f.Add(wellFormedFrom(uint16(unix.RTM_NEWADDR), 0, 0, 0, zeros(xtcpnl.IfAddrmsgSizeCst)))
	// NLMSG_ERROR carrying errno 0, which is an ACK and still closes.
	f.Add(concat(
		wellFormedFrom(uint16(unix.RTM_GETADDR), uint16(unix.NLM_F_REQUEST), fzSeq, 0, zeros(xtcpnl.IfAddrmsgSizeCst)),
		wellFormedFrom(uint16(unix.NLMSG_ERROR), 0, fzSeq, fzPidA, zeros(20)),
	))

	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := WalkDatagram(uint16(unix.NETLINK_ROUTE), data)
		if err != nil {
			if !IsBadCapture(err) {
				t.Fatalf("err = %v, want ErrBadHead or ErrShortDatagram", err)
			}
			return
		}

		s := Segment(d.Msgs)

		// Conservation. Every transaction contributes exactly one request, so
		// the four buckets must sum to the message count.
		accounted := len(s.Txns) + len(s.Notifications) + len(s.Orphans)
		for i := range s.Txns {
			accounted += len(s.Txns[i].Replies)
		}
		if accounted != len(d.Msgs) {
			t.Fatalf("accounted for %d messages (%d txns + %d replies + %d notifications "+
				"+ %d orphans), but the stream held %d: a message was dropped or "+
				"counted twice", accounted, len(s.Txns),
				accounted-len(s.Txns)-len(s.Notifications)-len(s.Orphans),
				len(s.Notifications), len(s.Orphans), len(d.Msgs))
		}

		for i := range s.Txns {
			txn := &s.Txns[i]

			if !txn.Request.IsRequest() {
				t.Fatalf("txn[%d] was opened by a message without NLM_F_REQUEST "+
					"(flags 0x%04x)", i, txn.Request.Hdr.Flags)
			}
			if txn.Seq != txn.Request.Hdr.Seq {
				t.Fatalf("txn[%d] Seq = %d, but its request carried seq %d",
					i, txn.Seq, txn.Request.Hdr.Seq)
			}

			// Pid 0 means unbound, and binding is the same event as attaching
			// the first reply — so the two must agree in both directions. A
			// reply cannot carry pid 0 (that path becomes a notification), which
			// is what makes 0 usable as the unbound sentinel.
			if (txn.Pid == 0) != (len(txn.Replies) == 0) {
				t.Fatalf("txn[%d] Pid = %d with %d replies: a bound transaction "+
					"must have at least one reply and vice versa",
					i, txn.Pid, len(txn.Replies))
			}

			if txn.Ambiguous && len(txn.Replies) == 0 {
				t.Fatalf("txn[%d] is Ambiguous with no replies; ambiguity is a "+
					"property of attaching a reply", i)
			}
			if txn.Closed() && len(txn.Replies) == 0 {
				t.Fatalf("txn[%d] is %s with no replies; only a reply can close a "+
					"transaction", i, txn.State)
			}

			for j := range txn.Replies {
				r := txn.Replies[j]
				if r.IsRequest() {
					t.Fatalf("txn[%d] reply[%d] has NLM_F_REQUEST set", i, j)
				}
				if r.Hdr.Pid != txn.Pid {
					t.Fatalf("txn[%d] reply[%d] carries pid %d but the transaction is "+
						"bound to %d; a transaction belongs to one socket",
						i, j, r.Hdr.Pid, txn.Pid)
				}
				if r.Hdr.Seq != txn.Seq {
					t.Fatalf("txn[%d] reply[%d] carries seq %d, not the transaction's %d",
						i, j, r.Hdr.Seq, txn.Seq)
				}
			}

			// The recorded end state must agree with the message that caused
			// it, which is always the last attached reply: closing removes the
			// transaction from the open set, so nothing attaches afterwards.
			if txn.Closed() {
				last := txn.Replies[len(txn.Replies)-1]
				switch txn.State {
				case TxnClosedByDone:
					if last.Hdr.Type != uint16(unix.NLMSG_DONE) {
						t.Fatalf("txn[%d] is closed-by-done but its last reply is type %d",
							i, last.Hdr.Type)
					}
				case TxnClosedByError:
					if last.Hdr.Type != uint16(unix.NLMSG_ERROR) {
						t.Fatalf("txn[%d] is closed-by-error but its last reply is type %d",
							i, last.Hdr.Type)
					}
				case TxnClosedBySingleReply:
					if last.IsMulti() {
						t.Fatalf("txn[%d] is closed-by-single-reply but its last reply "+
							"has NLM_F_MULTI set", i)
					}
				case TxnOpen:
					t.Fatalf("txn[%d] reports Closed() while in state %s", i, txn.State)
				default:
					t.Fatalf("txn[%d] is in unknown state %d", i, txn.State)
				}
			}
		}

		// A notification is a non-request with pid 0; an orphan is a non-request
		// with a pid that matched no open transaction. The pid test is what
		// separates them, so neither may hold the other's shape.
		for i, n := range s.Notifications {
			if n.IsRequest() || n.Hdr.Pid != 0 {
				t.Fatalf("notification[%d] has flags 0x%04x and pid %d; want a "+
					"non-request with pid 0", i, n.Hdr.Flags, n.Hdr.Pid)
			}
		}
		for i, o := range s.Orphans {
			if o.IsRequest() || o.Hdr.Pid == 0 {
				t.Fatalf("orphan[%d] has flags 0x%04x and pid %d; a pid-0 non-request "+
					"is a notification, not an orphan", i, o.Hdr.Flags, o.Hdr.Pid)
			}
		}

		pids := s.Pids()
		bound := 0
		for i := range s.Txns {
			if s.Txns[i].Pid != 0 {
				bound++
			}
		}
		covered := 0
		for i, p := range pids {
			if p == 0 {
				t.Fatalf("Pids()[%d] is 0, which is the unbound sentinel", i)
			}
			if i > 0 && p <= pids[i-1] {
				t.Fatalf("Pids() is not strictly ascending at %d: %v", i, pids)
			}
			n := len(s.TxnsForPid(p))
			if n == 0 {
				t.Fatalf("Pids() reports pid %d but TxnsForPid returns nothing", p)
			}
			covered += n
		}
		if covered != bound {
			t.Fatalf("TxnsForPid over Pids() covers %d transactions, but %d are bound: "+
				"a bound transaction is unreachable by pid", covered, bound)
		}

		// Clean() is all four conditions at once, so recompute them and demand
		// the biconditional — a Clean() that drifted either way would make a
		// polluted capture gate, or a usable one refuse to.
		wantClean := len(s.Notifications) == 0 && len(s.Orphans) == 0
		for i := range s.Txns {
			if s.Txns[i].Ambiguous || !s.Txns[i].Closed() {
				wantClean = false
			}
		}
		if s.Clean() != wantClean {
			t.Fatalf("Clean() = %v, want %v (%d notifications, %d orphans, %d txns)",
				s.Clean(), wantClean, len(s.Notifications), len(s.Orphans), len(s.Txns))
		}
	})
}

// FuzzDecodeAttrs fuzzes the attribute splitter. Its inputs are message bodies,
// which in production come straight out of a datagram the walker already
// accepted, so the interesting failures are length arithmetic rather than
// framing.
//
// go test ./pkg/nlparity/ -run FuzzDecodeAttrs -fuzz FuzzDecodeAttrs
func FuzzDecodeAttrs(f *testing.F) {
	f.Add(uint16(unix.RTM_GETLINK), []byte(nil))
	f.Add(uint16(unix.RTM_GETLINK), zeros(xtcpnl.IfInfomsgSizeCst))
	f.Add(uint16(unix.RTM_GETADDR), zeros(xtcpnl.IfAddrmsgSizeCst))
	f.Add(uint16(0xffff), zeros(64))

	f.Fuzz(func(t *testing.T, msgType uint16, body []byte) {
		hdr, attrs, remainder := DecodeAttrs(msgType, body)

		if len(hdr) > len(body) {
			t.Fatalf("family header = %d bytes, longer than the %d-byte body",
				len(hdr), len(body))
		}
		if len(remainder) > len(body) {
			t.Fatalf("remainder = %d bytes, longer than the %d-byte body",
				len(remainder), len(body))
		}

		// An unknown message type must yield nothing decoded and the whole body
		// back, rather than guessing a family header size.
		if FamilyHdrLen(msgType) < 0 {
			if len(attrs) != 0 || len(remainder) != len(body) {
				t.Fatalf("unknown msgType %d decoded %d attrs and returned a %d-byte "+
					"remainder; want 0 attrs and the whole %d-byte body",
					msgType, len(attrs), len(remainder), len(body))
			}
			return
		}

		total := len(hdr)
		for i, a := range attrs {
			if len(a.Val) > len(body) {
				t.Fatalf("attr[%d] value = %d bytes, longer than the body", i, len(a.Val))
			}
			total += xtcpnl.RTAttrSizeCst + len(a.Val)
		}
		if total > len(body) {
			t.Fatalf("header plus attributes = %d bytes, more than the %d-byte body",
				total, len(body))
		}
	})
}
