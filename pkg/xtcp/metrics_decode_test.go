package xtcp

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewDecodeMetrics(t *testing.T) {
	tests := []struct {
		description string
		debug       bool
		expectedOn  bool
	}{
		{description: "positive: debug off picks the no-op per-record metrics", debug: false, expectedOn: false},
		{description: "positive: debug on picks the recording per-record metrics", debug: true, expectedOn: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newTestDeserializeXTCP(t)
			m := newDecodeMetrics(x.pC, x.pH, tc.debug)
			_, on := m.record.(recordMetricsOn)
			if on != tc.expectedOn {
				t.Errorf("record is %T, expected on=%v", m.record, tc.expectedOn)
			}
		})
	}
}

func TestRecordMetricsOffNow(t *testing.T) {
	tests := []struct {
		description string
		record      recordMetrics
		expected    bool // now is the zero time
	}{
		{description: "positive: off never reads the clock", record: recordMetricsOff{}, expected: true},
		{description: "negative: on reads the clock", record: recordMetricsOn{}, expected: false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.record.now().Equal(time.Time{}); got != tc.expected {
				t.Errorf("now() is zero = %v, expected %v", got, tc.expected)
			}
		})
	}
}

// TestDecodeMetricsHandles: a cached handle is the same series a
// WithLabelValues lookup finds, so dashboards and the self-test see it.
func TestDecodeMetricsHandles(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	m := newDecodeMetrics(x.pC, x.pH, false)
	tests := []struct {
		description string
		inc         func()
		variable    string
		labelType   string
		expected    float64
	}{
		{description: "positive: envelopeAppend", inc: m.envelopeAppend.Inc, variable: "envelopeAppend", labelType: "count", expected: 1},
		{description: "positive: start", inc: m.start.Inc, variable: "start", labelType: "count", expected: 1},
		{description: "corner: envelopePostFlushDrop is an error series", inc: m.envelopePostFlushDrop.Inc, variable: "envelopePostFlushDrop", labelType: "error", expected: 1},
		{description: "corner: continue", inc: m.cont.Inc, variable: "continue", labelType: "count", expected: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tc.inc()
			if got := testutil.ToFloat64(x.pC.WithLabelValues("Deserialize", tc.variable, tc.labelType)); got != tc.expected {
				t.Errorf("Deserialize/%s = %v, expected %v", tc.variable, got, tc.expected)
			}
		})
	}
}
