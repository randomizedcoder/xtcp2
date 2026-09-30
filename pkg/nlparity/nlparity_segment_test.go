package nlparity

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The segmenter's table — §8.9's first half.
//
// Every positive row is a real nlmon capture, and the five of them together are
// the measurement that the attribution rule was written from rather than
// guessed at. Three of the numbers in them contradicted something written down
// before they were taken; see the row comments.
//
// Constructed streams appear only in the negative, boundary and corner rows,
// and two of those cases exist in no capture at all: a capture with an
// ambiguous same-seq collision would need two sockets to have outstanding
// requests simultaneously, which none of the committed fixtures manage even
// though eleven of them share one seq.

// segRow is one segmentation case.
//
// filename and stream are mutually exclusive, and a positive row may not set
// stream — the same provenance rule the walker's table enforces, for the same
// reason: a hand-built "positive" asserts only that the author and the code
// share an assumption.
type segRow struct {
	description string

	filename string // real capture, parsed with ParseRouteCaptureFile
	stream   []Msg  // constructed message stream; non-positive rows only

	// sidecar names where a number came from, so a reader can re-derive it.
	sidecar string

	wantTxns          int
	wantNotifications int
	wantOrphans       int
	wantClean         bool

	// wantPidCount is the number of DISTINCT bound port ids, which is the
	// sentinel for "how many sockets is this capture actually of". 0 means the
	// row does not assert it; a row wanting to assert zero asserts it through
	// wantPids instead.
	wantPidCount int
	// wantPids is the exact sorted set. nil = not asserted.
	wantPids []uint32

	// Per-transaction expectations, indexed by transaction. nil = not
	// asserted; a non-nil slice must be the full length of Txns.
	wantStates    []TxnState
	wantReplies   []int
	wantAmbiguous []bool
	wantTxnPids   []uint32

	// check is the pointed assertion for rows whose value is in one specific
	// transaction or message rather than in the counts.
	check func(t *testing.T, c Capture, s Segmentation)
}

// segReq builds a request: NLM_F_REQUEST set and nlmsg_pid 0, which is what
// iproute2 puts on the wire (the kernel fills the peer port id only on the way
// back).
func segReq(msgType uint16, seq uint32) Msg {
	return segMsg(msgType, uint16(unix.NLM_F_REQUEST|unix.NLM_F_ROOT|unix.NLM_F_MATCH), seq, 0)
}

// segReplyMulti builds one part of a multipart reply: NLM_F_MULTI set, so it
// does not close the transaction on its own.
func segReplyMulti(msgType uint16, seq, pid uint32) Msg {
	return segMsg(msgType, uint16(unix.NLM_F_MULTI), seq, pid)
}

// segReplySingle builds a non-multipart reply — the whole answer to a
// single-get, which closes the transaction with no NLMSG_DONE to follow.
func segReplySingle(msgType uint16, seq, pid uint32) Msg {
	return segMsg(msgType, 0, seq, pid)
}

// segNotification builds a multicast event: no NLM_F_REQUEST and nlmsg_pid 0,
// because nobody asked for it.
func segNotification(msgType uint16) Msg { return segMsg(msgType, 0, 0, 0) }

// segMsg is the common constructor. Body stays nil: the segmenter reads only
// the header, and giving it a body would suggest otherwise.
func segMsg(msgType, flags uint16, seq, pid uint32) Msg {
	return Msg{Hdr: xtcpnl.NlMsgHdr{
		Len:   uint32(xtcpnl.NlMsgHdrSizeCst),
		Type:  msgType,
		Flags: flags,
		Seq:   seq,
		Pid:   pid,
	}}
}

// checkSegProvenance enforces segRow's two invariants before anything is
// segmented.
func checkSegProvenance(t *testing.T, description, filename string, stream []Msg) {
	t.Helper()

	switch {
	case filename != "" && stream != nil:
		t.Fatalf("row %q sets both filename and stream; they are mutually exclusive", description)
	case filename == "" && stream == nil:
		t.Fatalf("row %q sets neither filename nor stream", description)
	}

	if classOf(description) == "positive" && stream != nil {
		t.Fatalf("row %q is positive but uses a constructed stream; positive "+
			"segmentation expectations must come from a real capture", description)
	}
}

// Fixture paths for the guest corpus. The 7_1_8 bulk captures are already
// named in testdata_test.go; these are the in-guest ones, which are the only
// captures in the repo taken in a namespace with no side traffic.
const (
	tdGuest         = "../xtcpnl/testdata/7_1_4/dumps"
	tdGuestGetAddr  = tdGuest + "/netlink_route_getaddr.pcap"
	tdGuestGetNeigh = tdGuest + "/netlink_route_getneigh.pcap"
	tdGuestLinkDev  = tdGuest + "/netlink_route_getlink_dev.pcap"

	// `ip neigh show dev NAME`. Its txn 0 is byte-identical to
	// tdGuestGetNeigh's, which is the property its allowlist entry rests on
	// and the reason it still needs a file of its own: the whole difference
	// between the two commands is eight bytes on txn 1.
	tdGuestGetNeighDev = tdGuest + "/netlink_route_getneigh_dev.pcap"

	// `ip neigh show proxy`. The narrowest request delta in the corpus and
	// the only one that is a single byte: its txn 1 body is
	// 00*10 08 00 where the bare command's is 00*12, the 0x08 being
	// NTF_PROXY at ndm_flags (offset 10). Measured, not derived — both
	// bodies are printed in the row below's sidecar.
	//
	// That one byte changes which TABLE the kernel walks, not how much of
	// one it returns (net/core/neighbour.c:2955-2957 picks
	// pneigh_dump_table over neigh_dump_table), so this capture's replies
	// are disjoint from tdGuestGetNeigh's rather than a subset — the
	// opposite of what tdGuestGetNeighDev's eight bytes do.
	tdGuestGetNeighProxy = tdGuest + "/netlink_route_getneigh_proxy.pcap"
)

// TestSegment is the attribution table.
//
// go test ./pkg/nlparity/ -run TestSegment
func TestSegment(t *testing.T) {
	tests := []segRow{
		{
			// The cleanest capture in the repo, and plan fact 5's "clean" row
			// made executable: 13 messages, one request, twelve replies, one
			// socket.
			description:   "positive: a clean single dump is one transaction on one socket, closed by NLMSG_DONE",
			filename:      tdBulkGetLink,
			sidecar:       "13 msgs, portids {0:1, 106975:12}",
			wantTxns:      1,
			wantClean:     true,
			wantPids:      []uint32{106975},
			wantStates:    []TxnState{TxnClosedByDone},
			wantReplies:   []int{12},
			wantTxnPids:   []uint32{106975},
			wantAmbiguous: []bool{false},
		},
		{
			// **The row the whole design exists for.** All eleven transactions
			// carry nlmsg_seq 1789012355, because rtnl_open seeds seq from
			// time(NULL) (lib/libnetlink.c:249) and these sockets were opened
			// in the same second. Keying on seq alone yields one transaction
			// with 85 replies; keying on the reply pid yields eleven, and the
			// plan expected this collision to have to be synthesized.
			//
			// Not ambiguous, and the reason is worth stating: each side-get is
			// answered before the next is sent, so there is never more than one
			// unbound transaction open at that seq.
			description:  "positive: eleven single-gets sharing one nlmsg_seq are eleven transactions, split by reply pid",
			filename:     tdBulkGetRoute,
			sidecar:      "96 msgs; 11 txns all at seq 1789012355; 11 distinct pids",
			wantTxns:     11,
			wantClean:    true,
			wantPidCount: 11,
			check: func(t *testing.T, _ Capture, s Segmentation) {
				const wantSeq = 1789012355
				for i := range s.Txns {
					if s.Txns[i].Seq != wantSeq {
						t.Errorf("txn %d seq = %d, want %d — the collision is the point of the row",
							i, s.Txns[i].Seq, wantSeq)
					}
					if s.Txns[i].Ambiguous {
						t.Errorf("txn %d is ambiguous; each side-get is answered before the next is sent", i)
					}
				}
				// The route dump first, then ten link single-gets: the dump is
				// multipart and the side-gets are not, so the two close by
				// different rules in the one capture.
				if got := s.Txns[0].State; got != TxnClosedByDone {
					t.Errorf("txn 0 (the route dump) state = %s, want %s", got, TxnClosedByDone)
				}
				for i := 1; i < len(s.Txns); i++ {
					if got := s.Txns[i].State; got != TxnClosedBySingleReply {
						t.Errorf("txn %d (an ll_link_get) state = %s, want %s", i, got, TxnClosedBySingleReply)
					}
				}
			},
		},
		{
			// `ip addr show` in the clean guest namespace: the ll_init_map link
			// dump, then the address dump, on ONE socket with consecutive seqs.
			// This is the L1 transaction-count assertion's reference value — a
			// goip that forgets the RTM_GETLINK first shows up here as 1 txn
			// instead of 2.
			description: "positive: `ip addr show` is the ll_init_map link dump then the addr dump, one socket",
			filename:    tdGuestGetAddr,
			sidecar:     "dumps/topology; seqs 1790540819 then 1790540820",
			wantTxns:    2,
			wantClean:   true,
			wantPids:    []uint32{825},
			wantStates:  []TxnState{TxnClosedByDone, TxnClosedByDone},
			wantReplies: []int{4, 7},
			wantTxnPids: []uint32{825, 825},
			check: func(t *testing.T, _ Capture, s Segmentation) {
				if got := s.Txns[0].Request.Hdr.Type; got != uint16(unix.RTM_GETLINK) {
					t.Errorf("txn 0 request type = %d, want RTM_GETLINK (%d)", got, unix.RTM_GETLINK)
				}
				if got := s.Txns[1].Request.Hdr.Type; got != uint16(unix.RTM_GETADDR) {
					t.Errorf("txn 1 request type = %d, want RTM_GETADDR (%d)", got, unix.RTM_GETADDR)
				}
				if s.Txns[1].Seq != s.Txns[0].Seq+1 {
					t.Errorf("seqs %d and %d are not consecutive; both dumps are on one socket",
						s.Txns[0].Seq, s.Txns[1].Seq)
				}
			},
		},
		{
			// `ip link show dev goip0`, the one non-dump measurement in the
			// guest corpus. Both transactions close on a single non-MULTI reply
			// with no NLMSG_DONE — which is exactly the shape
			// xtcpnl.DumpRtnetlink blocks on and TalkRtnetlink exists for, now
			// visible in a capture rather than argued from C source.
			description:  "positive: a non-dump `ip link show dev` closes on a single non-MULTI reply, no NLMSG_DONE",
			filename:     tdGuestLinkDev,
			sidecar:      "dumps/ip_link_dev; 4 datagrams, 4 messages",
			wantTxns:     2,
			wantClean:    true,
			wantPidCount: 2,
			wantStates:   []TxnState{TxnClosedBySingleReply, TxnClosedBySingleReply},
			wantReplies:  []int{1, 1},
			check: func(t *testing.T, _ Capture, s Segmentation) {
				for i := range s.Txns {
					for _, r := range s.Txns[i].Replies {
						if r.IsMulti() {
							t.Errorf("txn %d reply has NLM_F_MULTI; a single-get reply must not", i)
						}
						if r.Hdr.Type == uint16(unix.NLMSG_DONE) {
							t.Errorf("txn %d carries an NLMSG_DONE; a single-get gets none", i)
						}
					}
				}
			},
		},
		{
			// `ip neigh show`. The plan asserted from ip/ipneigh.c:601 that
			// this command sends an up-front RTM_GETLINK dump via ll_init_map,
			// "byte-identical to `ip` by construction". Here it is measured:
			// two transactions, RTM_GETLINK before RTM_GETNEIGH, one socket.
			description: "positive: `ip neigh show` sends ll_init_map's link dump before the neighbor dump",
			filename:    tdGuestGetNeigh,
			sidecar:     "ip/ipneigh.c:601; dumps/ip_neigh",
			wantTxns:    2,
			wantClean:   true,
			wantPids:    []uint32{978},
			wantStates:  []TxnState{TxnClosedByDone, TxnClosedByDone},
			// 12 neighbor replies, not the 6 this row carried before
			// nltopo::build_clean gained five flagged entries. The count is
			// the dump's, not `ip neigh`'s nine printed lines: the default
			// state filter hides some of what the kernel sends.
			wantReplies: []int{4, 12},
			check: func(t *testing.T, _ Capture, s Segmentation) {
				if got := s.Txns[0].Request.Hdr.Type; got != uint16(unix.RTM_GETLINK) {
					t.Errorf("txn 0 request type = %d, want RTM_GETLINK (%d)", got, unix.RTM_GETLINK)
				}
				if got := s.Txns[1].Request.Hdr.Type; got != uint16(unix.RTM_GETNEIGH) {
					t.Errorf("txn 1 request type = %d, want RTM_GETNEIGH (%d)", got, unix.RTM_GETNEIGH)
				}
			},
		},
		{
			// `ip neigh show proxy`, and the point of the row is the check:
			// the two transactions are the SAME SHAPE as the bare command's
			// — ll_init_map's link dump, then one neighbor dump, one socket,
			// both closed by NLMSG_DONE — because `proxy` adds no
			// transaction and reorders none. What it changes is twelve bytes
			// of one body, of which eleven are the same as before.
			//
			// Three replies against the bare command's twelve, and that is
			// not a narrower filter: pneigh_dump_table and neigh_dump_table are
			// different tables (net/core/neighbour.c:2955-2957), so these
			// two proxy entries appear in NO other capture in the repo. That
			// is why the fixture exists at all rather than the comparator
			// replaying netlink_route_getneigh.pcap and filtering it.
			description: "positive: `ip neigh show proxy` is the same two transactions as the bare command, one byte apart",
			filename:    tdGuestGetNeighProxy,
			sidecar:     "dumps/ip_neigh_proxy; txn 1 body 00*10 08 00 vs the bare command's 00*12",
			wantTxns:    2,
			wantClean:   true,
			wantPids:    []uint32{1107},
			wantStates:  []TxnState{TxnClosedByDone, TxnClosedByDone},
			wantReplies: []int{4, 3},
			check: func(t *testing.T, _ Capture, s Segmentation) {
				if got := s.Txns[0].Request.Hdr.Type; got != uint16(unix.RTM_GETLINK) {
					t.Errorf("txn 0 request type = %d, want RTM_GETLINK (%d)", got, unix.RTM_GETLINK)
				}
				if got := s.Txns[1].Request.Hdr.Type; got != uint16(unix.RTM_GETNEIGH) {
					t.Errorf("txn 1 request type = %d, want RTM_GETNEIGH (%d)", got, unix.RTM_GETNEIGH)
				}

				// The one byte, asserted against the bare command's own
				// capture rather than against a literal, so that a re-capture
				// of either file cannot quietly make the claim false. The
				// ndmsg is bare — no attribute follows — on both sides, which
				// is the other half of "one byte apart".
				bare, err := ParseRouteCaptureFile(tdGuestGetNeigh)
				if err != nil {
					t.Fatalf("%s: %v", tdGuestGetNeigh, err)
				}
				want := SegmentCapture(bare).Txns[1].Request.Body
				got := s.Txns[1].Request.Body
				if len(got) != len(want) {
					t.Fatalf("proxy ndmsg is %d bytes, the bare command's is %d; `proxy` adds no attribute",
						len(got), len(want))
				}
				for i := range got {
					wantByte := want[i]
					if i == 10 {
						wantByte = unix.NTF_PROXY
					}
					if got[i] != wantByte {
						t.Errorf("proxy ndmsg byte %d = 0x%02x, want 0x%02x", i, got[i], wantByte)
					}
				}
			},
		},
		{
			// A real committed fixture that is NOT a usable parity input, and
			// that is the finding. netlink_route_getlink_dump.pcap was
			// extracted for the decoder tests by keeping the replies and
			// throwing the request away, so it has nothing to attribute to:
			// every one of its twelve messages is an orphan and Clean is false.
			//
			// The renderer tests are right to use it — they want replies. A
			// comparator pointed at it must refuse rather than report a clean
			// zero-transaction diff, which is what this row pins.
			description: "negative: an extracted reply-only stream attributes nothing — every reply is an orphan",
			filename:    tdGetLinkDump,
			sidecar:     "testdata_test.go: 'keep the replies and throw the request away'",
			wantTxns:    0,
			wantOrphans: 12,
			wantClean:   false,
			wantPids:    []uint32{},
			check: func(t *testing.T, _ Capture, s Segmentation) {
				var newlink, done int
				for _, m := range s.Orphans {
					switch m.Hdr.Type {
					case uint16(unix.RTM_NEWLINK):
						newlink++
					case uint16(unix.NLMSG_DONE):
						done++
					}
					if m.Hdr.Pid == 0 {
						t.Errorf("orphan has pid 0, which would make it a notification, not an orphan")
					}
				}
				if newlink != 11 || done != 1 {
					t.Errorf("orphans = %d RTM_NEWLINK + %d NLMSG_DONE, want 11 + 1", newlink, done)
				}
			},
		},
		{
			// The hygiene rule. A read-only cut subscribes to no multicast
			// group, so a non-request with pid 0 means the capture window
			// caught someone else's event. It is recorded, not dropped: a
			// comparator that drops it reports green on a capture it did not
			// understand.
			description:       "negative: a multicast notification is recorded as a hygiene failure, not dropped",
			stream:            []Msg{segNotification(uint16(unix.RTM_NEWADDR))},
			wantTxns:          0,
			wantNotifications: 1,
			wantClean:         false,
			wantPids:          []uint32{},
		},
		{
			// The plan lists "a reply preceding its request in capture order"
			// as a case that must be handled or explicitly reported. It is
			// reported: there is no open transaction at that seq yet, so the
			// reply is an orphan, and the request that follows opens a
			// transaction that never gets it back. Retro-attribution is
			// deliberately not attempted — a reply that arrived before its
			// request did not come from that request.
			description: "negative: a reply preceding its own request is an orphan, not retro-attributed",
			stream: []Msg{
				segReplyMulti(uint16(unix.RTM_NEWLINK), 1, 55),
				segReq(uint16(unix.RTM_GETLINK), 1),
			},
			wantTxns:    1,
			wantOrphans: 1,
			wantClean:   false,
			wantPids:    []uint32{},
			wantStates:  []TxnState{TxnOpen},
			wantReplies: []int{0},
			wantTxnPids: []uint32{0},
		},
		{
			// NLMSG_ERROR closes the transaction regardless of the errno,
			// because an ACK is an NLMSG_ERROR with errno 0 — the reply is the
			// end of the exchange either way. The state name says how it
			// closed, not whether anything went wrong.
			description: "boundary: NLMSG_ERROR closes the transaction whether the errno is an error or an ACK",
			stream: []Msg{
				segReq(uint16(unix.RTM_GETADDR), 7),
				segReplySingle(uint16(unix.NLMSG_ERROR), 7, 9),
			},
			wantTxns:    1,
			wantClean:   true,
			wantPids:    []uint32{9},
			wantStates:  []TxnState{TxnClosedByError},
			wantReplies: []int{1},
			wantTxnPids: []uint32{9},
		},
		{
			// A request with no reply. The transaction stays open AND unbound,
			// and those are two separate facts: open because no terminator
			// arrived, unbound because a request carries pid 0 and only a reply
			// can say which socket sent it. Pid 0 here does not mean "port id
			// zero" — a reply cannot have one.
			description: "boundary: a request with no reply stays open and unbound, and Clean is false",
			stream:      []Msg{segReq(uint16(unix.RTM_GETLINK), 3)},
			wantTxns:    1,
			wantClean:   false,
			wantPids:    []uint32{},
			wantStates:  []TxnState{TxnOpen},
			wantReplies: []int{0},
			wantTxnPids: []uint32{0},
		},
		{
			// An empty stream is clean. Not a quibble: a capture whose family
			// filter excluded everything reaches the segmenter as an empty
			// message list, and "clean" there means "nothing to complain
			// about", which is why Clean alone never proves a capture is
			// usable. The transaction count is what does that.
			description: "boundary: an empty stream segments to nothing and is clean, which is why Clean is not sufficient",
			stream:      []Msg{},
			wantTxns:    0,
			wantClean:   true,
			wantPids:    []uint32{},
		},
		{
			// **The dirtiest capture in the repo, and the reason attribution
			// runs before normalization.** 251 messages, two separate `ip addr
			// show` runs, an unrelated process's eleven-transaction burst, a
			// write, and 24 notifications. Canonicalize first — zero the pids —
			// and it is one indistinguishable pile.
			//
			// Attributed, the tool's own run comes out exactly: TxnsForPid
			// returns the same two transactions, RTM_GETLINK then RTM_GETADDR,
			// for each of the two `ip` runs. And the two runs COLLIDE on both
			// seqs (1789012353 and 1789012354) without either being ambiguous,
			// because run 1 closed before run 2 opened.
			description:       "corner: the six-portid getaddr capture — per-pid attribution recovers each `ip` run exactly",
			filename:          tdBulkGetAddr,
			sidecar:           "251 msgs, 6 reply portids, 17 seqs (nlparity_capture.go)",
			wantTxns:          18,
			wantNotifications: 24,
			wantOrphans:       0,
			wantClean:         false,
			wantPidCount:      5,
			check: func(t *testing.T, c Capture, s Segmentation) {
				// Five BOUND pids, six reply pids. The sixth is 0, which
				// carries the 24 notifications and can never own a
				// transaction — which is how the census in
				// nlparity_capture.go's comment and Pids() reconcile.
				replyPids := map[uint32]int{}
				for _, m := range c.Msgs() {
					if !m.IsRequest() {
						replyPids[m.Hdr.Pid]++
					}
				}
				if len(replyPids) != 6 {
					t.Errorf("distinct reply pids = %d, want 6", len(replyPids))
				}
				if replyPids[0] != len(s.Notifications) {
					t.Errorf("pid-0 replies = %d but notifications = %d; every pid-0 reply is a notification",
						replyPids[0], len(s.Notifications))
				}

				// Each `ip addr show` run, recovered.
				for _, pid := range []uint32{106900, 106901} {
					txns := s.TxnsForPid(pid)
					if len(txns) != 2 {
						t.Errorf("pid %d has %d txns, want 2 (ll_init_map link dump + addr dump)", pid, len(txns))
						continue
					}
					if txns[0].Request.Hdr.Type != uint16(unix.RTM_GETLINK) ||
						txns[1].Request.Hdr.Type != uint16(unix.RTM_GETADDR) {
						t.Errorf("pid %d txn types = %d, %d; want RTM_GETLINK then RTM_GETADDR",
							pid, txns[0].Request.Hdr.Type, txns[1].Request.Hdr.Type)
					}
					if txns[0].Ambiguous || txns[1].Ambiguous {
						t.Errorf("pid %d has an ambiguous txn; the two runs collide on seq but not in time", pid)
					}
				}
				// The seq collision between the two runs, asserted rather than
				// implied: if these were not equal the row above would pass for
				// the wrong reason.
				a, b := s.TxnsForPid(106900), s.TxnsForPid(106901)
				if a[0].Seq != b[0].Seq || a[1].Seq != b[1].Seq {
					t.Errorf("the two runs do not collide on seq (%d,%d vs %d,%d); this row is stale",
						a[0].Seq, a[1].Seq, b[0].Seq, b[1].Seq)
				}

				// The captured write, which is its own hygiene point: a
				// read-only comparator's capture window caught an RTM_NEWADDR,
				// and the kernel's ACK is an NLMSG_ERROR carrying errno 0.
				var writes int
				for i := range s.Txns {
					tx := &s.Txns[i]
					if tx.Request.Hdr.Type != uint16(unix.RTM_NEWADDR) {
						continue
					}
					writes++
					if tx.State != TxnClosedByError {
						t.Errorf("the RTM_NEWADDR txn state = %s, want %s (an ACK is an NLMSG_ERROR)",
							tx.State, TxnClosedByError)
					}
					ack := tx.Replies[len(tx.Replies)-1]
					if len(ack.Body) < 4 {
						t.Fatalf("the ACK body is %d bytes, too short for an errno", len(ack.Body))
					}
					if errno := xtcpnl.NativeEndian().Uint32(ack.Body[:4]); errno != 0 {
						t.Errorf("the ACK errno = %d, want 0 — an ACK, not a failure", int32(errno))
					}
				}
				if writes != 1 {
					t.Errorf("captured RTM_NEWADDR requests = %d, want 1", writes)
				}
			},
		},
		{
			// The ambiguity case, which exists in no committed capture: it
			// needs two sockets with requests outstanding at the same seq
			// SIMULTANEOUSLY. Eleven fixtures share a seq and none manage it,
			// because netlink answers before the next request is sent.
			//
			// The reply binds the most recent unbound transaction, because
			// netlink is answered in order per socket — but "most recent" is a
			// guess when there are two candidates, and Ambiguous is where the
			// guess is recorded. The attachment still happens: dropping the
			// reply would understate the reply count, which is an L1 finding
			// the capture does not deserve.
			description: "corner: two unbound transactions at one seq — the reply binds the most recent and marks it ambiguous",
			stream: []Msg{
				segReq(uint16(unix.RTM_GETLINK), 5),
				segReq(uint16(unix.RTM_GETADDR), 5),
				segReplyMulti(uint16(unix.RTM_NEWADDR), 5, 77),
			},
			wantTxns:      2,
			wantClean:     false,
			wantPids:      []uint32{77},
			wantStates:    []TxnState{TxnOpen, TxnOpen},
			wantReplies:   []int{0, 1},
			wantTxnPids:   []uint32{0, 77},
			wantAmbiguous: []bool{false, true},
		},
		{
			// The other half, and the reason Ambiguous is per-transaction
			// rather than per-segmentation: once the second transaction is
			// bound to 77, a reply from 88 has exactly one unbound candidate
			// left, so binding it is a deduction and not a guess. The
			// ambiguity stays on the transaction where the guess was made and
			// does not spread.
			description: "corner: once one is bound, the next reply's bind is unambiguous — ambiguity does not spread",
			stream: []Msg{
				segReq(uint16(unix.RTM_GETLINK), 5),
				segReq(uint16(unix.RTM_GETADDR), 5),
				segReplyMulti(uint16(unix.RTM_NEWADDR), 5, 77),
				segReplyMulti(uint16(unix.RTM_NEWLINK), 5, 88),
				segReplySingle(uint16(unix.NLMSG_DONE), 5, 88),
				segReplySingle(uint16(unix.NLMSG_DONE), 5, 77),
			},
			wantTxns:      2,
			wantClean:     false,
			wantPids:      []uint32{77, 88},
			wantStates:    []TxnState{TxnClosedByDone, TxnClosedByDone},
			wantReplies:   []int{2, 2},
			wantTxnPids:   []uint32{88, 77},
			wantAmbiguous: []bool{false, true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			checkSegProvenance(t, tc.description, tc.filename, tc.stream)

			var c Capture
			if tc.filename != "" {
				var err error
				c, err = ParseRouteCaptureFile(tc.filename)
				if err != nil {
					t.Fatalf("parse %s: %v", tc.filename, err)
				}
			} else {
				c = Capture{Datagrams: []Datagram{{Msgs: tc.stream}}}
			}

			s := SegmentCapture(c)

			if len(s.Txns) != tc.wantTxns {
				t.Errorf("transactions = %d, want %d", len(s.Txns), tc.wantTxns)
			}
			if len(s.Notifications) != tc.wantNotifications {
				t.Errorf("notifications = %d, want %d", len(s.Notifications), tc.wantNotifications)
			}
			if len(s.Orphans) != tc.wantOrphans {
				t.Errorf("orphans = %d, want %d", len(s.Orphans), tc.wantOrphans)
			}
			if got := s.Clean(); got != tc.wantClean {
				t.Errorf("Clean() = %v, want %v", got, tc.wantClean)
			}

			pids := s.Pids()
			if tc.wantPidCount != 0 && len(pids) != tc.wantPidCount {
				t.Errorf("distinct bound pids = %d %v, want %d", len(pids), pids, tc.wantPidCount)
			}
			if tc.wantPids != nil {
				if len(pids) != len(tc.wantPids) {
					t.Errorf("bound pids = %v, want %v", pids, tc.wantPids)
				} else {
					for i := range pids {
						if pids[i] != tc.wantPids[i] {
							t.Errorf("bound pids = %v, want %v", pids, tc.wantPids)
							break
						}
					}
				}
			}

			assertPerTxn(t, s, tc)

			if tc.check != nil {
				tc.check(t, c, s)
			}
		})
	}
}

// assertPerTxn applies the four per-transaction expectation slices. Split out
// so the row loop above stays readable; each slice must be either nil or the
// full transaction count, so a row cannot half-assert a capture and look
// complete.
func assertPerTxn(t *testing.T, s Segmentation, tc segRow) {
	t.Helper()

	lenOK := func(what string, n int) bool {
		if n != len(s.Txns) {
			t.Errorf("row asserts %d %s for %d transactions; the slice must cover all of them",
				n, what, len(s.Txns))
			return false
		}
		return true
	}

	if tc.wantStates != nil && lenOK("states", len(tc.wantStates)) {
		for i := range s.Txns {
			if s.Txns[i].State != tc.wantStates[i] {
				t.Errorf("txn %d state = %s, want %s", i, s.Txns[i].State, tc.wantStates[i])
			}
		}
	}
	if tc.wantReplies != nil && lenOK("reply counts", len(tc.wantReplies)) {
		for i := range s.Txns {
			if len(s.Txns[i].Replies) != tc.wantReplies[i] {
				t.Errorf("txn %d replies = %d, want %d", i, len(s.Txns[i].Replies), tc.wantReplies[i])
			}
		}
	}
	if tc.wantAmbiguous != nil && lenOK("ambiguity flags", len(tc.wantAmbiguous)) {
		for i := range s.Txns {
			if s.Txns[i].Ambiguous != tc.wantAmbiguous[i] {
				t.Errorf("txn %d Ambiguous = %v, want %v", i, s.Txns[i].Ambiguous, tc.wantAmbiguous[i])
			}
		}
	}
	if tc.wantTxnPids != nil && lenOK("pids", len(tc.wantTxnPids)) {
		for i := range s.Txns {
			if s.Txns[i].Pid != tc.wantTxnPids[i] {
				t.Errorf("txn %d Pid = %d, want %d", i, s.Txns[i].Pid, tc.wantTxnPids[i])
			}
		}
	}
}
