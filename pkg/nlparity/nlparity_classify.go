package nlparity

import (
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrBadWriteFile is a GOIP_WRITE_OUT file that is not a sequence of
// admitted write requests.
var ErrBadWriteFile = errors.New("nlparity: the write file is not a sequence of write requests")

// IsWrite reports whether m is a write the gates admit: an rtnetlink type
// xtcpnl.IsAdmittedWriteType admits, an XFRM type
// xtcpnl.IsAdmittedXfrmWrite admits, or a generic netlink family's command
// xtcpnl.IsAdmittedGenlWrite admits by the family's name. A genl message
// whose family the capture never named is not one, since only the name says
// which commands write.
func IsWrite(m Msg) bool {
	switch m.Proto {
	case unix.NETLINK_GENERIC:
		return m.IsGenl() && len(m.Body) >= genlHdrLen && xtcpnl.IsAdmittedGenlWrite(m.Genl, m.Body[0])
	case unix.NETLINK_XFRM:
		// By XFRM's own gate: NEWSA's 16 is RTM_NEWLINK's.
		return m.IsXfrm() && xtcpnl.IsAdmittedXfrmWrite(m.Hdr.Type)
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
