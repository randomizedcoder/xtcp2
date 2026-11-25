package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// monfile is the .nl stream converter: the bytes `ip -d monitor file` walks
// (rtnl_from_file, lib/libnetlink.c:1315-1368, the same path the bond note's
// testsuite .nl files ride). The format takes no magic — each nlmsghdr, then
// its body zero-padded to the 4-byte boundary — because the reader freads the
// 16 header bytes, then NLMSG_ALIGN(nlmsg_len - 16) body bytes, and refuses a
// header cut short, a length under 16 or over its 16384 buffer, or a body cut
// short.
//
// Two inputs make the same stream:
//
//   - -spec: a crafted message list, JSONL, one message per line — the
//     nlmsghdr fields verbatim, the body as hex, and a why each line carries:
//     why the kernel cannot send it. A crafted blob is exactly the input the
//     live monitor can never produce (the EP2 bond partner states), so the
//     why is the fact that makes the line necessary, and the check asserts it
//     is never empty.
//   - -pcap: a captured pcap of rtnetlink traffic, the notifications a
//     subscriber was sent by default (nlparity.NotificationIndexes, the
//     delivery filter the internal/goip replay shares) or every message with
//     -all. The header is written as captured — type, flags, seq, pid —
//     because the replay must print the capture's own messages, not a
//     canonicalized stand-in for them.

// runMonfile is the monfile subcommand: -spec FILE or -pcap FILE, -out FILE.
func runMonfile(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("monfile", flag.ContinueOnError)
	fs.SetOutput(stderr)
	spec := fs.String("spec", "", "a crafted message list, one JSON message per line")
	pcap := fs.String("pcap", "", "a captured pcap of rtnetlink traffic")
	all := fs.Bool("all", false, "with -pcap: write every message, not the notifications alone")
	out := fs.String("out", "", "the .nl stream `ip -d monitor file` walks, to write")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" || (*spec == "") == (*pcap == "") {
		fmt.Fprintln(stderr, "nlcapgen monfile: exactly one of -spec or -pcap, and -out, are required")
		return 2
	}
	var msgs []monfileMsg
	if *spec != "" {
		var err error
		if msgs, err = readMonfileSpec(*spec); err != nil {
			fmt.Fprintln(stderr, "nlcapgen monfile:", err)
			return 1
		}
		fmt.Fprintf(stdout, "%d messages\n", len(msgs))
	} else {
		capture, err := nlparity.ParseRouteCaptureFile(*pcap)
		if err != nil {
			fmt.Fprintln(stderr, "nlcapgen monfile:", err)
			return 1
		}
		if n := capture.SkippedOtherFamily + capture.SkippedShortRecord + capture.SkippedBadDatagram; n > 0 {
			fmt.Fprintf(stderr, "nlcapgen monfile: skipped %d other-family, %d short and %d bad records\n",
				capture.SkippedOtherFamily, capture.SkippedShortRecord, capture.SkippedBadDatagram)
		}
		allMsgs := capture.Msgs()
		if *all {
			for i := range allMsgs {
				msgs = append(msgs, monfileMsg{hdr: allMsgs[i].Hdr, body: allMsgs[i].Body})
			}
			fmt.Fprintf(stdout, "%d messages\n", len(msgs))
		} else {
			for _, i := range nlparity.NotificationIndexes(allMsgs) {
				msgs = append(msgs, monfileMsg{hdr: allMsgs[i].Hdr, body: allMsgs[i].Body})
			}
			fmt.Fprintf(stdout, "%d of %d messages\n", len(msgs), len(allMsgs))
		}
	}
	var buf []byte
	for i := range msgs {
		buf = appendNlMsg(buf, msgs[i])
	}
	if err := writeFile(*out, func(w io.Writer) error {
		_, err := w.Write(buf)
		return err
	}); err != nil {
		fmt.Fprintln(stderr, "nlcapgen monfile:", err)
		return 1
	}
	return 0
}

// monfileMsg is one message of a .nl stream: the nlmsghdr verbatim and the
// body bytes, its Len re-derived as the header plus the body so a spec or a
// capture can never ask for a length the bytes contradict.
type monfileMsg struct {
	hdr  xtcpnl.NlMsgHdr
	body []byte
}

// appendNlMsg appends m's bytes to dst: the 16-byte nlmsghdr little-endian,
// then the body, then zero padding to the 4-byte boundary — generate_nlmsg's
// shape, a zeroed buffer cut at nlmsg_len, with the padding rtnl_from_file
// reads spelled out so a message with an unaligned body still ends where the
// next one begins.
func appendNlMsg(dst []byte, m monfileMsg) []byte {
	l := xtcpnl.NlMsgHdrSizeCst + len(m.body)
	h := make([]byte, xtcpnl.NlMsgHdrSizeCst)
	binary.LittleEndian.PutUint32(h[0:], uint32(l))
	binary.LittleEndian.PutUint16(h[4:], m.hdr.Type)
	binary.LittleEndian.PutUint16(h[6:], m.hdr.Flags)
	binary.LittleEndian.PutUint32(h[8:], m.hdr.Seq)
	binary.LittleEndian.PutUint32(h[12:], m.hdr.Pid)
	dst = append(dst, h...)
	dst = append(dst, m.body...)
	return append(dst, make([]byte, xtcpnl.FourByteAlignPadding(l))...)
}

// monfileSpec is one line of a -spec file. The JSON field names are the
// nlmsghdr's own, so a spec reads as the message it builds.
type monfileSpec struct {
	Type  uint16 `json:"type"`
	Flags uint16 `json:"flags"`
	Seq   uint32 `json:"seq"`
	Pid   uint32 `json:"pid"`
	// Body is the message body as hex, the family header plus attributes.
	Body string `json:"body"`
	// Why is why the kernel cannot send this message; empty is refused.
	Why string `json:"why"`
}

// readMonfileSpec reads a crafted message list. Blank lines and # comments
// carry no message, so a spec can be read as well as generated; every message
// line must name its why, and its length must stay inside rtnl_from_file's
// buffer, so the converter never writes bytes it knows the reader refuses.
func readMonfileSpec(path string) ([]monfileMsg, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var msgs []monfileMsg
	sc := bufio.NewScanner(f)
	// One hex body can be twice the reader's buffer, so the default line
	// limit is not enough.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || text[0] == '#' {
			continue
		}
		var s monfileSpec
		dec := json.NewDecoder(strings.NewReader(text))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&s); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if s.Why == "" {
			return nil, fmt.Errorf("%s:%d: the why is missing", path, line)
		}
		body, err := hex.DecodeString(s.Body)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: body: %w", path, line, err)
		}
		l := xtcpnl.NlMsgHdrSizeCst + len(body)
		if l > monfileBufCst {
			return nil, fmt.Errorf("%s:%d: len %d over rtnl_from_file's %d buffer",
				path, line, l, monfileBufCst)
		}
		msgs = append(msgs, monfileMsg{hdr: xtcpnl.NlMsgHdr{
			Len: uint32(l), Type: s.Type, Flags: s.Flags, Seq: s.Seq, Pid: s.Pid,
		}, body: body})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("%s: no messages", path)
	}
	return msgs, nil
}

// monfileBufCst is rtnl_from_file's buffer, the longest message it reads.
const monfileBufCst = 16384
