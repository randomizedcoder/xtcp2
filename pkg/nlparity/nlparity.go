// Package nlparity compares two nlmon captures of the same command and reports
// where the netlink traffic differs.
//
// It exists to make coverage gaps in pkg/xtcpnl fail a build. The layout oracle
// (nix/checks/proto-audit-netlink.nix) proves the fields xtcp2 decodes sit at
// the offsets the kernel uses; nothing proves the *set* of messages and
// attributes xtcp2 handles is the set a real tool exchanges. Capturing
// iproute2's `ip` and cmd/goip running the same command, then diffing the
// netlink, turns "did we cover this?" into a diff.
//
// The package is a pure function of bytes: it opens no socket, reads no
// /proc, and needs no root, so every assertion in it runs under
// `go test ./...` on the host. The capture side lives in Nix
// (nix/capture-netlink-fixtures.nix).
//
// # Why this has its own walker
//
// pkg/xtcpnl's walkNlMsgs is a *client* walker: it knows the seq it sent, it
// stops at NLMSG_DONE, it masks NLA_F_NESTED off attribute types, and a short
// trailing message is ErrBadMsgLen. Every one of those is wrong for parity:
//
//   - A replayed capture does not know the seq, because iproute2 seeds it from
//     time(NULL) (lib/libnetlink.c:249) and the same seq recurs across sockets.
//   - A short trailing remainder is the *normal* case, not an error. iproute2
//     sends `sizeof(req)` rather than nlmsg_len for addr, route and neigh
//     dumps, so the datagram carries a zeroed tail — 128 bytes for
//     rtnl_addrdump_req and rtnl_routedump_req, 256 for rtnl_neighdump_req.
//     Rejecting that would reject three of the four commands outright.
//   - A missing NLA_F_NESTED is itself a divergence worth reporting, so the
//     attribute type must stay unmasked here.
//   - NLMSG_DONE terminates a transaction but not a walk: a parity report wants
//     the DONE counted and compared like any other message.
//
// So the walk here is deliberately tolerant and records what it tolerated,
// rather than deciding. See WalkDatagram.
package nlparity

import "errors"

var (
	// ErrBadHead indicates the very first netlink message in a datagram is
	// unusable — nlmsg_len below a bare header, or overrunning the datagram.
	//
	// This is the one structural error the walker raises. Everything after a
	// good first message that cannot be parsed is classified as a tail and
	// reported (see Datagram.TailBytes), because that is what an oversend looks
	// like. A bad *head* is different in kind: there is no valid message to
	// anchor the datagram to, so nothing about it can be compared.
	ErrBadHead = errors.New("nlparity: first netlink message in the datagram is unusable")

	// ErrShortDatagram indicates a datagram shorter than a bare nlmsghdr.
	ErrShortDatagram = errors.New("nlparity: datagram shorter than nlmsghdr")
)
