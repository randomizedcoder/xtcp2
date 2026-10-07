package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// ErrNoReplay is returned when a replay source is asked for a message type the
// recorded capture does not contain — a missing fixture, not a protocol error.
var ErrNoReplay = errors.New("goip: no recorded reply of that type in the capture")

// Source is where an object handler gets its netlink replies.
//
// The interface is one method wide on purpose. Everything a read-only `show`
// does is "send this request, hand me the reply bodies", and keeping it that
// narrow is what lets the pcap implementation below exist at all — a formatter
// test needs no socket, no root and no VM, which is iproute2's own
// `ip addr showdump` trick (lib/libnetlink.c:1322) applied to the test suite
// rather than to the CLI.
//
// Dump returns the message bodies (the bytes after each nlmsghdr) of every
// reply of msgType, in wire order.
type Source interface {
	Dump(request []byte, msgType uint16) ([][]byte, error)
}

// TalkSource is the narrow sibling used by RTM_GETLINK single-get operations.
// It is intentionally not folded into Source: dump-only fixtures remain valid
// service inputs, while callers can select the stronger contract only when a
// get operation actually requires it.
type TalkSource interface {
	Talk(request []byte, msgType uint16) ([]byte, error)
}

// NetlinkSource is the live implementation: a bound NETLINK_ROUTE socket.
type NetlinkSource struct {
	fd int
	sa *unix.SockaddrNetlink
}

// OpenNetlink opens and binds a NETLINK_ROUTE socket.
//
// # Two socket options that do not appear in an nlmon capture
//
// nlmon mirrors datagrams, not syscalls, so neither of these is visible in a
// parity pcap — which is exactly why they are set here explicitly and
// commented, rather than left to the default:
//
//   - NETLINK_GET_STRICT_CHK, which `ip` enables at ip.c:312. It makes the
//     kernel validate a dump request's family header instead of ignoring the
//     parts it does not expect, and it is the reason
//     BuildDumpAddrRequestIndex's ifa_index in the *header* is honored at
//     all. Without it that filter is silently dropped and the reply set is
//     wrong while every request byte still matches.
//   - SO_RCVBUF at 1 MiB, matching `ip`. The default is much smaller, and a
//     dump on a host with many links or routes then arrives in more datagrams
//     than `ip` would see — a difference in reply framing that is not a
//     difference in what was asked.
//
// Strict checking degrades rather than refuses: a kernel predating
// NETLINK_GET_STRICT_CHK (pre-4.20) answers ENOPROTOOPT, and that one errno is
// ignored. Nothing else is. In particular SO_RCVBUF cannot fail on a socket
// this function just created — Linux *clamps* the request to net.core.rmem_max
// rather than rejecting it (net/core/sock.c, SO_RCVBUF) — so an error there is
// a bad fd, which is a bug here and not a property of the host. Reporting it is
// the point; a blanket `_ =` would have hidden it, and hidden a typo'd
// NETLINK_GET_STRICT_CHK level along with it.
func OpenNetlink() (*NetlinkSource, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, fmt.Errorf("goip: netlink socket: %w", err)
	}

	// abandon closes the half-built socket and returns the reason it was
	// abandoned. A failing Close is joined rather than dropped, since it is the
	// one signal that fd was not what this function thinks it was.
	abandon := func(cause error) error {
		if cerr := unix.Close(fd); cerr != nil {
			return errors.Join(cause, fmt.Errorf("goip: netlink close: %w", cerr))
		}
		return cause
	}

	// See the doc comment: ENOPROTOOPT means the running kernel has no strict
	// checking, which is a degraded run, not a failed one.
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_GET_STRICT_CHK, 1); err != nil &&
		!errors.Is(err, unix.ENOPROTOOPT) {
		return nil, abandon(fmt.Errorf("goip: netlink strict check: %w", err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
		return nil, abandon(fmt.Errorf("goip: netlink SO_RCVBUF: %w", err))
	}

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, sa); err != nil {
		return nil, abandon(fmt.Errorf("goip: netlink bind: %w", err))
	}
	return &NetlinkSource{fd: fd, sa: &unix.SockaddrNetlink{Family: unix.AF_NETLINK}}, nil
}

// Close releases the socket.
func (s *NetlinkSource) Close() error {
	if s.fd < 0 {
		return nil
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return err
}

// Dump sends the request and collects every reply body of msgType.
func (s *NetlinkSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	err := xtcpnl.DumpRtnetlink(s.fd, request, s.sa, func(mt uint16, body []byte) error {
		if mt != msgType {
			return nil
		}
		out = append(out, xtcpnl.CopyBytes(body))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("goip: dump: %w", err)
	}
	return out, nil
}

// Talk sends a non-dump request and returns its single reply body.
func (s *NetlinkSource) Talk(request []byte, msgType uint16) ([]byte, error) {
	typ, body, err := xtcpnl.TalkRtnetlink(s.fd, request, s.sa)
	if err != nil {
		return nil, fmt.Errorf("goip: talk: %w", err)
	}
	if typ != msgType {
		return nil, fmt.Errorf("goip: talk: reply type %d, want %d", typ, msgType)
	}
	return xtcpnl.CopyBytes(body), nil
}

// ReplaySource answers from a recorded nlmon capture instead of a socket.
//
// # Why this is the primary formatter test, not a convenience
//
// A renderer test that needs a live netlink socket cannot run in a nix build,
// cannot run unprivileged in CI, and — worst of all — asserts against whatever
// interfaces the machine happens to have. Replaying a committed pcap fixes all
// three: the expectations can cite `ip_link_n` line by line, because the pcap
// and the sidecar came from the same capture run.
//
// # Why it uses pkg/nlparity rather than xtcpnl.WalkNlMsgs
//
// WalkNlMsgs filters on the sequence number the *caller* put on its own
// request, and there is deliberately no "any seq" sentinel, because every
// uint32 is a legal nlmsg_seq — iproute2 seeds it from time(NULL). A replay
// did not send the request and cannot know the seq. WalkNlMsgs' own doc
// comment names pkg/nlparity as the answer for exactly this case, and
// nlparity's walker additionally tolerates the zeroed oversend tail that a
// real capture carries.
//
// # The request argument is ignored, and that is a real limitation
//
// This source cannot tell two different requests apart; it returns every
// recorded reply of the requested type. That is sufficient for a renderer
// test, whose subject is the reply formatting, and it is why parity of the
// *request* is asserted separately and byte-for-byte by Tier A rather than
// here.
//
// # Attribution, which a two-dump command cannot do without
//
// nlmon mirrors every netlink datagram on the host, so a capture is not one
// tool's conversation unless it happens to have been taken on an idle
// machine. netlink_route_getlink.pcap was; netlink_route_getaddr.pcap was not.
// That one holds 251 messages across six portids: two `ip` runs, a
// third-party RTM_NEWADDR write, nine RTM_GETLINK single-gets and a 110-reply
// RTM_GETNEIGH dump from an unrelated process, plus two 20-byte RTM_GETADDR
// dumps built on rtgenmsg rather than ifaddrmsg — not iproute2 at all.
//
// portid is the discriminator, and it is the only one available. Requests
// carry nlmsg_pid = 0 and so cannot be attributed directly, but every reply
// carries the portid of the socket that asked, and a dump's replies are
// therefore self-identifying. Sequence number is useless for this: iproute2
// seeds it from time(NULL), so the two `ip` runs in that capture — started
// within the same second — share both of their seq values exactly.
//
// Portid 0 means "every reply", which is right for a clean single-tool
// capture and wrong for this one.
type ReplaySource struct {
	cap    nlparity.Capture
	portid uint32
}

// OpenReplay reads a DLT_NETLINK pcap from disk and replays every reply in it.
func OpenReplay(path string) (*ReplaySource, error) {
	return OpenReplayPortid(path, 0)
}

// OpenReplayPortid reads a DLT_NETLINK pcap and replays only the replies
// addressed to one netlink portid. See the type's doc comment for why that is
// necessary rather than convenient.
func OpenReplayPortid(path string, portid uint32) (*ReplaySource, error) {
	c, err := nlparity.ParseRouteCaptureFile(path)
	if err != nil {
		return nil, err
	}
	return &ReplaySource{cap: c, portid: portid}, nil
}

// NewReplayFromBytes builds a replay source from pcap bytes already in memory.
func NewReplayFromBytes(data []byte) (*ReplaySource, error) {
	c, err := nlparity.ParseCapture(data, uint16(unix.NETLINK_ROUTE))
	if err != nil {
		return nil, err
	}
	return &ReplaySource{cap: c}, nil
}

// Dump returns every recorded reply body of msgType, in capture order.
//
// Requests are excluded by NLM_F_REQUEST, not by capture direction: nlmon
// records both directions with sll_pkttype PACKET_OUTGOING, because
// AF_PACKET's dev_queue_xmit_nit overwrites what __netlink_deliver_tap_skb
// set. Without this filter an `RTM_GETLINK` dump request would be handed to
// ParseNewLink as if it were a reply, since RTM_GETLINK and RTM_NEWLINK are
// different types but a GETADDR/NEWADDR mix-up of the same shape is easy to
// write.
//
// # An empty dump is an answer, and a missing fixture is not
//
// No replies of msgType used to mean ErrNoReplay unconditionally, which
// conflated two different things. `ip route show dev veth0` on the mesh
// topology is answered by NLMSG_DONE alone — the device owns no routes — and
// the real `ip` prints nothing and exits 0. Reporting that as a missing
// fixture made the corpus's only empty dump unusable, and would have made any
// future capture of an empty answer look like a broken pcap.
//
// The discriminator is the REQUEST, which is why this method's first parameter
// is no longer ignored. If the capture recorded a request of the same
// nlmsg_type the caller is sending, then this dump did happen and its answer
// was genuinely empty. If it recorded no such request, the capture is of some
// other command and the fixture really is missing.
//
// Matching on the caller's own nlmsg_type rather than deriving the GET type
// from msgType keeps this free of arithmetic on the RTM_ enum. The derivation
// would have been sound — `RTM_FAM` (include/uapi/linux/rtnetlink.h:211)
// depends on the NEW/DEL/GET/SET grouping, so GET is always NEW+2 — but the
// request is direct evidence and the enum layout is not evidence at all.
func (s *ReplaySource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	for _, m := range s.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != msgType {
			continue
		}
		if s.portid != 0 && m.Hdr.Pid != s.portid {
			continue
		}
		out = append(out, xtcpnl.CopyBytes(m.Body))
	}
	if len(out) == 0 && !s.recordedRequest(request) {
		return nil, fmt.Errorf("%w: type %d", ErrNoReplay, msgType)
	}
	return out, nil
}

// recordedRequest reports whether the capture holds a request with the same
// nlmsg_type as the one being sent.
//
// A short or absent request is reported as not recorded, which keeps the old
// ErrNoReplay behavior for any caller that has no request bytes to offer:
// without evidence that the dump was asked for, "no replies" stays a missing
// fixture.
func (s *ReplaySource) recordedRequest(request []byte) bool {
	if len(request) < xtcpnl.NlMsgHdrSizeCst {
		return false
	}
	typ := binary.LittleEndian.Uint16(request[4:6])
	for _, m := range s.cap.Msgs() {
		if m.IsRequest() && m.Hdr.Type == typ {
			return true
		}
	}
	return false
}

// replayPathFromEnv reports the capture a replay run should read, and whether
// one was requested at all.
//
// GOIP_REPLAY is how `goip` is pointed at a pcap instead of a socket. It is an
// environment variable rather than a flag so that the argv goip is driven with
// stays byte-identical to the argv `ip` is driven with — the parity harness
// runs both tools with the *same* arguments, and a goip-only flag would make
// that impossible.
func replayPathFromEnv() (string, bool) {
	p := os.Getenv("GOIP_REPLAY")
	return p, p != ""
}

// replayPortidFromEnv reports the portid a replay run should filter replies to,
// or 0 for all of them.
//
// GOIP_REPLAY_PORTID exists for the same reason GOIP_REPLAY does — the argv
// has to stay identical to the argv `ip` is driven with — and it is needed
// whenever the capture is not single-tool. Unparseable input yields 0, the
// same as unset: this is a debugging affordance on a replay path, and failing
// the whole command over a typo in an environment variable would be a worse
// outcome than replaying everything and having the output obviously wrong.
func replayPortidFromEnv() uint32 {
	v, err := strconv.ParseUint(os.Getenv("GOIP_REPLAY_PORTID"), 10, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}
