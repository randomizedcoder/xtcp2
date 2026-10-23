package goip

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip sr` is do_seg6 and seg6_do_cmd (ip/ipseg6.c): the argument parse,
// with the secret read before anything is sent, then genl_init_handle's
// SEG6 lookup, then one request:
//
//   - hmac show: a DUMPHMAC dump, each key printed by print_dumphmac;
//   - hmac set KEYID ALGO: a SETHMAC with NLM_F_ACK carrying the key id,
//     the secret's length, the algorithm and, unless it is blank, the
//     secret, which getpass reads; a blank secret deletes the key;
//   - tunsrc show: one GET_TUNSRC and its reply, printed by print_tunsrc;
//   - tunsrc set ADDRESS: a SET_TUNSRC with NLM_F_ACK carrying the address.
//
// goip sends the lookup and the reads, and writes a SETHMAC or SET_TUNSRC
// to GOIP_WRITE_OUT.

const (
	srHelpCst   = "help"
	srShowCst   = "show"
	srSetCst    = "set"
	srHmacCst   = "hmac"
	srTunsrcCst = "tunsrc"
	// srHmacPromptCst is HMAC_KEY_PROMPT.
	srHmacPromptCst = "Enter secret for HMAC key ID (blank to delete): "
	// srReqBufCst is SEG6_REQUEST's buffer, and srReqHdrCst the nlmsghdr
	// and genlmsghdr ahead of the attributes in it.
	srReqBufCst = 1024
	srReqHdrCst = xtcpnl.NlMsgHdrSizeCst + 4
	// srTTYCst is the terminal glibc's getpass reads.
	srTTYCst = "/dev/tty"
)

var (
	// ErrSrUsage is a refusal ipseg6.c ends with exit(-1), 255: invarg and,
	// in NEXT_ARG, incomplete_command.
	ErrSrUsage = errors.New("sr")
	// ErrSrDumpTerminated is a DUMPHMAC dump the kernel refuses or a reply
	// that does not decode: "Dump terminated", exit 1.
	ErrSrDumpTerminated = errors.New("dump terminated")
	// ErrSrReply is a GET_TUNSRC answer process_msg refuses: "Error parsing
	// reply", exit 1.
	ErrSrReply = errors.New("error parsing reply")
)

// srOpenTTY opens the terminal getpass prompts on and reads from. A test
// replaces it.
var srOpenTTY = func() (*os.File, error) {
	return os.OpenFile(srTTYCst, os.O_RDWR, 0)
}

// srUsage marks err as one of ErrSrUsage's refusals.
func srUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrSrUsage, err)
}

// srReq is seg6_do_cmd's opts: the command and what it carries.
type srReq struct {
	cmd   uint8
	addr  [16]byte
	keyID uint32
	pass  []byte
	algID uint8
}

// runSr is do_seg6. help, and so h, is checked before hmac, so `sr h` is
// usage.
func runSr(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], srHelpCst) {
		return fmt.Errorf("sr help: %w", ErrNotImplemented)
	}
	var r srReq
	var err error
	switch {
	case matchesPrefix(args[0], srHmacCst):
		err = srParseHmac(c, args, &r)
	case matchesPrefix(args[0], srTunsrcCst):
		err = srParseTunsrc(args, &r)
	default:
		err = srUsage(invargErr("unknown", args[0]))
	}
	if err != nil {
		return err
	}
	return srDoCmd(c, &r)
}

// srParseHmac is the hmac arm: show, or set with a key id other than 0,
// sha1 or sha256 by strcmp, then the secret.
func srParseHmac(c *runCtx, args []string, r *srReq) error {
	i, err := nextArg(args, 0)
	if err != nil {
		return srUsage(err)
	}
	switch {
	case matchesPrefix(args[i], srShowCst):
		r.cmd = xtcpnl.Seg6CmdDumpHmac
		return nil
	case !matchesPrefix(args[i], srSetCst):
		return srUsage(invargErr("unknown", args[i]))
	}
	if i, err = nextArg(args, i); err != nil {
		return srUsage(err)
	}
	id, ok := getU32(args[i], 0)
	if !ok || id == 0 {
		return srUsage(invargErr("hmac KEYID value is invalid", args[i]))
	}
	if i, err = nextArg(args, i); err != nil {
		return srUsage(err)
	}
	switch args[i] {
	case "sha1":
		r.algID = xtcpnl.Seg6HmacAlgoSHA1
	case "sha256":
		r.algID = xtcpnl.Seg6HmacAlgoSHA256
	default:
		return srUsage(invargErr("hmac ALGO value is invalid", args[i]))
	}
	r.cmd, r.keyID = xtcpnl.Seg6CmdSetHmac, id
	r.pass, err = srGetpass(c)
	return err
}

// srParseTunsrc is the tunsrc arm: show, or set with an IPv6 address,
// get_addr's, which exits 1 on anything else. "default", "any" and "all"
// are the zero address.
func srParseTunsrc(args []string, r *srReq) error {
	i, err := nextArg(args, 0)
	if err != nil {
		return srUsage(err)
	}
	switch {
	case matchesPrefix(args[i], srShowCst):
		r.cmd = xtcpnl.Seg6CmdGetTunsrc
		return nil
	case !matchesPrefix(args[i], srSetCst):
		return srUsage(invargErr("unknown", args[i]))
	}
	if i, err = nextArg(args, i); err != nil {
		return srUsage(err)
	}
	a, err := routeGetAddr(args[i], unix.AF_INET6)
	if err != nil {
		return err
	}
	r.cmd = xtcpnl.Seg6CmdSetTunsrc
	copy(r.addr[:], a.data)
	return nil
}

// srGetpass is glibc's getpass: the prompt on /dev/tty and a line read from
// it with echo off, or, with no terminal, as under setsid, the prompt on
// stderr and the line from stdin.
func srGetpass(c *runCtx) (line []byte, err error) {
	in, out := c.in, c.errOut
	echoOff := false
	if tty, terr := srOpenTTY(); terr == nil {
		defer func() { err = errors.Join(err, tty.Close()) }()
		in, out = tty, tty
		if restore, ok := srNoEcho(int(tty.Fd())); ok {
			defer func() { err = errors.Join(err, restore()) }()
			echoOff = true
		}
	}
	return srReadPass(in, out, echoOff)
}

// srReadPass is getpass after the terminal is chosen: the prompt, then one
// line. Its newline is dropped, and ip takes the secret's strlen, so it
// ends at the first NUL; nothing read is a blank secret. A terminal whose
// echo was turned off gets the newline the user's Enter did not echo.
func srReadPass(in io.Reader, out io.Writer, echoOff bool) ([]byte, error) {
	if _, err := io.WriteString(out, srHmacPromptCst); err != nil {
		return nil, err
	}
	if in == nil {
		return nil, nil
	}
	line, err := bufio.NewReader(in).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
		if echoOff {
			if _, err := io.WriteString(out, "\n"); err != nil {
				return nil, err
			}
		}
	}
	if i := bytes.IndexByte(line, 0); i >= 0 {
		line = line[:i]
	}
	return line, nil
}

// srNoEcho is getpass's tcsetattr: echo and signals off, TCSAFLUSH, and a
// restore of the saved settings. A descriptor that is not a terminal is
// left alone.
func srNoEcho(fd int) (func() error, bool) {
	saved, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, false
	}
	t := *saved
	t.Lflag &^= unix.ECHO | unix.ISIG
	if err := unix.IoctlSetTermios(fd, unix.TCSETSF, &t); err != nil {
		return nil, false
	}
	return func() error { return unix.IoctlSetTermios(fd, unix.TCSETSF, saved) }, true
}

// srDoCmd is seg6_do_cmd after the parse: the lookup, then the request.
func srDoCmd(c *runCtx, r *srReq) error {
	id, err := c.genlFamily(xtcpnl.Seg6GenlName)
	if err != nil {
		return err
	}
	switch r.cmd {
	case xtcpnl.Seg6CmdDumpHmac:
		return srHmacShow(c, id)
	case xtcpnl.Seg6CmdGetTunsrc:
		return srTunsrcShow(c, id)
	}
	attrs, err := srWriteAttrs(c, r)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.Seg6GenlName, id, r.cmd, xtcpnl.Seg6GenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return err
	}
	return writeAddrOut(req)
}

// srWriteAttrs is a write's attributes. SETHMAC's SECRETLEN is the
// secret's length as a u8, so 256 bytes is 0, and a secret past
// SEG6_REQUEST's 1024-byte buffer is left out with addattr_l's message,
// which ip ignores.
func srWriteAttrs(c *runCtx, r *srReq) ([]byte, error) {
	var raw [srReqBufCst - srReqHdrCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	if r.cmd == xtcpnl.Seg6CmdSetTunsrc {
		if err := b.PutBytes(xtcpnl.Seg6AttrDst, r.addr[:]); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	if err := b.PutU32(xtcpnl.Seg6AttrHmacKeyID, r.keyID); err != nil {
		return nil, err
	}
	if err := b.PutU8(xtcpnl.Seg6AttrSecretLen, uint8(len(r.pass))); err != nil {
		return nil, err
	}
	if err := b.PutU8(xtcpnl.Seg6AttrAlgID, r.algID); err != nil {
		return nil, err
	}
	if len(r.pass) != 0 {
		if err := b.PutBytes(xtcpnl.Seg6AttrSecret, r.pass); err != nil {
			if _, werr := fmt.Fprintf(c.errOut, "addattr_l ERROR: message exceeded bound of %d\n", srReqBufCst); werr != nil {
				return nil, werr
			}
		}
	}
	return b.Bytes(), nil
}

// srHmacShow is the DUMPHMAC dump: each key's line, or under -j one array
// of their objects. A reply process_msg cannot parse ends the dump with
// "Dump terminated", exit 1.
func srHmacShow(c *runCtx, id uint16) error {
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.Seg6GenlName, id, xtcpnl.Seg6CmdDumpHmac,
		xtcpnl.Seg6GenlVersion, unix.NLM_F_DUMP, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	if err != nil {
		// new_json_obj has opened the array by the time the dump fails, and
		// nothing closes it.
		if c.json {
			if _, werr := io.WriteString(c.out, "["); werr != nil {
				return werr
			}
		}
		return fmt.Errorf("%w: %w", ErrSrDumpTerminated, err)
	}
	var arr []byte
	for _, body := range bodies {
		m, err := xtcpnl.ParseSeg6(body)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrSrDumpTerminated, err)
		}
		if err := srHmacOne(c, &m, &arr); err != nil {
			return err
		}
	}
	return genlJSONEnd(c, arr)
}

// srHmacOne is process_msg for one reply: the key, or, for a reply of
// another command, an empty object under -j and nothing otherwise.
func srHmacOne(c *runCtx, m *xtcpnl.Seg6, arr *[]byte) error {
	if m.Command != xtcpnl.Seg6CmdDumpHmac {
		genlJSONAppend(arr, []byte("{}"))
		return nil
	}
	v := render.SrHmacViewOf(m)
	if v.Truncated != "" {
		if _, err := io.WriteString(c.errOut, v.Truncated); err != nil {
			return err
		}
	}
	if c.json {
		genlJSONAppend(arr, v.JSON())
		return nil
	}
	_, err := io.WriteString(c.out, v.Text())
	return err
}

// srTunsrcShow is the GET_TUNSRC talk: a refusal is the kernel's errno,
// exit 2, and an answer of another type "Error parsing reply", exit 1.
func srTunsrcShow(c *runCtx, id uint16) error {
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.Seg6GenlName, id, xtcpnl.Seg6CmdGetTunsrc,
		xtcpnl.Seg6GenlVersion, 0, c.nextSeq(), nil)
	if err != nil {
		return err
	}
	typ, body, err := gs.GenlTalk(req)
	if err != nil {
		return fmt.Errorf("RTNETLINK answers: %w", err)
	}
	if typ != id {
		return fmt.Errorf("%w: a reply of type %d", ErrSrReply, typ)
	}
	m, err := xtcpnl.ParseSeg6(body)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSrReply, err)
	}
	var arr []byte
	switch {
	case m.Command != xtcpnl.Seg6CmdGetTunsrc:
		genlJSONAppend(&arr, []byte("{}"))
	case c.json:
		genlJSONAppend(&arr, render.SrTunsrcJSON(&m))
	default:
		if _, err := io.WriteString(c.out, render.SrTunsrcText(&m)); err != nil {
			return err
		}
	}
	return genlJSONEnd(c, arr)
}
