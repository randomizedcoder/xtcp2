package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
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

// pcap file magics, as read by binary.LittleEndian.Uint32 of bytes 0:4.
//
// A file written big-endian yields the byte-swapped value instead. This package
// targets little-endian hosts only (see xtcpnl_rtnetlink.go) and every fixture
// here is little-endian, so the swapped magics are recognized solely in order to
// reject them with a clear error rather than misparse every subsequent field.
const (
	PcapMagicMicrosCst        = 0xa1b2c3d4 // ts_xsec is microseconds
	PcapMagicNanosCst         = 0xa1b23c4d // ts_xsec is nanoseconds
	PcapMagicMicrosSwappedCst = 0xd4c3b2a1
	PcapMagicNanosSwappedCst  = 0x4d3cb2a1
)

// PcapLinkTypeNetlinkCst is LINKTYPE_NETLINK / DLT_NETLINK: each record is a
// 16-byte Linux SLL cooked header followed by a netlink datagram. This is what
// a capture from an `nlmon` interface produces.
//
// https://www.tcpdump.org/linktypes.html
const PcapLinkTypeNetlinkCst = 253

// SllProtocolOffsetCst is the offset of the protocol field within the 16-byte
// Linux SLL ("cooked") header that prefixes every DLT_NETLINK record:
//
//	0:2   packet type          (big-endian)
//	2:4   ARPHRD_ type = 824   (ARPHRD_NETLINK, big-endian)
//	4:6   link-layer address length
//	6:14  link-layer address
//	14:16 protocol             (big-endian) — for netlink, the NETLINK_* family
//
// So bytes 14:16 carry NETLINK_ROUTE (0), NETLINK_SOCK_DIAG (4), and so on.
// This mirrors the `ether[14:2]==0` BPF filter in nix/capture-netlink-fixtures.nix.
const SllProtocolOffsetCst = 14

// PcapHeader represents the global header in a pcap file.
//
// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html#name-file-header
//
// LinkType is a single 32-bit field, per `bpf_u_int32 linktype` in the C struct
// quoted above. It was previously declared here as two uint16s (`FCS` then
// `LinkType`), which split the value: a DLT_NETLINK (253) capture read back as
// FCS=253, LinkType=0, so LinkType was 0 for every file this package has ever
// parsed. The struct is still exactly PcapHeaderSizeCst bytes, so
// DeserializePcapHeaderReflection's binary.Read is unaffected.
type PcapHeader struct {
	Magic        uint32 // 4 = 4
	VersionMajor uint16 // 2 = 6
	VersionMinor uint16 // 2 = 8
	Reserved1    uint32 // 4 = 12
	Reserved2    uint32 // 4 = 16
	SnapLen      uint32 // 4 = 20
	LinkType     uint32 // 4 = 24 ( 24 / 4 = 6 )
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
	ph.LinkType = binary.LittleEndian.Uint32(data[20:24])

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

// PcapRecordHeader represents a record header in a pcap file.
//
// https://www.ietf.org/archive/id/draft-gharris-opsawg-pcap-01.html#name-packet-record
type PcapRecordHeader struct {
	TsSec  uint32 // 4 = 4
	TsXsec uint32 // 4 = 8 // micro or nano depends on magic
	CapLen uint32 // 4 = 12
	Len    uint32 // 4 = 16
}

var (
	ErrPcapRecordHeaderSmall = errors.New("data too small for PcapRecordHeader")
)

// DeserializePcapRecordHeader does a binary read of a PcapRecordHeader
// with a basic length check.
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

// PcapRecord is one packet record: its header plus the captured bytes.
//
// Data aliases the buffer passed to ParsePcap — copy anything you retain.
type PcapRecord struct {
	Header PcapRecordHeader
	Data   []byte // CapLen bytes
}

var (
	// ErrPcapBadMagic indicates the first four bytes are not a pcap magic.
	ErrPcapBadMagic = errors.New("xtcpnl: not a pcap file (bad magic)")
	// ErrPcapByteSwapped indicates a big-endian (byte-swapped) pcap. This
	// package deserializes little-endian only, so such a file is rejected
	// rather than silently misparsed.
	ErrPcapByteSwapped = errors.New("xtcpnl: byte-swapped (big-endian) pcap is not supported")
	// ErrPcapTruncatedRecord indicates a record header or its payload runs past
	// the end of the file.
	ErrPcapTruncatedRecord = errors.New("xtcpnl: pcap record truncated")
	// ErrPcapCookedHeaderSmall indicates a record shorter than the 16-byte
	// Linux SLL cooked header that prefixes every DLT_NETLINK packet.
	ErrPcapCookedHeaderSmall = errors.New("xtcpnl: pcap record too small for the SLL cooked header")
	// ErrPcapNotNetlink indicates a capture whose link type is not DLT_NETLINK.
	ErrPcapNotNetlink = errors.New("xtcpnl: pcap link type is not DLT_NETLINK")
)

// ParsePcap walks a whole pcap file, returning the global header and every
// record in order.
//
// Unlike PcapNetlinkOffsetCst — which slices the single first record at a fixed
// offset — this handles a capture with any number of records, which is what a
// session of asynchronous kernel events looks like. A trailing remainder too
// short for a record header is an error rather than being tolerated: a pcap has
// no padding, so a partial record means a truncated file.
func ParsePcap(data []byte) (PcapHeader, []PcapRecord, error) {
	var ph PcapHeader

	if _, err := DeserializePcapHeader(data, &ph); err != nil {
		return ph, nil, err
	}

	switch ph.Magic {
	case PcapMagicMicrosCst, PcapMagicNanosCst:
		// supported
	case PcapMagicMicrosSwappedCst, PcapMagicNanosSwappedCst:
		return ph, nil, ErrPcapByteSwapped
	default:
		return ph, nil, ErrPcapBadMagic
	}

	var records []PcapRecord
	rest := data[PcapHeaderSizeCst:]
	for len(rest) > 0 {
		var prh PcapRecordHeader
		if _, err := DeserializePcapRecordHeader(rest, &prh); err != nil {
			return ph, nil, ErrPcapTruncatedRecord
		}
		rest = rest[PcapRecordHeaderSizeCst:]

		capLen := int(prh.CapLen)
		// The int conversion is safe on the 64-bit targets this package
		// supports, but guard anyway so a corrupt length cannot panic the slice.
		if capLen < 0 || capLen > len(rest) {
			return ph, nil, ErrPcapTruncatedRecord
		}

		records = append(records, PcapRecord{Header: prh, Data: rest[:capLen]})
		rest = rest[capLen:]
	}

	return ph, records, nil
}

// ParseNetlinkPcap is ParsePcap plus an assertion that the capture really is
// DLT_NETLINK, so a file recorded from the wrong interface fails loudly instead
// of yielding nonsense netlink families. Nothing validated the link type before
// this; every caller sliced at a fixed offset and assumed.
func ParseNetlinkPcap(data []byte) (PcapHeader, []PcapRecord, error) {
	ph, records, err := ParsePcap(data)
	if err != nil {
		return ph, nil, err
	}
	if ph.LinkType != PcapLinkTypeNetlinkCst {
		return ph, nil, fmt.Errorf("%w: link type %d", ErrPcapNotNetlink, ph.LinkType)
	}
	return ph, records, nil
}

// NetlinkPayload strips the 16-byte Linux SLL cooked header from a DLT_NETLINK
// record, returning the netlink family (NETLINK_ROUTE, NETLINK_SOCK_DIAG, …)
// and the netlink datagram that follows.
//
// The returned body aliases the record's Data.
func (r PcapRecord) NetlinkPayload() (family uint16, body []byte, err error) {
	if len(r.Data) < NetlinkCookedHeaderSizeCst {
		return 0, nil, ErrPcapCookedHeaderSmall
	}
	// The SLL protocol field is big-endian, unlike everything else this package
	// reads; it is a link-layer field, not a netlink one.
	family = binary.BigEndian.Uint16(r.Data[SllProtocolOffsetCst : SllProtocolOffsetCst+2])
	return family, r.Data[NetlinkCookedHeaderSizeCst:], nil
}
