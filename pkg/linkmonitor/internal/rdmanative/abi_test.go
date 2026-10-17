package rdmanative

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

var abiConstants = map[string]uint64{
	"asyncEventDescLen":               asyncEventDescLen,
	"asyncEventDescOffElement":        asyncEventDescOffElement,
	"asyncEventDescOffType":           asyncEventDescOffType,
	"cmdHdrLen":                       cmdHdrLen,
	"cmdHdrOffCommand":                cmdHdrOffCommand,
	"cmdHdrOffInWords":                cmdHdrOffInWords,
	"cmdHdrOffOutWords":               cmdHdrOffOutWords,
	"cmdGetContext":                   cmdGetContext,
	"getContextLen":                   getContextLen,
	"getContextOffResponse":           getContextOffResponse,
	"getContextRespLen":               getContextRespLen,
	"getContextRespOffAsyncFD":        getContextRespOffAsyncFD,
	"getContextRespOffNumCompVectors": getContextRespOffNumCompVectors,
	"eventDeviceFatal":                eventDeviceFatal,
	"eventPortActive":                 eventPortActive,
	"eventPortErr":                    eventPortErr,
	"eventLIDChange":                  eventLIDChange,
	"eventPKeyChange":                 eventPKeyChange,
	"eventSMChange":                   eventSMChange,
	"eventClientReregister":           eventClientReregister,
	"eventGIDChange":                  eventGIDChange,
	"eventDeviceSpeedChange":          eventDeviceSpeedChange,
}

// TestABILayout is the uverbs half of the assessment's ABI layout guard table.
func TestABILayout(t *testing.T) {
	testkit.CheckABILayout(t, []string{"abi.go"}, abiConstants, "rdmanative", []testkit.ABIRow{
		{Name: "async_desc", Category: "positive", Description: "ib_uverbs_async_event_desc size and offsets", Expected: "16 bytes; element at 0, type at 8", Prefixes: []string{"asyncEventDesc"}},
		{Name: "get_context", Category: "positive", Description: "ib_uverbs_cmd_hdr, get_context and get_context_resp", Expected: "8, 8, 8 bytes; GET_CONTEXT is command 0", Prefixes: []string{"cmd", "getContext"}},
		{Name: "event_enum", Category: "boundary", Description: "DEVICE_FATAL, PORT_ACTIVE, GID_CHANGE, DEVICE_SPEED_CHANGE and the other mapped events", Expected: "8, 9, 18, 20", Prefixes: []string{"event"}},
		{Name: "unmapped", Category: "corner", Description: "A constant in abi.go missing from the comparison map", Expected: "none", Text: "comparison map"},
	})
}
