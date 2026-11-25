// Package wsreplay is the nlmon (ws) pcap replay core the read-only clones
// share (godevlink, govdpa, gotipc, gobridge): a capture's messages walked in
// order, each request starting a transaction that the non-request messages
// after it (up to the next request) answer. A clone's request replays the
// next recorded transaction, matched byte for byte once its Canon has
// zeroed what differs between runs (nlmsg_seq and nlmsg_pid at least).
//
// Each clone keeps its own ErrReplay sentinel and message wording, so moving
// here changed neither what callers match with errors.Is nor what they print.
package wsreplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// HeaderLen is the nlmsghdr length.
const HeaderLen = xtcpnl.NlMsgHdrSizeCst

// Msg is one netlink message of a reply: the fields of its nlmsghdr the
// clones read, and the body past it.
type Msg struct {
	Type  uint16
	Flags uint16
	Seq   uint32
	PID   uint32
	Body  []byte
}

// Reply is a request's answer: its messages up to and including the
// NLMSG_DONE or NLMSG_ERROR that ends it.
type Reply struct {
	Msgs []Msg
}

// Options is what differs between clones.
type Options struct {
	// Err is the clone's replay sentinel; every error wraps it.
	Err error
	// Canon is the request as matched; nil is CanonSeqPID.
	Canon func([]byte) []byte
	// Clone and Tool name the two sides of a mismatch message ("govdpa",
	// "vdpa"); the shorter is padded so the hex lines align.
	Clone, Tool string
}

// CanonSeqPID is a request with nlmsg_seq and nlmsg_pid zeroed.
func CanonSeqPID(req []byte) []byte {
	b := bytes.Clone(req)
	if len(b) >= HeaderLen {
		clear(b[8:16])
	}
	return b
}

// CanonGenlCmd is CanonSeqPID with the genl command and version zeroed too,
// for a family whose command enum is renumbered between kernels (devlink).
func CanonGenlCmd(req []byte) []byte {
	b := CanonSeqPID(req)
	if len(b) >= HeaderLen+2 {
		clear(b[HeaderLen : HeaderLen+2])
	}
	return b
}

// SplitMsgs splits a datagram into messages, as mnl_nlmsg_ok walks them; a
// remainder too short to be one ends it.
func SplitMsgs(b []byte) []Msg {
	var out []Msg
	for len(b) >= HeaderLen {
		l := int(binary.LittleEndian.Uint32(b[0:4]))
		if l < HeaderLen || l > len(b) {
			break
		}
		out = append(out, Msg{
			Type:  binary.LittleEndian.Uint16(b[4:6]),
			Flags: binary.LittleEndian.Uint16(b[6:8]),
			Seq:   binary.LittleEndian.Uint32(b[8:12]),
			PID:   binary.LittleEndian.Uint32(b[12:16]),
			Body:  b[HeaderLen:l],
		})
		b = b[min(len(b), l+xtcpnl.FourByteAlignPadding(l)):]
	}
	return out
}

// splitFull splits a datagram into whole messages, bytes and all, for the
// request side where the full wire message is matched.
func splitFull(b []byte) [][]byte {
	var out [][]byte
	for len(b) >= HeaderLen {
		l := int(binary.LittleEndian.Uint32(b[0:4]))
		if l < HeaderLen || l > len(b) {
			break
		}
		out = append(out, bytes.Clone(b[:l]))
		b = b[min(len(b), l+xtcpnl.FourByteAlignPadding(l)):]
	}
	return out
}

// isRequest reports whether a full message carries NLM_F_REQUEST.
func isRequest(full []byte) bool {
	if len(full) < HeaderLen {
		return false
	}
	return binary.LittleEndian.Uint16(full[6:8])&unix.NLM_F_REQUEST != 0
}

// echoReply reports whether full, which carries NLM_F_REQUEST, is the
// kernel's answer to req rather than a new request: req asked for
// NLM_F_ECHO and full has its sequence number. The kernel copies a get's
// flags into its reply (tc_get_tfilter, and tc_ctl_chain, which also keeps
// RTM_GETCHAIN as the reply's type), so the flag alone cannot tell them
// apart; nlmon records no direction. Only the first answer counts: a
// get's echo is one message, and a request after it with the same sequence
// number is a new one (tc's ll_link_get opens its own socket, whose
// time-based seq can equal the main socket's).
func echoReply(full []byte, last tx) bool {
	if len(last.replies) > 0 {
		return false
	}
	req := last.req
	if len(full) < HeaderLen || len(req) < HeaderLen {
		return false
	}
	return binary.LittleEndian.Uint16(req[6:8])&unix.NLM_F_ECHO != 0 &&
		bytes.Equal(full[8:12], req[8:12])
}

type tx struct {
	req     []byte
	replies []Msg
}

// Replay answers requests from a capture's transactions, in order.
type Replay struct {
	opts Options
	txs  []tx
	next int
}

// New builds a Replay from captured netlink datagrams in capture order.
func New(datagrams [][]byte, opts Options) *Replay {
	if opts.Canon == nil {
		opts.Canon = CanonSeqPID
	}
	s := &Replay{opts: opts}
	for _, dg := range datagrams {
		for _, full := range splitFull(dg) {
			if isRequest(full) && (len(s.txs) == 0 || !echoReply(full, s.txs[len(s.txs)-1])) {
				s.txs = append(s.txs, tx{req: full})
				continue
			}
			if len(s.txs) == 0 {
				continue // a reply before any request: not ours.
			}
			last := &s.txs[len(s.txs)-1]
			last.replies = append(last.replies, SplitMsgs(full)...)
		}
	}
	return s
}

// Open reads the ws pcap at path. Every error wraps opts.Err.
func Open(path string, opts Options) (*Replay, error) {
	bs, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Join(opts.Err, err)
	}
	_, recs, err := xtcpnl.ParseNetlinkPcap(bs)
	if err != nil {
		return nil, errors.Join(opts.Err, err)
	}
	dgs := make([][]byte, 0, len(recs))
	for _, r := range recs {
		_, dg, perr := r.NetlinkPayload()
		if perr != nil {
			return nil, errors.Join(opts.Err, perr)
		}
		dgs = append(dgs, dg)
	}
	return New(dgs, opts), nil
}

// Talk answers req with the next recorded transaction, or opts.Err.
func (s *Replay) Talk(req []byte) (Reply, error) {
	if s.next >= len(s.txs) {
		return Reply{}, fmt.Errorf("%w: request %d is past the %d the capture has", s.opts.Err, s.next, len(s.txs))
	}
	t := s.txs[s.next]
	got, want := s.opts.Canon(req), s.opts.Canon(t.req)
	if !bytes.Equal(got, want) {
		w := max(len(s.opts.Clone), len(s.opts.Tool))
		return Reply{}, fmt.Errorf("%w: request %d differs from the capture's\n %-*s %x\n %-*s %x",
			s.opts.Err, s.next, w, s.opts.Clone, got, w, s.opts.Tool, want)
	}
	s.next++
	return Reply{Msgs: t.replies}, nil
}

// Pending is how many recorded requests were not asked for.
func (s *Replay) Pending() int { return len(s.txs) - s.next }
