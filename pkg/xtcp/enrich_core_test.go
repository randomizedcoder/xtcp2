package xtcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// Tests for the compile-time-gated enricher registry. Deliberately UNTAGGED:
// the registry's whole job is to behave correctly in a build that lacks an
// enricher, so these must run in the default `go test ./...` too. Where an
// expectation depends on the tag set, it is derived from EnricherCompiledIn
// rather than hardcoded, so one table covers all four flavors.

// ---- the known set ----------------------------------------------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestIsKnownEnricher
func TestIsKnownEnricher(t *testing.T) {
	tests := []struct {
		description string
		name        string
		want        bool
	}{
		// positive
		{"asn is a known enricher", EnricherAsn, true},
		{"locality is a known enricher", EnricherLocality, true},
		// negative
		{"an invented name is not known", "geoip", false},
		{"a destination scheme is not an enricher", schemeKafka, false},
		// boundary
		{"the empty string is not known", "", false},
		// corner — near misses that a careless caller might pass
		{"the build tag itself is not the enricher name", "enrich_asn", false},
		{"matching is case sensitive", "ASN", false},
		{"surrounding whitespace is not trimmed", " asn", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsKnownEnricher(tc.name); got != tc.want {
				t.Errorf("IsKnownEnricher(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestCompiledInEnrichers
func TestCompiledInEnrichers(t *testing.T) {
	got := CompiledInEnrichers()

	t.Run("every reported enricher is in the known set", func(t *testing.T) {
		for _, n := range got {
			if !IsKnownEnricher(n) {
				t.Errorf("CompiledInEnrichers reported %q, which is not in knownEnrichers", n)
			}
		}
	})

	t.Run("the result is sorted", func(t *testing.T) {
		if !sort.StringsAreSorted(got) {
			t.Errorf("CompiledInEnrichers() = %v, want sorted", got)
		}
	})

	t.Run("no duplicates", func(t *testing.T) {
		seen := map[string]bool{}
		for _, n := range got {
			if seen[n] {
				t.Errorf("CompiledInEnrichers() = %v, %q appears twice", got, n)
			}
			seen[n] = true
		}
	})

	t.Run("it agrees with EnricherCompiledIn for every known enricher", func(t *testing.T) {
		for _, n := range knownEnrichers {
			inList := false
			for _, g := range got {
				if g == n {
					inList = true
				}
			}
			if inList != EnricherCompiledIn(n) {
				t.Errorf("%q: in CompiledInEnrichers()=%v but EnricherCompiledIn=%v", n, inList, EnricherCompiledIn(n))
			}
		}
	})
}

// ---- registration guards ------------------------------------------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestRegisterEnricherPanics
func TestRegisterEnricherPanics(t *testing.T) {
	noop := func(context.Context, *XTCP) {}

	tests := []struct {
		description string
		name        string
		wantPanic   string // substring of the panic message; "" = must not panic
	}{
		// negative — a name that is not in knownEnrichers is a code bug
		{"an unknown enricher name panics", "geoip", "unknown enricher"},
		{"the empty name panics", "", "unknown enricher"},
		{"the build tag spelling panics", "enrich_asn", "unknown enricher"},
		// corner — re-registering a known enricher can only be a build-tag bug.
		// This row is meaningful only in a build that already registered it;
		// in a build without the tag the first registration succeeds instead,
		// which the row below asserts.
		{"re-registering a known enricher panics when it is already compiled in", EnricherAsn, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := tc.wantPanic
			if tc.name == EnricherAsn {
				// Derive rather than hardcode: with -tags enrich_asn the
				// package's own init() already claimed the slot, so a second
				// RegisterEnricher must panic; without the tag it must
				// succeed. Clean up so later tests see the original registry.
				if EnricherCompiledIn(EnricherAsn) {
					want = "called twice"
				} else {
					t.Cleanup(func() {
						enricherRegistryMu.Lock()
						delete(enricherRegistry, EnricherAsn)
						enricherRegistryMu.Unlock()
					})
				}
			}

			defer func() {
				r := recover()
				switch {
				case want == "" && r != nil:
					t.Errorf("RegisterEnricher(%q) panicked unexpectedly: %v", tc.name, r)
				case want != "" && r == nil:
					t.Errorf("RegisterEnricher(%q) did not panic, want panic containing %q", tc.name, want)
				case want != "" && !strings.Contains(toStr(r), want):
					t.Errorf("RegisterEnricher(%q) panicked with %v, want it to contain %q", tc.name, r, want)
				}
			}()
			RegisterEnricher(tc.name, noop)
		})
	}
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}

// ---- lookup + the operator-facing error ----------------------------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestLookupEnricherFactory
func TestLookupEnricherFactory(t *testing.T) {
	tests := []struct {
		description string
		name        string
		// wantStatus is enricherLookupUnknown for anything outside the known
		// set; for a known one it depends on this build's tags, resolved below.
		unknown bool
	}{
		{"asn resolves to found or not-compiled-in depending on the tag", EnricherAsn, false},
		{"locality resolves to found or not-compiled-in depending on the tag", EnricherLocality, false},
		{"an invented name is unknown", "geoip", true},
		{"the empty name is unknown", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			f, status := lookupEnricherFactory(tc.name)

			want := enricherLookupNotCompiledIn
			switch {
			case tc.unknown:
				want = enricherLookupUnknown
			case EnricherCompiledIn(tc.name):
				want = enricherLookupFound
			}
			if status != want {
				t.Errorf("lookupEnricherFactory(%q) status = %v, want %v", tc.name, status, want)
			}
			if (f != nil) != (want == enricherLookupFound) {
				t.Errorf("lookupEnricherFactory(%q) factory != nil = %v, want %v", tc.name, f != nil, want == enricherLookupFound)
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestEnricherLookupError
func TestEnricherLookupError(t *testing.T) {
	tests := []struct {
		description string
		name        string
		status      enricherLookup
		wantNil     bool
		wantSubstrs []string
	}{
		// positive — the found case is not an error at all
		{"found is not an error", EnricherAsn, enricherLookupFound, true, nil},
		// negative
		{"unknown names the valid set", "geoip", enricherLookupUnknown, false,
			[]string{`unknown enricher "geoip"`, EnricherAsn, EnricherLocality}},
		{"not-compiled-in names the flag, the tag and what this build has",
			EnricherAsn, enricherLookupNotCompiledIn, false,
			[]string{"-enrichAsn", "not compiled into this binary", "-tags enrich_asn", "Compiled-in enrichers"}},
		{"not-compiled-in for locality names the locality flag and tag",
			EnricherLocality, enricherLookupNotCompiledIn, false,
			[]string{"-enrichLocality", "-tags enrich_locality"}},
		// corner — an unmapped-but-known name must still produce a usable
		// message rather than an empty flag fragment.
		{"a known enricher with no flag entry falls back to its name",
			"geoip", enricherLookupNotCompiledIn, false, []string{"geoip", "-tags enrich_geoip"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := enricherLookupError(tc.name, tc.status)
			if tc.wantNil {
				if err != nil {
					t.Fatalf("enricherLookupError(%q, %v) = %v, want nil", tc.name, tc.status, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("enricherLookupError(%q, %v) = nil, want an error", tc.name, tc.status)
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
		})
	}
}

// ---- config -> requested enrichers, and the compiled-in gate --------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestRequestedEnrichers
func TestRequestedEnrichers(t *testing.T) {
	tests := []struct {
		description string
		config      *xtcp_config.XtcpConfig
		want        []string
	}{
		// boundary
		{"a nil config requests nothing", nil, nil},
		{"the zero config requests nothing", &xtcp_config.XtcpConfig{}, nil},
		// positive
		{"asn only", &xtcp_config.XtcpConfig{EnrichAsnEnable: true}, []string{EnricherAsn}},
		{"locality only", &xtcp_config.XtcpConfig{EnrichLocalityEnable: true}, []string{EnricherLocality}},
		{"both, in knownEnrichers order",
			&xtcp_config.XtcpConfig{EnrichAsnEnable: true, EnrichLocalityEnable: true},
			[]string{EnricherAsn, EnricherLocality}},
		// corner — the always-on enrichers are not compile-time gated and must
		// never appear here, however many of them are switched on.
		{"the ungated enrichers are not reported",
			&xtcp_config.XtcpConfig{
				EnrichContainerEnable: true, EnrichLldpEnable: true,
				EnrichNicEnable: true, PopulateNsid: true,
			}, nil},
		// corner — an ASN path without the enable flag is still "not requested"
		{"an asn_db_path without the enable flag requests nothing",
			&xtcp_config.XtcpConfig{AsnDbPath: "/run/xtcp2-asn/asn.parquet"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := requestedEnrichers(tc.config)
			if len(got) != len(tc.want) {
				t.Fatalf("requestedEnrichers = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("requestedEnrichers = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestCheckConfigEnrichersCompiledIn
func TestCheckConfigEnrichersCompiledIn(t *testing.T) {
	tests := []struct {
		description string
		config      *xtcp_config.XtcpConfig
		// wantErrFor is the enricher expected to be reported when it is NOT
		// compiled into this build; "" means the config can never fail.
		wantErrFor string
	}{
		// positive / boundary — nothing requested can never fail, in any flavor
		{"a nil config always passes", nil, ""},
		{"the zero config always passes", &xtcp_config.XtcpConfig{}, ""},
		{"the ungated enrichers always pass",
			&xtcp_config.XtcpConfig{EnrichContainerEnable: true, EnrichNicEnable: true}, ""},
		// negative — these fail in a build without the matching tag
		{"asn requested", &xtcp_config.XtcpConfig{EnrichAsnEnable: true}, EnricherAsn},
		{"locality requested", &xtcp_config.XtcpConfig{EnrichLocalityEnable: true}, EnricherLocality},
		// corner — with both requested the FIRST missing one is reported, in
		// knownEnrichers order, so the message is deterministic.
		{"both requested reports asn first",
			&xtcp_config.XtcpConfig{EnrichAsnEnable: true, EnrichLocalityEnable: true}, EnricherAsn},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := checkConfigEnrichersCompiledIn(tc.config)

			// Which enricher (if any) this build should actually complain
			// about: the first requested one that is missing here.
			wantName := ""
			for _, n := range requestedEnrichers(tc.config) {
				if !EnricherCompiledIn(n) {
					wantName = n
					break
				}
			}
			if wantName == "" {
				if err != nil {
					t.Fatalf("checkConfigEnrichersCompiledIn = %v, want nil (compiled in: %v)", err, CompiledInEnrichers())
				}
				return
			}
			if err == nil {
				t.Fatalf("checkConfigEnrichersCompiledIn = nil, want an error naming %q (compiled in: %v)", wantName, CompiledInEnrichers())
			}
			if !strings.Contains(err.Error(), wantName) {
				t.Errorf("error %q does not name the missing enricher %q", err, wantName)
			}
			// Sanity: the table's expectation and the derived one must agree
			// about *which* enricher is at fault whenever it is missing here.
			if tc.wantErrFor != "" && !EnricherCompiledIn(tc.wantErrFor) && wantName != tc.wantErrFor {
				t.Errorf("reported %q, want %q", wantName, tc.wantErrFor)
			}
		})
	}
}

// ---- initEnrichers: fatal on a missing build tag -------------------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestInitEnrichersCompiledInGate
func TestInitEnrichersCompiledInGate(t *testing.T) {
	tests := []struct {
		description string
		config      *xtcp_config.XtcpConfig
		// wantFatal is derived per build; the table says which enricher the
		// config asks for so the derivation can run.
		requests []string
	}{
		{"no enrichment requested never goes fatal", &xtcp_config.XtcpConfig{}, nil},
		{"asn requested is fatal only without the tag",
			&xtcp_config.XtcpConfig{EnrichAsnEnable: true, AsnDbPath: "/nonexistent/asn.parquet"},
			[]string{EnricherAsn}},
		{"locality requested is fatal only without the tag",
			&xtcp_config.XtcpConfig{
				EnrichLocalityEnable:    true,
				LocalityRefreshInterval: durationpb.New(0),
			}, []string{EnricherLocality}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newMetricsFixture(t, 0)
			x.config = tc.config

			var fatal string
			x.fatalf = func(format string, args ...any) {
				fatal = fmt.Sprintf(format, args...)
			}
			x.initEnrichers(context.Background())

			wantFatal := false
			for _, n := range tc.requests {
				if !EnricherCompiledIn(n) {
					wantFatal = true
				}
			}
			if (fatal != "") != wantFatal {
				t.Fatalf("fatal = %q, wantFatal %v (compiled in: %v)", fatal, wantFatal, CompiledInEnrichers())
			}
			if wantFatal && !strings.Contains(fatal, "not compiled into this binary") {
				t.Errorf("fatal message %q does not explain that the enricher is missing from the build", fatal)
			}
		})
	}
}

// ---- the compiledInEnrichers gauge ---------------------------------------------

// go test -ldflags=-checklinkname=0 ./pkg/xtcp/ -run TestPublishCompiledInEnrichers
func TestPublishCompiledInEnrichers(t *testing.T) {
	x := newMetricsFixture(t, 0)
	x.publishCompiledInEnrichers()

	for _, name := range knownEnrichers {
		t.Run("a series exists for "+name, func(t *testing.T) {
			want := 0.0
			if EnricherCompiledIn(name) {
				want = 1.0
			}
			got := testutil.ToFloat64(x.pGV.WithLabelValues("InitPromethus", "compiledInEnrichers", name))
			if got != want {
				t.Errorf("compiledInEnrichers{%s} = %v, want %v", name, got, want)
			}
		})
	}
}
