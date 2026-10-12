package xtcpnl

// RDMA netlink (NETLINK_RDMA, protocol 20) nldev constants. x/sys/unix
// v0.47.0 names none of them. Values from include/uapi/rdma/rdma_netlink.h
// (flake-pinned linuxHeaders 7.0): RDMA_NL_NLDEV, RDMA_NL_GROUP_NOTIFY, enum
// rdma_nldev_command, enum rdma_nldev_attr, enum rdma_nl_counter_mode and
// enum rdma_nl_notify_event_type. testdata/rdma_consts.h asserts every one
// of them against that header (nix/tests/linkmonitor-rdma.nix), and
// TestRdmaConstsHeaderList keeps the list and this block in step.
//
// The UAPI enums are append-only, so a newer kernel (7.3 adds the FRMR_POOLS
// attributes from 105) agrees on every value here except the two bounds,
// RdmaNldevNumOps and RdmaNldevAttrMax, which are 7.0's. A linuxHeaders bump
// fails the C gate on those two, and that is the cue to declare what the new
// header adds.
const (
	RdmaNlNldev       uint16 = 5
	RdmaNlGroupNotify uint32 = 4
	// rdmaNlClientShiftCst is the 10 in RDMA_NL_GET_TYPE(client, op).
	rdmaNlClientShiftCst = 10
)

// RdmaNlGetType is RDMA_NL_GET_TYPE: the nlmsg_type of an RDMA netlink
// message, client in the top six bits and op in the low ten.
func RdmaNlGetType(client, op uint16) uint16 {
	return client<<rdmaNlClientShiftCst + op
}

// RdmaNlGetClient is RDMA_NL_GET_CLIENT.
func RdmaNlGetClient(t uint16) uint16 { return t >> rdmaNlClientShiftCst }

// RdmaNlGetOp is RDMA_NL_GET_OP.
func RdmaNlGetOp(t uint16) uint16 { return t & (1<<rdmaNlClientShiftCst - 1) }

// enum rdma_nldev_command
const (
	RdmaNldevCmdUnspec        uint16 = 0
	RdmaNldevCmdGet           uint16 = 1
	RdmaNldevCmdSet           uint16 = 2
	RdmaNldevCmdNewlink       uint16 = 3
	RdmaNldevCmdDellink       uint16 = 4
	RdmaNldevCmdPortGet       uint16 = 5
	RdmaNldevCmdSysGet        uint16 = 6
	RdmaNldevCmdSysSet        uint16 = 7
	RdmaNldevCmdResGet        uint16 = 9
	RdmaNldevCmdResQPGet      uint16 = 10
	RdmaNldevCmdResCMIDGet    uint16 = 11
	RdmaNldevCmdResCQGet      uint16 = 12
	RdmaNldevCmdResMRGet      uint16 = 13
	RdmaNldevCmdResPDGet      uint16 = 14
	RdmaNldevCmdGetChardev    uint16 = 15
	RdmaNldevCmdStatSet       uint16 = 16
	RdmaNldevCmdStatGet       uint16 = 17
	RdmaNldevCmdStatDel       uint16 = 18
	RdmaNldevCmdResQPGetRaw   uint16 = 19
	RdmaNldevCmdResCQGetRaw   uint16 = 20
	RdmaNldevCmdResMRGetRaw   uint16 = 21
	RdmaNldevCmdResCtxGet     uint16 = 22
	RdmaNldevCmdResSRQGet     uint16 = 23
	RdmaNldevCmdStatGetStatus uint16 = 24
	RdmaNldevCmdResSRQGetRaw  uint16 = 25
	RdmaNldevCmdNewdev        uint16 = 26
	RdmaNldevCmdDeldev        uint16 = 27
	RdmaNldevCmdMonitor       uint16 = 28
	RdmaNldevNumOps           uint16 = 29
)

// enum rdma_nldev_attr
const (
	RdmaNldevAttrUnspec                  uint16 = 0
	RdmaNldevAttrDevIndex                uint16 = 1
	RdmaNldevAttrDevName                 uint16 = 2
	RdmaNldevAttrPortIndex               uint16 = 3
	RdmaNldevAttrCapFlags                uint16 = 4
	RdmaNldevAttrFwVersion               uint16 = 5
	RdmaNldevAttrNodeGUID                uint16 = 6
	RdmaNldevAttrSysImageGUID            uint16 = 7
	RdmaNldevAttrSubnetPrefix            uint16 = 8
	RdmaNldevAttrLID                     uint16 = 9
	RdmaNldevAttrSMLID                   uint16 = 10
	RdmaNldevAttrLMC                     uint16 = 11
	RdmaNldevAttrPortState               uint16 = 12
	RdmaNldevAttrPortPhysState           uint16 = 13
	RdmaNldevAttrDevNodeType             uint16 = 14
	RdmaNldevAttrResSummary              uint16 = 15
	RdmaNldevAttrResSummaryEntry         uint16 = 16
	RdmaNldevAttrResSummaryEntryName     uint16 = 17
	RdmaNldevAttrResSummaryEntryCurr     uint16 = 18
	RdmaNldevAttrResQP                   uint16 = 19
	RdmaNldevAttrResQPEntry              uint16 = 20
	RdmaNldevAttrResLQPN                 uint16 = 21
	RdmaNldevAttrResRQPN                 uint16 = 22
	RdmaNldevAttrResRqPSN                uint16 = 23
	RdmaNldevAttrResSqPSN                uint16 = 24
	RdmaNldevAttrResPathMigState         uint16 = 25
	RdmaNldevAttrResType                 uint16 = 26
	RdmaNldevAttrResState                uint16 = 27
	RdmaNldevAttrResPID                  uint16 = 28
	RdmaNldevAttrResKernName             uint16 = 29
	RdmaNldevAttrResCMID                 uint16 = 30
	RdmaNldevAttrResCMIDEntry            uint16 = 31
	RdmaNldevAttrResPS                   uint16 = 32
	RdmaNldevAttrResSrcAddr              uint16 = 33
	RdmaNldevAttrResDstAddr              uint16 = 34
	RdmaNldevAttrResCQ                   uint16 = 35
	RdmaNldevAttrResCQEntry              uint16 = 36
	RdmaNldevAttrResCQE                  uint16 = 37
	RdmaNldevAttrResUsecnt               uint16 = 38
	RdmaNldevAttrResPollCtx              uint16 = 39
	RdmaNldevAttrResMR                   uint16 = 40
	RdmaNldevAttrResMREntry              uint16 = 41
	RdmaNldevAttrResRKey                 uint16 = 42
	RdmaNldevAttrResLKey                 uint16 = 43
	RdmaNldevAttrResIOVA                 uint16 = 44
	RdmaNldevAttrResMrlen                uint16 = 45
	RdmaNldevAttrResPD                   uint16 = 46
	RdmaNldevAttrResPDEntry              uint16 = 47
	RdmaNldevAttrResLocalDMALKey         uint16 = 48
	RdmaNldevAttrResUnsafeGlobalRKey     uint16 = 49
	RdmaNldevAttrNdevIndex               uint16 = 50
	RdmaNldevAttrNdevName                uint16 = 51
	RdmaNldevAttrDriver                  uint16 = 52
	RdmaNldevAttrDriverEntry             uint16 = 53
	RdmaNldevAttrDriverString            uint16 = 54
	RdmaNldevAttrDriverPrintType         uint16 = 55
	RdmaNldevAttrDriverS32               uint16 = 56
	RdmaNldevAttrDriverU32               uint16 = 57
	RdmaNldevAttrDriverS64               uint16 = 58
	RdmaNldevAttrDriverU64               uint16 = 59
	RdmaNldevAttrResPDN                  uint16 = 60
	RdmaNldevAttrResCQN                  uint16 = 61
	RdmaNldevAttrResMRN                  uint16 = 62
	RdmaNldevAttrResCMIDN                uint16 = 63
	RdmaNldevAttrResCtxn                 uint16 = 64
	RdmaNldevAttrLinkType                uint16 = 65
	RdmaNldevSysAttrNetnsMode            uint16 = 66
	RdmaNldevAttrDevProtocol             uint16 = 67
	RdmaNldevNetNsFD                     uint16 = 68
	RdmaNldevAttrChardevType             uint16 = 69
	RdmaNldevAttrChardevName             uint16 = 70
	RdmaNldevAttrChardevABI              uint16 = 71
	RdmaNldevAttrChardev                 uint16 = 72
	RdmaNldevAttrUverbsDriverID          uint16 = 73
	RdmaNldevAttrStatMode                uint16 = 74
	RdmaNldevAttrStatRes                 uint16 = 75
	RdmaNldevAttrStatAutoModeMask        uint16 = 76
	RdmaNldevAttrStatCounter             uint16 = 77
	RdmaNldevAttrStatCounterEntry        uint16 = 78
	RdmaNldevAttrStatCounterID           uint16 = 79
	RdmaNldevAttrStatHwcounters          uint16 = 80
	RdmaNldevAttrStatHwcounterEntry      uint16 = 81
	RdmaNldevAttrStatHwcounterEntryName  uint16 = 82
	RdmaNldevAttrStatHwcounterEntryValue uint16 = 83
	RdmaNldevAttrDevDIM                  uint16 = 84
	RdmaNldevAttrResRaw                  uint16 = 85
	RdmaNldevAttrResCtx                  uint16 = 86
	RdmaNldevAttrResCtxEntry             uint16 = 87
	RdmaNldevAttrResSRQ                  uint16 = 88
	RdmaNldevAttrResSRQEntry             uint16 = 89
	RdmaNldevAttrResSRQN                 uint16 = 90
	RdmaNldevAttrMinRange                uint16 = 91
	RdmaNldevAttrMaxRange                uint16 = 92
	RdmaNldevSysAttrCopyOnFork           uint16 = 93
	RdmaNldevAttrStatHwcounterIndex      uint16 = 94
	RdmaNldevAttrStatHwcounterDynamic    uint16 = 95
	RdmaNldevSysAttrPrivilegedQKeyMode   uint16 = 96
	RdmaNldevAttrDriverDetails           uint16 = 97
	RdmaNldevAttrResSubtype              uint16 = 98
	RdmaNldevAttrDevType                 uint16 = 99
	RdmaNldevAttrParentName              uint16 = 100
	RdmaNldevAttrNameAssignType          uint16 = 101
	RdmaNldevAttrEventType               uint16 = 102
	RdmaNldevSysAttrMonitorMode          uint16 = 103
	RdmaNldevAttrStatOpcounterEnabled    uint16 = 104
	RdmaNldevAttrMax                     uint16 = 105
)

// enum rdma_nl_counter_mode
const (
	RdmaCounterModeNone   uint8 = 0
	RdmaCounterModeAuto   uint8 = 1
	RdmaCounterModeManual uint8 = 2
	RdmaCounterModeMax    uint8 = 3
)

// enum rdma_nl_notify_event_type
const (
	RdmaRegisterEvent     uint8 = 0
	RdmaUnregisterEvent   uint8 = 1
	RdmaNetdevAttachEvent uint8 = 2
	RdmaNetdevDetachEvent uint8 = 3
	RdmaRenameEvent       uint8 = 4
	RdmaNetdevRenameEvent uint8 = 5
)

// The port states nldev reports in RDMA_NLDEV_ATTR_PORT_STATE and
// RDMA_NLDEV_ATTR_PORT_PHYS_STATE. Values from include/rdma/ib_verbs.h, enum
// ib_port_state and enum ib_port_phys_state; that header is not UAPI, so no C
// gate pins them, and iproute2's rdma/link.c spells out the same tables.
const (
	IbPortNop         uint8 = 0
	IbPortDown        uint8 = 1
	IbPortInit        uint8 = 2
	IbPortArmed       uint8 = 3
	IbPortActive      uint8 = 4
	IbPortActiveDefer uint8 = 5
)

const (
	IbPortPhysStateSleep                     uint8 = 1
	IbPortPhysStatePolling                   uint8 = 2
	IbPortPhysStateDisabled                  uint8 = 3
	IbPortPhysStatePortConfigurationTraining uint8 = 4
	IbPortPhysStateLinkUp                    uint8 = 5
	IbPortPhysStateLinkErrorRecovery         uint8 = 6
	IbPortPhysStatePhyTest                   uint8 = 7
)
