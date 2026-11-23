package gotipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Msg is one netlink message of a reply: the fields of its nlmsghdr the
// clone reads, and the body past it.
type Msg struct {
	Type  uint16
	Flags uint16
	Seq   uint32
	PID   uint32
	Body  []byte
}

// Source is where gotipc's requests go, and what tipc reads from a TIPC
// socket rather than netlink. Every netlink request is a request/reply
// exchange (tipc opens a fresh genl socket for each per-socket and
// per-bearer follow-up, but each still carries one exchange), so one Talk is
// one request and the messages that answer it.
//
// OwnAddr is what `node get address` reads (getsockname on an AF_TIPC
// socket, node.c cmd_node_get_addr), and NodeName is hash2nodestr's
// SIOCGETNODEID ioctl (misc.c): the identity of the node a hash names, as
// nodeid2str prints it. Neither crosses netlink, so a capture does not hold
// them.
type Source interface {
	Talk(req []byte) (Reply, error)
	OwnAddr() uint32
	NodeName(hash uint32) string
}

// Reply is a request's answer: its messages up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it.
type Reply struct {
	Msgs []Msg
}

// ErrReplay is a replay that does not match the capture: a request the
// capture did not make, or more requests than it holds. Run exits 2.
var ErrReplay = errors.New("gotipc: replay")

const netlinkHeaderLen = xtcpnl.NlMsgHdrSizeCst

// splitMsgs splits a datagram into messages, as mnl_nlmsg_ok walks them; a
// remainder too short to be one ends it.
func splitMsgs(b []byte) []Msg {
	var out []Msg
	for len(b) >= netlinkHeaderLen {
		l := int(binary.LittleEndian.Uint32(b[0:4]))
		if l < netlinkHeaderLen || l > len(b) {
			break
		}
		out = append(out, Msg{
			Type:  binary.LittleEndian.Uint16(b[4:6]),
			Flags: binary.LittleEndian.Uint16(b[6:8]),
			Seq:   binary.LittleEndian.Uint32(b[8:12]),
			PID:   binary.LittleEndian.Uint32(b[12:16]),
			Body:  b[netlinkHeaderLen:l],
		})
		b = b[min(len(b), l+xtcpnl.FourByteAlignPadding(l)):]
	}
	return out
}

// isRequest reports whether a full message carries NLM_F_REQUEST.
func isRequest(full []byte) bool {
	if len(full) < netlinkHeaderLen {
		return false
	}
	return binary.LittleEndian.Uint16(full[6:8])&unix.NLM_F_REQUEST != 0
}

// splitFull splits a datagram into whole messages, bytes and all, for the
// request side where the full wire message is matched.
func splitFull(b []byte) [][]byte {
	var out [][]byte
	for len(b) >= netlinkHeaderLen {
		l := int(binary.LittleEndian.Uint32(b[0:4]))
		if l < netlinkHeaderLen || l > len(b) {
			break
		}
		out = append(out, bytes.Clone(b[:l]))
		b = b[min(len(b), l+xtcpnl.FourByteAlignPadding(l)):]
	}
	return out
}

type replayTx struct {
	req     []byte
	replies []Msg
}

// ReplaySource answers from an nlmon (ws) pcap of tipc: the messages are
// walked in capture order, each request starting a transaction that the
// non-request messages after it (up to the next request) answer. A request
// replays the next recorded transaction, matched byte for byte once
// nlmsg_seq and nlmsg_pid are zeroed (tipc's seq is the time it ran). Unlike
// godevlink's, the genl command byte IS matched: TIPC's command enum is
// append-only, so the capture kernel's numbers are upstream's.
//
// The socket state comes from the node: Own is the node's identity as
// `tipc node set identity` gave it ("" for none) and Addr its address (0
// until a bearer is enabled), which the caller knows from the setup the
// capture ran after (cmd.json's pre).
type ReplaySource struct {
	txs  []replayTx
	next int
	Own  string
	Addr uint32
}

// OwnAddr is the node address the capture's node had.
func (s *ReplaySource) OwnAddr() uint32 { return s.Addr }

// NodeName is SIOCGETNODEID as the kernel answers it for this node. Hash 0,
// or the node's own address, names the node itself. Any other hash is
// inverted as the kernel's hash128to32 makes it from an identity of four
// bytes or fewer, its big-endian bytes (net/tipc/addr.h); a longer
// identity's hash cannot be inverted, and a capture holds no other way to
// learn it.
func (s *ReplaySource) NodeName(hash uint32) string {
	if hash == 0 || hash == s.Addr {
		return nodeIDString(nodeIDBytes(s.Own))
	}
	var id [16]byte
	binary.BigEndian.PutUint32(id[:4], hash)
	return nodeIDString(id)
}

// NewReplaySource reads the ws pcap at path.
func NewReplaySource(path string) (*ReplaySource, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(ErrReplay, err)
	}
	_, recs, err := xtcpnl.ParseNetlinkPcap(bs)
	if err != nil {
		return nil, errors.Join(ErrReplay, err)
	}
	s := &ReplaySource{}
	for _, r := range recs {
		_, dg, perr := r.NetlinkPayload()
		if perr != nil {
			return nil, errors.Join(ErrReplay, perr)
		}
		for _, full := range splitFull(dg) {
			if isRequest(full) {
				s.txs = append(s.txs, replayTx{req: full})
				continue
			}
			if len(s.txs) == 0 {
				continue // a reply before any request: not ours.
			}
			s.txs[len(s.txs)-1].replies = append(s.txs[len(s.txs)-1].replies, splitMsgs(full)...)
		}
	}
	return s, nil
}

// canon is a request with nlmsg_seq and nlmsg_pid zeroed.
func canon(req []byte) []byte {
	b := bytes.Clone(req)
	if len(b) >= netlinkHeaderLen {
		clear(b[8:16])
	}
	return b
}

// Talk answers req with the next recorded transaction, or ErrReplay.
func (s *ReplaySource) Talk(req []byte) (Reply, error) {
	if s.next >= len(s.txs) {
		return Reply{}, fmt.Errorf("%w: request %d is past the %d the capture has", ErrReplay, s.next, len(s.txs))
	}
	tx := s.txs[s.next]
	if !bytes.Equal(canon(req), canon(tx.req)) {
		return Reply{}, fmt.Errorf("%w: request %d differs from the capture's\n gotipc %x\n tipc   %x",
			ErrReplay, s.next, canon(req), canon(tx.req))
	}
	s.next++
	return Reply{Msgs: tx.replies}, nil
}

// Pending is how many recorded requests were not asked for.
func (s *ReplaySource) Pending() int { return len(s.txs) - s.next }

// nodeIDBytes is str2nodeid (misc.c) for the identities the corpus sets: a
// name of 16 bytes or fewer is copied; anything else is read as hex.
func nodeIDBytes(s string) [16]byte {
	var id [16]byte
	if s == "" {
		return id
	}
	if len(s) <= 16 && isNodeName([]byte(s)) {
		copy(id[:], s)
		return id
	}
	for i := 0; i < 16 && 2*i+1 < len(s); i++ {
		var b byte
		if _, err := fmt.Sscanf(s[2*i:2*i+2], "%02x", &b); err != nil {
			break
		}
		id[i] = b
	}
	return id
}

// isNodeName is is_name (misc.c) over id with its trailing NULs dropped:
// at most 16 bytes, each a letter, digit or one of "-_.:@". An all-NUL id
// is a name, the empty one.
func isNodeName(id []byte) bool {
	last := len(id) - 1
	for last >= 0 && id[last] == 0 {
		last--
	}
	if last > 15 {
		return false
	}
	for _, c := range id[:last+1] {
		ok := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
			c == '-' || c == '_' || c == '.' || c == ':' || c == '@'
		if !ok {
			return false
		}
	}
	return true
}

// nodeIDString is nodeid2str (misc.c): a name prints as itself, anything
// else as 32 hex digits with its trailing '0' digits dropped.
func nodeIDString(id [16]byte) string {
	if isNodeName(id[:]) {
		n := 16
		for n > 0 && id[n-1] == 0 {
			n--
		}
		return string(id[:n])
	}
	s := []byte(fmt.Sprintf("%x", id[:]))
	n := len(s)
	for n > 0 && s[n-1] == '0' {
		n--
	}
	return string(s[:n])
}
