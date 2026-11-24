package xtcp

import (
	"sync"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// EnvelopeZero clears Row for sync.Pool reuse without dropping the
// underlying slice capacity. Caller is responsible for having already
// returned each *XtcpFlatRecord to xtcpRecordPool.
func (x *XTCP) EnvelopeZero(e *xtcp_flat_record.Envelope) {
	if e == nil {
		return
	}
	e.Row = e.Row[:0]
}

func (x *XTCP) InitZeroizers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.xtcpRecordZeroizer = make(map[xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm]func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord))
	// x.xtcpRecordZeroizer = make(map[xtcp_flat_record.Envelope_XtcpFlatRecord_CongestionAlgorithm]func(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord))

	// x.xtcpRecordZeroizer[XtcpFlatRecord]

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		// x.xtcpRecordZeroizer[xtcp_flat_record.Envelope_XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1] = func(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
		xtcpnl.ZeroizeBBRInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcppb.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR2] = func(xtcpRecord *xtcppb.XtcpFlatRecord) {
	// 	xtcpnl.ZeroizeBBR2InfoXTCP(xtcpRecord)
	// }

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		// x.xtcpRecordZeroizer[xtcp_flat_record.Envelope_XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP] = func(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
		xtcpnl.ZeroizeDCTCPInfoXTCP(xtcpRecord)
	}

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		// x.xtcpRecordZeroizer[xtcp_flat_record.Envelope_XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS] = func(xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) {
		xtcpnl.ZeroizeVegasInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_PRAGUE] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
	// 	xtcpnl.ZeroizePragueInfoXTCP(xtcpRecord)
	// }
}

// recycleRecord clears r for the record pool but keeps the capacity of its
// two address buffers. DeserializeInetDiagSockIDXTCP copies each socket's
// 16-byte source and destination into those buffers (it can't alias the
// receive buffer, which is reused for the next packet), so keeping them
// means a recycled record decodes its addresses without allocating. Reset
// alone nils them and costs two allocations per socket on every poll.
//
// Only call this once nothing else holds r: flushEnvelope has marshaled and
// sent the envelope, and gRPC stream sends are synchronous.
func recycleRecord(r *xtcp_flat_record.XtcpFlatRecord) {
	src := r.InetDiagMsgSocketSource[:0]
	dst := r.InetDiagMsgSocketDestination[:0]
	r.Reset()
	r.InetDiagMsgSocketSource = src
	r.InetDiagMsgSocketDestination = dst
}
