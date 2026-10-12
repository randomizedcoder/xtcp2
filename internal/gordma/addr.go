package gordma

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ntop is ss_ntop (rdma/res-cmid.c): the address of an AF_INET or AF_INET6
// sockaddr as inet_ntop prints it. Any other family is not printable.
func ntop(sa xtcpnl.RdmaSockaddr) (string, bool) {
	switch {
	case sa.Family == unix.AF_INET && sa.Addr.Is4():
		return sa.Addr.String(), true
	case sa.Family == unix.AF_INET6 && sa.Addr.Is6():
		return ntop6(sa.Addr.As16()), true
	}
	return "", false
}

// ntop6 is glibc's inet_ntop6, which differs from netip's String in one
// place: an address of the first 96 bits zero, or ::ffff:0:0/96, ends in
// dotted IPv4. The longest run of two or more zero words, the first if
// tied, is "::".
func ntop6(a [16]byte) string {
	var w [8]uint16
	for i := range w {
		w[i] = uint16(a[2*i])<<8 | uint16(a[2*i+1])
	}
	bestBase, bestLen, curBase, curLen := -1, 0, -1, 0
	for i, v := range w {
		if v == 0 {
			if curBase == -1 {
				curBase, curLen = i, 1
			} else {
				curLen++
			}
			continue
		}
		if curBase != -1 {
			if bestBase == -1 || curLen > bestLen {
				bestBase, bestLen = curBase, curLen
			}
			curBase = -1
		}
	}
	if curBase != -1 && (bestBase == -1 || curLen > bestLen) {
		bestBase, bestLen = curBase, curLen
	}
	if bestBase != -1 && bestLen < 2 {
		bestBase = -1
	}
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if bestBase != -1 && i >= bestBase && i < bestBase+bestLen {
			if i == bestBase {
				b.WriteByte(':')
			}
			continue
		}
		if i != 0 {
			b.WriteByte(':')
		}
		if i == 6 && bestBase == 0 && (bestLen == 6 || (bestLen == 5 && w[5] == 0xffff)) {
			fmt.Fprintf(&b, "%d.%d.%d.%d", a[12], a[13], a[14], a[15])
			return b.String()
		}
		fmt.Fprintf(&b, "%x", w[i])
	}
	if bestBase != -1 && bestBase+bestLen == 8 {
		b.WriteByte(':')
	}
	return b.String()
}
