package linuxio

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// capturedRDMAEvent is capturedRDMAEvents' i-th datagram.
func capturedRDMAEvent(t *testing.T, i int) []byte {
	t.Helper()
	var out []byte
	n := 0
	if err := xtcpnl.WalkNetlinkEnvelopes(capturedRDMAEvents, func(e xtcpnl.NetlinkEnvelope) error {
		if n == i {
			out = notification(e.Body)
		}
		n++
		return nil
	}); err != nil || out == nil {
		t.Fatalf("datagram %d of %d: %v", i, n, err)
	}
	return out
}

// TestRDMANotificationKinds is the design's NLDEV notification kinds table:
// what DecodeRDMANotificationsInto reports, and how Fatal splits an
// unregister of a held device from every other, topology-only, notification.
func TestRDMANotificationKinds(t *testing.T) {
	heldRxe1 := func(dev uint32) bool { return dev == 1 }
	event := func(v ...byte) []byte { return notifyAttribute(rdmaEventType, v) }
	for _, tc := range []struct {
		name, category, description, expected string
		data                                  []byte
		held                                  func(uint32) bool
		want                                  []RDMANotification
		wantFatal                             []bool
		wantErr                               error
	}{
		{"unregister", "positive", "captured UNREGISTER of rxe1, held", "Fatal for device 1",
			capturedRDMAEvent(t, 2), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyUnregister, EventType: 1, DevIndex: 1, HasDevIndex: true}},
			[]bool{true}, nil},
		{"attach", "positive", "captured NETDEV_ATTACH of rxe1 port 1 to ifindex 3", "Topology with device, port and netdev",
			capturedRDMAEvent(t, 1), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyNetdevAttach, EventType: 2, DevIndex: 1, HasDevIndex: true,
				Port: 1, HasPort: true, NetdevIndex: 3, HasNetdevIndex: true}},
			[]bool{false}, nil},
		{"register", "positive", "captured REGISTER of a held device", "Topology, never Fatal",
			capturedRDMAEvent(t, 0), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyRegister, EventType: 0, DevIndex: 1, HasDevIndex: true}},
			[]bool{false}, nil},
		{"all_kinds", "positive", "every known event type in one datagram", "one kind per type, in order",
			func() []byte {
				var b []byte
				for v := range byte(6) {
					b = append(b, notification(event(v))...)
				}
				return b
			}(), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyRegister}, {Kind: RDMANotifyUnregister, EventType: 1},
				{Kind: RDMANotifyNetdevAttach, EventType: 2}, {Kind: RDMANotifyNetdevDetach, EventType: 3},
				{Kind: RDMANotifyRename, EventType: 4}, {Kind: RDMANotifyNetdevRename, EventType: 5}},
			[]bool{false, false, false, false, false, false}, nil},
		{"unknown_event", "negative", "event type 255", "emitted as Unknown with its raw type; decoding continues",
			append(notification(event(255)), capturedRDMAEvent(t, 0)...), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyUnknown, EventType: 255},
				{Kind: RDMANotifyRegister, DevIndex: 1, HasDevIndex: true}},
			[]bool{false, false}, nil},
		{"malformed", "negative", "two-byte EVENT_TYPE", "ErrReply, nothing emitted",
			notification(event(1, 0)), heldRxe1, nil, nil, ErrReply},
		{"first_unknown", "boundary", "event type 6, one past NETDEV_RENAME", "Unknown",
			notification(event(6)), heldRxe1,
			[]RDMANotification{{Kind: RDMANotifyUnknown, EventType: 6}}, []bool{false}, nil},
		{"unregister_unmapped", "corner", "captured UNREGISTER of a device the monitor does not hold", "Topology only",
			capturedRDMAEvent(t, 2), func(uint32) bool { return false },
			[]RDMANotification{{Kind: RDMANotifyUnregister, EventType: 1, DevIndex: 1, HasDevIndex: true}},
			[]bool{false}, nil},
		{"unregister_no_handle", "corner", "UNREGISTER with EVENT_TYPE alone", "Topology only; held is not asked",
			notification(event(1)), func(uint32) bool { t.Error("held called without a device"); return true },
			[]RDMANotification{{Kind: RDMANotifyUnregister, EventType: 1}}, []bool{false}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var got []RDMANotification
			var fatal []bool
			err := DecodeRDMANotificationsInto(tc.data, func(n RDMANotification) bool {
				got = append(got, n)
				fatal = append(fatal, n.Fatal(tc.held))
				return true
			})
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err=%v", tc.expected, err)
			}
			if !slices.Equal(got, tc.want) || !slices.Equal(fatal, tc.wantFatal) {
				t.Fatalf("%s:\n got %+v %v\nwant %+v %v", tc.expected, got, fatal, tc.want, tc.wantFatal)
			}
		})
	}
}

// TestRDMANotificationsLegacyParity is the legacy_callback row: over every
// input the notification table and fuzz seeds use, DecodeRDMANotifications
// calls back exactly as often as DecodeRDMANotificationsInto emits, with the
// same error, including when the callback stops early.
func TestRDMANotificationsLegacyParity(t *testing.T) {
	t.Log("boundary: legacy_callback; expected: same callback count and error as DecodeRDMANotificationsInto")
	inputs := [][]byte{capturedRDMAEvents, notificationFixture(), nil, capturedRDMAEvents[:len(capturedRDMAEvents)-1],
		append(notification(notifyAttribute(rdmaEventType, []byte{255})), capturedRDMAEvents...)}
	for i, data := range inputs {
		for _, limit := range []int{-1, 0, 1, 2} {
			legacy, into := 0, 0
			errLegacy := DecodeRDMANotifications(data, func() bool { legacy++; return limit < 0 || legacy <= limit })
			errInto := DecodeRDMANotificationsInto(data, func(RDMANotification) bool { into++; return limit < 0 || into <= limit })
			if legacy != into || fmt.Sprint(errLegacy) != fmt.Sprint(errInto) {
				t.Fatalf("input %d limit %d: legacy %d (%v), into %d (%v)", i, limit, legacy, errLegacy, into, errInto)
			}
		}
	}
	// The captured buffer's three events, as TestRDMANotifications counts them.
	n := 0
	if err := DecodeRDMANotifications(capturedRDMAEvents, func() bool { n++; return true }); err != nil || n != 3 {
		t.Fatalf("captured: %d callbacks, %v", n, err)
	}
	stop := 0
	if err := DecodeRDMANotifications(capturedRDMAEvents, func() bool { stop++; return false }); !errors.Is(err, unix.ENOBUFS) || stop != 1 {
		t.Fatalf("stop: %d callbacks, %v", stop, err)
	}
}
