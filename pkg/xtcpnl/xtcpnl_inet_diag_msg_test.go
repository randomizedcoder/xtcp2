package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

type DeserializeInetDiagMsgTest struct {
	description string
	filename    string

	Family  uint8 // 1
	State   uint8 // 1
	Timer   uint8 // 1
	Retrans uint8 // 1 = 4

	Expires uint32 // 4
	Rqueue  uint32 // 4
	Wqueue  uint32 // 4
	UID     uint32 // 4
	Inode   uint32 // 4 = 68

	Func func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error)

	debugLevel int
}

// TestDeserializeInetDiagMsg can test both our specialized deserializer and
// the reflection based version.  Most of these tests don't test the reflection
// based version
func TestDeserializeInetDiagMsg(t *testing.T) {
	var tests = []DeserializeInetDiagMsgTest{
		{
			description: "6_10_3 port4322",
			filename:    "./testdata/6_10_3/netlink_sock_diag_reply_single_packet_port4322.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 14491, // honestly not sure if this correct. wireshark doesn't decode this. The timer seems about correct
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   15598,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port4018",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4018.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 9854, // honestly not sure if this correct. wireshark doesn't decode this. The timer seems about correct
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   204403,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port4018reflect",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4018.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 9854, // honestly not sure if this correct. wireshark doesn't decode this. The timer seems about correct
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   204403,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsgViaReflection(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port4001",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4001.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 6029,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   10698,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port4001reflect",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port4001.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 6029,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   10698,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsgViaReflection(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "4_19_319_port4005",
			filename:    "./testdata/4_19_319/netlink_sock_diag_reply_single_packet_port4005.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 10947,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   27461,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "4_19_319_port4005reflect",
			filename:    "./testdata/4_19_319/netlink_sock_diag_reply_single_packet_port4005.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 10947,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   27461,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsgViaReflection(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port443v4",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port443v4.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 36179,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   26664450,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port443v4reflect",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port443v4.pcap",

			Family:  2,
			State:   1,
			Timer:   2,
			Retrans: 0,
			// sockID
			Expires: 36179,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   26664450,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsgViaReflection(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port443v6",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port443v6.pcap",

			Family:  10,
			State:   1,
			Timer:   0,
			Retrans: 0,
			// sockID
			Expires: 0,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   26683184,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsg(data, idm, s)
			},

			debugLevel: 11,
		},
		{
			description: "port443v6reflect",
			filename:    "./testdata/6_6_44/netlink_sock_diag_reply_single_packet_port443v6.pcap",

			Family:  10,
			State:   1,
			Timer:   0,
			Retrans: 0,
			// sockID
			Expires: 0,
			Rqueue:  0,
			Wqueue:  0,
			UID:     1000,
			Inode:   26683184,

			Func: func(data []byte, idm *InetDiagMsg, s *InetDiagSockID) (n int, err error) {
				return DeserializeInetDiagMsgViaReflection(data, idm, s)
			},

			debugLevel: 11,
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

		var buf []byte
		if strings.HasSuffix(test.filename, ".pcap") {
			buf = bs[PcapNetlinkOffsetCst+NlMsgHdrSizeCst : PcapNetlinkOffsetCst+NlMsgHdrSizeCst+InetDiagMsgSizeCst]
		}
		// } else {
		// 	// fix me
		// 	//buf = bs[NlMsgHdrSizeCst : NlMsgHdrSizeCst+InetDiagReqV2SizeCst]
		// }

		if test.debugLevel > 100 {
			bsMax := len(bs)
			if bsMax > 80 {
				bsMax = 80
			}
			bufMax := len(buf)
			if bufMax > 80 {
				bufMax = 80
			}
			t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs[:bsMax]))
			t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf[:bufMax]))
		}

		idm := new(InetDiagMsg)
		s := new(InetDiagSockID)

		//_, errD := DeserializeInetDiagMsgViaReflection(buf, idm, s)
		//_, errD := DeserializeInetDiagMsg(buf, idm, s)
		_, errD := test.Func(buf, idm, s)
		if errD != nil {
			t.Fatal("Test Failed DeserializeInetDiagMsg err", errD)
		}

		if test.debugLevel > 1000 {
			t.Logf("i:%d, binary.Size(*idm):%d", i, binary.Size(*idm))
			t.Logf("i:%d, binary.Size(*s):%d", i, binary.Size(*s))
		}

		if idm.Family != test.Family {
			t.Errorf("Test %d %s idm.Family:%d != test.Family:%d", i, test.description, idm.Family, test.Family)
		}

		if idm.State != test.State {
			t.Errorf("Test %d %s idm.State:%d != test.State:%d", i, test.description, idm.State, test.State)
		}
		if test.debugLevel > 1000 {
			t.Logf("i:%d, idm.State:%d", i, idm.State)
		}

		if idm.Timer != test.Timer {
			t.Errorf("Test %d %s idm.Timer:%d != test.Timer:%d", i, test.description, idm.Timer, test.Timer)
		}

		if idm.Retrans != test.Retrans {
			t.Errorf("Test %d %s idm.Retrans:%d != test.Retrans:%d", i, test.description, idm.Retrans, test.Retrans)
		}

		// we have other tests covering SocketID

		if idm.Expires != test.Expires {
			t.Errorf("Test %d %s idm.Expires:%d != test.Expires:%d", i, test.description, idm.Expires, test.Expires)
		}
		if test.debugLevel > 1000 {
			d := time.Duration(idm.Expires) * time.Millisecond
			t.Logf("i:%d, idm.Expires:%d Seconds:%0.4f", i, idm.Expires, d.Seconds())
		}

		if idm.Rqueue != test.Rqueue {
			t.Errorf("Test %d %s idm.Rqueue:%d != test.Rqueue:%d", i, test.description, idm.Rqueue, test.Rqueue)
		}

		if idm.Wqueue != test.Wqueue {
			t.Errorf("Test %d %s idm.Wqueue:%d != test.Wqueue:%d", i, test.description, idm.Wqueue, test.Wqueue)
		}

		if idm.UID != test.UID {
			t.Errorf("Test %d %s idm.UID:%d != test.UID:%d", i, test.description, idm.UID, test.UID)
		}

		if idm.Inode != test.Inode {
			t.Errorf("Test %d %s idm.Inode:%d != test.Inode:%d", i, test.description, idm.Inode, test.Inode)
		}

	}
}
