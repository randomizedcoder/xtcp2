package xtcpnl

import (
	"errors"

	"golang.org/x/sys/unix"
)

// A generic netlink request is an nlmsghdr whose type is the family's id,
// assigned at boot, then a genlmsghdr (command, version, two reserved bytes)
// and the family's attributes: GENL_REQUEST's layout (lib/libgenl.h). Which
// commands write is the family's to say, so the write gate names a family by
// its controller name and a command, not by a type.

// TCPMetricsGenlVersion is TCP_METRICS_GENL_VERSION, the version ip's
// tcp_metrics requests carry (TCPM_REQUEST, ip/tcp_metrics.c).
const TCPMetricsGenlVersion uint8 = 1

// genlMsgHdrLenCst is sizeof(struct genlmsghdr).
const genlMsgHdrLenCst = 4

var (
	// ErrGenlFamilyID is a family id below GENL_ID_CTRL, which is an
	// nlmsghdr control type, not a family.
	ErrGenlFamilyID = errors.New("xtcpnl: a generic netlink family id is at least GENL_ID_CTRL")
	// ErrGenlWrite is a generic netlink command IsAdmittedGenlWrite admits,
	// asked of BuildGenlRequest, or one it does not, asked of
	// BuildGenlWriteRequest.
	ErrGenlWrite = errors.New("xtcpnl: generic netlink writes are built by BuildGenlWriteRequest, and only those IsAdmittedGenlWrite admits")
)

// IsAdmittedGenlWrite reports whether a generic netlink family's command is
// a write goip may build: tcp_metrics' TCP_METRICS_CMD_DEL, `ip tcp_metrics
// delete` and `flush` (ip/tcp_metrics.c). The family is its controller name.
func IsAdmittedGenlWrite(family string, cmd uint8) bool {
	return family == TCPMetricsGenlName && cmd == TCPMetricsCmdDel
}

// BuildGenlRequest lays out a generic netlink read request: the family id as
// the type, flags plus NLM_F_REQUEST, then the genlmsghdr and attrs. A
// command IsAdmittedGenlWrite admits is refused with ErrGenlWrite; it
// belongs on BuildGenlWriteRequest.
func BuildGenlRequest(family string, id uint16, cmd, version uint8, flags uint16, seq uint32, attrs []byte) ([]byte, error) {
	if IsAdmittedGenlWrite(family, cmd) {
		return nil, ErrGenlWrite
	}
	return genlRequest(id, cmd, version, flags, seq, attrs)
}

// BuildGenlWriteRequest is BuildGenlRequest for the write path: only a
// command IsAdmittedGenlWrite admits, anything else ErrGenlWrite.
func BuildGenlWriteRequest(family string, id uint16, cmd, version uint8, flags uint16, seq uint32, attrs []byte) ([]byte, error) {
	if !IsAdmittedGenlWrite(family, cmd) {
		return nil, ErrGenlWrite
	}
	return genlRequest(id, cmd, version, flags, seq, attrs)
}

// genlRequest is the two builders' layout, after their gates.
func genlRequest(id uint16, cmd, version uint8, flags uint16, seq uint32, attrs []byte) ([]byte, error) {
	if id < unix.GENL_ID_CTRL {
		return nil, ErrGenlFamilyID
	}
	hdr := [genlMsgHdrLenCst]byte{cmd, version}
	return layoutRequest(id, flags|uint16(unix.NLM_F_REQUEST), seq, hdr[:], attrs), nil
}
