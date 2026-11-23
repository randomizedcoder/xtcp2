package govdpa

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The vdpa.c show callbacks and pr_out_* renderers, over pkg/xtcpnl's
// ParseVdpa typed sections. vstats is the exception: pr_out_dev_net_vstats
// walks the reply's attributes in order, and what it prints depends on that
// order (see vstatsWalk), so it reads ParseVdpa's ordered attribute list
// rather than the Vstats section.
//
// Where ParseVdpa and vdpa's attr_cb differ — a repeated attribute (first
// wins here, last in tb[]), or a reply ParseVdpa rejects as malformed where
// mnl_attr_parse would stop at the bad attribute and render the rest — only
// a reply the kernel does not send tells them apart; govdpa treats such a
// reply as the callback's MNL_CB_ERROR.

// VIRTIO_ID_NET / VIRTIO_ID_BLOCK (include/uapi/linux/virtio_ids.h).
const (
	virtioIDNet   = 1
	virtioIDBlock = 2
)

// VIRTIO_NET_F_CTRL_VQ, VIRTIO_NET_S_LINK_UP and VIRTIO_NET_S_ANNOUNCE
// (include/uapi/linux/virtio_net.h).
const (
	virtioNetFCtrlVQ    = 17
	virtioNetSLinkUp    = 1
	virtioNetSAnnounce  = 2
	virtioTransportFLo  = 28 // VIRTIO_TRANSPORT_F_START
	virtioTransportFHi  = 42 // VIRTIO_TRANSPORT_F_END
	vdpaAttrMaxVQP      = 12 // VDPA_ATTR_DEV_NET_CFG_MAX_VQP
	vdpaAttrNegFeatures = 14 // VDPA_ATTR_DEV_NEGOTIATED_FEATURES
	vdpaAttrQueueIndex  = 17 // VDPA_ATTR_DEV_QUEUE_INDEX
	vdpaAttrVendorName  = 18 // VDPA_ATTR_DEV_VENDOR_ATTR_NAME
	vdpaAttrVendorValue = 19 // VDPA_ATTR_DEV_VENDOR_ATTR_VALUE
)

// netFeatureStrs is vdpa.c's net_feature_strs.
var netFeatureStrs = map[int]string{
	0: "CSUM", 1: "GUEST_CSUM", 2: "CTRL_GUEST_OFFLOADS", 3: "MTU", 5: "MAC",
	7: "GUEST_TSO4", 8: "GUEST_TSO6", 9: "GUEST_ECN", 10: "GUEST_UFO",
	11: "HOST_TSO4", 12: "HOST_TSO6", 13: "HOST_ECN", 14: "HOST_UFO",
	15: "MRG_RXBUF", 16: "STATUS", 17: "CTRL_VQ", 18: "CTRL_RX", 19: "CTRL_VLAN",
	20: "CTRL_RX_EXTRA", 21: "GUEST_ANNOUNCE", 22: "MQ", 24: "NOTIFY_ON_EMPTY",
	23: "CTRL_MAC_ADDR", 27: "ANY_LAYOUT", 61: "RSC_EXT", 57: "HASH_REPORT",
	60: "RSS", 62: "STANDBY", 63: "SPEED_DUPLEX",
}

// extFeatureStrs is vdpa.c's ext_feature_strs, by feature bit rather than
// bit - VIRTIO_TRANSPORT_F_START.
var extFeatureStrs = map[int]string{
	28: "RING_INDIRECT_DESC", 29: "RING_EVENT_IDX", 32: "VERSION_1",
	33: "ACCESS_PLATFORM", 34: "RING_PACKED", 35: "IN_ORDER",
	36: "ORDER_PLATFORM", 37: "SR_IOV", 38: "NOTIFICATION_DATA",
}

// parseClass is vdpa.c's parse_class over class_map.
func parseClass(i int) string {
	switch i {
	case virtioIDNet:
		return "net"
	case virtioIDBlock:
		return "block"
	}
	return "< unknown class >"
}

// deviceTypeName is vdpa.c's device_type_name.
func deviceTypeName(t uint32) string {
	switch t {
	case 1:
		return "network"
	case 2:
		return "block"
	}
	return "<unknown type>"
}

// parse decodes one reply with ParseVdpa, the subject under test.
func parse(m Msg) (xtcpnl.VdpaMessage, bool) {
	v, err := xtcpnl.ParseVdpa(m.Body, m.Flags)
	return v, err == nil
}

// --- pr_out helpers ---

// arrayStart and arrayEnd are pr_out_array_start / pr_out_array_end.
func (o *out) arrayStart(name string) {
	if !o.json {
		o.printNL()
		o.incIndent()
		o.printIndent()
	}
	o.openJSONArray(printAny, name)
}

func (o *out) arrayEnd() {
	o.closeJSONArray(printJSON, "")
	if !o.json {
		o.decIndent()
	}
}

// handleStart is pr_out_handle_start / __pr_out_vdev_handle_start: the
// handle names a JSON object, or prints "name: " in text.
func (o *out) handleStart(name string) {
	if o.json {
		o.openJSONObject(&name)
		return
	}
	o.raw(name + ": ")
}

// handleEnd is pr_out_handle_end / pr_out_vdev_handle_end.
func (o *out) handleEnd() {
	if o.json {
		o.closeJSONObject()
		return
	}
	o.printNL()
}

// mgmtdevName is the "bus/name" (or bare name) pr_out_handle_start and
// pr_out_dev print.
func mgmtdevName(v *xtcpnl.VdpaMessage) string {
	if v.MgmtdevBusName != "" {
		return v.MgmtdevBusName + "/" + v.MgmtdevDevName
	}
	return v.MgmtdevDevName
}

// printFeatures is vdpa.c's print_features: transport bits name from
// ext_feature_strs, the rest from the device's table (net's, for dev_id 1;
// none otherwise), and an unnamed bit prints as bit_N.
func (o *out) printFeatures(features uint64, mgmtdevf bool, devID uint16) {
	name := "negotiated_features"
	if mgmtdevf {
		name = "dev_features"
	}
	o.arrayStart(name)
	for i := range 64 {
		if features&(1<<i) == 0 {
			continue
		}
		var s string
		var ok bool
		if i < virtioTransportFLo || i > virtioTransportFHi {
			if devID == virtioIDNet {
				s, ok = netFeatureStrs[i]
			}
		} else {
			s, ok = extFeatureStrs[i]
		}
		if !ok {
			o.printUint(printAny, nil, " bit_%d", uint32(i))
			continue
		}
		o.printString(printAny, nil, " %s", s)
	}
	o.arrayEnd()
}

// --- mgmtdev show (cmd_mgmtdev_show_cb / pr_out_mgmtdev_show) ---

func (r *runner) mgmtdevCB(m Msg) bool {
	v, ok := parse(m)
	if !ok || v.MgmtdevDevName == "" {
		return false
	}
	md := v.Mgmtdev
	if md == nil {
		md = &xtcpnl.VdpaMgmtdev{}
	}
	o := r.o
	o.handleStart(mgmtdevName(&v))
	var classes uint64
	if md.SupportedClasses != nil {
		classes = *md.SupportedClasses
		o.arrayStart("supported_classes")
		for i := 1; i < 64; i++ {
			if classes&(1<<i) != 0 {
				o.printString(printAny, nil, " %s", parseClass(i))
			}
		}
		o.arrayEnd()
	}
	if md.MaxVQs != nil {
		o.printNL()
		o.printUint(printAny, ptr("max_supported_vqs"), "  max_supported_vqs %d", *md.MaxVQs)
	}
	if md.SupportedFeatures != nil {
		var devID uint16
		if classes&(1<<virtioIDNet) != 0 {
			devID = virtioIDNet
		}
		o.printFeatures(*md.SupportedFeatures, true, devID)
	}
	o.handleEnd()
	return true
}

// --- dev show (cmd_dev_show_cb / pr_out_dev) ---

func (r *runner) devCB(m Msg) bool {
	v, ok := parse(m)
	if !ok || v.MgmtdevDevName == "" || v.DevName == "" || v.DeviceID == nil {
		return false
	}
	d := v.Dev
	if d == nil {
		d = &xtcpnl.VdpaDev{}
	}
	o := r.o
	o.handleStart(v.DevName)
	o.printString(printAny, ptr("type"), "type %s", deviceTypeName(*v.DeviceID))
	o.printString(printAny, ptr("mgmtdev"), " mgmtdev %s", mgmtdevName(&v))
	if d.VendorID != nil {
		o.printUint(printAny, ptr("vendor_id"), " vendor_id %u", *d.VendorID)
	}
	if d.MaxVQs != nil {
		o.printUint(printAny, ptr("max_vqs"), " max_vqs %u", *d.MaxVQs)
	}
	if d.MaxVQSize != nil {
		o.printUint(printAny, ptr("max_vq_size"), " max_vq_size %u", uint32(*d.MaxVQSize))
	}
	o.handleEnd()
	return true
}

// --- dev config show (cmd_dev_config_show_cb / pr_out_dev_config) ---

func (r *runner) configCB(m Msg) bool {
	v, ok := parse(m)
	if !ok || v.DevName == "" || v.DeviceID == nil {
		return false
	}
	o := r.o
	o.handleStart(v.DevName)
	if *v.DeviceID == virtioIDNet {
		c := v.Config
		if c == nil {
			c = &xtcpnl.VdpaNetConfig{}
		}
		r.netConfig(c, *v.DeviceID)
	}
	o.handleEnd()
	return true
}

// netConfig is pr_out_dev_net_config, the "link " and "link_announce " JSON
// keys' trailing spaces included.
func (r *runner) netConfig(c *xtcpnl.VdpaNetConfig, devID uint32) {
	o := r.o
	if c.MAC != nil {
		o.printString(printAny, ptr("mac"), "mac %s ", llAddrN2A(c.MAC))
	}
	if c.Status != nil {
		link := "down"
		if *c.Status&virtioNetSLinkUp != 0 {
			link = "up"
		}
		o.printString(printAny, ptr("link "), "link %s ", link)
		o.printBool(printAny, ptr("link_announce "), "link_announce %s ", *c.Status&virtioNetSAnnounce != 0)
	}
	if c.MaxVQP != nil {
		o.printUint(printAny, ptr("max_vq_pairs"), "max_vq_pairs %d ", uint32(*c.MaxVQP))
	}
	if c.MTU != nil {
		o.printUint(printAny, ptr("mtu"), "mtu %d ", uint32(*c.MTU))
	}
	if c.NegotiatedFeatures != nil {
		// dev_id is a uint16_t there, so the u32 DEV_ID truncates.
		o.printFeatures(*c.NegotiatedFeatures, false, uint16(devID&0xffff))
	}
}

// llAddrN2A is lib/ll_addr.c's ll_addr_n2a for a non-tunnel type: hex
// bytes joined by ':'.
func llAddrN2A(b []byte) string {
	var sb strings.Builder
	for i, c := range b {
		if i > 0 {
			sb.WriteByte(':')
		}
		fmt.Fprintf(&sb, "%02x", c)
	}
	return sb.String()
}

// --- dev vstats show (cmd_dev_vstats_show_cb / pr_out_dev_vstats) ---

func (r *runner) vstatsCB(m Msg) bool {
	v, ok := parse(m)
	if !ok || v.DevName == "" || v.DeviceID == nil {
		return false
	}
	o := r.o
	o.handleStart(v.DevName)
	if *v.DeviceID == virtioIDNet {
		r.vstatsWalk(v.Attributes)
	}
	o.handleEnd()
	return true
}

// vstatsWalk is pr_out_dev_net_vstats: one pass over the attributes in
// order, comparing the raw nla_type. print_queue_type uses the MAX_VQP and
// features seen so far (max_vqp 0 when the kernel omits MAX_VQP, as it does
// without MQ or RSS, which is how qidx 0 prints "control_vq"), and reads the
// u32 QUEUE_INDEX as a u16 (the low half on a little-endian host). A value
// no name preceded prints its name as glibc prints a NULL %s, "(null)", and
// in JSON goes out with no key.
func (r *runner) vstatsWalk(attrs []xtcpnl.NetlinkAttribute) {
	o := r.o
	var (
		name     *string
		features uint64
		maxVQP   uint16
	)
	for i := range attrs {
		a := &attrs[i]
		switch a.Type {
		case vdpaAttrMaxVQP:
			maxVQP = u16(a.Data)
		case vdpaAttrNegFeatures:
			features = u64(a.Data)
		case vdpaAttrQueueIndex:
			qidx := u16(a.Data)
			qt := "rx"
			switch {
			case features&(1<<virtioNetFCtrlVQ) != 0 && int(qidx) == 2*int(maxVQP):
				qt = "control_vq"
			case qidx&1 != 0:
				qt = "tx"
			}
			o.printString(printAny, ptr("queue_type"), "queue_type %s ", qt)
		case vdpaAttrVendorName:
			s := cString(a.Data)
			name = &s
		case vdpaAttrVendorValue:
			label := "(null)"
			if name != nil {
				label = *name
			}
			o.printString(printFP, nil, "%s ", label)
			o.printU64(printAny, name, "%llu ", u64(a.Data))
		}
	}
}

// u16 and u64 are mnl_attr_get_u16/u64 on a little-endian host, reading
// only the bytes the attribute has.
func u16(b []byte) uint16 {
	var buf [2]byte
	copy(buf[:], b)
	return binary.LittleEndian.Uint16(buf[:])
}

func u64(b []byte) uint64 {
	var buf [8]byte
	copy(buf[:], b)
	return binary.LittleEndian.Uint64(buf[:])
}

// cString is mnl_attr_get_str: the payload up to its NUL.
func cString(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
