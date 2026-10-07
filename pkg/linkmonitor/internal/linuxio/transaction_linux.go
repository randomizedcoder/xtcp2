package linuxio

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const (
	maxTransactionBytes   = 64 * 1024 * 1024
	maxTransactionRecords = 65536
)

type request[T any] struct {
	build     func(sequence uint32, epoch uint64) ([]byte, error)
	match     func(xtcpnl.NetlinkEnvelope) (T, bool, error)
	family    uint16
	dump, ack bool
}

type transaction[T any] struct {
	request                       request[T]
	epoch                         uint64
	sequence                      uint32
	requestType                   uint16
	multipart, done, ack          bool
	bytes, byteLimit, recordLimit int
	values                        []T
}

func newTransaction[T any](r request[T], data []byte, epoch uint64, seq uint32) (*transaction[T], error) {
	var h xtcpnl.NlMsgHdr
	if _, err := xtcpnl.DeserializeNlMsgHdr(data, &h); err != nil {
		return nil, errors.Join(ErrRequest, err)
	}
	if int(h.Len) != len(data) || h.Seq != seq || seq == 0 || epoch == 0 || r.match == nil ||
		h.Flags&unix.NLM_F_REQUEST == 0 || (h.Flags&unix.NLM_F_ACK != 0) != r.ack ||
		(h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP) != r.dump {
		return nil, ErrRequest
	}
	return &transaction[T]{request: r, epoch: epoch, sequence: seq, requestType: h.Type,
		multipart: r.dump, byteLimit: maxTransactionBytes, recordLimit: maxTransactionRecords}, nil
}

func (t *transaction[T]) receive(ctx context.Context, conn transport) error {
	for !t.complete() {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Consume the entire available batch before accepting success: DONE or
		// GET data must not hide a malformed suffix, subsequent error or loss.
		n, err := conn.Receive(ctx, func(data []byte) error { return t.consume(t.epoch, data) })
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrReply
		}
	}
	return nil
}

func (t *transaction[T]) complete() bool {
	if t.request.ack && !t.ack {
		return false
	}
	if t.multipart {
		return t.done && (t.request.dump || len(t.values) != 0)
	}
	return len(t.values) == 1
}

func (t *transaction[T]) consume(epoch uint64, data []byte) error {
	if epoch != t.epoch {
		return ErrEpoch
	}
	if len(data) > t.byteLimit-t.bytes {
		return ErrLimit
	}
	t.bytes += len(data)
	return xtcpnl.WalkNetlinkEnvelopes(data, t.envelope)
}

func (t *transaction[T]) envelope(e xtcpnl.NetlinkEnvelope) error {
	// An overrun reports socket-wide loss even if its sequence is unrelated.
	if e.Control == xtcpnl.NetlinkOverrun {
		return unix.ENOBUFS
	}
	if e.Header.Seq != t.sequence {
		return nil
	}
	if e.Header.Flags&unix.NLM_F_DUMP_INTR != 0 {
		return ErrInterrupted
	}
	switch e.Control {
	case xtcpnl.NetlinkNoop:
		return nil
	case xtcpnl.NetlinkACK, xtcpnl.NetlinkError:
		return t.controlError(e)
	case xtcpnl.NetlinkDone:
		if err := statusError(e.Code); err != nil {
			return err
		}
		if !t.multipart || t.done {
			return ErrReply
		}
		t.done = true
		return nil
	case xtcpnl.NetlinkData:
		return t.data(e)
	default:
		return ErrReply
	}
}

func (t *transaction[T]) controlError(e xtcpnl.NetlinkEnvelope) error {
	// nlmsgerr includes the original request header, even with capped ACKs.
	if len(e.Body) < 4+xtcpnl.NlMsgHdrSizeCst {
		return ErrReply
	}
	var original xtcpnl.NlMsgHdr
	if _, err := xtcpnl.DeserializeNlMsgHdr(e.Body[4:], &original); err != nil {
		return errors.Join(ErrReply, err)
	}
	if original.Type != t.requestType || original.Seq != t.sequence || original.Len < xtcpnl.NlMsgHdrSizeCst {
		return ErrReply
	}
	if err := statusError(e.Code); err != nil {
		return err
	}
	if t.ack {
		return ErrReply
	}
	t.ack = true
	return nil
}

func statusError(code int32) error {
	if code > 0 {
		return ErrReply
	}
	if code < 0 {
		return unix.Errno(-int64(code))
	}
	return nil
}

func (t *transaction[T]) data(e xtcpnl.NetlinkEnvelope) error {
	if e.Header.Type != t.request.family {
		return nil
	}
	if e.Header.Flags&unix.NLM_F_REQUEST != 0 {
		return ErrReply
	}
	value, matches, err := t.request.match(e)
	if err != nil || !matches {
		return err
	}
	if t.done || (!t.request.dump && len(t.values) != 0) {
		return ErrReply
	}
	if len(t.values) >= t.recordLimit {
		return ErrLimit
	}
	t.multipart = t.multipart || e.Header.Flags&unix.NLM_F_MULTI != 0
	t.values = append(t.values, value)
	return nil
}

func genericCommand(body []byte, command uint8) (bool, error) {
	if len(body) < 4 {
		return false, ErrReply
	}
	return body[0] == command, nil
}

func replyIndex(body []byte) (uint32, error) {
	if len(body) < xtcpnl.IfInfomsgSizeCst {
		return 0, ErrReply
	}
	return binary.LittleEndian.Uint32(body[4:8]), nil
}
