package xtcpnl

import (
	"encoding/hex"
	"io"
	"os"
	"testing"
)

type DeserializeRTAttrTest struct {
	description string
	filename    string
	length      int
	tyype       int
}

func TestDeserializeRTAttr(t *testing.T) {
	var tests = []DeserializeRTAttrTest{
		// bbrinfo
		{
			description: tnAttrBbrinfo,
			filename:    tdAttrBbrinfo_6_6_44,
			length:      24,
			tyype:       16,
		},
		{
			description: "attribute_bbrinfo_another_example",
			filename:    "./testdata/6_6_44/attribute_bbrinfo_another_example",
			length:      24,
			tyype:       16,
		},
		// vegasinfo
		{
			description: tnAttrVegasinfo,
			filename:    tdAttrVegasinfo_6_6_44,
			length:      20,
			tyype:       3,
		},
		// dctcpinfo
		{
			description: "attribute_dctcpinfo",
			filename:    "./testdata/6_6_44/attribute_dctcpinfo",
			length:      20,
			tyype:       9,
		},
		// class_id
		{
			description: "attribute_class_id",
			filename:    tdAttrClassID_6_6_44,
			length:      8,
			tyype:       17,
		},
		// cong
		// Actually cong is a null terminated string, so this can be variable length
		{
			description: "attribute_cong",
			filename:    "./testdata/6_6_44/attribute_cong",
			length:      10,
			tyype:       4,
		},
		// cong_bbr
		{
			description: "attribute_cong_bbr",
			filename:    "./testdata/6_6_44/attribute_cong_bbr",
			length:      8,
			tyype:       4,
		},
		// cong_vegas
		{
			description: "attribute_cong_vegas",
			filename:    "./testdata/6_6_44/attribute_cong_vegas",
			length:      10,
			tyype:       4,
		},
		// cong_dctcp
		{
			description: "attribute_cong_dctcp",
			filename:    "./testdata/6_6_44/attribute_cong_dctcp",
			length:      10,
			tyype:       4,
		},
		// group_id
		{
			description: "attribute_group_id",
			filename:    tdAttrCgroupID_6_6_44,
			length:      12,
			tyype:       21,
		},
		// meninfo
		{
			description: "attribute_meminfo",
			filename:    "./testdata/6_6_44/attribute_meminfo",
			length:      20,
			tyype:       1,
		},
		{
			description: tnMeminfo4_19_319,
			filename:    tdAttrMeminfo_4_19_319,
			length:      20,
			tyype:       1,
		},
		// info
		{
			description: "6_10_3 attribute_info",
			filename:    tdAttrInfo_6_10_3,
			length:      252,
			tyype:       2,
		},
		{
			description: "6_8_12 attribute_info",
			filename:    "./testdata/6_8_12/attribute_info",
			length:      252,
			tyype:       2,
		},
		{
			description: "6_6_44 attribute_info",
			filename:    tdAttrInfo_6_6_44,
			length:      244,
			tyype:       2,
		},
		{
			description: "5_15_164_attribute_info",
			filename:    "./testdata/5_15_164/attribute_info",
			length:      236,
			tyype:       2,
		},
		{
			description: "5_4_281_attribute_info",
			filename:    "./testdata/5_4_281/attribute_info",
			length:      236,
			tyype:       2,
		},
		{
			description: "4_19_319_attribute_info",
			filename:    "./testdata/4_19_319/attribute_info",
			length:      228,
			tyype:       2,
		},
		{
			description: "7_0_3 INET_DIAG_INFO (with AccECN trailer)",
			filename:    "./testdata/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443_info",
			length:      284, // 280-byte tcp_info payload + 4-byte RTAttr header (kernel 7.0.3 added 32 bytes of AccECN fields)
			tyype:       2,
		},
		// shutdown
		{
			description: tnAttrShutdown,
			filename:    tdAttrShutdown_6_6_44,
			length:      5,
			tyype:       8,
		},
		// skmeminfo
		{
			description: "attribute_skmeminfo",
			filename:    "./testdata/6_6_44/attribute_skmeminfo",
			length:      40,
			tyype:       7,
		},
		{
			description: "4_19_319_attribute_skmeminfo",
			filename:    "./testdata/4_19_319/attribute_skmeminfo_send2626560_forward2096",
			length:      40,
			tyype:       7,
		},
		// sockopt
		{
			description: tnAttrSockopt,
			filename:    "./testdata/6_6_44/attribute_sockopt",
			length:      6,
			tyype:       22,
		},
		// tos
		{
			description: "attribute_tos",
			filename:    tdAttrTos_6_6_44,
			length:      5,
			tyype:       5,
		},
	}
	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Error("Test Failed Open error:", err)
		}
		defer f.Close()

		bs, err := io.ReadAll(f)
		if err != nil {
			t.Error("Test Failed ReadAll error:", err)
		}

		buf := bs
		// var buf []byte
		// if strings.HasSuffix(test.filename, ".pcap") {
		// 	buf = bs // fix me
		// } else {
		// 	buf = bs
		// }

		rta := new(RTAttr)

		_, errD := DeserializeRTAttr(buf, rta)
		if errD != nil {
			t.Fatal("Test Failed DeserializeRTAttr errD", errD)
		}

		if int(rta.Len) != test.length {
			t.Logf("i:%d, rta:%v", i, rta)
			t.Logf("i:%d, hex:%s", i, hex.EncodeToString(buf))
			t.Errorf("Test %d %s int(rta.Len):%d != test.length:%d", i, test.description, int(rta.Len), test.length)
		}

		if int(rta.Type) != test.tyype {
			t.Errorf("Test %d %s int(rta.Type):%d != test.tyype:%d", i, test.description, int(rta.Type), test.tyype)
		}
	}
}
