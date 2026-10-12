package rtnlmon

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/nlconn"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	tdLink = "../xtcpnl/testdata/7_1_4/netlink_route_events_link.pcap"
	tdAddr = "../xtcpnl/testdata/7_1_4/netlink_route_events_addr.pcap"
)

// datagrams returns a capture's datagrams, one per nlmon record.
func datagrams(t *testing.T, filename string) [][]byte {
	t.Helper()
	bs, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	_, records, err := xtcpnl.ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatal(err)
	}
	out := make([][]byte, 0, len(records))
	for _, r := range records {
		_, body, err := r.NetlinkPayload()
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, body)
	}
	return out
}

// item is one thing the fake socket yields: a datagram, or an error. A stale
// datagram is one queued ahead of an overrun; only Drain may consume it.
type item struct {
	data  []byte
	err   error
	stale bool
}

type fakeConn struct {
	items  []item
	log    *[]string
	cancel context.CancelFunc
	t      *testing.T
}

func (f *fakeConn) Receive(ctx context.Context, visit func([]byte) error) (int, error) {
	if len(f.items) == 0 {
		f.cancel()
		return 0, ctx.Err()
	}
	it := f.items[0]
	f.items = f.items[1:]
	if it.err != nil {
		return 0, it.err
	}
	if it.stale {
		*f.log = append(*f.log, "STALE DELIVERED")
	}
	return 1, visit(it.data)
}

func (f *fakeConn) Drain(context.Context) (int, error) {
	n := 0
	for len(f.items) > 0 && f.items[0].stale {
		f.items = f.items[1:]
		n++
	}
	*f.log = append(*f.log, fmt.Sprintf("drain %d", n))
	return n, nil
}

// recorder logs what Run hands it, and keeps the state a consumer would.
type recorder struct {
	log      *[]string
	state    *state
	failOn   string
	failWith error
}

func (r recorder) Snapshot(s Snapshot, reason Reason) error {
	line := "snapshot " + reason.String()
	*r.log = append(*r.log, line)
	if r.state != nil {
		r.state.reset(s)
	}
	if r.failOn == line {
		return r.failWith
	}
	return nil
}

func (r recorder) Event(ev any) error {
	line := "event " + describe(ev)
	*r.log = append(*r.log, line)
	if r.state != nil {
		r.state.apply(ev)
	}
	if r.failOn == "event" {
		return r.failWith
	}
	return nil
}

func describe(ev any) string {
	switch e := ev.(type) {
	case xtcpnl.LinkEvent:
		return fmt.Sprintf("link %s %d", e.Action, e.Link.Index)
	case xtcpnl.AddrEvent:
		return fmt.Sprintf("addr %s %d", e.Action, e.Addr.Index)
	default:
		return fmt.Sprintf("%T", ev)
	}
}

// state is the consumer the package comment describes: a Snapshot replaces
// it, an add upserts, a del deletes.
type state struct {
	links map[int32]xtcpnl.LinkInfo
	addrs map[string]xtcpnl.AddrInfo
}

func newState() *state {
	return &state{links: map[int32]xtcpnl.LinkInfo{}, addrs: map[string]xtcpnl.AddrInfo{}}
}

func addrKey(a xtcpnl.AddrInfo) string {
	return fmt.Sprintf("%d/%d/%d/%x/%x", a.Index, a.Family, a.Prefixlen, a.Address, a.Local)
}

func (s *state) reset(snap Snapshot) {
	*s = *newState()
	for i := range snap.Links {
		s.links[snap.Links[i].Index] = snap.Links[i]
	}
	for i := range snap.Addrs {
		s.addrs[addrKey(snap.Addrs[i])] = snap.Addrs[i]
	}
}

func (s *state) apply(ev any) {
	switch e := ev.(type) {
	case xtcpnl.LinkEvent:
		if e.Action == xtcpnl.EventActionDel {
			delete(s.links, e.Link.Index)
		} else {
			s.links[e.Link.Index] = e.Link
		}
	case xtcpnl.AddrEvent:
		if e.Action == xtcpnl.EventActionDel {
			delete(s.addrs, addrKey(e.Addr))
		} else {
			s.addrs[addrKey(e.Addr)] = e.Addr
		}
	}
}

func (s *state) snapshot() Snapshot {
	var out Snapshot
	for _, k := range slices.Sorted(maps.Keys(s.links)) {
		out.Links = append(out.Links, s.links[k])
	}
	for _, k := range slices.Sorted(maps.Keys(s.addrs)) {
		out.Addrs = append(out.Addrs, s.addrs[k])
	}
	return out
}

// fold is the reference: every notification in dgs applied in order to an
// empty state, with xtcpnl alone.
func fold(t *testing.T, dgs [][]byte) *state {
	t.Helper()
	s := newState()
	for _, d := range dgs {
		if err := xtcpnl.WalkNotifications(d, func(h xtcpnl.NlMsgHdr, body []byte) error {
			ev, err := xtcpnl.ParseRtnetlinkEvent(h.Type, body)
			if err != nil {
				return err
			}
			s.apply(ev)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func items(dgs [][]byte, stale bool) []item {
	out := make([]item, len(dgs))
	for i, d := range dgs {
		out[i] = item{data: d, stale: stale}
	}
	return out
}

// go test ./pkg/rtnlmon/ -run TestRunResyncOrder
func TestRunResyncOrder(t *testing.T) {
	link := datagrams(t, tdLink)
	a, b, c := link[0], link[1], link[2]
	ev := func(d []byte) string {
		var h xtcpnl.NlMsgHdr
		if _, err := xtcpnl.DeserializeNlMsgHdr(d, &h); err != nil {
			t.Fatal(err)
		}
		e, err := xtcpnl.ParseRtnetlinkEvent(h.Type, d[xtcpnl.NlMsgHdrSizeCst:h.Len])
		if err != nil {
			t.Fatal(err)
		}
		return "event " + describe(e)
	}
	cut := slices.Clone(a[:xtcpnl.NlMsgHdrSizeCst+4])
	binary.LittleEndian.PutUint32(cut, uint32(len(cut)))
	boom := errors.New("injected")

	for _, row := range []struct {
		description string
		items       []item
		dumpErrs    []error
		failOn      string
		expected    []string
		expectedErr error
		dumps       int
	}{
		{
			description: "positive: the start",
			items:       items([][]byte{a, b}, false),
			expected:    []string{"snapshot start", ev(a), ev(b)},
			expectedErr: context.Canceled,
			dumps:       1,
		},
		{
			description: "positive: an overrun drains before it dumps",
			items:       []item{{data: a}, {err: unix.ENOBUFS}, {data: b, stale: true}, {data: c}},
			expected:    []string{"snapshot start", ev(a), "drain 1", "snapshot overrun", ev(c)},
			expectedErr: context.Canceled,
			dumps:       2,
		},
		{
			description: "positive: a truncated datagram resyncs too",
			items:       []item{{err: nlconn.ErrTruncated}, {data: c}},
			expected:    []string{"snapshot start", "drain 0", "snapshot truncated", ev(c)},
			expectedErr: context.Canceled,
			dumps:       2,
		},
		{
			description: "negative: the dump fails",
			dumpErrs:    []error{boom},
			expected:    nil,
			expectedErr: boom,
			dumps:       1,
		},
		{
			description: "negative: the Handler refuses an event",
			items:       items([][]byte{a, b}, false),
			failOn:      "event",
			expected:    []string{"snapshot start", ev(a)},
			expectedErr: boom,
			dumps:       1,
		},
		{
			description: "negative: the Handler refuses a snapshot",
			failOn:      "snapshot start",
			expected:    []string{"snapshot start"},
			expectedErr: boom,
			dumps:       1,
		},
		{
			description: "negative: an undecodable notification",
			items:       []item{{data: cut}},
			expected:    []string{"snapshot start"},
			expectedErr: xtcpnl.ErrIfInfomsgSmall,
			dumps:       1,
		},
		{
			description: "negative: another receive error ends Run",
			items:       []item{{err: nlconn.ErrSender}},
			expected:    []string{"snapshot start"},
			expectedErr: nlconn.ErrSender,
			dumps:       1,
		},
		{
			description: "boundary: an interrupted dump is retaken",
			dumpErrs:    []error{xtcpnl.ErrDumpInterrupted, xtcpnl.ErrDumpInterrupted, nil},
			expected:    []string{"snapshot start"},
			expectedErr: context.Canceled,
			dumps:       3,
		},
		{
			description: "boundary: always interrupted",
			dumpErrs:    []error{xtcpnl.ErrDumpInterrupted, xtcpnl.ErrDumpInterrupted, xtcpnl.ErrDumpInterrupted, xtcpnl.ErrDumpInterrupted},
			expected:    nil,
			expectedErr: xtcpnl.ErrDumpInterrupted,
			dumps:       4,
		},
		{
			description: "corner: two overruns in a row",
			items:       []item{{err: unix.ENOBUFS}, {err: unix.ENOBUFS}, {data: c}},
			expected:    []string{"snapshot start", "drain 0", "snapshot overrun", "drain 0", "snapshot overrun", ev(c)},
			expectedErr: context.Canceled,
			dumps:       3,
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var log []string
			conn := &fakeConn{items: slices.Clone(row.items), log: &log, cancel: cancel, t: t}
			dumps := 0
			dump := func(context.Context) (Snapshot, error) {
				dumps++
				if dumps <= len(row.dumpErrs) && row.dumpErrs[dumps-1] != nil {
					return Snapshot{}, row.dumpErrs[dumps-1]
				}
				return Snapshot{}, nil
			}
			h := recorder{log: &log, failOn: row.failOn, failWith: boom}
			err := run(ctx, conn, dump, DefaultMaxDumpRetries, h)
			if !errors.Is(err, row.expectedErr) {
				t.Fatalf("err=%v, expected %v", err, row.expectedErr)
			}
			if !slices.Equal(log, row.expected) {
				t.Fatalf("calls:\n  %s\nexpected:\n  %s", strings.Join(log, "\n  "), strings.Join(row.expected, "\n  "))
			}
			if dumps != row.dumps {
				t.Fatalf("%d dumps, expected %d", dumps, row.dumps)
			}
		})
	}
}

// go test ./pkg/rtnlmon/ -run TestPlanDumps
func TestPlanDumps(t *testing.T) {
	all := []Group{GroupLink, GroupIPv4Addr, GroupIPv6Addr, GroupIPv4Route, GroupIPv6Route, GroupNeigh}
	for _, row := range []struct {
		description string
		groups      []Group
		expected    [][]byte
		expectedErr error
	}{
		{
			description: "positive: link and both address groups",
			groups:      []Group{GroupLink, GroupIPv4Addr, GroupIPv6Addr},
			expected:    [][]byte{xtcpnl.BuildDumpLinkRequest(1), xtcpnl.BuildDumpAddrRequest(unix.AF_UNSPEC, 2)},
		},
		{
			description: "positive: every group",
			groups:      all,
			expected: [][]byte{
				xtcpnl.BuildDumpLinkRequest(1),
				xtcpnl.BuildDumpAddrRequest(unix.AF_UNSPEC, 2),
				xtcpnl.BuildDumpRouteRequest(unix.AF_UNSPEC, 3),
				xtcpnl.BuildDumpNeighRequest(unix.AF_UNSPEC, 4),
			},
		},
		{description: "negative: no groups", expectedErr: ErrNoGroups},
		{description: "negative: an unsupported group", groups: []Group{GroupLink, Group(unix.RTNLGRP_IPV4_NETCONF)}, expectedErr: ErrUnknownGroup},
		{
			description: "boundary: one v6 route group",
			groups:      []Group{GroupIPv6Route},
			expected:    [][]byte{xtcpnl.BuildDumpRouteRequest(unix.AF_INET6, 1)},
		},
		{
			description: "boundary: one v4 address group",
			groups:      []Group{GroupIPv4Addr},
			expected:    [][]byte{xtcpnl.BuildDumpAddrRequest(unix.AF_INET, 1)},
		},
		{
			description: "corner: duplicates and order do not matter",
			groups:      []Group{GroupNeigh, GroupLink, GroupLink},
			expected:    [][]byte{xtcpnl.BuildDumpLinkRequest(1), xtcpnl.BuildDumpNeighRequest(unix.AF_UNSPEC, 2)},
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			plan, err := planDumps(row.groups)
			if !errors.Is(err, row.expectedErr) {
				t.Fatalf("err=%v, expected %v", err, row.expectedErr)
			}
			if len(plan) != len(row.expected) {
				t.Fatalf("%d dumps, expected %d", len(plan), len(row.expected))
			}
			for i, r := range plan {
				if got := r.request(uint32(i + 1)); !bytes.Equal(got, row.expected[i]) {
					t.Fatalf("dump %d: % x, expected % x", i, got, row.expected[i])
				}
			}
		})
	}
	t.Run("negative: Run refuses before opening a socket", func(t *testing.T) {
		if err := Run(context.Background(), Options{}, recorder{log: new([]string)}); !errors.Is(err, ErrNoGroups) {
			t.Fatalf("err=%v, expected ErrNoGroups", err)
		}
	})
}

// go test ./pkg/rtnlmon/ -run TestReplayCapturedEventsIntoState
func TestReplayCapturedEventsIntoState(t *testing.T) {
	for _, file := range []string{tdLink, tdAddr} {
		dgs := datagrams(t, file)
		want := fold(t, dgs).snapshot()
		h, k, d := len(dgs)/3, 5, 5 // delivered, queued (stale), dropped

		for _, row := range []struct {
			description string
			items       []item
			dumps       []Snapshot
		}{
			{
				description: "positive: " + file,
				items:       items(dgs, false),
				dumps:       []Snapshot{{}},
			},
			{
				description: "corner: an overrun mid-capture: " + file,
				items: slices.Concat(
					items(dgs[:h], false),
					[]item{{err: unix.ENOBUFS}},
					items(dgs[h:h+k], true),
					// dgs[h+k:h+k+d] are the notifications the kernel dropped.
					items(dgs[h+k+d:], false),
				),
				dumps: []Snapshot{{}, fold(t, dgs[:h+k+d]).snapshot()},
			},
		} {
			t.Run(row.description, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				var log []string
				st := newState()
				conn := &fakeConn{items: row.items, log: &log, cancel: cancel, t: t}
				n := 0
				dump := func(context.Context) (Snapshot, error) {
					s := row.dumps[n]
					n++
					return s, nil
				}
				if err := run(ctx, conn, dump, DefaultMaxDumpRetries, recorder{log: &log, state: st}); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if slices.Contains(log, "STALE DELIVERED") {
					t.Fatal("a notification queued ahead of the overrun reached the Handler")
				}
				if n != len(row.dumps) {
					t.Fatalf("%d dumps, expected %d", n, len(row.dumps))
				}
				got := st.snapshot()
				if len(got.Links) != len(want.Links) || len(got.Addrs) != len(want.Addrs) {
					t.Fatalf("%d links %d addrs, expected %d and %d", len(got.Links), len(got.Addrs), len(want.Links), len(want.Addrs))
				}
				for i := range got.Links {
					if got.Links[i].Index != want.Links[i].Index || got.Links[i].Name != want.Links[i].Name || got.Links[i].Flags != want.Links[i].Flags {
						t.Fatalf("link %d: %+v, expected %+v", i, got.Links[i], want.Links[i])
					}
				}
				for i := range got.Addrs {
					if addrKey(got.Addrs[i]) != addrKey(want.Addrs[i]) {
						t.Fatalf("addr %d: %s, expected %s", i, addrKey(got.Addrs[i]), addrKey(want.Addrs[i]))
					}
				}
			})
		}
	}
}
