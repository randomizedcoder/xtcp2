package rdmacaps

// Layouts from include/uapi/rdma/ib_user_mad.h and rdma_user_ioctl.h.
// TestABILayout compares every constant in this file with
// nix/tests/linkmonitor-abi.c output.
const (
	umadHdrLen             = 64
	umadHdrOffID           = 0
	umadHdrOffStatus       = 4
	umadHdrOffTimeout      = 8
	umadHdrOffRetries      = 12
	umadHdrOffLength       = 16
	umadHdrOffQPN          = 20
	umadHdrOffQKey         = 24
	umadHdrOffLID          = 28
	umadHdrOffSL           = 30
	umadHdrOffPathBits     = 31
	umadHdrOffGRHPresent   = 32
	umadHdrOffGIDIndex     = 33
	umadHdrOffHopLimit     = 34
	umadHdrOffTrafficClass = 35
	umadHdrOffGID          = 36
	umadHdrOffFlowLabel    = 52
	umadHdrOffPKeyIndex    = 56

	umadRegReq2Len           = 40
	umadRegReq2OffID         = 0
	umadRegReq2OffQPN        = 4
	umadRegReq2OffMgmtClass  = 8
	umadRegReq2OffVersion    = 9
	umadRegReq2OffRes        = 10
	umadRegReq2OffFlags      = 12
	umadRegReq2OffMethodMask = 16
	umadRegReq2OffOUI        = 32
	umadRegReq2OffRMPP       = 36

	umadRegisterAgent2  = 0xC0281B04
	umadUnregisterAgent = 0x40041B02
)
