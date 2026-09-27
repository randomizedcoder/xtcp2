package nsdiscover

import (
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// rtnetlink constants for the RTM_GETNSID request/response. These are stable
// kernel UAPI values (uapi/linux/rtnetlink.h, uapi/linux/net_namespace.h) that
// golang.org/x/sys/unix does not export, so we define the few we need.
const (
	rtmNewNsid = 88 // RTM_NEWNSID (reply message type)
	rtmGetNsid = 90 // RTM_GETNSID (request message type)

	netnsaNsid = 1 // NETNSA_NSID attribute (the id, int32; -1 = not assigned)
	netnsaFd   = 3 // NETNSA_FD attribute (fd referencing the target netns)

	rtgenLen  = 4 // NLMSG_ALIGN(sizeof(struct rtgenmsg)); rtgen_family is 1 byte
	fdAttrLen = 8 // nlattr header (4) + int32 fd payload (4)

	// nsidSeqCst is the nlmsg_seq this package puts on its request and then
	// demands back. The socket is opened, used and closed inside one Nsid call,
	// so there is never more than one request outstanding and the value itself
	// is arbitrary — but xtcpnl.WalkNlMsgs needs a seq to filter on, and reading
	// it out of the reply would defeat the point of filtering.
	nsidSeqCst = 1
)

// nativeEndian is the byte order netlink headers use: the host's. It is
// xtcpnl's, so the request this package writes and the walk xtcpnl does over
// the reply cannot disagree about it.
var nativeEndian = xtcpnl.NativeEndian()

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
	// recv timeout cannot be set we could block on a missing reply, so degrade to
	// (0,false) rather than take that risk.
	tv := unix.Timeval{Sec: 1}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return 0, false
	}

	req := buildGetNsidRequest(nsFD)
	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return 0, false
	}

	buf := make([]byte, 4096)
	n, _, err := unix.Recvfrom(fd, buf, 0)
	if err != nil || n < xtcpnl.NlMsgHdrSizeCst {
		return 0, false
	}
	return parseNsidResponse(buf[:n])
}

// buildGetNsidRequest lays out an RTM_GETNSID request: nlmsghdr + rtgenmsg
// (family AF_UNSPEC) + a single NETNSA_FD attribute carrying nsFD.
//
// This stays local rather than calling xtcpnl.BuildDumpRequest, which
// unconditionally sets NLM_F_REQUEST|NLM_F_DUMP. RTM_GETNSID is a single get,
// not a dump — asking the kernel to dump it would change the reply — and
// xtcpnl has no attribute encoder yet, so there is nothing here to reuse. The
// reply parsing, which is where the duplication actually was, is xtcpnl's.
func buildGetNsidRequest(nsFD int) []byte {
	total := xtcpnl.NlMsgHdrSizeCst + rtgenLen + fdAttrLen
	b := make([]byte, total)

	// struct nlmsghdr
	nativeEndian.PutUint32(b[0:4], uint32(total))              // nlmsg_len
	nativeEndian.PutUint16(b[4:6], rtmGetNsid)                 // nlmsg_type
	nativeEndian.PutUint16(b[6:8], uint16(unix.NLM_F_REQUEST)) // nlmsg_flags
	nativeEndian.PutUint32(b[8:12], nsidSeqCst)                // nlmsg_seq
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
//
// The framing walk is xtcpnl.WalkNlMsgs rather than a loop of our own: this
// package used to carry a second, independently-tested copy of nlmsghdr and
// nlattr parsing, which is the kind of duplication that drifts silently.
// xtcpnl's version is the one with real-pcap fixtures, fuzz targets and a
// benchmark gate behind it.
//
// Behaviour is unchanged except that the seq is now checked: WalkNlMsgs skips
// any message whose nlmsg_seq is not the one we sent, NLMSG_ERROR becomes a
// non-nil err (including the truncated-errno case), and NLMSG_DONE simply ends
// the walk with nothing found. All three land on (0, false) as before.
func parseNsidResponse(b []byte) (int32, bool) {
	var (
		id   int32
		ok   bool
		seen bool
	)

	// seen, not ok: the first RTM_NEWNSID decides, assigned or not. That is what
	// the hand-rolled loop this replaced did (it returned on the first match),
	// and it matches the kernel's own parse_rtattr first-wins convention.
	_, err := xtcpnl.WalkNlMsgs(b, nsidSeqCst, func(msgType uint16, body []byte) error {
		if msgType == rtmNewNsid && !seen {
			seen = true
			id, ok = parseNsidAttrs(body)
		}
		return nil
	})
	if err != nil {
		return 0, false
	}
	return id, ok
}

// parseNsidAttrs scans the attribute area of an RTM_NEWNSID payload (which starts
// with the aligned rtgenmsg header) for NETNSA_NSID.
func parseNsidAttrs(payload []byte) (int32, bool) {
	if len(payload) < rtgenLen {
		return 0, false
	}

	var (
		id    int32
		found bool
	)

	if err := xtcpnl.WalkRTAttrs(payload[rtgenLen:], func(atype uint16, val []byte) {
		if found || atype != netnsaNsid || len(val) < 4 {
			return
		}
		id = int32(nativeEndian.Uint32(val))
		found = true
	}); err != nil {
		return 0, false
	}

	if id < 0 { // NETNSA_NSID_NOT_ASSIGNED
		return 0, false
	}
	return id, found
}
