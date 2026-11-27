package xtcpnl

import "golang.org/x/sys/unix"

// This file names the tc object a `tc monitor` event is about, as
// accept_tcmsg (iproute2 tc/tc_monitor.c) dispatches it to print_filter,
// print_class, print_qdisc or print_action.

// TcObj is the tc object an rtnetlink message is about.
type TcObj uint8

const (
	// TcObjNone is not a tc event: tc's monitor prints `Unknown message`
	// for it, or nothing for NLMSG_ERROR, NLMSG_NOOP and NLMSG_DONE.
	TcObjNone TcObj = iota
	TcObjQdisc
	TcObjClass
	TcObjFilter
	TcObjChain
	TcObjAction
)

var tcObjNames = [...]string{"not tc", "qdisc", "class", "filter", "chain", "action"}

func (o TcObj) String() string {
	if int(o) < len(tcObjNames) {
		return tcObjNames[o]
	}
	return "TcObj(?)"
}

// TcEventKind is the tc object of a monitor event of type typ, and whether
// the event is a delete. It follows accept_tcmsg, not the uapi families of
// four: of the GET types only RTM_GETACTION is an event there (it reaches
// print_action), while RTM_GETQDISC, RTM_GETTCLASS, RTM_GETTFILTER and
// RTM_GETCHAIN fall through to `Unknown message`. A chain goes to
// print_filter in tc; it is kept apart here so a caller can tell.
//
// Values: RTM_NEWQDISC 36 .. RTM_GETACTION 50, RTM_NEWCHAIN 100,
// RTM_DELCHAIN 101 (linux/include/uapi/linux/rtnetlink.h).
func TcEventKind(typ uint16) (TcObj, bool) {
	switch typ {
	case unix.RTM_NEWQDISC:
		return TcObjQdisc, false
	case unix.RTM_DELQDISC:
		return TcObjQdisc, true
	case unix.RTM_NEWTCLASS:
		return TcObjClass, false
	case unix.RTM_DELTCLASS:
		return TcObjClass, true
	case unix.RTM_NEWTFILTER:
		return TcObjFilter, false
	case unix.RTM_DELTFILTER:
		return TcObjFilter, true
	case unix.RTM_NEWCHAIN:
		return TcObjChain, false
	case unix.RTM_DELCHAIN:
		return TcObjChain, true
	case unix.RTM_NEWACTION, unix.RTM_GETACTION:
		return TcObjAction, false
	case unix.RTM_DELACTION:
		return TcObjAction, true
	}
	return TcObjNone, false
}
