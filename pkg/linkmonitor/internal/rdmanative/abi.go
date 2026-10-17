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
