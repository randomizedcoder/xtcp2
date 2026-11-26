package gotc

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/nlmon"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This is tc/tc_monitor.c: `tc monitor`, live on RTNLGRP_TC, or `tc
// monitor file F` over raw nlmsghdr records, each event handed to the
// printers the shows use. The receive loop, the file walk and the stamp
// are internal/nlmon's, shared with goip.

// rtnlgrpTCCst is RTNLGRP_TC (linux/include/uapi/linux/rtnetlink.h);
// nl_mgrp makes it the bind mask 1 << (4 - 1), 0x8.
const rtnlgrpTCCst = unix.RTNLGRP_TC

// nlMgrp is lib/libnetlink.h nl_mgrp: the bind mask of a group up to 31.
func nlMgrp(group uint32) uint32 { return 1 << (group - 1) }

// MonitorSource is a Source that can subscribe to multicast groups.
type MonitorSource interface {
	Subscribe(groups uint32, errOut io.Writer) (MonitorConn, error)
}

// MonitorConn is one subscription. Talk runs a request on it, as
// ll_init_map's dump runs on the monitor's own socket; Listen hands every
// message after to fn until fn fails or the source ends: a socket ends only
// on an error, a replay at the end of its capture.
type MonitorConn interface {
	Source
	Listen(fn func(nlmon.Msg) error) error
	Close() error
}

// monitorUsage is tc_monitor.c usage, which exits -1. Its `[` is never
// closed, and it leaves out `file` (MO1).
const monitorUsage = "Usage: tc [-timestamp [-tshort] monitor\n"

// doMonitor is do_tcmonitor.
func (r *runner) doMonitor(args []string) error {
	file, hasFile := "", false
	for len(args) > 0 {
		switch w := args[0]; {
		case matches(w, "file"):
			if len(args) < 2 {
				return r.incomplete() // NEXT_ARG
			}
			args = args[1:]
			file, hasFile = args[0], true
		case matches(w, helpWord):
			r.errf("%s", monitorUsage)
			return exitCode(-1)
		default:
			r.errf("Argument \"%s\" is unknown, try \"tc monitor help\".\n", w)
			return exitCode(-1)
		}
		args = args[1:]
	}
	if hasFile {
		return r.monitorFile(file)
	}
	return r.monitorLive()
}

// monitorFile is the file branch: fopen, then rtnl_from_file with no
// ll_init_map, so a name the cache lacks is an RTM_GETLINK per index
// (MO12). Records are printed as they are read, so a pipe is followed as
// it comes. rtnl_from_file's -1 (a read error, a truncated or malformed
// record) is do_cmd's: main exits 255, a batch line fails and -force goes
// on. Only fopen's failure exits.
func (r *runner) monitorFile(path string) error {
	err := nlmon.ReadFile(path, r.acceptTcmsg)
	var ne *nlmon.Error
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ne) && errors.Is(err, nlmon.ErrOpen):
		r.errf("%s\n", ne.Text) // perror("Cannot fopen"); exit(-1)
		return exitCode(-1)
	case errors.As(err, &ne):
		r.errf("%s\n", ne.Text)
		return retCode(-1)
	}
	return err
}

// monitorLive is rtnl_open with RTNLGRP_TC, ll_init_map on that socket,
// then rtnl_listen, whose failure exits 2.
func (r *runner) monitorLive() error {
	ms, ok := r.src.(MonitorSource)
	if !ok {
		return fmt.Errorf("%w: tc monitor on this source", errNotImplemented)
	}
	conn, err := ms.Subscribe(nlMgrp(rtnlgrpTCCst), r.stderr)
	if err != nil {
		r.errf("%v\n", err)
		return exitCode(1)
	}
	defer func() {
		if cerr := conn.Close(); cerr != nil && r.errOut == nil {
			r.errOut = cerr
		}
	}()
	main := r.src
	r.src = conn
	err = r.initMap()
	r.src = main
	if err != nil {
		return err
	}
	err = conn.Listen(func(m nlmon.Msg) error {
		if err := r.acceptTcmsg(m); err != nil {
			return err
		}
		r.o.flush() // rtnl_listen's fflush(stdout) after each message
		return nil
	})
	var ne *nlmon.Error
	switch {
	case err == nil:
		return exitCode(0)
	case errors.As(err, &ne) && (errors.Is(err, nlmon.ErrMalformed) || errors.Is(err, nlmon.ErrRemnant)):
		r.errf("%s\n", ne.Text) // exit(1) inside rtnl_listen
		return exitCode(1)
	case errors.As(err, &ne):
		r.errf("%s\n", ne.Text) // Truncated message, EOF, a receive error
		return exitCode(2)
	}
	return err
}

// acceptTcmsg is accept_tcmsg: the stamp first, whatever follows (MO2),
// then the printer by type. A chain goes to print_filter. A type that is
// none of tc's, and not ERROR, NOOP or DONE, is reported with its length
// zero-padded in decimal (MO3). The printers' returns are dropped, as
// accept_tcmsg returns 0 whatever they say.
func (r *runner) acceptTcmsg(nm nlmon.Msg) error {
	if r.timestamp > 0 {
		r.o.raw(nlmon.Timestamp(r.now(), r.tshort > 0))
	}
	m := Msg{Type: nm.Hdr.Type, Flags: nm.Hdr.Flags, Seq: nm.Hdr.Seq, PID: nm.Hdr.Pid, Body: nm.Body}
	var err error
	switch obj, _ := xtcpnl.TcEventKind(m.Type); obj {
	case xtcpnl.TcObjFilter, xtcpnl.TcObjChain:
		_, err = r.printFilter(m)
	case xtcpnl.TcObjClass:
		_, err = r.printClass(m)
	case xtcpnl.TcObjQdisc:
		_, err = r.printQdisc(m)
	case xtcpnl.TcObjAction:
		_, err = r.printAction(m)
	case xtcpnl.TcObjNone:
		if m.Type != unix.NLMSG_ERROR && m.Type != unix.NLMSG_NOOP && m.Type != unix.NLMSG_DONE {
			r.errf("Unknown message: length %08d type %08x flags %08x\n", nm.Hdr.Len, m.Type, m.Flags)
		}
	}
	return err
}

// nowFunc is the monitor's clock: GOTC_NOW (RFC3339) for a replay, else
// the wall clock.
func nowFunc() func() time.Time { return nlmon.NowFunc("GOTC_NOW") }
