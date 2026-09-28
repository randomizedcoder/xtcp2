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

	req, err := buildGetNsidRequest(nsFD)
	if err != nil {
		return 0, false
	}
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
// # This used to be hand-packed, and no longer is
//
// The previous version wrote all 28 bytes here, with a comment saying xtcpnl
// had no attribute encoder so there was nothing to reuse. That stopped being
// true when xtcpnl.AttrBuilder and xtcpnl.BuildRequest landed, so this is now
// the same three facts — message type, seq, one attribute — expressed once
// each, and the offsets, the alignment and the length back-patch are all
// xtcpnl's problem.
//
// What did NOT change is the flags. `xtcpnl.BuildDumpRequest` sets
// NLM_F_REQUEST|NLM_F_DUMP unconditionally, and RTM_GETNSID is a single get:
// asking the kernel to dump it changes the reply. So this calls BuildRequest
// with flags 0, which ORs in NLM_F_REQUEST and nothing else. BuildRequest's
// read-only allowlist accepts RTM_GETNSID by arithmetic (90 = RTM_BASE + 4*18
// + 2), and FamilyHdrLen does not model rtgenmsg, so the 4-byte family header
// passes through unchecked — correct here, since rtgen_family is AF_UNSPEC (0)
// and the remaining three bytes are NLMSG_ALIGN padding.
//
// The error is structural rather than situational: the buffer is exactly the
// size of the one attribute, so the only way to reach it is an
// xtcpnl-side change. Nsid degrades to (0, false) on it, like every other
// failure on this path.
func buildGetNsidRequest(nsFD int) ([]byte, error) {
	ab := xtcpnl.NewAttrBuilder(make([]byte, fdAttrLen))
	// NETNSA_FD is an int32 in the kernel's policy (uapi/linux/net_namespace.h);
	// the conversion is through int32 so a negative fd would sign-extend the
	// way the kernel reads it, rather than through uint.
	if err := ab.PutU32(netnsaFd, uint32(int32(nsFD))); err != nil {
		return nil, err
	}
	return xtcpnl.BuildRequest(rtmGetNsid, 0, nsidSeqCst, make([]byte, rtgenLen), ab.Bytes())
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
// Behavior is unchanged except that the seq is now checked: WalkNlMsgs skips
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
