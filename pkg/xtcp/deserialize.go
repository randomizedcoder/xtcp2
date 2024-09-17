package xtcp

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
)

var (
	ErrParseDeserializeNlMsgHdr       = errors.New("Deserialize DeserializeNlMsgHdr error")
	ErrParseDeserializeNLHTypeUnknown = errors.New("Deserialize nlh.Type unknown error")
)

type DeserializeArgs struct {
	ctx            context.Context
	NLPacket       *[]byte
	xtcpRecordPool *sync.Pool
	nlhPool        *sync.Pool
	rtaPool        *sync.Pool
	pC             *prometheus.CounterVec
	pH             *prometheus.SummaryVec
	id             int
}

func (x *XTCP) Deserialize(ctx context.Context, d DeserializeArgs) (n uint64, err error) {

	startTime := time.Now()
	defer func() {
		d.pH.WithLabelValues("Deserialize", "complete", "count").Observe(time.Since(startTime).Seconds())
	}()
	d.pC.WithLabelValues("Deserialize", "start", "count").Inc()

	var hostname string
	if h, ok := x.hostname.Load(hostnameKeyCst); ok {
		hostname = h.(string)
	} else {
		d.pC.WithLabelValues("Deserialize", "hostname", "error").Inc()
	}

	var startPollTime time.Time
	if s, ok := x.pollTime.Load(startPollTimeKeyCst); ok {
		startPollTime = s.(time.Time)
	} else {
		d.pC.WithLabelValues("Deserialize", "pollTime", "error").Inc()
	}

	sec, nsec := startPollTime.UnixNano()/1e9, startPollTime.UnixNano()%1e9

	offset := 0
	length := 0
	end := len(*d.NLPacket)

	for n = 0; offset < end; n++ {

		d.pC.WithLabelValues("Deserialize", "n", "count").Inc()

		// if x.debugLevel > 1000 {
		// 	log.Printf("Deserialize n:%d", n)
		// }

		if *x.config.Modulus != 1 {
			if n%uint64((*x.config.Modulus)) != 1 {
				d.pC.WithLabelValues("Deserialize", "continue", "count").Inc()
				continue
			}
		}

		nlPacketStartTime := time.Now()
		xtcpRecord := x.xtcpRecordPool.Get().(*xtcppb.FlatXtcpRecord)
		(*xtcpRecord).Hostname = hostname
		(*xtcpRecord).Sec, (*xtcpRecord).Nsec = sec, nsec
		(*xtcpRecord).RecordCounter = n
		(*xtcpRecord).NetlinkerId = uint32(d.id)

		nlh := d.nlhPool.Get().(*xtcpnl.NlMsgHdr)

		var errD error
		length = xtcpnl.NlMsgHdrSizeCst
		_, errD = xtcpnl.DeserializeNlMsgHdr((*d.NLPacket)[offset:offset+length], nlh)
		if errD != nil {
			d.pC.WithLabelValues("Deserialize", "DeserializeNlMsgHdr", "error").Inc()
			return n, ErrParseDeserializeNlMsgHdr
		}

		offset += length

		if nlh.Type == xtcpnl.NlMsgHdrTypeDoneCst {
			x.netlinkerDoneCh <- time.Now()
			return n, nil
		}

		if nlh.Type != xtcpnl.NlMsgHdrTypeInetDiagCst {
			return n, ErrParseDeserializeNLHTypeUnknown
		}

		length := xtcpnl.InetDiagMsgSizeCst
		xtcpnl.DeserializeInetDiagMsgXTCP((*d.NLPacket)[offset:offset+length], xtcpRecord)
		offset += length

		length = int(nlh.Len) - xtcpnl.NlMsgHdrSizeCst - xtcpnl.InetDiagMsgSizeCst
		x.DeserializeAttributes(DeserializeAttributesArgs{
			NLPacket:   d.NLPacket,
			xtcpRecord: xtcpRecord,
			rtaPool:    d.rtaPool,
			pC:         d.pC,
			pH:         d.pH,
			id:         d.id,
			offset:     offset,
			end:        offset + length,
		})
		offset += length

		if x.debugLevel > 1000 {
			log.Printf("Deserialize n:%d x.Destation(ctx, x.Marshaler(xtcpRecord))", n)
		}

		n, err := x.Destation(ctx, x.Marshaler(xtcpRecord))
		if err != nil {
			d.pC.WithLabelValues("Deserialize", "Destation", "error").Inc()
		} else {
			d.pC.WithLabelValues("Deserialize", "Destation", "count").Add(float64(n))
		}

		if x.debugLevel > 10 {
			log.Printf("Deserialize %d n:%d xtcpRecord:%v", d.id, n, xtcpRecord)
		}

		d.nlhPool.Put(nlh)

		// We could use reset, but because we expect to overwrite all the values except "cong"
		// we can simply clear the "cong" specific attributes
		//xtcpRecord.Reset()
		x.ZeroXTCPCongRecord(xtcpRecord)
		d.xtcpRecordPool.Put(xtcpRecord)

		d.pH.WithLabelValues("Deserialize", "nlPacketComplete", "count").Observe(time.Since(nlPacketStartTime).Seconds())

	}
	return n, nil
}

// ZeroXTCPCongRecord will zero out the congestion algorithm specific fields
// We need to do this because these won't get over written each time
func (x *XTCP) ZeroXTCPCongRecord(xtcpRecord *xtcppb.FlatXtcpRecord) {
	if zeroer, ok := x.xtcpRecordZeroizer[xtcpRecord.CongestionAlgorithmEnum]; ok {
		zeroer(xtcpRecord)
	}
}

type DeserializeAttributesArgs struct {
	NLPacket   *[]byte
	xtcpRecord *xtcppb.FlatXtcpRecord
	rtaPool    *sync.Pool
	pC         *prometheus.CounterVec
	pH         *prometheus.SummaryVec
	id         int
	offset     int
	end        int
}

func (x *XTCP) DeserializeAttributes(d DeserializeAttributesArgs) {

	// Prometheus counters ended up using a lot of CPU, so don't bother
	// startTime := time.Now()
	// defer func() {
	// 	d.pH.WithLabelValues("Deserialize", "complete", "count").Observe(time.Since(startTime).Seconds())
	// }()
	// d.pC.WithLabelValues("Deserialize", "start", "count").Inc()

	for j := 0; d.offset < d.end; j++ {

		rta := d.rtaPool.Get().(*xtcpnl.RTAttr)

		length := xtcpnl.RTAttrSizeCst
		_, errD := xtcpnl.DeserializeRTAttr((*d.NLPacket)[d.offset:d.offset+length], rta)
		if errD != nil {
			log.Fatal("Test Failed DeserializeRTAttr errD", errD)
		}
		d.offset += length

		length = int(rta.Len) - xtcpnl.RTAttrSizeCst + xtcpnl.FourByteAlignPadding(int(rta.Len))
		x.DeserializeAttribute(DeserializeAttributeArgs{
			Type:       int(rta.Type),
			buf:        (*d.NLPacket)[d.offset : d.offset+length],
			xtcpRecord: d.xtcpRecord,
			pC:         d.pC,
			pH:         d.pH,
		})

		d.offset += length

		d.rtaPool.Put(rta)
	}
}

type DeserializeAttributeArgs struct {
	Type       int
	buf        []byte
	xtcpRecord *xtcppb.FlatXtcpRecord
	pC         *prometheus.CounterVec
	pH         *prometheus.SummaryVec
}

func (x *XTCP) DeserializeAttribute(d DeserializeAttributeArgs) error {

	// Prometheus counters ended up using a lot of CPU, so don't bother
	// startTime := time.Now()
	// defer func() {
	// 	pH.WithLabelValues("DeserializeAttribute", x.RTATypeDeserializerStr[Type], "count").Observe(time.Since(startTime).Seconds())
	// }()
	// pC.WithLabelValues("DeserializeAttribute", x.RTATypeDeserializerStr[Type], "count").Inc()

	if Deserializer, ok := x.RTATypeDeserializer[d.Type]; ok {
		Deserializer(d.buf, d.xtcpRecord)
		return nil
	}

	if x.debugLevel > 1000 {
		log.Printf("DeserializeAttribute skipping:%d", d.Type)
	}

	return nil
}
