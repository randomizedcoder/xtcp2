package rdmacaps

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

var abiConstants = map[string]uint64{
	"umadHdrLen":               umadHdrLen,
	"umadHdrOffID":             umadHdrOffID,
	"umadHdrOffStatus":         umadHdrOffStatus,
	"umadHdrOffTimeout":        umadHdrOffTimeout,
	"umadHdrOffRetries":        umadHdrOffRetries,
	"umadHdrOffLength":         umadHdrOffLength,
	"umadHdrOffQPN":            umadHdrOffQPN,
	"umadHdrOffQKey":           umadHdrOffQKey,
	"umadHdrOffLID":            umadHdrOffLID,
	"umadHdrOffSL":             umadHdrOffSL,
	"umadHdrOffPathBits":       umadHdrOffPathBits,
	"umadHdrOffGRHPresent":     umadHdrOffGRHPresent,
	"umadHdrOffGIDIndex":       umadHdrOffGIDIndex,
	"umadHdrOffHopLimit":       umadHdrOffHopLimit,
	"umadHdrOffTrafficClass":   umadHdrOffTrafficClass,
	"umadHdrOffGID":            umadHdrOffGID,
	"umadHdrOffFlowLabel":      umadHdrOffFlowLabel,
	"umadHdrOffPKeyIndex":      umadHdrOffPKeyIndex,
	"umadRegReq2Len":           umadRegReq2Len,
	"umadRegReq2OffID":         umadRegReq2OffID,
	"umadRegReq2OffQPN":        umadRegReq2OffQPN,
	"umadRegReq2OffMgmtClass":  umadRegReq2OffMgmtClass,
	"umadRegReq2OffVersion":    umadRegReq2OffVersion,
	"umadRegReq2OffRes":        umadRegReq2OffRes,
	"umadRegReq2OffFlags":      umadRegReq2OffFlags,
	"umadRegReq2OffMethodMask": umadRegReq2OffMethodMask,
	"umadRegReq2OffOUI":        umadRegReq2OffOUI,
	"umadRegReq2OffRMPP":       umadRegReq2OffRMPP,
	"umadRegisterAgent2":       umadRegisterAgent2,
	"umadUnregisterAgent":      umadUnregisterAgent,
}

// TestABILayout is the umad half of the assessment's ABI layout guard table.
func TestABILayout(t *testing.T) {
	testkit.CheckABILayout(t, []string{"umadhdr.go"}, abiConstants, "rdmacaps", []testkit.ABIRow{
		{Name: "umad_hdr", Category: "positive", Description: "ib_user_mad_hdr size and every offset", Expected: "64 bytes; offsets as in C3", Prefixes: []string{"umadHdr"}},
		{Name: "reg_req2", Category: "positive", Description: "ib_user_mad_reg_req2 size, offsets and the REGISTER_AGENT2 and UNREGISTER_AGENT numbers", Expected: "40 bytes, 0xC0281B04, 0x40041B02", Prefixes: []string{"umadRegReq2", "umadRegisterAgent2", "umadUnregisterAgent"}},
		{Name: "reg_req_v1", Category: "negative", Description: "No Go file in the package declares or writes IB_USER_MAD_REGISTER_AGENT", Expected: "no use", Text: "forbidden"},
		{Name: "unmapped", Category: "corner", Description: "A constant in umadhdr.go missing from the comparison map", Expected: "none", Text: "comparison map"},
	})
}
