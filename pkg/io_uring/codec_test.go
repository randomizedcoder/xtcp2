package io_uring

import "testing"

func TestCodecRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		req  EncodedRequest
		want uint64
	}{
		{
			name: "read_reqid_0",
			req:  EncodedRequest{Operation: OpRead, RequestID: 0},
			want: 0x0000_0000_0000_0000,
		},
		{
			name: "read_reqid_mid",
			req:  EncodedRequest{Operation: OpRead, RequestID: 0x12345678},
			want: 0x0000_0000_1234_5678,
		},
		{
			name: "send_udp_reqid_1",
			req:  EncodedRequest{Operation: OpSendUDP, RequestID: 1},
			want: 0x0100_0000_0000_0001,
		},
		{
			name: "send_unix_reqid_max",
			req:  EncodedRequest{Operation: OpSendUnix, RequestID: 0xFFFFFFFF},
			want: 0x0200_0000_FFFF_FFFF,
		},
		{
			name: "send_unixgram_reqid_high_bit",
			req:  EncodedRequest{Operation: OpSendUnixGram, RequestID: 0x8000_0000},
			want: 0x0300_0000_8000_0000,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := serialize(c.req)
			if got != c.want {
				t.Errorf("serialize(%+v) = 0x%016x, want 0x%016x", c.req, got, c.want)
			}
			back := deserialize(got)
			if back != c.req {
				t.Errorf("deserialize(0x%016x) = %+v, want %+v", got, back, c.req)
			}
		})
	}
}

// TestCodecReservedBitsAreZero confirms the 24 reserved bits stay zero
// for every (Operation, RequestID) pair — if we later use them, this is
// the test to update first.
func TestCodecReservedBitsAreZero(t *testing.T) {
	for op := Operation(0); op < 0xFF; op++ {
		for _, rid := range []uint32{0, 1, 0x12345678, 0xFFFFFFFF} {
			u := serialize(EncodedRequest{Operation: op, RequestID: rid})
			reserved := (u >> 32) & 0x00FF_FFFF
			if reserved != 0 {
				t.Fatalf("op=%d rid=0x%x: reserved bits non-zero: 0x%x", op, rid, reserved)
			}
		}
	}
}
