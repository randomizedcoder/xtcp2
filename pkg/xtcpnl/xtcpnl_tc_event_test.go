package xtcpnl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestTcEventKind holds TcEventKind to accept_tcmsg (tc/tc_monitor.c).
//
// go test ./pkg/xtcpnl/ -run TestTcEventKind
func TestTcEventKind(t *testing.T) {
	tests := []struct {
		description string
		typ         uint16
		expected    string // object ("not tc" for none), then "del" for a delete
	}{
		{"positive: RTM_NEWQDISC", unix.RTM_NEWQDISC, "qdisc"},
		{"positive: RTM_DELQDISC", unix.RTM_DELQDISC, "qdisc del"},
		{"positive: RTM_NEWTCLASS", unix.RTM_NEWTCLASS, "class"},
		{"positive: RTM_DELTCLASS", unix.RTM_DELTCLASS, "class del"},
		{"positive: RTM_NEWTFILTER", unix.RTM_NEWTFILTER, "filter"},
		{"positive: RTM_DELTFILTER", unix.RTM_DELTFILTER, "filter del"},
		{"positive: RTM_NEWCHAIN", unix.RTM_NEWCHAIN, "chain"},
		{"positive: RTM_DELCHAIN", unix.RTM_DELCHAIN, "chain del"},
		{"positive: RTM_NEWACTION", unix.RTM_NEWACTION, "action"},
		{"positive: RTM_DELACTION", unix.RTM_DELACTION, "action del"},
		{"negative: RTM_NEWLINK", unix.RTM_NEWLINK, "not tc"},
		{"negative: NLMSG_DONE", unix.NLMSG_DONE, "not tc"},
		{"negative: NLMSG_ERROR", unix.NLMSG_ERROR, "not tc"},
		{"negative: RTM_GETQDISC is Unknown message in tc", unix.RTM_GETQDISC, "not tc"},
		{"negative: RTM_GETTCLASS", unix.RTM_GETTCLASS, "not tc"},
		{"negative: RTM_GETTFILTER", unix.RTM_GETTFILTER, "not tc"},
		{"boundary: RTM_GETCHAIN (102), the highest tc type, is not an event", unix.RTM_GETCHAIN, "not tc"},
		{"boundary: RTM_NEWQDISC - 1 (RTM_SETROUTE slot 35)", unix.RTM_NEWQDISC - 1, "not tc"},
		{"boundary: RTM_GETACTION + 1 (51)", unix.RTM_GETACTION + 1, "not tc"},
		{"boundary: the largest type", 0xffff, "not tc"},
		{"corner: RTM_GETACTION reaches print_action", unix.RTM_GETACTION, "action"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			obj, del := TcEventKind(tc.typ)
			got := obj.String()
			if del {
				got += " del"
			}
			if got != tc.expected {
				t.Errorf("TcEventKind(%d) = %q, want %q", tc.typ, got, tc.expected)
			}
		})
	}
	if s := TcObj(200).String(); s != "TcObj(?)" {
		t.Errorf("TcObj(200) = %q", s)
	}
}

// tcEvent is one notification a tc monitor was sent.
type tcEvent struct {
	h    NlMsgHdr
	body []byte
}

// tcMonEvents is what of a monitor row's capture its subscriber was sent:
// tc events, less the trigger's requests (NLM_F_REQUEST with pid 0; the
// kernel's filter and action events carry NLM_F_REQUEST too, with the
// requester's pid, K22), the startup link dump and the acks. It is
// internal/nlmon.Notifications without the get and echo cases, which no
// tcmonitor row has (that package imports this one).
func tcMonEvents(msgs []capturedNlMsg) []tcEvent {
	var out []tcEvent
	for _, m := range msgs {
		h := m.h
		obj, _ := TcEventKind(h.Type)
		if m.proto != unix.NETLINK_ROUTE || obj == TcObjNone || h.Flags&unix.NLM_F_MULTI != 0 ||
			(h.Flags&unix.NLM_F_REQUEST != 0 && h.Pid == 0) {
			continue
		}
		out = append(out, tcEvent{h, m.body})
	}
	return out
}

// tcEventDecode decodes an event as far as gotc's printers read it: the
// header and top-level attributes (O6), then the kind's options, a
// matchall's or bpf's actions, and each action's options.
func tcEventDecode(typ uint16, body []byte) error {
	obj, _ := TcEventKind(typ)
	errs := []error{wsTcMsgProblem(typ, body)}
	switch obj {
	case TcObjNone:
		return nil
	case TcObjQdisc, TcObjClass, TcObjFilter, TcObjChain:
	case TcObjAction:
		ai, err := ParseTcAction(body)
		if err != nil {
			return err
		}
		for _, a := range ai.Actions {
			errs = append(errs, tcActOptDecode(a.Kind, a.Options))
		}
		return errors.Join(errs...)
	}
	ti, err := ParseTcMsg(body)
	if err != nil {
		return err
	}
	if ti.HasStats2 {
		_, err := ParseTcStats2(ti.Stats2)
		errs = append(errs, err)
	}
	if !ti.HasOptions {
		return errors.Join(errs...)
	}
	switch obj {
	case TcObjNone, TcObjChain, TcObjAction:
	case TcObjQdisc, TcObjClass:
		_, err := ParseTcQopt(ti.Kind, ti.Options)
		errs = append(errs, err)
	case TcObjFilter:
		f, err := ParseTcFopt(ti.Kind, ti.Options)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		act := map[string]uint16{TcFKindMatchall: TcaMatchallAct, TcFKindBpf: TcaBpfAct, TcFKindFw: TcaFwAct}[ti.Kind]
		if b := f.Tb.Bytes(act); act != 0 && b != nil {
			tab, err := ParseTcActTab(b)
			if err != nil {
				return errors.Join(append(errs, err)...)
			}
			for o := uint16(0); int(o) < tab.Len(); o++ {
				nest := tab.Bytes(o)
				if nest == nil {
					continue
				}
				one, err := ParseTcActOne(nest)
				if err != nil {
					errs = append(errs, err)
					continue
				}
				kind, _ := one.Str(TcaActKind)
				errs = append(errs, tcActOptDecode(kind, one.Bytes(TcaActOptions)))
			}
		}
	}
	return errors.Join(errs...)
}

// tcActOptDecode decodes the options of the tcmonitor space's two action
// kinds.
func tcActOptDecode(kind string, opt []byte) error {
	if opt == nil {
		return nil
	}
	var err error
	switch kind {
	case TcActKindGact:
		_, err = ParseTcGact(opt)
	case TcActKindMirred:
		_, err = ParseTcMirred(opt)
	}
	return err
}

// tcMonFileRecs walks an rtnl_from_file record file.
func tcMonFileRecs(t testing.TB, path string) []tcEvent {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []tcEvent
	for len(b) >= NlMsgHdrSizeCst {
		var h NlMsgHdr
		if _, err := DeserializeNlMsgHdr(b, &h); err != nil {
			t.Fatal(err)
		}
		l := int(h.Len)
		if l < NlMsgHdrSizeCst || l > len(b) {
			t.Fatalf("%s: record of %d bytes in %d", path, l, len(b))
		}
		out = append(out, tcEvent{h, b[NlMsgHdrSizeCst:l]})
		b = b[min((l+3)&^3, len(b)):]
	}
	return out
}

// TestWsTcMonitor decodes every event in the tcmonitor captures (gotc
// 14P), live and in the record files, and pins what they show of the
// kernel: K20, K22, K23 and MO5's sizes.
//
// go test ./pkg/xtcpnl/ -run TestWsTcMonitor
func TestWsTcMonitor(t *testing.T) {
	dir := filepath.Join("testdata", "7_3_1", "ws", "tcmonitor")
	rows, err := filepath.Glob(filepath.Join(dir, "*", "cmd.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 27 {
		t.Fatalf("%d tcmonitor rows, want 27", len(rows))
	}
	seen := map[string]int{}
	events := map[string][]tcEvent{}
	datagrams := map[string][][]capturedNlMsg{}
	for _, cj := range rows {
		rd := filepath.Dir(cj)
		id := filepath.Base(rd)
		datagrams[id] = pcapNlDatagrams(t, filepath.Join(rd, "ip.pcap"))
		evs := tcMonEvents(pcapNlMsgs(t, filepath.Join(rd, "ip.pcap")))
		if _, err := os.Stat(filepath.Join(rd, "m.bin")); err == nil && id[:2] == "ec" {
			evs = append(evs, tcMonFileRecs(t, filepath.Join(rd, "m.bin"))...)
		}
		events[id] = evs
		for i, e := range evs {
			obj, del := TcEventKind(e.h.Type)
			if obj == TcObjNone {
				continue
			}
			if err := tcEventDecode(e.h.Type, e.body); err != nil {
				t.Errorf("%s event %d (type %d): %v", id, i, e.h.Type, err)
			}
			seen[fmt.Sprintf("%s del=%t", obj, del)]++
		}
	}

	evOf := func(id string, typ uint16) []tcEvent {
		var out []tcEvent
		for _, e := range events[id] {
			if e.h.Type == typ {
				out = append(out, e)
			}
		}
		return out
	}
	tests := []struct {
		description string
		check       func() error
		expected    string // "" for no problem
	}{
		{"positive: NEW and DEL of all five objects decode", func() error {
			var errs []error
			for _, o := range []TcObj{TcObjQdisc, TcObjClass, TcObjFilter, TcObjChain, TcObjAction} {
				for _, del := range []bool{false, true} {
					if seen[fmt.Sprintf("%s del=%t", o, del)] == 0 {
						errs = append(errs, fmt.Errorf("no %s event with del=%t", o, del))
					}
				}
			}
			return errors.Join(errs...)
		}, ""},
		{"positive: K22, a filter event carries NLM_F_REQUEST and the requester's pid (ec-003)", func() error {
			f := evOf("ec-003", unix.RTM_NEWTFILTER)
			if len(f) == 0 {
				return errors.New("no filter event")
			}
			if h := f[0].h; h.Flags != 0x605 || h.Pid == 0 {
				return fmt.Errorf("flags %#x pid %d, want 0x605 and nonzero", h.Flags, h.Pid)
			}
			return nil
		}, ""},
		{"positive: K20, a qdisc on the noqueue root comes as DELQDISC then NEWQDISC in one datagram (ec-001)", func() error {
			for _, d := range datagrams["ec-001"] {
				for i := 1; i < len(d); i++ {
					if d[i-1].h.Type == unix.RTM_DELQDISC && d[i].h.Type == unix.RTM_NEWQDISC {
						return nil
					}
				}
			}
			return errors.New("no datagram holds DELQDISC then NEWQDISC")
		}, ""},
		{"negative: K23, a matchall add's event has handle 0 and no options (ec-004)", func() error {
			for _, e := range evOf("ec-004", unix.RTM_NEWTFILTER) {
				ti, err := ParseTcMsg(e.body)
				if err != nil {
					return err
				}
				if ti.Kind == TcFKindMatchall {
					if ti.Hdr.Handle != 0 || ti.HasOptions {
						return fmt.Errorf("handle %#x options %t: K23 fixed?", ti.Hdr.Handle, ti.HasOptions)
					}
					return nil
				}
			}
			return errors.New("no matchall add event")
		}, ""},
		{"boundary: MO5, a 2038-op bpf filter's event is 16380 bytes (ec-012)", func() error {
			return tcBpfEventLen(evOf("ec-012", unix.RTM_NEWTFILTER), 16380)
		}, ""},
		{"boundary: MO5, a 2040-op bpf filter's event is 16396 bytes, past 16384 (ng-001)", func() error {
			return tcBpfEventLen(evOf("ng-001", unix.RTM_NEWTFILTER), 16396)
		}, ""},
		{"corner: the record files' six events decode (ec-013)", func() error {
			if n := len(events["ec-013"]); n != 6 {
				return fmt.Errorf("%d events, want 6", n)
			}
			return nil
		}, ""},
		{"corner: the non-event file holds no tc event (ec-018)", func() error {
			for _, e := range events["ec-018"] {
				if obj, _ := TcEventKind(e.h.Type); obj != TcObjNone {
					return fmt.Errorf("type %d is a %s event", e.h.Type, obj)
				}
			}
			if len(events["ec-018"]) != 4 {
				return fmt.Errorf("%d records, want 4", len(events["ec-018"]))
			}
			return nil
		}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := tc.check()
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
	t.Logf("events %v", seen)
}

// tcBpfEventLen wants a bpf filter add event of n bytes among evs.
func tcBpfEventLen(evs []tcEvent, n uint32) error {
	for _, e := range evs {
		ti, err := ParseTcMsg(e.body)
		if err != nil {
			return err
		}
		if ti.Kind == TcFKindBpf {
			if e.h.Len != n {
				return fmt.Errorf("bpf event of %d bytes, want %d", e.h.Len, n)
			}
			return nil
		}
	}
	return errors.New("no bpf filter event")
}

// BenchmarkWsTcMonitor decodes every event of the tcmonitor corpus per
// iteration.
//
// go test ./pkg/xtcpnl/ -run '^$' -bench WsTcMonitor -benchmem
func BenchmarkWsTcMonitor(b *testing.B) {
	m, err := filepath.Glob(filepath.Join("testdata", "7_3_1", "ws", "tcmonitor", "*", "ip.pcap"))
	if err != nil {
		b.Fatal(err)
	}
	evs := make([]tcEvent, 0, 128)
	for _, p := range m {
		evs = append(evs, tcMonEvents(pcapNlMsgs(b, p))...)
	}
	if len(evs) == 0 {
		b.Fatal("no events")
	}
	var bytes int
	for _, e := range evs {
		bytes += len(e.body)
	}
	b.ReportAllocs()
	b.SetBytes(int64(bytes))
	for b.Loop() {
		for _, e := range evs {
			if err := tcEventDecode(e.h.Type, e.body); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(len(evs)), "ns/msg")
}

// BenchmarkTcEventKind is the dispatch alone.
func BenchmarkTcEventKind(b *testing.B) {
	types := []uint16{unix.RTM_NEWQDISC, unix.RTM_DELTFILTER, unix.RTM_NEWCHAIN, unix.RTM_GETACTION, unix.RTM_NEWLINK}
	var n int
	for b.Loop() {
		for _, typ := range types {
			if o, _ := TcEventKind(typ); o != TcObjNone {
				n++
			}
		}
	}
	if n == 0 {
		b.Fatal("no events")
	}
}
