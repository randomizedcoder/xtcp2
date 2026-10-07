package xtcpnl

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// NetlinkControl distinguishes control messages without completing a transaction.
type NetlinkControl uint8

const (
	NetlinkData NetlinkControl = iota
	NetlinkACK
	NetlinkError
	NetlinkDone
	NetlinkNoop
	NetlinkOverrun
)

// NetlinkEnvelope retains the entire header and a borrowed body. Code is the
// signed status from ERROR or DONE (zero for ACK or an empty DONE). Neither Pid
// nor Seq authenticates the sender; the transport must check recvmsg's sockaddr.
type NetlinkEnvelope struct {
	Header  NlMsgHdr
	Body    []byte
	Control NetlinkControl
	Code    int32
}

// WalkNetlinkEnvelopes walks one datagram without sequence filtering or early
// completion on control messages. Body is valid only during visit; copy anything
// retained. A callback error stops delivery. A malformed suffix may be detected
// after callbacks; callers must discard a candidate transaction on any error.
// Final message padding may be absent, but partial padding and stray tails fail.
func WalkNetlinkEnvelopes(data []byte, visit func(NetlinkEnvelope) error) error {
	if len(data) < NlMsgHdrSizeCst {
		return ErrShortRecv
	}
	for len(data) != 0 {
		var e NetlinkEnvelope
		if _, err := DeserializeNlMsgHdr(data, &e.Header); err != nil {
			return err
		}
		n := int(e.Header.Len)
		if n < NlMsgHdrSizeCst || n > len(data) {
			return ErrBadMsgLen
		}
		e.Body = data[NlMsgHdrSizeCst:n:n]
		if err := e.classify(); err != nil {
			return err
		}
		advance := n + FourByteAlignPadding(n)
		if n == len(data) {
			advance = n
		} else if advance > len(data) {
			return ErrBadMsgLen
		}
		if visit != nil {
			if err := visit(e); err != nil {
				return err
			}
		}
		data = data[advance:]
	}
	return nil
}

func (e *NetlinkEnvelope) classify() error {
	switch e.Header.Type {
	case unix.NLMSG_ERROR:
		if len(e.Body) < 4 {
			return ErrNetlinkError
		}
		e.Code = int32(binary.LittleEndian.Uint32(e.Body))
		e.Control = NetlinkError
		if e.Code == 0 {
			e.Control = NetlinkACK
		}
	case unix.NLMSG_DONE:
		e.Control = NetlinkDone
		if len(e.Body) != 0 {
			if len(e.Body) < 4 {
				return ErrNetlinkError
			}
			e.Code = int32(binary.LittleEndian.Uint32(e.Body))
		}
	case unix.NLMSG_NOOP:
		e.Control = NetlinkNoop
	case unix.NLMSG_OVERRUN:
		e.Control = NetlinkOverrun
	default:
		e.Control = NetlinkData
	}
	return nil
}
