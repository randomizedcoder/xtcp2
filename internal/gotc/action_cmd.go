package gotc

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/m_action.c do_action, tc_action_gd (get), tc_act_list_or_flush
// (list), print_action and act_usage (iproute2 v7.2.0). The writes (add,
// change, replace, delete and flush) are refused.

// actUsageText is act_usage's text.
const actUsageText = "usage: tc actions <ACTSPECOP>*\n" +
	"Where:\t\tACTSPECOP := ACR | GD | FL\n" +
	"\tACR := add | change | replace <ACTSPEC>*\n" +
	"\tGD := get | delete | <ACTISPEC>*\n" +
	"\tFL := ls | list | flush | <ACTNAMESPEC>\n" +
	"\tACTNAMESPEC :=  action <ACTNAME>\n" +
	"\tACTISPEC := <ACTNAMESPEC> <INDEXSPEC>\n" +
	"\tACTSPEC := action <ACTDETAIL> [INDEXSPEC] [HWSTATSSPEC] [SKIPSPEC]\n" +
	"\tINDEXSPEC := index <32 bit indexvalue>\n" +
	"\tHWSTATSSPEC := hw_stats [ immediate | delayed | disabled ]\n" +
	"\tSKIPSPEC := [ skip_sw | skip_hw ]\n" +
	"\tACTDETAIL := <ACTNAME> <ACTPARAMS>\n" +
	"\t\tExample ACTNAME is gact, mirred, bpf, etc\n" +
	"\t\tEach action has its own parameters (ACTPARAMS)\n" +
	"\n"

// actUsage is act_usage: exit(-1).
func (r *runner) actUsage() error {
	r.errf("%s", actUsageText)
	return exitCode(-1)
}

// actKindArg is strncpy(k, *argv, sizeof(k) - 1) into tc's char k[16]: at
// most 15 bytes; tc leaves k[15] unset (AC10), here a NUL.
func actKindArg(s string) string {
	if len(s) > xtcpnl.TcActKindMaxCst {
		return s[:xtcpnl.TcActKindMaxCst]
	}
	return s
}

// doAction is do_action: each sub-command in turn. A get that fails to
// talk returns 1 with its words unconsumed, so the loop meets the word
// after "get" as a command (AC8); list returns whatever follows the kind
// unread (AC7).
func (r *runner) doAction(args []string) error {
	for len(args) > 0 {
		w := args[0]
		switch {
		case matches(w, "add"), matches(w, "change"), matches(w, "replace"), matches(w, "delete"):
			return fmt.Errorf("%w: tc actions %s", errNotImplemented, w)
		case matches(w, "get"):
			rest, ret, err := r.actionGet(args[1:])
			if err != nil {
				return err
			}
			if ret < 0 {
				return retCode(-1)
			}
			args = rest
		case listWords(w):
			if len(args) <= 2 {
				return r.actUsage()
			}
			return r.actList(args[2:])
		case matches(w, "flush"):
			return fmt.Errorf("%w: tc actions %s", errNotImplemented, w)
		case matches(w, helpWord):
			return r.actUsage()
		default:
			r.errf("Command \"%s\" is unknown, try \"tc actions help\".\n", w)
			return retCode(-1)
		}
	}
	return nil
}

// actionGet is tc_action_gd(RTM_GETACTION): it skips the word after
// "get" whatever it is, so `get help` and `get action` send an empty tab
// (AC9). On success the words are consumed (rest is empty); on a failed
// talk or print it returns 1 and args as given.
func (r *runner) actionGet(args []string) (rest []string, ret int, err error) {
	g := args
	if len(g) > 0 {
		g = g[1:]
	}
	var acts []xtcpnl.TcActRef
	for len(g) > 0 {
		switch g[0] {
		case wordAction:
			g = g[1:]
			continue
		case helpWord:
			return nil, -1, nil
		}
		k := actKindArg(g[0])
		if !linkedActKinds[k] {
			r.errf("Error: non existent action: %s\n", k)
			return nil, -1, nil
		}
		g = g[1:]
		if len(g) == 0 || !matches(g[0], "index") {
			r.errf("Error: no index specified action: %s\n", k)
			return nil, -1, nil
		}
		g = g[1:]
		if len(g) == 0 {
			return nil, 0, r.incomplete()
		}
		i, ok := getU32(g[0], 10)
		if !ok {
			r.errf("Illegal \"index\"\n")
			return nil, -1, nil
		}
		g = g[1:]
		acts = append(acts, xtcpnl.TcActRef{Kind: k, Index: i})
	}
	if n := xtcpnl.TcActGetLen(acts); n > xtcpnl.TcActMaxMsgCst {
		return nil, 0, fmt.Errorf("%w: tc actions get of %d actions is %d bytes, past tc's MAX_MSG of %d: "+
			"tc ignores addattr_l's failures and sends what fit (AC38)",
			errNotImplemented, len(acts), n, xtcpnl.TcActMaxMsgCst)
	}
	req, berr := xtcpnl.BuildTcActionGet(acts, r.nextSeq())
	m, ok, err := r.talkAnswer(req, berr)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		r.errf("We have an error talking to the kernel\n")
		return args, 1, nil
	}
	r.o.newJSONObj()
	rc, err := r.printAction(m)
	if err != nil {
		return nil, 0, err
	}
	if rc < 0 {
		r.errf("Dump terminated\n")
		r.o.deleteJSONObj()
		return args, 1, nil
	}
	r.o.deleteJSONObj()
	return nil, 0, nil
}

// actList is tc_act_list_or_flush(RTM_GETACTION) on the words after the
// sub-command's second: a kind tc does not link is "bad action" and
// returns 0 (AC6); only `since` is read after the kind (AC7), and it
// sends TCA_ROOT_TIME_DELTA only when non-zero.
func (r *runner) actList(args []string) error {
	k := actKindArg(args[0])
	if !linkedActKinds[k] {
		r.errf("bad action %s\n", k)
		return nil
	}
	args = args[1:]
	var since uint32
	if len(args) > 0 && args[0] == "since" {
		if len(args) < 2 {
			return r.incomplete()
		}
		v, ok := getU32(args[1], 0)
		if !ok {
			return r.invarg("dump time \"since\" is invalid", args[1])
		}
		since = v
	}
	req, berr := xtcpnl.BuildTcActionDump(k, r.brief > 0, since, since != 0, r.nextSeq())
	r.o.newJSONObj()
	var ferr error
	rc, err := r.dump(req, berr, func(m Msg) int {
		n, err := r.printAction(m)
		if err != nil {
			ferr = err
			return -1
		}
		return n
	})
	if err == nil {
		err = ferr
	}
	if err != nil {
		return err
	}
	r.o.deleteJSONObj()
	if rc < 0 {
		return retCode(-1)
	}
	return nil
}

// printAction is print_action: "total acts" from TCA_ROOT_COUNT, read
// unchecked (here MIN-copied) and absent from a get's reply, so a get
// prints 0 (AC2); it prints before the tab is checked (AC3), as a JSON
// object of its own whose key has a space (AC1). The actions are unbound.
// A monitor's events add a word: Added, Replaced or Deleted action, or
// Flushed table.
func (r *runner) printAction(m Msg) (int, error) {
	o := r.o
	if len(m.Body) < xtcpnl.TcaMsgSizeCst {
		r.errf("Wrong len %d\n", len(m.Body)-xtcpnl.TcaMsgSizeCst)
		return -1, nil
	}
	tb := r.attrTable(m.Body[xtcpnl.TcaMsgSizeCst:], xtcpnl.TcaRootMaxKnown, 0)
	var count uint32
	var tot uint16 // tc_dump_action's tot_acts, an unsigned short
	if tb.Has(xtcpnl.TcaRootCount) {
		count, tot = attrU32(tb, xtcpnl.TcaRootCount), rtaU16(tb.Bytes(xtcpnl.TcaRootCount))
	}
	o.openObject("")
	o.uint(printAny, "total acts", "total acts "+u32s(count), count)
	o.nl()
	o.closeObject()
	if !tb.Has(xtcpnl.TcaRootTab) {
		if m.Type != unix.RTM_GETACTION {
			r.errf("print_action: NULL kind\n")
		}
		return -1, nil
	}
	// The event words go out through fprintf, outside any JSON (MO9).
	// NLM_F_ROOT and NLM_F_REPLACE are both 0x100: a delete's flag space is
	// a get's.
	switch {
	case m.Type == unix.RTM_DELACTION && m.Flags&unix.NLM_F_ROOT != 0:
		o.raw("Flushed table ")
		r.tabFlush = true
	case m.Type == unix.RTM_DELACTION:
		o.raw("Deleted action ")
	case m.Type == unix.RTM_NEWACTION && m.Flags&unix.NLM_F_CREATE != 0 && m.Flags&unix.NLM_F_REPLACE == 0:
		o.raw("Added action ")
	case m.Type == unix.RTM_NEWACTION && m.Flags&unix.NLM_F_REPLACE != 0:
		o.raw("Replaced action ")
	}
	o.openObject("")
	if err := r.dumpAction(tb.Bytes(xtcpnl.TcaRootTab), tot, false); err != nil {
		return 0, err
	}
	if tb.Has(xtcpnl.TcaRootExtWarnMsg) {
		w, _ := tb.Str(xtcpnl.TcaRootExtWarnMsg)
		o.str(printAny, colorNone, "warn", w, w)
		o.nl()
	}
	o.closeObject()
	return 0, nil
}
