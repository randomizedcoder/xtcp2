package linkmonitor

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestRDMACounterUnitsTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path, text                                   string
		want                                         uint64
		kind                                         model.SampleKind
		err                                          error
	}{
		{"zero", "boundary", "zero flap register", "present zero counter", "counters/link_downed", "0", 0, model.SampleCounter, nil},
		{"exact", "boundary", "integer above float exact range", "exact uint64 retained", "counters/link_downed", "9007199254740993", 9007199254740993, model.SampleCounter, nil},
		{"maximum", "boundary", "largest raw counter", "exact uint64 retained", "counters/port_rcv_packets", "18446744073709551615", math.MaxUint64, model.SampleCounter, nil},
		{"octets", "positive", "four-octet unit register", "multiply by four, not lane width", "counters/port_rcv_data", "7", 28, model.SampleCounter, nil},
		{"legacy", "corner", "legacy four-octet register", "separate legacy metric", "counters_ext/port_rcv_data_64", "7", 28, model.SampleCounter, nil},
		{"overflow", "negative", "byte conversion overflow", "no wrapped counter", "counters/port_rcv_data", "4611686018427387904", 0, 0, linuxio.ErrLimit},
		{"lifespan", "positive", "cache age in milliseconds", "integer seconds compatibility gauge", "hw_counters/lifespan", "1999", 1, model.SampleGauge, nil},
		{"ticks", "corner", "transmit wait ticks", "no conversion to seconds", "counters/port_xmit_wait", "12", 12, model.SampleCounter, nil},
		{"negative", "negative", "signed text for cumulative register", "malformed", "counters/link_downed", "-1", 0, 0, linuxio.ErrReply},
		{"missing", "negative", "N/A rather than a counter", "no fabricated zero", "counters/link_downed", "N/A (no PMA)", 0, 0, linuxio.ErrReply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, job, path := optionalRDMAFixture(t)
			rdmaWrite(t, filepath.Join(path, tc.path), tc.text)
			r := c.Collect(t.Context(), job)
			if !errors.Is(r.Err, tc.err) {
				t.Fatal(tc.expectedOutcome, r.Err)
			}
			if tc.err != nil {
				if len(r.Samples) != 0 {
					t.Fatal("failed set published")
				}
				return
			}
			if len(r.Samples) != 1 {
				t.Fatal("sample count", len(r.Samples))
			}
			sample := r.Samples[0]
			if n, ok := sample.Number.Uint64(); !ok || n != tc.want || sample.Kind != tc.kind {
				t.Fatalf("%s: %+v", tc.expectedOutcome, sample)
			}
		})
	}
}

func TestRDMACounterSchemaLifetime(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       string
		count                                        int
		bad                                          bool
	}{
		{"cached", "positive", "new file without discovery invalidation", "cached paths only", "add", 1, false},
		{"resync", "positive", "new file after resync", "new field discovered", "resync", 2, false},
		{"removed", "negative", "cached counter disappears", "whole set rejected and cache invalidated", "remove", 0, true},
		{"malformed", "negative", "counter text becomes malformed", "whole set rejected", "malformed", 0, true},
		{"overlap", "corner", "legacy and modern registers coexist", "distinct names and source identities", "overlap", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, job, path := optionalRDMAFixture(t)
			file := filepath.Join(path, "counters", "port_rcv_data")
			rdmaWrite(t, file, "5")
			first := c.Collect(t.Context(), job)
			if first.Err != nil {
				t.Fatal(first.Err)
			}
			job.StatisticSchema = first.StatisticSchema
			switch tc.change {
			case "add", "resync":
				rdmaWrite(t, filepath.Join(path, "counters", "link_downed"), "1")
			case "remove":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				rdmaWrite(t, file, "invalid")
			case "overlap":
				rdmaWrite(t, filepath.Join(path, "counters_ext", "port_rcv_data_64"), "5")
			}
			if tc.change == "resync" || tc.change == "overlap" {
				job.StatisticSchema = nil
			}
			second := c.Collect(t.Context(), job)
			if (second.Err != nil) != tc.bad || len(second.Samples) != tc.count {
				t.Fatal(tc.expectedOutcome, second.Err, len(second.Samples))
			}
			if tc.bad && !second.InvalidateSchema {
				t.Fatal("failure retained schema")
			}
			if first.Samples[0].Number != model.Unsigned(20) {
				t.Fatal("old result mutated")
			}
			if tc.change == "overlap" && (second.Samples[0].Descriptor == second.Samples[1].Descriptor || second.Samples[0].Counter == second.Samples[1].Counter) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestRDMACounterCatalog(t *testing.T) {
	t.Log("positive: every documented fixed RDMA counter; expected unique descriptor, known source and exact conversion")
	seen := make(map[string]bool)
	for path, field := range rdmaCounterCatalog {
		if seen[field.metric] {
			t.Fatal("duplicate descriptor", field.metric)
		}
		seen[field.metric] = true
		if !strings.Contains(path, "/") || field.scale != 0 && field.scale != 1 && field.scale != 4 {
			t.Fatal(path, field)
		}
	}
	doc, err := os.ReadFile("../../cmd/go-link-monitor/METRICS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(doc), "\n") {
		if !strings.HasPrefix(line, "| `node_infiniband_") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(line, "| `node_infiniband_"), "`")
		switch name {
		case "info", "rate_bytes_per_second", "state_id", "physical_state_id":
			continue
		}
		if !seen[name] {
			t.Fatal("documented metric missing source", name)
		}
		delete(seen, name)
	}
	if len(seen) != 0 {
		t.Fatal("undocumented metrics", seen)
	}
}

func TestRDMARateTable(t *testing.T) {
	for _, tc := range []struct {
		category, description, expectedOutcome, text string
		want                                         uint64
		bad                                          bool
	}{
		{"positive", "integer NDR rate", "bytes per second", "400 Gb/sec (4X NDR)", 50000000000, false},
		{"boundary", "fractional SDR rate", "exact decimal conversion", "2.5 Gb/sec (1X SDR)", 312500000, false},
		{"corner", "down link rate", "present zero", "0 Gb/sec", 0, false},
		{"negative", "wrong units", "reject", "100 Mb/sec", 0, true},
		{"negative", "overflow", "reject", "18446744073709551615 Gb/sec", 0, true},
		{"negative", "malformed decimal", "reject", "2.55 Gb/sec", 0, true},
	} {
		t.Run(tc.text, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			n, err := rdmaRate(tc.text)
			if (err != nil) != tc.bad || n != tc.want {
				t.Fatal(tc.expectedOutcome, n, err)
			}
		})
	}
}

func BenchmarkRDMACounterRead(b *testing.B) {
	c, job, path := optionalRDMAFixture(b)
	for name := range rdmaCounterCatalog {
		rdmaWrite(b, filepath.Join(path, name), strconv.Itoa(42))
	}
	first := c.Collect(b.Context(), job)
	if first.Err != nil {
		b.Fatal(first.Err)
	}
	job.StatisticSchema = first.StatisticSchema
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if r := c.Collect(b.Context(), job); r.Err != nil {
			b.Fatal(r.Err)
		}
	}
}

func TestRDMACounterSourceIdentity(t *testing.T) {
	for _, tc := range []struct {
		category, description, expectedOutcome string
		change                                 func(*model.RDMAPort, *model.Job)
		different                              bool
	}{
		{"positive", "ordinary repeat read", "stable source identity", func(*model.RDMAPort, *model.Job) {}, false},
		{"corner", "HCA recreated under same name and PCI path", "kernel index changes counter identity", func(p *model.RDMAPort, _ *model.Job) { p.Index++ }, true},
		{"corner", "fallback discovery replaces netlink evidence", "source domain stays distinct from real kernel index zero", func(p *model.RDMAPort, _ *model.Job) { p.Sysfs = true }, true},
		{"negative", "hardware replaced", "source identity changes", func(p *model.RDMAPort, _ *model.Job) { p.Hardware = "replacement" }, true},
		{"boundary", "canonical generation changes", "source lifetime changes", func(_ *model.RDMAPort, j *model.Job) { j.Token.Generation++ }, true},
	} {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p := model.RDMAPort{Device: "hca", Hardware: "pci", Port: 1}
			job := model.Job{Token: model.Token{Generation: 1}}
			first, err := rdmaCounterSample(job, p, "counters/link_downed", "10", nil)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&p, &job)
			second, err := rdmaCounterSample(job, p, "counters/link_downed", "10", nil)
			if err != nil {
				t.Fatal(err)
			}
			if (first.Counter != second.Counter) != tc.different {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
