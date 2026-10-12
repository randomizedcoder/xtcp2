package xtcpnl

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"testing"
)

func TestRdmaConsts(t *testing.T) {
	tests := []struct {
		description string
		got         uint16
		expected    uint16
	}{
		{description: "positive: GET type", got: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGet), expected: 0x1401},
		{description: "positive: PORT_GET type", got: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdPortGet), expected: 0x1405},
		{description: "positive: STAT_GET type", got: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdStatGet), expected: 0x1411},
		{description: "positive: MONITOR type", got: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdMonitor), expected: 0x141c},
		{description: "boundary: the last command", got: RdmaNldevNumOps - 1, expected: RdmaNldevCmdMonitor},
		{description: "boundary: op 0", got: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdUnspec), expected: 0x1400},
		{description: "boundary: the client of a GET type", got: RdmaNlGetClient(0x1401), expected: RdmaNlNldev},
		{description: "boundary: the op of a MONITOR type", got: RdmaNlGetOp(0x141c), expected: RdmaNldevCmdMonitor},
		{description: "boundary: the op is the low ten bits", got: RdmaNlGetOp(0xffff), expected: 0x3ff},
		// The bare numbers pkg/linkmonitor/internal/linuxio uses today.
		{description: "corner: linkmonitor's device get", got: 5<<10 | 1, expected: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdGet)},
		{description: "corner: linkmonitor's port get", got: 5<<10 | 5, expected: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdPortGet)},
		{description: "corner: linkmonitor's monitor", got: 5<<10 | 28, expected: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdMonitor)},
		{description: "corner: linkmonitor's system get", got: 5<<10 | 6, expected: RdmaNlGetType(RdmaNlNldev, RdmaNldevCmdSysGet)},
		{description: "corner: linkmonitor's event attribute", got: 102, expected: RdmaNldevAttrEventType},
		{description: "corner: linkmonitor's monitor mode attribute", got: 103, expected: RdmaNldevSysAttrMonitorMode},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("got %#x, want %#x", tc.got, tc.expected)
			}
		})
	}
}

// rdmaConstsByHeaderName is the Go block keyed by the header's name for
// each constant: the list testdata/rdma_consts.h asserts against the kernel
// header has to be exactly this set.
var rdmaConstsByHeaderName = map[string]uint64{
	"RDMA_NL_NLDEV":                              uint64(RdmaNlNldev),
	"RDMA_NL_GROUP_NOTIFY":                       uint64(RdmaNlGroupNotify),
	"RDMA_NLDEV_CMD_UNSPEC":                      uint64(RdmaNldevCmdUnspec),
	"RDMA_NLDEV_CMD_GET":                         uint64(RdmaNldevCmdGet),
	"RDMA_NLDEV_CMD_SET":                         uint64(RdmaNldevCmdSet),
	"RDMA_NLDEV_CMD_NEWLINK":                     uint64(RdmaNldevCmdNewlink),
	"RDMA_NLDEV_CMD_DELLINK":                     uint64(RdmaNldevCmdDellink),
	"RDMA_NLDEV_CMD_PORT_GET":                    uint64(RdmaNldevCmdPortGet),
	"RDMA_NLDEV_CMD_SYS_GET":                     uint64(RdmaNldevCmdSysGet),
	"RDMA_NLDEV_CMD_SYS_SET":                     uint64(RdmaNldevCmdSysSet),
	"RDMA_NLDEV_CMD_RES_GET":                     uint64(RdmaNldevCmdResGet),
	"RDMA_NLDEV_CMD_RES_QP_GET":                  uint64(RdmaNldevCmdResQPGet),
	"RDMA_NLDEV_CMD_RES_CM_ID_GET":               uint64(RdmaNldevCmdResCMIDGet),
	"RDMA_NLDEV_CMD_RES_CQ_GET":                  uint64(RdmaNldevCmdResCQGet),
	"RDMA_NLDEV_CMD_RES_MR_GET":                  uint64(RdmaNldevCmdResMRGet),
	"RDMA_NLDEV_CMD_RES_PD_GET":                  uint64(RdmaNldevCmdResPDGet),
	"RDMA_NLDEV_CMD_GET_CHARDEV":                 uint64(RdmaNldevCmdGetChardev),
	"RDMA_NLDEV_CMD_STAT_SET":                    uint64(RdmaNldevCmdStatSet),
	"RDMA_NLDEV_CMD_STAT_GET":                    uint64(RdmaNldevCmdStatGet),
	"RDMA_NLDEV_CMD_STAT_DEL":                    uint64(RdmaNldevCmdStatDel),
	"RDMA_NLDEV_CMD_RES_QP_GET_RAW":              uint64(RdmaNldevCmdResQPGetRaw),
	"RDMA_NLDEV_CMD_RES_CQ_GET_RAW":              uint64(RdmaNldevCmdResCQGetRaw),
	"RDMA_NLDEV_CMD_RES_MR_GET_RAW":              uint64(RdmaNldevCmdResMRGetRaw),
	"RDMA_NLDEV_CMD_RES_CTX_GET":                 uint64(RdmaNldevCmdResCtxGet),
	"RDMA_NLDEV_CMD_RES_SRQ_GET":                 uint64(RdmaNldevCmdResSRQGet),
	"RDMA_NLDEV_CMD_STAT_GET_STATUS":             uint64(RdmaNldevCmdStatGetStatus),
	"RDMA_NLDEV_CMD_RES_SRQ_GET_RAW":             uint64(RdmaNldevCmdResSRQGetRaw),
	"RDMA_NLDEV_CMD_NEWDEV":                      uint64(RdmaNldevCmdNewdev),
	"RDMA_NLDEV_CMD_DELDEV":                      uint64(RdmaNldevCmdDeldev),
	"RDMA_NLDEV_CMD_MONITOR":                     uint64(RdmaNldevCmdMonitor),
	"RDMA_NLDEV_NUM_OPS":                         uint64(RdmaNldevNumOps),
	"RDMA_NLDEV_ATTR_UNSPEC":                     uint64(RdmaNldevAttrUnspec),
	"RDMA_NLDEV_ATTR_DEV_INDEX":                  uint64(RdmaNldevAttrDevIndex),
	"RDMA_NLDEV_ATTR_DEV_NAME":                   uint64(RdmaNldevAttrDevName),
	"RDMA_NLDEV_ATTR_PORT_INDEX":                 uint64(RdmaNldevAttrPortIndex),
	"RDMA_NLDEV_ATTR_CAP_FLAGS":                  uint64(RdmaNldevAttrCapFlags),
	"RDMA_NLDEV_ATTR_FW_VERSION":                 uint64(RdmaNldevAttrFwVersion),
	"RDMA_NLDEV_ATTR_NODE_GUID":                  uint64(RdmaNldevAttrNodeGUID),
	"RDMA_NLDEV_ATTR_SYS_IMAGE_GUID":             uint64(RdmaNldevAttrSysImageGUID),
	"RDMA_NLDEV_ATTR_SUBNET_PREFIX":              uint64(RdmaNldevAttrSubnetPrefix),
	"RDMA_NLDEV_ATTR_LID":                        uint64(RdmaNldevAttrLID),
	"RDMA_NLDEV_ATTR_SM_LID":                     uint64(RdmaNldevAttrSMLID),
	"RDMA_NLDEV_ATTR_LMC":                        uint64(RdmaNldevAttrLMC),
	"RDMA_NLDEV_ATTR_PORT_STATE":                 uint64(RdmaNldevAttrPortState),
	"RDMA_NLDEV_ATTR_PORT_PHYS_STATE":            uint64(RdmaNldevAttrPortPhysState),
	"RDMA_NLDEV_ATTR_DEV_NODE_TYPE":              uint64(RdmaNldevAttrDevNodeType),
	"RDMA_NLDEV_ATTR_RES_SUMMARY":                uint64(RdmaNldevAttrResSummary),
	"RDMA_NLDEV_ATTR_RES_SUMMARY_ENTRY":          uint64(RdmaNldevAttrResSummaryEntry),
	"RDMA_NLDEV_ATTR_RES_SUMMARY_ENTRY_NAME":     uint64(RdmaNldevAttrResSummaryEntryName),
	"RDMA_NLDEV_ATTR_RES_SUMMARY_ENTRY_CURR":     uint64(RdmaNldevAttrResSummaryEntryCurr),
	"RDMA_NLDEV_ATTR_RES_QP":                     uint64(RdmaNldevAttrResQP),
	"RDMA_NLDEV_ATTR_RES_QP_ENTRY":               uint64(RdmaNldevAttrResQPEntry),
	"RDMA_NLDEV_ATTR_RES_LQPN":                   uint64(RdmaNldevAttrResLQPN),
	"RDMA_NLDEV_ATTR_RES_RQPN":                   uint64(RdmaNldevAttrResRQPN),
	"RDMA_NLDEV_ATTR_RES_RQ_PSN":                 uint64(RdmaNldevAttrResRqPSN),
	"RDMA_NLDEV_ATTR_RES_SQ_PSN":                 uint64(RdmaNldevAttrResSqPSN),
	"RDMA_NLDEV_ATTR_RES_PATH_MIG_STATE":         uint64(RdmaNldevAttrResPathMigState),
	"RDMA_NLDEV_ATTR_RES_TYPE":                   uint64(RdmaNldevAttrResType),
	"RDMA_NLDEV_ATTR_RES_STATE":                  uint64(RdmaNldevAttrResState),
	"RDMA_NLDEV_ATTR_RES_PID":                    uint64(RdmaNldevAttrResPID),
	"RDMA_NLDEV_ATTR_RES_KERN_NAME":              uint64(RdmaNldevAttrResKernName),
	"RDMA_NLDEV_ATTR_RES_CM_ID":                  uint64(RdmaNldevAttrResCMID),
	"RDMA_NLDEV_ATTR_RES_CM_ID_ENTRY":            uint64(RdmaNldevAttrResCMIDEntry),
	"RDMA_NLDEV_ATTR_RES_PS":                     uint64(RdmaNldevAttrResPS),
	"RDMA_NLDEV_ATTR_RES_SRC_ADDR":               uint64(RdmaNldevAttrResSrcAddr),
	"RDMA_NLDEV_ATTR_RES_DST_ADDR":               uint64(RdmaNldevAttrResDstAddr),
	"RDMA_NLDEV_ATTR_RES_CQ":                     uint64(RdmaNldevAttrResCQ),
	"RDMA_NLDEV_ATTR_RES_CQ_ENTRY":               uint64(RdmaNldevAttrResCQEntry),
	"RDMA_NLDEV_ATTR_RES_CQE":                    uint64(RdmaNldevAttrResCQE),
	"RDMA_NLDEV_ATTR_RES_USECNT":                 uint64(RdmaNldevAttrResUsecnt),
	"RDMA_NLDEV_ATTR_RES_POLL_CTX":               uint64(RdmaNldevAttrResPollCtx),
	"RDMA_NLDEV_ATTR_RES_MR":                     uint64(RdmaNldevAttrResMR),
	"RDMA_NLDEV_ATTR_RES_MR_ENTRY":               uint64(RdmaNldevAttrResMREntry),
	"RDMA_NLDEV_ATTR_RES_RKEY":                   uint64(RdmaNldevAttrResRKey),
	"RDMA_NLDEV_ATTR_RES_LKEY":                   uint64(RdmaNldevAttrResLKey),
	"RDMA_NLDEV_ATTR_RES_IOVA":                   uint64(RdmaNldevAttrResIOVA),
	"RDMA_NLDEV_ATTR_RES_MRLEN":                  uint64(RdmaNldevAttrResMrlen),
	"RDMA_NLDEV_ATTR_RES_PD":                     uint64(RdmaNldevAttrResPD),
	"RDMA_NLDEV_ATTR_RES_PD_ENTRY":               uint64(RdmaNldevAttrResPDEntry),
	"RDMA_NLDEV_ATTR_RES_LOCAL_DMA_LKEY":         uint64(RdmaNldevAttrResLocalDMALKey),
	"RDMA_NLDEV_ATTR_RES_UNSAFE_GLOBAL_RKEY":     uint64(RdmaNldevAttrResUnsafeGlobalRKey),
	"RDMA_NLDEV_ATTR_NDEV_INDEX":                 uint64(RdmaNldevAttrNdevIndex),
	"RDMA_NLDEV_ATTR_NDEV_NAME":                  uint64(RdmaNldevAttrNdevName),
	"RDMA_NLDEV_ATTR_DRIVER":                     uint64(RdmaNldevAttrDriver),
	"RDMA_NLDEV_ATTR_DRIVER_ENTRY":               uint64(RdmaNldevAttrDriverEntry),
	"RDMA_NLDEV_ATTR_DRIVER_STRING":              uint64(RdmaNldevAttrDriverString),
	"RDMA_NLDEV_ATTR_DRIVER_PRINT_TYPE":          uint64(RdmaNldevAttrDriverPrintType),
	"RDMA_NLDEV_ATTR_DRIVER_S32":                 uint64(RdmaNldevAttrDriverS32),
	"RDMA_NLDEV_ATTR_DRIVER_U32":                 uint64(RdmaNldevAttrDriverU32),
	"RDMA_NLDEV_ATTR_DRIVER_S64":                 uint64(RdmaNldevAttrDriverS64),
	"RDMA_NLDEV_ATTR_DRIVER_U64":                 uint64(RdmaNldevAttrDriverU64),
	"RDMA_NLDEV_ATTR_RES_PDN":                    uint64(RdmaNldevAttrResPDN),
	"RDMA_NLDEV_ATTR_RES_CQN":                    uint64(RdmaNldevAttrResCQN),
	"RDMA_NLDEV_ATTR_RES_MRN":                    uint64(RdmaNldevAttrResMRN),
	"RDMA_NLDEV_ATTR_RES_CM_IDN":                 uint64(RdmaNldevAttrResCMIDN),
	"RDMA_NLDEV_ATTR_RES_CTXN":                   uint64(RdmaNldevAttrResCtxn),
	"RDMA_NLDEV_ATTR_LINK_TYPE":                  uint64(RdmaNldevAttrLinkType),
	"RDMA_NLDEV_SYS_ATTR_NETNS_MODE":             uint64(RdmaNldevSysAttrNetnsMode),
	"RDMA_NLDEV_ATTR_DEV_PROTOCOL":               uint64(RdmaNldevAttrDevProtocol),
	"RDMA_NLDEV_NET_NS_FD":                       uint64(RdmaNldevNetNsFD),
	"RDMA_NLDEV_ATTR_CHARDEV_TYPE":               uint64(RdmaNldevAttrChardevType),
	"RDMA_NLDEV_ATTR_CHARDEV_NAME":               uint64(RdmaNldevAttrChardevName),
	"RDMA_NLDEV_ATTR_CHARDEV_ABI":                uint64(RdmaNldevAttrChardevABI),
	"RDMA_NLDEV_ATTR_CHARDEV":                    uint64(RdmaNldevAttrChardev),
	"RDMA_NLDEV_ATTR_UVERBS_DRIVER_ID":           uint64(RdmaNldevAttrUverbsDriverID),
	"RDMA_NLDEV_ATTR_STAT_MODE":                  uint64(RdmaNldevAttrStatMode),
	"RDMA_NLDEV_ATTR_STAT_RES":                   uint64(RdmaNldevAttrStatRes),
	"RDMA_NLDEV_ATTR_STAT_AUTO_MODE_MASK":        uint64(RdmaNldevAttrStatAutoModeMask),
	"RDMA_NLDEV_ATTR_STAT_COUNTER":               uint64(RdmaNldevAttrStatCounter),
	"RDMA_NLDEV_ATTR_STAT_COUNTER_ENTRY":         uint64(RdmaNldevAttrStatCounterEntry),
	"RDMA_NLDEV_ATTR_STAT_COUNTER_ID":            uint64(RdmaNldevAttrStatCounterID),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTERS":            uint64(RdmaNldevAttrStatHwcounters),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTER_ENTRY":       uint64(RdmaNldevAttrStatHwcounterEntry),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTER_ENTRY_NAME":  uint64(RdmaNldevAttrStatHwcounterEntryName),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTER_ENTRY_VALUE": uint64(RdmaNldevAttrStatHwcounterEntryValue),
	"RDMA_NLDEV_ATTR_DEV_DIM":                    uint64(RdmaNldevAttrDevDIM),
	"RDMA_NLDEV_ATTR_RES_RAW":                    uint64(RdmaNldevAttrResRaw),
	"RDMA_NLDEV_ATTR_RES_CTX":                    uint64(RdmaNldevAttrResCtx),
	"RDMA_NLDEV_ATTR_RES_CTX_ENTRY":              uint64(RdmaNldevAttrResCtxEntry),
	"RDMA_NLDEV_ATTR_RES_SRQ":                    uint64(RdmaNldevAttrResSRQ),
	"RDMA_NLDEV_ATTR_RES_SRQ_ENTRY":              uint64(RdmaNldevAttrResSRQEntry),
	"RDMA_NLDEV_ATTR_RES_SRQN":                   uint64(RdmaNldevAttrResSRQN),
	"RDMA_NLDEV_ATTR_MIN_RANGE":                  uint64(RdmaNldevAttrMinRange),
	"RDMA_NLDEV_ATTR_MAX_RANGE":                  uint64(RdmaNldevAttrMaxRange),
	"RDMA_NLDEV_SYS_ATTR_COPY_ON_FORK":           uint64(RdmaNldevSysAttrCopyOnFork),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTER_INDEX":       uint64(RdmaNldevAttrStatHwcounterIndex),
	"RDMA_NLDEV_ATTR_STAT_HWCOUNTER_DYNAMIC":     uint64(RdmaNldevAttrStatHwcounterDynamic),
	"RDMA_NLDEV_SYS_ATTR_PRIVILEGED_QKEY_MODE":   uint64(RdmaNldevSysAttrPrivilegedQKeyMode),
	"RDMA_NLDEV_ATTR_DRIVER_DETAILS":             uint64(RdmaNldevAttrDriverDetails),
	"RDMA_NLDEV_ATTR_RES_SUBTYPE":                uint64(RdmaNldevAttrResSubtype),
	"RDMA_NLDEV_ATTR_DEV_TYPE":                   uint64(RdmaNldevAttrDevType),
	"RDMA_NLDEV_ATTR_PARENT_NAME":                uint64(RdmaNldevAttrParentName),
	"RDMA_NLDEV_ATTR_NAME_ASSIGN_TYPE":           uint64(RdmaNldevAttrNameAssignType),
	"RDMA_NLDEV_ATTR_EVENT_TYPE":                 uint64(RdmaNldevAttrEventType),
	"RDMA_NLDEV_SYS_ATTR_MONITOR_MODE":           uint64(RdmaNldevSysAttrMonitorMode),
	"RDMA_NLDEV_ATTR_STAT_OPCOUNTER_ENABLED":     uint64(RdmaNldevAttrStatOpcounterEnabled),
	"RDMA_NLDEV_ATTR_MAX":                        uint64(RdmaNldevAttrMax),
	"RDMA_COUNTER_MODE_NONE":                     uint64(RdmaCounterModeNone),
	"RDMA_COUNTER_MODE_AUTO":                     uint64(RdmaCounterModeAuto),
	"RDMA_COUNTER_MODE_MANUAL":                   uint64(RdmaCounterModeManual),
	"RDMA_COUNTER_MODE_MAX":                      uint64(RdmaCounterModeMax),
	"RDMA_REGISTER_EVENT":                        uint64(RdmaRegisterEvent),
	"RDMA_UNREGISTER_EVENT":                      uint64(RdmaUnregisterEvent),
	"RDMA_NETDEV_ATTACH_EVENT":                   uint64(RdmaNetdevAttachEvent),
	"RDMA_NETDEV_DETACH_EVENT":                   uint64(RdmaNetdevDetachEvent),
	"RDMA_RENAME_EVENT":                          uint64(RdmaRenameEvent),
	"RDMA_NETDEV_RENAME_EVENT":                   uint64(RdmaNetdevRenameEvent),
}

var rdmaStaticAssertRe = regexp.MustCompile(`^_Static_assert\(([A-Z0-9_]+) == ([0-9]+), "([A-Z0-9_]+)"\);$`)

// TestRdmaConstsHeaderList ties the C gate to the Go block: every line of
// testdata/rdma_consts.h names one Go constant, with its value, and every Go
// constant has a line.
func TestRdmaConstsHeaderList(t *testing.T) {
	f, err := os.Open("testdata/rdma_consts.h")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	}()
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := rdmaStaticAssertRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if m[1] != m[3] {
			t.Errorf("%s: the message names %s", m[1], m[3])
		}
		if seen[m[1]] {
			t.Errorf("%s is asserted twice", m[1])
		}
		seen[m[1]] = true
		v, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		want, ok := rdmaConstsByHeaderName[m[1]]
		switch {
		case !ok:
			t.Errorf("%s is asserted but not declared in Go", m[1])
		case v != want:
			t.Errorf("%s: the header list says %d, Go says %d", m[1], v, want)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	for name := range rdmaConstsByHeaderName {
		if !seen[name] {
			t.Errorf("%s is declared in Go but not asserted", name)
		}
	}
	if len(seen) != len(rdmaConstsByHeaderName) {
		t.Errorf("%d asserted, %d declared", len(seen), len(rdmaConstsByHeaderName))
	}
}
