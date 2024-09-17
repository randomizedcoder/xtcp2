package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html
// https://datatracker.ietf.org/doc/draft-ietf-opsawg-pcap/

// https://github.com/0intro/pcap/blob/main/common.go#L13

// Pcap header constants
// https://github.com/0intro/pcap/blob/main/cmd/pcapdump/main.go
// https://pkg.go.dev/github.com/0intro/pcap#pkg-overview

// https://wiki.wireshark.org/Development/LibpcapFileFormat

// https://github.com/the-tcpdump-group/libpcap/blob/master/pcap/pcap.h#L204
// struct pcap_file_header {
// 	bpf_u_int32 magic;
// 	u_short version_major;
// 	u_short version_minor;
// 	bpf_int32 thiszone;	/* not used - SHOULD be filled with 0 */
// 	bpf_u_int32 sigfigs;	/* not used - SHOULD be filled with 0 */
// 	bpf_u_int32 snaplen;	/* max length saved portion of each pkt */
// 	bpf_u_int32 linktype;	/* data link type (LINKTYPE_*) */
// };

const (
	PcapHeaderSizeCst           = 24
	PcapRecordHeaderSizeCst     = 16
	NetlinkCookedHeaderSizeCst  = 16
	PcapNetlinkOffsetCst        = PcapHeaderSizeCst + PcapRecordHeaderSizeCst + NetlinkCookedHeaderSizeCst
	PcapInetDiagSockIDOffsetCst = PcapNetlinkOffsetCst + NlMsgHdrSizeCst + InetDiagMsgBytesBeforeSocketIDCst
)

// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html#name-file-header
// A Header represents the global header in a pcap file.
type PcapHeader struct {
	Magic        uint32 // 4 = 4
	VersionMajor uint16 // 2 = 6
	VersionMinor uint16 // 2 = 8
	Reserved1    uint32 // 4 = 12
	Reserved2    uint32 // 4 = 16
	SnapLen      uint32 // 4 = 20
	FCS          uint16 // 2 = 22
	LinkType     uint16 // 2 = 24
}

var (
	ErrPcapHeaderSmall = errors.New("data too small for PcapHeader")
)

// DeserializePcapHeader does a binary read of a PcapHeader
// It does a basic length check
func DeserializePcapHeader(data []byte, ph *PcapHeader) (n int, err error) {

	if len(data) < PcapHeaderSizeCst {
		return 0, ErrPcapHeaderSmall
	}

	ph.Magic = binary.LittleEndian.Uint32(data[0:4])
	ph.VersionMajor = binary.LittleEndian.Uint16(data[4:6])
	ph.VersionMinor = binary.LittleEndian.Uint16(data[6:8])
	ph.Reserved1 = binary.LittleEndian.Uint32(data[8:12])
	ph.Reserved2 = binary.LittleEndian.Uint32(data[12:16])
	ph.SnapLen = binary.LittleEndian.Uint32(data[16:20])
	ph.FCS = binary.LittleEndian.Uint16(data[20:22])
	ph.LinkType = binary.LittleEndian.Uint16(data[22:24])

	return PcapHeaderSizeCst, nil
}

func DeserializePcapHeaderReflection(data []byte, ph *PcapHeader) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, ph)
	if err != nil {
		return 0, err
	}

	return PcapHeaderSizeCst, err
}

// https://github.com/the-tcpdump-group/libpcap/blob/master/pcap/pcap.h#L299C1-L303C3
// struct pcap_pkthdr {
// 	struct timeval ts;	/* time stamp */
// 	bpf_u_int32 caplen;	/* length of portion present in data */
// 	bpf_u_int32 len;	/* length of this packet prior to any slicing */
// };

// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html#name-packet-record
// A RecordHeader represents a record header in a pcap file.
type PcapRecordHeader struct {
	TsSec  uint32 // 4 = 4
	TsXsec uint32 // 4 = 8 // micro or nano depends on magic
	CapLen uint32 // 4 = 12
	Len    uint32 // 4 = 16
}

var (
	ErrPcapRecordHeaderSmall = errors.New("data too small for PcapRecordHeader")
)

// DeserializePcapHeader does a binary read of a PcapHeader
// It does a basic length check
func DeserializePcapRecordHeader(data []byte, prh *PcapRecordHeader) (n int, err error) {

	if len(data) < PcapRecordHeaderSizeCst {
		return 0, ErrPcapRecordHeaderSmall
	}

	prh.TsSec = binary.LittleEndian.Uint32(data[0:4])
	prh.TsXsec = binary.LittleEndian.Uint32(data[4:8])
	prh.CapLen = binary.LittleEndian.Uint32(data[8:12])
	prh.Len = binary.LittleEndian.Uint32(data[12:16])

	return PcapRecordHeaderSizeCst, nil
}

func DeserializePcapRecordHeaderReflection(data []byte, prh *PcapRecordHeader) (n int, err error) {

	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, prh)
	if err != nil {
		return 0, err
	}

	return PcapRecordHeaderSizeCst, err
}
