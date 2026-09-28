package xtcpnl

import (
	"errors"
	"testing"
)

// This file covers IFA_CACHEINFO's struct reader and the two lifetime
// predicates on AddrInfo.
//
// The lifetimes are the one part of an address that is wall-clock state rather
// than configuration, so they are also the part a parity comparison has to
// normalize. Getting them wrong is invisible in a single run and shows up as an
// unstable diff two runs later — which is why the predicates exist as named
// functions with their own table instead of as inline comparisons at the call
// site.

// TestDeserializeIfaCacheinfo covers the fixed-offset reader for
// struct ifa_cacheinfo.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeIfaCacheinfo
func TestDeserializeIfaCacheinfo(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		want        IfaCacheinfo
		wantN       int
		wantErr     error
	}{
		{
			// The four fields are distinct little-endian u32s in kernel order:
			// the preferred lifetime, the valid lifetime, cstamp, tstamp.
			// Distinct values are the point — equal ones would pass even with
			// two offsets swapped.
			description: "positive: four distinct u32s decode in kernel field order",
			data: concat(
				le32(0x11111111), le32(0x22222222), le32(0x33333333), le32(0x44444444),
			),
			want: IfaCacheinfo{
				Preferred: 0x11111111, Valid: 0x22222222,
				Cstamp: 0x33333333, Tstamp: 0x44444444,
			},
			wantN: IfaCacheinfoSizeCst,
		},
		{
			// The shape of every permanent address in the committed dumps:
			// both lifetimes at INFINITY_LIFE_TIME, timestamps at whatever the
			// boot clock said.
			description: "positive: INFINITY_LIFE_TIME in both lifetimes, as ip_addr_n:4 renders forever",
			data: concat(
				le32(IfaLifetimeInfinityCst), le32(IfaLifetimeInfinityCst), le32(108), le32(108),
			),
			want: IfaCacheinfo{
				Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
				Cstamp: 108, Tstamp: 108,
			},
			wantN: IfaCacheinfoSizeCst,
		},
		{
			description: "boundary: exactly IfaCacheinfoSizeCst bytes of zeros",
			data:        make([]byte, IfaCacheinfoSizeCst),
			want:        IfaCacheinfo{},
			wantN:       IfaCacheinfoSizeCst,
		},
		{
			// Forward compatibility: if the kernel struct ever grows, a longer
			// attribute must still decode the four fields that exist rather
			// than being rejected.
			description: "boundary: 20 bytes decodes the first 16 and ignores the rest",
			data: concat(
				le32(1), le32(2), le32(3), le32(4), le32(0xdeadbeef),
			),
			want:  IfaCacheinfo{Preferred: 1, Valid: 2, Cstamp: 3, Tstamp: 4},
			wantN: IfaCacheinfoSizeCst,
		},
		{
			// One byte short. ParseNewAddr turns this into "no cacheinfo"
			// rather than failing the address; see the 15-byte row in
			// TestParseNewAddr.
			description: "negative: 15 bytes -> ErrIfaCacheinfoSmall",
			data:        make([]byte, IfaCacheinfoSizeCst-1),
			wantErr:     ErrIfaCacheinfoSmall,
		},
		{
			description: "negative: nil -> ErrIfaCacheinfoSmall",
			data:        nil,
			wantErr:     ErrIfaCacheinfoSmall,
		},
		{
			// All-ones is not a sentinel for the timestamps, only for the
			// lifetimes, so it decodes verbatim in all four fields.
			description: "corner: all 0xff bytes decode verbatim in every field",
			data:        []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			want: IfaCacheinfo{
				Preferred: 0xffffffff, Valid: 0xffffffff,
				Cstamp: 0xffffffff, Tstamp: 0xffffffff,
			},
			wantN: IfaCacheinfoSizeCst,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got IfaCacheinfo
			n, err := DeserializeIfaCacheinfo(tc.data, &got)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if n != 0 {
					t.Errorf("n = %d on error, want 0", n)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if got != tc.want {
				t.Errorf("IfaCacheinfo = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestAddrInfoLifetimes covers IsPermanent and IsDeprecated.
//
// go test ./pkg/xtcpnl/ -run TestAddrInfoLifetimes
func TestAddrInfoLifetimes(t *testing.T) {
	tests := []struct {
		description   string
		addr          AddrInfo
		wantPermanent bool
		wantDeprecate bool
	}{
		{
			// ip_addr_n:4 "valid_lft forever preferred_lft forever"
			description: "positive: both lifetimes infinite is permanent",
			addr: AddrInfo{
				HasCacheInfo: true,
				CacheInfo: IfaCacheinfo{
					Preferred: IfaLifetimeInfinityCst, Valid: IfaLifetimeInfinityCst,
				},
			},
			wantPermanent: true,
		},
		{
			// ip_addr_n:11 "valid_lft 47871sec preferred_lft 47871sec" — the
			// DHCP address, which is exactly the one `ip` does NOT call forever.
			description: "positive: a finite lifetime is not permanent",
			addr: AddrInfo{
				HasCacheInfo: true,
				CacheInfo:    IfaCacheinfo{Preferred: 47877, Valid: 47877},
			},
		},
		{
			// ip_addr_n:15 "valid_lft 34941sec preferred_lft 0sec"
			description: "positive: preferred 0 with a live valid lifetime is deprecated",
			addr: AddrInfo{
				HasCacheInfo: true,
				CacheInfo:    IfaCacheinfo{Preferred: 0, Valid: 34946},
			},
			wantDeprecate: true,
		},
		{
			// No IFA_CACHEINFO at all. The kernel sends the attribute precisely
			// when there is a lifetime to report, so its absence means the
			// address does not expire — treating absence as "lifetime 0" would
			// report every such address as expired.
			description:   "boundary: no IFA_CACHEINFO is permanent by omission",
			addr:          AddrInfo{},
			wantPermanent: true,
		},
		{
			// Half-infinite: valid forever, preferred finite. Not permanent —
			// `ip` prints "valid_lft forever preferred_lft 3600sec", two
			// different words, so one predicate cannot claim both.
			description: "boundary: valid infinite but preferred finite is neither permanent nor deprecated",
			addr: AddrInfo{
				HasCacheInfo: true,
				CacheInfo:    IfaCacheinfo{Preferred: 3600, Valid: IfaLifetimeInfinityCst},
			},
		},
		{
			// Both zero is an address on its way out, not a deprecated one:
			// a deprecated address is still valid, and this one is not. The
			// row exists because `Preferred == 0` alone would call it
			// deprecated.
			description: "corner: both lifetimes 0 is not deprecated, because it is not still valid",
			addr: AddrInfo{
				HasCacheInfo: true,
				CacheInfo:    IfaCacheinfo{Preferred: 0, Valid: 0},
			},
		},
		{
			// A zero-value CacheInfo with the flag set: the flag is what the
			// predicates read, so this is deliberately distinct from the
			// omission row above.
			description: "corner: HasCacheInfo with a zeroed struct is not permanent",
			addr:        AddrInfo{HasCacheInfo: true},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.addr.IsPermanent(); got != tc.wantPermanent {
				t.Errorf("IsPermanent() = %v, want %v", got, tc.wantPermanent)
			}
			if got := tc.addr.IsDeprecated(); got != tc.wantDeprecate {
				t.Errorf("IsDeprecated() = %v, want %v", got, tc.wantDeprecate)
			}
		})
	}
}
