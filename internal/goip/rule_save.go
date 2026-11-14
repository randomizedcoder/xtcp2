package goip

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"golang.org/x/sys/unix"
)

// `ip rule save`, `ip rule restore` and `ip rule flush` (ip/iprule.c:605-660,
// :744-960).
//
// save and flush are iprule_list_flush_or_save with save_rule or flush_rule as
// the filter function: the list's one dump, in preferred_family or AF_INET.
// save writes a 4-byte magic and then every reply whole; save_rule runs no
// filter. flush runs filter_nlmsg and, for each reply carrying FRA_PRIORITY,
// sends the reply back as an RTM_DELRULE flagged NLM_F_REQUEST, which
// rtnl_talk acks, on a socket of its own. The kernel leaves FRA_PRIORITY out
// of a priority-0 rule, the local rule's, so that one is kept. Unlike
// the address and route flushes there are no rounds: one dump, one pass.
//
// restore reads a save file from stdin and sends each record back, its flags
// ORed with NLM_F_REQUEST|NLM_F_CREATE|NLM_F_ACK, after ll_init_map's link
// dump. goip writes the deletes and the restored rules to GOIP_WRITE_OUT, as
// the other flushes and restores do.

// ErrRuleDumpTerminated is the rule dump failing inside save or flush:
// iprule_list_flush_or_save returns 1, which do_cmd negates, so 255.
var ErrRuleDumpTerminated = errors.New("rule dump terminated")

const (
	// ruleDumpMagicCst is rule_dump_magic (ip/iprule.c:605), written in host
	// order.
	ruleDumpMagicCst = 0x71706986
)

// ruleDumpFamily is the list's family substitution, which save and flush
// share with show.
func ruleDumpFamily(c *runCtx) uint8 {
	if c.family == unix.AF_UNSPEC {
		return unix.AF_INET
	}
	return c.family
}

// ruleSave is iprule_list_flush_or_save's save arm. The refusal of any
// argument comes first, then save_rule_prep's terminal refusal and the magic,
// then the dump.
func ruleSave(c *runCtx, args []string) error {
	// iproute2 ends the line with a period; Go error strings do not.
	if len(args) > 0 {
		return fmt.Errorf("%w: \"ip rule save\" does not take any arguments", ErrInvalidArgument)
	}
	// new_json_obj opens an array on stdout after the dump request, so `ip
	// -j rule save` appends `[]` to the file; no capture shows it.
	if c.json {
		return fmt.Errorf("-j rule save: %w", ErrNotImplemented)
	}
	if _, ok := c.src.(MsgDumpSource); !ok {
		return fmt.Errorf("rule save on a source without whole replies: %w", ErrNotImplemented)
	}
	if isTerminal(c.out) {
		return fmt.Errorf("%w: Not sending a binary stream to stdout", ErrInvalidArgument)
	}
	var magic [addrDumpMagicLenCst]byte
	binary.NativeEndian.PutUint32(magic[:], ruleDumpMagicCst)
	if _, werr := c.out.Write(magic[:]); werr != nil {
		return fmt.Errorf("%w: Can't write magic to dump file: %w", ErrInvalidArgument, werr)
	}
	msgs, err := service.New(c.src, c.nextSeq).RuleMsgs(ruleDumpFamily(c))
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrRuleDumpTerminated, err)
	}
	for _, m := range msgs {
		if _, werr := c.out.Write(m); werr != nil {
			return fmt.Errorf("%w: Dump terminated: %w", ErrRuleDumpTerminated, werr)
		}
	}
	return nil
}
