package linkmonitor

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func statisticNames(names ...string) []byte {
	data := make([]byte, len(names)*statisticNameBytes)
	for i, name := range names {
		copy(data[i*statisticNameBytes:(i+1)*statisticNameBytes], name)
	}
	return data
}

func TestStatisticSchema(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		names                                        []string
		include, exclude                             string
		selected                                     int
		invalid                                      bool
	}{
		{"default", "positive", "ordinary queue and driver names", "all selected verbatim", []string{"rx_queue_0", " TX bytes "}, ".*", "^$", 2, false},
		{"include", "positive", "select queue prefix", "one selected index", []string{"rx_queue_0", "tx"}, "^rx", "^$", 1, false},
		{"exclude wins", "positive", "include and exclude match", "excluded name omitted", []string{"rx_queue_0", "tx"}, ".*", "rx", 1, false},
		{"none", "boundary", "no matching names", "valid empty schema selection", []string{"rx"}, "^tx$", "^$", 0, false},
		{"empty expressions", "corner", "both regexps match everywhere", "exclude removes everything", []string{"rx"}, "", "", 0, false},
		{"empty name", "boundary", "single empty source name", "default excludes empty name", []string{""}, ".*", "^$", 0, false},
		{"empty allowed", "corner", "explicit filter includes empty name", "empty label preserved", []string{""}, ".*", "a^", 1, false},
		{"terminated", "boundary", "31 bytes followed by NUL", "31 bytes preserved", []string{strings.Repeat("x", 31)}, ".*", "^$", 1, false},
		{"full width", "boundary", "32 nonterminated bytes", "32 bytes preserved", []string{strings.Repeat("x", 32)}, ".*", "^$", 1, false},
		{"duplicate", "negative", "identical source names", "whole set rejected", []string{"rx", "rx"}, ".*", "^$", 0, true},
		{"hidden duplicate", "negative", "duplicate names excluded by filter", "whole set still rejected", []string{"rx", "rx"}, "tx", ".*", 0, true},
		{"padding", "corner", "different padding after terminating NUL", "decoded duplicate rejected", []string{"rx\x00a", "rx\x00b"}, ".*", "^$", 0, true},
		{"encoding", "corner", "invalid UTF-8 and its textual hex", "distinct encoding labels", []string{"\xff", "ff"}, ".*", "^$", 2, false},
		{"invalid filter", "corner", "regexp matches original invalid byte as replacement rune", "filter before reversible hex encoding", []string{"\xff"}, "^\ufffd$", "^$", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			data := statisticNames(tc.names...)
			schema, err := decodeStatisticSchema(data, len(tc.names), regexp.MustCompile(tc.include), regexp.MustCompile(tc.exclude))
			if (err != nil) != tc.invalid {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err != nil {
				return
			}
			if len(schema.Selected) != tc.selected {
				t.Fatal(tc.expectedOutcome)
			}
			clear(data)
			for i, index := range schema.Selected {
				original := strings.Split(tc.names[index], "\x00")[0]
				if schema.Names[index] != original {
					t.Fatal("source bytes changed")
				}
				if original == "\xff" {
					if schema.Labels[i][0].Value != "ff" || schema.Labels[i][1].Value != "hex" {
						t.Fatal("encoding lost")
					}
				} else if schema.Labels[i][0].Value != original || schema.Labels[i][1].Value != "utf8" {
					t.Fatal("valid name changed")
				}
			}
		})
	}
}

func generatedStatisticNames(count int) []byte {
	data := make([]byte, count*statisticNameBytes)
	for i := range count {
		copy(data[i*statisticNameBytes:], fmt.Sprintf("queue_%d", i))
	}
	return data
}

func TestStatisticSchemaBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count, bytes                                 int
		invalid                                      bool
	}{
		{"zero", "boundary", "empty source", "empty valid schema", 0, 0, false},
		{"one", "positive", "single name", "accepted", 1, 32, false},
		{"limit", "boundary", "65536 names", "all accepted", maximumSamples, maximumSamples * 32, false},
		{"oversize", "negative", "65537 names", "rejected", maximumSamples + 1, 0, true},
		{"short", "negative", "short name slot", "rejected", 1, 31, true},
		{"extra", "negative", "trailing partial slot", "rejected", 1, 33, true},
		{"negative", "corner", "negative count from parser caller", "rejected before multiplication", -1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			data := generatedStatisticNames(tc.bytes / 32)
			data = append(data, make([]byte, tc.bytes%32)...)
			schema, err := decodeStatisticSchema(data, tc.count, regexp.MustCompile(".*"), regexp.MustCompile("^$"))
			if (err != nil) != tc.invalid {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err == nil && len(schema.Selected) != tc.count {
				t.Fatal("truncated schema")
			}
		})
	}
}

func FuzzStatisticSchema(f *testing.F) {
	f.Add(statisticNames("rx", "tx"), uint32(2))
	f.Add(statisticNames("\xff"), uint32(1))
	include, exclude := regexp.MustCompile(".*"), regexp.MustCompile("^$")
	f.Fuzz(func(t *testing.T, data []byte, count uint32) {
		if count > maximumSamples {
			count = maximumSamples + 1
		}
		schema, err := decodeStatisticSchema(data, int(count), include, exclude)
		if err == nil && (len(schema.Names) != int(count) || len(schema.Selected) != len(schema.Labels)) {
			t.Fatal("invalid successful schema")
		}
	})
}

func BenchmarkStatisticSchema(b *testing.B) {
	include, exclude := regexp.MustCompile(".*"), regexp.MustCompile("^$")
	for _, count := range []int{0, 64, 1024, 8192, maximumSamples} {
		data := generatedStatisticNames(count)
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := decodeStatisticSchema(data, count, include, exclude); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestStatisticUntypedPublication(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		value                                        uint64
	}{
		{"zero", "boundary", "zero raw register", "present untyped zero", 0},
		{"precision", "boundary", "value exceeds float64 precision", "exact unsigned storage", 1<<53 + 1},
		{"maximum", "boundary", "maximum uint64", "exact unsigned storage", ^uint64(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := newReducer(1)
			mustObserve(t, r, observed(r, 1, true))
			sample := model.Sample{Descriptor: "ethtool_statistic", Kind: model.SampleUntyped, Number: model.Unsigned(tc.value)}
			state := collect(t, r, model.CollectorDriver, sample)
			old := state.block
			sample.Number = model.Unsigned(0)
			state = collect(t, r, model.CollectorDriver, sample)
			value, ok := old.values[0].Uint64()
			if !ok || value != tc.value || old.schema != state.block.schema || state.discontinuities != 0 || len(state.history) != 0 || SampleKind(old.schema.entries[0].kind) != SampleUntyped {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
