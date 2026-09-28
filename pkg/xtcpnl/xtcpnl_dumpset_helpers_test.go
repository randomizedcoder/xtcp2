package xtcpnl

// Byte builders and small assertions shared by the dump-set tables.
//
// These exist so the boundary/corner/negative rows can state a shape in one
// line instead of a hex blob, while the POSITIVE rows keep coming from real
// captures - the standing rule that constructed bytes are only for truncation,
// malformed and boundary cases.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"testing"

	"golang.org/x/sys/unix"
)

// nextHopSpec describes one `struct rtnexthop` to synthesize.
type nextHopSpec struct {
	flags   uint8
	hops    uint8
	ifindex int32
	gateway []byte // emitted as a nested RTA_GATEWAY when non-empty
	via     []byte // emitted as a nested RTA_VIA when non-empty
}

// buildNextHops lays out an RTA_MULTIPATH payload: one rtnexthop header per
// spec, each followed by its nested attributes, each advanced by
// RTNH_ALIGN(rtnh_len). The layout is the kernel's, so a row built here fails
// for the same reason a real reply would.
func buildNextHops(t *testing.T, specs ...nextHopSpec) []byte {
	t.Helper()
	// One rtnexthop header per spec is the part whose size is known up front;
	// the nested attributes grow it from there.
	out := make([]byte, 0, len(specs)*RtNextHopSizeCst)
	for _, s := range specs {
		ab := NewAttrBuilder(make([]byte, 256))
		if len(s.gateway) > 0 {
			if err := ab.PutBytes(uint16(unix.RTA_GATEWAY), s.gateway); err != nil {
				t.Fatalf("buildNextHops: PutBytes(RTA_GATEWAY): %v", err)
			}
		}
		if len(s.via) > 0 {
			if err := ab.PutBytes(uint16(unix.RTA_VIA), s.via); err != nil {
				t.Fatalf("buildNextHops: PutBytes(RTA_VIA): %v", err)
			}
		}
		attrs := ab.Bytes()

		nhLen := RtNextHopSizeCst + len(attrs)
		hdr := make([]byte, RtNextHopSizeCst)
		binary.LittleEndian.PutUint16(hdr[0:2], uint16(nhLen))
		hdr[2] = s.flags
		hdr[3] = s.hops
		binary.LittleEndian.PutUint32(hdr[4:8], uint32(s.ifindex))

		out = append(out, hdr...)
		out = append(out, attrs...)
		// RTNH_ALIGN(rtnh_len): pad to the next 4-byte boundary so the next
		// entry starts where the kernel would put it.
		out = append(out, make([]byte, FourByteAlignPadding(nhLen))...)
	}
	return out
}

// metricSpec describes one RTAX_* attribute to synthesize inside RTA_METRICS.
// Exactly one of u32 or str is meaningful, chosen by rtax.
type metricSpec struct {
	rtax uint16
	u32  uint32
	str  string
}

// buildMetrics lays out an RTA_METRICS payload.
func buildMetrics(t *testing.T, specs ...metricSpec) []byte {
	t.Helper()
	ab := NewAttrBuilder(make([]byte, 512))
	for _, s := range specs {
		if s.str != "" {
			if err := ab.PutString(s.rtax, s.str); err != nil {
				t.Fatalf("buildMetrics: PutString(%d): %v", s.rtax, err)
			}
			continue
		}
		if err := ab.PutU32(s.rtax, s.u32); err != nil {
			t.Fatalf("buildMetrics: PutU32(%d): %v", s.rtax, err)
		}
	}
	return append([]byte(nil), ab.Bytes()...)
}

// truncateLast chops n bytes off the end, which is how a capture that stopped
// mid-datagram presents. The declared lengths inside are left alone on
// purpose: that disagreement between declared and available is the bug the
// corner rows are checking for.
func truncateLast(b []byte, n int) []byte {
	if n >= len(b) {
		return nil
	}
	return b[:len(b)-n]
}

// ipText renders raw network-order address bytes the way an expectation reads.
// A length net.IP cannot represent comes back as "?<len>" rather than as a
// mangled address, so a row asserting that bad bytes SURVIVED can say so
// without the helper pretending they were valid.
func ipText(b []byte) string {
	switch len(b) {
	case 0:
		return ""
	case net.IPv4len, net.IPv6len:
		return net.IP(b).String()
	default:
		return fmt.Sprintf("?%d", len(b))
	}
}

// errorIsWant compares with errors.Is, so a wrapped sentinel still matches.
func errorIsWant(got, want error) bool {
	return errors.Is(got, want)
}
