package rdmanative

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
	"golang.org/x/sys/unix"
)

// fakeRoute delivers one datagram per Receive, then the final error.
type fakeRoute struct {
	datagrams [][]byte
	end       error
	closes    int
}

func (f *fakeRoute) Receive(ctx context.Context, visit func([]byte) error) (int, error) {
	if len(f.datagrams) == 0 {
		if f.end == nil {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		return 0, f.end
	}
	d := f.datagrams[0]
	f.datagrams = f.datagrams[1:]
	return 1, visit(d)
}

func (f *fakeRoute) Close() error { f.closes++; return nil }

// nlmsg frames body as a netlink message of typ with flags.
func nlmsg(typ, flags uint16, body []byte) []byte {
	b := make([]byte, 16, 16+len(body)+3)
	binary.LittleEndian.PutUint32(b, uint32(16+len(body)))
	binary.LittleEndian.PutUint16(b[4:], typ)
	binary.LittleEndian.PutUint16(b[6:], flags)
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// TestSourceRun is the source's condition table: what reaches emit, what Run
// returns, and what is counted as malformed.
func TestSourceRun(t *testing.T) {
	carrierDown := nlmsg(rtmNewLink, 0, seedBody(2, down))
	for _, tc := range []struct {
		name, category, description, expected string
		datagrams                             [][]byte
		end                                   error
		cancel                                bool
		refuse                                bool
		want                                  []rdmaevents.Event
		wantErr                               error
		malformed                             uint64
	}{
		{"events", "positive", "a link and an address notification in one datagram", "Refresh then Topology",
			[][]byte{append(slices.Clone(carrierDown), nlmsg(rtmNewAddr, 0, addr(rtmNewAddr, 3).body)...)}, unix.ENOBUFS, false, false,
			[]rdmaevents.Event{refresh("mlx5_0", 1), topology("mlx5_1", 1)}, rdmaevents.ErrLost, 0},
		{"enobufs", "negative", "the kernel dropped notifications", "ErrLost", nil, unix.ENOBUFS, false, false, nil, rdmaevents.ErrLost, 0},
		{"truncated", "negative", "a datagram larger than the receive buffer", "ErrLost", nil, nlconn.ErrTruncated, false, false, nil, rdmaevents.ErrLost, 0},
		{"socket_error", "negative", "any other receive error", "returned as is", nil, unix.EBADF, false, false, nil, unix.EBADF, 0},
		{"refused", "negative", "emit refuses an event", "ErrLost, as pollSource.Run",
			[][]byte{carrierDown}, nil, false, true, []rdmaevents.Event{refresh("mlx5_0", 1)}, rdmaevents.ErrLost, 0},
		{"malformed_datagram", "boundary", "a datagram whose length field overruns it, then a good one", "counted, skipped, the next delivered",
			[][]byte{{0xff, 0, 0, 0, 16, 0, 0, 0}, carrierDown}, unix.ENOBUFS, false, false,
			[]rdmaevents.Event{refresh("mlx5_0", 1)}, rdmaevents.ErrLost, 1},
		{"short_body", "boundary", "an RTM_NEWLINK with a 4-byte body", "counted, skipped",
			[][]byte{nlmsg(rtmNewLink, 0, []byte{0, 0, 0, 0})}, unix.ENOBUFS, false, false, nil, rdmaevents.ErrLost, 1},
		{"not_notification", "corner", "a dump reply (NLM_F_MULTI) and a request (NLM_F_REQUEST)", "ignored",
			[][]byte{nlmsg(rtmNewLink, unix.NLM_F_MULTI, seedBody(2, down)), nlmsg(rtmNewLink, unix.NLM_F_REQUEST, seedBody(2, down))},
			unix.ENOBUFS, false, false, nil, rdmaevents.ErrLost, 0},
		{"control", "corner", "NLMSG_DONE and NLMSG_ERROR", "ignored", [][]byte{nlmsg(unix.NLMSG_DONE, 0, make([]byte, 4))},
			unix.ENOBUFS, false, false, nil, rdmaevents.ErrLost, 0},
		{"cancel", "corner", "context canceled while waiting", "ctx.Err()", nil, nil, true, false, nil, context.Canceled, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			m, err := NewPortMap(testBindings, testLinks)
			if err != nil {
				t.Fatal(err)
			}
			conn := &fakeRoute{datagrams: tc.datagrams, end: tc.end}
			s := newSource(conn, m)
			ctx, cancel := context.WithCancel(t.Context())
			if tc.cancel {
				cancel()
			}
			defer cancel()
			var got []rdmaevents.Event
			err = s.Run(ctx, func(e rdmaevents.Event) bool {
				got = append(got, e)
				return !tc.refuse
			})
			if !errors.Is(err, tc.wantErr) || !slices.Equal(got, tc.want) || s.Malformed() != tc.malformed {
				t.Fatalf("%s: got %v, %v, %d malformed", tc.expected, got, err, s.Malformed())
			}
			if err := s.Close(); err != nil || conn.closes != 1 {
				t.Fatalf("close: %v, %d", err, conn.closes)
			}
		})
	}
	if _, err := Open(t.Context(), nil); !errors.Is(err, ErrPortMap) {
		t.Fatalf("Open(nil): %v", err)
	}
}

// TestPortMapDevice is the NLDEV index lookup UNREGISTER uses. The kernel
// allocates indices from 0 (xa_alloc_cyclic over XA_FLAGS_ALLOC), so index 0
// is the first device registered, not "none".
func TestPortMapDevice(t *testing.T) {
	m, err := NewPortMap([]Binding{
		{Device: "mlx5_0", Port: 1, Netdev: 2, DevIndex: 7, HasDevIndex: true},
		{Device: "rxe0", Port: 1, Netdev: 4, DevIndex: 0, HasDevIndex: true},
		{Device: "siw0", Port: 1, Netdev: 5},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	noZero, err := NewPortMap([]Binding{{Device: "siw0", Port: 1, Netdev: 5}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, expected string
		m                                     *PortMap
		index                                 uint32
		want                                  string
		ok                                    bool
	}{
		{"held", "positive", "index of a held device", "its name", m, 7, "mlx5_0", true},
		{"not_held", "negative", "an index the map does not hold", "not found", m, 8, "", false},
		{"zero_held", "boundary", "index 0, the first device the kernel registered", "its name", m, 0, "rxe0", true},
		{"max", "boundary", "the largest uint32, which no kernel allocates", "not found", m, math.MaxUint32, "", false},
		{"zero_unknown", "corner", "index 0 against a map whose one device has no index (sysfs only)", "not found", noZero, 0, "", false},
	} {
		t.Logf("%s: %s: %s; expected: %s", tc.category, tc.name, tc.description, tc.expected)
		if got, ok := tc.m.Device(tc.index); got != tc.want || ok != tc.ok {
			t.Fatalf("%s: %q %v", tc.name, got, ok)
		}
	}
}
