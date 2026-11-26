package gotc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/nlmon"
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fakeMonSource is a fakeSource (the main socket: link gets) that can
// subscribe: the subscription answers ll_init_map with dump, then plays
// datagrams through a receive of nlmon.RecvBufCst bytes, as the kernel
// would cut them.
type fakeMonSource struct {
	fakeSource
	dump     Reply
	dgs      [][]byte
	subErr   error
	groups   uint32
	connReqs int
}

func (f *fakeMonSource) Subscribe(groups uint32, _ io.Writer) (MonitorConn, error) {
	f.groups = groups
	if f.subErr != nil {
		return nil, f.subErr
	}
	return &fakeMonConn{f}, nil
}

type fakeMonConn struct{ f *fakeMonSource }

func (c *fakeMonConn) Talk([]byte) (Reply, error) {
	c.f.connReqs++
	return c.f.dump, nil
}

func (c *fakeMonConn) Listen(fn func(nlmon.Msg) error) error {
	for _, dg := range c.f.dgs {
		n := min(len(dg), nlmon.RecvBufCst)
		if err := nlmon.Datagram(dg[:n], len(dg) > n, -1, fn); err != nil {
			return err
		}
	}
	return nil
}

func (c *fakeMonConn) Close() error { return nil }

// nlm is a whole netlink message: header, then body, padded.
func nlm(typ, flags uint16, body []byte) []byte {
	n, _ := cint.U32(nlHeaderLen + len(body))
	b := binary.LittleEndian.AppendUint32(nil, n)
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = binary.LittleEndian.AppendUint16(b, flags)
	b = append(b, make([]byte, 8)...) // seq, pid
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// recordWriter counts the writes that reach it: one per flush.
type recordWriter struct {
	bytes.Buffer
	writes int
}

func (w *recordWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.Buffer.Write(p)
}

const (
	tcHMinIngressCst = 0xFFFFFFF2 // TC_H_MAKE(TC_H_CLSACT, TC_H_MIN_INGRESS): "ingress"
	tcHRootCst       = 0xFFFFFFFF
	monCreate        = unix.NLM_F_CREATE
	monExcl          = unix.NLM_F_EXCL
	monReplace       = unix.NLM_F_REPLACE
)

// TestGotcMonitorUnit holds doTcMonitor, acceptTcmsg and the printers'
// event branches to tc_monitor.c with crafted events, live (a fake
// subscription) and from files.
//
// go test ./internal/gotc/ -run TestGotcMonitorUnit
func TestGotcMonitorUnit(t *testing.T) {
	noqueue := tcBody(2, 0, tcHRootCst, 1, kind("noqueue")) // refcnt 1 is not printed
	noqueueD1 := tcBody(3, 0, tcHRootCst, 1, kind("noqueue"))
	matchall := tcBody(2, 0, tcHMinIngressCst, 2<<16|0x0300, kind("matchall"), attr(xtcpnl.TcaChain, le32(0)))
	chain1 := tcBody(2, 0, 0xFFFF0000, 0, attr(xtcpnl.TcaChain, le32(1)))
	gact := func(index uint32) []byte {
		return cat(make([]byte, xtcpnl.TcaMsgSizeCst), attr(xtcpnl.TcaRootTab,
			actOrder(1, "gact", attr(xtcpnl.TcaGactParms, genB(index, 0, 1, 0)))))
	}
	flushBody := func(orders ...uint16) []byte {
		var tab []byte
		for _, o := range orders {
			tab = cat(tab, nest(o, attr(xtcpnl.TcaKind, str0("gact")), attr(xtcpnl.TcaFcnt, le32(3))))
		}
		return cat(make([]byte, xtcpnl.TcaMsgSizeCst), attr(xtcpnl.TcaRootTab, tab))
	}
	classDel := tcBody(2, 0x10001, 0x10000, 0, kind("htb"))
	big := func(total int) []byte { return nlm(unix.RTM_NEWLINK, 0, make([]byte, total-nlHeaderLen)) }

	const (
		qNoqueue  = "qdisc noqueue 0: dev d0 root \n"
		gactLines = "\taction order 1: gact action pass\n\t random type none pass val 0\n\t index 7 ref 1 bind 0\n"
	)
	stamp := "Timestamp: Wed Nov 25 10:00:00 2026 123 usec\n"
	stampShort := "[2026-11-25T10:00:00.000123] "

	type want struct {
		stdout, stderr string
		rc             int
	}
	tests := []struct {
		description string
		args        []string
		dgs         [][]byte                                   // live events, one datagram each
		file        []byte                                     // written to F, which "F" in args names
		stdin       string                                     // -batch -
		replies     []Reply                                    // the main socket's
		check       func(*fakeMonSource, *recordWriter) string // "" when fine
		expected    want
	}{
		// Positive.
		{"positive: a NEWQDISC event with flags 0 says neither added nor replaced (MO6)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil, nil, want{stdout: qNoqueue}},
		{"positive: DELQDISC and NEWQDISC in one datagram (K20)", []string{"monitor"},
			[][]byte{cat(nlm(unix.RTM_DELQDISC, 0, noqueue), nlm(unix.RTM_NEWQDISC, monReplace, noqueue))}, nil, "", nil, nil,
			want{stdout: "deleted " + qNoqueue + qNoqueue}},
		{"positive: filter add (CREATE|EXCL), replace (CREATE), change (0), delete (MO7)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWTFILTER, monCreate|monExcl, matchall), nlm(unix.RTM_NEWTFILTER, monCreate, matchall),
				nlm(unix.RTM_NEWTFILTER, 0, matchall), nlm(unix.RTM_DELTFILTER, 0, matchall)}, nil, "", nil, nil,
			want{stdout: "added filter dev d0 ingress protocol all pref 2 matchall chain 0 \n" +
				"replaced filter dev d0 ingress protocol all pref 2 matchall chain 0 \n" +
				"filter dev d0 ingress protocol all pref 2 matchall chain 0 \n" +
				"deleted filter dev d0 ingress protocol all pref 2 matchall chain 0 \n"}},
		{"positive: action add, replace, delete and flush, each after total acts 0 (MO9)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWACTION, monCreate|monExcl, gact(7)), nlm(unix.RTM_NEWACTION, monCreate|monReplace, gact(7)),
				nlm(unix.RTM_DELACTION, 0, gact(7)), nlm(unix.RTM_DELACTION, unix.NLM_F_ROOT, flushBody(0))}, nil, "", nil, nil,
			want{stdout: "total acts 0\nAdded action \n" + gactLines + "total acts 0\nReplaced action \n" + gactLines +
				"total acts 0\nDeleted action \n" + gactLines + "total acts 0\nFlushed table  gact (3 entries)\n"}},
		{"positive: NEWCHAIN and DELCHAIN go to print_filter", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWCHAIN, monCreate|monExcl, chain1), nlm(unix.RTM_DELCHAIN, 0, chain1)}, nil, "", nil, nil,
			want{stdout: "added chain dev d0 parent ffff: chain 1 \ndeleted chain dev d0 parent ffff: chain 1 \n"}},
		{"positive: -t puts a line ahead of each event", []string{"-t", "monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil, nil, want{stdout: stamp + qNoqueue}},
		{"positive: -tshort alone stamps, in brackets ahead", []string{"-tshort", "monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil, nil, want{stdout: stampShort + qNoqueue}},
		{"positive: the subscription is RTNLGRP_TC, and ll_init_map runs on it", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil,
			func(f *fakeMonSource, _ *recordWriter) string {
				if f.groups != 0x8 || f.connReqs != 1 || len(f.reqs) != 0 {
					return "groups, subscription requests or main requests wrong"
				}
				return ""
			}, want{stdout: qNoqueue}},
		{"positive: stdout is flushed after each event", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue), nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil,
			func(_ *fakeMonSource, w *recordWriter) string {
				if w.writes != 2 {
					return "not one write per event"
				}
				return ""
			}, want{stdout: qNoqueue + qNoqueue}},
		// Negative.
		{"negative: an unknown word (MO14)", []string{"monitor", "bogus"}, nil, nil, "", nil, nil,
			want{stderr: "Argument \"bogus\" is unknown, try \"tc monitor help\".\n", rc: 255}},
		{"negative: help, whose usage leaves a [ open (MO1)", []string{"monitor", "help"}, nil, nil, "", nil, nil,
			want{stderr: monitorUsage, rc: 255}},
		{"negative: file with no value", []string{"monitor", "file"}, nil, nil, "", nil, nil,
			want{stderr: "Command line is not complete. Try option \"help\"\n", rc: 255}},
		{"negative: no such file", []string{"monitor", "file", "/nonexistent/m.bin"}, nil, nil, "", nil, nil,
			want{stderr: "Cannot fopen: No such file or directory\n", rc: 255}},
		{"negative: a subscription that fails exits 1", []string{"monitor"}, nil, nil, "", nil, nil,
			want{stderr: "subscribe failed\n", rc: 1}},
		{"negative: a file cut inside a record (MO13)", []string{"monitor", "file", "F"}, nil,
			nlm(unix.RTM_NEWQDISC, 0, noqueue)[:30], "", nil, nil,
			want{stderr: "rtnl-from_file: truncated message\n", rc: 255}},
		{"negative: a record of length 8 (MO13)", []string{"monitor", "file", "F"}, nil,
			cat(le32(8), make([]byte, 12)), "", nil, nil, want{stderr: "!!!malformed message: len=8 @16\n", rc: 255}},
		// Boundary.
		{"boundary: a 16385-byte event is cut: Truncated message, exit 2 (MO5)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue), big(16385)}, nil, "", nil, nil,
			want{stdout: qNoqueue, stderr: "Truncated message\n", rc: 2}},
		{"boundary: a 16384-byte message fits", []string{"monitor"}, [][]byte{big(16384)}, nil, "", nil, nil,
			want{stderr: "Unknown message: length 00016384 type 00000010 flags 00000000\n"}},
		{"boundary: an empty file", []string{"monitor", "file", "F"}, nil, []byte{}, "", nil, nil, want{}},
		// Corner.
		{"corner: -j monitor prints text (MO4)", []string{"-j", "monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, 0, noqueue)}, nil, "", nil, nil, want{stdout: qNoqueue}},
		{"corner: -t over a DONE record stamps with nothing after (MO2)", []string{"-t", "monitor", "file", "F"}, nil,
			nlm(unix.NLMSG_DONE, 0, le32(0)), "", nil, nil, want{stdout: stamp}},
		{"corner: an RTM_NEWLINK in a file, its length zero-padded decimal (MO3)", []string{"monitor", "file", "F"}, nil,
			nlm(unix.RTM_NEWLINK, 0, make([]byte, 16)), "", nil, nil,
			want{stderr: "Unknown message: length 00000032 type 00000010 flags 00000000\n"}},
		{"corner: crafted CREATE|EXCL and CREATE|REPLACE qdisc events reach added and replaced (MO6)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, monCreate|monExcl, noqueue), nlm(unix.RTM_NEWQDISC, monCreate|monReplace, noqueue)},
			nil, "", nil, nil, want{stdout: "added " + qNoqueue + "replaced " + qNoqueue}},
		{"corner: EXCL or REPLACE without CREATE is neither added nor replaced (MO6)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_NEWQDISC, monExcl, noqueue), nlm(unix.RTM_NEWQDISC, monReplace, noqueue)},
			nil, "", nil, nil, want{stdout: qNoqueue + qNoqueue}},
		{"corner: after a flush is printed, an order-0 table prints as actions", []string{"monitor"},
			[][]byte{nlm(unix.RTM_DELACTION, unix.NLM_F_ROOT, flushBody(0)), nlm(unix.RTM_NEWACTION, 0,
				cat(make([]byte, xtcpnl.TcaMsgSizeCst), attr(xtcpnl.TcaRootTab, actOrder(0, "gact", attr(xtcpnl.TcaGactParms, genB(7, 0, 1, 0))))))},
			nil, "", nil, nil, want{stdout: "total acts 0\nFlushed table  gact (3 entries)\ntotal acts 0\n\n" +
				strings.Replace(gactLines, "order 1", "order 0", 1)}},
		{"corner: a batch's qdisc show dev d0 filters the file's qdisc events (MO11)", []string{"-batch", "-"}, nil,
			cat(nlm(unix.RTM_NEWQDISC, 0, noqueueD1), nlm(unix.RTM_NEWQDISC, 0, noqueue)), "qdisc show dev d0\nmonitor file F\n",
			[]Reply{links(), rep(done())}, nil, want{stdout: "qdisc noqueue 0: root \n"}},
		{"corner: a class DEL under -j is text; the null branch is unreachable (MO8)", []string{"-j", "monitor"},
			[][]byte{nlm(unix.RTM_DELTCLASS, 0, classDel)}, nil, "", nil, nil,
			want{stdout: "deleted class htb 1:1 dev d0 parent 1: \n"}},
		{"corner: file mode skips ll_init_map; an unknown ifindex is one RTM_GETLINK (MO12)", []string{"monitor", "file", "F"}, nil,
			nlm(unix.RTM_NEWQDISC, 0, noqueue), "", []Reply{rep(linkMsg(2, "x2"))},
			func(f *fakeMonSource, _ *recordWriter) string {
				if len(f.reqs) != 1 || f.connReqs != 0 {
					return "not one link get on the main socket"
				}
				if typ := binary.LittleEndian.Uint16(f.reqs[0][4:6]); typ != unix.RTM_GETLINK {
					return "the request is not RTM_GETLINK"
				}
				return ""
			}, want{stdout: "qdisc noqueue 0: dev x2 root \n"}},
		{"corner: a flush table that is not order 0 alone leaves tab_flush set for the next (MO15)", []string{"monitor"},
			[][]byte{nlm(unix.RTM_DELACTION, unix.NLM_F_ROOT, flushBody(0, 1)), nlm(unix.RTM_NEWACTION, 0, flushBody(0))}, nil, "", nil, nil,
			// In an ordinary table TCA_FCNT (6) reads as TCA_ACT_COOKIE.
			want{stdout: "total acts 0\nFlushed table \n\taction order 0: gact \tcookie 03000000\n\n\taction order 1: gact \tcookie 03000000\n" +
				"total acts 0\n gact (3 entries)\n"}},
	}
	now := time.Date(2026, 11, 25, 10, 0, 0, 123000, time.Local)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeMonSource{fakeSource: fakeSource{replies: tc.replies}, dump: links(), dgs: tc.dgs}
			if strings.HasPrefix(tc.expected.stderr, "subscribe failed") {
				src.subErr = errors.New("subscribe failed")
			}
			args := tc.args
			if tc.file != nil {
				dir := t.TempDir()
				path := filepath.Join(dir, "F")
				if err := os.WriteFile(path, tc.file, 0o600); err != nil {
					t.Fatal(err)
				}
				t.Chdir(dir)
			}
			var out recordWriter
			var errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(tc.stdin), sys: emptySys(t), now: func() time.Time { return now }}
			rc := run(cfg, args, &out, &errb)
			got := want{stdout: out.String(), stderr: errb.String(), rc: rc}
			if got != tc.expected {
				t.Fatalf("tc %q\n got  %#v\n want %#v", args, got, tc.expected)
			}
			if tc.check != nil {
				if msg := tc.check(src, &out); msg != "" {
					t.Error(msg)
				}
			}
		})
	}
	t.Run("negative: a source with no monitor support is refused", func(t *testing.T) {
		var out, errb bytes.Buffer
		rc := run(config{src: &fakeSource{}, sys: emptySys(t)}, []string{"monitor"}, &out, &errb)
		if rc != 255 || !strings.Contains(errb.String(), "not implemented: tc monitor on this source") {
			t.Errorf("rc %d stderr %q", rc, errb.String())
		}
	})
}
