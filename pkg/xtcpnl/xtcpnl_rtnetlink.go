package xtcpnl

// This file adds the rtnetlink (NETLINK_ROUTE) DUMP machinery xtcp2 needs to
// discover a network namespace's local links, addresses and routes so socket
// endpoints can be classified as self / connected-subnet / remote before the
// IP->ASN lookup.
//
// It mirrors the existing inet_diag request/parse style in this package: manual
// little-endian (de)serialization with explicit length checks (the host targets
// are amd64/arm64, both little-endian). The kernel UAPI enum values (RTM_*,
// IFA_*, RTA_*, RTN_*, RT_SCOPE_*, RT_TABLE_*, NLM_*, NLMSG_*) are taken from
// golang.org/x/sys/unix, which exports all of them.
//
// A DUMP request is one nlmsghdr (NLM_F_REQUEST|NLM_F_DUMP) followed by the
// family header (ifinfomsg / ifaddrmsg / rtmsg). The kernel replies with a
// multipart stream of RTM_NEW* messages terminated by NLMSG_DONE; DumpRtnetlink
// drives that stream.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// rtnetlinkRecvBufCst bounds a single Recvfrom. A busy host's route dump is
	// large (many /32 local-table entries); 64 KiB holds many messages per
	// recv, and DumpRtnetlink loops recvs until NLMSG_DONE regardless.
	rtnetlinkRecvBufCst = 64 * 1024
)

var (
	// ErrShortRecv indicates a recv returned fewer bytes than a bare nlmsghdr.
	ErrShortRecv = errors.New("xtcpnl: rtnetlink recv shorter than nlmsghdr")
	// ErrBadMsgLen indicates a message length that is impossible or overruns
	// the received buffer.
	ErrBadMsgLen = errors.New("xtcpnl: rtnetlink message length out of range")
	// ErrNetlinkError indicates a malformed NLMSG_ERROR (too short for errno).
	ErrNetlinkError = errors.New("xtcpnl: rtnetlink error message truncated")
	// ErrDumpInterrupted indicates the kernel flagged a reply with
	// NLM_F_DUMP_INTR: the table changed while it was being dumped, so the
	// stream may be inconsistent (entries missing or duplicated). DumpRtnetlink
	// still drains the stream to NLMSG_DONE before returning this, so the socket
	// is immediately reusable; callers should discard what onMsg collected and
	// re-issue the request.
	ErrDumpInterrupted = errors.New("xtcpnl: rtnetlink dump interrupted (NLM_F_DUMP_INTR), retry")
	// ErrShortRequest indicates a request shorter than a bare nlmsghdr.
	ErrShortRequest = errors.New("xtcpnl: rtnetlink request shorter than nlmsghdr")
)

// BuildDumpRequest lays out a DUMP request: a 16-byte nlmsghdr
// (NLM_F_REQUEST|NLM_F_DUMP) followed by the caller's family header, which the
// caller has already sized (ifinfomsg 16, ifaddrmsg 8, rtmsg 12) and populated
// with at least its family byte. All three sizes are already 4-byte aligned.
//
// It is the attribute-free, infallible shorthand for BuildRequest, and shares
// its layout code and its read-only allowlist. Because the signature has no
// error to return, a msgType outside the allowlist yields **nil** rather than a
// write request. That is not a silent failure: DumpRtnetlink rejects a nil
// request with ErrShortRequest before sending anything. Callers wanting the
// reason should use BuildRequest.
func BuildDumpRequest(msgType uint16, seq uint32, familyHdr []byte) []byte {
	if !IsBuildableRequestType(msgType) {
		return nil
	}
	return layoutRequest(msgType, uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP), seq, familyHdr, nil)
}

// BuildDumpLinkRequest builds an RTM_GETLINK dump request (ifinfomsg,
// AF_UNSPEC) to enumerate all links.
func BuildDumpLinkRequest(seq uint32) []byte {
	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = unix.AF_UNSPEC
	return BuildDumpRequest(uint16(unix.RTM_GETLINK), seq, hdr)
}

// BuildDumpAddrRequest builds an RTM_GETADDR dump request (ifaddrmsg) for the
// given address family (unix.AF_INET, unix.AF_INET6, or unix.AF_UNSPEC for
// both).
func BuildDumpAddrRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, IfAddrmsgSizeCst)
	hdr[0] = family
	return BuildDumpRequest(uint16(unix.RTM_GETADDR), seq, hdr)
}

// BuildDumpRouteRequest builds an RTM_GETROUTE dump request (rtmsg) for the
// given address family. The kernel dumps the main table by default; callers
// wanting the local table read RTA_TABLE on each reply (RouteInfo.Table).
func BuildDumpRouteRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, RtMsgSizeCst)
	hdr[0] = family
	return BuildDumpRequest(uint16(unix.RTM_GETROUTE), seq, hdr)
}

// DumpRtnetlink sends request on fd and drives the multipart reply, invoking
// onMsg for every RTM_NEW* message body (the bytes after the 16-byte nlmsghdr)
// whose nlmsg_seq matches the request's. It returns nil at NLMSG_DONE (or a
// zero-errno ACK), a wrapped syscall.Errno for a non-zero NLMSG_ERROR, and
// skips NLMSG_NOOP. The socket should have a receive timeout set so a missing
// DONE degrades to an error instead of blocking. onMsg must copy any bytes it
// needs to retain — the receive buffer is reused across recvs.
//
// Hardening (see WalkNlMsgs for the per-datagram rules):
//   - datagrams whose sender pid is not the kernel (nlmsg from another
//     userspace process on a multicast-joined socket) are ignored;
//   - messages whose nlmsg_seq differs from the request's are ignored, so a
//     stale reply (or a stale NLMSG_DONE) left over from an earlier timed-out
//     dump on the same socket cannot be mistaken for this one;
//   - a reply flagged NLM_F_DUMP_INTR makes the whole dump return
//     ErrDumpInterrupted — but only after the stream has been drained to
//     NLMSG_DONE, so the caller can retry on the same socket straight away.
//
// sa may be nil for a connected socket (tests drive this over an AF_UNIX
// SOCK_SEQPACKET socketpair); on a bound NETLINK_ROUTE socket pass the kernel
// address {Family: AF_NETLINK}.
func DumpRtnetlink(fd int, request []byte, sa *unix.SockaddrNetlink, onMsg func(msgType uint16, body []byte) error) error {
	if len(request) < NlMsgHdrSizeCst {
		return ErrShortRequest
	}
	seq := binary.LittleEndian.Uint32(request[8:12])

	var to unix.Sockaddr
	if sa != nil {
		to = sa
	}
	if err := unix.Sendto(fd, request, 0, to); err != nil {
		return fmt.Errorf("xtcpnl: rtnetlink send: %w", err)
	}

	buf := make([]byte, rtnetlinkRecvBufCst)
	interrupted := false
	for {
		n, from, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return fmt.Errorf("xtcpnl: rtnetlink recv: %w", err)
		}
		if !fromKernel(from) {
			continue
		}

		deliver := onMsg
		if interrupted {
			deliver = nil // draining only: nothing more is delivered after an interruption
		}
		done, werr := WalkNlMsgs(buf[:n], seq, deliver)
		switch {
		case errors.Is(werr, ErrDumpInterrupted):
			interrupted = true
		case werr != nil:
			return werr
		}
		if done {
			if interrupted {
				return ErrDumpInterrupted
			}
			return nil
		}
	}
}

// errTalkStopCst is an internal sentinel: the onMsg callback TalkRtnetlink
// hands to WalkNlMsgs returns it to stop the walk after the first reply.
// WalkNlMsgs propagates an onMsg error immediately, which is exactly the
// "stop here" primitive needed, and reusing the one walker is better than
// giving the single-get path a second copy of the message loop.
//
// It never escapes TalkRtnetlink.
var errTalkStopCst = errors.New("xtcpnl: internal: first reply captured")

// TalkRtnetlink sends one NON-DUMP request and returns the single reply.
//
// # Why DumpRtnetlink cannot do this
//
// A single get — `ip link show dev X`, iproute2's ll_link_get — sends
// flags=NLM_F_REQUEST with no NLM_F_DUMP, and the kernel answers with exactly
// one message: no NLM_F_MULTI, and **no NLMSG_DONE**. DumpRtnetlink's loop
// terminates only on DONE or NLMSG_ERROR, so pointing it at a single get makes
// it recv until SO_RCVTIMEO and then report a timeout, which looks like a
// kernel problem rather than a misuse. Verified against the ten captured
// single-gets in testdata/7_1_8/netlink_route_getroute.pcap, all
// flags=0x0001 with a lone RTM_NEWLINK reply.
//
// The converse misuse is just as wrong: pointing TalkRtnetlink at a dump
// returns the first message and abandons the rest mid-stream, leaving the
// socket holding the remainder. Use DumpRtnetlink for anything with
// NLM_F_DUMP set.
//
// # What it returns
//
//   - the first reply that is not a control message: its type and a COPY of its
//     body (unlike DumpRtnetlink's callback, which aliases the receive buffer),
//     with a nil error;
//   - (0, nil, nil) for an NLMSG_ERROR carrying errno 0 — that is an ACK, the
//     normal answer to a request that has nothing to return — and likewise for
//     an NLMSG_DONE, which a single get should not send but which is not an
//     error if it does;
//   - a wrapped syscall.Errno for an NLMSG_ERROR with a non-zero errno, so
//     callers can errors.Is(err, unix.ENODEV);
//   - ErrShortRequest if the request is not even an nlmsghdr, before anything
//     is sent. BuildDumpRequest's nil for a rejected message type lands here.
//
// Messages whose nlmsg_seq is not the request's are skipped, and datagrams
// whose sender is not the kernel are dropped, both by the same rules
// DumpRtnetlink uses (see WalkNlMsgs and fromKernel). If a datagram contains
// nothing for this request, it recvs again — so the socket must have
// SO_RCVTIMEO set, or a request the kernel never answers blocks forever.
//
// sa may be nil for a connected socket; the tests drive this over an AF_UNIX
// SOCK_SEQPACKET socketpair.
func TalkRtnetlink(fd int, request []byte, sa *unix.SockaddrNetlink) (msgType uint16, body []byte, err error) {
	if len(request) < NlMsgHdrSizeCst {
		return 0, nil, ErrShortRequest
	}
	seq := binary.LittleEndian.Uint32(request[8:12])

	var to unix.Sockaddr
	if sa != nil {
		to = sa
	}
	if serr := unix.Sendto(fd, request, 0, to); serr != nil {
		return 0, nil, fmt.Errorf("xtcpnl: rtnetlink send: %w", serr)
	}

	buf := make([]byte, rtnetlinkRecvBufCst)
	for {
		n, from, rerr := unix.Recvfrom(fd, buf, 0)
		if rerr != nil {
			return 0, nil, fmt.Errorf("xtcpnl: rtnetlink recv: %w", rerr)
		}
		if !fromKernel(from) {
			continue
		}

		var (
			gotType uint16
			gotBody []byte
		)
		done, werr := WalkNlMsgs(buf[:n], seq, func(mt uint16, mbody []byte) error {
			gotType = mt
			gotBody = CopyBytes(mbody)
			return errTalkStopCst
		})
		switch {
		case errors.Is(werr, errTalkStopCst):
			return gotType, gotBody, nil
		case werr != nil:
			return 0, nil, werr
		case done:
			// NLMSG_DONE, or an NLMSG_ERROR with errno 0: an ACK.
			return 0, nil, nil
		}
		// Every message in this datagram belonged to another request. Recv again
		// rather than reporting a reply we never saw.
	}
}

// fromKernel reports whether a datagram's sender address is the kernel. On a
// netlink socket the kernel is always nlmsg_pid 0; a datagram from any other
// netlink port id is a userspace peer and is dropped. A nil or non-netlink
// address (AF_UNIX socketpair in tests) is accepted.
func fromKernel(from unix.Sockaddr) bool {
	sn, ok := from.(*unix.SockaddrNetlink)
	return !ok || sn.Pid == 0
}

// WalkNlMsgs parses one received datagram: a run of 4-byte-aligned netlink
// messages. It is pure (no I/O) so it can be table- and fuzz-tested directly.
//
// Rules, in order, for each message:
//   - fewer than 16 bytes in the datagram at all → ErrShortRecv;
//   - nlmsg_len < 16 or overrunning the datagram → ErrBadMsgLen;
//   - nlmsg_seq != seq → skipped entirely (stale reply, including a stale DONE);
//   - NLM_F_DUMP_INTR set → the rest of this datagram is walked but not
//     delivered, and the return error is ErrDumpInterrupted (with done set if
//     DONE was also reached);
//   - NLMSG_DONE → done=true; NLMSG_ERROR → done=true with netlinkErr (nil for
//     a zero-errno ACK); NLMSG_NOOP skipped; anything else → onMsg (a nil onMsg
//     discards), whose error is returned immediately.
//
// A trailing remainder shorter than a header is ignored, mirroring the
// kernel's NLMSG_OK walk. done=false, err=nil means the dump continues in the
// next datagram.
//
// seq must be the sequence number the caller put on its own request. There is
// deliberately no "accept any seq" sentinel, because every uint32 is a legal
// nlmsg_seq — iproute2's rtnl_open seeds it from time(NULL)
// (lib/libnetlink.c:249), so real captures contain arbitrary values. Code that
// reads back a recorded stream and so cannot know the seq should take it from
// the first header, or use pkg/nlparity, whose tolerant walker does not filter
// at all.
func WalkNlMsgs(data []byte, seq uint32, onMsg func(msgType uint16, body []byte) error) (done bool, err error) {
	if len(data) < NlMsgHdrSizeCst {
		return false, ErrShortRecv
	}

	interrupted := false
	for len(data) >= NlMsgHdrSizeCst {
		var h NlMsgHdr
		if _, derr := DeserializeNlMsgHdr(data, &h); derr != nil {
			return false, derr
		}
		msgLen := int(h.Len)
		if msgLen < NlMsgHdrSizeCst || msgLen > len(data) {
			return false, ErrBadMsgLen
		}
		body := data[NlMsgHdrSizeCst:msgLen]

		adv := msgLen + FourByteAlignPadding(msgLen)
		if adv > len(data) {
			adv = len(data) // last message: padding may legitimately be absent
		}

		if h.Seq != seq {
			data = data[adv:]
			continue
		}
		if h.Flags&uint16(unix.NLM_F_DUMP_INTR) != 0 {
			interrupted = true
		}

		switch h.Type {
		case uint16(unix.NLMSG_DONE):
			if interrupted {
				return true, ErrDumpInterrupted
			}
			return true, nil
		case uint16(unix.NLMSG_ERROR):
			if interrupted {
				return true, ErrDumpInterrupted
			}
			return true, netlinkErr(body)
		case uint16(unix.NLMSG_NOOP):
			// nothing to do
		default:
			if !interrupted && onMsg != nil {
				if cerr := onMsg(h.Type, body); cerr != nil {
					return false, cerr
				}
			}
		}

		data = data[adv:]
	}

	if interrupted {
		return false, ErrDumpInterrupted
	}
	return false, nil
}

// netlinkErr decodes an NLMSG_ERROR body. The kernel puts a negative errno in
// the first int32; a zero errno is an ACK (not an error).
func netlinkErr(body []byte) error {
	if len(body) < 4 {
		return ErrNetlinkError
	}
	errno := int32(binary.LittleEndian.Uint32(body[0:4]))
	if errno == 0 {
		return nil
	}
	return fmt.Errorf("xtcpnl: rtnetlink error: %w", syscall.Errno(-errno))
}

// NlaTypeMaskCst clears the two flag bits the kernel ORs into nla_type, leaving
// the attribute type itself:
//
//	NLA_F_NESTED        0x8000  the payload is itself a stream of attributes
//	NLA_F_NET_BYTEORDER 0x4000  the payload is big-endian, not host order
//
// Both are defined in include/uapi/linux/netlink.h and exported by
// golang.org/x/sys/unix.
const NlaTypeMaskCst uint16 = ^uint16(unix.NLA_F_NESTED | unix.NLA_F_NET_BYTEORDER)

// WalkRTAttrs iterates the RTAttr TLVs in data, calling fn for each with its
// type and value slice (a view into data — copy what you retain). It validates
// each attribute length and advances by the 4-byte-aligned length, tolerating a
// short trailing remainder like the kernel's NLA_ALIGN walk.
//
// The type passed to fn has NLA_F_NESTED and NLA_F_NET_BYTEORDER masked off, so
// a caller comparing against a bare IFLA_*/RTA_*/NDA_* constant matches whether
// or not the kernel flagged the attribute. Without the mask a nested attribute
// silently fails every switch case, which is a bug that presents as missing
// data rather than as an error.
func WalkRTAttrs(data []byte, fn func(atype uint16, val []byte)) error {
	for len(data) >= RTAttrSizeCst {
		var rta RTAttr
		if _, err := DeserializeRTAttr(data, &rta); err != nil {
			return err
		}
		alen := int(rta.Len)
		if alen < RTAttrSizeCst || alen > len(data) {
			return ErrRTAttrSmall
		}
		fn(rta.Type&NlaTypeMaskCst, data[RTAttrSizeCst:alen])

		adv := alen + FourByteAlignPadding(alen)
		if adv <= 0 || adv > len(data) {
			break
		}
		data = data[adv:]
	}
	return nil
}

// WalkRTAttrsNested descends into a nested attribute's payload, which is itself
// a stream of TLVs laid out exactly like a top-level one. It is a thin alias
// for WalkRTAttrs, named so call sites read as a descent and so the nesting is
// visible when reading a parser.
func WalkRTAttrsNested(val []byte, fn func(atype uint16, val []byte)) error {
	return WalkRTAttrs(val, fn)
}

// CopyBytes returns a fresh copy of b, or nil for an empty slice, so parsed
// results never alias the reused receive buffer.
func CopyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}
