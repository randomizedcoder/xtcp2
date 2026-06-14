package xtcp

import (
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

const (
	RTATypeDeserializerMapLengthCst = 25
)

func GetAllDeserializers() (deserializers []string) {
	deserializers = append(deserializers, "meminfo")
	deserializers = append(deserializers, "info")
	deserializers = append(deserializers, "vegas")
	deserializers = append(deserializers, "cong")
	deserializers = append(deserializers, "tos")
	deserializers = append(deserializers, "tc")
	deserializers = append(deserializers, "skmem")
	deserializers = append(deserializers, "shut")
	deserializers = append(deserializers, "dctcp")
	deserializers = append(deserializers, "bbr")
	deserializers = append(deserializers, "classid")
	deserializers = append(deserializers, "cgroup")
	deserializers = append(deserializers, "sockopt")
	return deserializers
}

func (x *XTCP) InitDeserializers(wg *sync.WaitGroup) {

	defer wg.Done()

	x.RTATypeDeserializer = make(map[int]func(buf []byte, xtcpRecord *xtcp_flat_record.XtcpFlatRecord) (err error), RTATypeDeserializerMapLengthCst)
	// x.RTATypeDeserializer = make(map[int]func(buf []byte, xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error), RTATypeDeserializerMapLengthCst)
	x.RTATypeDeserializerStr = make(map[int]string, RTATypeDeserializerMapLengthCst)

	// x.RTATypeDeserializer[0] = None

	// INET_DIAG_MEMINFO 1
	key := "meminfo"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.MemInfoEmumValueCst] = xtcpnl.DeserializeMemInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.MemInfoEmumValueCst] = key
	}

	// INET_DIAG_INFO 2
	key = "info"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.TCPInfoEmumValueCst] = xtcpnl.DeserializeTCPInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.TCPInfoEmumValueCst] = key
	}

	// INET_DIAG_VEGASINFO 3
	key = "vegas"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.VegasInfoEnumValueCst] = xtcpnl.DeserializeVegasInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.VegasInfoEnumValueCst] = key
	}

	// INET_DIAG_CONG 4
	key = "cong"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.CongInfoEmumValueCst] = xtcpnl.DeserializeCongInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.CongInfoEmumValueCst] = key
	}

	// INET_DIAG_TOS 5
	key = "tos"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.TypeOfServiceEmumValueCst] = xtcpnl.DeserializeTypeOfServiceXTCP
		x.RTATypeDeserializerStr[xtcpnl.TypeOfServiceEmumValueCst] = key
	}

	// INET_DIAG_TCLASS 6
	key = "tc"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.TrafficClassEmumValueCst] = xtcpnl.DeserializeTrafficClassXTCP
		x.RTATypeDeserializerStr[xtcpnl.TrafficClassEmumValueCst] = key
	}

	// INET_DIAG_SKMEMINFO 7
	key = "skmem"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.SkMemInfoEnumValueCst] = xtcpnl.DeserializeSkMemInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.SkMemInfoEnumValueCst] = key
	}

	// INET_DIAG_SHUTDOWN 8
	key = "shut"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.ShutdownEmumValueCst] = xtcpnl.DeserializeShutdownXTCP
		x.RTATypeDeserializerStr[xtcpnl.ShutdownEmumValueCst] = key
	}

	// INET_DIAG_DCTCPINFO 9
	key = "dctcp"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.DCTCPInfoEnumValueCst] = xtcpnl.DeserializeDCTCPInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.DCTCPInfoEnumValueCst] = key
	}

	// INET_DIAG_PROTOCOL 10
	// INET_DIAG_SKV6ONLY 11
	// INET_DIAG_LOCALS 12
	// INET_DIAG_PEERS 13
	// INET_DIAG_PAD 14
	// INET_DIAG_MARK 15

	// INET_DIAG_BBRINFO 16
	key = "bbr"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.BBRInfoEnumValueCst] = xtcpnl.DeserializeBBRInfoXTCP
		x.RTATypeDeserializerStr[xtcpnl.BBRInfoEnumValueCst] = key
	}

	// INET_DIAG_CLASS_ID 17
	key = "classid"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.ClassIDEnumValueCst] = xtcpnl.DeserializeClassIDXTCP
		x.RTATypeDeserializerStr[xtcpnl.ClassIDEnumValueCst] = key
	}

	// INET_DIAG_MD5SIG 18
	// INET_DIAG_ULP_INFO 19
	// INET_DIAG_SK_BPF_STORAGES 20

	// INET_DIAG_CGROUP_ID 21
	key = "cgroup"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.CGroupIDEnumValueCst] = xtcpnl.DeserializeCGroupIDXTCP
		x.RTATypeDeserializerStr[xtcpnl.CGroupIDEnumValueCst] = key
	}

	// INET_DIAG_SOCKOPT 22
	key = "sockopt"
	if _, exists := x.config.EnabledDeserializers.Enabled[key]; exists {
		x.RTATypeDeserializer[xtcpnl.SockOptEnumValueCst] = xtcpnl.DeserializeCGroupIDXTCP
		x.RTATypeDeserializerStr[xtcpnl.SockOptEnumValueCst] = key
	}

	// // INET_DIAG_PRAGUEINFO 23
	// x.RTATypeDeserializer[xtcpnl.SockOptEnumValueCst] = func(buf []byte, xtcpRecord *xtcp_flat_record.Envelope_XtcpFlatRecord) (err error) {
	// 	return xtcpnl.DeserializeCGroupIDXTCP(buf, xtcpRecord)
	// }

	// if x.debugLevel > 10 {
	// 	for k := range x.RTATypeDeserializer {
	// 		log.Printf("RTATypeDeserializer k:%d", k)
	// 	}
	// }

	if x.debugLevel > 10 {
		for k := range x.RTATypeDeserializerStr {
			log.Printf("RTATypeDeserializerStr k:%d %s", k, x.RTATypeDeserializerStr[k])
		}
	}
}
