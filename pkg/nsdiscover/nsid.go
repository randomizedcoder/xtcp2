package nsdiscover

import (
	"encoding/binary"
	"unsafe"

	"golang.org/x/sys/unix"
)

// rtnetlink constants for the RTM_GETNSID request/response. These are stable
// kernel UAPI values (uapi/linux/rtnetlink.h, uapi/linux/net_namespace.h) that
// golang.org/x/sys/unix does not export, so we define the few we need.
const (
	rtmNewNsid = 88 // RTM_NEWNSID (reply message type)
	rtmGetNsid = 90 // RTM_GETNSID (request message type)

	netnsaNsid = 1 // NETNSA_NSID attribute (the id, int32; -1 = not assigned)
	netnsaFd   = 3 // NETNSA_FD attribute (fd referencing the target netns)

	nlmsgHdrLen = 16 // sizeof(struct nlmsghdr)
	rtgenLen    = 4  // NLMSG_ALIGN(sizeof(struct rtgenmsg)); rtgen_family is 1 byte
	fdAttrLen   = 8  // nlattr header (4) + int32 fd payload (4)
)

// nativeEndian is the byte order netlink expects (native/host order), resolved
// once. netlink message and attribute headers are host-endian, unlike the
// network-order fields inside some payloads.
var nativeEndian = func() binary.ByteOrder {
	var x uint16 = 1
	if *(*byte)(unsafe.Pointer(&x)) == 1 {
		return binary.LittleEndian
	}
	return binary.BigEndian
}()

// Nsid queries the kernel for the NETNSA_NSID of the network namespace referenced
// by nsFD — an open fd to a netns handle such as /run/netns/<name> or
// /proc/<pid>/ns/net — as seen from the caller's current network namespace, via
// an RTM_GETNSID rtnetlink request.
//
// It is strictly best-effort: it returns (0, false) on any error and on
// NETNSA_NSID_NOT_ASSIGNED (-1), which is the common case for Docker/containerd
// namespaces the host has never assigned an id to. Only a successfully-returned,
// non-negative id yields (id, true). nsid is relative to the caller's netns and
// is NOT a stable global identity — netns_inode is. Callers gate this behind an
// opt-in flag because it is usually unset.
func Nsid(nsFD int) (int32, bool) {
	if nsFD < 0 {
		return 0, false
	}

	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return 0, false
	}
	defer unix.Close(fd) //nolint:errcheck // best-effort netlink socket; nothing to recover on close

	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, false
	}

	// Bound the receive so a missing/odd reply degrades to (0,false) instead of
	// blocking the caller (this runs per-namespace on the reconcile path). If the
	// setsockopt is unsupported we still rely on the kernel's guaranteed reply to
	// RTM_GETNSID, so the failure is non-fatal.
	tv := unix.Timeval{Sec: 1}
	unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv) //nolint:errcheck,gosec // best-effort recv timeout

	req := buildGetNsidRequest(nsFD)
	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, false
	}

	buf := make([]byte, 4096)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil || n < nlmsgHdrLen {
		return 0, false
	}
	return parseNsidResponse(buf[:n])
}

// buildGetNsidRequest lays out an RTM_GETNSID request: nlmsghdr + rtgenmsg
// (family AF_UNSPEC) + a single NETNSA_FD attribute carrying nsFD.
func buildGetNsidRequest(nsFD int) []byte {
	total := nlmsgHdrLen + rtgenLen + fdAttrLen
	b := make([]byte, total)

	// struct nlmsghdr
	nativeEndian.PutUint32(b[0:4], uint32(total))              // nlmsg_len
	nativeEndian.PutUint16(b[4:6], rtmGetNsid)                 // nlmsg_type
	nativeEndian.PutUint16(b[6:8], uint16(unix.NLM_F_REQUEST)) // nlmsg_flags
	nativeEndian.PutUint32(b[8:12], 1)                         // nlmsg_seq
	// b[12:16] nlmsg_pid = 0 (kernel fills the peer pid)

	// struct rtgenmsg: rtgen_family = AF_UNSPEC (0); b[16:20] left zero (padded).

	// struct nlattr { __u16 nla_len; __u16 nla_type; } + int32 payload, at off 20.
	nativeEndian.PutUint16(b[20:22], fdAttrLen)
	nativeEndian.PutUint16(b[22:24], netnsaFd)
	nativeEndian.PutUint32(b[24:28], uint32(int32(nsFD)))
	return b
}

// parseNsidResponse walks the netlink reply buffer for an RTM_NEWNSID message and
// returns its NETNSA_NSID attribute value. Any error message, malformed length,
// missing attribute, or not-assigned (-1) id yields (0, false).
func parseNsidResponse(b []byte) (int32, bool) {
	for len(b) >= nlmsgHdrLen {
		msgLen := nativeEndian.Uint32(b[0:4])
		msgType := nativeEndian.Uint16(b[4:6])
		if msgLen < nlmsgHdrLen || int(msgLen) > len(b) {
			return 0, false
		}
		switch msgType {
		case uint16(unix.NLMSG_ERROR), uint16(unix.NLMSG_DONE):
			return 0, false
		case rtmNewNsid:
			return parseNsidAttrs(b[nlmsgHdrLen:msgLen])
		}
		adv := nlmsgAlign(int(msgLen))
		if adv <= 0 || adv > len(b) {
			return 0, false
		}
		b = b[adv:]
	}
	return 0, false
}

// parseNsidAttrs scans the attribute area of an RTM_NEWNSID payload (which starts
// with the aligned rtgenmsg header) for NETNSA_NSID.
func parseNsidAttrs(payload []byte) (int32, bool) {
	if len(payload) < rtgenLen {
		return 0, false
	}
	attrs := payload[rtgenLen:]
	for len(attrs) >= 4 {
		alen := int(nativeEndian.Uint16(attrs[0:2]))
		atype := nativeEndian.Uint16(attrs[2:4])
		if alen < 4 || alen > len(attrs) {
			return 0, false
		}
		if atype == netnsaNsid && alen >= 8 {
			id := int32(nativeEndian.Uint32(attrs[4:8]))
			if id < 0 { // NETNSA_NSID_NOT_ASSIGNED
				return 0, false
			}
			return id, true
		}
		adv := nlmsgAlign(alen)
		if adv <= 0 || adv > len(attrs) {
			return 0, false
		}
		attrs = attrs[adv:]
	}
	return 0, false
}

// nlmsgAlign rounds up to NLMSG_ALIGNTO (4), matching NLMSG_ALIGN / NLA_ALIGN.
func nlmsgAlign(n int) int {
	return (n + 3) &^ 3
}
