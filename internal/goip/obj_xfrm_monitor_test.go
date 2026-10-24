package goip

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// xfrmMonRun runs `goip xfrm ARGS` over src, d0 in the link cache at
// ifindex d0 when it is not 0, under -s when stats, and -t at a fixed
// time when stamp.
func xfrmMonRun(src Source, d0 int32, stats int, stamp, allNsid bool, args string) xfrmRun {
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.showStats = src, stats
	if d0 != 0 {
		c.lltab.Fill([]xtcpnl.LinkInfo{{Index: d0, Name: "d0"}})
	}
	if stamp {
		c.timestamp, c.timestampShort = 1, 1
	}
	fields := strings.Fields(args)
	if allNsid {
		fields = append(fields, "all-nsid")
	}
	err := runXfrm(c, fields)
	return xfrmRun{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

// TestXfrmMonitorMatchesIp replays the seven C3 monitor captures: every
// XFRM message the kernel sent, the events and the trigger's ACKs, handed
// to the monitor in capture order. stdout must be ip's, the exit status
// ip's. monitor_mapping's trigger printed its ESP packet in hex to the
// same stdout first, which is no line of ip's and is dropped.
//
// go test ./internal/goip/ -run TestXfrmMonitorMatchesIp
func TestXfrmMonitorMatchesIp(t *testing.T) {
	tests := []struct {
		description string
		row         string
		args        string
		exit        int
		text        string // in the error, for a refusal
	}{
		{description: "positive: every group, eleven writes' events: SAs and policies added, updated, deleted, " +
			"flushed, and the default policies", row: "monitor_sa_policy", args: "monitor"},
		{description: "positive: acquire, the policy that asked and the packet's selector, its dev named",
			row: "monitor_acquire", args: "monitor acquire"},
		{description: "positive: expire, an SA's soft and hard EXPIREs", row: "monitor_expire", args: "monitor expire"},
		{description: "positive: expire, a policy's soft and hard POLEXPIREs", row: "monitor_polexpire",
			args: "monitor expire"},
		{description: "positive: aevent, the timer's and the replay's NEWAEs", row: "monitor_aevent",
			args: "monitor aevent"},
		{description: "positive: every group, an SA added and its NAT-T mapping changed", row: "monitor_mapping",
			args: "monitor"},
		{description: "negative: a word it does not know, exit 255", row: "monitor_unknown", args: "monitor bogus",
			exit: ExitMinusOne, text: `Argument "bogus" is unknown, try "ip xfrm monitor help"`},
		{description: "corner: nokeys and all are read and change nothing an event prints",
			row: "monitor_sa_policy", args: "monitor nokeys all"},
		{description: "corner: ac is acquire, not all-nsid", row: "monitor_acquire", args: "monitor ac"},
		{description: "corner: e is expire", row: "monitor_expire", args: "monitor e"},
		{description: "corner: ae is aevent", row: "monitor_aevent", args: "monitor ae"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(tdXfrm, tc.row)
			src, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
			if err != nil {
				t.Fatal(err)
			}
			r := xfrmMonRun(src, xfrmAcquireIfindex(src), 0, false, false, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.text != "" && !strings.Contains(fmt.Sprint(r.err), tc.text) {
				t.Errorf("err %v, want one holding %q", r.err, tc.text)
			}
			want := mustReadFile(t, filepath.Join(dir, "ip.out"))
			if tc.row == "monitor_mapping" {
				_, want, _ = strings.Cut(want, "\n")
			}
			if r.stdout != want {
				t.Errorf("stdout\n got %q\nwant %q", r.stdout, want)
			}
			if r.stderr != "" {
				t.Errorf("stderr %q, want none", r.stderr)
			}
		})
	}
}

// xfrmAcquireIfindex is the selector's ifindex of the capture's ACQUIRE,
// the dev ip named by a route socket's get, which the XFRM capture does
// not hold; 0 for none.
func xfrmAcquireIfindex(src *ReplaySource) int32 {
	for _, m := range src.xmsgs {
		if m.Hdr.Type == xtcpnl.XfrmMsgAcquire && len(m.Body) >= xtcpnl.XfrmUserAcquireSizeCst {
			return int32(binary.LittleEndian.Uint32(m.Body[40+48:]))
		}
	}
	return 0
}

// xfrmMonFake is a source whose XFRM listen hands over msgs.
type xfrmMonFake struct {
	xfrmFake
	msgs              []monitorMsg
	groups            uint32
	allNsid, listened bool
	listenErr         error
}

func (f *xfrmMonFake) XfrmListen(groups uint32, allNsid bool, _ io.Writer, fn func(monitorMsg) error) error {
	f.groups, f.allNsid, f.listened = groups, allNsid, true
	for _, m := range f.msgs {
		if err := fn(m); err != nil {
			return err
		}
	}
	return f.listenErr
}

// xfrmCaptureEvent is the first message the kernel sent of type typ in a
// C3 capture.
func xfrmCaptureEvent(t *testing.T, row string, typ uint16) monitorMsg {
	t.Helper()
	src, err := OpenReplay(filepath.Join(tdXfrm, row, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range src.xmsgs {
		if m.Hdr.Type == typ && m.Hdr.Flags&unix.NLM_F_REQUEST == 0 {
			return monitorMsg{hdr: m.Hdr, body: m.Body, nsid: -1}
		}
	}
	t.Fatalf("%s has no %d", row, typ)
	return monitorMsg{}
}

// xfrmMonMsg is a message of type typ with body.
func xfrmMonMsg(typ uint16, body ...byte) monitorMsg {
	return monitorMsg{hdr: xtcpnl.NlMsgHdr{Type: typ}, body: body, nsid: -1}
}

// TestXfrmMonitorArgs runs the monitor over a fake listen: the groups its
// words subscribe, all-nsid, and each message's text on stdout and
// stderr, the exit status and a fragment of the error.
//
// go test ./internal/goip/ -run TestXfrmMonitorArgs
func TestXfrmMonitorArgs(t *testing.T) {
	newSA := xfrmCaptureEvent(t, "monitor_sa_policy", xtcpnl.XfrmMsgNewSA)
	delPol := xfrmCaptureEvent(t, "monitor_sa_policy", xtcpnl.XfrmMsgDelPolicy)
	acq := xfrmCaptureEvent(t, "monitor_acquire", xtcpnl.XfrmMsgAcquire)
	ae := xfrmCaptureEvent(t, "monitor_aevent", xtcpnl.XfrmMsgNewAE)
	aeFlags := func(flags uint32, proto uint8) monitorMsg {
		m := ae
		m.body = append([]byte(nil), ae.body...)
		binary.LittleEndian.PutUint32(m.body[40:], flags)
		m.body[22] = proto
		return m
	}
	delSANoSA := xfrmMonMsg(xtcpnl.XfrmMsgDelSA, make([]byte, xtcpnl.XfrmUsersaIDSizeCst)...)
	shortFlush := xfrmMonMsg(xtcpnl.XfrmMsgFlushSA)
	nsid5 := newSA
	nsid5.nsid = 5
	acqTCP := acq
	acqTCP.body = append([]byte(nil), acq.body...)
	acqTCP.body[96+44] = unix.IPPROTO_TCP
	acqSPI := acq
	acqSPI.body = append([]byte(nil), acq.body...)
	binary.BigEndian.PutUint32(acqSPI.body[16:], 0x100)
	const all = xfrmMonitorAllCst
	tests := []struct {
		description string
		args        string
		msgs        []monitorMsg
		stats       int
		stamp       bool
		listenErr   error
		groups      uint32 // 0: no listen
		allNsid     bool
		exit        int
		stdout      string // what stdout holds
		stderr      string // stderr
		text        string // a fragment of the error
	}{
		// The words and the groups they subscribe, by XFRMNLGRP_*'s bit.
		{description: "positive: no word is every group", args: "monitor", groups: all},
		{description: "positive: acquire, group 1", args: "monitor acquire", groups: 1},
		{description: "positive: expire, group 2", args: "monitor expire", groups: 2},
		{description: "positive: SA, group 3", args: "monitor SA", groups: 4},
		{description: "positive: policy, group 4", args: "monitor policy", groups: 8},
		{description: "positive: aevent, group 5", args: "monitor aevent", groups: 16},
		{description: "positive: report, group 6", args: "monitor report", groups: 32},
		{description: "positive: two objects, both groups", args: "monitor acquire expire", groups: 3},
		{description: "positive: all with an object is the object's", args: "monitor all policy", groups: 8},
		{description: "positive: all-nsid, every group", args: "monitor all-nsid", groups: all, allNsid: true},
		{description: "corner: a is all-nsid, ahead of acquire and aevent", args: "monitor a", groups: all,
			allNsid: true},
		{description: "corner: p is policy, r report, S SA", args: "monitor p r S", groups: 8 | 32 | 4},
		{description: "boundary: nokeys alone is every group", args: "monitor nokeys", groups: all},
		{description: "negative: sa, lowercase, is no word", args: "monitor sa", exit: ExitMinusOne,
			text: `Argument "sa" is unknown`},
		{description: "negative: help is the usage, exit 255", args: "monitor help", exit: ExitMinusOne,
			text: "Usage: ip xfrm monitor [ nokeys ]"},
		{description: "negative: h is help", args: "monitor h", exit: ExitMinusOne, text: "OBJECTS := { acquire"},
		{description: "negative: file with no name", args: "monitor file", exit: ExitMinusOne,
			text: "Command line is not complete"},
		{description: "negative: a file that is not there, exit 255", args: "monitor file /nonexistent/x",
			exit: ExitMinusOne, text: "Cannot fopen"},
		{description: "negative: a listen error, exit 2", args: "monitor", groups: all, listenErr: unix.ENOBUFS,
			exit: ExitFailure},

		// Each message's printer.
		{description: "positive: a FLUSHSA of esp", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgFlushSA, unix.IPPROTO_ESP, 0, 0, 0)},
			stdout: "Flushed state proto esp\n"},
		{description: "boundary: a FLUSHSA of 0, its number", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgFlushSA, 0, 0, 0, 0)}, stdout: "Flushed state proto 0\n"},
		{description: "negative: a FLUSHSA short of its struct is BUG on stderr, and the listen goes on",
			args: "monitor", groups: all, msgs: []monitorMsg{shortFlush, xfrmMonMsg(xtcpnl.XfrmMsgFlushPolicy)},
			stdout: "Flushed policy \n", stderr: "BUG: wrong nlmsg len -4\n"},
		{description: "boundary: a FLUSHSA of one byte, short of its aligned struct", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgFlushSA, unix.IPPROTO_ESP)}, stderr: "BUG: wrong nlmsg len -3\n"},
		{description: "positive: a FLUSHPOLICY with a policy type", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgFlushPolicy, 10, 0, 16, 0, 1, 0, 0, 0, 0, 0, 0, 0)},
			stdout: "Flushed policy ptype sub \n"},
		{description: "positive: the default policies", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgGetDefault, 1, 2, 0, 0)},
			stdout: "Default policies:\n in:  block\n fwd: accept\n out: unspec\n"},
		{description: "negative: a GETDEFAULT short of its struct is BUG on stderr", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgGetDefault, 1, 2, 0)},
			stderr: "BUG: short nlmsg len 19 (expect 20) for XFRM_MSG_GETDEFAULT\n"},
		{description: "corner: another type is Unknown message, its length decimal", args: "monitor", groups: all,
			msgs:   []monitorMsg{{hdr: xtcpnl.NlMsgHdr{Type: 99, Flags: 2}, body: make([]byte, 4), nsid: -1}},
			stdout: "Unknown message: 00000020 0x00000063 0x00000002\n"},
		{description: "corner: an ERROR, a NOOP and a DONE print nothing", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(unix.NLMSG_ERROR, make([]byte, 20)...), xfrmMonMsg(unix.NLMSG_NOOP),
				xfrmMonMsg(unix.NLMSG_DONE, 0, 0, 0, 0)}},
		{description: "negative: a REPORT is not implemented", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgReport, make([]byte, 60)...)}, exit: ExitUsage,
			text: "REPORT"},
		{description: "positive: an async event's three flags", args: "monitor", groups: all,
			msgs:   []monitorMsg{aeFlags(0x70, unix.IPPROTO_ESP)},
			stdout: "Async event  (0x70)  replay update  timer expired  policy updated \n\tsrc 192.0.2.1 "},
		{description: "boundary: an async event with no flags", args: "monitor", groups: all,
			msgs: []monitorMsg{aeFlags(0, unix.IPPROTO_AH)}, stdout: "Async event  (0x0) \n\tsrc 192.0.2.1 dst " +
				"192.0.2.2  reqid 0x1 protocol ah  SPI 0x100\n"},
		{description: "negative: an async event's protocol goip cannot name", args: "monitor", groups: all,
			msgs: []monitorMsg{aeFlags(0x10, 108)}, exit: ExitUsage, text: "protocol 108"},
		{description: "negative: an async event short of its struct is not implemented", args: "monitor",
			groups: all, msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgNewAE, 1, 2)}, exit: ExitUsage,
			text: "an async event"},
		{description: "negative: a mapping change short of its struct is not implemented", args: "monitor",
			groups: all, msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgMapping, 1, 2)}, exit: ExitUsage,
			text: "a mapping change"},
		{description: "positive: an acquire's SPI and, under -s, its sequence and masks", args: "monitor",
			groups: all, msgs: []monitorMsg{acqSPI}, stats: 1,
			stdout: "acquire proto esp spi 0x00000100(256) \n"},
		{description: "positive: under -s, an acquire's last line", args: "monitor", groups: all,
			msgs: []monitorMsg{acq}, stats: 1, stdout: "  seq 0x"},
		{description: "boundary: an acquire of its struct alone prints", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgAcquire, make([]byte, xtcpnl.XfrmUserAcquireSizeCst)...)},
			stdout: "acquire proto 0 \n"},
		{description: "corner: an acquire whose policy's selector has another protocol, each named",
			args: "monitor", groups: all, msgs: []monitorMsg{acqTCP},
			stdout: "proto udp sport 37132 dport 9 dev if"},
		{description: "corner: and the policy's selector names tcp", args: "monitor", groups: all,
			msgs: []monitorMsg{acqTCP}, stdout: "  policy src 192.0.2.1/32 dst 192.0.2.2/32 proto tcp \n"},
		{description: "negative: an acquire short of its struct is BUG on stderr", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgAcquire, make([]byte, 279)...)}, stderr: "BUG: wrong nlmsg len -1\n"},
		{description: "negative: a DELSA with no XFRMA_SA is Deleted, then Buggy on stderr", args: "monitor",
			groups: all, msgs: []monitorMsg{delSANoSA}, stdout: "Deleted ",
			stderr: "Buggy XFRM_MSG_DELSA: no XFRMA_SA\n"},
		{description: "positive: a DELPOLICY prints its XFRMA_POLICY", args: "monitor", groups: all,
			msgs: []monitorMsg{delPol}, stdout: "Deleted src 192.0.2.1/32 dst 192.0.2.2/32 \n\tdir out priority 5 \n"},
		{description: "negative: an EXPIRE short of its struct is BUG on stderr", args: "monitor", groups: all,
			msgs: []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgExpire, make([]byte, 231)...)}, stderr: "BUG: wrong nlmsg len -1\n"},
		{description: "negative: a POLEXPIRE short of its struct is BUG on stderr", args: "monitor", groups: all,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgPolExpire, make([]byte, 175)...)},
			stderr: "BUG: wrong nlmsg len -1\n"},

		// The headers.
		{description: "positive: all-nsid, the current netns", args: "monitor all-nsid", groups: all, allNsid: true,
			msgs: []monitorMsg{newSA}, stdout: "[nsid current]src 192.0.2.1 dst 192.0.2.2\n"},
		{description: "positive: all-nsid, another netns's id", args: "monitor all-nsid", groups: all,
			allNsid: true, msgs: []monitorMsg{nsid5}, stdout: "[nsid 5]src 192.0.2.1"},
		{description: "positive: -ts, the time ahead of the message", args: "monitor", groups: all, stamp: true,
			msgs:   []monitorMsg{xfrmMonMsg(xtcpnl.XfrmMsgFlushSA, 0, 0, 0, 0)},
			stdout: "[2026-10-24T12:00:00.000007] Flushed state proto 0\n"},
		{description: "corner: an ERROR still gets its headers", args: "monitor all-nsid", groups: all,
			allNsid: true, msgs: []monitorMsg{xfrmMonMsg(unix.NLMSG_ERROR, make([]byte, 20)...)},
			stdout: "[nsid current]"},
	}
	defer func(f func() time.Time) { xfrmMonitorNow = f }(xfrmMonitorNow)
	xfrmMonitorNow = func() time.Time { return time.Date(2026, 10, 24, 12, 0, 0, 7000, time.Local) }
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f := &xfrmMonFake{msgs: tc.msgs, listenErr: tc.listenErr}
			r := xfrmMonRun(f, 0, tc.stats, tc.stamp, false, tc.args)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if tc.text != "" && !strings.Contains(fmt.Sprint(r.err), tc.text) {
				t.Errorf("err %v, want one holding %q", r.err, tc.text)
			}
			if f.listened != (tc.groups != 0) || f.groups != tc.groups || f.allNsid != tc.allNsid {
				t.Errorf("listened %v groups %#x all-nsid %v, want groups %#x all-nsid %v", f.listened, f.groups,
					f.allNsid, tc.groups, tc.allNsid)
			}
			if !strings.Contains(r.stdout, tc.stdout) || tc.stdout == "" && r.stdout != "" {
				t.Errorf("stdout %q, want it to hold %q", r.stdout, tc.stdout)
			}
			if r.stderr != tc.stderr {
				t.Errorf("stderr %q, want %q", r.stderr, tc.stderr)
			}
		})
	}
}

// TestXfrmMonitorFile walks files of netlink messages, as `ip xfrm
// monitor file` reads them: no listen, each message printed, and the walk
// refused on a message cut short.
//
// go test ./internal/goip/ -run TestXfrmMonitorFile
func TestXfrmMonitorFile(t *testing.T) {
	msg := func(typ uint16, body ...byte) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(xtcpnl.NlMsgHdrSizeCst+len(body)))
		b = binary.LittleEndian.AppendUint16(b, typ)
		b = append(b, make([]byte, 10)...)
		return append(b, body...)
	}
	flush := msg(xtcpnl.XfrmMsgFlushSA, unix.IPPROTO_AH, 0, 0, 0)
	def := msg(xtcpnl.XfrmMsgGetDefault, 2, 2, 1, 0)
	tests := []struct {
		description string
		data        []byte
		allNsid     bool
		exit        int
		expected    string // stdout
	}{
		{description: "positive: a FLUSHSA, then the default policies", data: append(append([]byte(nil), flush...),
			def...), expected: "Flushed state proto ah\nDefault policies:\n in:  accept\n fwd: accept\n out: block\n"},
		{description: "boundary: an empty file prints nothing", data: nil},
		{description: "negative: a header cut short ends the walk, exit 1", data: append(append([]byte(nil),
			flush...), 1, 2, 3), exit: ExitUsage, expected: "Flushed state proto ah\n"},
		{description: "corner: all-nsid reads a file as the current netns's", data: flush, allNsid: true,
			expected: "[nsid current]Flushed state proto ah\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "msgs")
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			f := &xfrmMonFake{}
			r := xfrmMonRun(f, 0, 0, false, tc.allNsid, "monitor file "+path)
			if code := exitCodeOf(r.err); r.err != nil && code != tc.exit || r.err == nil && tc.exit != 0 {
				t.Fatalf("exit %d (%v), want %d", code, r.err, tc.exit)
			}
			if f.listened {
				t.Error("listened, want the file alone")
			}
			if r.stdout != tc.expected {
				t.Errorf("stdout %q, want %q", r.stdout, tc.expected)
			}
		})
	}
}
