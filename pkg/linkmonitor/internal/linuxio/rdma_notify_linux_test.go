package linuxio

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func notifyAttribute(id uint16, value []byte) []byte {
	b := make([]byte, (4+len(value)+3)&^3)
	binary.LittleEndian.PutUint16(b, uint16(4+len(value)))
	binary.LittleEndian.PutUint16(b[2:], id)
	copy(b[4:], value)
	return b
}

// notification frames attributes as rdma_nl_notify_event does: type MONITOR
// with zero sequence, sender and flags.
func notification(attrs ...[]byte) []byte {
	b := make([]byte, 16, 128)
	binary.LittleEndian.PutUint16(b[4:], rdmaMonitor)
	for _, a := range attrs {
		b = append(b, a...)
	}
	binary.LittleEndian.PutUint32(b, uint32(len(b)))
	return b
}

func notificationFixture() []byte {
	return notification(notifyAttribute(1, []byte{0, 0, 0, 0}), notifyAttribute(2, []byte("mlx5_0\x00")),
		notifyAttribute(rdmaEventType, []byte{0}))
}

func TestRDMANotifications(t *testing.T) {
	// Attribute shapes follow nldev.c rdma_nl_notify_event: fill_nldev_handle for
	// (un)register and rename, fill_mon_netdev_association for attach/detach and
	// fill_mon_netdev_rename for netdev rename.
	dev := func(name string) [][]byte {
		return [][]byte{notifyAttribute(1, word(4)), notifyAttribute(2, []byte(name+"\x00"))}
	}
	ndev := func(name string) [][]byte {
		return [][]byte{notifyAttribute(50, word(7)), notifyAttribute(51, []byte(name+"\x00"))}
	}
	event := func(v ...byte) []byte { return notifyAttribute(rdmaEventType, v) }
	msg := func(event []byte, parts ...[][]byte) []byte {
		var attrs [][]byte
		for _, p := range parts {
			attrs = append(attrs, p...)
		}
		return notification(append(attrs, event)...)
	}
	port := [][]byte{notifyAttribute(3, word(1))}
	header := func(offset int, v byte) []byte {
		b := msg(event(0), dev("mlx5_0"))
		b[offset] = v
		return b
	}
	for _, tc := range []struct {
		category, description, expected string
		data                            []byte
		hints                           int
		ok                              bool
	}{
		{"positive", "register event with the device handle", "one topology hint", msg(event(0), dev("mlx5_0")), 1, true},
		{"positive", "unregister event with the device handle", "one topology hint", msg(event(1), dev("mlx5_0")), 1, true},
		{"positive", "device rename event with the device handle", "one topology hint", msg(event(4), dev("rocep1s0")), 1, true},
		{"positive", "netdev attach with device, port and netdev", "one topology hint", msg(event(2), dev("mlx5_0"), port, ndev("enp1s0")), 1, true},
		{"positive", "netdev detach after the netdev is gone: device and port only", "one topology hint", msg(event(3), dev("mlx5_0"), port), 1, true},
		{"positive", "netdev rename: netdev index and name only, no device handle", "one topology hint", msg(event(5), ndev("lmr0")), 1, true},
		{"corner", "attach whose netdev is in another namespace: EVENT_TYPE only", "one topology hint", msg(event(2)), 1, true},
		{"corner", "netdev rename not visible in this namespace: EVENT_TYPE only", "one topology hint", msg(event(5)), 1, true},
		{"corner", "unknown future event with an unknown attribute", "conservative topology hint", msg(event(255), [][]byte{notifyAttribute(999, []byte{1})}), 1, true},
		{"corner", "two notifications in one datagram", "two topology hints", append(msg(event(0), dev("mlx5_0")), msg(event(5), ndev("lmr0"))...), 2, true},
		{"corner", "valid notification followed by a malformed one", "first hint delivered, then rejected", append(msg(event(0), dev("mlx5_0")), msg(event(0, 0))...), 1, false},
		{"positive", "the captured monitor_add events: REGISTER, NETDEV_ATTACH and UNREGISTER of rxe1", "three topology hints", capturedRDMAEvents, 3, true},
		{"negative", "device handle without EVENT_TYPE", "reject before delivery", notification(dev("mlx5_0")...), 0, false},
		{"negative", "a 4-byte CAP_FLAGS in an event, which xtcpnl checks and linkmonitor used to skip", "reject width", msg(event(0), dev("mlx5_0"), [][]byte{notifyAttribute(4, word(0))}), 0, false},
		{"negative", "a slash in the device name", "reject name", msg(event(0), dev("mlx/0")), 0, false},
		{"negative", "nonzero sequence: a request reply on the event socket", "reject before delivery", header(8, 1), 0, false},
		{"negative", "nonzero header sender", "reject before delivery", header(12, 1), 0, false},
		{"negative", "nonzero header flags", "reject before delivery", header(6, 1), 0, false},
		{"negative", "event type repeated", "reject duplicate", msg(event(1), [][]byte{event(0)}), 0, false},
		{"negative", "netdev name repeated", "reject duplicate", msg(event(5), ndev("lmr0"), [][]byte{notifyAttribute(51, []byte("x\x00"))}), 0, false},
		{"negative", "nested flag on the device name", "reject type mismatch", msg(event(0), [][]byte{notifyAttribute(1, word(4)), notifyAttribute(0x8002, []byte("mlx5_0\x00"))}), 0, false},
		{"negative", "netdev index zero", "reject invalid ifindex", msg(event(5), [][]byte{notifyAttribute(50, word(0)), notifyAttribute(51, []byte("lmr0\x00"))}), 0, false},
		{"boundary", "empty EVENT_TYPE", "reject width", msg(event(), dev("mlx5_0")), 0, false},
		{"boundary", "two-byte EVENT_TYPE", "reject width", msg(event(0, 0), dev("mlx5_0")), 0, false},
		{"boundary", "datagram one byte short", "reject malformed envelope", func() []byte { b := msg(event(0), dev("mlx5_0")); return b[:len(b)-1] }(), 0, false},
		{"boundary", "15-byte netdev name, the IFNAMSIZ maximum", "one topology hint", msg(event(5), ndev(strings.Repeat("n", 15))), 1, true},
		{"boundary", "16-byte netdev name, one over IFNAMSIZ", "reject oversized name", msg(event(5), ndev(strings.Repeat("n", 16))), 0, false},
		{"boundary", "63-byte device name, the IB_DEVICE_NAME_MAX maximum", "one topology hint", msg(event(4), dev(strings.Repeat("d", 63))), 1, true},
		{"boundary", "64-byte device name, one over IB_DEVICE_NAME_MAX", "reject oversized name", msg(event(4), dev(strings.Repeat("d", 64))), 0, false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			n := 0
			err := DecodeRDMANotifications(tc.data, func() bool { n++; return true })
			if (err == nil) != tc.ok || n != tc.hints {
				t.Fatalf("%s: hints=%d err=%v", tc.expected, n, err)
			}
		})
	}
}

func TestRDMAMonitorModeTransaction(t *testing.T) {
	// nldev_sys_get_doit replies with type SYS_GET, flags 0, MONITOR_MODE as u8.
	for _, tc := range []struct {
		category, description, expected string
		reply                           func(request []byte, seq uint32) []byte
		wantErr                         error
	}{
		{"positive", "kernel reports monitor mode on", "notifications supported",
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaSysGet, 0, seq, attribute(rdmaMonitorMode, []byte{1}))
			}, nil},
		{"corner", "kernel reports monitor mode off", "EOPNOTSUPP",
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaSysGet, 0, seq, attribute(rdmaMonitorMode, []byte{0}))
			}, unix.EOPNOTSUPP},
		{"corner", "older kernel without the attribute", "EOPNOTSUPP",
			func(_ []byte, seq uint32) []byte { return frame(rdmaSysGet, 0, seq, nil) }, unix.EOPNOTSUPP},
		{"negative", "kernel rejects the request", "EOPNOTSUPP from the error reply",
			func(request []byte, seq uint32) []byte {
				return frame(unix.NLMSG_ERROR, 0, seq, append(word(-int32(unix.EOPNOTSUPP)), request...))
			}, unix.EOPNOTSUPP},
		{"negative", "reply typed as a device GET", "ignored; no reply before the deadline",
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaDeviceGet, 0, seq, attribute(rdmaMonitorMode, []byte{1}))
			}, context.DeadlineExceeded},
		{"boundary", "empty MONITOR_MODE", "reject width",
			func(_ []byte, seq uint32) []byte { return frame(rdmaSysGet, 0, seq, attribute(rdmaMonitorMode, nil)) }, ErrReply},
		{"boundary", "two-byte MONITOR_MODE", "reject width",
			func(_ []byte, seq uint32) []byte {
				return frame(rdmaSysGet, 0, seq, attribute(rdmaMonitorMode, []byte{1, 0}))
			}, ErrReply},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			socket := &scriptSocket{}
			socket.onSend = func(data []byte) {
				if typ := binary.LittleEndian.Uint16(data[4:]); typ != rdmaSysGet {
					t.Errorf("request type %#x, want SYS_GET", typ)
				}
				request := append([]byte(nil), data...)
				socket.batches = []batch{{data: [][]byte{tc.reply(request, binary.LittleEndian.Uint32(data[8:]))}}}
			}
			c := scriptedClient(unix.NETLINK_RDMA, socket)
			t.Cleanup(func() {
				if err := c.Close(); err != nil {
					t.Error(err)
				}
			})
			err := c.monitorMode(t.Context())
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err=%v", tc.expected, err)
			}
		})
	}
}

func TestRDMAMonitorMode(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		body                                         []byte
		enabled, valid                               bool
	}{
		{"on", "positive", "monitor mode one", "coverage supported", notifyAttribute(rdmaMonitorMode, []byte{1}), true, true},
		{"off", "negative", "monitor mode zero", "coverage unavailable", notifyAttribute(rdmaMonitorMode, []byte{0}), false, true},
		{"absent", "boundary", "old kernel without mode", "coverage unavailable", nil, false, true},
		{"future", "corner", "unknown mode value", "malformed mode rejected", notifyAttribute(rdmaMonitorMode, []byte{2}), false, false},
		{"captured", "positive", "the captured sys_show reply, all four sys attributes", "coverage supported", capturedRDMASys, true, true},
		{"netnsWidth", "negative", "a 4-byte NETNS_MODE, which xtcpnl checks and linkmonitor used to skip", "malformed reply rejected",
			append(notifyAttribute(66, word(1)), notifyAttribute(rdmaMonitorMode, []byte{1})...), false, false},
		{"nested", "corner", "NLA_F_NESTED on MONITOR_MODE", "malformed reply rejected", notifyAttribute(rdmaMonitorMode|unix.NLA_F_NESTED, []byte{1}), false, false},
		{"duplicate", "negative", "MONITOR_MODE twice", "malformed reply rejected",
			append(notifyAttribute(rdmaMonitorMode, []byte{1}), notifyAttribute(rdmaMonitorMode, []byte{1})...), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			enabled, err := decodeMonitorMode(tc.body)
			if enabled != tc.enabled || (err == nil) != tc.valid {
				t.Fatal(enabled, err)
			}
		})
	}
}

func FuzzRDMANotifications(f *testing.F) {
	f.Add(notificationFixture())
	f.Add([]byte{})
	f.Add(capturedRDMAEvents)
	// An accepted event is one xtcpnl decodes too: the admission only narrows.
	f.Fuzz(func(t *testing.T, b []byte) {
		err := DecodeRDMANotifications(b, func() bool { return true })
		if err != nil {
			return
		}
		if err := xtcpnl.WalkNetlinkEnvelopes(b, func(e xtcpnl.NetlinkEnvelope) error {
			_, err := xtcpnl.ParseRdmaEvent(e.Body)
			return err
		}); err != nil {
			t.Fatalf("accepted what xtcpnl refuses: %v", err)
		}
	})
}
