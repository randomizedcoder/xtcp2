package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

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

// MsgDumpSource is the narrow sibling `ip addr save` needs: each reply of
// msgType whole, nlmsg_len bytes with its nlmsghdr, as the kernel sent it.
// save writes the replies to the dump file verbatim (ip/ipaddress.c:1821),
// and the header's flags are not derivable from the body: a dump the kernel
// filtered by ifindex carries NLM_F_DUMP_FILTERED.
type MsgDumpSource interface {
	DumpMsgs(request []byte, msgType uint16) ([][]byte, error)
}

// wholeMsg lays a reply out again as it arrived: the header verbatim, then
// the body.
func wholeMsg(h xtcpnl.NlMsgHdr, body []byte) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], h.Len)
	binary.LittleEndian.PutUint16(b[4:6], h.Type)
	binary.LittleEndian.PutUint16(b[6:8], h.Flags)
	binary.LittleEndian.PutUint32(b[8:12], h.Seq)
	binary.LittleEndian.PutUint32(b[12:16], h.Pid)
	copy(b[xtcpnl.NlMsgHdrSizeCst:], body)
	return b
}

// NetlinkSource is the live implementation: a bound NETLINK_ROUTE socket, and
// the NETLINK_GENERIC one a genl object opens on first use (source_genl.go).
type NetlinkSource struct {
	fd int
	sa *unix.SockaddrNetlink
	// gfd is the genl socket, valid when gopen is set.
	gfd   int
	gopen bool
	// xfd is the NETLINK_XFRM socket, valid when xopen is set.
	xfd   int
	xopen bool
	// rcvTimeout is OpenNetlinkTimeout's, which the genl socket gets too
	// when it opens; zero is none.
	rcvTimeout time.Duration
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
//   - NETLINK_EXT_ACK, which `ip` enables at lib/libnetlink.c:222. A refusal
//     then carries the kernel's NLMSGERR_ATTR_MSG and NLM_F_ACK_TLVS in its
//     own header, so without it goip's NLMSG_ERROR differs from ip's in
//     header.flags (`neigh get` of a missing entry: ip 0x0200, goip 0).
//   - SO_RCVBUF at 1 MiB, matching `ip`. The default is much smaller, and a
//     dump on a host with many links or routes then arrives in more datagrams
//     than `ip` would see — a difference in reply framing that is not a
//     difference in what was asked.
//
// Both netlink options degrade rather than refuse: a kernel predating
// NETLINK_GET_STRICT_CHK (pre-4.20) or NETLINK_EXT_ACK (pre-4.12) answers
// ENOPROTOOPT, and that one errno is ignored. Nothing else is. In particular SO_RCVBUF cannot fail on a socket
// this function just created — Linux *clamps* the request to net.core.rmem_max
// rather than rejecting it (net/core/sock.c, SO_RCVBUF) — so an error there is
// a bad fd, which is a bug here and not a property of the host. Reporting it is
// the point; a blanket `_ =` would have hidden it, and hidden a typo'd
// NETLINK_GET_STRICT_CHK level along with it.
func OpenNetlink() (*NetlinkSource, error) {
	return openNetlink(0)
}

// ErrBadTimeout is OpenNetlinkTimeout refusing a timeout that is not positive.
var ErrBadTimeout = errors.New("goip: netlink receive timeout must be positive")

// OpenNetlinkTimeout is OpenNetlink with SO_RCVTIMEO set to d, for callers
// that cannot afford to block forever on a request the kernel never answers.
//
// The CLI does not need it: `ip` sets no receive timeout either, and a hung
// command is the operator's to interrupt. A server does need it, because the
// goroutine stuck in recvmsg holds whatever it was admitted with, and neither
// http.TimeoutHandler nor a canceled context can reach into a blocking
// syscall to release it. A timed-out receive surfaces as EAGAIN.
//
// Zero is refused rather than treated as "no timeout", which is what
// SO_RCVTIMEO itself means by zero: a caller asking for a timeout and passing
// an unset duration should hear about it, not get OpenNetlink.
func OpenNetlinkTimeout(d time.Duration) (*NetlinkSource, error) {
	if d <= 0 {
		return nil, fmt.Errorf("%w: %v", ErrBadTimeout, d)
	}
	return openNetlink(d)
}

func openNetlink(rcvTimeout time.Duration) (*NetlinkSource, error) {
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
	// checking or extended ack, which is a degraded run, not a failed one.
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_GET_STRICT_CHK, 1); err != nil &&
		!errors.Is(err, unix.ENOPROTOOPT) {
		return nil, abandon(fmt.Errorf("goip: netlink strict check: %w", err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_EXT_ACK, 1); err != nil &&
		!errors.Is(err, unix.ENOPROTOOPT) {
		return nil, abandon(fmt.Errorf("goip: netlink extended ack: %w", err))
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 1<<20); err != nil {
		return nil, abandon(fmt.Errorf("goip: netlink SO_RCVBUF: %w", err))
	}
	if err := setRcvTimeout(fd, rcvTimeout); err != nil {
		return nil, abandon(fmt.Errorf("goip: netlink %w", err))
	}

	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, sa); err != nil {
		return nil, abandon(fmt.Errorf("goip: netlink bind: %w", err))
	}
	return &NetlinkSource{fd: fd, sa: &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, rcvTimeout: rcvTimeout}, nil
}

// setRcvTimeout sets SO_RCVTIMEO to d, or nothing for zero.
func setRcvTimeout(fd int, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	tv := unix.NsecToTimeval(d.Nanoseconds())
	// Sub-microsecond durations truncate to a zero timeval, which the
	// kernel reads as "block forever" — the opposite of what was asked.
	if tv.Sec == 0 && tv.Usec == 0 {
		tv.Usec = 1
	}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return fmt.Errorf("SO_RCVTIMEO: %w", err)
	}
	return nil
}

// Close releases the sockets.
func (s *NetlinkSource) Close() error {
	var gerr error
	if s.gopen {
		gerr = unix.Close(s.gfd)
		s.gopen = false
	}
	if s.xopen {
		gerr = errors.Join(gerr, unix.Close(s.xfd))
		s.xopen = false
	}
	if s.fd < 0 {
		return gerr
	}
	err := unix.Close(s.fd)
	s.fd = -1
	return errors.Join(err, gerr)
}

// Dump sends the request and collects every reply body of msgType. A dump
// the kernel ends with an errno in its NLMSG_DONE fails, as `ip`'s does.
func (s *NetlinkSource) Dump(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	err := xtcpnl.DumpRtnetlinkMsgsDoneErr(s.fd, request, s.sa, func(h xtcpnl.NlMsgHdr, body []byte) error {
		if h.Type != msgType {
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

// DumpMsgs sends the request and collects every reply of msgType whole.
func (s *NetlinkSource) DumpMsgs(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	err := xtcpnl.DumpRtnetlinkMsgsDoneErr(s.fd, request, s.sa, func(h xtcpnl.NlMsgHdr, body []byte) error {
		if h.Type != msgType {
			return nil
		}
		out = append(out, wholeMsg(h, body))
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
//
// The capture's NETLINK_GENERIC records are read too, segmented into the
// exchanges a genl object replays by request (source_genl.go).
type ReplaySource struct {
	cap    nlparity.Capture
	portid uint32
	// gseg is the capture's genl exchanges, and gused the ones replayed.
	gseg  nlparity.Segmentation
	gused map[int]bool
	// xseg is the capture's NETLINK_XFRM exchanges, and xused the ones
	// replayed; xmsgs are its messages, in capture order, which the
	// monitor's listen replays.
	xseg  nlparity.Segmentation
	xused map[int]bool
	xmsgs []nlparity.Msg
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
	g, err := nlparity.ParseCaptureFile(path, unix.NETLINK_GENERIC)
	if err != nil {
		return nil, err
	}
	x, err := nlparity.ParseCaptureFile(path, unix.NETLINK_XFRM)
	if err != nil {
		return nil, err
	}
	return &ReplaySource{cap: c, portid: portid, gseg: nlparity.SegmentCapture(g),
		xseg: nlparity.SegmentCapture(x), xmsgs: x.Msgs()}, nil
}

// NewReplayFromBytes builds a replay source from pcap bytes already in memory.
func NewReplayFromBytes(data []byte) (*ReplaySource, error) {
	c, err := nlparity.ParseCapture(data, uint16(unix.NETLINK_ROUTE))
	if err != nil {
		return nil, err
	}
	g, err := nlparity.ParseCapture(data, unix.NETLINK_GENERIC)
	if err != nil {
		return nil, err
	}
	x, err := nlparity.ParseCapture(data, unix.NETLINK_XFRM)
	if err != nil {
		return nil, err
	}
	return &ReplaySource{cap: c, gseg: nlparity.SegmentCapture(g), xseg: nlparity.SegmentCapture(x),
		xmsgs: x.Msgs()}, nil
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
	if err := s.doneErr(); err != nil {
		return nil, err
	}
	if len(out) == 0 && !s.recordedRequest(request) {
		return nil, fmt.Errorf("%w: type %d", ErrNoReplay, msgType)
	}
	return out, nil
}

// doneErr is the live source's NLMSG_DONE rule over the capture: a recorded
// DONE carrying an errno, for this portid, fails the dump with it.
func (s *ReplaySource) doneErr() error {
	for _, m := range s.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != unix.NLMSG_DONE {
			continue
		}
		if s.portid != 0 && m.Hdr.Pid != s.portid {
			continue
		}
		if err := xtcpnl.DoneErr(m.Body); err != nil {
			return fmt.Errorf("goip: dump: %w", err)
		}
	}
	return nil
}

// DumpMsgs is Dump with each reply whole, its recorded header included, by
// the same type, portid and recorded-request rules.
func (s *ReplaySource) DumpMsgs(request []byte, msgType uint16) ([][]byte, error) {
	var out [][]byte
	for _, m := range s.cap.Msgs() {
		if m.IsRequest() || m.Hdr.Type != msgType {
			continue
		}
		if s.portid != 0 && m.Hdr.Pid != s.portid {
			continue
		}
		out = append(out, wholeMsg(m.Hdr, m.Body))
	}
	if err := s.doneErr(); err != nil {
		return nil, err
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

// nowFromEnv is the wall clock a run renders timestamps against. GOIP_NOW (an
// RFC3339 instant) exists for the same reason GOIP_REPLAY does — to reproduce a
// captured timestamp without runtime pcap parsing, the argv staying identical to
// `ip`'s. Unset or unparseable yields time.Now, the production clock: only the
// ntable `-s` config block reads this, and failing a command over a typo in a
// debugging variable would be worse than using the real time.
func nowFromEnv() time.Time {
	if t, err := time.Parse(time.RFC3339, os.Getenv("GOIP_NOW")); err == nil {
		return t
	}
	return time.Now()
}

// nowFuncFromEnv is the clock a monitor stamps each message with: GOIP_NOW's
// instant when it parses, for a replay, else the wall clock, read per message.
func nowFuncFromEnv() func() time.Time {
	if t, err := time.Parse(time.RFC3339, os.Getenv("GOIP_NOW")); err == nil {
		return func() time.Time { return t }
	}
	return time.Now
}
