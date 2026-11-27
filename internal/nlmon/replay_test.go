package nlmon

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func msg(typ, flags uint16, seq, pid uint32, body ...byte) Msg {
	return Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags, Seq: seq, Pid: pid}, Body: body, Nsid: -1}
}

// errFor is an NLMSG_ERROR naming a request of typ and seq.
func errFor(typ uint16, seq uint32) Msg {
	return msg(unix.NLMSG_ERROR, 0, seq, 100, cat(le32(0), hdr(typ, 0, seq, 0, xtcpnl.NlMsgHdrSizeCst))...)
}

// TestNotifications is which of a capture's messages a subscriber was
// sent: requests, dump parts, errors, a get's answer and an echo left out.
//
// go test ./internal/nlmon/ -run TestNotifications
func TestNotifications(t *testing.T) {
	const (
		req  = unix.NLM_F_REQUEST
		dump = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
		add  = unix.NLM_F_REQUEST | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	)
	tests := []struct {
		description string
		msgs        []Msg
		expected    []int
	}{
		{"positive: notifications alone", []Msg{msg(unix.RTM_NEWADDR, 0, 0, 0), msg(unix.RTM_DELADDR, 0, 0, 0)},
			[]int{0, 1}},
		{"negative: a dump and its parts", []Msg{msg(unix.RTM_GETLINK, dump, 1, 0),
			msg(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 1, 100), msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, 1, 100),
			msg(unix.RTM_NEWLINK, 0, 0, 0)}, []int{3}},
		{"positive: a link dump, then three qdisc events", []Msg{msg(unix.RTM_GETLINK, dump, 1, 0),
			msg(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 1, 100), msg(unix.NLMSG_DONE, unix.NLM_F_MULTI, 1, 100),
			msg(unix.RTM_NEWQDISC, 0, 9, 4242), msg(unix.RTM_DELQDISC, 0, 10, 4242), msg(unix.RTM_NEWQDISC, unix.NLM_F_REPLACE, 10, 4242)},
			[]int{3, 4, 5}},
		{"positive: a nexthop event carries REQUEST and the requester's port", []Msg{
			msg(unix.RTM_NEWNEXTHOP, req|unix.NLM_F_CREATE, 5, 0), errFor(unix.RTM_NEWNEXTHOP, 5),
			msg(unix.RTM_NEWNEXTHOP, req|unix.NLM_F_CREATE, 5, 4242)}, []int{2}},
		{"positive: a tc filter event carries the request's flags and port (K22)", []Msg{
			msg(unix.RTM_NEWTFILTER, add, 7, 0), msg(unix.RTM_NEWTFILTER, add, 7, 4242), errFor(unix.RTM_NEWTFILTER, 7)},
			[]int{1}},
		{"negative: the trigger's own qdisc add and its ack", []Msg{msg(unix.RTM_NEWQDISC, add, 3, 0),
			errFor(unix.RTM_NEWQDISC, 3), msg(unix.RTM_NEWQDISC, 0, 3, 4242)}, []int{2}},
		{"negative: a get's answer", []Msg{msg(unix.RTM_GETNSID, req, 7, 0), msg(unix.RTM_NEWNSID, 0, 7, 100),
			msg(unix.RTM_NEWNSID, 0, 0, 0)}, []int{2}},
		{"negative: a GETTFILTER's answer is dropped once", []Msg{msg(unix.RTM_GETTFILTER, req|unix.NLM_F_ECHO, 8, 0),
			msg(unix.RTM_NEWTFILTER, 0, 8, 100), msg(unix.RTM_NEWTFILTER, 0, 8, 100)}, []int{2}},
		{"corner: a get's answer is the first of its type and seq alone", []Msg{
			msg(unix.RTM_GETNSID, req, 7, 0), msg(unix.RTM_NEWADDR, 0, 7, 0), msg(unix.RTM_NEWNSID, 0, 7, 100),
			msg(unix.RTM_NEWNSID, 0, 7, 100)}, []int{1, 3}},
		{"corner: a refused get waits for no answer", []Msg{msg(unix.RTM_GETNSID, req, 7, 0),
			errFor(unix.RTM_GETNSID, 7), msg(unix.RTM_NEWNSID, 0, 7, 0)}, []int{2}},
		{"boundary: a short error names nothing", []Msg{msg(unix.RTM_GETNSID, req, 7, 0),
			msg(unix.NLMSG_ERROR, 0, 7, 0, 0, 0, 0, 0), msg(unix.RTM_NEWNSID, 0, 7, 0)}, nil},
		{"corner: an echoed add's unicast copy is left out", []Msg{msg(unix.RTM_NEWTFILTER, add|unix.NLM_F_ECHO, 4, 0),
			msg(unix.RTM_NEWTFILTER, add|unix.NLM_F_ECHO, 4, 4242, 1), msg(unix.RTM_NEWTFILTER, add|unix.NLM_F_ECHO, 4, 4242, 1),
			errFor(unix.RTM_NEWTFILTER, 4)}, []int{1}},
		{"corner: an echo's copies must be equal", []Msg{msg(unix.RTM_NEWTFILTER, add|unix.NLM_F_ECHO, 4, 0),
			msg(unix.RTM_NEWTFILTER, add, 4, 4242, 1), msg(unix.RTM_NEWTFILTER, add, 4, 4242, 2)}, []int{1, 2}},
		{"boundary: none", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := Notifications(tc.msgs)
			if fmt.Sprint(got) != fmt.Sprint(tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestDumpEnd is the index past a recorded dump's NLMSG_DONE, where a
// replayed monitor's notifications begin.
//
// go test ./internal/nlmon/ -run TestDumpEnd
func TestDumpEnd(t *testing.T) {
	m := func(typ, flags uint16, seq, pid uint32) Msg { return msg(typ, flags, seq, pid) }
	dump := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)
	tests := []struct {
		description string
		msgs        []Msg
		expected    int
	}{
		{"positive: past the DONE", []Msg{m(unix.RTM_NEWADDR, 0, 0, 0), m(unix.RTM_GETLINK, dump, 3, 0),
			m(unix.RTM_NEWADDR, 0, 0, 0), m(unix.NLMSG_DONE, 2, 3, 100), m(unix.RTM_NEWADDR, 0, 0, 0)}, 4},
		{"negative: no dump is 0", []Msg{m(unix.RTM_NEWADDR, 0, 0, 0)}, 0},
		{"negative: a dump with no DONE is 0", []Msg{m(unix.RTM_GETLINK, dump, 3, 0)}, 0},
		{"corner: a DONE of another seq is not the dump's", []Msg{m(unix.RTM_GETLINK, dump, 3, 0),
			m(unix.NLMSG_DONE, 2, 2, 100), m(unix.NLMSG_DONE, 2, 3, 100)}, 3},
		{"corner: a get of the type is not a dump", []Msg{m(unix.RTM_GETLINK, unix.NLM_F_REQUEST, 3, 0),
			m(unix.NLMSG_DONE, 2, 3, 100)}, 0},
		{"boundary: a reply from the kernel is not the request", []Msg{m(unix.RTM_GETLINK, dump, 3, 100),
			m(unix.NLMSG_DONE, 2, 3, 100)}, 0},
		{"boundary: an event during the dump is before the end, so lost", []Msg{m(unix.RTM_GETLINK, dump, 3, 0),
			m(unix.RTM_NEWQDISC, 0, 9, 4242), m(unix.NLMSG_DONE, 2, 3, 100)}, 3},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := DumpEnd(tc.msgs, unix.RTM_GETLINK); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestIsRtmGet is the GET of each family of four, the tc ones included.
//
// go test ./internal/nlmon/ -run TestIsRtmGet
func TestIsRtmGet(t *testing.T) {
	tests := []struct {
		description string
		typ         uint16
		expected    bool
	}{
		{"positive: GETLINK", unix.RTM_GETLINK, true},
		{"positive: GETQDISC", unix.RTM_GETQDISC, true},
		{"positive: GETTFILTER", unix.RTM_GETTFILTER, true},
		{"positive: GETACTION", unix.RTM_GETACTION, true},
		{"boundary: GETCHAIN, the highest tc one", unix.RTM_GETCHAIN, true},
		{"negative: NEWQDISC", unix.RTM_NEWQDISC, false},
		{"negative: DELACTION", unix.RTM_DELACTION, false},
		{"boundary: under RTM_BASE", unix.NLMSG_DONE, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsRtmGet(tc.typ); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// monitorAllPcap is goip's `ip monitor all` capture: a link dump on the
// monitor's socket, then the trigger's requests and the events.
const monitorAllPcap = "../../pkg/xtcpnl/testdata/7_1_4/link-write/rtnl/monitor_all/ip.pcap"

// TestPcapMsgs is a capture's NETLINK_ROUTE messages.
//
// go test ./internal/nlmon/ -run TestPcapMsgs
func TestPcapMsgs(t *testing.T) {
	dir := t.TempDir()
	garbage := dir + "/garbage.pcap"
	if err := os.WriteFile(garbage, []byte("not a pcap"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		description string
		path        string
		check       func([]Msg) string
		err         bool
	}{
		{"positive: the monitor capture splits into a dump and its events", monitorAllPcap, func(ms []Msg) string {
			end := DumpEnd(ms, unix.RTM_GETLINK)
			n := 0
			for _, i := range Notifications(ms) {
				if i >= end {
					n++
				}
			}
			if end == 0 || n == 0 {
				return fmt.Sprintf("dump end %d, %d events after it", end, n)
			}
			return ""
		}, false},
		{"negative: a missing file", dir + "/none", nil, true},
		{"negative: not a pcap", garbage, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ms, err := PcapMsgs(tc.path)
			if (err != nil) != tc.err {
				t.Fatalf("error %v, want error %v", err, tc.err)
			}
			if err != nil && !errors.Is(err, ErrPcap) {
				t.Errorf("error %v is not ErrPcap", err)
			}
			if tc.check != nil {
				if s := tc.check(ms); s != "" {
					t.Error(s)
				}
			}
		})
	}
}

// TestReplay is a capture's notifications played through rtnl_listen's
// buffer: the offsets Datagram records decide what a receive would cut.
//
// go test ./internal/nlmon/ -run TestReplay
func TestReplay(t *testing.T) {
	type result struct {
		types string
		err   string
	}
	// sized is a message of n bytes in all at offset off.
	sized := func(typ uint16, n uint32, off int) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Len: n, Type: typ}, Nsid: -1, Off: off}
	}
	stop := errors.New("stop")
	tests := []struct {
		description string
		msgs        []Msg
		notes       []int
		start, buf  int
		fn          func(Msg) error
		expected    result
	}{
		{"positive: every notification in order", []Msg{sized(36, 20, 0), sized(16, 20, 0), sized(37, 20, 0)},
			[]int{0, 2}, 0, 0, nil, result{types: "36 37 "}},
		{"positive: those before start are lost", []Msg{sized(36, 20, 0), sized(37, 20, 0)},
			[]int{0, 1}, 1, 0, nil, result{types: "37 "}},
		{"negative: a 16396-byte event is cut (MO5)", []Msg{sized(100, 36, 0), sized(44, 16396, 0)},
			[]int{0, 1}, 0, 0, nil, result{types: "100 ", err: "Truncated message"}},
		{"negative: fn's error ends it as it is", []Msg{sized(36, 20, 0), sized(37, 20, 0)},
			[]int{0, 1}, 0, 0, func(Msg) error { return stop }, result{err: "stop"}},
		{"boundary: a 16384-byte event fits", []Msg{sized(44, RecvBufCst, 0)}, []int{0}, 0, 0, nil, result{types: "44 "}},
		{"boundary: a 16380-byte event at offset 4 fits; at 8 it does not", []Msg{sized(44, 16380, 4), sized(44, 16380, 8)},
			[]int{0, 1}, 0, 0, nil, result{types: "44 ", err: "Truncated message"}},
		{"boundary: no notifications", []Msg{sized(36, 20, 0)}, nil, 0, 0, nil, result{}},
		{"corner: a smaller buffer cuts sooner", []Msg{sized(36, 20, 0), sized(37, 40, 0)}, []int{0, 1}, 0, 32, nil,
			result{types: "36 ", err: "Truncated message"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b strings.Builder
			fn := tc.fn
			if fn == nil {
				fn = func(m Msg) error {
					fmt.Fprintf(&b, "%d ", m.Hdr.Type)
					return nil
				}
			}
			err := Replay(tc.msgs, tc.notes, tc.start, tc.buf, fn)
			got := result{types: b.String()}
			if err != nil {
				got.err = err.Error()
				if got.err == "Truncated message" && !errors.Is(err, ErrTruncated) {
					t.Errorf("%v is not ErrTruncated", err)
				}
			}
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestDatagramOffsets pins Off: each message's start in its datagram,
// past the padding of those before it.
func TestDatagramOffsets(t *testing.T) {
	tests := []struct {
		description string
		datagram    []byte
		expected    string
	}{
		{"positive: three messages", cat(rec(1, le32(1)), rec(2, nil), rec(3, nil)), "0 20 36 "},
		{"boundary: one message", rec(1, nil), "0 "},
		{"corner: a padded body moves the next", cat(rec(1, []byte{1}), rec(2, nil)), "0 20 "},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b strings.Builder
			if err := Datagram(tc.datagram, false, -1, func(m Msg) error {
				fmt.Fprintf(&b, "%d ", m.Off)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if b.String() != tc.expected {
				t.Errorf("got %q, want %q", b.String(), tc.expected)
			}
		})
	}
}
