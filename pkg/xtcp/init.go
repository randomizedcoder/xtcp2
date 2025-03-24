package xtcp

import (
	"context"
	"log"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	destinationReadyChSize    = 2
	changePollFrequencyChSize = 2
	pollRequestChSize         = 2
)

func (x *XTCP) Init(ctx context.Context) {

	startTime := time.Now()

	if x.debugLevel > 10 {
		log.Println("Init starting")
	}

	x.schemaID = 5 // TODO change this!!

	if err := x.checkCapabilities(); err != nil {
		log.Print(err) // TODO log.Fatal
	}

	// initChanenls first, so that signaling channels are ready
	x.initChannels()

	if x.debugLevel > 10 {
		log.Printf("InitMarshallers starting, after:%0.3f", time.Since(startTime).Seconds())
	}

	wg := new(sync.WaitGroup)

	wg.Add(1)
	go x.InitMarshallers(wg)
	wg.Add(1)
	go x.InitDests(ctx, wg)

	wg.Wait()

	x.InputValidation()

	wg.Add(1)
	go x.InitPromethus(wg)

	wg.Add(1)
	go x.InitDeserializers(wg)
	wg.Add(1)
	go x.InitZeroizers(wg)

	wg.Add(1)
	go x.InitSyncPools(wg)
	x.initSyncMaps()

	// x.InitIOURing()

	//x.socketFD = xtcpnl.OpenNetlinkSocketWithTimeout(*x.config.NLTimeout)
	wg.Add(1)
	x.nlRequest = x.CreateNetLinkRequest(wg)

	x.initHostname()

	wg.Wait()

	if x.debugLevel > 10 {
		log.Printf("Init complete after:%0.3f", time.Since(startTime).Seconds())
	}
}

func (x *XTCP) initChannels() {

	x.DestinationReady = make(chan struct{}, destinationReadyChSize)
	x.netlinkerDoneCh = make(chan netlinkerDone, int(x.config.NetlinkersDoneChanSize))
	x.changePollFrequencyCh = make(chan time.Duration, changePollFrequencyChSize)
	x.pollRequestCh = make(chan struct{}, pollRequestChSize)

}

func (x *XTCP) initHostname() {
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatalf("os.Hostname() error:%s", err)
	}
	x.hostname = hostname
}

func (x *XTCP) initSyncMaps() {
	x.nsMap = &sync.Map{}
	x.fdToNsMap = &sync.Map{}
	x.netNsDirs = &sync.Map{}

	if _, err := os.Stat(linuxNetNSDirCst); err == nil {
		x.netNsDirs.Store(linuxNetNSDirCst, true)
		if x.debugLevel > 10 {
			log.Println("initSyncMaps x.netNsDirs.Store(" + linuxNetNSDirCst + ")")
		}
	} else {
		if x.debugLevel > 10 {
			log.Println("initSyncMaps NOT x.netNsDirs.Store(" + linuxNetNSDirCst + ")")
		}
	}
	if _, err := os.Stat(dockerNetNsDirCst); err == nil {
		x.netNsDirs.Store(dockerNetNsDirCst, true)
		if x.debugLevel > 10 {
			log.Println("initSyncMaps x.netNsDirs.Store(" + dockerNetNsDirCst + ")")
		}
	} else {
		if x.debugLevel > 10 {
			log.Println("initSyncMaps NOT x.netNsDirs.Store(" + dockerNetNsDirCst + ")")
		}
	}

	i := 0
	x.netNsDirs.Range(func(key, value interface{}) bool {
		i++
		return true
	})

	if i < 1 {
		log.Fatal("initSyncMaps neither network namespace directory exists.  ??!")
	}
}

// CreateNetLinkRequest builds the netlink request
// TODO this currently only creates IPv4 version
func (x *XTCP) CreateNetLinkRequest(wg *sync.WaitGroup) (nlRequest *[]byte) {

	defer wg.Done()

	nlh := xtcpnl.NlMsgHdr{
		Len:   xtcpnl.InetDiagRequestSizeCst,
		Type:  xtcpnl.SocketDiagByFamilyCst,
		Flags: uint16(syscall.NLM_F_DUMP | syscall.NLM_F_REQUEST | syscall.NLM_F_REPLACE | syscall.NLM_F_EXCL),
		Seq:   uint32(x.config.NlmsgSeq),
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

	xtcpnl.SerializeNetlinkDiagRequest(nlh, req, nlRequest)

	return nlRequest
}
