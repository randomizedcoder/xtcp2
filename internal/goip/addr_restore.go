package goip

import (
	"errors"
	"fmt"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip address restore` (ipaddr_restore and restore_handler,
// ip/ipaddress.c:1867-1889) reads a file `save` wrote from stdin and sends
// each saved message back, its flags ORed with NLM_F_REQUEST|NLM_F_CREATE|
// NLM_F_ACK. restore_handler calls ll_init_map before every message, and
// ll_init_map's static guard (lib/ll_map.c:388) sends its link dump once,
// ahead of the first write.
//
// goip sends the link dump and writes the messages to GOIP_WRITE_OUT,
// concatenated, at the seqs `ip` would send them with. Nothing is applied, so
// the kernel's answers, EEXIST included, are not goip's to print: `ip` prints
// each error the kernel attaches and ignores EEXIST, and with the kernel
// answering nothing else its rc is 0, as goip's is.

// ErrDumpTerminated is ll_init_map's link dump failing: `ip` prints "Dump
// terminated" and exits 1.
var ErrDumpTerminated = errors.New("address restore")

// restoreFlagsCst is what restore_handler ORs into each saved message's flags.
const restoreFlagsCst = unix.NLM_F_REQUEST | unix.NLM_F_CREATE | unix.NLM_F_ACK

// addrRestore is ipaddr_restore. Its arguments are ignored, as `ip` ignores
// them, and so are -4, -6, -j, -br and -o.
//
// GOIP_WRITE_OUT is checked at the first record, so the link dump goes out
// only when the writes can be written, and a file holding the magic alone
// sends nothing and needs no hook, as `ip` sends nothing for it. A record
// rtnl_from_file refuses partway still writes the messages before it, which
// `ip` has sent, and then fails.
func addrRestore(c *runCtx, _ []string) error {
	if err := checkDumpMagic(c, addrDumpFormat, "restore"); err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	var out []byte
	first := true
	err := readDumpRecords(c, addrDumpFormat, func(h xtcpnl.NlMsgHdr, body []byte) error {
		msg, err := restoreRecord(c, svc, h, body, first)
		if err != nil {
			return err
		}
		first = false
		out = append(out, msg...)
		return nil
	})
	if err != nil && !errors.Is(err, ErrDumpFile) {
		return err
	}
	if len(out) > 0 {
		if werr := writeAddrOut(out); werr != nil {
			return werr
		}
	}
	return err
}

// restoreRecord is restore_handler for one saved message: the link dump if it
// is the first, then the message as `ip` sends it, padded to NLMSG_ALIGN for
// the next.
func restoreRecord(c *runCtx, svc *service.Service, h xtcpnl.NlMsgHdr, body []byte, first bool) ([]byte, error) {
	// `ip` sends whatever the file holds. Only an address the dump could
	// have produced is modeled; the kernel's answer to anything else is not
	// in the corpus.
	if h.Type != unix.RTM_NEWADDR {
		return nil, fmt.Errorf("address restore of a type %d record: %w", h.Type, ErrNotImplemented)
	}
	hdrLen := xtcpnl.FamilyHdrLen(unix.RTM_NEWADDR)
	if len(body) < hdrLen {
		return nil, fmt.Errorf("address restore of a %d-byte record: %w", len(body), ErrNotImplemented)
	}
	if first {
		if path, ok := os.LookupEnv(writeOutEnvCst); !ok || path == "" {
			return nil, ErrWriteNotApplied
		}
		if err := svc.AddressRestoreLinks(); err != nil {
			return nil, fmt.Errorf("%w: Dump terminated: %w", ErrDumpTerminated, err)
		}
	}
	// rtnl_talk overwrites the seq. It keeps the saved pid, which
	// BuildWriteRequest zeroes, as flush's copies do; the parity masks it.
	msg, err := xtcpnl.BuildWriteRequest(unix.RTM_NEWADDR, h.Flags|restoreFlagsCst, c.nextSeq(), body[:hdrLen], body[hdrLen:])
	if err != nil {
		return nil, fmt.Errorf("goip: build address restore: %w", err)
	}
	if pad := nlmsgAlign(len(msg)) - len(msg); pad > 0 {
		msg = append(msg, make([]byte, pad)...)
	}
	return msg, nil
}
