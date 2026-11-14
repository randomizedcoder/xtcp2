package gofou

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

// Source is where gofou's requests go. `ip fou` opens one generic netlink
// socket (genl_rth) for the run; genl_init_handle resolves the family, then
// `fou show` dumps. Each Talk is one request and the messages that answer it.
type Source interface {
	Talk(req []byte) (Reply, error)
}

// Reply is a request's answer: its messages up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it.
type Reply struct {
	Msgs []Msg
}

// ErrReplay is a replay that does not match the capture: a request the
// capture did not make, or more requests than it holds. Run exits 2.
var ErrReplay = errors.New("gofou: replay")

const netlinkHeaderLen = xtcpnl.NlMsgHdrSizeCst

// splitMsgs splits a datagram into messages, as the dump walker does; a
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

// ReplaySource answers from an nlmon (ws) pcap of `ip fou`: the messages are
// walked in capture order, each request starting a transaction that the
// non-request messages after it (up to the next request) answer. A request
// replays the next recorded transaction, matched byte for byte once
// nlmsg_seq and nlmsg_pid are zeroed. The genl command byte is matched too:
// fou's command enum is upstream's, and `ip fou show`'s request is fixed
// (FOU_CMD_GET, a header-only dump), so the match is exact.
type ReplaySource struct {
	txs  []replayTx
	next int
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
		return Reply{}, fmt.Errorf("%w: request %d differs from the capture's\n gofou %x\n ip     %x",
			ErrReplay, s.next, canon(req), canon(tx.req))
	}
	s.next++
	return Reply{Msgs: tx.replies}, nil
}

// Pending is how many recorded requests were not asked for.
func (s *ReplaySource) Pending() int { return len(s.txs) - s.next }
