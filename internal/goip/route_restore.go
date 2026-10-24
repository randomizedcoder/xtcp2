package goip

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip route restore` (iproute_restore and restore_handler,
// ip/iproute.c:2304-2388) reads a file `route save` wrote from stdin and
// sends each saved message back, its flags ORed with NLM_F_REQUEST|
// NLM_F_CREATE|NLM_F_ACK, as `ip addr restore` does. Unlike it, the file is
// read three times, rewound between passes, and each pass sends one class of
// route, so a gateway is reachable by the time a route through it goes in:
// 0, routes with no gateway whose preferred source, if any, is their
// destination; 1, routes with no gateway and another preferred source; 2,
// routes with a gateway. ll_init_map's link dump goes out once, ahead of the
// first message sent.
//
// goip writes the messages to GOIP_WRITE_OUT in that order, at the seqs `ip`
// would send them with. Nothing is applied; `ip` ignores EEXIST and fails on
// any other refusal, which is the kernel's answer and not goip's.

// routeRestorePassesCst is iproute_restore's prio loop.
const routeRestorePassesCst = 3

// routeRestoreRecord is one saved message, read once and sent in its pass.
type routeRestoreRecord struct {
	hdr  xtcpnl.NlMsgHdr
	body []byte
	prio int
}

// routeRestore is iproute_restore. Its arguments are ignored, as `ip`
// ignores them, and so are -4, -6 and -j.
//
// The file is read whole before anything is sent. A record rtnl_from_file
// refuses ends the first pass there, so `ip` has sent the class-0 messages
// ahead of it and exits 2 without a second pass; goip writes those and fails
// alike. A record goip cannot classify is refused before anything is sent.
func routeRestore(c *runCtx, _ []string) error {
	if err := checkDumpMagic(c, routeDumpFormat, "restore"); err != nil {
		return err
	}
	var recs []routeRestoreRecord
	readErr := readDumpRecords(c, routeDumpFormat, func(h xtcpnl.NlMsgHdr, body []byte) error {
		prio, err := routeRestorePrio(h, body)
		if err != nil {
			return err
		}
		recs = append(recs, routeRestoreRecord{hdr: h, body: body, prio: prio})
		return nil
	})
	if readErr != nil && !errors.Is(readErr, ErrRouteDumpFile) {
		return readErr
	}
	passes := routeRestorePassesCst
	if readErr != nil {
		passes = 1
	}
	svc := service.New(c.src, c.nextSeq)
	var out []byte
	for prio := 0; prio < passes; prio++ {
		for i := range recs {
			if recs[i].prio != prio {
				continue
			}
			msg, err := routeRestoreMsg(c, svc, &recs[i], len(out) == 0)
			if err != nil {
				return err
			}
			out = append(out, msg...)
		}
	}
	if len(out) > 0 {
		if werr := writeAddrOut(out); werr != nil {
			return werr
		}
	}
	return readErr
}

// routeRestorePrio is restore_handler's classification of one record
// (ip/iproute.c:2316-2327). parse_rtattr keeps the first of each attribute,
// and rtattr_cmp calls two attributes equal when their lengths and payloads
// are. `ip` sends whatever the file holds; only a route a dump could have
// produced is modeled.
func routeRestorePrio(h xtcpnl.NlMsgHdr, body []byte) (int, error) {
	if h.Type != unix.RTM_NEWROUTE {
		return 0, fmt.Errorf("route restore of a type %d record: %w", h.Type, ErrNotImplemented)
	}
	if len(body) < rtmsgLenCst {
		return 0, fmt.Errorf("route restore of a %d-byte record: %w", len(body), ErrNotImplemented)
	}
	var gw, prefsrc, dst []byte
	err := xtcpnl.WalkRTAttrs(body[rtmsgLenCst:], func(atype uint16, val []byte) {
		switch atype {
		case unix.RTA_GATEWAY:
			if gw == nil {
				gw = val
			}
		case unix.RTA_PREFSRC:
			if prefsrc == nil {
				prefsrc = val
			}
		case unix.RTA_DST:
			if dst == nil {
				dst = val
			}
		}
	})
	if err != nil {
		return 0, fmt.Errorf("route restore of an undecodable record: %w: %w", ErrNotImplemented, err)
	}
	switch {
	case gw != nil:
		return 2, nil
	case prefsrc != nil && (dst == nil || !bytes.Equal(prefsrc, dst)):
		return 1, nil
	}
	return 0, nil
}

// routeRestoreMsg is restore_handler's send for one record: the link dump if
// it is the first sent, then the message as `ip` sends it, padded to
// NLMSG_ALIGN for the next.
func routeRestoreMsg(c *runCtx, svc *service.Service, r *routeRestoreRecord, first bool) ([]byte, error) {
	if first {
		if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
			return nil, ErrWriteNotApplied
		}
		if err := svc.RestoreLinks(); err != nil {
			return nil, fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
		}
	}
	// rtnl_talk overwrites the seq. It keeps the saved pid, which
	// BuildWriteRequest zeroes, as address restore's copies do; the parity
	// masks it.
	msg, err := xtcpnl.BuildWriteRequest(unix.RTM_NEWROUTE, r.hdr.Flags|restoreFlagsCst, c.nextSeq(),
		r.body[:rtmsgLenCst], r.body[rtmsgLenCst:])
	if err != nil {
		return nil, fmt.Errorf("goip: build route restore: %w", err)
	}
	if pad := nlmsgAlign(len(msg)) - len(msg); pad > 0 {
		msg = append(msg, make([]byte, pad)...)
	}
	return msg, nil
}
