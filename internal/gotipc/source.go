package gotipc

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/wsreplay"
)

// Msg is one netlink message of a reply: the fields of its nlmsghdr the
// clone reads, and the body past it.
type Msg = wsreplay.Msg

// Reply is a request's answer: its messages up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it.
type Reply = wsreplay.Reply

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

// ErrReplay is a replay that does not match the capture: a request the
// capture did not make, or more requests than it holds. Run exits 2.
var ErrReplay = errors.New("gotipc: replay")

const netlinkHeaderLen = wsreplay.HeaderLen

// ReplaySource answers from an nlmon (ws) pcap of tipc (internal/wsreplay).
// A request is matched byte for byte once nlmsg_seq and nlmsg_pid are
// zeroed (tipc's seq is the time it ran). Unlike godevlink's, the genl
// command byte IS matched: TIPC's command enum is append-only, so the
// capture kernel's numbers are upstream's.
//
// The socket state comes from the node: Own is the node's identity as
// `tipc node set identity` gave it ("" for none) and Addr its address (0
// until a bearer is enabled), which the caller knows from the setup the
// capture ran after (cmd.json's pre).
type ReplaySource struct {
	*wsreplay.Replay
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
	r, err := wsreplay.Open(path, wsreplay.Options{Err: ErrReplay, Clone: "gotipc", Tool: "tipc"})
	if err != nil {
		return nil, err
	}
	return &ReplaySource{Replay: r}, nil
}

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
