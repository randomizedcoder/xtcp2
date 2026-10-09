package xtcpnl

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDeserializeRtaCacheinfo drives the struct read on its own.
//
// Every positive row here is constructed rather than captured, and that is
// forced by the corpus rather than chosen: all 48 RTA_CACHEINFO attributes in
// the committed fixtures are 32 zero bytes, because the kernel fills three of
// the eight members only behind `if (dst)` and the dump path passes NULL. A
// table built from captured bytes alone would assert nothing but "zero decodes
// as zero" and would pass with every field offset shifted. The real-fixture
// assertions live in TestParseNewRouteCacheinfo below, which is where the
// presence/absence split per family IS evidence.
//
// Field order is the kernel's: clntref, lastuse, expires, error, used, id, ts,
// tsage. One member at a time, so a transposed pair cannot hide behind a row
// that sets everything at once.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeRtaCacheinfo
func TestDeserializeRtaCacheinfo(t *testing.T) {
	// full is a payload with each member set to a distinct value that is also
	// its 1-based position, so a misread offset lands on an obviously wrong
	// number rather than on a plausible one.
	full := u32le(11, 22, 33, 44, 55, 66, 77, 88)

	tests := []struct {
		description string
		data        []byte
		want        RtaCacheinfo
		wantN       int
		wantErrIs   error
	}{
		{
			description: "positive: all eight members decode to distinct values in kernel field order (constructed; the corpus is all zeros)",
			data:        full,
			want: RtaCacheinfo{
				Clntref: 11, Lastuse: 22, Expires: 33, Error: 44,
				Used: 55, ID: 66, Ts: 77, Tsage: 88,
			},
			wantN: 32,
		},
		{
			description: "positive: clntref alone lands in Clntref and nowhere else — the member `-s` prints as `users`",
			data:        u32le(7, 0, 0, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{Clntref: 7},
			wantN:       32,
		},
		{
			description: "positive: lastuse alone lands in Lastuse — `age`, at offset 4, NOT offset 8",
			data:        u32le(0, 7, 0, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{Lastuse: 7},
			wantN:       32,
		},
		{
			description: "positive: expires alone lands in Expires at offset 8, between lastuse and error",
			data:        u32le(0, 0, 7, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{Expires: 7},
			wantN:       32,
		},
		{
			description: "positive: error alone lands in Error",
			data:        u32le(0, 0, 0, 7, 0, 0, 0, 0),
			want:        RtaCacheinfo{Error: 7},
			wantN:       32,
		},
		{
			description: "positive: used alone lands in Used at offset 16 — it follows error, it does not precede it",
			data:        u32le(0, 0, 0, 0, 7, 0, 0, 0),
			want:        RtaCacheinfo{Used: 7},
			wantN:       32,
		},
		{
			description: "positive: id alone lands in ID, the member behind the `ipid` token",
			data:        u32le(0, 0, 0, 0, 0, 7, 0, 0),
			want:        RtaCacheinfo{ID: 7},
			wantN:       32,
		},
		{
			description: "positive: ts alone lands in Ts",
			data:        u32le(0, 0, 0, 0, 0, 0, 7, 0),
			want:        RtaCacheinfo{Ts: 7},
			wantN:       32,
		},
		{
			description: "positive: tsage alone lands in Tsage, the last member",
			data:        u32le(0, 0, 0, 0, 0, 0, 0, 7),
			want:        RtaCacheinfo{Tsage: 7},
			wantN:       32,
		},
		{
			description: "boundary: exactly 32 bytes of zeros decodes cleanly — this is what every committed v6 route actually carries",
			data:        u32le(0, 0, 0, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{},
			wantN:       32,
		},
		{
			description: "boundary: 31 bytes, one short (constructed; the kernel never emits a short one) — refused outright, not partially decoded",
			data:        full[:31],
			wantErrIs:   ErrRtaCacheinfoSmall,
		},
		{
			description: "boundary: 28 bytes, exactly one member short — still refused, because this is a fixed-width struct and not a growable array",
			data:        full[:28],
			wantErrIs:   ErrRtaCacheinfoSmall,
		},
		{
			description: "boundary: 36 bytes, a longer future struct (constructed) — the first 32 decode and the remainder is ignored, as a C struct cast would do",
			data:        append(append([]byte{}, full...), u32le(999)...),
			want: RtaCacheinfo{
				Clntref: 11, Lastuse: 22, Expires: 33, Error: 44,
				Used: 55, ID: 66, Ts: 77, Tsage: 88,
			},
			wantN: 32,
		},
		{
			description: "negative: a nil payload is refused rather than decoding as the zero struct — absent and all-zero are different states on this attribute",
			data:        nil,
			wantErrIs:   ErrRtaCacheinfoSmall,
		},
		{
			description: "negative: an empty payload is refused",
			data:        []byte{},
			wantErrIs:   ErrRtaCacheinfoSmall,
		},
		{
			description: "corner: rta_expires = -1 decodes SIGNED to -1, not to 4294967295 — the struct's one __s32, and the member the kernel negates for a lapsed expiry",
			data:        u32le(0, 0, 0xFFFFFFFF, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{Expires: -1},
			wantN:       32,
		},
		{
			description: "corner: rta_expires = INT32_MIN, the most negative value, survives the round trip",
			data:        u32le(0, 0, 0x80000000, 0, 0, 0, 0, 0),
			want:        RtaCacheinfo{Expires: -2147483648},
			wantN:       32,
		},
		{
			description: "corner: 0xFFFFFFFF in every member — the seven unsigned ones read as 4294967295 while expires alone reads as -1, which is the whole point of the mixed signedness",
			data:        u32le(0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF),
			want: RtaCacheinfo{
				Clntref: 4294967295, Lastuse: 4294967295, Expires: -1,
				Error: 4294967295, Used: 4294967295, ID: 4294967295,
				Ts: 4294967295, Tsage: 4294967295,
			},
			wantN: 32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var got RtaCacheinfo
			n, err := DeserializeRtaCacheinfo(tt.data, &got)

			if tt.wantErrIs != nil {
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("err = %v, want %v", err, tt.wantErrIs)
				}
				if n != 0 {
					t.Errorf("n = %d on error, want 0", n)
				}
				if got != (RtaCacheinfo{}) {
					t.Errorf("struct was written on a refused payload: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}
			if got != tt.want {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

// TestRtaUserHzDivision pins the USER_HZ truncation that print_rta_cacheinfo
// applies to `age` and `expires`.
//
// Separate from the render tests because the divisor is a decode-side
// constant with a derivation worth pinning on its own: `ip` divides with C
// integer division and prints the truncated integer, never a fraction.
//
// go test ./pkg/xtcpnl/ -run TestRtaUserHzDivision
func TestRtaUserHzDivision(t *testing.T) {
	tests := []struct {
		description string
		ticks       int32
		want        int32
	}{
		{
			description: "positive: the divisor is 100, which is USER_HZ and what `getconf CLK_TCK` reports",
			ticks:       RtaUserHzCst,
			want:        1,
		},
		{
			description: "positive: 30000 ticks is 300sec",
			ticks:       30000,
			want:        300,
		},
		{
			description: "boundary: 149 truncates to 1sec, not 1.49 and not 2 — integer division, not rounding",
			ticks:       149,
			want:        1,
		},
		{
			description: "boundary: 99 truncates to 0sec, so a sub-second age prints as `age 0sec` rather than being suppressed — suppression keys on the RAW member being zero, not the divided one",
			ticks:       99,
			want:        0,
		},
		{
			description: "boundary: 1 tick is 0sec",
			ticks:       1,
			want:        0,
		},
		{
			description: "corner: a negative expiry truncates toward zero on both sides — C99 and Go agree, so -149 is -1sec rather than -2sec",
			ticks:       -149,
			want:        -1,
		},
		{
			description: "corner: INT32_MAX ticks does not overflow the division",
			ticks:       2147483647,
			want:        21474836,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := tt.ticks / RtaUserHzCst; got != tt.want {
				t.Errorf("%d/%d = %d, want %d",
					tt.ticks, RtaUserHzCst, got, tt.want)
			}
		})
	}
}

// TestParseNewRouteCacheinfo asserts the one thing the committed captures CAN
// prove about RTA_CACHEINFO: which routes carry it, and that every one that
// does carries 32 zero bytes.
//
// That split is the evidence, and it is sharp. The kernel reaches
// rtnl_put_cacheinfo from exactly two places, and on a DUMP only the IPv6 one
// is live: rt6_fill_node calls it unconditionally (net/ipv6/route.c:5944),
// while the IPv4 FIB dump never calls it at all. So "every v6 route has the
// attribute and no v4 route does" is a statement about kernel structure, not
// about these topologies, and a decoder that dropped the attribute entirely
// would pass a value-only table while failing this one.
//
// The all-zero assertion is the other half, and it is why the `-s` row for
// route show is a no-op rather than a feature: rta_clntref, rta_lastuse and
// rta_used are the three members `-s` gates, and the kernel writes them only
// behind `if (dst)`, which a dump never satisfies.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteCacheinfo
func TestParseNewRouteCacheinfo(t *testing.T) {
	tests := []struct {
		description string
		path        string
		// dumpSet selects the reader. The 7_1_4 dumps keep the whole
		// transaction — request record then reply records — so they are
		// walked by readDumpSetReplies, while the 7_1_8 captures are
		// replies-only and use readDumpFixture.
		dumpSet bool
		// wantWith is the number of routes carrying RTA_CACHEINFO, and
		// wantTotal the number of routes in the fixture. Both are exact:
		// "some have it" would pass with the family guard inverted.
		wantWith  int
		wantTotal int
	}{
		{
			description: "negative: the v4 route dump carries NO RTA_CACHEINFO on any route — fib_dump_info never calls rtnl_put_cacheinfo",
			path:        tdDumpGetRoute_7_1_4,
			dumpSet:     true,
			wantWith:    0,
			wantTotal:   7,
		},
		{
			description: "positive: every route in the v6 dump carries RTA_CACHEINFO — rt6_fill_node emits it unconditionally, with or without -s",
			path:        tdDumpGetRoute6_7_1_4,
			dumpSet:     true,
			wantWith:    5,
			wantTotal:   5,
		},
		{
			description: "corner: the mixed-family `table all` dump splits — only its v6 routes carry the attribute, which is the family dependence stated as one fixture",
			path:        tdDumpGetRouteAll_7_1_4,
			dumpSet:     true,
			wantWith:    13,
			wantTotal:   28,
		},
		{
			description: "negative: the mesh namespace's v4 dump carries none either — the absence tracks the family, not the topology",
			path:        tdDumpMeshGetRoute_7_1_4,
			dumpSet:     true,
			wantWith:    0,
			wantTotal:   2,
		},
		// THE TUNNEL NAMESPACE, which was predicted to be where this table
		// finally met a non-zero member and is not.
		//
		// The prediction had a reason: a tunnel route is the kind that can
		// carry a lifetime, and rta_expires is the ONE member reachable
		// without a dst — net/ipv6/route.c:5931 takes it from rt->expires
		// under RTF_EXPIRES — so a route with a finite lifetime would make
		// plain `-6 route show` print `expires Nsec`. All four dumps were
		// probed and every RTA_CACHEINFO in them is 32 zero bytes, because
		// nothing in build_tunnel sets a lifetime on anything. The rows stay
		// because the absence is now measured rather than assumed, and
		// because they extend the family split to a third topology.
		{
			description: "negative: the tunnel namespace's v4 dump carries no RTA_CACHEINFO, on routes out of a gre device",
			path:        tdDumpTunnelGetRoute_7_1_4,
			dumpSet:     true,
			wantWith:    0,
			wantTotal:   2,
		},
		{
			description: "positive: the tunnel namespace's v6 dump carries it on both routes, all 32 bytes zero",
			path:        tdDumpTunnelGetRoute6_7_1_4,
			dumpSet:     true,
			wantWith:    2,
			wantTotal:   2,
		},
		{
			// The `dev` form, which is a FILTERED dump rather than a full
			// one. Worth its own row because the filter is applied by the
			// kernel on the v4 FIB walk, and a filtered walk reaching
			// rtnl_put_cacheinfo where an unfiltered one does not would be a
			// difference no other row could see.
			description: "boundary: `route show dev gre1` is a filtered dump and still carries none",
			path:        tdDumpTunnelGetRouteDev_7_1_4,
			dumpSet:     true,
			wantWith:    0,
			wantTotal:   2,
		},
		{
			description: "corner: the tunnel `table all` dump splits by family like the clean one, across four route types",
			path:        tdDumpTunnelGetRouteAll_7_1_4,
			dumpSet:     true,
			wantWith:    6,
			wantTotal:   13,
		},
		{
			description: "boundary: the 7_1_8 host dump, the largest in the corpus, holds 48 v6 routes with the attribute out of 74 total",
			path:        tdRouteGetRouteDump_7_1_8,
			wantWith:    48,
			wantTotal:   74,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var bodies [][]byte
			if tt.dumpSet {
				bodies, _ = readDumpSetReplies(t, tt.path, uint16(unix.RTM_NEWROUTE))
			} else {
				bodies, _ = readDumpFixture(t, tt.path, uint16(unix.RTM_NEWROUTE))
			}
			if len(bodies) != tt.wantTotal {
				t.Fatalf("route count = %d, want %d", len(bodies), tt.wantTotal)
			}

			with := 0
			for i, b := range bodies {
				ri, err := ParseNewRoute(b)
				if err != nil {
					t.Fatalf("ParseNewRoute(msg %d): %v", i, err)
				}
				if ri.CacheInfo == nil {
					continue
				}
				with++

				// Every captured attribute is all-zero. If this ever fails the
				// finding is not in this test: a non-zero rta_expires means
				// plain `route show` renders a token, so the ALREADY-GATED
				// route_show rows are what to re-measure first.
				if *ri.CacheInfo != (RtaCacheinfo{}) {
					t.Errorf("msg %d (family %d): cacheinfo = %+v, want all zero; "+
						"a non-zero member here changes what plain `route show` prints",
						i, ri.Family, *ri.CacheInfo)
				}

				// The family guard, asserted from the wire rather than from
				// the kernel source quoted above.
				if ri.Family != unix.AF_INET6 {
					t.Errorf("msg %d: RTA_CACHEINFO on a family-%d route; "+
						"only AF_INET6 dumps carry it", i, ri.Family)
				}
			}

			if with != tt.wantWith {
				t.Errorf("routes with RTA_CACHEINFO = %d, want %d", with, tt.wantWith)
			}
		})
	}
}
