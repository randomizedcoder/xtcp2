package rdmanative

// Layouts from include/uapi/rdma/ib_user_verbs.h. TestABILayout compares every
// constant in this file with nix/tests/linkmonitor-abi.c output, kept in
// testdata/abi_layout.txt.
const (
	asyncEventDescLen        = 16
	asyncEventDescOffElement = 0
	asyncEventDescOffType    = 8

	cmdHdrLen         = 8
	cmdHdrOffCommand  = 0
	cmdHdrOffInWords  = 4
	cmdHdrOffOutWords = 6
	cmdGetContext     = 0

	getContextLen                   = 8
	getContextOffResponse           = 0
	getContextRespLen               = 8
	getContextRespOffAsyncFD        = 0
	getContextRespOffNumCompVectors = 4
)

// Async record event_type values. The kernel's enum ib_event_type is not uapi;
// rdma-core's enum ibv_event_type mirrors it and is what the guard compiles.
const (
	eventDeviceFatal       = 8
	eventPortActive        = 9
	eventPortErr           = 10
	eventLIDChange         = 11
	eventPKeyChange        = 12
	eventSMChange          = 13
	eventClientReregister  = 17
	eventGIDChange         = 18
	eventDeviceSpeedChange = 20
)

// rtnetlink layouts from include/uapi/linux/rtnetlink.h and if_addr.h, and the
// link flags from include/uapi/linux/if.h: the translator reads only the fixed
// headers, never an attribute.
const (
	ifinfomsgLen      = 16
	ifinfomsgOffIndex = 4
	ifinfomsgOffFlags = 8
	ifaddrmsgLen      = 8
	ifaddrmsgOffIndex = 4
	rtmNewLink        = 16
	rtmDelLink        = 17
	rtmNewAddr        = 20
	rtmDelAddr        = 21
	iffRunning        = 0x40
	iffLowerUp        = 0x10000
	rtnlGroupLink     = 1
	rtnlGroupIPv4Addr = 5
	rtnlGroupIPv6Addr = 9
)
