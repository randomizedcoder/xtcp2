package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"log"
)

//import "github.com/randomizedcoder/xtcp2/xtcpnl" // netlink related functions

const (
	ddebugLevel int = 11
)

func Swap32(i uint32) uint32 {
	return (i&0xff000000)>>24 | (i&0xff0000)>>8 | (i&0xff00)<<8 | (i&0xff)<<24
}

// DecodeNetlinkDagRequestFromBytes uses reflection and so it very slow
// Don't use this!!
func DecodeNetlinkDagRequestFromBytes(b []byte) (nlh NlMsgHdr, req InetDiagReqV2) {

	r := bytes.NewReader(b)

	err1 := binary.Read(r, binary.LittleEndian, &nlh)
	if err1 != nil {
		panic(err1)
	}

	if ddebugLevel > 100 {
		log.Printf("Deserialize nlh:%v", nlh)
	}

	err2 := binary.Read(r, binary.LittleEndian, &req)
	if err2 != nil {
		panic(err2)
	}

	if ddebugLevel > 100 {
		log.Printf("Deserialize req:%v", req)
		log.Printf("req.IDiagStates: %02x", req.IDiagStates)
	}

	// IDiagStates is shown by wireshark as big endian.  Going to copy wireshark
	req.IDiagStates = Swap32(req.IDiagStates)

	if ddebugLevel > 100 {
		log.Printf("Deserialize after swap req:%v", req)
		log.Printf("req.IDiagStates: %02x", req.IDiagStates)
	}

	return nlh, req
}
