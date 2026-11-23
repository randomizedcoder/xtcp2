package godevlink

import (
	"encoding/binary"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// prOutParam is devlink.c's pr_out_param. It renders from the raw PARAM nest
// (ParseNetlinkAttributes) rather than ParseDevlink's DevlinkParam, because the
// per-value default is not surfaced there; the shape otherwise follows
// pr_out_param/pr_out_param_value exactly.
func (r *runner) prOutParam(m xtcpnl.DevlinkMessage) {
	d := r.d
	paramAttr, ok := rawAttr(m.Attributes, attrParam)
	if !ok {
		return
	}
	nlaParam, err := xtcpnl.ParseNetlinkAttributes(paramAttr.Data)
	if err != nil {
		return
	}
	name, hasName := rawStr(nlaParam, attrParamName)
	nlaType, hasType := rawU8(nlaParam, attrParamType)
	valuesList, hasList := rawNested(nlaParam, attrParamValuesList)
	if !hasName || !hasType || !hasList {
		return
	}

	d.handleStartArr(m.BusName, m.DevName)
	d.checkIndentNewline()
	d.printString(printAny, "name", "name %s ", name)
	if !rawPresent(nlaParam, attrParamGeneric) {
		d.printString(printAny, "type", "type %s", "driver-specific")
	} else {
		d.printString(printAny, "type", "type %s", "generic")
	}

	d.arrayStart("values")
	for _, v := range valuesList {
		if v.ID() != attrParamValue {
			continue
		}
		valNest, perr := xtcpnl.ParseNetlinkAttributes(v.Data)
		if perr != nil {
			break
		}
		d.entryStart()
		r.prOutParamValue(nlaType, valNest)
		d.entryEnd()
	}
	d.arrayEnd()
	d.handleEnd()
}

func (r *runner) prOutParamValue(nlaType uint8, nlaValue []xtcpnl.NetlinkAttribute) {
	d := r.d
	cmode, hasCmode := rawU8(nlaValue, attrParamValueCmode)
	dataAttr, hasData := rawAttr(nlaValue, attrParamValueData)
	if !hasCmode || (nlaType != varTypeFlag && !hasData) {
		return
	}
	d.checkIndentNewline()
	d.printString(printAny, "cmode", "cmode %s", paramCmodeName(cmode))

	r.paramValuePrint(nlaType, dataAttr, hasData, "value", false)

	if dflt, ok := rawAttr(nlaValue, attrParamValueDflt); ok {
		r.paramValuePrint(nlaType, dflt, true, "default", true)
	}
}

// paramValuePrint is devlink.c's pr_out_param_value_print for the value types
// netdevsim uses (u8/u16/u32/string/flag).
func (r *runner) paramValuePrint(nlaType uint8, valAttr *xtcpnl.NetlinkAttribute, present bool, label string, flagAsU8 bool) {
	d := r.d
	d.printString(printFP, "", " %s ", label)
	switch nlaType {
	case varTypeU8:
		if present && len(valAttr.Data) >= 1 {
			d.printUint(printAny, label, "%u", uint32(valAttr.Data[0]))
		}
	case varTypeU16:
		if present && len(valAttr.Data) >= 2 {
			d.printUint(printAny, label, "%u", uint32(binary.LittleEndian.Uint16(valAttr.Data)))
		}
	case varTypeU32:
		if present && len(valAttr.Data) >= 4 {
			d.printUint(printAny, label, "%u", binary.LittleEndian.Uint32(valAttr.Data))
		}
	case varTypeU64:
		if present && len(valAttr.Data) >= 8 {
			d.printU64(printAny, label, "%u", binary.LittleEndian.Uint64(valAttr.Data))
		}
	case varTypeString:
		if present {
			s := valAttr.Data
			if n := len(s); n > 0 && s[n-1] == 0 {
				s = s[:n-1]
			}
			d.printString(printAny, label, "%s", string(s))
		}
	case varTypeFlag:
		if flagAsU8 {
			v := present && len(valAttr.Data) >= 1 && valAttr.Data[0] != 0
			d.printBool(printAny, label, "%s", v)
		} else {
			d.printBool(printAny, label, "%s", present)
		}
	}
}
