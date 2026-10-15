package xtcp

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// seriesValue finds the {function, variable} series in c: a counter's
// value or a summary's sample count. ok is false when there's no series.
func seriesValue(tb testing.TB, c prometheus.Collector, function, variable string) (v float64, ok bool) {
	tb.Helper()
	ch := make(chan prometheus.Metric, 256)
	go func() {
		c.Collect(ch)
		close(ch)
	}()
	for m := range ch {
		var pb dto.Metric
		if err := m.Write(&pb); err != nil {
			tb.Fatal(err)
		}
		labels := map[string]string{}
		for _, l := range pb.GetLabel() {
			labels[l.GetName()] = l.GetValue()
		}
		if labels[promLabelFunction] != function || labels[promLabelVariable] != variable {
			continue
		}
		ok = true
		switch {
		case pb.GetCounter() != nil:
			v = pb.GetCounter().GetValue()
		case pb.GetSummary() != nil:
			v = float64(pb.GetSummary().GetSampleCount())
		}
	}
	return v, ok
}

// inetDiagDump is records bodiless inet_diag messages and an NLMSG_DONE.
func inetDiagDump(records int) []byte {
	var b []byte
	for range records {
		b = append(b, mkNlMsg(xtcpnl.NlMsgHdrTypeInetDiagCst,
			xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst,
			xtcpnl.NlMsgHdrSizeCst+xtcpnl.InetDiagMsgSizeCst)...)
	}
	return append(b, mkNlMsg(xtcpnl.NlMsgHdrTypeDoneCst, xtcpnl.NlMsgHdrSizeCst, xtcpnl.NlMsgHdrSizeCst)...)
}

// TestDeserializeMetrics decodes one packet of 300 records with
// debug_prometheus off and on. Off, the per-record series (Deserialize n,
// nlPacketComplete) and flatRecordServiceSend's (no gRPC clients) are
// never created; the per-packet and per-record counts consumers read
// (start, complete, envelopeAppend) are the same either way.
func TestDeserializeMetrics(t *testing.T) {
	const records = 300
	type series struct {
		function, variable string
	}
	tests := []struct {
		description string
		debug       bool
		expected    map[series]float64 // -1: no such series
	}{
		{
			description: "negative: debug off, the per-record and no-client series aren't created",
			expected: map[series]float64{
				{"Deserialize", "n"}:                  -1,
				{"Deserialize", "nlPacketComplete"}:   -1,
				{"flatRecordServiceSend", "start"}:    -1,
				{"flatRecordServiceSend", "complete"}: -1,
			},
		},
		{
			description: "positive: debug off, the series consumers read still count",
			expected: map[series]float64{
				{"Deserialize", "start"}:          1,
				{"Deserialize", "complete"}:       1,
				{"Deserialize", "envelopeAppend"}: records,
			},
		},
		{
			description: "positive: debug on, the per-record series count every message and record",
			debug:       true,
			expected: map[series]float64{
				{"Deserialize", "n"}:                records + 1, // and the NLMSG_DONE
				{"Deserialize", "nlPacketComplete"}: records,
				{"Deserialize", "start"}:            1,
				{"Deserialize", "complete"}:         1,
				{"Deserialize", "envelopeAppend"}:   records,
			},
		},
		{
			description: "corner: debug on, no gRPC clients still means no flatRecordServiceSend series",
			debug:       true,
			expected: map[series]float64{
				{"flatRecordServiceSend", "start"}:    -1,
				{"flatRecordServiceSend", "complete"}: -1,
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newTestDeserializeXTCP(t)
			x.config.DebugPrometheus = tc.debug
			x.currentEnvelope = &xtcp_flat_record.Envelope{}
			buf := inetDiagDump(records)
			ns := "test-ns"
			n, err := x.Deserialize(context.Background(), DeserializeArgs{
				ns:             &ns,
				NLPacket:       &buf,
				xtcpRecordPool: &x.xtcpRecordPool,
				nlhPool:        &x.nlhPool,
				rtaPool:        &x.rtaPool,
				pC:             x.pC,
				pH:             x.pH,
			})
			if err != nil || n != records {
				t.Fatalf("Deserialize = %d, %v; expected %d, nil", n, err, records)
			}
			for s, want := range tc.expected {
				var c prometheus.Collector = x.pC
				if s.variable == "complete" || s.variable == "nlPacketComplete" {
					c = x.pH
				}
				got, ok := seriesValue(t, c, s.function, s.variable)
				switch {
				case want < 0 && ok:
					t.Errorf("%s/%s = %v, expected no series", s.function, s.variable, got)
				case want >= 0 && (!ok || got != want):
					t.Errorf("%s/%s = %v (exists %v), expected %v", s.function, s.variable, got, ok, want)
				}
			}
		})
	}
}
