//go:build gotc_write

package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

const tdTcqwrite = "testdata/7_3_1/ws/tcqwrite/"

// tcWriteCaptured is the first qdisc or class write in a tcqwrite row's
// capture: the request tc 7.2.0 sent.
func tcWriteCaptured(t testing.TB, id string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, tdTcqwrite+id+"/ip.pcap") {
		if m.h.Flags&unix.NLM_F_REQUEST == 0 || !IsAdmittedWriteType(m.h.Type) {
			continue
		}
		b := make([]byte, NlMsgHdrSizeCst, NlMsgHdrSizeCst+len(m.body))
		binary.LittleEndian.PutUint32(b[0:], uint32(NlMsgHdrSizeCst+len(m.body)))
		binary.LittleEndian.PutUint16(b[4:], m.h.Type)
		binary.LittleEndian.PutUint16(b[6:], m.h.Flags)
		return append(b, m.body...)
	}
	t.Fatalf("%s: no tc write in the capture", id)
	return nil
}

// tcSameRequest compares two requests with seq and pid left out.
func tcSameRequest(got, want []byte) bool {
	if len(got) != len(want) || len(got) < NlMsgHdrSizeCst {
		return false
	}
	return bytes.Equal(got[:8], want[:8]) && bytes.Equal(got[NlMsgHdrSizeCst:], want[NlMsgHdrSizeCst:])
}

// tcCapturedIfindex is the tcmsg ifindex of a captured request: d0's index
// in its row's namespace.
func tcCapturedIfindex(req []byte) int32 {
	return int32(binary.LittleEndian.Uint32(req[NlMsgHdrSizeCst+4:]))
}

// tcCapturedStabData is the TCA_STAB_DATA payload of a captured request,
// so the stab case checks the nest's layout with the table tc computed
// (the table's values are internal/gotc's TestTcEstimatorTables).
func tcCapturedStabData(t *testing.T, req []byte) []uint16 {
	t.Helper()
	attrs := req[NlMsgHdrSizeCst+TcMsgSizeCst:]
	for len(attrs) >= RTAttrSizeCst {
		l := int(binary.LittleEndian.Uint16(attrs))
		typ := binary.LittleEndian.Uint16(attrs[2:])
		if typ == TcaStab {
			in := attrs[RTAttrSizeCst:l]
			for len(in) >= RTAttrSizeCst {
				il := int(binary.LittleEndian.Uint16(in))
				if binary.LittleEndian.Uint16(in[2:]) == TcaStabData {
					p := in[RTAttrSizeCst:il]
					out := make([]uint16, len(p)/2)
					for i := range out {
						out[i] = binary.LittleEndian.Uint16(p[2*i:])
					}
					return out
				}
				in = in[il+FourByteAlignPadding(il):]
			}
		}
		attrs = attrs[l+FourByteAlignPadding(l):]
	}
	t.Fatal("no TCA_STAB_DATA")
	return nil
}

func tcOpts(v []byte) func(*AttrBuilder) error {
	return func(ab *AttrBuilder) error { return ab.PutBytes(TcaOptions, v) }
}

const (
	tcAdd     = unix.NLM_F_EXCL | unix.NLM_F_CREATE | unix.NLM_F_ACK
	tcReplace = unix.NLM_F_CREATE | unix.NLM_F_REPLACE | unix.NLM_F_ACK
	tcLink    = unix.NLM_F_REPLACE | unix.NLM_F_ACK
	tcChange  = unix.NLM_F_ACK
	tcRoot    = 0xffffffff
)

// TestBuildTcWrite is BuildTcWriteRequest against the requests tc sent for
// tcqwrite rows: the frame (tcmsg, kind, estimator, blocks, stab) and
// each verb's flags, with a kind's options given as tc encoded them.
//
// go test -tags gotc_write ./pkg/xtcpnl/ -run TestBuildTcWrite
func TestBuildTcWrite(t *testing.T) {
	fifo100 := TcFifoQopt{Limit: 100}.Append(nil)
	prio := TcPrioQopt{Bands: 3, Priomap: [16]uint8{0, 1, 2, 2, 1, 2, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1}}.Append(nil)
	tests := []struct {
		description string
		id          string
		w           TcWrite
		stabData    bool // take TCA_STAB_DATA from the capture
	}{
		{"positive: add pfifo limit 100", "ec-002", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", Opts: tcOpts(fifo100)}, false},
		{"positive: change is ACK alone", "ec-003", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcChange,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", Opts: tcOpts(fifo100)}, false},
		{"positive: replace is CREATE|REPLACE", "ec-004", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcReplace,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", Opts: tcOpts(fifo100)}, false},
		{"positive: link is REPLACE alone", "ec-005", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcLink,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", Opts: tcOpts(fifo100)}, false},
		{"positive: delete root has no attributes", "ec-006", TcWrite{MsgType: unix.RTM_DELQDISC,
			Flags: unix.NLM_F_ACK, Parent: tcRoot}, false},
		{"positive: prio is a compat nest", "ec-020", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "prio", Opts: func(ab *AttrBuilder) error {
				c, err := ab.BeginNestCompat(TcaOptions, prio)
				if err != nil {
					return err
				}
				return ab.EndNestCompat(c)
			}}, false},
		// ingress_parse_opt and clsact_parse_opt only check argv; neither
		// writes TCA_OPTIONS.
		{"positive: ingress sends no TCA_OPTIONS", "ec-073", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0xffff0000, Parent: 0xfffffff1, Kind: "ingress"}, false},
		// TC_H_CLSACT is TC_H_INGRESS (pkt_sched.h), so clsact's handle and
		// parent are ingress's.
		{"positive: clsact with both blocks", "ec-077", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0xffff0000, Parent: 0xfffffff1, Kind: "clsact", IngressBlock: 5, EgressBlock: 6}, false},
		{"positive: estimator 1s 8s", "ec-081", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", Est: TcEstimator{Interval: 0, EwmaLog: 3}}, false},
		{"positive: stab atm with a table", "ec-083", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", HasStab: true,
			Stab: TcSizespec{CellLog: 5, CellAlign: -1, Overhead: 20, Linklayer: 2, Mpu: 64, Mtu: 2048, Tsize: 128}}, true},
		// tc_calc_size_table zeroes mtu, tsize, cell_log and cell_align and
		// leaves linklayer as parse_size_table left it: 0 when none was given.
		{"boundary: stab of overhead alone has no DATA", "ec-084", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo", HasStab: true,
			Stab: TcSizespec{Overhead: 4}}, false},
		{"corner: handle 5: is overwritten by ingress (W2)", "ec-079", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0xffff0000, Parent: 0xfffffff1, Kind: "ingress"}, false},
		{"corner: handle 0: handle 1: sends 1: (W3)", "ec-089", TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd,
			Handle: 0x10000, Parent: tcRoot, Kind: "pfifo"}, false},
		{"positive: class del", "ec-098", TcWrite{MsgType: unix.RTM_DELTCLASS, Flags: unix.NLM_F_ACK,
			Handle: 0x10010}, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := tcWriteCaptured(t, tc.id)
			w := tc.w
			w.Ifindex = tcCapturedIfindex(want)
			if tc.stabData {
				w.StabData = tcCapturedStabData(t, want)
			}
			got, err := BuildTcWriteRequest(&w, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !tcSameRequest(got, want) {
				t.Errorf("request differs\n got %s\nwant %s", hex.EncodeToString(got), hex.EncodeToString(want))
			}
		})
	}
}

// TestBuildTcWriteRefusals is the builder's own refusals.
func TestBuildTcWriteRefusals(t *testing.T) {
	big := make([]byte, TcClassWriteBufCst)
	tests := []struct {
		description string
		w           TcWrite
		expected    error
	}{
		{"negative: a route type is not a tc write", TcWrite{MsgType: unix.RTM_NEWROUTE}, ErrTcWriteType},
		{"negative: a GET is not a tc write", TcWrite{MsgType: unix.RTM_GETQDISC}, ErrTcWriteType},
		{"negative: DATA that is not tsize entries", TcWrite{MsgType: unix.RTM_NEWQDISC, HasStab: true,
			Stab: TcSizespec{Tsize: 4}, StabData: []uint16{1, 2}}, ErrTcWriteStab},
		{"boundary: a class write past its 4096 bytes (W6)", TcWrite{MsgType: unix.RTM_NEWTCLASS,
			Kind: "htb", Opts: tcOpts(big)}, ErrAttrNoSpace},
		{"boundary: the same options fit a qdisc write", TcWrite{MsgType: unix.RTM_NEWQDISC,
			Kind: "htb", Opts: tcOpts(big)}, nil},
		{"corner: stab on a class is not sent", TcWrite{MsgType: unix.RTM_NEWTCLASS, HasStab: true,
			Stab: TcSizespec{Tsize: 4}, StabData: []uint16{1, 2}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, err := BuildTcWriteRequest(&tc.w, 0)
			if !errors.Is(err, tc.expected) {
				t.Errorf("err %v, want %v", err, tc.expected)
			}
		})
	}
}

// TestTcWriteAdmitted is the four tc write types through the write gate.
func TestTcWriteAdmitted(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		expected    bool
	}{
		{"positive: RTM_NEWQDISC", unix.RTM_NEWQDISC, true},
		{"positive: RTM_DELQDISC", unix.RTM_DELQDISC, true},
		{"positive: RTM_NEWTCLASS", unix.RTM_NEWTCLASS, true},
		{"positive: RTM_DELTCLASS", unix.RTM_DELTCLASS, true},
		{"negative: RTM_GETQDISC", unix.RTM_GETQDISC, false},
		{"negative: RTM_NEWTFILTER is not yet", unix.RTM_NEWTFILTER, false},
		{"boundary: RTM_NEWCHAIN is not yet", unix.RTM_NEWCHAIN, false},
		{"corner: RTM_NEWACTION is not yet", unix.RTM_NEWACTION, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsAdmittedWriteType(tc.msgType); got != tc.expected {
				t.Errorf("IsAdmittedWriteType(%d) = %t, want %t", tc.msgType, got, tc.expected)
			}
		})
	}
}

// TestBuildTcWriteAllocs pins the allocations per request: the attribute
// buffer and the laid-out message.
func TestBuildTcWriteAllocs(t *testing.T) {
	fifo := TcFifoQopt{Limit: 100}.Append(nil)
	w := TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd, Ifindex: 2, Handle: 0x10000, Parent: tcRoot,
		Kind: "pfifo", Est: TcEstimator{EwmaLog: 3}, Opts: tcOpts(fifo)}
	n := testing.AllocsPerRun(100, func() {
		if _, err := BuildTcWriteRequest(&w, 1); err != nil {
			t.Fatal(err)
		}
	})
	if n > 2 {
		t.Errorf("%v allocations per request, want at most 2", n)
	}
}

// BenchmarkBuildTcWrite is a pfifo add with an estimator.
//
// go test -tags gotc_write ./pkg/xtcpnl/ -run '^$' -bench BuildTcWrite -benchmem
func BenchmarkBuildTcWrite(b *testing.B) {
	fifo := TcFifoQopt{Limit: 100}.Append(nil)
	w := TcWrite{MsgType: unix.RTM_NEWQDISC, Flags: tcAdd, Ifindex: 2, Handle: 0x10000, Parent: tcRoot,
		Kind: "pfifo", Est: TcEstimator{EwmaLog: 3}, Opts: tcOpts(fifo)}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := BuildTcWriteRequest(&w, 1); err != nil {
			b.Fatal(err)
		}
	}
}
