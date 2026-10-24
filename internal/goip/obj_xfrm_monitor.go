package goip

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip xfrm monitor` is do_xfrm_monitor (ip/xfrm_monitor.c): its words,
// then either the messages of a file, rtnl_from_file's, or a NETLINK_XFRM
// socket of its own in the groups the words name, every group by default,
// each message xfrm_accept_msg prints until the listen ends.
//
// xfrm_accept_msg prints an SA event as xfrm_state_print does, keys and
// all, whatever nokeys says, and a policy event as xfrm_policy_print, with
// no filter; an acquire, a flush, an async event, a NAT-T mapping change
// and the default policies with printers of their own; NLMSG_ERROR,
// NOOP and DONE with nothing; any other type as "Unknown message". A
// printer's refusal is its message on stderr, and the listen goes on, as
// xfrm_accept_msg returns 0 whatever its printer returned. REPORT, which
// only Mobile IPv6 route optimization sends, is not implemented.

const (
	xfrmMonitorUsageCst = "Usage: ip xfrm monitor [ nokeys ] [ all-nsid ] [ all | OBJECTS | help ]\n" +
		"OBJECTS := { acquire | expire | SA | aevent | policy | report }"
	// xfrmMonitorAllCst is do_xfrm_monitor's groups when no object is
	// named: every bit.
	xfrmMonitorAllCst = ^uint32(0)
)

// xfrmMonitorObjects are do_xfrm_monitor's object words, in its order, by
// prefix, each its XFRMNLGRP_*.
var xfrmMonitorObjects = []struct {
	word  string
	group uint32
}{
	{"acquire", 1}, {"expire", 2}, {"SA", 3}, {"aevent", 5}, {"policy", 4}, {"report", 6},
}

// xfrmMonitorArgs is what do_xfrm_monitor's loop reads.
type xfrmMonitorArgs struct {
	file             string
	hasFile, allNsid bool
	groups           uint32
}

// parseXfrmMonitorArgs is do_xfrm_monitor's loop: file by prefix and its
// value, nokeys and all by strcmp, all-nsid, then each object by prefix,
// in that order, so `a` is all-nsid; help is the usage, exit 255, and any
// other word "unknown", exit 255. An object's group replaces the default
// of every group. nokeys is read and changes nothing an event prints.
func parseXfrmMonitorArgs(args []string) (xfrmMonitorArgs, error) {
	a := xfrmMonitorArgs{groups: xfrmMonitorAllCst}
	objects := uint32(0)
	x := &xfrmArgs{args: args}
	for ; x.i < len(x.args); x.i++ {
		w := x.cur()
		switch {
		case matchesPrefix(w, monitorFileCst):
			if err := x.next(); err != nil {
				return a, err
			}
			a.file, a.hasFile = x.cur(), true
		case w == "nokeys", w == monitorAllCst:
		case matchesPrefix(w, monitorAllNsidCst):
			a.allNsid = true
		case matchesPrefix(w, xfrmHelpCst):
			return a, fmt.Errorf("%w: %s", ErrXfrmUsage, xfrmMonitorUsageCst)
		default:
			g, ok := xfrmMonitorGroup(w)
			if !ok {
				return a, fmt.Errorf("%w: Argument %q is unknown, try \"ip xfrm monitor help\"", ErrXfrmUsage, w)
			}
			objects |= 1 << (g - 1)
		}
	}
	if objects != 0 {
		a.groups = objects
	}
	return a, nil
}

// xfrmMonitorGroup is an object word's group, by prefix in
// do_xfrm_monitor's order, ahead of help.
func xfrmMonitorGroup(w string) (uint32, bool) {
	for _, o := range xfrmMonitorObjects {
		if matchesPrefix(w, o.word) {
			return o.group, true
		}
	}
	return 0, false
}

// xfrmMon is one monitor run: its words and the clock -t reads.
type xfrmMon struct {
	c   *runCtx
	a   xfrmMonitorArgs
	now func() time.Time
}

// xfrmMonitorNow is -t's clock, a variable so a test can fix it.
var xfrmMonitorNow = time.Now

// xfrmMonitor is do_xfrm_monitor.
func xfrmMonitor(c *runCtx, args []string) error {
	a, err := parseXfrmMonitorArgs(args)
	if err != nil {
		return err
	}
	m := &xfrmMon{c: c, a: a, now: xfrmMonitorNow}
	if a.hasFile {
		return monitorReadFile(a.file, m.accept)
	}
	xs, ok := c.src.(XfrmMonitorSource)
	if !ok {
		return fmt.Errorf("xfrm monitor: %w", ErrNotImplemented)
	}
	return xs.XfrmListen(a.groups, a.allNsid, c.errOut, m.accept)
}

// accept is xfrm_accept_msg: the timestamp under -t, the sender's netns
// under all-nsid, then the message's printer by type. A printer's refusal
// goes to stderr and the listen goes on; one goip cannot print ends it.
func (m *xfrmMon) accept(msg monitorMsg) error {
	var head string
	if m.c.timestamp > 0 {
		head = timestampText(m.now(), m.c.timestampShort > 0)
	}
	if m.a.allNsid {
		if msg.nsid < 0 {
			head += "[nsid current]"
		} else {
			head += fmt.Sprintf("[nsid %d]", msg.nsid)
		}
	}
	if _, err := io.WriteString(m.c.out, head); err != nil {
		return err
	}
	err := m.print(xfrmMsg{typ: msg.hdr.Type, flags: msg.hdr.Flags, body: msg.body})
	if err == nil || errors.Is(err, ErrNotImplemented) {
		return err
	}
	_, werr := fmt.Fprintln(m.c.errOut, xfrmRefusalText(err))
	return werr
}

// xfrmRefusalText is a printer's refusal as ip's fprintf has it, without
// the sentinel goip wraps it in.
func xfrmRefusalText(err error) string {
	return strings.TrimPrefix(err.Error(), ErrXfrmRefused.Error()+": ")
}

// print is xfrm_accept_msg's switch.
func (m *xfrmMon) print(x xfrmMsg) error {
	switch x.typ {
	case xtcpnl.XfrmMsgNewSA, xtcpnl.XfrmMsgDelSA, xtcpnl.XfrmMsgUpdSA, xtcpnl.XfrmMsgExpire:
		return xfrmStatePrint(m.c, &xfrmFilter{}, false, x)
	case xtcpnl.XfrmMsgNewPolicy, xtcpnl.XfrmMsgDelPolicy, xtcpnl.XfrmMsgUpdPolicy, xtcpnl.XfrmMsgPolExpire:
		return xfrmPolicyPrint(m.c, &xfrmPolicyFilter{}, x)
	case xtcpnl.XfrmMsgAcquire:
		return m.acquire(x)
	case xtcpnl.XfrmMsgFlushSA:
		if len(x.body) < nlmsgAlign(xtcpnl.XfrmUsersaFlushSizeCst) {
			return fmt.Errorf("BUG: wrong nlmsg len %d", len(x.body)-nlmsgAlign(xtcpnl.XfrmUsersaFlushSizeCst))
		}
		return m.write(render.XfrmFlushSA(x.body[0]))
	case xtcpnl.XfrmMsgFlushPolicy:
		p, err := xtcpnl.ParseXfrmPolicy(xtcpnl.NlMsgHdr{Type: x.typ}, x.body)
		if err != nil {
			return err
		}
		return m.write(render.XfrmFlushPolicy(p.Attrs.PolicyType))
	case xtcpnl.XfrmMsgReport:
		return fmt.Errorf("xfrm monitor: a REPORT: %w", ErrNotImplemented)
	case xtcpnl.XfrmMsgNewAE:
		return m.aevent(x)
	case xtcpnl.XfrmMsgMapping:
		return m.mapping(x)
	case xtcpnl.XfrmMsgGetDefault:
		if n := xtcpnl.NlMsgHdrSizeCst + len(x.body); n < xfrmDefaultMinCst {
			return fmt.Errorf("BUG: short nlmsg len %d (expect %d) for XFRM_MSG_GETDEFAULT", n, xfrmDefaultMinCst)
		}
		return m.write(render.XfrmPolicyDefault(&xtcpnl.XfrmUserpolicyDefault{In: x.body[0], Fwd: x.body[1],
			Out: x.body[2]}))
	case unix.NLMSG_ERROR, unix.NLMSG_NOOP, unix.NLMSG_DONE:
		return nil
	}
	return m.write(fmt.Sprintf("Unknown message: %08d 0x%08x 0x%08x\n", xtcpnl.NlMsgHdrSizeCst+len(x.body), x.typ,
		x.flags))
}

// write is one printer's text to stdout.
func (m *xfrmMon) write(s string) error {
	_, err := io.WriteString(m.c.out, s)
	return err
}

// acquire is xfrm_acquire_print: a body short of its struct is refused.
func (m *xfrmMon) acquire(x xfrmMsg) error {
	if len(x.body) < xtcpnl.XfrmUserAcquireSizeCst {
		return fmt.Errorf("BUG: wrong nlmsg len %d", len(x.body)-xtcpnl.XfrmUserAcquireSizeCst)
	}
	e, err := xtcpnl.ParseXfrmEvent(xtcpnl.NlMsgHdr{Type: x.typ}, x.body)
	if err != nil {
		return err
	}
	o := render.XfrmOpt{Stats: m.c.stats(), Family: m.c.family,
		IndexName: func(idx int32) string { return genlIndexToName(m.c, idx) }}
	if err := xfrmProtoOpt(&o, e.Acquire.Sel.Proto, e.Acquire.Policy.Sel.Proto); err != nil {
		return err
	}
	return m.write(render.XfrmAcquire(e.Acquire, &e.Attrs, &o))
}

// aevent is xfrm_ae_print, which reads the struct with no length check;
// goip refuses a body short of it.
func (m *xfrmMon) aevent(x xfrmMsg) error {
	s, err := xtcpnl.ParseXfrmState(xtcpnl.NlMsgHdr{Type: x.typ}, x.body)
	if err != nil {
		return fmt.Errorf("an async event: %w: %w", err, ErrNotImplemented)
	}
	name, err := xfrmProtoName(s.AEvent.SAID.Proto)
	if err != nil {
		return err
	}
	return m.write(render.XfrmAevent(s.AEvent, name))
}

// mapping is xfrm_mapping_print, which reads the struct with no length
// check; goip refuses a body short of it.
func (m *xfrmMon) mapping(x xfrmMsg) error {
	e, err := xtcpnl.ParseXfrmEvent(xtcpnl.NlMsgHdr{Type: x.typ}, x.body)
	if err != nil {
		return fmt.Errorf("a mapping change: %w: %w", err, ErrNotImplemented)
	}
	name, err := xfrmProtoName(e.Mapping.ID.Proto)
	if err != nil {
		return err
	}
	return m.write(render.XfrmMapping(e.Mapping, name))
}
