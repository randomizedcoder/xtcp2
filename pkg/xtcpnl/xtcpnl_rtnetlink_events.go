package xtcpnl

// This file adds the rtnetlink *event* layer on top of the DUMP machinery in
// xtcpnl_rtnetlink.go.
//
// A dump and an event carry the identical body: RTM_DELLINK has the same
// ifinfomsg + IFLA_* layout as RTM_NEWLINK, RTM_DELROUTE the same rtmsg + RTA_*
// as RTM_NEWROUTE, and so on. The only thing the event layer adds is the
// add-vs-remove discriminator that nlmsg_type carries, which the dump path had
// no need for because a dump only ever replies RTM_NEW*.
//
// Where the two genuinely differ is the transport, not the parsing: kernel
// notifications arrive unsolicited on a socket that has joined the relevant
// RTNLGRP_* multicast groups, answer no request, and are never terminated by
// NLMSG_DONE. DumpRtnetlink therefore cannot drive them — it sends first,
// filters replies on the request's nlmsg_seq, and stops at DONE. The listener
// is tracked as TODO-SOON.md §13; this file is the pure parsing half, which is
// what the pcap fixtures exercise.
//
// Telling a notification apart from a request or a dump reply is
// IsRtnetlinkNotification below, and it is not the one-liner it looks like —
// read its comment before reaching for nlmsg_pid/nlmsg_seq.

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// EventAction is the add-or-remove sense of an rtnetlink event, decoded from
// nlmsg_type (RTM_NEW* vs RTM_DEL*).
type EventAction uint8

const (
	EventActionUnknown EventAction = iota
	EventActionAdd                 // RTM_NEW*
	EventActionDel                 // RTM_DEL*
)

// String renders an EventAction for logs and test failure messages.
func (a EventAction) String() string {
	switch a {
	case EventActionAdd:
		return "add"
	case EventActionDel:
		return "del"
	case EventActionUnknown:
		return "unknown"
	default:
		return fmt.Sprintf("EventAction(%d)", uint8(a))
	}
}

// The four event records. Each pairs the action with the same *Info struct the
// dump path already produces, so a consumer that handles dumps needs no new
// decoding to handle events.
type (
	LinkEvent struct {
		Action EventAction
		Link   LinkInfo
	}
	AddrEvent struct {
		Action EventAction
		Addr   AddrInfo
	}
	RouteEvent struct {
		Action EventAction
		Route  RouteInfo
	}
	NeighEvent struct {
		Action EventAction
		Neigh  NeighInfo
	}
)

// ErrNotAnEvent indicates an nlmsg_type outside the link/addr/route/neigh
// add-or-delete set this package decodes. Control messages (NLMSG_DONE,
// NLMSG_ERROR, NLMSG_NOOP) and every other RTM_* land here.
var ErrNotAnEvent = errors.New("xtcpnl: netlink message type is not a link/addr/route/neigh event")

// IsRtnetlinkNotification reports whether a message is an unsolicited kernel
// notification of one of the four families, as opposed to a request or a dump
// reply. Use it to filter a mixed stream — a capture, or a socket that both
// dumps and listens — before calling ParseRtnetlinkEvent.
//
// The test is on nlmsg_flags, and the reason matters because the obvious
// alternative is wrong:
//
//	NLM_F_REQUEST  set   -> a request. Never a notification.
//	NLM_F_MULTI    set   -> part of a multipart DUMP reply, not an event.
//	neither              -> an unsolicited notification.
//
// It is tempting to test `nlmsg_pid == 0 && nlmsg_seq == 0` instead, on the
// reasoning that a notification answers no request. That is wrong in both
// directions, and a real capture shows it:
//
//   - When a change is made from userspace, the kernel echoes the ORIGINATING
//     port and sequence into the notification (rtnl_notify passes the
//     requester's portid and nlmsghdr through). So a notification for
//     `ip route add` carries pid=725, seq=1790381641 — not zeros — and a
//     pid/seq filter silently drops exactly the events an operator caused.
//   - Meanwhile `ip` sends its requests on an unbound socket, so the request's
//     own header reads pid=0, seq=<n>, and with seq ignored a pid filter
//     admits the requests it was meant to exclude.
//
// Only kernel-internal changes (carrier transitions, autoconfigured routes,
// the neighbor state machine) carry pid=0 and seq=0, which is why the mistake
// survives casual testing against link events alone.
func IsRtnetlinkNotification(h NlMsgHdr) bool {
	if !IsRtnetlinkEventType(h.Type) {
		return false
	}
	return h.Flags&(unix.NLM_F_REQUEST|unix.NLM_F_MULTI) == 0
}

// IsRtnetlinkEventType reports whether ParseRtnetlinkEvent can decode msgType.
// It classifies the nlmsg_type only; it says nothing about whether the message
// is a request, a dump reply or a notification — use IsRtnetlinkNotification
// for that.
func IsRtnetlinkEventType(msgType uint16) bool {
	switch msgType {
	case uint16(unix.RTM_NEWLINK), uint16(unix.RTM_DELLINK),
		uint16(unix.RTM_NEWADDR), uint16(unix.RTM_DELADDR),
		uint16(unix.RTM_NEWROUTE), uint16(unix.RTM_DELROUTE),
		uint16(unix.RTM_NEWNEIGH), uint16(unix.RTM_DELNEIGH):
		return true
	default:
		return false
	}
}

// ParseRtnetlinkEvent decodes one rtnetlink message body (the bytes after the
// 16-byte nlmsghdr) into a LinkEvent, AddrEvent, RouteEvent or NeighEvent,
// dispatching on nlmsg_type.
//
// It returns ErrNotAnEvent for any other type, so a caller can walk a mixed
// stream and skip what it does not handle:
//
//	ev, err := ParseRtnetlinkEvent(h.Type, body)
//	if errors.Is(err, ErrNotAnEvent) {
//		continue
//	}
//
// The returned value is one of the four concrete event types, never a pointer,
// and is nil whenever err is non-nil — the helpers below return an untyped nil
// rather than a boxed zero-value struct, so `if ev != nil` is a sound check.
func ParseRtnetlinkEvent(msgType uint16, body []byte) (any, error) {
	switch msgType {
	case uint16(unix.RTM_NEWLINK):
		return parseLinkEvent(EventActionAdd, body)
	case uint16(unix.RTM_DELLINK):
		return parseLinkEvent(EventActionDel, body)

	case uint16(unix.RTM_NEWADDR):
		return parseAddrEvent(EventActionAdd, body)
	case uint16(unix.RTM_DELADDR):
		return parseAddrEvent(EventActionDel, body)

	case uint16(unix.RTM_NEWROUTE):
		return parseRouteEvent(EventActionAdd, body)
	case uint16(unix.RTM_DELROUTE):
		return parseRouteEvent(EventActionDel, body)

	case uint16(unix.RTM_NEWNEIGH):
		return parseNeighEvent(EventActionAdd, body)
	case uint16(unix.RTM_DELNEIGH):
		return parseNeighEvent(EventActionDel, body)

	default:
		return nil, fmt.Errorf("%w: nlmsg_type %d", ErrNotAnEvent, msgType)
	}
}

// The four helpers return `any` rather than their concrete event type on
// purpose. Returning `(LinkEvent, error)` and letting the caller widen it would
// box a zero-value LinkEvent into a non-nil interface on the error path, which
// is the classic typed-nil trap: ParseRtnetlinkEvent would hand back an
// apparently valid empty event alongside the error.

func parseLinkEvent(action EventAction, body []byte) (any, error) {
	li, err := ParseNewLink(body)
	if err != nil {
		return nil, err
	}
	return LinkEvent{Action: action, Link: li}, nil
}

func parseAddrEvent(action EventAction, body []byte) (any, error) {
	ai, err := ParseNewAddr(body)
	if err != nil {
		return nil, err
	}
	return AddrEvent{Action: action, Addr: ai}, nil
}

func parseRouteEvent(action EventAction, body []byte) (any, error) {
	ri, err := ParseNewRoute(body)
	if err != nil {
		return nil, err
	}
	return RouteEvent{Action: action, Route: ri}, nil
}

func parseNeighEvent(action EventAction, body []byte) (any, error) {
	ni, err := ParseNeigh(body)
	if err != nil {
		return nil, err
	}
	return NeighEvent{Action: action, Neigh: ni}, nil
}
