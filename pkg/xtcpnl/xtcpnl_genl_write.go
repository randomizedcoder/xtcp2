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

// Seg6GenlVersion is SEG6_GENL_VERSION, the version ip's sr requests carry
// (SEG6_REQUEST, ip/ipseg6.c).
const Seg6GenlVersion uint8 = 1

// IOAM6GenlVersion is IOAM6_GENL_VERSION, the version ip's ioam requests
// carry (IOAM6_REQUEST, ip/ipioam6.c).
const IOAM6GenlVersion uint8 = 1

// MPTCPPMGenlVersion is MPTCP_PM_VER, the version ip's mptcp requests carry
// (MPTCP_REQUEST, ip/ipmptcp.c).
const MPTCPPMGenlVersion uint8 = 1

// FOUGenlVersion is FOU_GENL_VERSION, the version ip's fou requests carry
// (include/uapi/linux/fou.h).
const FOUGenlVersion uint8 = 1

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
// delete` and `flush` (ip/tcp_metrics.c), and SEG6's SETHMAC and
// SET_TUNSRC, `ip sr hmac set` and `ip sr tunsrc set` (ip/ipseg6.c), and
// IOAM6's namespace and schema adds, deletes and binding, `ip ioam
// namespace` and `schema` (ip/ipioam6.c), and mptcp_pm's endpoint adds,
// deletes, changes and flush and its limits set, `ip mptcp endpoint` and
// `limits set` (ip/ipmptcp.c), and fou's ADD and DEL, `ip fou add` and
// `del` (ip/ipfou.c). The family is its controller name.
func IsAdmittedGenlWrite(family string, cmd uint8) bool {
	switch family {
	case TCPMetricsGenlName:
		return cmd == TCPMetricsCmdDel
	case Seg6GenlName:
		return cmd == Seg6CmdSetHmac || cmd == Seg6CmdSetTunsrc
	case IOAM6GenlName:
		switch cmd {
		case IOAM6CmdAddNamespace, IOAM6CmdDelNamespace, IOAM6CmdAddSchema, IOAM6CmdDelSchema, IOAM6CmdNsSetSchema:
			return true
		}
	case MPTCPPMGenlName:
		switch cmd {
		case MPTCPPMCmdAddAddr, MPTCPPMCmdDelAddr, MPTCPPMCmdFlushAddrs, MPTCPPMCmdSetLimits, MPTCPPMCmdSetFlags:
			return true
		}
	case FOUGenlName:
		return cmd == FOUCmdAdd || cmd == FOUCmdDel
	}
	return false
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
