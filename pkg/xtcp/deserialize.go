package xtcp

import (
	"context"
	"errors"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

var (
	ErrParseDeserializeNlMsgHdr       = errors.New("Deserialize DeserializeNlMsgHdr error")
	ErrParseDeserializeNLHTypeUnknown = errors.New("Deserialize nlh.Type unknown error")
)

type DeserializeArgs struct {
	ns             *string
	fd             int
	NLPacket       *[]byte
	xtcpRecordPool *sync.Pool
	nlhPool        *sync.Pool
	rtaPool        *sync.Pool
	pC             *prometheus.CounterVec
	pH             *prometheus.SummaryVec
	id             uint32
}

func (x *XTCP) Deserialize(ctx context.Context, d DeserializeArgs) (n uint64, err error) {

	startTime := time.Now()
	defer func() {
		d.pH.WithLabelValues("Deserialize", "complete", "count").Observe(time.Since(startTime).Seconds())
	}()
	d.pC.WithLabelValues("Deserialize", "start", "count").Inc()

	var startPollTime time.Time
	if s, ok := x.pollTime.Load(d.fd); ok {
		startPollTime = s.(time.Time)
	} else {
		d.pC.WithLabelValues("Deserialize", "pollTime", "error").Inc()
	}

	timestampNs := float64(startPollTime.UnixNano()) / 1e9
	// sec, nsec := uint64(startPollTime.UnixNano()/1e9), uint64(startPollTime.UnixNano()%1e9)

	offset := 0
	length := 0
	end := len(*d.NLPacket)

	for n = 0; offset < end; n++ {

		d.pC.WithLabelValues("Deserialize", "n", "count").Inc()

		// if x.debugLevel > 1000 {
		// 	log.Printf("Deserialize n:%d", n)
		// }

		if x.config.Modulus != 1 {
			if n%uint64(x.config.Modulus) != 1 {
				d.pC.WithLabelValues("Deserialize", "continue", "count").Inc()
				continue
			}
		}

		nlPacketStartTime := time.Now()
		xtcpRecord := x.xtcpRecordPool.Get().(*xtcp_flat_record.Envelope_XtcpFlatRecord)

		(*xtcpRecord).Hostname = x.hostname
		(*xtcpRecord).TimestampNs = timestampNs
		// (*xtcpRecord).Sec, (*xtcpRecord).Nsec = sec, nsec

		(*xtcpRecord).Netns = *d.ns
		(*xtcpRecord).RecordCounter = n
		(*xtcpRecord).SocketFd = uint64(d.fd)
		(*xtcpRecord).NetlinkerId = uint64(d.id)

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

			select {
			case x.netlinkerDoneCh <- netlinkerDone{
				fd: d.fd,
				t:  time.Now(),
			}:
			// Non-blocking
			// This allows us to see if the channel ever becomes blocking
			default:
				d.pC.WithLabelValues("Deserialize", "netlinkerDoneCh", "error").Inc()
				// Blocking
				x.netlinkerDoneCh <- netlinkerDone{
					fd: d.fd,
					t:  time.Now(),
				}
			}

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
			log.Printf("Deserialize n:%d x.Destination(ctx, x.Marshaler(xtcpRecord))", n)
		}

		// single record send to GRPC client
		x.flatRecordServiceSend(xtcpRecord)

		//xr := xtcp_flat_record.Envelope_XtcpFlatRecord(xtcpRecord)

		x.envelopeMu.Lock()
		x.currentEnvelope.Row = append(x.currentEnvelope.Row, xtcpRecord)
		x.envelopeMu.Unlock()

		if x.debugLevel > 100 {
			log.Printf("Deserialize %d n:%d xtcpRecord:%v", d.id, n, xtcpRecord)
		}

		if x.debugLevel > 1000 {
			d.pC.WithLabelValues("Deserialize", strconv.Itoa(d.fd), "count").Inc()
		}

		d.nlhPool.Put(nlh)

		// We could use reset, but because we expect to overwrite all the values except "cong"
		// we can simply clear the "cong" specific attributes
		x.ZeroXTCPCongRecord(xtcpRecord)
		xtcpRecord.Reset()

		d.xtcpRecordPool.Put(xtcpRecord)

		d.pH.WithLabelValues("Deserialize", "nlPacketComplete", "count").Observe(time.Since(nlPacketStartTime).Seconds())

	}
	return n, nil
}

// ZeroXTCPCongRecord will zero out the congestion algorithm specific fields
// We need to do this because these won't get over written each time
func (x *XTCP) ZeroXTCPCongRecord(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
	if zeroer, ok := x.xtcpRecordZeroizer[xtcpRecord.CongestionAlgorithmEnum]; ok {
		zeroer(xtcpRecord)
	}
}

type DeserializeAttributesArgs struct {
	NLPacket   *[]byte
	xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
	rtaPool    *sync.Pool
	pC         *prometheus.CounterVec
	pH         *prometheus.SummaryVec
	id         uint32
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
	xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
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
