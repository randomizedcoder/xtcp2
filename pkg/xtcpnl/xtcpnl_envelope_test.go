package xtcpnl

import (
	"encoding/binary"
	"errors"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

func monitorTestEnvelope(typ uint16, body []byte) []byte {
	out := make([]byte, 16+len(body))
	binary.LittleEndian.PutUint32(out, uint32(len(out)))
	binary.LittleEndian.PutUint16(out[4:], typ)
	binary.LittleEndian.PutUint16(out[6:], unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR)
	binary.LittleEndian.PutUint32(out[8:], 0xffffffff)
	binary.LittleEndian.PutUint32(out[12:], 123)
	copy(out[16:], body)
	return out
}

func TestNetlinkEnvelopes(t *testing.T) {
	rows := []struct {
		name, description string
		typ               uint16
		body              []byte
		want              NetlinkControl
		code              int32
		wantErr           bool
	}{
		{"data", "data and unaligned final body are retained", unix.RTM_NEWLINK, []byte{1}, NetlinkData, 0, false},
		{"ack", "zero ERROR is ACK, not DONE", unix.NLMSG_ERROR, ethtoolTestU32(0), NetlinkACK, 0, false},
		{"error", "negative errno is retained", unix.NLMSG_ERROR, ethtoolTestU32(0xffffffea), NetlinkError, -22, false},
		{"done_empty", "old empty DONE remains distinct", unix.NLMSG_DONE, nil, NetlinkDone, 0, false},
		{"done_failure", "DONE error is not discarded", unix.NLMSG_DONE, ethtoolTestU32(0xfffffff4), NetlinkDone, -12, false},
		{"noop", "NOOP reaches callback", unix.NLMSG_NOOP, nil, NetlinkNoop, 0, false},
		{"overrun", "OVERRUN reaches callback", unix.NLMSG_OVERRUN, nil, NetlinkOverrun, 0, false},
		{"short_error", "partial errno rejected", unix.NLMSG_ERROR, []byte{0, 0, 0}, 0, 0, true},
		{"short_done", "partial DONE status rejected", unix.NLMSG_DONE, []byte{0}, 0, 0, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			data := monitorTestEnvelope(row.typ, row.body)
			calls := 0
			err := WalkNetlinkEnvelopes(data, func(e NetlinkEnvelope) error {
				calls++
				if e.Control != row.want || e.Code != row.code || !reflect.DeepEqual(e.Body, data[16:]) {
					t.Fatalf("envelope = %+v", e)
				}
				if e.Header.Seq != 0xffffffff || e.Header.Pid != 123 || e.Header.Flags != unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR {
					t.Fatalf("lost header: %+v", e.Header)
				}
				if len(e.Body) > 0 && &e.Body[0] != &data[16] {
					t.Fatal("body was copied instead of borrowed")
				}
				return nil
			})
			if (err != nil) != row.wantErr || (!row.wantErr && calls != 1) || (row.wantErr && calls != 0) {
				t.Fatalf("calls=%d error=%v, want error=%v", calls, err, row.wantErr)
			}
		})
	}
}

func TestNetlinkEnvelopeBoundaries(t *testing.T) {
	badLength := monitorTestEnvelope(20, nil)
	binary.LittleEndian.PutUint32(badLength, 0xffffffff)
	rows := []struct {
		name, description string
		data              []byte
		wantErr           bool
	}{
		{"empty", "empty datagram rejected", nil, true},
		{"short_header", "15 bytes rejected", make([]byte, 15), true},
		{"overrun_length", "maximum u32 does not overrun", badLength, true},
		{"undersize_length", "zero message length rejected", make([]byte, 16), true},
		{"partial_padding", "one of three alignment bytes is invalid", append(monitorTestEnvelope(20, []byte{1}), 0), true},
		{"full_padding", "all alignment bytes accepted", append(monitorTestEnvelope(20, []byte{1}), 0, 0, 0), false},
		{"stray_tail", "tail cannot hide malformed next header", append(monitorTestEnvelope(20, nil), 0), true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Log(row.description)
			if err := WalkNetlinkEnvelopes(row.data, nil); (err != nil) != row.wantErr {
				t.Fatalf("error=%v, want error=%v", err, row.wantErr)
			}
		})
	}
	data := ethtoolTestJoin(monitorTestEnvelope(unix.NLMSG_ERROR, ethtoolTestU32(0)), monitorTestEnvelope(20, nil), monitorTestEnvelope(unix.NLMSG_DONE, nil), monitorTestEnvelope(21, nil))
	var got []NetlinkControl
	if err := WalkNetlinkEnvelopes(data, func(e NetlinkEnvelope) error { got = append(got, e.Control); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []NetlinkControl{NetlinkACK, NetlinkData, NetlinkDone, NetlinkData}) {
		t.Fatalf("premature completion: %v", got)
	}
	stop := errors.New("stop")
	calls := 0
	if err := WalkNetlinkEnvelopes(data, func(NetlinkEnvelope) error { calls++; return stop }); !errors.Is(err, stop) || calls != 1 {
		t.Fatalf("callback stop: %v, %d", err, calls)
	}
}

func FuzzWalkNetlinkEnvelopes(f *testing.F) {
	for _, typ := range []uint16{unix.RTM_NEWLINK, unix.NLMSG_ERROR, unix.NLMSG_DONE, unix.NLMSG_OVERRUN} {
		f.Add(monitorTestEnvelope(typ, ethtoolTestU32(0)))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = WalkNetlinkEnvelopes(data, func(e NetlinkEnvelope) error {
			if int(e.Header.Len) != len(e.Body)+16 || cap(e.Body) != len(e.Body) {
				t.Fatalf("invalid envelope: %+v", e.Header)
			}
			return nil
		})
	})
}
