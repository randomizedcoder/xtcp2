package godevlink

import (
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The devlink.c pr_out_* show renderers, over pkg/xtcpnl's ParseDevlink typed
// sections. Two fields ParseDevlink does not surface — a region's snapshot
// list and a parameter value's default — are read from the message's raw
// attributes (still xtcpnl: ParseNetlinkAttributes), exactly as devlink.c
// reads them from tb[].

// parseReply decodes one reply body with ParseDevlink (the subject under test).
func parseReply(body []byte) (xtcpnl.DevlinkMessage, error) {
	return xtcpnl.ParseDevlink(body, 0)
}

// handleArgs splits a DEV handle ("bus/dev") a show was given, or "","" for a
// bare dump.
func (r *runner) handleArgs() (bus, dev string, present bool) {
	if len(r.args) == 0 {
		return "", "", false
	}
	// "bus/dev" (dev names hold no '/'); a bare word is the bus with no dev.
	bus, dev, _ = strings.Cut(r.args[0], "/")
	return bus, dev, true
}

// handleAttrs builds a DEV_GET selector (BUS_NAME + DEV_NAME).
func handleAttrs(bus, dev string) []byte {
	return append(strAttr(attrBusName, bus), strAttr(attrDevName, dev)...)
}

// showBodies runs a show: a bare dump, or a targeted GET of the DEV handle.
func (r *runner) showBodies(dumpCmd uint8) ([][]byte, error) {
	if bus, dev, ok := r.handleArgs(); ok {
		return r.get(dumpCmd, handleAttrs(bus, dev))
	}
	return r.dump(dumpCmd)
}

// --- dev show (devlink.c cmd_dev_show / pr_out_dev) ---

func (r *runner) cmdDevShow() error {
	r.d.sectionStart("dev")
	bodies, err := r.showBodies(cmdGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutDev(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutDev(m xtcpnl.DevlinkMessage) {
	d := r.d
	if (m.ReloadFailed != nil && *m.ReloadFailed != 0) || m.Index != nil {
		d.handleStart(m.BusName, m.DevName, true, false)
		if m.Index != nil {
			d.printUint(printAny, "index", " index %u", uint32(*m.Index))
		}
		d.handleEnd()
	} else {
		d.handle(m.BusName, m.DevName)
	}
}

// --- port show (cmd_port_show / pr_out_port) ---

func (r *runner) cmdPortShow() error {
	r.d.sectionStart("port")
	bodies, err := r.showBodies(cmdPortGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutPort(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutPort(m xtcpnl.DevlinkMessage) {
	d := r.d
	p := m.Port
	if p == nil || p.Index == nil {
		return
	}
	d.portHandleStart(m.BusName, m.DevName, *p.Index, false)
	d.checkIndentNewline()
	if p.Type != nil {
		d.printString(printAny, "type", "type %s", portTypeName(*p.Type))
	}
	if p.NetdevName != "" {
		d.printString(printAny, "netdev", " netdev %s", p.NetdevName)
	}
	if p.Flavour != nil {
		d.printString(printAny, "flavour", " flavour %s", portFlavourName(*p.Flavour))
	}
	if p.Number != nil {
		d.printUint(printAny, "port", " port %u", *p.Number)
	}
	// Splittable is a bool the kernel always sends for a physical port; the
	// capture has it, so it is rendered when present. ParseDevlink does not
	// surface it, so read it from the raw attributes.
	if v, ok := rawU8(m.Attributes, attrPortSplittable); ok {
		d.printBool(printAny, "splittable", " splittable %s", v != 0)
	}
	d.portHandleEnd()
}

// --- sb show (cmd_sb_show): netdevsim registers none, an empty dump ---

func (r *runner) cmdSBShow() error {
	r.d.sectionStart("sb")
	_, err := r.showBodies(cmdSBGet)
	// pr_out_sb would render each, but netdevsim has none: an empty section.
	r.d.sectionEnd()
	return err
}

// --- resource show (cmd_resource_show / resources_show / resource_show) ---

func (r *runner) cmdResourceShow() error {
	r.d.sectionStart("resources")
	bodies, err := r.dump(cmdResourceDump)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			if derr := r.resourcesShow(m); derr != nil {
				err = derr
				break
			}
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) resourcesShow(m xtcpnl.DevlinkMessage) error {
	// resources_dpipe_tables_init: a fresh socket, one DPIPE_TABLE_GET.
	if err := r.dpipeConsume(m.BusName, m.DevName); err != nil {
		return err
	}
	for i := range m.Resources {
		r.d.handleStartArr(m.BusName, m.DevName)
		r.resourceShow(&m.Resources[i])
		r.d.handleEnd()
	}
	return nil
}

func (r *runner) resourceShow(res *xtcpnl.DevlinkResource) {
	d := r.d
	d.checkIndentNewline()
	d.printString(printAny, "name", "name %s", res.Name)
	if res.Size != nil {
		d.prOutU64("size", *res.Size)
	}
	if res.Size != nil && res.SizeNew != nil && *res.Size != *res.SizeNew {
		d.prOutU64("size_new", *res.SizeNew)
	}
	if res.Occ != nil {
		d.printUint(printAny, "occ", " occ %u", uint32(*res.Occ))
	}
	if res.Unit != nil {
		d.printString(printAny, "unit", " unit %s", resourceUnitName(*res.Unit))
	}
	if res.SizeMin != nil && res.SizeMax != nil && *res.SizeMin != *res.SizeMax {
		d.printUint(printAny, "size_min", " size_min %u", uint32(*res.SizeMin))
		d.prOutU64("size_max", *res.SizeMax)
		if res.SizeGran != nil {
			d.printUint(printAny, "size_gran", " size_gran %u", uint32(*res.SizeGran))
		}
	}
	// resource_dpipe_tables_show: netdevsim registers no dpipe table mapped to
	// a resource, so the list is always "none".
	d.printString(printAny, "dpipe_tables", " dpipe_tables none", "none")

	if len(res.Children) == 0 {
		return
	}
	d.arrayStart("resources")
	for i := range res.Children {
		d.entryStart()
		r.resourceShow(&res.Children[i])
		d.entryEnd()
	}
	d.arrayEnd()
}

// --- dev param show (cmd_dev_param_show / pr_out_param) ---

func (r *runner) cmdDevParamShow() error {
	r.d.sectionStart("param")
	bodies, err := r.showBodies(cmdParamGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutParam(m)
		}
	}
	r.d.sectionEnd()
	return err
}

// --- region show (cmd_region_show / pr_out_region) ---

func (r *runner) cmdRegionShow() error {
	r.d.sectionStart("regions")
	bodies, err := r.showBodies(cmdRegionGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutRegion(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutRegion(m xtcpnl.DevlinkMessage) {
	d := r.d
	reg := m.Region
	if reg == nil {
		return
	}
	d.regionHandleStart(m.BusName, m.DevName, reg.Name)
	if reg.Size != nil {
		d.prOutU64("size", *reg.Size)
	}
	// Snapshots: the attr list is not in ParseDevlink's DevlinkRegion, so read
	// it raw (pr_out_snapshots over DEVLINK_ATTR_REGION_SNAPSHOTS).
	if snaps, ok := rawNested(m.Attributes, attrRegionSnapshots); ok {
		// pr_out_snapshots: "snapshot [" id ... "]" as text, a JSON array.
		d.indentNewlineSep()
		if d.isJSON() {
			d.jw.name("snapshot")
			d.jw.startArray()
		} else {
			d.prOut("snapshot [")
		}
		idx := 0
		for _, snap := range snaps {
			if snap.ID() != attrRegionSnapshot {
				continue
			}
			sub, err := xtcpnl.ParseNetlinkAttributes(snap.Data)
			if err != nil {
				break
			}
			if id, ok := rawU32(sub, attrRegionSnapshotID); ok {
				if d.isJSON() {
					d.printUint(printJSON, "", "", id)
				} else {
					if idx > 0 {
						d.prOut(" ")
					}
					d.printUint(printFP, "", "%u", id)
				}
				idx++
			}
		}
		if d.isJSON() {
			d.jw.endArray()
		} else {
			d.prOut("]")
		}
	}
	if reg.MaxSnapshots != nil {
		d.prOutU64("max", uint64(*reg.MaxSnapshots))
	}
	d.regionHandleEnd()
}

// --- dev info (cmd_dev_info / pr_out_info) ---

func (r *runner) cmdDevInfo() error {
	r.d.sectionStart("info")
	bodies, err := r.showBodies(cmdInfoGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutInfo(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutInfo(m xtcpnl.DevlinkMessage) {
	d := r.d
	info := m.Info
	if info == nil {
		return
	}
	d.handleStart(m.BusName, m.DevName, true, false)
	d.indentInc()
	if info.DriverName != "" {
		if !d.isJSON() {
			d.newline()
		}
		d.checkIndentNewline()
		d.printString(printAny, "driver", "driver %s", info.DriverName)
	}
	d.indentDec()

	hasVersions := len(info.Fixed) > 0 || len(info.Running) > 0 || len(info.Stored) > 0
	if hasVersions {
		d.objectStart("versions")
		r.prOutVersions("fixed", info.Fixed)
		r.prOutVersions("running", info.Running)
		r.prOutVersions("stored", info.Stored)
		d.objectEnd()
	}
	d.handleEnd()
}

func (r *runner) prOutVersions(name string, vers []xtcpnl.DevlinkInfoVersion) {
	if len(vers) == 0 {
		return
	}
	d := r.d
	d.objectStart(name)
	for _, v := range vers {
		d.checkIndentNewline()
		d.printStringNameValue(v.Name, v.Value)
		if !d.isJSON() {
			d.newline()
		}
	}
	d.objectEnd()
}

// --- health show (cmd_health_show / pr_out_health) ---

func (r *runner) cmdHealthShow() error {
	r.d.sectionStart("health")
	bodies, err := r.showBodies(cmdHealthReporterGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutHealth(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutHealth(m xtcpnl.DevlinkMessage) {
	d := r.d
	h := m.HealthReporter
	if h == nil || h.State == nil || h.ErrCount == nil || h.RecoverCount == nil {
		return
	}
	d.handleStartArr(m.BusName, m.DevName)
	d.checkIndentNewline()
	d.printString(printAny, "reporter", "reporter %s", h.Name)
	if !d.isJSON() {
		d.newline()
		d.indentInc()
	}
	d.checkIndentNewline()
	d.printString(printAny, "state", "state %s", healthStateName(*h.State))
	d.prOutU64("error", *h.ErrCount)
	d.prOutU64("recover", *h.RecoverCount)
	if h.GracePeriod != nil {
		d.prOutU64("grace_period", *h.GracePeriod)
	}
	if h.BurstPeriod != nil {
		d.prOutU64("burst_period", *h.BurstPeriod)
	}
	if h.AutoRecover != nil {
		d.printBool(printAny, "auto_recover", " auto_recover %s", *h.AutoRecover != 0)
	}
	if h.AutoDump != nil {
		d.printBool(printAny, "auto_dump", " auto_dump %s", *h.AutoDump != 0)
	}
	d.indentDec()
	d.handleEnd()
}

// --- trap show (cmd_trap_show / pr_out_trap) ---

func (r *runner) cmdTrapShow() error {
	r.d.sectionStart("trap")
	bodies, err := r.showBodies(cmdTrapGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutTrap(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutTrap(m xtcpnl.DevlinkMessage) {
	d := r.d
	t := m.Trap
	if t == nil || t.Action == nil || t.Type == nil {
		return
	}
	d.handleStartArr(m.BusName, m.DevName)
	d.checkIndentNewline()
	d.printString(printAny, "name", "name %s", t.Name)
	d.printString(printAny, "type", " type %s", trapTypeName(*t.Type))
	d.printBool(printAny, "generic", " generic %s", t.Generic)
	d.printString(printAny, "action", " action %s", trapActionName(*t.Action))
	d.printString(printAny, "group", " group %s", t.GroupName)
	d.handleEnd()
}

// --- trap group show (cmd_trap_group_show / pr_out_trap_group) ---

func (r *runner) cmdTrapGroupShow() error {
	r.d.sectionStart("trap_group")
	bodies, err := r.showBodies(cmdTrapGroupGet)
	if err == nil {
		for _, b := range bodies {
			m, perr := parseReply(b)
			if perr != nil {
				err = perr
				break
			}
			r.prOutTrapGroup(m)
		}
	}
	r.d.sectionEnd()
	return err
}

func (r *runner) prOutTrapGroup(m xtcpnl.DevlinkMessage) {
	d := r.d
	g := m.TrapGroup
	if g == nil {
		return
	}
	d.handleStartArr(m.BusName, m.DevName)
	d.checkIndentNewline()
	d.printString(printAny, "name", "name %s", g.Name)
	d.printBool(printAny, "generic", " generic %s", g.Generic)
	if g.PolicerID != nil {
		d.printUint(printAny, "policer", " policer %u", *g.PolicerID)
	}
	d.handleEnd()
}
