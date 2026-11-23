package xtcpnl

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// This file decodes the vdpa generic netlink family behind the `vdpa` CLI
// (iproute2 vdpa/vdpa.c): the kernel's read-only answers to mgmtdev show, dev
// show, dev config show and dev vstats show (drivers/vdpa/vdpa.c). The family
// id is assigned at boot, so the caller resolves "vdpa" through the controller
// before handing a body here. The family is registered with netnsok false, so
// it answers only in init_net.
//
// Unlike devlink's, vdpa's command enum is short and append-only, but the
// replies do not echo the request's command (a DEV_GET is answered with
// DEV_NEW, a MGMTDEV_GET with MGMTDEV_NEW), so ParseVdpa classifies a message
// by the attributes it carries, as ParseDevlink does, and keeps the raw
// command in VdpaMessage.Command. Each message decodes to at most one typed
// section; a dump of many objects is many messages, one object each.

const (
	// VdpaGenlName is the family's name in the controller.
	VdpaGenlName = "vdpa"

	// VDPA_CMD_* (include/uapi/linux/vdpa.h).
	vdpaCmdMgmtdevGet   uint8 = 2
	vdpaCmdDevGet       uint8 = 5
	vdpaCmdDevConfigGet uint8 = 6
	vdpaCmdDevVstatsGet uint8 = 7

	// VDPA_ATTR_* (include/uapi/linux/vdpa.h). Only the read-only show
	// attributes this package decodes are named.
	vdpaAttrMgmtdevBusName       uint16 = 1
	vdpaAttrMgmtdevDevName       uint16 = 2
	vdpaAttrMgmtdevSupportedCls  uint16 = 3
	vdpaAttrDevName              uint16 = 4
	vdpaAttrDevID                uint16 = 5
	vdpaAttrDevVendorID          uint16 = 6
	vdpaAttrDevMaxVQs            uint16 = 7
	vdpaAttrDevMaxVQSize         uint16 = 8
	vdpaAttrDevMinVQSize         uint16 = 9
	vdpaAttrDevNetCfgMACAddr     uint16 = 10
	vdpaAttrDevNetStatus         uint16 = 11
	vdpaAttrDevNetCfgMaxVQP      uint16 = 12
	vdpaAttrDevNetCfgMTU         uint16 = 13
	vdpaAttrDevNegotiatedFeat    uint16 = 14
	vdpaAttrDevMgmtdevMaxVQs     uint16 = 15
	vdpaAttrDevSupportedFeatures uint16 = 16
	vdpaAttrDevQueueIndex        uint16 = 17
	vdpaAttrDevVendorAttrName    uint16 = 18
	vdpaAttrDevVendorAttrValue   uint16 = 19
	vdpaAttrDevFeatures          uint16 = 20

	// vdpaNameMax bounds a device name a request builder accepts. The kernel
	// takes any NUL-terminated string; the bound only keeps a request small.
	vdpaNameMax = 255
)

// VdpaMgmtdev is one management device from MGMTDEV_GET's dump (vdpa
// mgmtdev show): the classes it can create, the most virtqueues a device
// may have, and the virtio features it supports.
type VdpaMgmtdev struct {
	SupportedClasses  *uint64
	MaxVQs            *uint32
	SupportedFeatures *uint64
}

// VdpaDev is one device from DEV_GET (vdpa dev show).
type VdpaDev struct {
	VendorID  *uint32
	MaxVQs    *uint32
	MaxVQSize *uint16
	MinVQSize *uint16
}

// VdpaNetConfig is a net device's DEV_CONFIG_GET answer (vdpa dev config
// show). MaxVQP is present only when MQ or RSS was negotiated
// (vdpa_dev_net_mq_config_fill); Status is a u16 on the wire, though the
// uapi header's comment calls it u8 (vdpa_dev_net_status_config_fill puts it
// with nla_put_u16). NegotiatedFeatures is present once a driver has
// negotiated; Features is what the device was provisioned with.
type VdpaNetConfig struct {
	MAC                []byte
	Status             *uint16
	MaxVQP             *uint16
	MTU                *uint16
	NegotiatedFeatures *uint64
	Features           *uint64
}

// VdpaVendorStat is one vendor counter from a vstats reply. The kernel puts
// each as a VENDOR_ATTR_NAME string then a VENDOR_ATTR_VALUE u64, repeated,
// so the pair is matched by position. Name is nil for a value no name
// preceded.
type VdpaVendorStat struct {
	Name  *string
	Value uint64
}

// VdpaVstats is DEV_VSTATS_GET's answer (vdpa dev vstats show) for one
// queue: the negotiated features, MaxVQP when MQ or RSS was negotiated, and
// the queue's vendor counters in the order the kernel sent them.
type VdpaVstats struct {
	NegotiatedFeatures *uint64
	MaxVQP             *uint16
	Stats              []VdpaVendorStat
}

// VdpaMessage is one decoded vdpa message. The handle fields are decoded
// for every message, request or reply; at most one typed section is set,
// chosen by the attributes present.
type VdpaMessage struct {
	GenericNetlinkMessage
	Command        uint8
	Request        bool
	MgmtdevBusName string
	MgmtdevDevName string
	DevName        string
	DeviceID       *uint32
	QueueIndex     *uint32
	Mgmtdev        *VdpaMgmtdev
	Dev            *VdpaDev
	Config         *VdpaNetConfig
	Vstats         *VdpaVstats
}

// ParseVdpa decodes a vdpa body AFTER the caller has resolved its family as
// VdpaGenlName. It builds on ParseGenericNetlink, so framing is checked as
// for every genl family. An unknown command or an attribute this package
// does not model (a block device's config, say) is not an error; those bytes
// stay in GenericNetlinkMessage.Attributes.
func ParseVdpa(body []byte, flags uint16) (VdpaMessage, error) {
	var m VdpaMessage
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	m.Command = g.Command
	m.Request = flags&unix.NLM_F_REQUEST != 0
	d := genlDecoder{attrs: g.Attributes}
	if s := d.str(vdpaAttrMgmtdevBusName); s != nil {
		m.MgmtdevBusName = *s
	}
	if s := d.str(vdpaAttrMgmtdevDevName); s != nil {
		m.MgmtdevDevName = *s
	}
	if s := d.str(vdpaAttrDevName); s != nil {
		m.DevName = *s
	}
	d.u32(vdpaAttrDevID, &m.DeviceID)
	d.u32(vdpaAttrDevQueueIndex, &m.QueueIndex)
	if d.err != nil {
		return m, d.err
	}
	if m.Request {
		// A request names what it asks about; it carries no section.
		return m, nil
	}

	// Most specific first: a vstats reply carries the queue index (and the
	// negotiated features a config also has); a dev reply carries its
	// mgmtdev's name, so MAX_VQS must win over the mgmtdev markers.
	switch {
	case m.QueueIndex != nil:
		m.Vstats = d.decodeVdpaVstats(g.Attributes)
	case hasAttr(g.Attributes, vdpaAttrDevMaxVQs):
		m.Dev = d.decodeVdpaDev()
	case m.DevName != "":
		m.Config = d.decodeVdpaConfig()
	case hasAttr(g.Attributes, vdpaAttrMgmtdevSupportedCls),
		hasAttr(g.Attributes, vdpaAttrDevMgmtdevMaxVQs),
		hasAttr(g.Attributes, vdpaAttrDevSupportedFeatures):
		m.Mgmtdev = d.decodeVdpaMgmtdev()
	}
	return m, d.err
}

func (d *genlDecoder) decodeVdpaMgmtdev() *VdpaMgmtdev {
	md := &VdpaMgmtdev{}
	d.u64(vdpaAttrMgmtdevSupportedCls, &md.SupportedClasses)
	d.u32(vdpaAttrDevMgmtdevMaxVQs, &md.MaxVQs)
	d.u64(vdpaAttrDevSupportedFeatures, &md.SupportedFeatures)
	return md
}

func (d *genlDecoder) decodeVdpaDev() *VdpaDev {
	dev := &VdpaDev{}
	d.u32(vdpaAttrDevVendorID, &dev.VendorID)
	d.u32(vdpaAttrDevMaxVQs, &dev.MaxVQs)
	d.u16(vdpaAttrDevMaxVQSize, &dev.MaxVQSize)
	d.u16(vdpaAttrDevMinVQSize, &dev.MinVQSize)
	return dev
}

func (d *genlDecoder) decodeVdpaConfig() *VdpaNetConfig {
	c := &VdpaNetConfig{}
	if mac := d.bytes(vdpaAttrDevNetCfgMACAddr); mac != nil {
		if len(mac) != 6 {
			d.err = fmt.Errorf("%w: vdpa MAC attribute is %d bytes, want 6", ErrGenericNetlink, len(mac))
			return c
		}
		c.MAC = append([]byte(nil), mac...)
	}
	d.u16(vdpaAttrDevNetStatus, &c.Status)
	d.u16(vdpaAttrDevNetCfgMaxVQP, &c.MaxVQP)
	d.u16(vdpaAttrDevNetCfgMTU, &c.MTU)
	d.u64(vdpaAttrDevNegotiatedFeat, &c.NegotiatedFeatures)
	d.u64(vdpaAttrDevFeatures, &c.Features)
	return c
}

// decodeVdpaVstats reads the singletons through the decoder, then walks attrs
// in order for the repeated name/value pairs: a value takes the name most
// recently seen, and a name no value follows is dropped, as vdpa does.
func (d *genlDecoder) decodeVdpaVstats(attrs []NetlinkAttribute) *VdpaVstats {
	v := &VdpaVstats{}
	d.u64(vdpaAttrDevNegotiatedFeat, &v.NegotiatedFeatures)
	d.u16(vdpaAttrDevNetCfgMaxVQP, &v.MaxVQP)
	if d.err != nil {
		return v
	}
	var name *string
	for i := range attrs {
		a := attrs[i]
		switch a.ID() {
		case vdpaAttrDevVendorAttrName:
			n := len(a.Data)
			if n == 0 || a.Data[n-1] != 0 || a.Type&0xc000 != 0 {
				d.err = fmt.Errorf("%w: vdpa vendor stat name needs a NUL-terminated string", ErrGenericNetlink)
				return v
			}
			s := string(a.Data[:n-1])
			name = &s
		case vdpaAttrDevVendorAttrValue:
			if len(a.Data) != 8 || a.Type&0xc000 != 0 {
				d.err = fmt.Errorf("%w: vdpa vendor stat value needs an unflagged 8-byte scalar", ErrGenericNetlink)
				return v
			}
			v.Stats = append(v.Stats, VdpaVendorStat{Name: name, Value: binary.LittleEndian.Uint64(a.Data)})
		}
	}
	return v
}

// VdpaKind is a read-only vdpa show.
type VdpaKind uint8

const (
	// VdpaMgmtdevKind is `vdpa mgmtdev show` (a dump).
	VdpaMgmtdevKind VdpaKind = iota + 1
	// VdpaDevKind is `vdpa dev show [DEV]`.
	VdpaDevKind
	// VdpaConfigKind is `vdpa dev config show [DEV]`.
	VdpaConfigKind
	// VdpaVstatsKind is `vdpa dev vstats show DEV [qidx N]`.
	VdpaVstatsKind
)

// ErrUnsupportedVdpa reports a VdpaKind this package does not build.
var ErrUnsupportedVdpa = fmt.Errorf("%w: unsupported vdpa request", ErrGenericNetlink)

// BuildGetVdpaRequest builds the request vdpa sends for a read-only show,
// byte for byte: genl version 1, NLM_F_ACK always, and NLM_F_DUMP when dev
// is empty. A mgmtdev show is always a dump here; a vstats show needs a
// device. qidx is VDPA_ATTR_DEV_QUEUE_INDEX, valid only for vstats, and may
// be nil there because vdpa sends the GET without it (the kernel refuses it
// with EINVAL). family must be resolved as vdpa by the caller.
func BuildGetVdpaRequest(family uint16, kind VdpaKind, dev string, qidx *uint32, seq uint32) ([]byte, error) {
	if family <= GenlControllerID || len(dev) > vdpaNameMax {
		return nil, ErrGenericNetlink
	}
	for i := 0; i < len(dev); i++ {
		if dev[i] == 0 {
			return nil, ErrGenericNetlink
		}
	}
	var command uint8
	switch kind {
	case VdpaMgmtdevKind:
		command = vdpaCmdMgmtdevGet
		if dev != "" {
			return nil, ErrUnsupportedVdpa
		}
	case VdpaDevKind:
		command = vdpaCmdDevGet
	case VdpaConfigKind:
		command = vdpaCmdDevConfigGet
	case VdpaVstatsKind:
		command = vdpaCmdDevVstatsGet
		if dev == "" {
			return nil, ErrGenericNetlink
		}
	default:
		return nil, ErrUnsupportedVdpa
	}
	if qidx != nil && kind != VdpaVstatsKind {
		return nil, ErrGenericNetlink
	}

	var attrs []byte
	if dev != "" {
		var ok bool
		if attrs, ok = appendVdpaAttr(attrs, vdpaAttrDevName, append([]byte(dev), 0)); !ok {
			return nil, ErrGenericNetlink
		}
	}
	if qidx != nil {
		attrs, _ = appendVdpaAttr(attrs, vdpaAttrDevQueueIndex, binary.LittleEndian.AppendUint32(nil, *qidx))
	}
	out := buildGenericGet(family, command, 1, seq, attrs)
	flags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_ACK)
	if dev == "" {
		flags |= unix.NLM_F_DUMP
	}
	binary.LittleEndian.PutUint16(out[6:8], flags)
	return out, nil
}

// appendVdpaAttr appends one attribute, its length unpadded and its payload
// padded to 4 bytes with zeros, as libmnl's mnl_attr_put does. ok is false
// for a payload too long for an attribute's u16 length.
func appendVdpaAttr(dst []byte, typ uint16, payload []byte) (out []byte, ok bool) {
	n := 4 + len(payload)
	n16, ok := cint.U16(n)
	if !ok {
		return dst, false
	}
	dst = binary.LittleEndian.AppendUint16(dst, n16)
	dst = binary.LittleEndian.AppendUint16(dst, typ)
	dst = append(dst, payload...)
	for pad := (4 - n%4) % 4; pad > 0; pad-- {
		dst = append(dst, 0)
	}
	return dst, true
}
