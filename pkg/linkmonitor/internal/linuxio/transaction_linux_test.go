package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func frame(typ, flags uint16, seq uint32, body []byte) []byte {
	n := 16 + len(body)
	b := make([]byte, (n+3)&^3)
	binary.LittleEndian.PutUint32(b, uint32(n))
	binary.LittleEndian.PutUint16(b[4:], typ)
	binary.LittleEndian.PutUint16(b[6:], flags)
	binary.LittleEndian.PutUint32(b[8:], seq)
	binary.LittleEndian.PutUint32(b[12:], 321) // Header PID is not sender authentication.
	copy(b[16:], body)
	return b
}

func word(n int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(n))
	return b
}

func control(code int32, seq uint32) []byte {
	body := append(word(code), frame(unix.RTM_GETLINK, unix.NLM_F_REQUEST, seq, nil)...)
	return frame(unix.NLMSG_ERROR, 0, seq, body)
}

func numberRequest(dump, ack bool) request[uint32] {
	return request[uint32]{family: unix.RTM_NEWLINK, dump: dump, ack: ack,
		build: func(seq uint32, _ uint64) ([]byte, error) {
			flags := uint16(unix.NLM_F_REQUEST)
			if dump {
				flags |= unix.NLM_F_DUMP
			}
			if ack {
				flags |= unix.NLM_F_ACK
			}
			return frame(unix.RTM_GETLINK, flags, seq, nil), nil
		},
		match: func(e xtcpnl.NetlinkEnvelope) (uint32, bool, error) {
			if len(e.Body) != 4 {
				return 0, false, ErrReply
			}
			return binary.LittleEndian.Uint32(e.Body), true, nil
		},
	}
}

func TestTransactionCompletion(t *testing.T) {
	data := frame(unix.RTM_NEWLINK, 0, 1, word(7))
	multi := frame(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 1, word(7))
	done := frame(unix.NLMSG_DONE, 0, 1, word(0))
	ack := control(0, 1)
	rows := []struct {
		name, description string
		dump, requireACK  bool
		frames            [][]byte
		wantComplete      bool
		wantCount         int
		wantErr           error
	}{
		{"get", "single data completes without ACK", false, false, [][]byte{data}, true, 1, nil},
		{"ack_only", "ACK cannot replace GET data", false, false, [][]byte{ack}, false, 0, nil},
		{"ack_before", "ACK before data does not end the read", false, true, [][]byte{ack, data}, true, 1, nil},
		{"ack_after", "requested ACK is awaited after data", false, true, [][]byte{data, ack}, true, 1, nil},
		{"missing_ack", "data without requested ACK stays incomplete", false, true, [][]byte{data}, false, 1, nil},
		{"multipart", "GET with MULTI requires DONE", false, false, [][]byte{multi}, false, 1, nil},
		{"multipart_done", "GET with MULTI and DONE succeeds", false, false, [][]byte{multi, done}, true, 1, nil},
		{"dump", "multiple records ACK and DONE complete dump", true, true, [][]byte{multi, ack, multi, done}, true, 2, nil},
		{"empty_dump", "empty successful dump is valid", true, false, [][]byte{done}, true, 0, nil},
		{"empty_get", "DONE alone cannot complete a GET", false, false, [][]byte{done}, false, 0, ErrReply},
		{"partial_dump", "dump missing DONE stays incomplete", true, false, [][]byte{multi, ack}, false, 1, nil},
		{"done_error", "DONE errno invalidates data", true, false, [][]byte{multi, frame(unix.NLMSG_DONE, 0, 1, word(-int32(unix.EIO)))}, false, 1, unix.EIO},
		{"data_error", "kernel error after data invalidates candidate", false, false, [][]byte{data, control(-int32(unix.ENODEV), 1)}, false, 1, unix.ENODEV},
		{"done_then_error", "DONE must not hide a later error", true, false, [][]byte{multi, done, control(-int32(unix.EINVAL), 1)}, false, 1, unix.EINVAL},
		{"interrupted_data", "DUMP_INTR on data fails", true, false, [][]byte{frame(unix.RTM_NEWLINK, unix.NLM_F_DUMP_INTR, 1, word(7))}, false, 0, ErrInterrupted},
		{"interrupted_done", "DUMP_INTR on DONE fails", true, false, [][]byte{frame(unix.NLMSG_DONE, unix.NLM_F_DUMP_INTR, 1, nil)}, false, 0, ErrInterrupted},
		{"overrun", "socket loss cannot be sequence-filtered away", true, false, [][]byte{frame(unix.NLMSG_OVERRUN, 0, 999, nil)}, false, 0, unix.ENOBUFS},
		{"wrong_sequence", "late reply cannot establish current state", false, false, [][]byte{frame(unix.RTM_NEWLINK, 0, 99, word(7))}, false, 0, nil},
		{"wrong_family", "unrelated family cannot satisfy request", false, false, [][]byte{frame(55, 0, 1, word(7))}, false, 0, nil},
		{"unrelated_error", "old error cannot cancel current sequence", false, false, [][]byte{control(-int32(unix.EIO), 99), data}, true, 1, nil},
		{"noop", "NOOP supplies no data", false, false, [][]byte{frame(unix.NLMSG_NOOP, 0, 1, nil), data}, true, 1, nil},
		{"duplicate_get", "multiple single-GET records are ambiguous", false, false, [][]byte{data, data}, false, 1, ErrReply},
		{"data_after_done", "data after DONE is rejected", true, false, [][]byte{done, multi}, false, 0, ErrReply},
		{"duplicate_done", "duplicate DONE is rejected", true, false, [][]byte{done, done}, false, 0, ErrReply},
		{"duplicate_ack", "duplicate ACK is rejected", true, true, [][]byte{ack, ack}, false, 0, ErrReply},
		{"request_flag", "kernel data cannot be a user request", false, false, [][]byte{frame(unix.RTM_NEWLINK, unix.NLM_F_REQUEST, 1, word(7))}, false, 0, ErrReply},
		{"short_ack", "ACK must include original request header", false, false, [][]byte{frame(unix.NLMSG_ERROR, 0, 1, word(0))}, false, 0, ErrReply},
		{"positive_status", "positive DONE status is malformed", true, false, [][]byte{frame(unix.NLMSG_DONE, 0, 1, word(1))}, false, 0, ErrReply},
		{"short_payload", "malformed required data rejects candidate", false, false, [][]byte{frame(unix.RTM_NEWLINK, 0, 1, nil)}, false, 0, ErrReply},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			r := numberRequest(row.dump, row.requireACK)
			b, _ := r.build(1, 1)
			state, err := newTransaction(r, b, 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range row.frames {
				if err = state.consume(1, f); err != nil {
					break
				}
			}
			if !errors.Is(err, row.wantErr) || len(state.values) != row.wantCount {
				t.Fatalf("error=%v records=%d; want error=%v records=%d", err, len(state.values), row.wantErr, row.wantCount)
			}
			if err == nil && state.complete() != row.wantComplete {
				t.Fatalf("complete=%v want=%v", state.complete(), row.wantComplete)
			}
		})
	}
}

func TestTransactionBounds(t *testing.T) {
	data := frame(unix.RTM_NEWLINK, 0, 1, word(7))
	rows := []struct {
		name, description string
		epoch             uint64
		bytes, records    int
		frames            [][]byte
		wantErr           error
	}{
		{"byte_equal", "exact byte limit allowed", 1, len(data), 1, [][]byte{data}, nil},
		{"byte_over", "one byte beyond limit fails before parsing", 1, len(data) - 1, 1, [][]byte{data}, ErrLimit},
		{"cumulative", "limits include unrelated sequence traffic", 1, len(data), 1, [][]byte{frame(unix.NLMSG_NOOP, 0, 99, nil), data}, ErrLimit},
		{"records_zero", "record budget is checked before append", 1, 100, 0, [][]byte{data}, ErrLimit},
		{"records_over", "dump exceeding record budget fails", 1, 100, 1, [][]byte{data, data}, ErrLimit},
		{"stale_epoch", "old socket cannot deliver into new transaction", 2, 100, 1, [][]byte{data}, ErrEpoch},
		{"malformed_tail", "successful prefix cannot hide a malformed suffix", 1, 100, 1, [][]byte{append(append([]byte{}, data...), 1)}, xtcpnl.ErrNlMsgHdrSmall},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			r := numberRequest(true, false)
			b, _ := r.build(1, 1)
			state, err := newTransaction(r, b, 1, 1)
			if err != nil {
				t.Fatal(err)
			}
			state.byteLimit, state.recordLimit = row.bytes, row.records
			for _, f := range row.frames {
				if err = state.consume(row.epoch, f); err != nil {
					break
				}
			}
			if !errors.Is(err, row.wantErr) {
				t.Fatalf("error=%v want=%v", err, row.wantErr)
			}
		})
	}
}

type batch struct {
	data [][]byte
	err  error
}
type scriptSocket struct {
	batches           []batch
	sent              [][]byte
	sendErr, closeErr error
	closes            int
	onSend            func([]byte)
}

func (s *scriptSocket) Send(_ context.Context, b []byte) error {
	s.sent = append(s.sent, append([]byte(nil), b...))
	if s.onSend != nil {
		s.onSend(b)
	}
	return s.sendErr
}
func (s *scriptSocket) Receive(_ context.Context, visit func([]byte) error) (int, error) {
	if len(s.batches) == 0 {
		return 0, context.DeadlineExceeded
	}
	b := s.batches[0]
	s.batches = s.batches[1:]
	for i, data := range b.data {
		if err := visit(data); err != nil {
			return i + 1, err
		}
	}
	return len(b.data), b.err
}
func (s *scriptSocket) Close() error { s.closes++; return s.closeErr }

func scriptedClient(protocol int, sockets ...*scriptSocket) *Client {
	c, _ := NewClient(protocol)
	c.open = func(context.Context, int) (transport, error) {
		if len(sockets) == 0 {
			return nil, unix.EMFILE
		}
		s := sockets[0]
		sockets = sockets[1:]
		return s, nil
	}
	return c
}

func TestCandidateFailureIsAtomic(t *testing.T) {
	data := frame(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 1, word(7))
	done := frame(unix.NLMSG_DONE, 0, 1, nil)
	rows := []struct {
		name, description string
		batches           []batch
		wantErr           error
	}{
		{"partial", "timeout discards every partial record", []batch{{data: [][]byte{data}}}, context.DeadlineExceeded},
		{"error_after_done", "same-batch error after DONE rejects candidate", []batch{{data: [][]byte{data, done, control(-int32(unix.EIO), 1)}}}, unix.EIO},
		{"transport_loss", "receive loss after completed prefix rejects candidate", []batch{{data: [][]byte{data, done}, err: unix.ENOBUFS}}, unix.ENOBUFS},
		{"malformed_after_done", "whole datagram is validated past DONE", []batch{{data: [][]byte{append(done, 1)}}}, xtcpnl.ErrNlMsgHdrSmall},
		{"no_progress", "broken adapter cannot spin with zero datagrams", []batch{{}}, ErrReply},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			cleanup := errors.New("close failure")
			s := &scriptSocket{batches: row.batches, closeErr: cleanup}
			c := scriptedClient(unix.NETLINK_ROUTE, s)
			got, err := execute(context.Background(), c, numberRequest(true, false))
			if !errors.Is(err, row.wantErr) || !errors.Is(err, cleanup) || got.Values != nil || got.Epoch != 0 || s.closes != 1 {
				t.Fatalf("result=%+v error=%v closes=%d", got, err, s.closes)
			}
		})
	}
}
