package xtcpnl

import (
	"bytes"
	"errors"
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// sockID builds an inet_diag_sockid: ports, src[16], dst[16], if, cookie.
func sockID(src, dst netip.Addr) []byte {
	b := make([]byte, InetDiagSockIDSizeCst)
	b[0], b[1], b[2], b[3] = 0x0f, 0xa0, 0xc3, 0x50 // 4000, 50000
	put := func(off int, a netip.Addr) {
		if a.Is4() {
			v := a.As4()
			copy(b[off:off+4], v[:])
			return
		}
		v := a.As16()
		copy(b[off:off+16], v[:])
	}
	put(4, src)
	put(20, dst)
	return b
}

// TestSockIDXTCPOwnsAddresses pins that a decoded record owns its source
// and destination bytes. The receive buffer goes back to the pool after
// its packet is decoded and is reused for the next packet while the record
// waits in the envelope for the poll's flush, so a record that aliased the
// buffer picked up a later socket's address: in the tcp-families microVM,
// AF_INET rows carried the AF_INET6 bytes of a later dump. Before the copy,
// buffer_reused and family_switch failed.
func TestSockIDXTCPOwnsAddresses(t *testing.T) {
	v4a, v4b := netip.MustParseAddr("192.0.2.1"), netip.MustParseAddr("192.0.2.2")
	v6a, v6b := netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("2001:db8::2")
	type expectation struct {
		src, dst []byte
		err      error
	}
	as16 := func(a netip.Addr) []byte {
		if a.Is4() {
			v := a.As4()
			return append(v[:], make([]byte, 12)...)
		}
		v := a.As16()
		return v[:]
	}
	tests := []struct {
		name        string
		category    string
		description string
		first       []byte // decoded into the record first (nil: none)
		data        []byte // then this; the buffer is overwritten with 0xff after
		expected    expectation
	}{
		{
			name: "buffer_reused", category: "positive",
			description: "an IPv4 record keeps its addresses when the buffer is overwritten",
			data:        sockID(v4a, v4b),
			expected:    expectation{src: as16(v4a), dst: as16(v4b)},
		},
		{
			name: "buffer_reused_v6", category: "positive",
			description: "an IPv6 record keeps its addresses when the buffer is overwritten",
			data:        sockID(v6a, v6b),
			expected:    expectation{src: as16(v6a), dst: as16(v6b)},
		},
		{
			name: "short", category: "negative",
			description: "a short sock id is an error and leaves the record's addresses alone",
			first:       sockID(v4a, v4b), data: sockID(v6a, v6b)[:InetDiagSockIDSizeCst-1],
			expected: expectation{src: as16(v4a), dst: as16(v4b), err: ErrInetDiagSockIDSmall},
		},
		{
			name: "exact_size", category: "boundary",
			description: "a sock id of exactly 48 bytes decodes",
			data:        sockID(v6b, v6a),
			expected:    expectation{src: as16(v6b), dst: as16(v6a)},
		},
		{
			name: "family_switch", category: "corner",
			description: "a pooled record that held an IPv6 address, reused for IPv4, has no IPv6 bytes left",
			first:       sockID(v6a, v6b), data: sockID(v4a, v4b),
			expected: expectation{src: as16(v4a), dst: as16(v4b)},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var x xtcp_flat_record.XtcpFlatRecord
			if tc.first != nil {
				buf := bytes.Clone(tc.first)
				if err := DeserializeInetDiagSockIDXTCP(buf, &x); err != nil {
					t.Fatal(err)
				}
				for i := range buf {
					buf[i] = 0xff
				}
			}
			buf := bytes.Clone(tc.data)
			err := DeserializeInetDiagSockIDXTCP(buf, &x)
			for i := range buf {
				buf[i] = 0xff
			}
			got := expectation{src: x.InetDiagMsgSocketSource, dst: x.InetDiagMsgSocketDestination, err: err}
			if !bytes.Equal(got.src, tc.expected.src) || !bytes.Equal(got.dst, tc.expected.dst) || !errors.Is(got.err, tc.expected.err) {
				t.Errorf("%s (%s): %s\n got      src %x dst %x err %v\n expected src %x dst %x err %v",
					tc.name, tc.category, tc.description, got.src, got.dst, got.err, tc.expected.src, tc.expected.dst, tc.expected.err)
			}
		})
	}
}

// TestSockIDXTCPNoAllocs pins the copy's cost: a record reused from the
// pool decodes into its own address buffers, allocating nothing.
func TestSockIDXTCPNoAllocs(t *testing.T) {
	var x xtcp_flat_record.XtcpFlatRecord
	data := sockID(netip.MustParseAddr("2001:db8::1"), netip.MustParseAddr("192.0.2.1"))
	if err := DeserializeInetDiagSockIDXTCP(data, &x); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _ = DeserializeInetDiagSockIDXTCP(data, &x) }); n != 0 {
		t.Errorf("%v allocations per decode, expected 0", n)
	}
}
