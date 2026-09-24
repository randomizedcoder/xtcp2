//go:build enrich_asn

package xtcp

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/output"
	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
)

// asnIdx returns the live *ipasn.Index behind x's asnLookuper seam, or nil
// when the ASN enricher was not installed. The tests assert against the table
// actually answering lookups, which the seam deliberately hides from untagged
// code.
func asnIdx(x *XTCP) *ipasn.Index {
	a, ok := x.asn.(asnIndex)
	if !ok {
		return nil
	}
	return a.idx
}

// ---- initAsnEnricher --------------------------------------------------------
//
// Drives the ASN enricher's start-up and refresh decisions against real Parquet
// artifacts written with the collector's own writer, with a short refresh
// interval so the background loop is observed within the test.

var (
	asnRowsA = []model.Record{
		{Prefix: "1.1.1.0/24", IPVersion: 4, ASN: 13335, NetworkOwner: "cloudflare"},
		{Prefix: "8.8.8.0/24", IPVersion: 4, ASN: 15169, NetworkOwner: "google"},
	}
	asnRowsB = []model.Record{
		{Prefix: "1.1.1.0/24", IPVersion: 4, ASN: 1, NetworkOwner: "replaced-owner"},
	}
)

func writeAsnArtifact(t *testing.T, path string, rows []model.Record) {
	t.Helper()
	if _, err := output.WriteParquet(path, rows); err != nil {
		t.Fatalf("WriteParquet(%s): %v", path, err)
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestInitAsnEnricher
func TestInitAsnEnricher(t *testing.T) {
	const tick = 20 * time.Millisecond

	tests := []struct {
		description string
		enable      bool
		pathMode    string // "valid" | "missing" | "empty" | "corrupt"
		interval    time.Duration
		wantIndex   bool   // the ASN seam is installed after init
		wantOwner   string // owner of 1.1.1.1 right after init ("" = miss)
		// after is an optional second phase exercising the refresh loop.
		after func(t *testing.T, x *XTCP, path string)
	}{
		// positive
		{"enabled, valid artifact, interval 0 -> index live, no refresh", true, "valid", 0, true, "cloudflare", nil},
		{"enabled, valid artifact, refresh on -> index live", true, "valid", tick, true, "cloudflare", nil},
		{"refresh picks up a rewritten artifact", true, "valid", tick, true, "cloudflare",
			func(t *testing.T, x *XTCP, path string) {
				// Make sure the mtime differs from the first write even on coarse filesystems.
				time.Sleep(15 * time.Millisecond)
				writeAsnArtifact(t, path, asnRowsB)
				waitFor(t, "reload of the rewritten artifact", func() bool {
					a, ok := asnIdx(x).Lookup(netip.MustParseAddr("1.1.1.1"))
					return ok && a.NetworkOwner == "replaced-owner"
				})
				if _, ok := asnIdx(x).Lookup(netip.MustParseAddr("8.8.8.8")); ok {
					t.Error("old prefix still present after reload (table not swapped atomically)")
				}
			}},
		{"unchanged artifact is not rebuilt on the tick (stat short-circuit)", true, "valid", tick, true, "cloudflare",
			func(t *testing.T, x *XTCP, _ string) {
				waitFor(t, "a few refresh ticks", func() bool {
					return testutil.ToFloat64(x.pC.WithLabelValues("refreshAsn", "reload", "unchanged")) >= 3
				})
				if ok := testutil.ToFloat64(x.pC.WithLabelValues("refreshAsn", "reload", "ok")); ok != 0 {
					t.Errorf("reload/ok = %v on an unchanged file, want 0", ok)
				}
			}},

		// negative
		{"disabled -> no index", false, "valid", tick, false, "", nil},
		{"enabled but asn_db_path empty -> no index", true, "empty", tick, false, "", nil},
		{"enabled, missing artifact, interval 0 -> disabled (nothing would ever retry)", true, "missing", 0, false, "", nil},
		{"enabled, corrupt artifact, interval 0 -> disabled", true, "corrupt", 0, false, "", nil},

		// corner — retry armed although the first load failed
		{"enabled, missing artifact, refresh on -> empty index installed, loads once the file appears", true, "missing", tick, true, "",
			func(t *testing.T, x *XTCP, path string) {
				waitFor(t, "at least one failed reload attempt", func() bool {
					return testutil.ToFloat64(x.pC.WithLabelValues("refreshAsn", "reload", "error")) >= 1
				})
				writeAsnArtifact(t, path, asnRowsA)
				waitFor(t, "late-arriving artifact to load", func() bool {
					a, ok := asnIdx(x).Lookup(netip.MustParseAddr("1.1.1.1"))
					return ok && a.NetworkOwner == "cloudflare"
				})
			}},
		{"a bad refresh keeps the table in service", true, "valid", tick, true, "cloudflare",
			func(t *testing.T, x *XTCP, path string) {
				time.Sleep(15 * time.Millisecond)
				writeAsnArtifact(t, path, nil) // zero-row artifact -> ErrNoPrefixes on reload
				waitFor(t, "the failed reload to be counted", func() bool {
					return testutil.ToFloat64(x.pC.WithLabelValues("refreshAsn", "reload", "error")) >= 1
				})
				if a, ok := asnIdx(x).Lookup(netip.MustParseAddr("1.1.1.1")); !ok || a.NetworkOwner != "cloudflare" {
					t.Errorf("table degraded after failed reload: (%+v,%v)", a, ok)
				}
			}},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newMetricsFixture(t, time.Minute)
			dir := t.TempDir()
			path := filepath.Join(dir, "feeds.parquet")
			switch tc.pathMode {
			case "valid":
				writeAsnArtifact(t, path, asnRowsA)
			case "corrupt":
				writeBytesFile(t, path, []byte("not parquet"))
			case "empty":
				path = ""
			case "missing":
				// leave absent
			}
			x.config = &xtcp_config.XtcpConfig{
				EnrichAsnEnable:    tc.enable,
				AsnDbPath:          path,
				AsnRefreshInterval: durationpb.New(tc.interval),
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			x.initAsnEnricher(ctx)

			if (asnIdx(x) != nil) != tc.wantIndex {
				t.Fatalf("asn enricher installed = %v, want %v", asnIdx(x) != nil, tc.wantIndex)
			}
			if asnIdx(x) != nil {
				a, ok := asnIdx(x).Lookup(netip.MustParseAddr("1.1.1.1"))
				if got := ownerOrEmpty(a.NetworkOwner, ok); got != tc.wantOwner {
					t.Errorf("Lookup(1.1.1.1) owner = %q, want %q", got, tc.wantOwner)
				}
			}
			if tc.after != nil {
				tc.after(t, x, path)
			}
		})
	}
}

// ---- loadAsn metrics ----------------------------------------------------------
//
// The lookup table's operational metrics live under function="loadAsn":
// gauges prefixes / artifactBytes / loadedAt and the build / error duration
// summaries. These rows drive loadAsn directly (no ticker) so each outcome is
// observed deterministically.

// summaryCount returns the sample count of the pH summary at (function, variable).
func summaryCount(t *testing.T, x *XTCP, function, variable string) uint64 {
	t.Helper()
	m := &dto.Metric{}
	obs, ok := x.pH.WithLabelValues(function, variable, "duration").(prometheus.Metric)
	if !ok {
		t.Fatalf("pH observer for %s/%s is not a prometheus.Metric", function, variable)
	}
	if err := obs.Write(m); err != nil {
		t.Fatalf("Write summary %s/%s: %v", function, variable, err)
	}
	return m.GetSummary().GetSampleCount()
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestLoadAsnMetrics
func TestLoadAsnMetrics(t *testing.T) {
	tests := []struct {
		description string
		// prime, when set, is run first with force=true against a valid artifact
		// so the row starts from a populated table.
		prime bool
		// pathMode picks what the measured loadAsn call sees.
		pathMode string // "valid" | "rewritten" | "missing" | "corrupt" | "zeroRow"
		force    bool

		wantReloaded   bool
		wantErr        bool
		wantPrefixes   float64 // loadAsn/prefixes gauge afterwards
		wantBytesPos   bool    // loadAsn/artifactBytes gauge > 0
		wantLoadedAt   bool    // loadAsn/loadedAt gauge > 0
		wantBuildCount uint64  // loadAsn/build duration samples
		wantErrCount   uint64  // loadAsn/error duration samples
	}{
		// positive
		{"forced start-up load of a valid artifact publishes the table size and one build sample",
			false, "valid", true, true, false, 2, true, true, 1, 0},
		{"stat-gated load on an empty index still loads (zero index always loads)",
			false, "valid", false, true, false, 2, true, true, 1, 0},
		{"rewritten artifact on the refresh path moves the gauge and adds a build sample",
			true, "rewritten", false, true, false, 1, true, true, 2, 0},
		// corner — nothing to do
		{"unchanged artifact on the refresh path: no new build sample, gauges as before",
			true, "valid", false, false, false, 2, true, true, 1, 0},
		// negative — failures leave the published table alone
		{"missing artifact on start-up: error sample, gauges stay 0",
			false, "missing", true, false, true, 0, false, false, 0, 1},
		{"corrupt artifact on start-up: error sample, gauges stay 0",
			false, "corrupt", true, false, true, 0, false, false, 0, 1},
		{"zero-row artifact on the refresh path keeps the good table's gauges and counts the error",
			true, "zeroRow", false, false, true, 2, true, true, 1, 1},
		{"artifact deleted between ticks keeps the good table's gauges and counts the error",
			true, "missing", false, false, true, 2, true, true, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newMetricsFixture(t, time.Minute)
			path := filepath.Join(t.TempDir(), "feeds.parquet")
			idx := &ipasn.Index{}
			if tc.prime {
				writeAsnArtifact(t, path, asnRowsA)
				if _, err := x.loadAsn(idx, path, true); err != nil {
					t.Fatalf("prime load: %v", err)
				}
			}
			switch tc.pathMode {
			case "valid":
				if !tc.prime {
					writeAsnArtifact(t, path, asnRowsA)
				}
			case "rewritten":
				time.Sleep(15 * time.Millisecond) // distinct mtime on coarse filesystems
				writeAsnArtifact(t, path, asnRowsB)
			case "missing":
				_ = os.Remove(path)
			case "corrupt":
				writeBytesFile(t, path, []byte("not parquet"))
			case "zeroRow":
				time.Sleep(15 * time.Millisecond)
				writeAsnArtifact(t, path, nil)
			}

			reloaded, err := x.loadAsn(idx, path, tc.force)
			if reloaded != tc.wantReloaded {
				t.Errorf("reloaded = %v, want %v", reloaded, tc.wantReloaded)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tc.wantErr)
			}

			gauge := func(variable string) float64 {
				return testutil.ToFloat64(x.pGV.WithLabelValues("loadAsn", variable, "gauge"))
			}
			if got := gauge("prefixes"); got != tc.wantPrefixes {
				t.Errorf("loadAsn/prefixes gauge = %v, want %v", got, tc.wantPrefixes)
			}
			if got := gauge("artifactBytes"); (got > 0) != tc.wantBytesPos {
				t.Errorf("loadAsn/artifactBytes gauge = %v, want >0: %v", got, tc.wantBytesPos)
			}
			if got := gauge("loadedAt"); (got > 0) != tc.wantLoadedAt {
				t.Errorf("loadAsn/loadedAt gauge = %v, want >0: %v", got, tc.wantLoadedAt)
			}
			if got := summaryCount(t, x, "loadAsn", "build"); got != tc.wantBuildCount {
				t.Errorf("loadAsn/build duration samples = %d, want %d", got, tc.wantBuildCount)
			}
			if got := summaryCount(t, x, "loadAsn", "error"); got != tc.wantErrCount {
				t.Errorf("loadAsn/error duration samples = %d, want %d", got, tc.wantErrCount)
			}
			// The gauge must agree with the table actually answering lookups.
			if st := idx.Stats(); float64(st.Prefixes) != gauge("prefixes") {
				t.Errorf("gauge %v disagrees with idx.Stats().Prefixes %d", gauge("prefixes"), st.Prefixes)
			}
		})
	}
}

func ownerOrEmpty(owner string, ok bool) string {
	if !ok {
		return ""
	}
	return owner
}
