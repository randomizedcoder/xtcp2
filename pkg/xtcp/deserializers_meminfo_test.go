package xtcp

import (
	"context"
	"io"
	"os"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// enabledFromKeys turns a set of deserializer keys into the map shape
// EnabledDeserializers.Enabled uses (key -> true).
func enabledFromKeys(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// TestIDiagExtFromEnabled pins the request-bitmask derivation exhaustively: only
// extensions 1..8 are addressable by the uint8 idiag_ext (bit N-1 requests
// extension N), and a bit is set iff that extension's deserializer is enabled.
// These are the values the netlink request now carries instead of the historical
// hardcoded 127. The per-key sub-table walks EVERY dispatchTable entry so a newly
// added attribute (or an enum change) forces this test to be updated.
func TestIDiagExtFromEnabled(t *testing.T) {
	// Per-key: enabling exactly one deserializer sets exactly its bit when the
	// extension is addressable (enum 1..8), and nothing otherwise.
	t.Run("per-key", func(t *testing.T) {
		vegasBit := uint8(1) << uint8(xtcpnl.VegasInfoEnumValueCst-1) // ext 3, bit 2
		for _, e := range dispatchTable {
			e := e
			t.Run(e.key, func(t *testing.T) {
				var want uint8
				if e.enum >= 1 && e.enum <= 8 {
					want = uint8(1) << uint8(e.enum-1)
				}
				// Congestion-control coupling: vegas/dctcp/bbr all request via the
				// single VEGASINFO bit (ext 3). dctcp(9)/bbr(16) are unaddressable
				// on their own, so enabling them must still set bit 2.
				if e.key == dsKeyVegas || e.key == dsKeyDctcp || e.key == dsKeyBbr {
					want |= vegasBit
				}
				if got := IDiagExtFromEnabled(enabledFromKeys(e.key)); got != want {
					t.Errorf("IDiagExtFromEnabled({%s}) = %d, want %d (enum %d)", e.key, got, want, e.enum)
				}
			})
		}
	})

	// Aggregate / combination cases.
	cases := []struct {
		name    string
		enabled map[string]bool
		want    uint8
	}{
		// all keys -> every addressable bit (1..8) set = 0xFF.
		{name: "all", enabled: enabledFromKeys(GetAllDeserializers()...), want: 255},
		// default (all minus meminfo) -> 0xFF without bit 0 = 254. This is the
		// out-of-the-box request: drops meminfo (redundant with sk_mem_info) and,
		// because shut is in the default set, sets bit 7 (SHUTDOWN) for the first time.
		{name: "default", enabled: enabledFromKeys(GetDefaultDeserializers()...), want: 254},
		{name: "none", enabled: map[string]bool{}, want: 0},
		{name: "nil", enabled: nil, want: 0},
		// meminfo=enum1 (bit 0) + skmem=enum7 (bit 6) = 1 + 64 = 65.
		{name: "meminfo+skmem", enabled: enabledFromKeys(dsKeyMemInfo, dsKeySkmem), want: 65},
		// skmem alone = enum7 = bit 6 = 64.
		{name: "skmem", enabled: enabledFromKeys(dsKeySkmem), want: 64},
		// info=enum2 (bit 1) + tos=enum5 (bit 4) = 2 + 16 = 18.
		{name: "info+tos", enabled: enabledFromKeys(dsKeyInfo, dsKeyTos), want: 18},
		// Congestion-control coupling: bbr(enum16) and dctcp(enum9) are unaddressable
		// on their own, but the kernel gates ALL cc-info on the VEGASINFO bit
		// (ext 3, bit 2 = 4), so enabling either must set bit 2 — otherwise a
		// bbr-only config would silently request nothing (the footgun this guards).
		{name: "bbr-only-sets-vegas-bit", enabled: enabledFromKeys(dsKeyBbr), want: 4},
		{name: "dctcp-only-sets-vegas-bit", enabled: enabledFromKeys(dsKeyDctcp), want: 4},
		// skmem(bit 6=64) + bbr → 64 + VEGASINFO bit (4) = 68.
		{name: "skmem+bbr", enabled: enabledFromKeys(dsKeySkmem, dsKeyBbr), want: 68},
		// cong (enum4, bit 3=8) is the algorithm NAME string, a separate bit; it must
		// NOT pull in the VEGASINFO cc-info bit.
		{name: "cong-only-no-vegas-bit", enabled: enabledFromKeys(dsKeyCong), want: 8},
		// A key that is not in dispatchTable at all is ignored.
		{name: "bogus-key-ignored", enabled: enabledFromKeys("not-a-real-key"), want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IDiagExtFromEnabled(tc.enabled); got != tc.want {
				t.Errorf("IDiagExtFromEnabled(%v) = %d, want %d", tc.enabled, got, tc.want)
			}
		})
	}
}

// TestGetDefaultDeserializers asserts the default set excludes meminfo (redundant
// with sk_mem_info) but retains every other dispatchTable key, including skmem.
func TestGetDefaultDeserializers(t *testing.T) {
	def := GetDefaultDeserializers()
	got := make(map[string]bool, len(def))
	for _, k := range def {
		got[k] = true
	}

	if got[dsKeyMemInfo] {
		t.Errorf("GetDefaultDeserializers should exclude %q; got %v", dsKeyMemInfo, def)
	}
	for _, k := range []string{dsKeySkmem, dsKeyInfo, dsKeyCong, dsKeyShut, dsKeyBbr} {
		if !got[k] {
			t.Errorf("GetDefaultDeserializers should include %q; got %v", k, def)
		}
	}
	// default set = all set minus the off-by-default keys.
	if want := len(GetAllDeserializers()) - len(deserializersOffByDefault); len(def) != want {
		t.Errorf("GetDefaultDeserializers len = %d, want %d (all minus off-by-default)", len(def), want)
	}
}

// newTestRequestXTCP builds the minimal XTCP that CreateNetLinkRequest needs: a
// config carrying the enabled deserializers and an nlmsg seq. CreateNetLinkRequest
// calls wg.Done(), so the caller passes a wg it has Add(1)'d.
func newTestRequestXTCP(enabled map[string]bool) *XTCP {
	x := new(XTCP)
	x.config = &xtcp_config.XtcpConfig{
		NlmsgSeq:             1,
		EnabledDeserializers: &xtcp_config.EnabledDeserializers{Enabled: enabled},
	}
	return x
}

// TestCreateNetLinkRequest_idiagExtByte is the end-to-end request-side table test
// the change hinges on: for a spread of deserializer configs, build the real
// netlink request via CreateNetLinkRequest and assert the serialized idiag_ext
// wire byte (offset 18, see SerializeNetlinkDiagRequest) equals what
// IDiagExtFromEnabled derives. This proves "when different fields are requested we
// ask the kernel for exactly the matching extension bits" — the whole point of
// making the request config-derived instead of the old hardcoded 127.
func TestCreateNetLinkRequest_idiagExtByte(t *testing.T) {
	const idiagExtOffset = 18 // byte offset of idiag_ext in the serialized request

	cases := []struct {
		name    string
		enabled map[string]bool
		want    uint8
	}{
		{name: "default", enabled: enabledFromKeys(GetDefaultDeserializers()...), want: 254},
		{name: "all", enabled: enabledFromKeys(GetAllDeserializers()...), want: 255},
		{name: "none", enabled: map[string]bool{}, want: 0},
		{name: "skmem-only", enabled: enabledFromKeys(dsKeySkmem), want: 64},
		{name: "meminfo+skmem", enabled: enabledFromKeys(dsKeyMemInfo, dsKeySkmem), want: 65},
		{name: "info-only", enabled: enabledFromKeys(dsKeyInfo), want: 2},
		// bbr-only must serialize the VEGASINFO bit (4) so the kernel actually
		// returns BBRINFO — the cc-info coupling, end-to-end on the wire byte.
		{name: "bbr-only", enabled: enabledFromKeys(dsKeyBbr), want: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := newTestRequestXTCP(tc.enabled)
			var wg sync.WaitGroup
			wg.Add(1)
			req := x.CreateNetLinkRequest(&wg)
			wg.Wait()
			if req == nil || len(*req) <= idiagExtOffset {
				t.Fatalf("request too short: len=%d", len(deref(req)))
			}
			// The derived helper and the serialized byte must agree.
			wantDerived := IDiagExtFromEnabled(tc.enabled)
			if wantDerived != tc.want {
				t.Fatalf("test setup: IDiagExtFromEnabled=%d, table want=%d", wantDerived, tc.want)
			}
			if got := (*req)[idiagExtOffset]; got != tc.want {
				t.Errorf("serialized idiag_ext byte = %d, want %d", got, tc.want)
			}
		})
	}
}

func deref(b *[]byte) []byte {
	if b == nil {
		return nil
	}
	return *b
}

// TestMemInfoRedundantWithSkMemInfo confirms on real captured data the finding
// that drove this change: the four mem_info_* fields carry the same values as a
// subset of the sk_mem_info_* fields, because both come from the same kernel sk
// counters. The full-reply fixtures were captured with the old idiag_ext=127
// request so they still carry BOTH attributes; we enable both deserializers and
// assert the four mapped pairs are equal for every decoded row. This guards the
// mapping meminfo{rmem,wmem,fmem,tmem} == skmem{rmem_alloc,wmem_queued,fwd_alloc,
// wmem_alloc}.
//
// NOTE on values: the mapped counters are 0 for the idle sockets in these captures
// (see the "most examples I have are zeros" note in
// pkg/xtcpnl/xtcpnl_inet_diag_meminfo_test.go). Equality still proves the mapping;
// the per-attribute deserializer unit tests in pkg/xtcpnl exercise the non-zero
// byte copy, and the microvm clickhouse-pipeline self-test exercises live non-zero
// kernel values. We log the max observed value so a future non-zero fixture is
// visible rather than silently vacuous.
func TestMemInfoRedundantWithSkMemInfo(t *testing.T) {
	fixtures := []string{
		"../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet2.pcap",
		"../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet_from_10k.pcap",
	}

	var totalRows int
	var maxVal uint32
	for _, fixture := range fixtures {
		x := newTestDeserializeXTCP(t)
		// Register both meminfo and skmem so both attributes decode into the record.
		x.config.EnabledDeserializers = &xtcp_config.EnabledDeserializers{
			Enabled: enabledFromKeys(dsKeyMemInfo, dsKeySkmem),
		}
		var wg sync.WaitGroup
		wg.Add(1)
		x.InitDeserializers(&wg)
		wg.Wait()

		// Never flush during the test so the rows stay in currentEnvelope.
		x.config.EnvelopeFlushThresholdRows = 1_000_000
		x.config.EnvelopeFlushThresholdBytes = 1 << 30
		x.currentEnvelope = &xtcp_flat_record.Envelope{}

		f, err := os.Open(fixture)
		if err != nil {
			t.Fatalf("open %s: %v", fixture, err)
		}
		bs, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("read %s: %v", fixture, err)
		}
		buf := bs[xtcpnl.PcapNetlinkOffsetCst:]

		nsName := "test-ns-name"
		n, errD := x.Deserialize(context.Background(), DeserializeArgs{
			ns:             &nsName,
			fd:             0,
			NLPacket:       &buf,
			xtcpRecordPool: &x.xtcpRecordPool,
			nlhPool:        &x.nlhPool,
			rtaPool:        &x.rtaPool,
			pC:             x.pC,
			pH:             x.pH,
			id:             0,
		})
		if errD != nil {
			t.Fatalf("%s: Deserialize err: %v (n=%d)", fixture, errD, n)
		}
		if n == 0 || len(x.currentEnvelope.Row) == 0 {
			t.Fatalf("%s: no records produced: n=%d rows=%d", fixture, n, len(x.currentEnvelope.Row))
		}

		for i, r := range x.currentEnvelope.Row {
			if r.GetMemInfoRmem() != r.GetSkMemInfoRmemAlloc() {
				t.Errorf("%s row %d: mem_info_rmem=%d != sk_mem_info_rmem_alloc=%d", fixture, i, r.GetMemInfoRmem(), r.GetSkMemInfoRmemAlloc())
			}
			if r.GetMemInfoWmem() != r.GetSkMemInfoWmemQueued() {
				t.Errorf("%s row %d: mem_info_wmem=%d != sk_mem_info_wmem_queued=%d", fixture, i, r.GetMemInfoWmem(), r.GetSkMemInfoWmemQueued())
			}
			if r.GetMemInfoFmem() != r.GetSkMemInfoFwdAlloc() {
				t.Errorf("%s row %d: mem_info_fmem=%d != sk_mem_info_fwd_alloc=%d", fixture, i, r.GetMemInfoFmem(), r.GetSkMemInfoFwdAlloc())
			}
			if r.GetMemInfoTmem() != r.GetSkMemInfoWmemAlloc() {
				t.Errorf("%s row %d: mem_info_tmem=%d != sk_mem_info_wmem_alloc=%d", fixture, i, r.GetMemInfoTmem(), r.GetSkMemInfoWmemAlloc())
			}
			for _, v := range []uint32{r.GetMemInfoRmem(), r.GetMemInfoWmem(), r.GetMemInfoFmem(), r.GetMemInfoTmem()} {
				if v > maxVal {
					maxVal = v
				}
			}
			totalRows++
		}
	}

	t.Logf("TestMemInfoRedundantWithSkMemInfo: checked %d rows across %d fixtures; max mapped mem value observed=%d",
		totalRows, len(fixtures), maxVal)
}
