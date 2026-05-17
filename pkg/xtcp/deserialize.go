package xtcp

import (
	"context"
	"errors"
	"log"
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
		startPollTime, _ = s.(time.Time) //nolint:errcheck // sync.Map Store sites all use time.Time
	} else {
		d.pC.WithLabelValues("Deserialize", "pollTime", "error").Inc()
	}

	timestampNs := float64(startPollTime.UnixNano()) / 1e9

	offset := 0
	length := 0
	end := len(*d.NLPacket)

	if x.debugLevel > 10 {
		log.Printf("Deserialize n:%d, offset:%d, end:%d", n, offset, end)
	}

	for n = 0; offset < end; n++ {

		d.pC.WithLabelValues("Deserialize", "n", "count").Inc()

		// Safety net before slicing: if the remaining buffer is shorter than
		// a netlink header, the packet is truncated. DeserializeNlMsgHdr
		// below would already return ErrNlMsgHdrSmall, but the slice
		// expression that feeds it would panic with "slice bounds out of
		// range" first. Reject cleanly so a malformed kernel response (or
		// adversarial input) can't crash the daemon.
		if end-offset < xtcpnl.NlMsgHdrSizeCst {
			d.pC.WithLabelValues("Deserialize", "truncatedAtHeader", "error").Inc()
			return n, ErrParseDeserializeNlMsgHdr
		}

		if x.config.Modulus != 1 {
			if n%x.config.Modulus != 1 {
				d.pC.WithLabelValues("Deserialize", "continue", "count").Inc()
				continue
			}
		}

		nlPacketStartTime := time.Now()
		xtcpRecord, _ := x.xtcpRecordPool.Get().(*xtcp_flat_record.XtcpFlatRecord) //nolint:errcheck // pool.New returns *XtcpFlatRecord

		xtcpRecord.Hostname = x.hostname
		xtcpRecord.TimestampNs = timestampNs
		xtcpRecord.Netns = *d.ns
		xtcpRecord.RecordCounter = n
		xtcpRecord.SocketFd = uint64(d.fd)
		xtcpRecord.NetlinkerId = uint64(d.id)

		nlh, _ := d.nlhPool.Get().(*xtcpnl.NlMsgHdr) //nolint:errcheck // pool.New returns *NlMsgHdr

		length = xtcpnl.NlMsgHdrSizeCst
		if _, errD := xtcpnl.DeserializeNlMsgHdr((*d.NLPacket)[offset:offset+length], nlh); errD != nil {
			d.pC.WithLabelValues("Deserialize", "DeserializeNlMsgHdr", "error").Inc()
			return n, ErrParseDeserializeNlMsgHdr
		}
		offset += length

		if nlh.Type == xtcpnl.NlMsgHdrTypeDoneCst {
			x.signalNetlinkerDone(d)
			d.nlhPool.Put(nlh)
			d.xtcpRecordPool.Put(xtcpRecord)
			return n, nil
		}

		if nlh.Type != xtcpnl.NlMsgHdrTypeInetDiagCst {
			advance, ok := x.skipUnknownNlmsg(d, nlh, offset, end)
			d.nlhPool.Put(nlh)
			d.xtcpRecordPool.Put(xtcpRecord)
			if !ok {
				return n, nil
			}
			offset = advance
			continue
		}

		offset = x.processInetDiagRecord(ctx, d, xtcpRecord, nlh, offset, n)
		d.nlhPool.Put(nlh)
		d.pH.WithLabelValues("Deserialize", "nlPacketComplete", "count").Observe(time.Since(nlPacketStartTime).Seconds())
	}
	return n, nil
}

// processInetDiagRecord parses the InetDiagMsg body + its attributes
// into xtcpRecord, fans the populated record out to the gRPC stream
// service, and ships it through the configured destination. Returns
// the new offset after consuming the message body.
func (x *XTCP) processInetDiagRecord(
	ctx context.Context,
	d DeserializeArgs,
	xtcpRecord *xtcp_flat_record.XtcpFlatRecord,
	nlh *xtcpnl.NlMsgHdr,
	offset int,
	n uint64,
) int {
	length := xtcpnl.InetDiagMsgSizeCst
	if ierr := xtcpnl.DeserializeInetDiagMsgXTCP((*d.NLPacket)[offset:offset+length], xtcpRecord); ierr != nil {
		d.pC.WithLabelValues("Deserialize", "DeserializeInetDiagMsgXTCP", "error").Inc()
	}
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
		log.Printf("Deserialize n:%d x.dest.Send(ctx, x.Marshaler(xtcpRecord))", n)
	}

	x.flatRecordServiceSend(xtcpRecord)

	sent, serr := x.dest.Send(ctx, x.Marshaller(xtcpRecord))
	if serr != nil {
		d.pC.WithLabelValues("Deserialize", "Destation", "error").Inc()
	} else {
		d.pC.WithLabelValues("Deserialize", "Destation", "count").Add(float64(sent))
	}

	return offset
}

// signalNetlinkerDone emits the per-fd "dump complete" event the poller
// is waiting for. Tries a non-blocking send first so we can count
// instances where the channel is saturated, then falls back to the
// blocking send to preserve at-least-once delivery.
func (x *XTCP) signalNetlinkerDone(d DeserializeArgs) {
	select {
	case x.netlinkerDoneCh <- netlinkerDone{fd: d.fd, t: time.Now()}:
	default:
		d.pC.WithLabelValues("Deserialize", "netlinkerDoneCh", "error").Inc()
		x.netlinkerDoneCh <- netlinkerDone{fd: d.fd, t: time.Now()}
	}
}

// skipUnknownNlmsg advances `offset` past a non-InetDiag message body
// (NLMSG_NOOP=1, NLMSG_ERROR=2, NLMSG_OVERRUN=4, or any vendor/firewall
// message the kernel interleaves). nlh.Len covers header + payload;
// the header has already been consumed, so the body length is
// `nlh.Len - 16`. Returns (newOffset, true) on success or (0, false)
// when the declared length would either rewind the cursor or overrun
// the buffer — both of which would otherwise lead to an infinite loop
// or a panic. The caller releases pool resources in either case.
func (x *XTCP) skipUnknownNlmsg(d DeserializeArgs, nlh *xtcpnl.NlMsgHdr, offset, end int) (int, bool) {
	d.pC.WithLabelValues("Deserialize", "skipUnknownType", "count").Inc()
	if x.debugLevel > 10 {
		log.Printf("Deserialize skipping nlh.Type:%d nlh.Len:%d offset:%d end:%d",
			nlh.Type, nlh.Len, offset, end)
	}
	bodyLen := int(nlh.Len) - xtcpnl.NlMsgHdrSizeCst
	if bodyLen < 0 || offset+bodyLen > end {
		d.pC.WithLabelValues("Deserialize", "skipUnknownTypeBadLen", "error").Inc()
		return 0, false
	}
	return offset + bodyLen, true
}

// ZeroXTCPCongRecord will zero out the congestion algorithm specific fields
// We need to do this because these won't get over written each time
func (x *XTCP) ZeroXTCPCongRecord(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
	// func (x *XTCP) ZeroXTCPCongRecord(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
	if zeroer, ok := x.xtcpRecordZeroizer[xtcpRecord.CongestionAlgorithmEnum]; ok {
		zeroer(xtcpRecord)
	}
}

type DeserializeAttributesArgs struct {
	NLPacket   *[]byte
	xtcpRecord *xtcp_flat_record.XtcpFlatRecord
	// xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
	rtaPool *sync.Pool
	pC      *prometheus.CounterVec
	pH      *prometheus.SummaryVec
	id      uint32
	offset  int
	end     int
}

func (x *XTCP) DeserializeAttributes(d DeserializeAttributesArgs) {

	// Prometheus counters ended up using a lot of CPU, so don't bother
	// startTime := time.Now()
	// defer func() {
	// 	d.pH.WithLabelValues("Deserialize", "complete", "count").Observe(time.Since(startTime).Seconds())
	// }()
	// d.pC.WithLabelValues("Deserialize", "start", "count").Inc()

	for j := 0; d.offset < d.end; j++ {

		rta, _ := d.rtaPool.Get().(*xtcpnl.RTAttr) //nolint:errcheck // pool.New returns *RTAttr

		length := xtcpnl.RTAttrSizeCst
		_, errD := xtcpnl.DeserializeRTAttr((*d.NLPacket)[d.offset:d.offset+length], rta)
		if errD != nil {
			log.Fatal("Test Failed DeserializeRTAttr errD", errD)
		}
		d.offset += length

		length = int(rta.Len) - xtcpnl.RTAttrSizeCst + xtcpnl.FourByteAlignPadding(int(rta.Len))
		_ = x.DeserializeAttribute(DeserializeAttributeArgs{ //nolint:errcheck // always returns nil today; signature reserves the option
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
	xtcpRecord *xtcp_flat_record.XtcpFlatRecord
	// xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord
	pC *prometheus.CounterVec
	pH *prometheus.SummaryVec
}

func (x *XTCP) DeserializeAttribute(d DeserializeAttributeArgs) error {

	// Prometheus counters ended up using a lot of CPU, so don't bother
	// startTime := time.Now()
	// defer func() {
	// 	pH.WithLabelValues("DeserializeAttribute", x.RTATypeDeserializerStr[Type], "count").Observe(time.Since(startTime).Seconds())
	// }()
	// pC.WithLabelValues("DeserializeAttribute", x.RTATypeDeserializerStr[Type], "count").Inc()

	if Deserializer, ok := x.RTATypeDeserializer[d.Type]; ok {
		_ = Deserializer(d.buf, d.xtcpRecord) //nolint:errcheck // per-attribute deserializers currently return nil; signature reserves the option
		return nil
	}

	if x.debugLevel > 1000 {
		log.Printf("DeserializeAttribute skipping:%d", d.Type)
	}

	return nil
}
