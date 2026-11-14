package goip

import (
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip macsec` is do_ipmacsec (ip/ipmacsec.c): usage with no argument or
// help by prefix, then genl_init_handle's macsec lookup, then the verb by
// prefix, show, add, set, delete or offload:
//
//   - show: GET_TXSC's dump, every SecY printed, or with a device, which
//     ll_name_to_index must find, that device's alone, under -j as one
//     array;
//   - add, set and delete: the device, ll_name_to_index's, then tx and an
//     SA, or rx, a channel's SCI and optionally its SA, each one request,
//     rtnl_talk's with NLM_F_ACK;
//   - offload: the device and the offload type, UPD_OFFLOAD.
//
// goip sends the lookup, the device's and the dump, and writes a write's
// request to GOIP_WRITE_OUT.

const (
	macsecHelpCst    = "help"
	macsecShowCst    = "show"
	macsecAddCst     = "add"
	macsecSetCst     = "set"
	macsecDeleteCst  = "delete"
	macsecOffloadCst = "offload"
	// macsecUsageCst is ipmacsec_usage's text.
	macsecUsageCst = "Usage: ip macsec add DEV tx sa { 0..3 } [ OPTS ] key ID KEY\n" +
		"       ip macsec set DEV tx sa { 0..3 } [ OPTS ]\n" +
		"       ip macsec del DEV tx sa { 0..3 }\n" +
		"       ip macsec add DEV rx SCI [ on | off ]\n" +
		"       ip macsec set DEV rx SCI [ on | off ]\n" +
		"       ip macsec del DEV rx SCI\n" +
		"       ip macsec add DEV rx SCI sa { 0..3 } [ OPTS ] key ID KEY\n" +
		"       ip macsec set DEV rx SCI sa { 0..3 } [ OPTS ]\n" +
		"       ip macsec del DEV rx SCI sa { 0..3 }\n" +
		"       ip macsec show\n" +
		"       ip macsec show DEV\n" +
		"       ip macsec offload DEV [ off | phy | mac ]\n" +
		"where  OPTS := [ pn <u32> | xpn <u64> ] [ salt SALT ] [ ssci <u32> ] [ on | off ]\n" +
		"       ID   := 128-bit hex string\n" +
		"       KEY  := 128-bit or 256-bit hex string\n" +
		"       SCI  := { sci <u64> | port { 1..2^16-1 } address <lladdr> }\n" +
		"       SALT := 96-bit hex string"
)

var (
	// ErrMacsecUsage is a refusal ipmacsec.c ends with exit(-1), 255:
	// usage, an unknown command or word, invarg, duparg2,
	// incomplete_command, a missing SCI and an offload type it does not
	// know.
	ErrMacsecUsage = errors.New("macsec")
	// ErrMacsecRefused is a refusal ipmacsec.c returns -1 for, which ip
	// exits 1 with: a device ll_name_to_index does not find, and
	// check_sa_args' an SA added with no key or PN, or set with a key.
	ErrMacsecRefused = errors.New("macsec refused")
	// ErrMacsecDumpTerminated is a dump the kernel refuses or a message
	// process refuses: "Dump terminated", exit(1).
	ErrMacsecDumpTerminated = errors.New("dump terminated")
	// errMacsecIncomplete is a message validate_dump or
	// validate_secy_dump refuses.
	errMacsecIncomplete = errors.New("incomplete dump message")
)

// macsecUsage is ipmacsec_usage, exit(-1).
func macsecUsage() error {
	return fmt.Errorf("%w: %s", ErrMacsecUsage, macsecUsageCst)
}

// macsecVerb is enum cmd, with the commands macsec_commands gives it for
// a transmit SA, a receive channel and a receive SA.
type macsecVerb struct {
	add, upd, del    bool
	txsa, rxsc, rxsa uint8
}

var (
	macsecVerbAdd = macsecVerb{add: true, txsa: xtcpnl.MacsecCmdAddTxsa, rxsc: xtcpnl.MacsecCmdAddRxsc,
		rxsa: xtcpnl.MacsecCmdAddRxsa}
	macsecVerbUpd = macsecVerb{upd: true, txsa: xtcpnl.MacsecCmdUpdTxsa, rxsc: xtcpnl.MacsecCmdUpdRxsc,
		rxsa: xtcpnl.MacsecCmdUpdRxsa}
	macsecVerbDel = macsecVerb{del: true, txsa: xtcpnl.MacsecCmdDelTxsa, rxsc: xtcpnl.MacsecCmdDelRxsc,
		rxsa: xtcpnl.MacsecCmdDelRxsa}
)

// runMacsec is do_ipmacsec.
func runMacsec(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], macsecHelpCst) {
		return macsecUsage()
	}
	id, err := c.genlFamily(xtcpnl.MacsecGenlName)
	if err != nil {
		return err
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, macsecShowCst):
		return macsecShow(c, id, rest)
	case matchesPrefix(verb, macsecAddCst):
		return writeOps.macsecModify(c, id, &macsecVerbAdd, rest)
	case matchesPrefix(verb, macsecSetCst):
		return writeOps.macsecModify(c, id, &macsecVerbUpd, rest)
	case matchesPrefix(verb, macsecDeleteCst):
		return writeOps.macsecModify(c, id, &macsecVerbDel, rest)
	case matchesPrefix(verb, macsecOffloadCst):
		return writeOps.macsecOffload(c, id, rest)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip macsec help\"", ErrMacsecUsage, verb)
}

// macsecDev is ll_name_to_index, whose 0 is "Device does not exist" and
// -1.
func macsecDev(c *runCtx, name string) (int32, error) {
	idx, err := genlNameToIndex(c, name)
	if err != nil {
		return 0, err
	}
	if idx == 0 {
		return 0, fmt.Errorf("%w: Device %q does not exist", ErrMacsecRefused, name)
	}
	return idx, nil
}

// macsecShow is do_show: with a device, ll_name_to_index's, which must
// find it, and usage for any word after it; then do_dump: GET_TXSC's
// dump, each message read by process, under -j in one array, closed
// however the dump ends.
func macsecShow(c *runCtx, id uint16, args []string) error {
	var ifindex int32
	if len(args) != 0 {
		idx, err := macsecDev(c, args[0])
		if err != nil {
			return err
		}
		if len(args) > 1 {
			return macsecUsage()
		}
		ifindex = idx
	}
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.MacsecGenlName, id, xtcpnl.MacsecCmdGetTxsc,
		xtcpnl.MacsecGenlVersion, unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	var arr []byte
	for i := 0; err == nil && i < len(bodies); i++ {
		err = macsecPrint(c, ifindex, bodies[i], &arr)
	}
	if jerr := genlJSONEnd(c, arr); jerr != nil {
		return jerr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrMacsecDumpTerminated, err)
	}
	return nil
}

// macsecPrint is process: a body shorter than its genlmsghdr is refused,
// a command other than GET_TXSC skipped, then validate_dump and
// validate_secy_dump, whose refusal prints "incomplete dump message",
// then the device filter, then the SecY printed, as text or into arr
// under -j. ParseMacsec's stricter refusals, which the kernel's messages
// do not meet, end the dump the same way.
func macsecPrint(c *runCtx, ifindex int32, body []byte, arr *[]byte) error {
	if len(body) >= mptcpGenlHdrLenCst && body[0] != xtcpnl.MacsecCmdGetTxsc {
		return nil
	}
	m, err := xtcpnl.ParseMacsec(body)
	if err != nil {
		return err
	}
	if !macsecValid(&m) {
		if _, err := fmt.Fprintln(c.errOut, errMacsecIncomplete.Error()); err != nil {
			return err
		}
		return errMacsecIncomplete
	}
	idx := int32(*m.Ifindex)
	if ifindex != 0 && idx != ifindex {
		return nil
	}
	text, obj := render.MacsecSecY(&m, genlIndexToName(c, idx), c.stats())
	if c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err = io.WriteString(c.out, text)
	return err
}

// macsecValid is validate_dump and validate_secy_dump: the ifindex, the
// SecY, both lists and both statistics nests, and every SecY attribute but
// the window.
func macsecValid(m *xtcpnl.Macsec) bool {
	if m.Ifindex == nil || m.SecY == nil || m.TxSAs == nil || m.RxSCs == nil || m.TxscStats == nil ||
		m.SecYStats == nil {
		return false
	}
	s := m.SecY
	for _, p := range []*uint8{s.EncodingSA, s.ICVLen, s.Protect, s.Replay, s.Oper, s.Validate, s.Encrypt,
		s.IncSCI, s.ES, s.SCB} {
		if p == nil {
			return false
		}
	}
	return s.SCI != nil && s.CipherSuite != nil
}
