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
	//c           config.Config
	xtcpRecord *xtcp_flat_record.XtcpFlatRecord
	// xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
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

	xtcp := new(XTCP)

	// https://github.com/prometheus/client_golang/issues/1140
	reg := prometheus.NewRegistry()
	pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp",
			Name:      "histograms",
			Help:      "xtcp historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)

	xtcpRecordPool := sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.XtcpFlatRecord)
			//return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}

	nlhPool := sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.NlMsgHdr)
		},
	}

	rtaPool := sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.RTAttr)
		},
	}

	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Error("Test Failed Open error:", err)
		}
		defer f.Close()

		bs, err := io.ReadAll(f)
		if err != nil {
			t.Error("Test Failed ReadAll error:", err)
		}

		//t.Logf("i:%d, binary.Size(bs):%d", i, binary.Size(bs))
		//t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs))

		var buf []byte
		if strings.HasSuffix(test.filename, ".pcap") {
			buf = bs[xtcpnl.PcapNetlinkOffsetCst:]
		} else {
			buf = bs
		}

		//t.Logf("i:%d, binary.Size(buf):%d", i, binary.Size(buf))
		//t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf))

		xtcpRecord := new(xtcp_flat_record.XtcpFlatRecord)
		// xtcpRecord := new(xtcp_flat_record.Envelope_XtcpFlatRecord)

		nsName := "fixme"

		_, errD := xtcp.Deserialize(
			ctx,
			DeserializeArgs{
				ns:             &nsName,
				fd:             0, //FIXME
				NLPacket:       &buf,
				xtcpRecordPool: &xtcpRecordPool,
				nlhPool:        &nlhPool,
				rtaPool:        &rtaPool,
				pC:             pC,
				pH:             pH,
				id:             0,
			})

		if errD != nil {
			t.Fatal("Test Failed Deserialize errD", errD)
		}

		if (*xtcpRecord).Hostname != test.xtcpRecord.Hostname {
			t.Errorf("Test %d %s (*xtcpRecord).Hostname:%s != test.xtcpRecord.Hostname:%s", i, test.description, (*xtcpRecord).Hostname, test.xtcpRecord.Hostname)
		}
	}
}

var (
	resultXtcpFlatRecord *xtcp_flat_record.XtcpFlatRecord
	// resultXtcpFlatRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
)

// go test -bench=BenchmarkDeserializeSpawn
// go test -bench=BenchmarkDeserializeSpawn -benchtime=60s
func BenchmarkDeserialize(b *testing.B) {
	DeserializeBoth(b, 0)
}

func DeserializeBoth(b *testing.B, s int) {

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

	xtcp := new(XTCP)

	reg := prometheus.NewRegistry()
	pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp",
			Name:      "histograms",
			Help:      "xtcp historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)

	xtcpRecordPool := sync.Pool{
		New: func() interface{} {
			return new(xtcp_flat_record.XtcpFlatRecord)
			// return new(xtcp_flat_record.Envelope_XtcpFlatRecord)
		},
	}

	nlhPool := sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.NlMsgHdr)
		},
	}

	rtaPool := sync.Pool{
		New: func() interface{} {
			return new(xtcpnl.RTAttr)
		},
	}

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	var buf []byte
	if strings.HasSuffix(test.filename, ".pcap") {
		buf = bs[xtcpnl.PcapNetlinkOffsetCst:]
	} else {
		buf = bs
	}

	xtcpRecord := new(xtcp_flat_record.XtcpFlatRecord)
	// xtcpRecord := new(xtcp_flat_record.Envelope_XtcpFlatRecord)

	nsName := "fixme"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {

		_, errD := xtcp.Deserialize(
			ctx,
			DeserializeArgs{
				ns:             &nsName,
				fd:             0, //FIXME
				NLPacket:       &buf,
				xtcpRecordPool: &xtcpRecordPool,
				nlhPool:        &nlhPool,
				rtaPool:        &rtaPool,
				pC:             pC,
				pH:             pH,
				id:             0,
			})

		if errD != nil {
			b.Fatal("Test Failed Deserialize errD", errD)
		}
	}

	resultXtcpFlatRecord = xtcpRecord

}
