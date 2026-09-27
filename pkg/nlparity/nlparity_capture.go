package nlparity

import (
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Capture is a whole nlmon pcap reduced to the datagrams of one netlink family,
// plus a census of what was left out.
//
// The census is not bookkeeping for its own sake. nlmon captures the *whole
// host*, not one process, and the committed fixtures prove how dirty that gets:
// pkg/xtcpnl/testdata/7_1_8/netlink_route_getaddr.pcap holds 251 messages
// across six distinct netlink port ids and seventeen sequence numbers — two
// separate `ip` runs, 143 messages from an unrelated process, and 20-byte
// RTM_GETADDRs built on rtgenmsg, which iproute2 never sends. A comparator that
// silently drops what it cannot attribute compares noise and reports green, so
// every exclusion is counted and surfaced.
type Capture struct {
	// Datagrams are the datagrams of the requested family, in capture order.
	Datagrams []Datagram
	// SkippedOtherFamily counts records whose SLL protocol field named a
	// different NETLINK_* family (NETLINK_SOCK_DIAG, NETLINK_KOBJECT_UEVENT and
	// so on). Expected to be non-zero on a busy host.
	SkippedOtherFamily int
	// SkippedShortRecord counts records too short for the 16-byte SLL cooked
	// header.
	SkippedShortRecord int
	// SkippedBadDatagram counts datagrams of the right family that the walker
	// rejected outright — ErrShortDatagram or ErrBadHead. Distinct from the two
	// above because this one means a netlink datagram was malformed, which is
	// worth a loud report rather than a shrug.
	SkippedBadDatagram int
}

// Msgs flattens the capture into every message of every datagram, in capture
// order, which is the order the segmenter needs.
func (c Capture) Msgs() []Msg {
	var out []Msg
	for _, d := range c.Datagrams {
		out = append(out, d.Msgs...)
	}
	return out
}

// Requests returns just the messages with NLM_F_REQUEST set.
func (c Capture) Requests() []Msg {
	var out []Msg
	for _, m := range c.Msgs() {
		if m.IsRequest() {
			out = append(out, m)
		}
	}
	return out
}

// ParseCapture reads a DLT_NETLINK pcap and walks every record of the given
// netlink family (unix.NETLINK_ROUTE for everything goip does).
//
// A capture recorded from the wrong interface fails here rather than yielding
// nonsense families, because xtcpnl.ParseNetlinkPcap asserts the link type is
// 253. That check is the reason this reuses xtcpnl's pcap reader instead of
// slicing at a fixed offset the way the older fixture tests do.
func ParseCapture(data []byte, family uint16) (Capture, error) {
	var c Capture

	_, records, err := xtcpnl.ParseNetlinkPcap(data)
	if err != nil {
		return c, fmt.Errorf("nlparity: %w", err)
	}

	for _, rec := range records {
		fam, body, perr := rec.NetlinkPayload()
		if perr != nil {
			c.SkippedShortRecord++
			continue
		}
		if fam != family {
			c.SkippedOtherFamily++
			continue
		}

		d, werr := WalkDatagram(fam, body)
		if werr != nil {
			c.SkippedBadDatagram++
			continue
		}
		c.Datagrams = append(c.Datagrams, d)
	}

	return c, nil
}

// ParseRouteCaptureFile reads a pcap from disk and walks its NETLINK_ROUTE
// datagrams. It is the one convenience wrapper; everything else in this package
// takes bytes so it stays testable without a filesystem.
func ParseRouteCaptureFile(path string) (Capture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Capture{}, fmt.Errorf("nlparity: read %s: %w", path, err)
	}
	c, err := ParseCapture(data, uint16(unix.NETLINK_ROUTE))
	if err != nil {
		return c, fmt.Errorf("nlparity: parse %s: %w", path, err)
	}
	return c, nil
}

// IsBadCapture reports whether err is one of the walker's structural errors, so
// a caller can tell "this file is not a usable capture" from "this file is not
// there".
func IsBadCapture(err error) bool {
	return errors.Is(err, ErrBadHead) || errors.Is(err, ErrShortDatagram)
}
