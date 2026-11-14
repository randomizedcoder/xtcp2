//go:build goip_write

package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

const ruleObjectCst = "rule"

// ruleDumpFormat is `ip rule save`'s file. iprule_restore exits -1 on a
// terminal or a bad magic, and exits with rtnl_from_file's -1 on a malformed
// record, so both are 255.
var ruleDumpFormat = dumpFileFormat{
	magic: ruleDumpMagicCst, object: ruleObjectCst, badFile: ErrDumpFile, badRecord: ErrDumpFile,
}

// ruleFlush is iprule_list_flush_or_save's flush arm and flush_rule, and
// writes its RTM_DELRULE copies to GOIP_WRITE_OUT. Nothing copied, nothing
// written, and no hook needed: a flush with nothing to delete sends what `ip`
// sends, the dump alone.
//
// The selectors are refused, as `rule show`'s are: filter_nlmsg applies them
// to the replies and they change no request byte goip could be compared on.
// With none given, filter_nlmsg checks only the family, and only when one was
// asked for: `ip -6 rule flush` dumps AF_INET6 and keeps every reply, while a
// bare flush dumps AF_INET and checks nothing.
func ruleFlush(c *runCtx, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("rule flush %q: %w", args[0], ErrNotImplemented)
	}
	// new_json_obj prints `[]` around a flush that prints nothing else; no
	// capture shows it.
	if c.json {
		return fmt.Errorf("-j rule flush: %w", ErrNotImplemented)
	}
	if _, ok := c.src.(MsgDumpSource); !ok {
		return fmt.Errorf("rule flush on a source without whole replies: %w", ErrNotImplemented)
	}
	msgs, err := service.New(c.src, c.nextSeq).RuleMsgs(ruleDumpFamily(c))
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrRuleDumpTerminated, err)
	}
	var out []byte
	for _, m := range msgs {
		del, ok, cerr := ruleFlushCopy(c.family, m)
		if cerr != nil {
			return cerr
		}
		if ok {
			out = append(out, del...)
		}
	}
	if len(out) > 0 {
		return writeAddrOut(out)
	}
	return nil
}

// ruleFlushCopy is flush_rule on one reply: the message itself, its type and
// flags rewritten and everything else kept, its seq the dump's and its port
// id the reply's, as `ip` sends it. ok is false for a reply it leaves.
func ruleFlushCopy(family uint8, m []byte) (del []byte, ok bool, err error) {
	body := m[xtcpnl.NlMsgHdrSizeCst:]
	// flush_rule's `len < 0` returns -1, which ends the dump.
	if len(body) < xtcpnl.FibRuleHdrSizeCst {
		return nil, false, fmt.Errorf("%w: Dump terminated: a %d-byte rule", ErrRuleDumpTerminated, len(body))
	}
	if family != unix.AF_UNSPEC && body[0] != family {
		return nil, false, nil
	}
	var prio bool
	if werr := xtcpnl.WalkRTAttrs(body[xtcpnl.FibRuleHdrSizeCst:], func(atype uint16, _ []byte) {
		prio = prio || atype == unix.FRA_PRIORITY
	}); werr != nil {
		return nil, false, fmt.Errorf("rule flush of an undecodable reply: %w: %w", ErrNotImplemented, werr)
	}
	if !prio {
		return nil, false, nil
	}
	del = append([]byte(nil), m...)
	binary.NativeEndian.PutUint16(del[4:6], unix.RTM_DELRULE)
	binary.NativeEndian.PutUint16(del[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	return del, true, nil
}

// ruleRestore is iprule_restore: the magic check, then rtnl_from_file with
// restore_handler. Its arguments are ignored, as `ip` ignores them, and so
// are -4, -6 and -j.
//
// The file is read whole before anything is written. A record rtnl_from_file
// refuses ends the loop there, after `ip` has sent the records ahead of it,
// so goip writes those and fails alike. `ip` sends a record of any type; goip
// models the RTM_NEWRULE records save writes and refuses any other before
// anything is written.
func ruleRestore(c *runCtx, _ []string) error {
	if err := checkDumpMagic(c, ruleDumpFormat, routeRestoreVerbCst); err != nil {
		return err
	}
	var recs []routeRestoreRecord
	readErr := readDumpRecords(c, ruleDumpFormat, func(h xtcpnl.NlMsgHdr, body []byte) error {
		if h.Type != unix.RTM_NEWRULE {
			return fmt.Errorf("rule restore of a type %d record: %w", h.Type, ErrNotImplemented)
		}
		if len(body) < xtcpnl.FibRuleHdrSizeCst {
			return fmt.Errorf("rule restore of a %d-byte record: %w", len(body), ErrNotImplemented)
		}
		recs = append(recs, routeRestoreRecord{hdr: h, body: body})
		return nil
	})
	if readErr != nil && !errors.Is(readErr, ErrDumpFile) {
		return readErr
	}
	svc := service.New(c.src, c.nextSeq)
	var out []byte
	for i := range recs {
		msg, err := ruleRestoreMsg(c, svc, &recs[i], i == 0)
		if err != nil {
			return err
		}
		out = append(out, msg...)
	}
	if len(out) > 0 {
		if werr := writeAddrOut(out); werr != nil {
			return werr
		}
	}
	return readErr
}

// ruleRestoreMsg is restore_handler for one record: ll_init_map's link dump
// if it is the first, which later calls skip, then the message as rtnl_talk
// sends it at the next seq.
func ruleRestoreMsg(c *runCtx, svc *service.Service, r *routeRestoreRecord, first bool) ([]byte, error) {
	if first {
		if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
			return nil, ErrWriteNotApplied
		}
		if err := svc.RestoreLinks(); err != nil {
			return nil, fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
		}
	}
	// rtnl_talk keeps the saved port id, which BuildWriteRequest zeroes, as
	// the route and address restores' copies do; the parity masks it.
	msg, err := xtcpnl.BuildWriteRequest(unix.RTM_NEWRULE, r.hdr.Flags|restoreFlagsCst, c.nextSeq(),
		r.body[:xtcpnl.FibRuleHdrSizeCst], r.body[xtcpnl.FibRuleHdrSizeCst:])
	if err != nil {
		return nil, fmt.Errorf("goip: build rule restore: %w", err)
	}
	if pad := nlmsgAlign(len(msg)) - len(msg); pad > 0 {
		msg = append(msg, make([]byte, pad)...)
	}
	return msg, nil
}
