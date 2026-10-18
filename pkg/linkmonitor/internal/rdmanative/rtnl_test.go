package rdmanative

import (
	"encoding/binary"
	"errors"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

const (
	up   = iffRunning | iffLowerUp | 0x1 // IFF_UP as well, as the kernel sends it
	down = 0x1                           // administratively up, no carrier
)

// rtnlMsg is one rtnetlink message as the translator receives it.
type rtnlMsg struct {
	typ  uint16
	body []byte
}

func newLink(ifindex, flags uint32) rtnlMsg { return rtnlMsg{rtmNewLink, seedBody(ifindex, flags)} }
func delLink(ifindex uint32) rtnlMsg        { return rtnlMsg{rtmDelLink, seedBody(ifindex, 0)} }

func addr(typ uint16, ifindex uint32) rtnlMsg {
	b := make([]byte, ifaddrmsgLen, ifaddrmsgLen+20)
	b[0] = 10 // AF_INET6
	binary.LittleEndian.PutUint32(b[ifaddrmsgOffIndex:], ifindex)
	// An IFA_ADDRESS the translator must not need.
	b = append(b, 20, 0, 1, 0)
	return rtnlMsg{typ, append(b, make([]byte, 16)...)}
}

func refresh(dev string, port uint32) rdmaevents.Event {
	return rdmaevents.Event{Device: dev, Port: port, Kind: rdmaevents.Refresh}
}

func topology(dev string, port uint32) rdmaevents.Event {
	return rdmaevents.Event{Device: dev, Port: port, Kind: rdmaevents.Topology}
}

// One mlx5-like host: mlx5_0/1 on eth0 (2), mlx5_1/1 on eth1 (3), both
// enslaved to bond0 (10); vlan100 (20) on eth0 and vlan200 (21) on bond0;
// rxe0/1 and rxe0/2 share eth2 (4).
var (
	testBindings = []Binding{{"mlx5_0", 1, 2, 0, false}, {"mlx5_1", 1, 3, 0, false}, {"rxe0", 1, 4, 0, false}, {"rxe0", 2, 4, 0, false}}
	testLinks    = []Link{{Index: 2, Master: 10}, {Index: 3, Master: 10}, {Index: 4}, {Index: 10},
		{Index: 20, Lower: 2}, {Index: 21, Lower: 10}, {Index: 30, Lower: 99}}
)

// TestTranslate is the design's rtnetlink translator table. Each row seeds
// the translator from a link dump in which every netdev is up, then feeds its
// messages in order.
func TestTranslate(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		seed                                  bool
		msgs                                  []rtnlMsg
		want                                  []rdmaevents.Event
		wantErr                               error
	}{
		{"carrier_down", "positive", "RTM_NEWLINK clearing IFF_LOWER_UP on a mapped netdev", "one Refresh for its port",
			true, []rtnlMsg{newLink(2, iffRunning|0x1)}, []rdmaevents.Event{refresh("mlx5_0", 1)}, nil},
		{"carrier_up", "positive", "carrier down, then back", "Refresh each way",
			true, []rtnlMsg{newLink(2, down), newLink(2, up)}, []rdmaevents.Event{refresh("mlx5_0", 1), refresh("mlx5_0", 1)}, nil},
		{"addr_add", "positive", "RTM_NEWADDR on a mapped netdev", "one Topology",
			true, []rtnlMsg{addr(rtmNewAddr, 3)}, []rdmaevents.Event{topology("mlx5_1", 1)}, nil},
		{"addr_del", "positive", "RTM_DELADDR on a mapped netdev", "one Topology",
			true, []rtnlMsg{addr(rtmDelAddr, 3)}, []rdmaevents.Event{topology("mlx5_1", 1)}, nil},
		{"vlan_addr", "positive", "RTM_NEWADDR on a VLAN upper", "Topology for the lower's port",
			true, []rtnlMsg{addr(rtmNewAddr, 20)}, []rdmaevents.Event{topology("mlx5_0", 1)}, nil},
		{"vlan_link", "negative", "carrier change on a VLAN upper", "nothing: an upper's state is not the port's",
			true, []rtnlMsg{newLink(20, down)}, nil, nil},
		{"unmapped", "negative", "RTM_NEWLINK for an unrelated ifindex", "no event",
			true, []rtnlMsg{newLink(7, down), addr(rtmNewAddr, 7)}, nil, nil},
		{"upper_of_unmapped", "negative", "address on an upper whose lower is not mapped", "no event",
			true, []rtnlMsg{addr(rtmNewAddr, 30)}, nil, nil},
		{"other_type", "negative", "RTM_NEWROUTE for a mapped ifindex", "no event, no error",
			true, []rtnlMsg{{24, seedBody(2, 0)}}, nil, nil},
		{"same_state", "negative", "two RTM_NEWLINK with the same running and carrier bits", "one event, then none",
			true, []rtnlMsg{newLink(4, down), newLink(4, down|0x1000)}, []rdmaevents.Event{refresh("rxe0", 1), refresh("rxe0", 2)}, nil},
		{"running_only", "boundary", "IFF_RUNNING without IFF_LOWER_UP", "down: both bits are needed",
			true, []rtnlMsg{newLink(2, iffRunning)}, []rdmaevents.Event{refresh("mlx5_0", 1)}, nil},
		{"lower_up_only", "boundary", "IFF_LOWER_UP without IFF_RUNNING", "down: both bits are needed",
			true, []rtnlMsg{newLink(2, iffLowerUp)}, []rdmaevents.Event{refresh("mlx5_0", 1)}, nil},
		{"short_ifaddrmsg", "boundary", "RTM_NEWADDR of 7 bytes", "ErrRtnl",
			true, []rtnlMsg{{rtmNewAddr, make([]byte, ifaddrmsgLen-1)}}, nil, ErrRtnl},
		{"two_ports_one_netdev", "boundary", "two RDMA ports on one netdev", "one event per port",
			true, []rtnlMsg{newLink(4, down)}, []rdmaevents.Event{refresh("rxe0", 1), refresh("rxe0", 2)}, nil},
		{"bond_master", "corner", "RTM_NEWLINK on a bond master with two enslaved RDMA ports", "Refresh for both",
			true, []rtnlMsg{newLink(10, down)}, []rdmaevents.Event{refresh("mlx5_0", 1), refresh("mlx5_1", 1)}, nil},
		{"bond_vlan_addr", "corner", "address on a VLAN over the bond", "Topology for both enslaved ports",
			true, []rtnlMsg{addr(rtmNewAddr, 21)}, []rdmaevents.Event{topology("mlx5_0", 1), topology("mlx5_1", 1)}, nil},
		{"startup_state", "corner", "first message after the dump matches the dumped state", "no event",
			true, []rtnlMsg{newLink(2, up)}, nil, nil},
		{"unseeded", "corner", "first message for a port the dump did not report", "Refresh: an unknown state is reported",
			false, []rtnlMsg{newLink(2, up), newLink(2, up)}, []rdmaevents.Event{refresh("mlx5_0", 1)}, nil},
		{"dellink", "corner", "RTM_DELLINK on a mapped netdev", "Refresh; last state down",
			true, []rtnlMsg{delLink(3), newLink(3, down), delLink(3)}, []rdmaevents.Event{refresh("mlx5_1", 1), refresh("mlx5_1", 1)}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			m, err := NewPortMap(testBindings, testLinks)
			if err != nil {
				t.Fatal(err)
			}
			tr := NewTranslator(m)
			if tc.seed {
				for _, l := range testLinks {
					if err := tr.Seed(rtmNewLink, seedBody(l.Index, up)); err != nil {
						t.Fatal(err)
					}
				}
			}
			var got []rdmaevents.Event
			var batch [MaxPorts]rdmaevents.Event
			var err2 error
			for _, msg := range tc.msgs {
				n, err := tr.Translate(msg.typ, msg.body, &batch)
				if err != nil {
					err2 = err
				}
				got = append(got, batch[:n]...)
			}
			if !errors.Is(err2, tc.wantErr) || !slices.Equal(got, tc.want) {
				t.Fatalf("%s:\n got %v, %v\nwant %v, %v", tc.expected, got, err2, tc.want, tc.wantErr)
			}
		})
	}
}

// TestTranslateTruncated is the truncated_ifinfomsg row: every link message
// shorter than ifinfomsg is a length error that reports nothing and leaves
// the state alone.
func TestTranslateTruncated(t *testing.T) {
	t.Log("boundary: truncated_ifinfomsg, each length from 0 to 15; expected: ErrRtnl, no event, no panic, state kept")
	m, err := NewPortMap(testBindings, testLinks)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewTranslator(m)
	if err := tr.Seed(rtmNewLink, seedBody(2, up)); err != nil {
		t.Fatal(err)
	}
	full := seedBody(2, down)
	var batch [MaxPorts]rdmaevents.Event
	for _, typ := range []uint16{rtmNewLink, rtmDelLink} {
		for n := range ifinfomsgLen {
			if got, err := tr.Translate(typ, full[:n], &batch); got != 0 || !errors.Is(err, ErrRtnl) {
				t.Fatalf("type %d, %d bytes: %d events, %v", typ, n, got, err)
			}
			if err := tr.Seed(typ, full[:n]); typ == rtmNewLink && !errors.Is(err, ErrRtnl) {
				t.Fatalf("seed %d bytes: %v", n, err)
			}
		}
	}
	if got, err := tr.Translate(rtmNewLink, seedBody(2, up), &batch); got != 0 || err != nil {
		t.Fatalf("state changed by a truncated message: %d events, %v", got, err)
	}
}

// TestPortMap is the port map's construction table, rendered as
// "ifindex:device/port" with l for link state and a for addresses.
func TestPortMap(t *testing.T) {
	many := make([]Binding, MaxPorts+1)
	for i := range many {
		many[i] = Binding{Device: "d" + itoa(uint32(i)), Port: 1, Netdev: 5}
	}
	for _, tc := range []struct {
		name, category, description, expected string
		bindings                              []Binding
		links                                 []Link
		want                                  string
		devices                               []string
		wantErr                               error
	}{
		{"direct", "positive", "two ports on their own netdevs", "link and address refs, sorted by ifindex",
			[]Binding{{"mlx5_1", 1, 3, 0, false}, {"mlx5_0", 1, 2, 0, false}}, nil, "2:mlx5_0/1la 3:mlx5_1/1la", []string{"mlx5_0", "mlx5_1"}, nil},
		{"full", "positive", "bond, VLANs and a shared netdev", "masters link and address, uppers address only",
			testBindings, testLinks,
			"2:mlx5_0/1la 3:mlx5_1/1la 4:rxe0/1la 4:rxe0/2la 10:mlx5_0/1la 10:mlx5_1/1la 20:mlx5_0/1a 21:mlx5_0/1a 21:mlx5_1/1a",
			[]string{"mlx5_0", "mlx5_1", "rxe0"}, nil},
		{"no_netdev", "negative", "a port with no netdev (native InfiniBand)", "no ref, device still listed",
			[]Binding{{"mlx5_2", 1, 0, 0, false}}, nil, "", []string{"mlx5_2"}, nil},
		{"no_port", "negative", "a binding with port 0", "no ref",
			[]Binding{{"mlx5_0", 0, 2, 0, false}}, nil, "", []string{"mlx5_0"}, nil},
		{"duplicate", "boundary", "the same binding twice", "one ref",
			[]Binding{{"rxe0", 1, 4, 0, false}, {"rxe0", 1, 4, 0, false}}, nil, "4:rxe0/1la", []string{"rxe0"}, nil},
		{"max_ports", "boundary", "MaxPorts+1 ports behind one ifindex", "ErrPortMap",
			many, nil, "", nil, ErrPortMap},
		{"self_lower", "corner", "a link that is its own lower", "ignored",
			[]Binding{{"rxe0", 1, 4, 0, false}}, []Link{{Index: 4, Lower: 4}}, "4:rxe0/1la", []string{"rxe0"}, nil},
		{"upper_is_bound", "corner", "an upper that is itself bound", "keeps link and address",
			[]Binding{{"a", 1, 4, 0, false}, {"b", 1, 5, 0, false}}, []Link{{Index: 5, Lower: 4}}, "4:a/1la 5:a/1a 5:b/1la", []string{"a", "b"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			m, err := NewPortMap(tc.bindings, tc.links)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err=%v", tc.expected, err)
			}
			if err != nil {
				return
			}
			if got := m.String(); got != tc.want || !slices.Equal(m.Devices(), tc.devices) {
				t.Fatalf("%s:\n got %q %v\nwant %q %v", tc.expected, got, m.Devices(), tc.want, tc.devices)
			}
			again, _ := NewPortMap(tc.bindings, tc.links)
			if !m.Equal(again) {
				t.Fatalf("rebuilt map differs: %s", again)
			}
		})
	}
	a, _ := NewPortMap(testBindings, testLinks)
	b, _ := NewPortMap(testBindings, nil)
	if a.Equal(b) || !(*PortMap)(nil).Equal(nil) || a.Equal(nil) {
		t.Fatal("Equal: different maps compare equal, or nil handling is wrong")
	}
}

func asyncRec(element uint64, event uint32, n int) []byte {
	b := make([]byte, max(n, asyncEventDescLen))
	binary.LittleEndian.PutUint64(b[asyncEventDescOffElement:], element)
	binary.LittleEndian.PutUint32(b[asyncEventDescOffType:], event)
	return b[:n]
}

// TestDecodeAsyncEvent is the async record decoder's table, checked against
// event_copy.h's mapping.
func TestDecodeAsyncEvent(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		rec                                   []byte
		want                                  rdmaevents.Event
		wantErr                               error
	}{
		{"port_err", "positive", "captured PORT_ERR on port 1", "Refresh port 1", asyncRec(1, eventPortErr, 16), refresh("d", 1), nil},
		{"port_active", "positive", "PORT_ACTIVE on port 2", "Refresh port 2", asyncRec(2, eventPortActive, 16), refresh("d", 2), nil},
		{"gid_change", "positive", "captured GID_CHANGE", "Topology port 1", asyncRec(1, eventGIDChange, 16), topology("d", 1), nil},
		{"topology_kinds", "positive", "CLIENT_REREGISTER", "Topology", asyncRec(1, eventClientReregister, 16), topology("d", 1), nil},
		{"fatal", "positive", "DEVICE_FATAL with an element", "Fatal, port 0", asyncRec(7, eventDeviceFatal, 16),
			rdmaevents.Event{Device: "d", Kind: rdmaevents.Fatal}, nil},
		{"speed", "negative", "DEVICE_SPEED_CHANGE, which the cgo source does not map", "Unknown, port 0", asyncRec(1, eventDeviceSpeedChange, 16),
			rdmaevents.Event{Device: "d", Kind: rdmaevents.Unknown}, nil},
		{"short", "boundary", "15-byte record", "ErrAsyncEvent", asyncRec(1, eventPortErr, 15), rdmaevents.Event{}, ErrAsyncEvent},
		{"long", "boundary", "17-byte record", "ErrAsyncEvent", asyncRec(1, eventPortErr, 17), rdmaevents.Event{}, ErrAsyncEvent},
		{"port_zero", "corner", "PORT_ERR with element 0", "Refresh of the whole device", asyncRec(0, eventPortErr, 16), refresh("d", 0), nil},
		{"negative_port", "corner", "element whose low 32 bits are negative as an int", "port 0, as libibverbs' int port_num", asyncRec(0xffffffff, eventPortErr, 16), refresh("d", 0), nil},
		{"high_bits", "corner", "element with high bits set", "only the low 32 bits count", asyncRec(1<<32|3, eventGIDChange, 16), topology("d", 3), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			got, err := DecodeAsyncEvent("d", tc.rec)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("%s: got %+v, %v", tc.expected, got, err)
			}
		})
	}
}

// TestTranslateAllocs is the zero-allocation guard for the per-message paths.
func TestTranslateAllocs(t *testing.T) {
	m, err := NewPortMap(testBindings, testLinks)
	if err != nil {
		t.Fatal(err)
	}
	tr := NewTranslator(m)
	var batch [MaxPorts]rdmaevents.Event
	link, flap, address := seedBody(10, up), seedBody(10, down), addr(rtmNewAddr, 21).body
	rec := asyncRec(1, eventPortErr, 16)
	for _, tc := range []struct {
		name string
		fn   func()
	}{
		{"translate_link", func() {
			_, _ = tr.Translate(rtmNewLink, link, &batch)
			_, _ = tr.Translate(rtmNewLink, flap, &batch)
		}},
		{"translate_addr", func() { _, _ = tr.Translate(rtmNewAddr, address, &batch) }},
		{"translate_error", func() { _, _ = tr.Translate(rtmNewLink, link[:3], &batch) }},
		{"seed", func() { _ = tr.Seed(rtmNewLink, link) }},
		{"decode_async", func() { _, _ = DecodeAsyncEvent("d", rec) }},
	} {
		if n := testing.AllocsPerRun(100, tc.fn); n != 0 {
			t.Errorf("%s: %v allocs, want 0", tc.name, n)
		}
	}
}

func BenchmarkTranslateRtnl(b *testing.B) {
	m, err := NewPortMap(testBindings, testLinks)
	if err != nil {
		b.Fatal(err)
	}
	tr := NewTranslator(m)
	var batch [MaxPorts]rdmaevents.Event
	msgs := []rtnlMsg{newLink(10, down), newLink(10, up), addr(rtmNewAddr, 21), newLink(7, down)}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		msg := msgs[i%len(msgs)]
		if _, err := tr.Translate(msg.typ, msg.body, &batch); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeAsyncEvent(b *testing.B) {
	rec := asyncRec(1, eventGIDChange, 16)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeAsyncEvent("mlx5_0", rec); err != nil {
			b.Fatal(err)
		}
	}
}

// FuzzTranslateRtnl feeds arbitrary bodies under each handled type: no panic,
// at most the refs behind one ifindex, and only Refresh for links and
// Topology for addresses.
func FuzzTranslateRtnl(f *testing.F) {
	f.Add(uint16(rtmNewLink), seedBody(10, down))
	f.Add(uint16(rtmDelLink), seedBody(4, 0))
	f.Add(uint16(rtmNewAddr), addr(rtmNewAddr, 21).body)
	f.Add(uint16(rtmDelAddr), []byte{})
	f.Add(uint16(24), seedBody(2, 0))
	m, err := NewPortMap(testBindings, testLinks)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, typ uint16, body []byte) {
		tr := NewTranslator(m)
		var batch [MaxPorts]rdmaevents.Event
		n, err := tr.Translate(typ, body, &batch)
		if err != nil && n != 0 {
			t.Fatalf("%d events with %v", n, err)
		}
		for _, e := range batch[:n] {
			want := rdmaevents.Refresh
			if typ == rtmNewAddr || typ == rtmDelAddr {
				want = rdmaevents.Topology
			}
			if e.Kind != want || e.Port == 0 || !slices.Contains(m.Devices(), e.Device) {
				t.Fatalf("type %d: %+v", typ, e)
			}
		}
		if n > 2 {
			t.Fatalf("%d events; no ifindex in the map has more than 2 ports", n)
		}
	})
}
