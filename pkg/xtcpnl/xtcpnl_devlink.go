package xtcpnl

import "encoding/binary"

// This file decodes the devlink generic netlink family behind the `devlink`
// CLI (devlink/devlink.c): the kernel's read-only dump answers to dev, port,
// sb, resource, param, region, info, health and trap(-group) show. The family
// id is assigned at boot (net/devlink/netlink.c registers "devlink"), so the
// caller resolves "devlink" through the controller before handing a body here.
//
// A devlink reply is self-describing: the DEVLINK_CMD_* a dump answers with is
// kernel-version-specific (new commands are inserted mid-enum, shifting the
// later numbers), but the DEVLINK_ATTR_* tree is append-only and stable. So
// ParseDevlink classifies by which attributes a message carries, not by its
// command, and keeps the raw command in DevlinkMessage.Command for the caller.
// Each message decodes to exactly one typed section; a dump of many objects is
// many messages, one object each, which the caller aggregates.

const (
	// DevlinkGenlName is the family's name in the controller.
	DevlinkGenlName = "devlink"

	// DEVLINK_ATTR_* (include/uapi/linux/devlink.h). Only the read-only
	// show/dump attributes this package decodes are named.
	devlinkAttrBusName            uint16 = 1
	devlinkAttrDevName            uint16 = 2
	devlinkAttrPortIndex          uint16 = 3
	devlinkAttrPortType           uint16 = 4
	devlinkAttrPortNetdevIfindex  uint16 = 6
	devlinkAttrPortNetdevName     uint16 = 7
	devlinkAttrResourceList       uint16 = 63
	devlinkAttrResource           uint16 = 64
	devlinkAttrResourceName       uint16 = 65
	devlinkAttrResourceID         uint16 = 66
	devlinkAttrResourceSize       uint16 = 67
	devlinkAttrResourceSizeNew    uint16 = 68
	devlinkAttrResourceSizeValid  uint16 = 69
	devlinkAttrResourceSizeMin    uint16 = 70
	devlinkAttrResourceSizeMax    uint16 = 71
	devlinkAttrResourceSizeGran   uint16 = 72
	devlinkAttrResourceUnit       uint16 = 73
	devlinkAttrResourceOcc        uint16 = 74
	devlinkAttrPortFlavour        uint16 = 77
	devlinkAttrPortNumber         uint16 = 78
	devlinkAttrParam              uint16 = 80
	devlinkAttrParamName          uint16 = 81
	devlinkAttrParamGeneric       uint16 = 82
	devlinkAttrParamType          uint16 = 83
	devlinkAttrParamValuesList    uint16 = 84
	devlinkAttrParamValue         uint16 = 85
	devlinkAttrParamValueData     uint16 = 86
	devlinkAttrParamValueCmode    uint16 = 87
	devlinkAttrRegionName         uint16 = 88
	devlinkAttrRegionSize         uint16 = 89
	devlinkAttrInfoDriverName     uint16 = 98
	devlinkAttrInfoVersionFixed   uint16 = 100
	devlinkAttrInfoVersionRunning uint16 = 101
	devlinkAttrInfoVersionStored  uint16 = 102
	devlinkAttrInfoVersionName    uint16 = 103
	devlinkAttrInfoVersionValue   uint16 = 104
	devlinkAttrHealthReporter     uint16 = 114
	devlinkAttrHealthName         uint16 = 115
	devlinkAttrHealthState        uint16 = 116
	devlinkAttrHealthErrCount     uint16 = 117
	devlinkAttrHealthRecoverCount uint16 = 118
	devlinkAttrHealthGracePeriod  uint16 = 120
	devlinkAttrHealthAutoRecover  uint16 = 121
	devlinkAttrTrapName           uint16 = 130
	devlinkAttrTrapAction         uint16 = 131
	devlinkAttrTrapType           uint16 = 132
	devlinkAttrTrapGeneric        uint16 = 133
	devlinkAttrTrapGroupName      uint16 = 135
	devlinkAttrReloadFailed       uint16 = 136
	devlinkAttrHealthAutoDump     uint16 = 141
	devlinkAttrTrapPolicerID      uint16 = 142
	devlinkAttrRegionMaxSnapshots uint16 = 170
	devlinkAttrHealthBurstPeriod  uint16 = 181
	devlinkAttrParamValueDefault  uint16 = 182
	devlinkAttrIndex              uint16 = 184

	// Param value data is typed by DEVLINK_ATTR_PARAM_TYPE, whose values are
	// the standard NLA_* type codes (include/uapi/linux/netlink.h).
	devlinkParamTypeU8     uint8 = 1
	devlinkParamTypeU16    uint8 = 2
	devlinkParamTypeU32    uint8 = 3
	devlinkParamTypeU64    uint8 = 4
	devlinkParamTypeString uint8 = 5
	devlinkParamTypeFlag   uint8 = 6
)

// DevlinkPort is one port from DEVLINK_CMD_PORT_GET's dump (devlink port show).
type DevlinkPort struct {
	Index         *uint32
	Type          *uint16
	NetdevIfindex *uint32
	NetdevName    string
	Flavour       *uint16
	Number        *uint32
}

// DevlinkResource is one resource and its children from the recursive
// DEVLINK_ATTR_RESOURCE_LIST tree (devlink resource show). Size and the
// bounds are u64; the sentinel 0xffffffffffffffff is devlink's "unlimited".
type DevlinkResource struct {
	Name                       string
	ID, Size, SizeNew, SizeMin *uint64
	SizeMax, SizeGran, Occ     *uint64
	Unit, SizeValid            *uint8
	Children                   []DevlinkResource
}

// DevlinkParamValue is one cmode's value of a parameter. Num holds the value
// for the integer types, Str for a string, Flag for a flag; Data keeps the
// raw bytes. HasDefault reports a DEVLINK_ATTR_PARAM_VALUE_DEFAULT alongside.
type DevlinkParamValue struct {
	Cmode      *uint8
	Num        *uint64
	Str        string
	Flag       bool
	Data       []byte
	HasDefault bool
}

// DevlinkParam is one parameter from DEVLINK_CMD_PARAM_GET's dump
// (devlink dev param show): its name, whether it is a generic (vs
// driver-specific) param, its value type, and a value per cmode.
type DevlinkParam struct {
	Name    string
	Generic bool
	Type    *uint8
	Values  []DevlinkParamValue
}

// DevlinkRegion is one region from DEVLINK_CMD_REGION_GET's dump.
type DevlinkRegion struct {
	Name         string
	Size         *uint64
	MaxSnapshots *uint32
}

// DevlinkInfoVersion is one name/value pair from an info version nest.
type DevlinkInfoVersion struct{ Name, Value string }

// DevlinkInfo is DEVLINK_CMD_INFO_GET's answer (devlink dev info): the driver
// and its fixed/running/stored version lists, each a repeated nest.
type DevlinkInfo struct {
	DriverName string
	Fixed      []DevlinkInfoVersion
	Running    []DevlinkInfoVersion
	Stored     []DevlinkInfoVersion
}

// DevlinkHealthReporter is one reporter from DEVLINK_CMD_HEALTH_REPORTER_GET's
// dump (devlink health show). The grace/burst periods and auto-recover are
// present only for a reporter that supports recovery.
type DevlinkHealthReporter struct {
	Name                     string
	State                    *uint8
	ErrCount, RecoverCount   *uint64
	GracePeriod, BurstPeriod *uint64
	AutoRecover, AutoDump    *uint8
}

// DevlinkTrap is one trap from DEVLINK_CMD_TRAP_GET's dump (devlink trap show).
type DevlinkTrap struct {
	Name      string
	GroupName string
	Type      *uint8
	Action    *uint8
	Generic   bool
}

// DevlinkTrapGroup is one group from DEVLINK_CMD_TRAP_GROUP_GET's dump
// (devlink trap group show).
type DevlinkTrapGroup struct {
	Name      string
	Generic   bool
	PolicerID *uint32
}

// DevlinkMessage is one decoded devlink reply. Every reply carries the
// bus/dev handle; exactly one typed section is set, chosen by the attributes
// present. Index is DEVLINK_ATTR_INDEX, which `devlink dev show` prints.
type DevlinkMessage struct {
	GenericNetlinkMessage
	Command        uint8
	Request        bool
	BusName        string
	DevName        string
	Index          *uint64
	ReloadFailed   *uint8
	Port           *DevlinkPort
	Resources      []DevlinkResource
	Param          *DevlinkParam
	Region         *DevlinkRegion
	Info           *DevlinkInfo
	HealthReporter *DevlinkHealthReporter
	Trap           *DevlinkTrap
	TrapGroup      *DevlinkTrapGroup
}

// ParseDevlink decodes a devlink body AFTER the caller has resolved its family
// as DevlinkGenlName. It builds on ParseGenericNetlink, so framing is checked
// as for every genl family. The handle (bus/dev) is decoded, then the one
// typed section whose marker attribute is present. An unknown command or an
// attribute from a section this package does not model is not an error; those
// bytes stay in GenericNetlinkMessage.Attributes.
func ParseDevlink(body []byte, flags uint16) (DevlinkMessage, error) {
	var m DevlinkMessage
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	m.Command = g.Command
	m.Request = flags&0x1 != 0 // NLM_F_REQUEST
	d := genlDecoder{attrs: g.Attributes}
	if busName := d.str(devlinkAttrBusName); busName != nil {
		m.BusName = *busName
	}
	if devName := d.str(devlinkAttrDevName); devName != nil {
		m.DevName = *devName
	}
	d.nlaUint(devlinkAttrIndex, &m.Index)
	d.u8(devlinkAttrReloadFailed, &m.ReloadFailed)

	// Classify by marker attribute, most specific first: a resource dump
	// carries a stray PORT_INDEX, so RESOURCE_LIST must win over it, and a
	// trap carries a group name, so TRAP_NAME must win over TRAP_GROUP_NAME.
	switch {
	case hasAttr(g.Attributes, devlinkAttrResourceList):
		m.Resources = d.decodeResourceList(devlinkAttrResourceList)
	case hasAttr(g.Attributes, devlinkAttrParam):
		m.Param = d.decodeParam()
	case hasAttr(g.Attributes, devlinkAttrRegionName):
		m.Region = d.decodeRegion()
	case hasAttr(g.Attributes, devlinkAttrInfoDriverName):
		m.Info = d.decodeInfo(g.Attributes)
	case hasAttr(g.Attributes, devlinkAttrHealthReporter):
		m.HealthReporter = d.decodeHealth()
	case hasAttr(g.Attributes, devlinkAttrTrapName):
		m.Trap = d.decodeTrap()
	case hasAttr(g.Attributes, devlinkAttrTrapGroupName):
		m.TrapGroup = d.decodeTrapGroup()
	case hasAttr(g.Attributes, devlinkAttrPortIndex):
		m.Port = d.decodePort()
	}
	return m, d.err
}

func hasAttr(attrs []NetlinkAttribute, id uint16) bool {
	for _, a := range attrs {
		if a.ID() == id {
			return true
		}
	}
	return false
}

func (d *genlDecoder) decodePort() *DevlinkPort {
	p := &DevlinkPort{}
	d.u32(devlinkAttrPortIndex, &p.Index)
	d.u16(devlinkAttrPortType, &p.Type)
	d.u32(devlinkAttrPortNetdevIfindex, &p.NetdevIfindex)
	if name := d.str(devlinkAttrPortNetdevName); name != nil {
		p.NetdevName = *name
	}
	d.u16(devlinkAttrPortFlavour, &p.Flavour)
	d.u32(devlinkAttrPortNumber, &p.Number)
	return p
}

// decodeResourceList reads the nest at id as a list of RESOURCE nests, each of
// which may hold a nested RESOURCE_LIST of children.
func (d *genlDecoder) decodeResourceList(id uint16) []DevlinkResource {
	items := d.list(id)
	if items == nil {
		return nil
	}
	out := make([]DevlinkResource, 0, len(items))
	for i := range items {
		rd := items[i]
		var r DevlinkResource
		if name := rd.str(devlinkAttrResourceName); name != nil {
			r.Name = *name
		}
		rd.u64(devlinkAttrResourceID, &r.ID)
		rd.u64(devlinkAttrResourceSize, &r.Size)
		rd.u64(devlinkAttrResourceSizeNew, &r.SizeNew)
		rd.u64(devlinkAttrResourceSizeMin, &r.SizeMin)
		rd.u64(devlinkAttrResourceSizeMax, &r.SizeMax)
		rd.u64(devlinkAttrResourceSizeGran, &r.SizeGran)
		rd.u64(devlinkAttrResourceOcc, &r.Occ)
		rd.u8(devlinkAttrResourceUnit, &r.Unit)
		rd.u8(devlinkAttrResourceSizeValid, &r.SizeValid)
		if hasAttr(rd.attrs, devlinkAttrResourceList) {
			r.Children = rd.decodeResourceList(devlinkAttrResourceList)
		}
		if rd.err != nil && d.err == nil {
			d.err = rd.err
		}
		out = append(out, r)
	}
	return out
}

func (d *genlDecoder) decodeParam() *DevlinkParam {
	nd, ok := d.nested(devlinkAttrParam)
	if !ok {
		return nil
	}
	p := &DevlinkParam{}
	if name := nd.str(devlinkAttrParamName); name != nil {
		p.Name = *name
	}
	p.Generic = nd.flag(devlinkAttrParamGeneric)
	nd.u8(devlinkAttrParamType, &p.Type)
	for _, vd := range nd.list(devlinkAttrParamValuesList) {
		var v DevlinkParamValue
		vd.u8(devlinkAttrParamValueCmode, &v.Cmode)
		v.Data = vd.bytes(devlinkAttrParamValueData)
		v.HasDefault = hasAttr(vd.attrs, devlinkAttrParamValueDefault)
		decodeParamValue(&v, p.Type)
		if vd.err != nil && nd.err == nil {
			nd.err = vd.err
		}
		p.Values = append(p.Values, v)
	}
	d.err = nd.err
	return p
}

// decodeParamValue interprets a value's raw data by the parameter's type: an
// integer of the matching width, a string, or a flag (present data = true).
func decodeParamValue(v *DevlinkParamValue, typ *uint8) {
	if typ == nil {
		return
	}
	switch *typ {
	case devlinkParamTypeFlag:
		v.Flag = v.Data != nil
	case devlinkParamTypeString:
		if n := len(v.Data); n > 0 && v.Data[n-1] == 0 {
			v.Str = string(v.Data[:n-1])
		}
	case devlinkParamTypeU8:
		if len(v.Data) == 1 {
			n := uint64(v.Data[0])
			v.Num = &n
		}
	case devlinkParamTypeU16:
		if len(v.Data) == 2 {
			n := uint64(binary.LittleEndian.Uint16(v.Data))
			v.Num = &n
		}
	case devlinkParamTypeU32:
		if len(v.Data) == 4 {
			n := uint64(binary.LittleEndian.Uint32(v.Data))
			v.Num = &n
		}
	case devlinkParamTypeU64:
		if len(v.Data) == 8 {
			n := binary.LittleEndian.Uint64(v.Data)
			v.Num = &n
		}
	}
}

func (d *genlDecoder) decodeRegion() *DevlinkRegion {
	r := &DevlinkRegion{}
	if name := d.str(devlinkAttrRegionName); name != nil {
		r.Name = *name
	}
	d.u64(devlinkAttrRegionSize, &r.Size)
	d.u32(devlinkAttrRegionMaxSnapshots, &r.MaxSnapshots)
	return r
}

func (d *genlDecoder) decodeInfo(attrs []NetlinkAttribute) *DevlinkInfo {
	info := &DevlinkInfo{}
	if name := d.str(devlinkAttrInfoDriverName); name != nil {
		info.DriverName = *name
	}
	info.Fixed = d.infoVersions(attrs, devlinkAttrInfoVersionFixed)
	info.Running = d.infoVersions(attrs, devlinkAttrInfoVersionRunning)
	info.Stored = d.infoVersions(attrs, devlinkAttrInfoVersionStored)
	return info
}

// infoVersions collects every nest of the given id (the version lists repeat
// the same attribute id, so they are siblings, not one container) and reads
// each one's name/value pair.
func (d *genlDecoder) infoVersions(attrs []NetlinkAttribute, id uint16) []DevlinkInfoVersion {
	if d.err != nil {
		return nil
	}
	var out []DevlinkInfoVersion
	for _, a := range attrs {
		if a.ID() != id || a.Type&0x4000 != 0 {
			continue
		}
		fields, err := ParseNetlinkAttributes(a.Data)
		if err != nil {
			d.err = err
			return out
		}
		vd := genlDecoder{attrs: fields}
		var ver DevlinkInfoVersion
		if name := vd.str(devlinkAttrInfoVersionName); name != nil {
			ver.Name = *name
		}
		if value := vd.str(devlinkAttrInfoVersionValue); value != nil {
			ver.Value = *value
		}
		if vd.err != nil {
			d.err = vd.err
			return out
		}
		out = append(out, ver)
	}
	return out
}

func (d *genlDecoder) decodeHealth() *DevlinkHealthReporter {
	nd, ok := d.nested(devlinkAttrHealthReporter)
	if !ok {
		return nil
	}
	h := &DevlinkHealthReporter{}
	if name := nd.str(devlinkAttrHealthName); name != nil {
		h.Name = *name
	}
	nd.u8(devlinkAttrHealthState, &h.State)
	nd.u64(devlinkAttrHealthErrCount, &h.ErrCount)
	nd.u64(devlinkAttrHealthRecoverCount, &h.RecoverCount)
	nd.u64(devlinkAttrHealthGracePeriod, &h.GracePeriod)
	nd.u64(devlinkAttrHealthBurstPeriod, &h.BurstPeriod)
	nd.u8(devlinkAttrHealthAutoRecover, &h.AutoRecover)
	nd.u8(devlinkAttrHealthAutoDump, &h.AutoDump)
	d.err = nd.err
	return h
}

func (d *genlDecoder) decodeTrap() *DevlinkTrap {
	t := &DevlinkTrap{}
	if name := d.str(devlinkAttrTrapName); name != nil {
		t.Name = *name
	}
	if group := d.str(devlinkAttrTrapGroupName); group != nil {
		t.GroupName = *group
	}
	d.u8(devlinkAttrTrapType, &t.Type)
	d.u8(devlinkAttrTrapAction, &t.Action)
	t.Generic = d.flag(devlinkAttrTrapGeneric)
	return t
}

func (d *genlDecoder) decodeTrapGroup() *DevlinkTrapGroup {
	g := &DevlinkTrapGroup{}
	if name := d.str(devlinkAttrTrapGroupName); name != nil {
		g.Name = *name
	}
	g.Generic = d.flag(devlinkAttrTrapGeneric)
	d.u32(devlinkAttrTrapPolicerID, &g.PolicerID)
	return g
}
