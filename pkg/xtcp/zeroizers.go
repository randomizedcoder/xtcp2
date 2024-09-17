package xtcp

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
)

func (x *XTCP) InitZeroizers() {

	x.xtcpRecordZeroizer = make(map[xtcppb.FlatXtcpRecordCongestionAlgorithm]func(xtcpRecord *xtcppb.FlatXtcpRecord))

	//x.xtcpRecordZeroizer[FlatXtcpRecord_CONGESTION_ALGORITHM_UNSPECIFIED]

	x.xtcpRecordZeroizer[xtcppb.FlatXtcpRecord_CONGESTION_ALGORITHM_BBR1] = func(xtcpRecord *xtcppb.FlatXtcpRecord) {
		xtcpnl.ZeroizeBBRInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcppb.FlatXtcpRecord_CONGESTION_ALGORITHM_BBR2] = func(xtcpRecord *xtcppb.FlatXtcpRecord) {
	// 	xtcpnl.ZeroizeBBR2InfoXTCP(xtcpRecord)
	// }

	x.xtcpRecordZeroizer[xtcppb.FlatXtcpRecord_CONGESTION_ALGORITHM_DCTCP] = func(xtcpRecord *xtcppb.FlatXtcpRecord) {
		xtcpnl.ZeroizeDCTCPInfoXTCP(xtcpRecord)
	}

	x.xtcpRecordZeroizer[xtcppb.FlatXtcpRecord_CONGESTION_ALGORITHM_VEGAS] = func(xtcpRecord *xtcppb.FlatXtcpRecord) {
		xtcpnl.ZeroizeVegasInfoXTCP(xtcpRecord)
	}

	// x.xtcpRecordZeroizer[xtcppb.FlatXtcpRecord_CONGESTION_ALGORITHM_PRAGUE] = func(xtcpRecord *xtcppb.FlatXtcpRecord) {
	// 	xtcpnl.ZeroizePragueInfoXTCP(xtcpRecord)
	// }
}
