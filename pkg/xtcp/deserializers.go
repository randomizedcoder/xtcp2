package xtcp

import (
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	RTATypeDeserializerMapLengthCst = 25

	// Deserializer key strings. Each maps to one INET_DIAG_* attribute
	// type (see pkg/xtcpnl/*EnumValueCst). Lifted to consts so the
	// linter (goconst) stops complaining about repeated literals across
	// GetAllDeserializers + InitDeserializers — and so an operator can
	// grep for the canonical name once.
	dsKeyMemInfo = "meminfo"
	dsKeyInfo    = "info"
	dsKeyVegas   = "vegas"
	dsKeyCong    = "cong"
	dsKeyTos     = "tos"
	dsKeyTc      = "tc"
	dsKeySkmem   = "skmem"
	dsKeyShut    = "shut"
	dsKeyDctcp   = "dctcp"
	dsKeyBbr     = "bbr"
	dsKeyClassID = "classid"
	dsKeyCgroup  = "cgroup"
	dsKeySockopt = "sockopt"
)

// deserializerFunc is the signature every XTCP-record per-attribute
// deserializer satisfies. Sharing the type at package scope lets the
// dispatch table below stay concise.
type deserializerFunc = func(buf []byte, xtcpRecord *xtcp_flat_record.XtcpFlatRecord) (err error)

// deserializerEntry binds a CLI/config key (e.g. "meminfo") to the
// INET_DIAG_* enum value the kernel uses and the function that decodes
// that attribute's payload into XtcpFlatRecord. dispatchTable below is
// the single source of truth for both InitDeserializers (registration)
// and GetAllDeserializers (key enumeration); the previous implementation
// repeated the same {key, enum, func} triple 13× with a separate
// `if _, exists := ...; exists { ... }` block each — gocyclo 17 +
// hard to extend.
type deserializerEntry struct {
	key  string
	enum int
	fn   deserializerFunc
}

// dispatchTable lists every supported INET_DIAG attribute in kernel-enum
// order. The kernel comment column ("// INET_DIAG_X N") that used to
// punctuate the registration code lives in the trailing comment per row.
// Keep the order matching the kernel header for grep-ability.
var dispatchTable = []deserializerEntry{
	{dsKeyMemInfo, xtcpnl.MemInfoEmumValueCst, xtcpnl.DeserializeMemInfoXTCP},               // 1  MEMINFO
	{dsKeyInfo, xtcpnl.TCPInfoEmumValueCst, xtcpnl.DeserializeTCPInfoXTCP},                  // 2  INFO
	{dsKeyVegas, xtcpnl.VegasInfoEnumValueCst, xtcpnl.DeserializeVegasInfoXTCP},             // 3  VEGASINFO
	{dsKeyCong, xtcpnl.CongInfoEmumValueCst, xtcpnl.DeserializeCongInfoXTCP},                // 4  CONG
	{dsKeyTos, xtcpnl.TypeOfServiceEmumValueCst, xtcpnl.DeserializeTypeOfServiceXTCP},       // 5  TOS
	{dsKeyTc, xtcpnl.TrafficClassEmumValueCst, xtcpnl.DeserializeTrafficClassXTCP},          // 6  TCLASS
	{dsKeySkmem, xtcpnl.SkMemInfoEnumValueCst, xtcpnl.DeserializeSkMemInfoXTCP},             // 7  SKMEMINFO
	{dsKeyShut, xtcpnl.ShutdownEmumValueCst, xtcpnl.DeserializeShutdownXTCP},                // 8  SHUTDOWN
	{dsKeyDctcp, xtcpnl.DCTCPInfoEnumValueCst, xtcpnl.DeserializeDCTCPInfoXTCP},             // 9  DCTCPINFO
	{dsKeyBbr, xtcpnl.BBRInfoEnumValueCst, xtcpnl.DeserializeBBRInfoXTCP},                   // 16 BBRINFO
	{dsKeyClassID, xtcpnl.ClassIDEnumValueCst, xtcpnl.DeserializeClassIDXTCP},               // 17 CLASS_ID
	{dsKeyCgroup, xtcpnl.CGroupIDEnumValueCst, xtcpnl.DeserializeCGroupIDXTCP},              // 21 CGROUP_ID
	{dsKeySockopt, xtcpnl.SockOptEnumValueCst, xtcpnl.DeserializeSockOptXTCP},               // 22 SOCKOPT (bug 39: was incorrectly DeserializeCGroupIDXTCP before)
}

func GetAllDeserializers() (deserializers []string) {
	deserializers = make([]string, 0, len(dispatchTable))
	for _, e := range dispatchTable {
		deserializers = append(deserializers, e.key)
	}
	return deserializers
}

// InitDeserializers populates x.RTATypeDeserializer + x.RTATypeDeserializerStr
// with each entry from dispatchTable whose key is enabled in
// x.config.EnabledDeserializers.Enabled. The 13-block repetitive
// registration was extracted into a single table walk — gocyclo 17 → 5.
func (x *XTCP) InitDeserializers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.RTATypeDeserializer = make(map[int]deserializerFunc, RTATypeDeserializerMapLengthCst)
	x.RTATypeDeserializerStr = make(map[int]string, RTATypeDeserializerMapLengthCst)

	// Defensive against tests that build XtcpConfig{} manually without
	// setting EnabledDeserializers; the per-entry Enabled[key] lookups
	// below would otherwise nil-deref. Both production constructors
	// (NewXTCP / NewNsTestingXTCP) set the field, but a fresh XTCP{}
	// fixture is easy to slip through (bug 77).
	if x.config.EnabledDeserializers == nil {
		return
	}

	for _, e := range dispatchTable {
		if _, exists := x.config.EnabledDeserializers.Enabled[e.key]; !exists {
			continue
		}
		x.RTATypeDeserializer[e.enum] = e.fn
		x.RTATypeDeserializerStr[e.enum] = e.key
	}

	if x.debugLevel > 10 {
		for k := range x.RTATypeDeserializerStr {
			log.Printf("RTATypeDeserializerStr k:%d %s", k, x.RTATypeDeserializerStr[k])
		}
	}
}
