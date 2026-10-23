package nlparity

import (
	"errors"
	"fmt"
	"sort"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// A write row is compared differently from a read row, because only one side
// sends the write.
//
// `ip link set la1 master bond0` is three transactions on the wire: two
// RTM_GETLINK lookups (ll_name_to_index, master first, then the device) and
// the RTM_NEWLINK itself with its NLMSG_ERROR. goip sends the lookups, the
// same ones in the same order, and then writes the RTM_NEWLINK to the file
// GOIP_WRITE_OUT names instead of sending it (link-write-path.md,
// "Parity: write rows in the matrix"). So the goip capture holds one
// transaction fewer than ip's, and the request goip did not send is a file.
//
// The comparison splits ip's final transaction off, runs the ordinary Diff
// over the lookups, and compares ip's write request with goip's file at L2.
// It is request-only: goip cannot know whether the kernel would refuse the
// write, so the NLMSG_ERROR is not compared, and a refused row (enslave_up)
// passes on its request alone.
//
// `ip addr flush` writes N messages, not one, and they are not last. Its
// capture is a lookup and a dump, then a batch of RTM_DELADDR copies of the
// dump replies with NLM_F_REQUEST alone (no ACK asked for, so each is a
// transaction that never closes), in as many datagrams as the 3584-byte
// buffer needed, then a second dump that finds nothing
// (addr-write-path.md, "Multi-write rows: flush and restore"). So the split
// is three ways: the transactions before the writes, the writes, and the
// transactions after them. goip's file holds the same N messages
// concatenated (decision 3). For every link, route and address write row N
// is 1 and nothing comes after, and the comparison is the one above.

var (
	// ErrNoWriteTxn is an `ip` capture of a write row that holds no admitted
	// write, or whose writes are not one contiguous run.
	ErrNoWriteTxn = errors.New("nlparity: the capture does not hold one run of write transactions")
	// ErrWriteCount is an `ip` capture whose run of writes is not the length
	// the row expects.
	ErrWriteCount = errors.New("nlparity: the capture holds a different number of writes")
	// ErrBadWriteFile is a GOIP_WRITE_OUT file that is not a sequence of
	// admitted write requests.
	ErrBadWriteFile = errors.New("nlparity: the write file is not a sequence of write requests")
)

// WriteSplit is an `ip` write capture cut around its writes.
type WriteSplit struct {
	// Before holds the transactions ahead of the writes: the lookups, and for
	// flush the round-1 dump. It keeps the capture's notifications and
	// orphans, so HygieneDiff still sees them.
	Before Segmentation
	// Writes are the write requests in send order.
	Writes []Msg
	// Datagrams is how many of Writes each datagram carried, in order. ip
	// sends one write per datagram except for a flush batch, which fills a
	// 3584-byte buffer; flush_many's 120 DELs went out as 47, 47 and 26.
	Datagrams []int
	// After holds the transactions behind the writes: flush's round-2 dump.
	// Empty for every single-write row.
	After []Txn
}

// SplitWrites cuts an `ip` write capture into what came before its writes,
// the writes, and what came after.
//
// The writes must be one contiguous run of exactly n transactions, n >= 1.
// A datagram boundary is where a message's offset does not follow the one
// before it: Msg.Offset is 0 at the start of each datagram.
func SplitWrites(s Segmentation, n int) (WriteSplit, error) {
	first, last := -1, -1
	for i := range s.Txns {
		if !IsWrite(s.Txns[i].Request) {
			continue
		}
		if first >= 0 && last != i-1 {
			return WriteSplit{}, fmt.Errorf("%w: transaction %d of %d is a write after a non-write",
				ErrNoWriteTxn, i, len(s.Txns))
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return WriteSplit{}, ErrNoWriteTxn
	}
	if got := last - first + 1; got != n {
		return WriteSplit{}, fmt.Errorf("%w: %d, want %d", ErrWriteCount, got, n)
	}

	w := WriteSplit{Before: s, Writes: make([]Msg, 0, n)}
	w.Before.Txns = append([]Txn(nil), s.Txns[:first]...)
	w.After = append([]Txn(nil), s.Txns[last+1:]...)
	for i := first; i <= last; i++ {
		m := s.Txns[i].Request
		if len(w.Writes) == 0 || m.Offset <= w.Writes[len(w.Writes)-1].Offset {
			w.Datagrams = append(w.Datagrams, 0)
		}
		w.Datagrams[len(w.Datagrams)-1]++
		w.Writes = append(w.Writes, m)
	}
	return w, nil
}

// others is the split with its writes taken out: the non-write transactions
// in capture order, which is what goip's capture holds.
func (w WriteSplit) others() Segmentation {
	s := w.Before
	s.Txns = append(append([]Txn(nil), w.Before.Txns...), w.After...)
	return s
}

// IsWrite reports whether m is a write the gates admit: an rtnetlink type
// xtcpnl.IsAdmittedWriteType admits, or a generic netlink family's command
// xtcpnl.IsAdmittedGenlWrite admits by the family's name. A genl message
// whose family the capture never named is not one, since only the name says
// which commands write.
func IsWrite(m Msg) bool {
	switch m.Proto {
	case unix.NETLINK_GENERIC:
		return m.IsGenl() && len(m.Body) >= genlHdrLen && xtcpnl.IsAdmittedGenlWrite(m.Genl, m.Body[0])
	case unix.NETLINK_XFRM:
		// No XFRM write is admitted yet; NEWSA's 16 is RTM_NEWLINK's.
		return false
	}
	return xtcpnl.IsAdmittedWriteType(m.Hdr.Type)
}

// ParseWriteRequests reads an rtnetlink GOIP_WRITE_OUT file: one or more
// netlink requests back to back, each of an admitted write type, each
// starting on a four-byte boundary as in a datagram. The last one ends the
// file.
func ParseWriteRequests(b []byte) ([]Msg, error) {
	return ParseWriteRequestsProto(b, unix.NETLINK_ROUTE, nil)
}

// ParseWriteRequestsProto is ParseWriteRequests for a protocol's file. A
// generic netlink request is labeled with the family families names its id,
// the map of the capture goip resolved the family in (Capture.GenlFamilies),
// and must then be a write IsWrite admits.
func ParseWriteRequestsProto(b []byte, proto uint16, families map[uint16]string) ([]Msg, error) {
	if len(b) == 0 {
		return nil, fmt.Errorf("%w: empty", ErrBadWriteFile)
	}
	var out []Msg
	for off := 0; off < len(b); {
		rest := b[off:]
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(rest, &h); err != nil {
			return nil, fmt.Errorf("%w: message %d: %w", ErrBadWriteFile, len(out), err)
		}
		if h.Len < xtcpnl.NlMsgHdrSizeCst || int(h.Len) > len(rest) {
			return nil, fmt.Errorf("%w: message %d: nlmsg_len %d with %d bytes left",
				ErrBadWriteFile, len(out), h.Len, len(rest))
		}
		m := Msg{Hdr: h, Body: rest[xtcpnl.NlMsgHdrSizeCst:h.Len], Offset: off, Proto: proto}
		if m.IsGenl() {
			m.Genl = families[h.Type]
		}
		if !m.IsRequest() || !IsWrite(m) {
			return nil, fmt.Errorf("%w: message %d: %s flags 0x%04x",
				ErrBadWriteFile, len(out), MsgTypeName(h.Type), h.Flags)
		}
		out = append(out, m)
		off += (int(h.Len) + 3) &^ 3
	}
	return out, nil
}

// DiffWrites is the write row's diff: ref's non-write transactions against
// sub's, then ref's writes against subWrites.
//
// The transactions before the writes are compared as Diff compares them. The
// ones after are compared on their requests only: flush's round-2 dump asks
// the same question on both sides, but ip's kernel has applied the batch and
// answers nothing, while goip's has not and answers the round-1 dump again
// (addr-write-path.md, decision 5). The writes are compared message by
// message after NormalizeMsg, since a DEL is a copy of a dump reply and
// carries that reply's pid and IFA_CACHEINFO stamps; each is numbered where
// it sat in ref's capture. With one write and nothing after it this is Diff
// over the lookups plus L2 over the two write requests.
func DiffWrites(command string, ref WriteSplit, sub Segmentation, subWrites []Msg) []Divergence {
	var out []Divergence
	others := ref.others()
	if len(others.Txns) != len(sub.Txns) {
		out = append(out, Divergence{
			Command: command, Level: LevelTxnCount, Class: DivergenceTransactionCount,
			Locus: "txn-count", Txn: -1,
			Ref: fmt.Sprintf("%d", len(others.Txns)), Sub: fmt.Sprintf("%d", len(sub.Txns)),
		})
	}
	before := len(ref.Before.Txns)
	for i := range min(len(others.Txns), len(sub.Txns)) {
		txn := i
		if i >= before {
			txn += len(ref.Writes)
		}
		out = append(out, diffRequest(command, txn, others.Txns[i].Request, sub.Txns[i].Request)...)
		if i < before {
			out = append(out, diffReplies(command, txn, others.Txns[i], sub.Txns[i])...)
		}
	}

	if len(ref.Writes) != len(subWrites) {
		out = append(out, Divergence{
			Command: command, Level: LevelTxnCount, Class: DivergenceTransactionCount,
			Locus: "write-count", Txn: -1,
			Ref: fmt.Sprintf("%d", len(ref.Writes)), Sub: fmt.Sprintf("%d", len(subWrites)),
		})
	}
	refWrites, subWrites := byObjectKey(ref.Writes), byObjectKey(subWrites)
	for i := range min(len(refWrites), len(subWrites)) {
		out = append(out, diffRequest(command, before+i,
			NormalizeMsg(refWrites[i]), NormalizeMsg(subWrites[i]))...)
	}
	return out
}

// byObjectKey orders a batch of writes by genlObjectKey when every one has
// a family's key, and leaves any other batch in send order. A tcp_metrics
// flush deletes in its dump's order, which is a per-namespace hash's, so ip's
// batch and goip's name the same entries in different orders; sorted, they
// pair entry by entry, and a missing or extra one still shows as a pair that
// differs. A batch is returned unsorted rather than partly sorted, so an
// rtnetlink flush keeps the order its comparison has always checked.
func byObjectKey(ws []Msg) []Msg {
	keys := make([]string, len(ws))
	for i := range ws {
		k, ok := genlObjectKey(ws[i])
		if !ok {
			return ws
		}
		keys[i] = k
	}
	idx := make([]int, len(ws))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	out := make([]Msg, len(ws))
	for i, j := range idx {
		out[i] = ws[j]
	}
	return out
}

// WritesControl is D_control for a write row: DiffWrites between the two `ip`
// captures, each split with SplitWrites.
func WritesControl(command string, ipA, ipB Segmentation, n int) ([]Divergence, error) {
	a, err := SplitWrites(ipA, n)
	if err != nil {
		return nil, fmt.Errorf("ip_a: %w", err)
	}
	b, err := SplitWrites(ipB, n)
	if err != nil {
		return nil, fmt.Errorf("ip_b: %w", err)
	}
	return DiffWrites(command, a, b.others(), b.Writes), nil
}

// CompareWrites is Compare for a write row of n writes. ref is the whole `ip`
// capture, writes included; sub is goip's capture, which holds no writes, and
// subWrites are the requests goip wrote to GOIP_WRITE_OUT.
//
// Unlike Compare it accepts zero lookups on both sides, because `ip link
// add` resolves nothing. RefTxns counts ip's writes, and SubTxns counts each
// of goip's file messages as a write, so a clean row reads as both sides
// running the same number of transactions.
func CompareWrites(command string, ref, sub Segmentation, subWrites []Msg, n int,
	control []Divergence, al *Allowlist) Report {
	r := Report{
		Command:     command,
		RefTxns:     len(ref.Txns),
		SubTxns:     len(sub.Txns) + len(subWrites),
		ControlSize: len(control),
		RefPids:     ref.Pids(),
		SubPids:     sub.Pids(),
	}
	if al != nil {
		r.Gated = al.IsGated(command)
	}

	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "ip", withoutUnackedWrites(ref))...)
	r.Hygiene = append(r.Hygiene, HygieneDiff(command, "goip", sub)...)

	split, err := SplitWrites(ref, n)
	if err != nil {
		r.Err = fmt.Errorf("reference capture: %w", err)
		return r
	}
	if sentWrite(sub) {
		r.Err = fmt.Errorf("subject capture: %w", ErrGoipSentWrite)
		return r
	}

	r.suppress(DiffWrites(command, split, sub, subWrites), control, al)
	return r
}

// withoutUnackedWrites drops the write transactions that asked for no ACK
// and got no reply. A flush DEL carries NLM_F_REQUEST alone, so the kernel
// answers only an error, and an open DEL is the expected end of it, not a
// capture that closed early. A write that asked for an ACK and has none is
// still open, and HygieneDiff still reports it.
func withoutUnackedWrites(s Segmentation) Segmentation {
	out := s
	out.Txns = make([]Txn, 0, len(s.Txns))
	for _, t := range s.Txns {
		if IsWrite(t.Request) && len(t.Replies) == 0 &&
			t.Request.Hdr.Flags&uint16(unix.NLM_F_ACK) == 0 {
			continue
		}
		out.Txns = append(out.Txns, t)
	}
	return out
}

// ErrGoipSentWrite is a goip capture that holds a write on the wire. goip
// never sends one, so this is a broken build, and it fails regardless of
// gating.
var ErrGoipSentWrite = errors.New("nlparity: goip sent a write request")

// sentWrite reports whether any transaction's request is a write.
func sentWrite(s Segmentation) bool {
	for i := range s.Txns {
		if IsWrite(s.Txns[i].Request) {
			return true
		}
	}
	return false
}
