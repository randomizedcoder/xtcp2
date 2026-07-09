package xtcp

import (
	"context"
	"log"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/cgroupid"
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

	if err := capabilityCheck(x); err != nil {
		// checkCapabilities returns a multi-line, actionable error when
		// a hard-required capability (CAP_NET_ADMIN / CAP_SYS_ADMIN) is
		// missing. Fatal at startup so the operator gets a clean exit
		// + diagnostic — far better than a daemon that limps for
		// 1-2 hours and then crashes with "thread exhaustion" because
		// it couldn't setns into discovered namespaces. Soft-required
		// caps (CAP_NET_RAW, CAP_SYS_RESOURCE) print a warning and the
		// daemon continues.
		x.fatalf("startup capability check: %v", err)
		return
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
	go x.InitEnvelopeMarshallers(wg)
	wg.Add(1)
	go x.InitDests(ctx, wg)
	wg.Add(1)
	go x.InitNetlinkers(ctx, wg)

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

	// x.socketFD = xtcpnl.OpenNetlinkSocketWithTimeout(*x.config.NLTimeout)
	wg.Add(1)
	x.nlRequest = x.CreateNetLinkRequest(wg)

	x.initHostname()
	x.initContainerResolver()

	wg.Wait()

	if x.debugLevel > 10 {
		log.Printf("Init complete after:%0.3f", time.Since(startTime).Seconds())
	}
}

// initContainerResolver builds the cgroup-id -> container-id resolver when
// enabled via config. Off by default; the daemon runs identically without it,
// leaving container_id/container_runtime empty. See pkg/cgroupid.
func (x *XTCP) initContainerResolver() {
	if x.config == nil || !x.config.ResolveContainerId {
		return
	}
	x.cgroupResolver = cgroupid.New(cgroupid.DefaultRoot)
	if x.debugLevel > 10 {
		log.Printf("initContainerResolver: cgroup->container resolution enabled (root:%s)", cgroupid.DefaultRoot)
	}
}

func (x *XTCP) initChannels() {

	x.DestinationReady = make(chan struct{}, destinationReadyChSize)
	x.NetlinkerReady = make(chan struct{}, netlinkerReadyChSize)
	x.netlinkerDoneCh = make(chan netlinkerDone, int(x.config.NetlinkersDoneChanSize))
	x.changePollFrequencyCh = make(chan time.Duration, changePollFrequencyChSize)
	x.pollRequestCh = make(chan struct{}, pollRequestChSize)

}

// hostnameLookup is the indirection point for x.initHostname so tests can
// inject an error-returning fake without breaking the host. Production
// defaults to os.Hostname.
var hostnameLookup = os.Hostname

func (x *XTCP) initHostname() {
	// An explicit override wins over os.Hostname(). This is required in a
	// container: os.Hostname() there returns the container id (Docker's UTS
	// hostname), not the host. Operators inject the real host identity via the
	// -hostname flag / XTCP_HOSTNAME env.
	if x.config != nil && x.config.Hostname != "" {
		x.hostname = x.config.Hostname
		return
	}
	hostname, err := hostnameLookup()
	if err != nil {
		x.callFatalf("os.Hostname() error:%s", err)
		return
	}
	x.hostname = hostname
}

// callFatalf invokes x.fatalf when set, falling back to log.Fatalf when
// it isn't (paths that call initSyncMaps / initHostname before the parent
// constructor wires up the fatalf field shouldn't crash on nil-deref).
func (x *XTCP) callFatalf(format string, args ...any) {
	if x.fatalf != nil {
		x.fatalf(format, args...)
		return
	}
	log.Fatalf(format, args...)
}

// netNsCandidateDirs is the list of directories initSyncMaps probes for
// network-namespace mounts. Production lists the two well-known kernel +
// docker locations; tests can prepend a tempdir to this slice so the
// probe finds at least one valid entry and the function runs to
// completion.
var netNsCandidateDirs = []string{linuxNetNSDirCst, dockerNetNsDirCst}

func (x *XTCP) initSyncMaps() {
	x.nsMap = &sync.Map{}
	x.fdToNsMap = &sync.Map{}
	x.netNsDirs = &sync.Map{}

	for _, dir := range netNsCandidateDirs {
		if _, err := os.Stat(dir); err == nil {
			x.netNsDirs.Store(dir, true)
			if x.debugLevel > 10 {
				log.Println("initSyncMaps x.netNsDirs.Store(" + dir + ")")
			}
		} else if x.debugLevel > 10 {
			log.Println("initSyncMaps NOT x.netNsDirs.Store(" + dir + ")")
		}
	}

	i := 0
	x.netNsDirs.Range(func(key, value interface{}) bool {
		i++
		return true
	})

	if i < 1 {
		x.callFatalf("%s", "initSyncMaps neither network namespace directory exists.  ??!")
		return
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
		Seq:   x.config.NlmsgSeq,
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
