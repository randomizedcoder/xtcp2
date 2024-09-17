package xtcp

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"github.com/randomizedcoder/xtcp2/pkg/xtcppb"
)

const (
	RTATypeDeserializerMapLengthCst = 25
)

func (x *XTCP) InitDeserializers() {

	x.RTATypeDeserializer = make(map[int]func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error), RTATypeDeserializerMapLengthCst)
	x.RTATypeDeserializerStr = make(map[int]string, RTATypeDeserializerMapLengthCst)

	//x.RTATypeDeserializer[0] = None

	// INET_DIAG_MEMINFO 1
	x.RTATypeDeserializer[xtcpnl.MemInfoEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeMemInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.MemInfoEmumValueCst] = "meminfo"

	// INET_DIAG_INFO 2
	x.RTATypeDeserializer[xtcpnl.TCPInfoEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeTCPInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.TCPInfoEmumValueCst] = "info"

	// INET_DIAG_VEGASINFO 3
	x.RTATypeDeserializer[xtcpnl.VegasInfoEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeVegasInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.VegasInfoEnumValueCst] = "vegas"

	// INET_DIAG_CONG 4
	x.RTATypeDeserializer[xtcpnl.CongInfoEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeCongInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.CongInfoEmumValueCst] = "cong"

	// INET_DIAG_TOS 5
	x.RTATypeDeserializer[xtcpnl.TypeOfServiceEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeTypeOfServiceXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.TypeOfServiceEmumValueCst] = "tos"

	// INET_DIAG_TCLASS 6
	x.RTATypeDeserializer[xtcpnl.TrafficClassEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeTrafficClassXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.TrafficClassEmumValueCst] = "tc"

	// INET_DIAG_SKMEMINFO 7
	x.RTATypeDeserializer[xtcpnl.SkMemInfoEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeSkMemInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.SkMemInfoEnumValueCst] = "skmem"

	// INET_DIAG_SHUTDOWN 8
	x.RTATypeDeserializer[xtcpnl.ShutdownEmumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeShutdownXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.ShutdownEmumValueCst] = "shut"

	// INET_DIAG_DCTCPINFO 9
	x.RTATypeDeserializer[xtcpnl.DCTCPInfoEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeDCTCPInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.DCTCPInfoEnumValueCst] = "dctcp"

	// INET_DIAG_PROTOCOL 10
	// INET_DIAG_SKV6ONLY 11
	// INET_DIAG_LOCALS 12
	// INET_DIAG_PEERS 13
	// INET_DIAG_PAD 14
	// INET_DIAG_MARK 15

	// INET_DIAG_BBRINFO 16
	x.RTATypeDeserializer[xtcpnl.BBRInfoEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeBBRInfoXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.BBRInfoEnumValueCst] = "bbr"

	// INET_DIAG_CLASS_ID 17
	x.RTATypeDeserializer[xtcpnl.ClassIDEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeClassIDXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.ClassIDEnumValueCst] = "classid"

	// INET_DIAG_MD5SIG 18
	// INET_DIAG_ULP_INFO 19
	// INET_DIAG_SK_BPF_STORAGES 20

	// INET_DIAG_CGROUP_ID 21
	x.RTATypeDeserializer[xtcpnl.CGroupIDEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeCGroupIDXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.CGroupIDEnumValueCst] = "cgroup"

	// INET_DIAG_SOCKOPT 22
	x.RTATypeDeserializer[xtcpnl.SockOptEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
		return xtcpnl.DeserializeCGroupIDXTCP(buf, xtcpRecord)
	}
	x.RTATypeDeserializerStr[xtcpnl.SockOptEnumValueCst] = "sockopt"

	// // INET_DIAG_PRAGUEINFO 23
	// x.RTATypeDeserializer[xtcpnl.SockOptEnumValueCst] = func(buf []byte, xtcpRecord *xtcppb.FlatXtcpRecord) (err error) {
	// 	return xtcpnl.DeserializeCGroupIDXTCP(buf, xtcpRecord)
	// }

	// if x.debugLevel > 10 {
	// 	for k := range x.RTATypeDeserializer {
	// 		log.Printf("RTATypeDeserializer k:%d", k)
	// 	}
	// }
	// if x.debugLevel > 10 {
	// 	for k := range x.RTATypeDeserializerStr {
	// 		log.Printf("RTATypeDeserializerStr k:%d %s", k, x.RTATypeDeserializerStr[k])
	// 	}
	// }
}
