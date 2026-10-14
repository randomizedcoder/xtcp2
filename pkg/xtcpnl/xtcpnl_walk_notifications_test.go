package xtcpnl

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// pcapDatagrams returns each record's netlink payload: nlmon records one
// datagram per record, so each is what one recvmsg on the socket returned.
func pcapDatagrams(t *testing.T, filename string) [][]byte {
	t.Helper()
	bs, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", filename, err)
	}
	_, records, err := ParseNetlinkPcap(bs)
	if err != nil {
		t.Fatalf("ParseNetlinkPcap(%s): %v", filename, err)
	}
	out := make([][]byte, 0, len(records))
	for i, r := range records {
		_, body, perr := r.NetlinkPayload()
		if perr != nil {
			t.Fatalf("%s record %d: %v", filename, i, perr)
		}
		out = append(out, body)
	}
	return out
}

// go test ./pkg/xtcpnl/ -run TestWalkNotifications
func TestWalkNotifications(t *testing.T) {
	// The four per-family captures: their add+del totals are
	// TestEventFixtureCounts' pins, 355 in all.
	events := make([][]byte, 0, 355)
	for _, f := range []string{tdEventsLink_7_1_4, tdEventsAddr_7_1_4, tdEventsRoute_7_1_4, tdEventsNeigh_7_1_4} {
		events = append(events, pcapDatagrams(t, f)...)
	}
	dump := pcapDatagrams(t, tdRouteGetLinkDump_7_1_8)

	code := -int32(unix.EPERM)
	eperm := binary.LittleEndian.AppendUint32(nil, uint32(code))
	ifinfo := make([]byte, IfInfomsgSizeCst)
	twoEvents := append(nlmsg(unix.RTM_NEWLINK, 0, 0, ifinfo), nlmsg(unix.RTM_DELLINK, 0, 0, ifinfo)...)
	stop := errors.New("visit refused")

	for _, row := range []struct {
		description string
		datagrams   [][]byte
		refuse      bool
		expected    int
		expectedErr error
	}{
		{description: "positive: the captured event streams", datagrams: events, expected: 116 + 9 + 39 + 27 + 64 + 56 + 11 + 33},
		{description: "negative: a dump reply is skipped", datagrams: dump, expected: 0},
		{description: "negative: an NLMSG_ERROR with an errno", datagrams: [][]byte{nlmsg(unix.NLMSG_ERROR, 0, 0, eperm)}, expectedErr: unix.EPERM},
		{description: "negative: a request is skipped", datagrams: [][]byte{nlmsg(unix.RTM_NEWLINK, unix.NLM_F_REQUEST, 0, ifinfo)}, expected: 0},
		{description: "boundary: an NLMSG_DONE only", datagrams: [][]byte{nlmsg(unix.NLMSG_DONE, 0, 0, make([]byte, 4))}, expected: 0},
		{description: "boundary: an empty datagram", datagrams: [][]byte{{}}, expectedErr: ErrShortRecv},
		{description: "corner: a truncated header", datagrams: [][]byte{make([]byte, 10)}, expectedErr: ErrShortRecv},
		{description: "corner: an overrunning length", datagrams: [][]byte{nlmsg(unix.RTM_NEWLINK, 0, 0, ifinfo)[:20]}, expectedErr: ErrBadMsgLen},
		{description: "corner: visit's error stops the walk", datagrams: [][]byte{twoEvents}, refuse: true, expected: 1, expectedErr: stop},
	} {
		t.Run(row.description, func(t *testing.T) {
			visits := 0
			var err error
			for _, d := range row.datagrams {
				err = WalkNotifications(d, func(h NlMsgHdr, body []byte) error {
					visits++
					if !IsRtnetlinkNotification(h) || len(body) != int(h.Len)-NlMsgHdrSizeCst {
						t.Fatalf("visited a non-notification or a wrong body: %+v, %d bytes", h, len(body))
					}
					if row.refuse {
						return stop
					}
					return nil
				})
				if err != nil {
					break
				}
			}
			if visits != row.expected || !errors.Is(err, row.expectedErr) || (row.expectedErr == nil && err != nil) {
				t.Fatalf("visits=%d err=%v, expected %d, %v", visits, err, row.expected, row.expectedErr)
			}
		})
	}

	// A notification a userspace change caused carries the requester's pid and
	// seq; a pid/seq filter would drop it, so the walker must not.
	t.Run("positive: a userspace-caused event is kept", func(t *testing.T) {
		kept := 0
		for _, d := range pcapDatagrams(t, tdEventsRoute_7_1_4) {
			if err := WalkNotifications(d, func(h NlMsgHdr, _ []byte) error {
				if h.Pid != 0 && h.Seq != 0 {
					kept++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		if kept == 0 {
			t.Fatal("no notification with a nonzero pid and seq was visited")
		}
	})
}
