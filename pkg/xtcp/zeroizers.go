package xtcp

import (
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func (x *XTCP) InitZeroizers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.xtcpRecordZeroizer = make(map[xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm]func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord))

	//x.xtcpRecordZeroizer[XtcpFlatRecord]

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		xtcpnl.ZeroizeBBRInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcppb.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR2] = func(xtcpRecord *xtcppb.XtcpFlatRecord) {
	// 	xtcpnl.ZeroizeBBR2InfoXTCP(xtcpRecord)
	// }

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		xtcpnl.ZeroizeDCTCPInfoXTCP(xtcpRecord)
	}

	x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
		xtcpnl.ZeroizeVegasInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_PRAGUE] = func(xtcpRecord *xtcp_flat_record.XtcpFlatRecord) {
	// 	xtcpnl.ZeroizePragueInfoXTCP(xtcpRecord)
	// }
}
