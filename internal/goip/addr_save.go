package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip address save` and `ip address showdump` (ip/ipaddress.c:1784-1865).
//
// save writes a 4-byte magic and then the address dump's replies to stdout,
// each whole as the kernel sent it, header included. showdump reads that
// file back from stdin and prints each record as `ifN:` and the address
// lines, sending nothing. Neither writes to the kernel, so neither needs
// GOIP_WRITE_OUT. restore, the third, reads the same file (addr_restore.go).

var (
	// ErrDumpFile is showdump refusing its stdin: a terminal, a bad magic,
	// or a malformed or truncated record. `ip` exits -1 for each, so 255.
	ErrDumpFile = errors.New("dump file")
	// ErrSaveTerminated is a save whose dump failed after the magic went
	// out. `ip` prints "Save terminated" and exits 1, leaving what it wrote.
	ErrSaveTerminated = errors.New("address save")
)

const (
	// addrDumpMagicCst is ipadd_dump_magic (ip/ipaddress.c:1782), written
	// in host order.
	addrDumpMagicCst = 0x47361222
	// addrDumpMagicLenCst is the magic's size, the bytes read before the
	// first record. route_dump_magic is a __u32 too.
	addrDumpMagicLenCst = 4
	// addrDumpBufCst is rtnl_from_file's `char buf[16384]`
	// (lib/libnetlink.c:1326): no record may be longer.
	addrDumpBufCst = 16384
	// ifaddrmsgLenCst is sizeof(struct ifaddrmsg).
	ifaddrmsgLenCst = 8
)

// dumpFileFormat is what tells one object's dump file from another's: the
// magic, the object the terminal refusal names, and the errors the two kinds
// of refusal wrap, which carry `ip`'s exit codes. The address verbs exit -1
// for both (255); the route verbs return -1 for a terminal or a bad magic and
// -2 when rtnl_from_file fails, which do_cmd negates to 1 and 2.
type dumpFileFormat struct {
	magic  uint32
	object string
	// badFile is a terminal or a magic mismatch.
	badFile error
	// badRecord is a malformed or truncated record, or one the handler
	// refuses.
	badRecord error
}

// addrDumpFormat is `ip address save`'s file.
var addrDumpFormat = dumpFileFormat{
	magic: addrDumpMagicCst, object: addressObjectCst, badFile: ErrDumpFile, badRecord: ErrDumpFile,
}

// addrSave is ipaddr_list_flush_or_save's save arm (ip/ipaddress.c:2263-2280).
//
// The keyword loop is the show loop, and its device lookup goes out first:
// `ip addr save dev nosuch` fails on the lookup, and on a terminal the
// lookup is sent before the refusal. The dump is preferred_family's with
// the device's ifindex; no other selector filters it, and -j, -br and -o
// change nothing.
func addrSave(c *runCtx, args []string) error {
	// No capture shows what the kernel answers an AF_PACKET address dump.
	if c.family == unix.AF_PACKET {
		return fmt.Errorf("-0 address save: %w", ErrNotImplemented)
	}
	if _, ok := c.src.(MsgDumpSource); !ok {
		return fmt.Errorf("address save on a source without whole replies: %w", ErrNotImplemented)
	}
	svc := service.New(c.src, c.nextSeq)
	f, err := parseAddrShowArgs(args, c.family, addrShowLookups(c, svc))
	if err != nil {
		return err
	}
	if isTerminal(c.out) {
		return fmt.Errorf("%w: Not sending a binary stream to stdout", ErrInvalidArgument)
	}
	var magic [4]byte
	binary.NativeEndian.PutUint32(magic[:], addrDumpMagicCst)
	if _, werr := c.out.Write(magic[:]); werr != nil {
		return fmt.Errorf("%w: Can't write magic to dump file: %w", ErrInvalidArgument, werr)
	}
	var ifindex uint32
	if f.ifindex > 0 {
		ifindex = uint32(f.ifindex)
	}
	msgs, err := svc.AddressMsgs(c.family, ifindex)
	if err != nil {
		return fmt.Errorf("%w: Save terminated: %w", ErrSaveTerminated, err)
	}
	for _, m := range msgs {
		if _, werr := c.out.Write(m); werr != nil {
			return fmt.Errorf("%w: Save terminated: %w", ErrSaveTerminated, werr)
		}
	}
	return nil
}

// addrShowdump is ipaddr_showdump (ip/ipaddress.c:1847-1865): the magic
// check, then rtnl_from_file (lib/libnetlink.c:1322-1368) with show_handler.
// Its arguments are ignored, as `ip` ignores them, and the filter
// print_addrinfo reads is zero, so every record prints.
func addrShowdump(c *runCtx, _ []string) error {
	// Each changes print_addrinfo's output, and no capture shows it.
	switch {
	case c.json:
		return fmt.Errorf("-j address showdump: %w", ErrNotImplemented)
	case c.brief:
		return fmt.Errorf("-br address showdump: %w", ErrNotImplemented)
	case c.oneline:
		return fmt.Errorf("-o address showdump: %w", ErrNotImplemented)
	}
	if err := checkDumpMagic(c, addrDumpFormat, "showdump"); err != nil {
		return err
	}
	return readDumpRecords(c, addrDumpFormat, func(h xtcpnl.NlMsgHdr, body []byte) error {
		return showDumpRecord(c.out, h.Type, body)
	})
}

// checkDumpMagic is ipadd_dump_check_magic (ip/ipaddress.c:1803-1818), or
// route_dump_check_magic (ip/iproute.c:2342-2359) for a route file, which
// showdump and restore both run first: the terminal refusal, then the magic.
func checkDumpMagic(c *runCtx, f dumpFileFormat, verb string) error {
	if c.in == nil {
		return fmt.Errorf("%s %s without stdin: %w", f.object, verb, ErrNotImplemented)
	}
	if isTerminal(c.in) {
		return fmt.Errorf("%w: Can't restore %s dump from a terminal", f.badFile, f.object)
	}
	// fread(&magic, 4, 1) counts whole elements, and glibc copies a short
	// read's bytes into magic all the same.
	var magic [addrDumpMagicLenCst]byte
	n, rerr := io.ReadFull(c.in, magic[:])
	if got := binary.NativeEndian.Uint32(magic[:]); got != f.magic || rerr != nil {
		return fmt.Errorf("%w: Magic mismatch (%d elems, %x magic)", f.badFile, n/len(magic), got)
	}
	return nil
}

// readDumpRecords is rtnl_from_file's loop after the magic, handing each
// whole record to onRecord as the handler gets it. pos counts the bytes read,
// the magic's included, which the malformed message reports as ftell does.
func readDumpRecords(c *runCtx, f dumpFileFormat, onRecord func(h xtcpnl.NlMsgHdr, body []byte) error) error {
	pos := addrDumpMagicLenCst
	var hdr [xtcpnl.NlMsgHdrSizeCst]byte
	for {
		n, err := io.ReadFull(c.in, hdr[:])
		pos += n
		if n == 0 && errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return dumpReadError(err, f.badRecord)
		}
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(hdr[:], &h); err != nil {
			return fmt.Errorf("goip: decode a dump file record: %w", err)
		}
		// len is an int, so a length past 2^31 is negative, l < 0 and
		// refused, as one past the buffer is.
		length := cInt32(h.Len)
		l := length - xtcpnl.NlMsgHdrSizeCst
		if l < 0 || h.Len > addrDumpBufCst {
			return fmt.Errorf("%w: !!!malformed message: len=%d @%d", f.badRecord, length, pos)
		}
		body := make([]byte, nlmsgAlign(int(l)))
		n, err = io.ReadFull(c.in, body)
		pos += n
		if err != nil {
			return dumpReadError(err, f.badRecord)
		}
		if err := onRecord(h, body[:l]); err != nil {
			return err
		}
	}
}

// showDumpRecord is show_handler: `if%d:` from the ifaddrmsg's index, then
// print_addrinfo, which prints nothing for a type that is not an address.
func showDumpRecord(w io.Writer, msgType uint16, body []byte) error {
	// show_handler reads ifa_index from rtnl_from_file's buffer whatever
	// the record's length, so a shorter record prints a stale index.
	if len(body) < ifaddrmsgLenCst {
		return fmt.Errorf("address showdump of a %d-byte record: %w", len(body), ErrNotImplemented)
	}
	switch msgType {
	case unix.RTM_NEWADDR:
	case unix.RTM_DELADDR, unix.RTM_NEWMULTICAST, unix.RTM_DELMULTICAST,
		unix.RTM_NEWANYCAST, unix.RTM_DELANYCAST:
		return fmt.Errorf("address showdump of a type %d record: %w", msgType, ErrNotImplemented)
	default:
		_, err := fmt.Fprintf(w, "if%d:\n", cInt32(binary.LittleEndian.Uint32(body[4:8])))
		return err
	}
	ai, err := xtcpnl.ParseNewAddr(body)
	if err != nil {
		return fmt.Errorf("goip: decode RTM_NEWADDR: %w", err)
	}
	_, err = fmt.Fprintf(w, "if%d:\n%s", cInt32(ai.Index), render.AddrViewOf(ai).Text())
	return err
}

// dumpReadError is rtnl_from_file's short read: truncation at EOF, else the
// read error perror reports. kind is the format's badRecord.
func dumpReadError(err, kind error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: rtnl-from_file: truncated message", kind)
	}
	return fmt.Errorf("%w: rtnl_from_file: fread: %w", kind, err)
}

// cInt32 is a uint32 as C's %d prints it.
func cInt32(v uint32) int64 {
	n := int64(v)
	if n > math.MaxInt32 {
		n -= 1 << 32
	}
	return n
}

// isTerminal is isatty: true for a file whose descriptor answers TCGETS.
// A character device that is not a terminal, such as /dev/null, is not one,
// so `ip addr save > /dev/null` runs. Anything without a descriptor is not
// a terminal.
func isTerminal(v any) bool {
	f, ok := v.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	_, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	return err == nil
}
