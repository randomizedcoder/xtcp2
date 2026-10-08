package linkmonitor

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func trafficRecord(key model.DeviceKey) *model.LinkStatistics {
	r := &model.LinkStatistics{Key: key, Width: 64, Fields: 25, Observed: presentValue(model.Stamp{})}
	for i := range r.Values {
		r.Values[i] = uint64(i + 1)
	}
	for i := range r.Carrier {
		r.Carrier[i] = presentValue(uint64(0))
	}
	r.Carrier[0] = presentValue(uint64(1))
	return r
}

func TestTrafficProjection(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		fields                                uint8
		wide                                  bool
	}{
		{"all", "positive", "all direct 64-bit fields", "25 independent counters", 25, true},
		{"legacy", "boundary", "complete 32-bit structure", "24 widened counters, no otherhost field", 24, false},
		{"short", "boundary", "one complete trailing-independent field", "one counter only", 1, true},
		{"absent", "negative", "MIB fallback has no direct field presence", "no traffic metrics", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			link := xtcpnl.LinkInfo{Index: 1, StatsFields: tc.fields, StatsIs64: tc.wide, Stats: &xtcpnl.RtnlLinkStats64{
				RxPackets: 1, TxPackets: 2, RxBytes: 3, TxBytes: 4, RxErrors: 5, TxErrors: 6, RxDropped: 7, TxDropped: 8,
				Multicast: 9, Collisions: 10, RxLengthErrors: 11, RxOverErrors: 12, RxCrcErrors: 13, RxFrameErrors: 14,
				RxFifoErrors: 15, RxMissedErrors: 16, TxAbortedErrors: 17, TxCarrierErrors: 18, TxFifoErrors: 19,
				TxHeartbeatErrors: 20, TxWindowErrors: 21, RxCompressed: 22, TxCompressed: 23, RxNohandler: 24, RxOtherhostDropped: 25}}
			record := decodeTraffic(1, &link)
			if err := validTraffic(&record); err != nil {
				t.Fatal(err)
			}
			samples := trafficSamples(&record, 7)
			if len(samples) != int(tc.fields) {
				t.Fatal("invented missing fields")
			}
			for i := range samples {
				value, _ := samples[i].Number.Uint64()
				if value != uint64(i+1) || samples[i].Counter.Lifetime != 7 {
					t.Fatal("field order or counter identity changed")
				}
			}
			if len(samples) == 25 && samples[24].Descriptor != "go_link_monitor_interface_receive_otherhost_dropped_total" {
				t.Fatal("otherhost descriptor missing")
			}
		})
	}
}

func TestTrafficValuesAndValidation(t *testing.T) {
	key := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: 1}
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func(*model.LinkStatistics)
		bad                                   bool
	}{
		{"zero", "boundary", "present zero", "preserve zero", func(r *model.LinkStatistics) { r.Values[0] = 0 }, false},
		{"maximum", "boundary", "maximum uint64", "preserve exact unsigned integer", func(r *model.LinkStatistics) { r.Values[0] = math.MaxUint64 }, false},
		{"width", "negative", "32-bit overflow", "reject", func(r *model.LinkStatistics) { r.Width = 32; r.Fields = 24; r.Values[0] = math.MaxUint32 + 1 }, true},
		{"tail", "negative", "25 fields in legacy structure", "reject", func(r *model.LinkStatistics) { r.Width = 32 }, true},
		{"index", "negative", "invalid index", "reject", func(r *model.LinkStatistics) { r.Key.Index = 0 }, true},
		{"carrier", "negative", "carrier is two", "reject", func(r *model.LinkStatistics) { r.Carrier[0] = presentValue(uint64(2)) }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r := trafficRecord(key)
			tc.mutate(r)
			if err := validTraffic(r); (err != nil) != tc.bad {
				t.Fatalf("validation: %v", err)
			}
			if !tc.bad {
				got, _ := trafficSamples(r, 1)[0].Number.Uint64()
				if got != r.Values[0] {
					t.Fatal("value rounded")
				}
			}
		})
	}
}

type testLinkRequests struct {
	dumps, gets int
	links       []xtcpnl.LinkInfo
	err         error
}

func (s *testLinkRequests) DumpLinks(context.Context) (linuxio.Result[xtcpnl.LinkInfo], error) {
	s.dumps++
	return linuxio.Result[xtcpnl.LinkInfo]{Values: s.links}, s.err
}
func (s *testLinkRequests) GetLink(context.Context, int32) (linuxio.Result[xtcpnl.LinkInfo], error) {
	s.gets++
	return linuxio.Result[xtcpnl.LinkInfo]{Values: s.links}, s.err
}
func (s *testLinkRequests) Close() error { return nil }

func TestTrafficRequestsRejectPartial(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		links                                 []xtcpnl.LinkInfo
		err                                   error
		index                                 uint32
		bad                                   bool
	}{
		{"dump", "positive", "many devices in one dump", "one request", []xtcpnl.LinkInfo{{Index: 1}, {Index: 2}}, nil, 0, false},
		{"target", "positive", "targeted request supplies both sources", "one GET", []xtcpnl.LinkInfo{{Index: 1}}, nil, 1, false},
		{"failed suffix", "negative", "data followed by dump error", "no healthy prefix", []xtcpnl.LinkInfo{{Index: 1}}, errors.New("interrupted"), 0, true},
		{"duplicate", "negative", "duplicate device identity", "reject whole result", []xtcpnl.LinkInfo{{Index: 1}, {Index: 1}}, nil, 0, true},
		{"wrong target", "negative", "GET returns another index", "reject", []xtcpnl.LinkInfo{{Index: 2}}, nil, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			source := &testLinkRequests{links: tc.links, err: tc.err}
			collector := trafficCollector{client: source, namespace: 1}
			result := collector.readTraffic(t.Context(), trafficRequest{index: tc.index})
			if (result.err != nil) != tc.bad || source.dumps+source.gets != 1 {
				t.Fatalf("result=%v dump=%d get=%d", result.err, source.dumps, source.gets)
			}
			if tc.bad && len(result.records) != 0 {
				t.Fatal("partial records escaped")
			}
		})
	}
}
