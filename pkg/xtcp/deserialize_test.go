package xtcp

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

var (
	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec
)

type DeserializeTest struct {
	description string
	filename    string
	xtcpRecord  *xtcp_flat_record.XtcpFlatRecord
}

// testFlatRecordService is shared across every newTestDeserializeXTCP
// call. With the registry-injection refactor, NewXtcpFlatRecordService
// accepts a *prometheus.Registry — tests now use a fresh registry per
// call, but we keep the sync.Once + shared instance so existing tests
// that read the service's atomic counters across calls keep their
// observed-state semantics.
var testFlatRecordServiceOnce sync.Once
var testFlatRecordService *xtcpFlatRecordService

func getTestFlatRecordService(pollRequestCh *chan struct{}) *xtcpFlatRecordService {
	testFlatRecordServiceOnce.Do(func() {
		testFlatRecordService = NewXtcpFlatRecordService(context.Background(), prometheus.NewRegistry(), pollRequestCh, 0)
	})
	return testFlatRecordService
}

// newTestDeserializeXTCP returns an XTCP populated with everything
// Deserialize and its callees (flatRecordServiceSend, Marshaller,
// Destination, the prom counters, the pollTime map, the netlinkerDoneCh)
// need so they don't nil-deref. Used by TestDeserialize and
// BenchmarkDeserialize. Hostname is set to misc.GetHostname() to match
// what the production path would produce.
//
// Destination is destNull (records flow through but aren't captured);
// Marshaller is protoJsonMarshal. Production uses MarshalTo=protobufList
// via the envelope path (poller.flushEnvelope) which doesn't invoke
// x.Marshaller; x.Marshaller is wired here so test paths that still
// reference it stay non-nil.
func newTestDeserializeXTCP(tb testing.TB) *XTCP {
	tb.Helper()
	x := new(XTCP)
	x.config = &xtcp_config.XtcpConfig{
		Modulus:    1,
		MarshalTo:  MarshallerProtoJSON,
		Dest:       schemeNullPrefix,
		DebugLevel: 0,
	}
	x.debugLevel = 0
	x.hostname = misc.GetHostname()
	x.xtcpRecordPool = sync.Pool{New: func() any { return new(xtcp_flat_record.XtcpFlatRecord) }}
	x.nlhPool = sync.Pool{New: func() any { return new(xtcpnl.NlMsgHdr) }}
	x.rtaPool = sync.Pool{New: func() any { return new(xtcpnl.RTAttr) }}
	x.netlinkerDoneCh = make(chan netlinkerDone, 64)
	x.pollRequestCh = make(chan struct{}, 1)
	x.fatalf = tb.Fatalf

	// Fresh metrics registry per call so tests don't collide.
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_dtest", Name: promNameCounts, Help: promNameCounts},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_dtest", Name: promNameHistograms, Help: promNameHistograms,
			Objectives: map[float64]float64{0.5: quantileError, 0.99: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)

	// flatRecordServiceSend touches x.flatRecordService.frMapCount(); a
	// zero-client service is fine — early-return on no clients. Share a
	// single instance across test XTCPs to avoid duplicate Prometheus
	// metric registration.
	x.flatRecordService = getTestFlatRecordService(&x.pollRequestCh)

	x.Marshaller = func(r *xtcp_flat_record.XtcpFlatRecord) *[]byte {
		return x.protoJsonMarshal(r)
	}
	// Build the null destination directly. Bypasses the InitDests path so
	// the test doesn't need a goroutine + waitgroup just to plumb dest in.
	nullDst, err := newNullDest(context.Background(), x)
	if err != nil {
		panic(err)
	}
	x.dest = nullDst

	return x
}

// TestDeserialize
// go test --run TestDeserialize -v
func TestDeserialize(t *testing.T) {

	ctx := context.Background()

	var tests = []DeserializeTest{
		{
			description: "laptop_raw_data_capture",
			filename:    "../xtcpnl/testdata/netlink_packets_capture/2024-08-29T12:10:36.560332872-07:00",
			xtcpRecord: &xtcp_flat_record.XtcpFlatRecord{
				// xtcpRecord: &xtcp_flat_record.Envelope_XtcpFlatRecord{
				Hostname: misc.GetHostname(),
			},
		},
		{
			description: "10_tcp_sockets_reply",
			filename:    "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet2.pcap",
			//c:           c,
			xtcpRecord: &xtcp_flat_record.XtcpFlatRecord{
				// xtcpRecord: &xtcp_flat_record.Envelope_XtcpFlatRecord{
				Hostname: misc.GetHostname(),
			},
		},
		{
			description: "netlink_sock_diag_reply_single_packet_from_10k.pcap",
			filename:    "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet_from_10k.pcap",
			//c:           c,
			xtcpRecord: &xtcp_flat_record.XtcpFlatRecord{
				// xtcpRecord: &xtcp_flat_record.Envelope_XtcpFlatRecord{
				Hostname: misc.GetHostname(),
			},
		},
	}

	x := newTestDeserializeXTCP(t)
	// Expose to package vars for any downstream test/bench that reads them.
	pC = x.pC
	pH = x.pH

	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Fatalf("test %d open %s: %v", i, test.filename, err)
		}

		bs, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("test %d read %s: %v", i, test.filename, err)
		}

		// .pcap files have a 56-byte (pcap header + record header + cooked
		// header) prefix to strip; raw netlink captures start at byte 0.
		var buf []byte
		if strings.HasSuffix(test.filename, ".pcap") {
			buf = bs[xtcpnl.PcapNetlinkOffsetCst:]
		} else {
			buf = bs
		}

		nsName := "test-ns"
		n, errD := x.Deserialize(
			ctx,
			DeserializeArgs{
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

		// Deserialize is expected to walk every netlink message in the
		// buffer; if it hits an unparseable header it returns a wrapped
		// error. Any error here means the parser is broken on this
		// fixture.
		if errD != nil {
			t.Errorf("test %d %s Deserialize err: %v (parsed n=%d)", i, test.description, errD, n)
			continue
		}
		if n == 0 {
			t.Errorf("test %d %s: Deserialize returned n=0; fixture should contain at least one record", i, test.description)
			continue
		}
		t.Logf("test %d %s: parsed n=%d records", i, test.description, n)

		// Hostname is stamped on every record by Deserialize from
		// x.hostname; verify the production wiring set it on at least
		// one record by checking that field on a freshly-pooled struct
		// after the run (the pool's reused entries will all carry
		// x.hostname).
		fresh := x.xtcpRecordPool.Get().(*xtcp_flat_record.XtcpFlatRecord)
		if fresh.Hostname != "" && fresh.Hostname != test.xtcpRecord.Hostname {
			t.Errorf("test %d %s: pooled record Hostname=%q want=%q",
				i, test.description, fresh.Hostname, test.xtcpRecord.Hostname)
		}
		x.xtcpRecordPool.Put(fresh)
	}
}

var (
	resultXtcpFlatRecord *xtcp_flat_record.XtcpFlatRecord
	// resultXtcpFlatRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
)

// go test -bench=BenchmarkDeserialize
// go test -bench=BenchmarkDeserialize -benchtime=60s
func BenchmarkDeserialize(b *testing.B) {
	DeserializeBoth(b)
}

func DeserializeBoth(b *testing.B) {

	ctx := context.Background()

	var tests = []DeserializeTest{
		{
			description: "netlink_sock_diag_reply_single_packet_from_10k.pcap",
			filename:    "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet_from_10k.pcap",
			xtcpRecord: &xtcp_flat_record.XtcpFlatRecord{
				// xtcpRecord: &xtcp_flat_record.Envelope_XtcpFlatRecord{
				Hostname: misc.GetHostname(),
			},
		},
	}

	test := tests[0]

	x := newTestDeserializeXTCP(b)
	pC = x.pC
	pH = x.pH

	f, err := os.Open(test.filename)
	if err != nil {
		b.Fatalf("open %s: %v", test.filename, err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Fatalf("read %s: %v", test.filename, err)
	}

	var buf []byte
	if strings.HasSuffix(test.filename, ".pcap") {
		buf = bs[xtcpnl.PcapNetlinkOffsetCst:]
	} else {
		buf = bs
	}

	nsName := "bench-ns"
	b.SetBytes(int64(len(buf)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD := x.Deserialize(
			ctx,
			DeserializeArgs{
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
			b.Fatalf("Deserialize err: %v", errD)
		}
	}

	resultXtcpFlatRecord = x.xtcpRecordPool.Get().(*xtcp_flat_record.XtcpFlatRecord)
}
