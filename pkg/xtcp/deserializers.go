package xtcp

import (
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
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
	{dsKeyMemInfo, xtcpnl.MemInfoEmumValueCst, xtcpnl.DeserializeMemInfoXTCP},         // 1  MEMINFO
	{dsKeyInfo, xtcpnl.TCPInfoEmumValueCst, xtcpnl.DeserializeTCPInfoXTCP},            // 2  INFO
	{dsKeyVegas, xtcpnl.VegasInfoEnumValueCst, xtcpnl.DeserializeVegasInfoXTCP},       // 3  VEGASINFO
	{dsKeyCong, xtcpnl.CongInfoEmumValueCst, xtcpnl.DeserializeCongInfoXTCP},          // 4  CONG
	{dsKeyTos, xtcpnl.TypeOfServiceEmumValueCst, xtcpnl.DeserializeTypeOfServiceXTCP}, // 5  TOS
	{dsKeyTc, xtcpnl.TrafficClassEmumValueCst, xtcpnl.DeserializeTrafficClassXTCP},    // 6  TCLASS
	{dsKeySkmem, xtcpnl.SkMemInfoEnumValueCst, xtcpnl.DeserializeSkMemInfoXTCP},       // 7  SKMEMINFO
	{dsKeyShut, xtcpnl.ShutdownEmumValueCst, xtcpnl.DeserializeShutdownXTCP},          // 8  SHUTDOWN
	{dsKeyDctcp, xtcpnl.DCTCPInfoEnumValueCst, xtcpnl.DeserializeDCTCPInfoXTCP},       // 9  DCTCPINFO
	{dsKeyBbr, xtcpnl.BBRInfoEnumValueCst, xtcpnl.DeserializeBBRInfoXTCP},             // 16 BBRINFO
	{dsKeyClassID, xtcpnl.ClassIDEnumValueCst, xtcpnl.DeserializeClassIDXTCP},         // 17 CLASS_ID
	{dsKeyCgroup, xtcpnl.CGroupIDEnumValueCst, xtcpnl.DeserializeCGroupIDXTCP},        // 21 CGROUP_ID
	{dsKeySockopt, xtcpnl.SockOptEnumValueCst, xtcpnl.DeserializeSockOptXTCP},         // 22 SOCKOPT (bug 39: was incorrectly DeserializeCGroupIDXTCP before)
}

func GetAllDeserializers() (deserializers []string) {
	deserializers = make([]string, 0, len(dispatchTable))
	for _, e := range dispatchTable {
		deserializers = append(deserializers, e.key)
	}
	return deserializers
}

// deserializersOffByDefault lists supported deserializers that are NOT enabled
// unless the operator asks for them explicitly ("-deserializers all", or by
// naming them). meminfo duplicates sk_mem_info value-for-value (rmem→rmem_alloc,
// wmem→wmem_queued, fmem→fwd_alloc, tmem→wmem_alloc), so by default we neither
// request it from the kernel nor decode it — the same data is in sk_mem_info.
var deserializersOffByDefault = map[string]bool{
	dsKeyMemInfo: true,
}

// GetDefaultDeserializers returns every dispatchTable key except those in
// deserializersOffByDefault. This is the set enabled by "-deserializers default".
func GetDefaultDeserializers() (deserializers []string) {
	deserializers = make([]string, 0, len(dispatchTable))
	for _, e := range dispatchTable {
		if deserializersOffByDefault[e.key] {
			continue
		}
		deserializers = append(deserializers, e.key)
	}
	return deserializers
}

// IDiagExtFromEnabled derives the inet_diag request extension bitmask
// (inet_diag_req_v2.idiag_ext) from the enabled deserializers, so the daemon
// requests exactly the optional attributes it will actually parse — instead of
// the historical hardcoded 127. Only extensions 1..8 map to a bit in the uint8
// idiag_ext (bit N-1); higher-numbered attributes cannot be addressed directly.
// A bit is set iff that extension's deserializer is enabled — plus the
// congestion-control coupling described below.
//
// Congestion-control private state (VEGASINFO=3, DCTCPINFO=9, BBRINFO=16) is all
// gated by ONE request bit — the VEGASINFO bit (ext 3, bit 2). The kernel calls
// only the socket's own congestion module's get_info(sk, ext, …) and returns at
// most one cc-info struct per socket (whichever matches its algorithm, or none
// for cubic). Each module keys off the VEGASINFO bit: see net/ipv4/tcp_bbr.c
// (bbr_get_info), net/ipv4/tcp_dctcp.c (dctcp_get_info), net/ipv4/tcp_vegas.c
// (tcp_vegas_get_info) — and uapi/linux/inet_diag.h documents DCTCPINFO/BBRINFO
// as "request as INET_DIAG_VEGASINFO". Because dctcp(9)/bbr(16) exceed the 8-bit
// idiag_ext, the plain enum-bit loop would set no bit for them: enabling `bbr`
// or `dctcp` (but not `vegas`) would silently return nothing. So we set the
// VEGASINFO bit whenever ANY of vegas/dctcp/bbr is enabled. (`cong`, ext 4, is
// the algorithm NAME string and is a separate, independent bit.)
//
// Caveat, verified against net/ipv4/inet_diag.c (inet_diag_msg_attrs_fill): the
// kernel does NOT gate INET_DIAG_SHUTDOWN (ext 8) on idiag_ext — it emits it
// unconditionally — so setting bit 7 (which we do when `shut` is enabled) is a
// harmless no-op. CLASS_ID(17)/CGROUP_ID(21)/SOCKOPT(22) are likewise returned
// regardless. The genuinely gated, byte-saving bits are MEMINFO(1), INFO(2),
// VEGASINFO(3, and its cc-info family), CONG(4), TOS(5), TCLASS(6, IPv6-only) and
// SKMEMINFO(7). Dropping meminfo (bit 0) is what reduces the kernel→userland
// reply. The real kernel behavior is asserted by tools/idiag-extprobe in the
// microvm self-test.
func IDiagExtFromEnabled(enabled map[string]bool) uint8 {
	var ext uint8
	for _, e := range dispatchTable {
		if e.enum >= 1 && e.enum <= 8 && enabled[e.key] {
			ext |= uint8(1) << uint8(e.enum-1)
		}
	}
	// Congestion-control coupling: any of vegas/dctcp/bbr is requested via the
	// single VEGASINFO bit (ext 3). dctcp(9)/bbr(16) have no addressable bit of
	// their own, so without this a bbr-only or dctcp-only config would request
	// no cc-info at all.
	if enabled[dsKeyVegas] || enabled[dsKeyDctcp] || enabled[dsKeyBbr] {
		ext |= uint8(1) << uint8(xtcpnl.VegasInfoEnumValueCst-1)
	}
	return ext
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
		// Register only groups explicitly enabled (key present AND true). The
		// bool value is honored, not just key existence, so a config with
		// Enabled["bbr"]=false disables BBR — this is what lets a soft-restart
		// (ConfigService.Set) turn an attribute group off, not only on. The
		// CLI only ever writes true keys, so existing configs are unaffected.
		if enabled, exists := x.config.EnabledDeserializers.Enabled[e.key]; !exists || !enabled {
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
