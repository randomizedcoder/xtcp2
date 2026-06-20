// Package recordfmt formats xtcp2 TCP records (xtcp_flat_record.XtcpFlatRecord
// and the Envelope batch) into the various export encodings — protobufList,
// protoJson, protoText, msgpack, jsonl, csv, tsv, and a humanized JSON. It is
// a pure library shared by the xtcp2 daemon (pkg/xtcp) and the xtcp2client
// gRPC client: functions return ([]byte, error) and never log, count metrics,
// or pool buffers — callers own those concerns.
package recordfmt

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// Address family values from the kernel inet_diag message.
const (
	afInet  = 2  // AF_INET
	afInet6 = 10 // AF_INET6
)

// IPString renders an inet_diag address (raw bytes) as a dotted-quad or
// RFC-5952 IPv6 string. The kernel returns the address in a 16-byte
// __be32[4] slot regardless of family, with only the first 4 bytes
// meaningful for IPv4 — so family is authoritative and consulted before
// length, otherwise an IPv4 address in a 16-byte buffer is misread as IPv6
// ("c0a8:7a01::"). Empty input → "".
func IPString(family uint32, b []byte) string {
	if len(b) == 0 {
		return ""
	}
	switch family {
	case afInet:
		if len(b) >= net.IPv4len {
			return net.IP(b[:net.IPv4len]).String()
		}
	case afInet6:
		if len(b) >= net.IPv6len {
			return net.IP(b[:net.IPv6len]).String()
		}
	}
	return net.IP(b).String()
}

// tcpStateNames maps kernel TCP state integers (include/net/tcp_states.h) to
// the conventional names `ss` prints. xtcp2 carries state as a bare uint8.
var tcpStateNames = map[uint32]string{
	1:  "ESTABLISHED",
	2:  "SYN_SENT",
	3:  "SYN_RECV",
	4:  "FIN_WAIT1",
	5:  "FIN_WAIT2",
	6:  "TIME_WAIT",
	7:  "CLOSE",
	8:  "CLOSE_WAIT",
	9:  "LAST_ACK",
	10: "LISTEN",
	11: "CLOSING",
	12: "NEW_SYN_RECV",
}

// TCPStateName returns the conventional name for a TCP state integer, or the
// decimal value as a string for anything outside the known range.
func TCPStateName(state uint32) string {
	if name, ok := tcpStateNames[state]; ok {
		return name
	}
	return strconv.FormatUint(uint64(state), 10)
}

// CongestionAlgorithmName returns the short congestion-control name (e.g.
// "CUBIC", "BBR3") by trimming the generated enum's CONGESTION_ALGORITHM_
// prefix. UNSPECIFIED renders as "".
func CongestionAlgorithmName(e xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm) string {
	if e == xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED {
		return ""
	}
	return strings.TrimPrefix(e.String(), "CONGESTION_ALGORITHM_")
}

// TimestampRFC3339 formats a record's timestamp_ns (Unix nanoseconds held as a
// double) as RFC3339 with nanosecond precision in UTC. Zero → "".
func TimestampRFC3339(ns float64) string {
	if ns <= 0 {
		return ""
	}
	sec := int64(ns) / 1e9
	nsec := int64(ns) % 1e9
	return time.Unix(sec, nsec).UTC().Format(time.RFC3339Nano)
}
