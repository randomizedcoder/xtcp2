package xtcp

import (
	"context"
	"log"
	"os"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func (x *XTCP) Init(ctx context.Context, allDoneCh *chan struct{}) {

	x.InitMarshalers()
	x.InitDestinations(ctx)
	x.InputValidation()

	x.InitSyncPools()
	x.InitPromethus()
	x.InitDeserializers()
	x.InitZeroizers()

	// x.InitIOURing()

	x.socketFD, x.socketAddress = xtcpnl.OpenNetlinkSocketWithTimeout(*x.config.NLTimeout)
	x.nlRequest = x.CreateNetLinkRequest()

	x.netlinkerDoneCh = make(chan time.Time, netlinkerDoneChSizeCst)
	x.allDoneCh = allDoneCh

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("os.Hostname() error:%s", err)
	}
	x.hostname.Store(hostnameKeyCst, hostname)

	if x.debugLevel > 10 {
		log.Println("NewXTCP init complete")
	}
}

func (x *XTCP) CreateNetLinkRequest() (nlRequest *[]byte) {

	nlh := xtcpnl.NlMsgHdr{
		Len:   xtcpnl.InetDiagRequestSizeCst,
		Type:  xtcpnl.SocketDiagByFamilyCst,
		Flags: uint16(syscall.NLM_F_DUMP | syscall.NLM_F_REQUEST | syscall.NLM_F_REPLACE | syscall.NLM_F_EXCL),
		Seq:   uint32(*x.config.NlmsgSeq),
		//Pid: 0,
	}

	//https://github.com/torvalds/linux/blob/481ed297d900af0ce395f6ca8975903b76a5a59e/include/linux/socket.h#L165
	//#define AF_INET		2	/* Internet IP Protocol 	*/
	//#define AF_INET6	10	/* IP version 6			*/
	req := xtcpnl.InetDiagReqV2{
		SDiagFamily:   2, // #define AF_INET      2
		SDiagProtocol: 6, // IPPROTO_TCP = 6
		IDiagExt:      127,
		IDiagStates:   4282318848, // This value is just copied from "ss" requests.  Previous xtcp just lit it up with 0xFF
	}

	requestBytes := make([]byte, xtcpnl.InetDiagRequestSizeCst)
	nlRequest = &requestBytes

	xtcpnl.SerializeNetlinkDagRequest(nlh, req, nlRequest)

	return nlRequest
}

func (x *XTCP) InitPromethus() {

	x.pC = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: "xtcp",
			Name:      "counts",
			Help:      "xtcp counts",
		},
		[]string{"function", "variable", "type"},
	)

	x.pH = promauto.NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp",
			Name:      "histograms",
			Help:      "xtcp historgrams",
			Objectives: map[float64]float64{
				0.1:  quantileError,
				0.5:  quantileError,
				0.9:  quantileError,
				0.99: quantileError,
			},
			MaxAge: summaryVecMaxAge,
		},
		[]string{"function", "variable", "type"},
	)

}
