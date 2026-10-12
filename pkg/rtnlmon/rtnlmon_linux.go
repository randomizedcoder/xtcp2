package rtnlmon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// Group is an rtnetlink multicast group number (RTNLGRP_*, not an RTMGRP_*
// bit mask). Run accepts the six whose notifications xtcpnl decodes.
type Group uint32

const (
	GroupLink      Group = unix.RTNLGRP_LINK
	GroupIPv4Addr  Group = unix.RTNLGRP_IPV4_IFADDR
	GroupIPv6Addr  Group = unix.RTNLGRP_IPV6_IFADDR
	GroupIPv4Route Group = unix.RTNLGRP_IPV4_ROUTE
	GroupIPv6Route Group = unix.RTNLGRP_IPV6_ROUTE
	GroupNeigh     Group = unix.RTNLGRP_NEIGH
)

// Reason says why a Snapshot was taken.
type Reason uint8

const (
	// ReasonStart is the first snapshot, taken after the groups were joined.
	ReasonStart Reason = iota + 1
	// ReasonOverrun follows ENOBUFS: the kernel dropped notifications.
	ReasonOverrun
	// ReasonTruncated follows a datagram larger than the receive buffer,
	// which the kernel discarded; the buffer is enlarged for the next one.
	ReasonTruncated
	// ReasonPeriodic is a refresh after Options.ResyncAfter, with nothing
	// lost: it bounds how long a change the caller's fold misses can last.
	ReasonPeriodic
)

// String renders a Reason for logs and the probe's output.
func (r Reason) String() string {
	switch r {
	case ReasonStart:
		return "start"
	case ReasonOverrun:
		return "overrun"
	case ReasonTruncated:
		return "truncated"
	case ReasonPeriodic:
		return "periodic"
	default:
		return fmt.Sprintf("Reason(%d)", uint8(r))
	}
}

// Snapshot is the current content of the tables the joined groups cover. A
// table no group covers is nil.
type Snapshot struct {
	Links  []xtcpnl.LinkInfo
	Addrs  []xtcpnl.AddrInfo
	Routes []xtcpnl.RouteInfo
	Neighs []xtcpnl.NeighInfo
}

// Handler receives Run's output, on Run's goroutine. Its error ends Run.
type Handler interface {
	// Snapshot replaces the caller's state.
	Snapshot(s Snapshot, reason Reason) error
	// Event is an xtcpnl.LinkEvent, AddrEvent, RouteEvent or NeighEvent,
	// to apply to the state the last Snapshot set.
	Event(ev any) error
}

// Options configure Run.
type Options struct {
	// Groups to join; at least one, each one of the six Group constants.
	Groups []Group
	// RcvBuf sizes the listening socket's receive buffer; zero keeps the
	// kernel default. See nlconn.Options.
	RcvBuf int
	// DumpTimeout bounds each receive of a snapshot's dumps; zero is
	// DefaultDumpTimeout.
	DumpTimeout time.Duration
	// MaxDumpRetries is how often a snapshot is retaken after the kernel
	// marks a dump NLM_F_DUMP_INTR; zero is DefaultMaxDumpRetries.
	MaxDumpRetries int
	// ResyncAfter, when positive, retakes the snapshot (ReasonPeriodic)
	// once the last one is at least this old. It is checked after each
	// received datagram has been delivered, not on a timer: Receive blocks
	// until the kernel sends something, so an idle socket is not
	// refreshed. Zero never refreshes.
	ResyncAfter time.Duration
}

const (
	DefaultDumpTimeout    = 2 * time.Second
	DefaultMaxDumpRetries = 3
)

var (
	// ErrNoGroups is Run's error when Options.Groups is empty.
	ErrNoGroups = errors.New("rtnlmon: no groups to join")
	// ErrUnknownGroup is Run's error for a group outside the six.
	ErrUnknownGroup = errors.New("rtnlmon: unsupported group")
)

// Run joins opts.Groups, delivers a Snapshot(ReasonStart), then each
// notification, until ctx ends or an error. It returns ctx's error when ctx
// ended it; any other error (a failed dump, a Handler's refusal, a message it
// cannot decode) is returned wrapped, and the caller restarts Run.
//
// The groups are joined before the first dump, so no change can fall between
// the snapshot and the stream after it: a change during the dump is both in
// the dump (or not yet) and queued as a notification, and applying it again
// is idempotent.
//
// An overrun is where order matters. When a broadcast does not fit the
// receive buffer the kernel (net/netlink/af_netlink.c):
//   - sets sk_err to ENOBUFS once and marks the socket congested
//     (netlink_overrun, :351);
//   - while congested, drops every further broadcast to it
//     (netlink_broadcast_deliver, :1416);
//   - clears the congestion only when the receive queue is empty
//     (netlink_rcv_wake, :363).
//
// And the ENOBUFS is reported before anything still queued is dequeued
// (__skb_try_recv_datagram, net/core/datagram.c:252). So what is still queued
// at the ENOBUFS predates the dropped notifications, and applying it after a
// new snapshot would replay stale state: a queued add whose delete was
// dropped would resurrect the object. Run therefore drains the queue, which
// also lifts the congestion, then dumps, then resumes. Every notification
// after the drain is gap-free.
func Run(ctx context.Context, opts Options, h Handler) (err error) {
	if h == nil {
		return errors.New("rtnlmon: nil Handler")
	}
	plan, err := planDumps(opts.Groups)
	if err != nil {
		return err
	}
	groups := make([]uint32, len(opts.Groups))
	for i, g := range opts.Groups {
		groups[i] = uint32(g)
	}
	conn, err := nlconn.OpenWithOptions(ctx, unix.NETLINK_ROUTE, groups, nlconn.Options{RcvBuf: opts.RcvBuf})
	if err != nil {
		return fmt.Errorf("rtnlmon: open: %w", err)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	timeout := opts.DumpTimeout
	if timeout <= 0 {
		timeout = DefaultDumpTimeout
	}
	dump := func(context.Context) (Snapshot, error) { return dumpSnapshot(plan, timeout) }
	retries := opts.MaxDumpRetries
	if retries <= 0 {
		retries = DefaultMaxDumpRetries
	}
	return run(ctx, conn, dump, runConfig{retries: retries, resyncAfter: opts.ResyncAfter, now: time.Now}, h)
}

// runConfig is run's tuning; now is the clock the periodic resync reads.
type runConfig struct {
	retries     int
	resyncAfter time.Duration
	now         func() time.Time
}

// eventConn is the part of nlconn.Conn run uses.
type eventConn interface {
	Receive(ctx context.Context, visit func([]byte) error) (int, error)
	Drain(ctx context.Context) (int, error)
}

type dumpFunc func(ctx context.Context) (Snapshot, error)

func run(ctx context.Context, conn eventConn, dump dumpFunc, cfg runConfig, h Handler) error {
	if err := resync(ctx, dump, cfg.retries, h, ReasonStart); err != nil {
		return err
	}
	last := cfg.now()
	deliver := func(data []byte) error {
		return xtcpnl.WalkNotifications(data, func(hdr xtcpnl.NlMsgHdr, body []byte) error {
			ev, err := xtcpnl.ParseRtnetlinkEvent(hdr.Type, body)
			if err != nil {
				return fmt.Errorf("rtnlmon: decode nlmsg_type %d: %w", hdr.Type, err)
			}
			return h.Event(ev)
		})
	}
	for {
		_, err := conn.Receive(ctx, deliver)
		var reason Reason
		switch {
		case err == nil:
			if cfg.resyncAfter <= 0 || cfg.now().Sub(last) < cfg.resyncAfter {
				continue
			}
			// The stream is gap-free, so there is nothing to drain.
			reason = ReasonPeriodic
		case ctx.Err() != nil:
			return ctx.Err()
		case errors.Is(err, unix.ENOBUFS):
			reason = ReasonOverrun
		case errors.Is(err, nlconn.ErrTruncated):
			reason = ReasonTruncated
		default:
			return fmt.Errorf("rtnlmon: receive: %w", err)
		}
		if reason != ReasonPeriodic {
			if _, err := conn.Drain(ctx); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return fmt.Errorf("rtnlmon: drain after %s: %w", reason, err)
			}
		}
		if err := resync(ctx, dump, cfg.retries, h, reason); err != nil {
			return err
		}
		last = cfg.now()
	}
}

// resync takes a snapshot, retaking it while the kernel reports an
// interrupted dump, and hands it to h.
func resync(ctx context.Context, dump dumpFunc, retries int, h Handler, reason Reason) error {
	for attempt := 0; ; attempt++ {
		s, err := dump(ctx)
		if errors.Is(err, xtcpnl.ErrDumpInterrupted) && attempt < retries {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("rtnlmon: %s snapshot: %w", reason, err)
		}
		if err := h.Snapshot(s, reason); err != nil {
			return fmt.Errorf("rtnlmon: %s snapshot refused: %w", reason, err)
		}
		return nil
	}
}

// dumpReq is one dump a snapshot needs: an RTM_GET* type and its family.
type dumpReq struct {
	msgType uint16
	family  uint8
}

// planDumps returns the dumps the groups' tables need, in a fixed order:
// links, addresses, routes, neighbors. Both address (or route) groups are one
// AF_UNSPEC dump; one alone is a dump of its family. Neighbor notifications
// carry both families, so its dump is AF_UNSPEC.
func planDumps(groups []Group) ([]dumpReq, error) {
	if len(groups) == 0 {
		return nil, ErrNoGroups
	}
	for _, g := range groups {
		switch g {
		case GroupLink, GroupIPv4Addr, GroupIPv6Addr, GroupIPv4Route, GroupIPv6Route, GroupNeigh:
		default:
			return nil, fmt.Errorf("%w: %d", ErrUnknownGroup, g)
		}
	}
	has := func(g Group) bool { return slices.Contains(groups, g) }
	family := func(v4, v6 Group) uint8 {
		switch {
		case has(v4) && has(v6):
			return unix.AF_UNSPEC
		case has(v4):
			return unix.AF_INET
		default:
			return unix.AF_INET6
		}
	}
	var plan []dumpReq
	if has(GroupLink) {
		plan = append(plan, dumpReq{unix.RTM_GETLINK, unix.AF_UNSPEC})
	}
	if has(GroupIPv4Addr) || has(GroupIPv6Addr) {
		plan = append(plan, dumpReq{unix.RTM_GETADDR, family(GroupIPv4Addr, GroupIPv6Addr)})
	}
	if has(GroupIPv4Route) || has(GroupIPv6Route) {
		plan = append(plan, dumpReq{unix.RTM_GETROUTE, family(GroupIPv4Route, GroupIPv6Route)})
	}
	if has(GroupNeigh) {
		plan = append(plan, dumpReq{unix.RTM_GETNEIGH, unix.AF_UNSPEC})
	}
	return plan, nil
}

// request builds r's dump request with xtcpnl's builders.
func (r dumpReq) request(seq uint32) []byte {
	switch r.msgType {
	case unix.RTM_GETLINK:
		return xtcpnl.BuildDumpLinkRequest(seq)
	case unix.RTM_GETADDR:
		return xtcpnl.BuildDumpAddrRequest(r.family, seq)
	case unix.RTM_GETROUTE:
		return xtcpnl.BuildDumpRouteRequest(r.family, seq)
	default:
		return xtcpnl.BuildDumpNeighRequest(r.family, seq)
	}
}

// collect adds one dump reply to s.
func collect(s *Snapshot, msgType uint16, body []byte) error {
	if !xtcpnl.IsRtnetlinkEventType(msgType) {
		return nil
	}
	ev, err := xtcpnl.ParseRtnetlinkEvent(msgType, body)
	if err != nil {
		return fmt.Errorf("decode dump nlmsg_type %d: %w", msgType, err)
	}
	switch e := ev.(type) {
	case xtcpnl.LinkEvent:
		s.Links = append(s.Links, e.Link)
	case xtcpnl.AddrEvent:
		s.Addrs = append(s.Addrs, e.Addr)
	case xtcpnl.RouteEvent:
		s.Routes = append(s.Routes, e.Route)
	case xtcpnl.NeighEvent:
		s.Neighs = append(s.Neighs, e.Neigh)
	}
	return nil
}

// dumpSnapshot runs plan's dumps on one blocking socket of its own, so dump
// replies never interleave with the multicast stream, in the calling thread's
// network namespace.
func dumpSnapshot(plan []dumpReq, timeout time.Duration) (s Snapshot, err error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	sa := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	if err := unix.Bind(fd, sa); err != nil {
		return Snapshot{}, err
	}
	tv := unix.NsecToTimeval(timeout.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return Snapshot{}, err
	}
	for i, r := range plan {
		if err := xtcpnl.DumpRtnetlink(fd, r.request(uint32(i+1)), sa, func(mt uint16, body []byte) error {
			return collect(&s, mt, body)
		}); err != nil {
			return Snapshot{}, err
		}
	}
	return s, nil
}
