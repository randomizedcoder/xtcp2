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

func TestRDMAHWCounterCatchAll(t *testing.T) {
	type file struct{ path, text string }
	type want struct {
		descriptor, counter string
		value               uint64
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		files                                        []file
		want                                         []want
	}{
		{"vendor", "positive", "uncataloged ionic error counter", "one untyped catch-all sample with its file as the counter label",
			[]file{{"hw_counters/req_rx_cqe_err", "3"}}, []want{{rdmaHWCounterDescriptor, "req_rx_cqe_err", 3}}},
		{"beside-catalog", "positive", "cataloged and uncataloged hw_counters side by side", "cataloged file keeps its reviewed metric; the other joins the catch-all",
			[]file{{"hw_counters/lifespan", "2000"}, {"hw_counters/rdma_req_rx_cqe_err", "4"}}, []want{{"go_link_monitor_infiniband_" + rdmaCounterCatalog["hw_counters/lifespan"].metric, "", 2}, {rdmaHWCounterDescriptor, "rdma_req_rx_cqe_err", 4}}},
		{"two", "positive", "two uncataloged files", "two series told apart by the counter label, in name order",
			[]file{{"hw_counters/b_err", "2"}, {"hw_counters/a_err", "1"}}, []want{{rdmaHWCounterDescriptor, "a_err", 1}, {rdmaHWCounterDescriptor, "b_err", 2}}},
		{"zero", "boundary", "uncataloged file reading zero", "present zero, not skipped",
			[]file{{"hw_counters/active_qps", "0"}}, []want{{rdmaHWCounterDescriptor, "active_qps", 0}}},
		{"maximum", "boundary", "largest uint64", "exact value, no scaling",
			[]file{{"hw_counters/tx_bytes", "18446744073709551615"}}, []want{{rdmaHWCounterDescriptor, "tx_bytes", math.MaxUint64}}},
		{"overflow", "boundary", "one past uint64", "file skipped; the set still succeeds",
			[]file{{"hw_counters/tx_bytes", "18446744073709551616"}}, nil},
		{"standard-group", "negative", "unknown name in the standard counters group", "still dropped; only hw_counters is vendor-defined",
			[]file{{"counters/vendor_extra", "7"}}, nil},
		{"text", "negative", "uncataloged file that is not a number", "skipped; the reviewed counter beside it still publishes",
			[]file{{"hw_counters/fw_state", "N/A"}, {"counters/link_downed", "1"}}, []want{{"go_link_monitor_infiniband_" + rdmaCounterCatalog["counters/link_downed"].metric, "", 1}}},
		{"signed", "negative", "uncataloged file with signed text", "skipped rather than wrapped",
			[]file{{"hw_counters/delta", "-1"}}, nil},
		{"directory", "corner", "uncataloged entry that is a directory", "skipped, not a malformed set",
			[]file{{"hw_counters/nested/x", "1"}, {"hw_counters/req_rx_cqe_err", "5"}}, []want{{rdmaHWCounterDescriptor, "req_rx_cqe_err", 5}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, job, path := optionalRDMAFixture(t)
			for _, f := range tc.files {
				rdmaWrite(t, filepath.Join(path, f.path), f.text)
			}
			r := c.Collect(t.Context(), job)
			if r.Err != nil {
				t.Fatal(tc.expectedOutcome, r.Err)
			}
			if len(r.Samples) != len(tc.want) {
				t.Fatalf("%s: %d samples %+v", tc.expectedOutcome, len(r.Samples), r.Samples)
			}
			for i, w := range tc.want {
				s := r.Samples[i]
				counter := ""
				for _, l := range s.Labels {
					if l.Name == "counter" {
						counter = l.Value
					}
				}
				n, ok := s.Number.Uint64()
				if s.Descriptor != w.descriptor || counter != w.counter || !ok || n != w.value || !s.NoInterfaceLabel {
					t.Fatalf("%s: sample %d %+v", tc.expectedOutcome, i, s)
				}
				if s.Descriptor == rdmaHWCounterDescriptor && (s.Kind != model.SampleUntyped || s.Counter != (model.CounterIdentity{})) {
					t.Fatalf("%s: catch-all must be untyped with no counter identity: %+v", tc.expectedOutcome, s)
				}
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
			first, _, err := rdmaCounterSample(job, p, "counters/link_downed", "10", nil)
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&p, &job)
			second, _, err := rdmaCounterSample(job, p, "counters/link_downed", "10", nil)
			if err != nil {
				t.Fatal(err)
			}
			if (first.Counter != second.Counter) != tc.different {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
