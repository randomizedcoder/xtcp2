package xtcp

import (
	"net/netip"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// The stamping hot path, driven through the untagged seams. Both asnLookuper
// and localityEnricher are declared in untagged code precisely so a fake can
// be installed on XTCP in any build flavor, which is what lets this file stay
// untagged and run in all four: applyEnrichment's contract (when it no-ops,
// which columns it writes, and the self/local-subnet short-circuit that skips
// the ASN feed) is flavor-independent.

// fakeAsn is an asnLookuper over a fixed table.
type fakeAsn struct {
	byAddr map[string]struct {
		asn   uint32
		owner string
	}
	calls int
}

func (f *fakeAsn) LookupAsn(addr netip.Addr) (uint32, string, bool) {
	f.calls++
	a, ok := f.byAddr[addr.String()]
	return a.asn, a.owner, ok
}

// fakeLocality is a localityEnricher over a fixed per-inode table.
type fakeLocality struct {
	active  bool
	byInode map[uint64]localityResult
	// lastBound records the boundIfindex Resolve was called with, so the
	// kernel's idiag_if can be shown to reach the resolver.
	lastBound uint32
}

func (f *fakeLocality) Refresh(map[uint64]nsIdentity) {}
func (f *fakeLocality) Active() bool                  { return f.active }
func (f *fakeLocality) Resolve(inode uint64, _ netip.Addr, boundIfindex uint32) (localityResult, bool) {
	f.lastBound = boundIfindex
	r, ok := f.byInode[inode]
	return r, ok
}

// ipv4 / ipv6 build the kernel's 16-byte __be32[4] destination slot.
func ipv4(a, b, c, d byte) []byte { return append([]byte{a, b, c, d}, make([]byte, 12)...) }

func ipv6(s string) []byte {
	addr := netip.MustParseAddr(s).As16()
	return addr[:]
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestDestAddr
func TestDestAddr(t *testing.T) {
	tests := []struct {
		description string
		family      uint32
		buf         []byte
		wantOK      bool
		wantAddr    string
	}{
		// positive
		{"AF_INET reads the first 4 bytes of the 16-byte slot", unix.AF_INET, ipv4(1, 1, 1, 1), true, "1.1.1.1"},
		{"AF_INET6 reads all 16 bytes", unix.AF_INET6, ipv6("2001:db8::1"), true, "2001:db8::1"},
		{"an IPv4-mapped IPv6 address is unmapped to v4", unix.AF_INET6, ipv6("::ffff:8.8.8.8"), true, "8.8.8.8"},
		// boundary — exactly the minimum length for each family
		{"AF_INET accepts a 4-byte buffer", unix.AF_INET, []byte{10, 0, 0, 1}, true, "10.0.0.1"},
		{"AF_INET6 accepts a 16-byte buffer", unix.AF_INET6, ipv6("fe80::1"), true, "fe80::1"},
		{"AF_INET ignores trailing bytes beyond the first 4", unix.AF_INET, ipv4(192, 168, 0, 1), true, "192.168.0.1"},
		// negative
		{"AF_INET with 3 bytes is rejected", unix.AF_INET, []byte{1, 1, 1}, false, ""},
		{"AF_INET6 with 15 bytes is rejected", unix.AF_INET6, make([]byte, 15), false, ""},
		{"a nil buffer is rejected", unix.AF_INET, nil, false, ""},
		{"an empty buffer is rejected", unix.AF_INET, []byte{}, false, ""},
		{"family 0 is rejected even with a full buffer", 0, ipv4(1, 1, 1, 1), false, ""},
		{"AF_UNIX is rejected", unix.AF_UNIX, ipv4(1, 1, 1, 1), false, ""},
		// corner — the all-zero destination a listening socket carries
		{"the all-zero v4 destination still parses", unix.AF_INET, make([]byte, 16), true, "0.0.0.0"},
		// :: is the v6 unspecified address, NOT ::ffff:0.0.0.0, so Unmap
		// leaves it alone — the v4 and v6 zero destinations stay distinct.
		{"the all-zero v6 destination stays the v6 unspecified address", unix.AF_INET6, make([]byte, 16), true, "::"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			addr, ok := destAddr(tc.family, tc.buf)
			if ok != tc.wantOK {
				t.Fatalf("destAddr ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				if addr.IsValid() {
					t.Errorf("destAddr returned %v with ok=false, want the zero Addr", addr)
				}
				return
			}
			if addr.String() != tc.wantAddr {
				t.Errorf("destAddr = %v, want %v", addr, tc.wantAddr)
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestApplyEnrichmentSeams
func TestApplyEnrichmentSeams(t *testing.T) {
	const inode = uint64(4026531840)

	cloudflare := map[string]struct {
		asn   uint32
		owner string
	}{
		"1.1.1.1": {13335, "cloudflare"},
	}

	tests := []struct {
		description string
		// asn / locality install the seams; nil means "that enricher is not
		// present", which is exactly what a build without the tag produces.
		asn      *fakeAsn
		locality *fakeLocality

		family uint32
		dest   []byte
		bound  uint32

		wantAsn      uint64
		wantOwner    string
		wantLocality xtcp_flat_record.XtcpFlatRecord_Locality
		wantEgressIf uint32
		wantEgressNm string
		wantBoundNm  string
		wantAsnCalls int
	}{
		// boundary — nothing installed is the slim-build case and must be a
		// complete no-op, not merely a lookup that misses.
		{description: "neither enricher installed stamps nothing",
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1)},

		// positive — ASN alone
		{description: "asn alone stamps the asn columns",
			asn:    &fakeAsn{byAddr: cloudflare},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantAsn: 13335, wantOwner: "cloudflare", wantAsnCalls: 1},
		{description: "an asn miss leaves the columns empty but still consults the table",
			asn:    &fakeAsn{byAddr: cloudflare},
			family: unix.AF_INET, dest: ipv4(9, 9, 9, 9),
			wantAsnCalls: 1},

		// positive — locality alone
		{description: "locality alone stamps the locality and interface columns",
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{
				inode: {
					Locality:      xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE,
					EgressIfindex: 2, EgressIfname: "eth0", BoundIfname: "eth0", Remote: true,
				},
			}},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1), bound: 2,
			wantLocality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE,
			wantEgressIf: 2, wantEgressNm: "eth0", wantBoundNm: "eth0"},

		// positive — both, the production combination
		{description: "a remote destination is classified and then looked up",
			asn: &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{
				inode: {Locality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE, EgressIfindex: 2, EgressIfname: "eth0", Remote: true},
			}},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantAsn: 13335, wantOwner: "cloudflare",
			wantLocality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_REMOTE,
			wantEgressIf: 2, wantEgressNm: "eth0", wantAsnCalls: 1},

		// corner — the short-circuit this whole ordering exists for: a self or
		// connected-subnet destination must NOT reach the internet ASN feed.
		{description: "a self destination skips the asn lookup entirely",
			asn: &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{
				inode: {Locality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_SELF, Remote: false},
			}},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantLocality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_SELF, wantAsnCalls: 0},
		{description: "a local-subnet destination skips the asn lookup entirely",
			asn: &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{
				inode: {Locality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_LOCAL_SUBNET, EgressIfindex: 3, EgressIfname: "eth1", Remote: false},
			}},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantLocality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_LOCAL_SUBNET,
			wantEgressIf: 3, wantEgressNm: "eth1", wantAsnCalls: 0},

		// corner — locality installed but with no snapshot for this namespace:
		// remote defaults to true so the ASN lookup behaves as if locality
		// were off, rather than silently suppressing it.
		{description: "an unknown namespace leaves locality unset and still runs the asn lookup",
			asn:      &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{}},
			family:   unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantAsn: 13335, wantOwner: "cloudflare", wantAsnCalls: 1},

		// corner — installed but no pass has published yet (Active false):
		// the whole block must be skipped, including the ASN half when ASN is
		// not installed either.
		{description: "an inactive locality enricher with no asn is a full no-op",
			locality: &fakeLocality{active: false, byInode: map[uint64]localityResult{
				inode: {Locality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_SELF},
			}},
			family: unix.AF_INET, dest: ipv4(1, 1, 1, 1)},
		{description: "an inactive locality enricher does not suppress the asn lookup",
			asn:      &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: false},
			family:   unix.AF_INET, dest: ipv4(1, 1, 1, 1),
			wantAsn: 13335, wantOwner: "cloudflare", wantAsnCalls: 1},

		// negative — an undecodable destination must not reach either seam
		{description: "a short destination buffer reaches neither seam",
			asn: &fakeAsn{byAddr: cloudflare},
			locality: &fakeLocality{active: true, byInode: map[uint64]localityResult{
				inode: {Locality: xtcp_flat_record.XtcpFlatRecord_LOCALITY_SELF},
			}},
			family: unix.AF_INET, dest: []byte{1, 1}, wantAsnCalls: 0},
		{description: "an unknown address family reaches neither seam",
			asn:    &fakeAsn{byAddr: cloudflare},
			family: 0, dest: ipv4(1, 1, 1, 1), wantAsnCalls: 0},

		// positive — IPv6 travels the same path
		{description: "an IPv4-mapped IPv6 destination is looked up as IPv4",
			asn:    &fakeAsn{byAddr: cloudflare},
			family: unix.AF_INET6, dest: ipv6("::ffff:1.1.1.1"),
			wantAsn: 13335, wantOwner: "cloudflare", wantAsnCalls: 1},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := new(XTCP)
			if tc.asn != nil {
				x.asn = tc.asn
			}
			if tc.locality != nil {
				x.locality = tc.locality
			}

			r := &xtcp_flat_record.XtcpFlatRecord{
				NetnsInode:                   inode,
				InetDiagMsgFamily:            tc.family,
				InetDiagMsgSocketDestination: tc.dest,
				InetDiagMsgSocketInterface:   tc.bound,
			}
			x.applyEnrichment(r)

			if r.EnrichSocketDestAsn != tc.wantAsn {
				t.Errorf("EnrichSocketDestAsn = %d, want %d", r.EnrichSocketDestAsn, tc.wantAsn)
			}
			if r.EnrichSocketDestNetworkOwner != tc.wantOwner {
				t.Errorf("EnrichSocketDestNetworkOwner = %q, want %q", r.EnrichSocketDestNetworkOwner, tc.wantOwner)
			}
			if r.EnrichSocketDestLocality != tc.wantLocality {
				t.Errorf("EnrichSocketDestLocality = %v, want %v", r.EnrichSocketDestLocality, tc.wantLocality)
			}
			if r.EnrichSocketDestEgressIfindex != tc.wantEgressIf {
				t.Errorf("EnrichSocketDestEgressIfindex = %d, want %d", r.EnrichSocketDestEgressIfindex, tc.wantEgressIf)
			}
			if r.EnrichSocketDestEgressIfname != tc.wantEgressNm {
				t.Errorf("EnrichSocketDestEgressIfname = %q, want %q", r.EnrichSocketDestEgressIfname, tc.wantEgressNm)
			}
			if r.EnrichSocketInterfaceName != tc.wantBoundNm {
				t.Errorf("EnrichSocketInterfaceName = %q, want %q", r.EnrichSocketInterfaceName, tc.wantBoundNm)
			}
			if tc.asn != nil && tc.asn.calls != tc.wantAsnCalls {
				t.Errorf("asn lookups = %d, want %d", tc.asn.calls, tc.wantAsnCalls)
			}
			// The kernel's idiag_if must reach the resolver unchanged — it is
			// what names the socket's bound interface.
			if tc.locality != nil && tc.locality.active && tc.bound != 0 && tc.locality.lastBound != tc.bound {
				t.Errorf("Resolve got boundIfindex %d, want %d", tc.locality.lastBound, tc.bound)
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestRefreshLocalityDispatch
func TestRefreshLocalityDispatch(t *testing.T) {
	tests := []struct {
		description string
		install     bool
	}{
		{"with no locality enricher installed it is a no-op", false},
		{"with one installed the call is forwarded", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := new(XTCP)
			var got bool
			if tc.install {
				x.locality = &recordingLocality{onRefresh: func() { got = true }}
			}
			// Must not panic in either case: ns_discover.go calls this on
			// every reconcile, in every build flavor.
			x.refreshLocality(map[uint64]nsIdentity{1: {inode: 1}})
			if got != tc.install {
				t.Errorf("Refresh forwarded = %v, want %v", got, tc.install)
			}
		})
	}
}

type recordingLocality struct {
	onRefresh func()
}

func (r *recordingLocality) Refresh(map[uint64]nsIdentity) { r.onRefresh() }
func (r *recordingLocality) Active() bool                  { return false }
func (r *recordingLocality) Resolve(uint64, netip.Addr, uint32) (localityResult, bool) {
	return localityResult{}, false
}
