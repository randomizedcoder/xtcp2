package xtcpnl

// This file adds the rtnetlink (NETLINK_ROUTE) DUMP machinery xtcp2 needs to
// discover a network namespace's local links, addresses and routes so socket
// endpoints can be classified as self / connected-subnet / remote before the
// IP->ASN lookup.
//
// It mirrors the existing inet_diag request/parse style in this package: manual
// little-endian (de)serialisation with explicit length checks (the host targets
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
)

// buildDumpRequest lays out a DUMP request: a 16-byte nlmsghdr
// (NLM_F_REQUEST|NLM_F_DUMP) followed by the caller's family header, which the
// caller has already sized (ifinfomsg 16, ifaddrmsg 8, rtmsg 12) and populated
// with at least its family byte. All three sizes are already 4-byte aligned.
func buildDumpRequest(msgType uint16, seq uint32, familyHdr []byte) []byte {
	total := NlMsgHdrSizeCst + len(familyHdr)
	b := make([]byte, total)

	binary.LittleEndian.PutUint32(b[0:4], uint32(total))                              // nlmsg_len
	binary.LittleEndian.PutUint16(b[4:6], msgType)                                    // nlmsg_type
	binary.LittleEndian.PutUint16(b[6:8], uint16(unix.NLM_F_REQUEST|unix.NLM_F_DUMP)) // nlmsg_flags
	binary.LittleEndian.PutUint32(b[8:12], seq)                                       // nlmsg_seq
	// b[12:16] nlmsg_pid = 0 (kernel fills the peer pid)

	copy(b[NlMsgHdrSizeCst:], familyHdr)
	return b
}

// BuildDumpLinkRequest builds an RTM_GETLINK dump request (ifinfomsg,
// AF_UNSPEC) to enumerate all links.
func BuildDumpLinkRequest(seq uint32) []byte {
	hdr := make([]byte, IfInfomsgSizeCst)
	hdr[0] = unix.AF_UNSPEC
	return buildDumpRequest(uint16(unix.RTM_GETLINK), seq, hdr)
}

// BuildDumpAddrRequest builds an RTM_GETADDR dump request (ifaddrmsg) for the
// given address family (unix.AF_INET, unix.AF_INET6, or unix.AF_UNSPEC for
// both).
func BuildDumpAddrRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, IfAddrmsgSizeCst)
	hdr[0] = family
	return buildDumpRequest(uint16(unix.RTM_GETADDR), seq, hdr)
}

// BuildDumpRouteRequest builds an RTM_GETROUTE dump request (rtmsg) for the
// given address family. The kernel dumps the main table by default; callers
// wanting the local table read RTA_TABLE on each reply (RouteInfo.Table).
func BuildDumpRouteRequest(family uint8, seq uint32) []byte {
	hdr := make([]byte, RtMsgSizeCst)
	hdr[0] = family
	return buildDumpRequest(uint16(unix.RTM_GETROUTE), seq, hdr)
}

// DumpRtnetlink sends request on fd and drives the multipart reply, invoking
// onMsg for every RTM_NEW* message body (the bytes after the 16-byte nlmsghdr).
// It returns nil at NLMSG_DONE (or a zero-errno ACK), a wrapped syscall.Errno
// for a non-zero NLMSG_ERROR, and skips NLMSG_NOOP. The socket should have a
// receive timeout set so a missing DONE degrades to an error instead of
// blocking. onMsg must copy any bytes it needs to retain — the receive buffer
// is reused across recvs.
func DumpRtnetlink(fd int, request []byte, sa *unix.SockaddrNetlink, onMsg func(msgType uint16, body []byte) error) error {
	if err := unix.Sendto(fd, request, 0, sa); err != nil {
		return fmt.Errorf("xtcpnl: rtnetlink send: %w", err)
	}

	buf := make([]byte, rtnetlinkRecvBufCst)
	for {
		n, _, err := unix.Recvfrom(fd, buf, 0)
		if err != nil {
			return fmt.Errorf("xtcpnl: rtnetlink recv: %w", err)
		}
		if n < NlMsgHdrSizeCst {
			return ErrShortRecv
		}

		data := buf[:n]
		for len(data) >= NlMsgHdrSizeCst {
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(data, &h); err != nil {
				return err
			}
			msgLen := int(h.Len)
			if msgLen < NlMsgHdrSizeCst || msgLen > len(data) {
				return ErrBadMsgLen
			}

			switch h.Type {
			case uint16(unix.NLMSG_DONE):
				return nil
			case uint16(unix.NLMSG_ERROR):
				return netlinkErr(data[NlMsgHdrSizeCst:msgLen])
			case uint16(unix.NLMSG_NOOP):
				// nothing to do
			default:
				if err := onMsg(h.Type, data[NlMsgHdrSizeCst:msgLen]); err != nil {
					return err
				}
			}

			adv := msgLen + FourByteAlignPadding(msgLen)
			if adv <= 0 || adv > len(data) {
				break
			}
			data = data[adv:]
		}
	}
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

// walkRTAttrs iterates the RTAttr TLVs in data, calling fn for each with its
// type and value slice (a view into data — copy what you retain). It validates
// each attribute length and advances by the 4-byte-aligned length, tolerating a
// short trailing remainder like the kernel's NLA_ALIGN walk.
func walkRTAttrs(data []byte, fn func(atype uint16, val []byte)) error {
	for len(data) >= RTAttrSizeCst {
		var rta RTAttr
		if _, err := DeserializeRTAttr(data, &rta); err != nil {
			return err
		}
		alen := int(rta.Len)
		if alen < RTAttrSizeCst || alen > len(data) {
			return ErrRTAttrSmall
		}
		fn(rta.Type, data[RTAttrSizeCst:alen])

		adv := alen + FourByteAlignPadding(alen)
		if adv <= 0 || adv > len(data) {
			break
		}
		data = data[adv:]
	}
	return nil
}

// copyBytes returns a fresh copy of b, or nil for an empty slice, so parsed
// results never alias the reused receive buffer.
func copyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}
