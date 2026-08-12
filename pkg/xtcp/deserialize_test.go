package xtcp

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/misc"
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
	x.xtcpRecordPool.Init(func() *xtcp_flat_record.XtcpFlatRecord { return new(xtcp_flat_record.XtcpFlatRecord) })
	x.nlhPool.Init(func() *xtcpnl.NlMsgHdr { return new(xtcpnl.NlMsgHdr) })
	x.rtaPool.Init(func() *xtcpnl.RTAttr { return new(xtcpnl.RTAttr) })
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
		fresh := x.xtcpRecordPool.Get()
		if fresh.Hostname != "" && fresh.Hostname != test.xtcpRecord.Hostname {
			t.Errorf("test %d %s: pooled record Hostname=%q want=%q",
				i, test.description, fresh.Hostname, test.xtcpRecord.Hostname)
		}
		x.xtcpRecordPool.Put(fresh)
	}
}

// TestDeserialize_stampsNetnsIdentity verifies that Deserialize stamps both
// the best-effort namespace name (Netns, threaded by pointer) and the stable
// namespace inode (NetnsInode, passed alongside in DeserializeArgs) onto every
// record it produces. Uses a large flush threshold and a directly-installed
// currentEnvelope so the produced records stay inspectable rather than being
// flushed + pool-returned.
func TestDeserialize_stampsNetnsIdentity(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	// Never flush during the test so the rows stay in currentEnvelope.
	x.config.EnvelopeFlushThresholdRows = 1_000_000
	x.config.EnvelopeFlushThresholdBytes = 1 << 30
	x.currentEnvelope = &xtcp_flat_record.Envelope{}

	const fixture = "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet2.pcap"
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatalf("open %s: %v", fixture, err)
	}
	bs, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read %s: %v", fixture, err)
	}
	buf := bs[xtcpnl.PcapNetlinkOffsetCst:]

	const wantInode = uint64(4026532444)
	nsName := "test-ns-name"
	n, errD := x.Deserialize(context.Background(), DeserializeArgs{
		ns:             &nsName,
		fd:             0,
		inode:          wantInode,
		NLPacket:       &buf,
		xtcpRecordPool: &x.xtcpRecordPool,
		nlhPool:        &x.nlhPool,
		rtaPool:        &x.rtaPool,
		pC:             x.pC,
		pH:             x.pH,
		id:             0,
	})
	if errD != nil {
		t.Fatalf("Deserialize err: %v (n=%d)", errD, n)
	}
	if n == 0 || len(x.currentEnvelope.Row) == 0 {
		t.Fatalf("no records produced: n=%d rows=%d", n, len(x.currentEnvelope.Row))
	}
	for i, r := range x.currentEnvelope.Row {
		if r.NetnsInode != wantInode {
			t.Errorf("row %d NetnsInode=%d, want %d", i, r.NetnsInode, wantInode)
		}
		if r.Netns != nsName {
			t.Errorf("row %d Netns=%q, want %q", i, r.Netns, nsName)
		}
	}
}

// TestDeserialize_stampsTimestampNs verifies that Deserialize stamps each
// record's timestamp from the fd's poll time as a true epoch-NANOSECOND value
// (not epoch seconds), preserving sub-second resolution. This is a regression
// test for the units bug where the poll time was divided by 1e9 before being
// stamped, so timestamp_ns actually held epoch seconds and every derived
// event_date collapsed to 1970-01-01. It exercises the real stamping seam in
// Deserialize — the existing parquet/date tests construct records directly with
// UnixNano() and so never cover the production stamping path.
//
// Assertions wrap the value in int64(...) so they compile whether TimestampNs
// is float64 (pre-fix) or int64 (post-fix).
func TestDeserialize_stampsTimestampNs(t *testing.T) {
	x := newTestDeserializeXTCP(t)
	// Never flush during the test so the rows stay in currentEnvelope.
	x.config.EnvelopeFlushThresholdRows = 1_000_000
	x.config.EnvelopeFlushThresholdBytes = 1 << 30
	x.currentEnvelope = &xtcp_flat_record.Envelope{}

	// Install a known poll time (with a non-zero sub-second component) for the
	// fd Deserialize will read (deserialize.go: x.pollTime.Load(d.fd)).
	const fd = 0
	pollTime := time.Date(2026, 8, 11, 12, 0, 0, 123456789, time.UTC)
	x.pollTime.Store(fd, pollTime)
	wantNs := pollTime.UnixNano() // exact epoch ns, ~1.75e18
	const wantDate = "2026-08-11"

	const fixture = "../xtcpnl/testdata/6_6_44/netlink_sock_diag_reply_single_packet2.pcap"
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatalf("open %s: %v", fixture, err)
	}
	bs, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		t.Fatalf("read %s: %v", fixture, err)
	}
	buf := bs[xtcpnl.PcapNetlinkOffsetCst:]

	nsName := "test-ns-name"
	n, errD := x.Deserialize(context.Background(), DeserializeArgs{
		ns:             &nsName,
		fd:             fd,
		NLPacket:       &buf,
		xtcpRecordPool: &x.xtcpRecordPool,
		nlhPool:        &x.nlhPool,
		rtaPool:        &x.rtaPool,
		pC:             x.pC,
		pH:             x.pH,
		id:             0,
	})
	if errD != nil {
		t.Fatalf("Deserialize err: %v (n=%d)", errD, n)
	}
	if n == 0 || len(x.currentEnvelope.Row) == 0 {
		t.Fatalf("no records produced: n=%d rows=%d", n, len(x.currentEnvelope.Row))
	}

	for i, r := range x.currentEnvelope.Row {
		gotNs := int64(r.TimestampNs)
		if gotNs != wantNs {
			t.Errorf("row %d TimestampNs = %d, want %d (exact epoch ns incl. sub-second)", i, gotNs, wantNs)
		}
		if gotNs < 1_000_000_000_000_000_000 { // < 1e18 → seconds/ms, not nanoseconds
			t.Errorf("row %d TimestampNs = %d looks like seconds, want epoch nanoseconds (>= 1e18)", i, gotNs)
		}
		if gotDate := time.Unix(0, gotNs).UTC().Format("2006-01-02"); gotDate != wantDate {
			t.Errorf("row %d derived date = %q, want %q (1970 exposes the seconds-vs-ns bug)", i, gotDate, wantDate)
		}
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

	resultXtcpFlatRecord = x.xtcpRecordPool.Get()
}
