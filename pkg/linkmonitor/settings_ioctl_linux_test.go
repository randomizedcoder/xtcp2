package linkmonitor

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

func TestSettingsIoctlIdentityFence(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failCheck, wantCalls                         int
	}{
		{"stable", "positive", "identity stable before and after ioctl", "accept complete result", 0, 1},
		{"before", "negative", "interface gone before ioctl", "no ioctl issued", 1, 0},
		{"after", "corner", "rename or index reuse during ioctl", "discard completed result", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			checks, calls := 0, 0
			c := &ethtoolIoctl{validate: func(context.Context, string, uint32) error {
				checks++
				if checks == tc.failCheck {
					return unix.ENODEV
				}
				return nil
			}, invoke: func(string, []byte) error { calls++; return nil }}
			m, err := c.read(t.Context(), "eth0", 1, xtcpnl.EthtoolRingsKind)
			if calls != tc.wantCalls || (err != nil) != (tc.failCheck != 0) || (tc.failCheck != 0 && m.Rings != nil) {
				t.Fatalf("%s: calls %d err %v", tc.expectedOutcome, calls, err)
			}
		})
	}
}

func TestSettingsIoctlHandshakeCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		before                                       bool
		calls                                        int
	}{
		{"before", "negative", "context canceled before handshake", "no syscall", true, 0},
		{"between", "corner", "handshake completes after cancellation", "no second syscall or payload allocation", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.before {
				cancel()
			}
			calls := 0
			c := &ethtoolIoctl{invoke: func(_ string, data []byte) error { calls++; data[15] = 252; cancel(); return nil }}
			m, err := c.modes(ctx, "eth0")
			if !errors.Is(err, context.Canceled) || calls != tc.calls || m != nil {
				t.Fatalf("%s: calls %d err %v", tc.expectedOutcome, calls, err)
			}
		})
	}
}

func TestSettingsIoctlHandshake(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		words                                        int
		change                                       bool
		failure                                      error
		wantCalls                                    int
		wantError                                    error
	}{
		{"minimum", "boundary", "one bitmap word", "two calls and owned modern bitmap", 1, false, nil, 2, nil},
		{"maximum", "boundary", "maximum positive signed count", "bounded allocation and two calls", maxLinkModeWords, false, nil, 2, nil},
		{"zero", "negative", "zero requested count returned", "reject before second call", 0, false, nil, 1, linuxio.ErrReply},
		{"oversize", "boundary", "negative 128 cannot be returned positively", "reject before allocating payload", 128, false, nil, 1, linuxio.ErrReply},
		{"positive handshake", "negative", "unexpected positive initial count", "reject handshake", -1, false, nil, 1, linuxio.ErrReply},
		{"changed count", "corner", "second call changes requested count", "reject inconsistent bitmap", 4, true, nil, 2, linuxio.ErrReply},
		{"unsupported", "negative", "modern API unavailable", "return unsupported without legacy API", 4, false, unix.EOPNOTSUPP, 1, unix.EOPNOTSUPP},
		{"permission", "negative", "ioctl denied", "retain permission error", 4, false, unix.EPERM, 1, unix.EPERM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			calls := 0
			c := &ethtoolIoctl{invoke: func(_ string, data []byte) error {
				calls++
				if binary.NativeEndian.Uint32(data) != unix.ETHTOOL_GLINKSETTINGS {
					t.Fatal("non-modern or mutating command")
				}
				if tc.failure != nil {
					return tc.failure
				}
				if calls == 1 {
					data[15] = byte(-tc.words)
					return nil
				}
				if len(data) != linkSettingsHeader+12*tc.words {
					t.Fatal("unbounded payload")
				}
				if tc.change {
					data[15]++
					return nil
				}
				binary.NativeEndian.PutUint32(data[4:], 10000)
				data[8] = 1
				binary.NativeEndian.PutUint32(data[linkSettingsHeader:], 1<<12)
				return nil
			}}
			m, err := c.modes(t.Context(), "eth0")
			if !errors.Is(err, tc.wantError) || calls != tc.wantCalls {
				t.Fatalf("%s: err %v, calls %d", tc.expectedOutcome, err, calls)
			}
			if err == nil && (len(m.Ours.Mask) != tc.words || *m.Speed != 10000 || m.Ours.Mask[0] != 1<<12) {
				t.Fatalf("%s: modes %+v", tc.expectedOutcome, m)
			}
		})
	}
}

func TestSettingsIoctlConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         xtcpnl.EthtoolKind
		command                                      uint32
	}{
		{"channels", "positive", "eight legacy channel values", "current and maxima preserved", xtcpnl.EthtoolChannelsKind, unix.ETHTOOL_GCHANNELS},
		{"rings", "corner", "eight legacy ring values", "legacy fields only, no invented extensions", xtcpnl.EthtoolRingsKind, unix.ETHTOOL_GRINGPARAM},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := &ethtoolIoctl{invoke: func(_ string, data []byte) error {
				if binary.NativeEndian.Uint32(data) != tc.command {
					t.Fatal("incorrect command")
				}
				for i := range 8 {
					binary.NativeEndian.PutUint32(data[4+i*4:], uint32(i))
				}
				return nil
			}}
			m, err := c.readKind(t.Context(), "eth0", tc.kind)
			if err != nil {
				t.Fatal(err)
			}
			if samples := configurationSamples(&m); len(samples) != 8 {
				t.Fatalf("%s: %d samples", tc.expectedOutcome, len(samples))
			}
			if m.Rings != nil && m.Rings.RXBufLen != nil {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
