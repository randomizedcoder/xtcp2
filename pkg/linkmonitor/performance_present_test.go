package linkmonitor_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

// Partial availability is a measured capacity/freshness outcome, never license
// to accept incorrect values. Validate every published synthetic source sample.
func performancePresentSamples(t *testing.T, snapshot linkmonitor.Snapshot, fields int) {
	t.Helper()
	snapshot.RangeSamples(func(s linkmonitor.SampleView) bool {
		if s.DescriptorKey() != "ethtool_statistic" && s.DescriptorKey() != "phy_statistic" {
			t.Error("unexpected source descriptor", s.DescriptorKey())
			return false
		}
		statistic, encoding, device := "", "", ""
		s.RangeLabels(func(name, value string) bool {
			switch name {
			case "statistic":
				statistic = value
			case "encoding":
				encoding = value
			case "interface":
				device = value
			default:
				t.Error("unexpected source label", name)
			}
			return true
		})
		index, err := strconv.Atoi(strings.TrimPrefix(statistic, "queue_"))
		value, known := s.Number().Uint64()
		if err != nil || index < 0 || index >= fields || !known || value != uint64(index) || s.Kind() != linkmonitor.SampleUntyped || encoding != "utf8" || device == "" {
			t.Error("incorrect source metric", statistic, value)
		}
		return true
	})
}
