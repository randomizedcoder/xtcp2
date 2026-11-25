package xtcpnl

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/strpolicy"
)

// https://github.com/torvalds/linux/blob/master/include/uapi/linux/inet_diag.h#L134
// https://github.com/torvalds/linux/blob/29d9f30d4ce6c7a38745a54a8cddface10013490/include/uapi/linux/inet_diag.h#L133
// INET_DIAG_NONE 0
// INET_DIAG_MEMINFO 1
// INET_DIAG_INFO 2
// INET_DIAG_VEGASINFO 3
// INET_DIAG_CONG 4
// INET_DIAG_TOS 5
// INET_DIAG_TCLASS 6
// INET_DIAG_SKMEMINFO 7
// INET_DIAG_SHUTDOWN 8
// INET_DIAG_DCTCPINFO 9
// INET_DIAG_PROTOCOL 10
// INET_DIAG_SKV6ONLY 11
// INET_DIAG_LOCALS 12
// INET_DIAG_PEERS 13
// INET_DIAG_PAD 14
// INET_DIAG_MARK 15
// INET_DIAG_BBRINFO 16
// INET_DIAG_CLASS_ID 17
// INET_DIAG_MD5SIG 18
// INET_DIAG_ULP_INFO 19
// INET_DIAG_SK_BPF_STORAGES 20
// INET_DIAG_CGROUP_ID 21
// INET_DIAG_SOCKOPT 22
// 23
// __INET_DIAG_MAX 24

// INET_DIAG_CONG 4

type CongInfo struct {
	Cong []byte // null terminated
}

const (
	// CongInfoSizeCst is a one-letter name plus its NUL. The kernel sends
	// ca_ops->name with nla_put_string, unpadded, and the shortest in-tree
	// names are two letters ("nv", "lp": 3 bytes).
	CongInfoSizeCst = 2
	CongInfoReadCst = CongInfoSizeCst

	CongInfoEmumValueCst = 4
)

var (
	ErrCongInfoSmall = errors.New("data too small for CongInfo")
)

// DeserializeCongInfo does a binary read of a CongInfo
// It does a basic length check
func DeserializeCongInfo(data []byte, ci *CongInfo) (n int, err error) {

	if len(data) < CongInfoSizeCst {
		return 0, ErrCongInfoSmall
	}

	ci.Cong = data
	// n = copy(ci.Cong[:], data)
	// for i := 0; i < len(data); i++ {
	// 	ci.Cong[i] = data[i]
	// }
	// ci.Cong = *((*[6]byte)(data[0:6]))
	n = len(data)

	return n, nil
}

// CongInfo has no reflection twin: Cong is a []byte, and binary.Read cannot
// decode into a slice of unknown length. DeserializeCongInfo is the only
// implementation, so its layout is pinned by the captured fixtures alone.

func DeserializeCongInfoXTCP(data []byte, x *xtcp_flat_record.XtcpFlatRecord) (err error) {
	// func DeserializeCongInfoXTCP(data []byte, x *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {

	if len(data) < CongInfoSizeCst {
		return ErrCongInfoSmall
	}

	// Find the NUL and check the name is printable ASCII in one pass. The
	// payload may carry NLA padding after the NUL, or no NUL at all.
	name, printable := data, true
	for i, c := range data {
		if c == 0 {
			name = data[:i]
			break
		}
		if c < 0x20 || c > 0x7e {
			printable = false
		}
	}
	if known, algorithm, ok := congKnown(name); ok {
		// A kernel name: a constant, so no copy out of the reused buffer.
		x.InetDiagCong, x.InetDiagCongEnum = known, algorithm
		return nil
	}
	// Preserve any other name: controller exposure is based on this field.
	// Copy out of the reused netlink buffer; anything outside printable
	// ASCII or longer than TCP_CA_NAME_MAX goes through the string policy.
	x.InetDiagCongEnum = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED
	if printable && len(name) <= strpolicy.MaxCong {
		x.InetDiagCong = string(name)
	} else {
		x.InetDiagCong = strpolicy.Clean(string(name), strpolicy.MaxCong)
	}
	return nil
}

// congKnown maps a congestion-control name to a constant string and its
// enum, matching the whole name: "dctcp-reno" (dctcp's fallback when ECN
// isn't negotiated, tcp_dctcp.c) is not DCTCP, and "bbr2"/"bbr3" are told
// apart from "bbr". The list is the in-tree modules (net/ipv4/tcp_*.c, Linux
// 7.3) plus the out-of-tree BBRv2/v3 and Prague. switch on string(name)
// doesn't allocate.
func congKnown(name []byte) (string, xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm, bool) {
	const unspecified = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED
	switch string(name) {
	case "cubic":
		return "cubic", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC, true
	case "bbr", "bbr1":
		return string(name), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1, true
	case "bbr2":
		return "bbr2", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR2, true
	case "bbr3":
		return "bbr3", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3, true
	case "dctcp":
		return "dctcp", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP, true
	case "vegas":
		return "vegas", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS, true
	case "prague":
		return "prague", xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_PRAGUE, true
	case "reno":
		return "reno", unspecified, true
	case "dctcp-reno":
		return "dctcp-reno", unspecified, true
	case "westwood":
		return "westwood", unspecified, true
	case "htcp":
		return "htcp", unspecified, true
	case "illinois":
		return "illinois", unspecified, true
	case "nv":
		return "nv", unspecified, true
	case "cdg":
		return "cdg", unspecified, true
	case "hybla":
		return "hybla", unspecified, true
	case "lp":
		return "lp", unspecified, true
	case "scalable":
		return "scalable", unspecified, true
	case "veno":
		return "veno", unspecified, true
	case "yeah":
		return "yeah", unspecified, true
	case "bic":
		return "bic", unspecified, true
	case "highspeed":
		return "highspeed", unspecified, true
	case "roccet":
		return "roccet", unspecified, true
	}
	return "", unspecified, false
}
