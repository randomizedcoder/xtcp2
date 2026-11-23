package gotipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// --- requests (tipc/msg.c) ---

// dumpit is msg_init + msg_dumpit: the command with NLM_F_DUMP, one
// top-level nest when top is set.
func (r *runner) dumpit(cmd uint8, top uint16, put func(*xtcpnl.AttrBuilder) error, cb dataCB) error {
	req, err := xtcpnl.BuildTIPCRequest(r.familyID, cmd, unix.NLM_F_DUMP, r.nextSeq(), top, put)
	if err != nil {
		return err
	}
	return r.sndrcv(req, cb)
}

// doit is msg_init + msg_doit: the command with NLM_F_ACK.
func (r *runner) doit(cmd uint8, top uint16, put func(*xtcpnl.AttrBuilder) error, cb dataCB) error {
	req, err := xtcpnl.BuildTIPCRequest(r.familyID, cmd, unix.NLM_F_ACK, r.nextSeq(), top, put)
	if err != nil {
		return err
	}
	return r.sndrcv(req, cb)
}

func putName(id uint16, name string) func(*xtcpnl.AttrBuilder) error {
	return func(ab *xtcpnl.AttrBuilder) error { return ab.PutString(id, name) }
}

func putU32(id uint16, v uint32) func(*xtcpnl.AttrBuilder) error {
	return func(ab *xtcpnl.AttrBuilder) error { return ab.PutU32(id, v) }
}

// tipcCB decodes a reply for its callback: a reply ParseTIPC refuses is
// the callback's MNL_CB_ERROR. (tipc reads whatever bytes are there; for a
// malformed reply gotipc refuses where tipc would print or crash.)
func tipcCB(f func(m xtcpnl.TIPCMessage) error) dataCB {
	return func(msg Msg) error {
		m, err := xtcpnl.ParseTIPC(msg.Body, msg.Flags)
		if err != nil {
			return errCBError
		}
		return f(m)
	}
}

// need is mnl_attr_get_u32 on an attribute tipc did not check: absent, it
// reads NULL and tipc crashes.
func need(p *uint32) (uint32, error) {
	if p == nil {
		return 0, errCrash
	}
	return *p, nil
}

func needStr(p *string) (string, error) {
	if p == nil {
		return "", errCrash
	}
	return *p, nil
}

// propOf is the PROP id a get command's word names, as each cmd_*_get_prop
// compares cmd->cmd.
func propOf(name string) uint16 {
	switch name {
	case wPriority:
		return xtcpnl.TIPCPropPrio
	case wTolerance:
		return xtcpnl.TIPCPropTol
	case wWindow:
		return xtcpnl.TIPCPropWin
	case wMTU:
		return xtcpnl.TIPCPropMTU
	}
	return 0
}

func propValue(p *xtcpnl.TIPCProp, id uint16) *uint32 {
	if p == nil {
		return nil
	}
	switch id {
	case xtcpnl.TIPCPropPrio:
		return p.Prio
	case xtcpnl.TIPCPropTol:
		return p.Tol
	case xtcpnl.TIPCPropWin:
		return p.Win
	case xtcpnl.TIPCPropMTU:
		return p.MTU
	case xtcpnl.TIPCPropBroadcast:
		return p.Broadcast
	case xtcpnl.TIPCPropBroadcastRatio:
		return p.BroadcastRatio
	}
	return nil
}

// --- bearer.c ---

// supMedia is struct tipc_sup_media: a media, the key naming its bearer,
// and that key's help.
type supMedia struct {
	media, identifier string
	help              func(r *runner, c *cmdl, media string)
}

// mediaType is cmd_get_media_type: the media key's value, or its refusal.
func (r *runner) mediaType(c *cmdl, cmd *cmdEntry, opts []opt) (string, error) {
	m := getOpt(opts, wMedia)
	if m == nil {
		if r.help {
			return "", r.cmdHelp(c, cmd)
		}
		r.errf("error, missing bearer media\n")
		return "", errFail
	}
	return *m, nil
}

// uniqueBearerName is cmd_get_unique_bearer_name: MEDIA:IDENTIFIER, cut to
// TIPC_MAX_BEARER_NAME-1 bytes as snprintf cuts it.
func (r *runner) uniqueBearerName(c *cmdl, cmd *cmdEntry, opts []opt, sup []supMedia) (string, error) {
	media, err := r.mediaType(c, cmd, opts)
	if err != nil {
		return "", err
	}
	for _, e := range sup {
		if e.media != media {
			continue
		}
		v := getOpt(opts, e.identifier)
		if v == nil {
			if r.help {
				e.help(r, c, media)
			} else {
				r.errf("error, missing bearer %s\n", e.identifier)
			}
			return "", errFail
		}
		name := media + ":" + *v
		return name[:min(len(name), 31)], nil
	}
	r.errf("error, invalid media type %s\n", media)
	return "", errFail
}

// bearerGetProp is cmd_bearer_get_prop and bearer_get_cb.
func bearerGetProp(r *runner, c *cmdl, cmd *cmdEntry) error {
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	prop := propOf(cmd.name)
	opts := []opt{{key: wDevice, keyval: true}, {key: wMedia, keyval: true}, {key: wName, keyval: true}}
	if err := r.parseOpts(opts, c); err != nil {
		return err
	}
	if prop == xtcpnl.TIPCPropMTU {
		media, err := r.mediaType(c, cmd, opts)
		if err != nil {
			return err
		}
		if media != wUDP {
			r.errf("error, not supported for media\n")
			return errFail
		}
	}
	bname, err := r.uniqueBearerName(c, cmd, opts, []supMedia{
		{wUDP, wName, bearerGetUDPHelp}, {"eth", wDevice, bearerGetL2Help}, {"ib", wDevice, bearerGetL2Help},
	})
	if err != nil {
		return err
	}
	return r.doit(xtcpnl.TIPCCmdBearerGet, xtcpnl.TIPCAttrBearer, putName(xtcpnl.TIPCBearerName, bname),
		tipcCB(func(m xtcpnl.TIPCMessage) error {
			if m.Bearer == nil || m.Bearer.Prop == nil {
				return errCBError
			}
			v := propValue(m.Bearer.Prop, prop)
			if v == nil {
				return errCBError
			}
			r.o.printf("%d\n", *v)
			return nil
		}))
}

// bearerList is cmd_bearer_list and bearer_list_cb.
func bearerList(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s bearer list\n", c.argv[0])
		return errFail
	}
	return r.dumpit(xtcpnl.TIPCCmdBearerGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Bearer == nil {
			r.errf("No bearer in netlink response\n")
			return errCBError
		}
		if m.Bearer.Name == nil {
			r.errf("Bearer name missing in netlink response\n")
			return errCBError
		}
		r.o.printf("%s\n", *m.Bearer.Name)
		return nil
	}))
}

// --- media.c ---

// mediaGetProp is cmd_media_get_prop and media_get_cb.
func mediaGetProp(r *runner, c *cmdl, cmd *cmdEntry) error {
	prop := propOf(cmd.name)
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	opts := []opt{{key: wMedia, keyval: true}}
	if err := r.parseOpts(opts, c); err != nil {
		return err
	}
	media := getOpt(opts, wMedia)
	if media == nil {
		r.errf("error, missing media\n")
		return errFail
	}
	if prop == xtcpnl.TIPCPropMTU && *media != wUDP {
		r.errf("error, not supported for media\n")
		return errFail
	}
	return r.doit(xtcpnl.TIPCCmdMediaGet, xtcpnl.TIPCAttrMedia, putName(xtcpnl.TIPCMediaName, *media),
		tipcCB(func(m xtcpnl.TIPCMessage) error {
			if m.Media == nil || m.Media.Prop == nil {
				return errCBError
			}
			v := propValue(m.Media.Prop, prop)
			if v == nil {
				return errCBError
			}
			r.o.printf("%d\n", *v)
			return nil
		}))
}

// mediaList is cmd_media_list and media_list_cb.
func mediaList(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s media list\n", c.argv[0])
		return errFail
	}
	return r.dumpit(xtcpnl.TIPCCmdMediaGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Media == nil || m.Media.Name == nil {
			return errCBError
		}
		r.o.printf("%s\n", *m.Media.Name)
		return nil
	}))
}

// --- link.c ---

// linkList is cmd_link_list and link_list_cb: one JSON object for all the
// links, a key each.
func linkList(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s link list\n", c.argv[0])
		return errFail
	}
	r.o.newJSONObj()
	r.o.openJSONObject(nil)
	err := r.dumpit(xtcpnl.TIPCCmdLinkGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Link == nil || m.Link.Name == nil {
			return errCBError
		}
		name := *m.Link.Name
		state := "down"
		if m.Link.Up {
			state = "up"
		}
		if err := r.o.printString(printFP, nil, ptr("%s: "), &name); err != nil {
			return err
		}
		return r.o.printString(printAny, &name, ptr("%s\n"), &state)
	}))
	if isFatal(err) {
		return err
	}
	r.o.closeJSONObject()
	r.o.deleteJSONObj()
	return err
}

// isFatal is an error that ends the run before tipc's own cleanup could:
// a crash, or a replay that no longer matches.
func isFatal(err error) bool {
	return errors.Is(err, errCrash) || errors.Is(err, errNullString) || errors.Is(err, errFPE) ||
		errors.Is(err, ErrReplay)
}

// linkGetProp is cmd_link_get_prop and link_get_cb, whose JSON writer opens
// and closes in the callback.
func linkGetProp(r *runner, c *cmdl, cmd *cmdEntry) error {
	prop := propOf(cmd.name)
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	opts := []opt{{key: wLink, keyval: true}}
	if err := r.parseOpts(opts, c); err != nil {
		return err
	}
	link := getOpt(opts, wLink)
	if link == nil {
		r.errf("error, missing link\n")
		return errFail
	}
	return r.doit(xtcpnl.TIPCCmdLinkGet, xtcpnl.TIPCAttrLink, putName(xtcpnl.TIPCLinkName, *link),
		tipcCB(func(m xtcpnl.TIPCMessage) error {
			if m.Link == nil || m.Link.Prop == nil {
				return errCBError
			}
			v := propValue(m.Link.Prop, prop)
			if v == nil {
				return errCBError
			}
			r.o.newJSONObj()
			r.o.openJSONObject(nil)
			r.o.printUint(printAny, ptr(cmd.name), "%u\n", *v)
			r.o.closeJSONObject()
			r.o.deleteJSONObj()
			return nil
		}))
}

// linkGetBcast is cmd_link_get_bcast and cmd_link_get_bcast_cb: the
// broadcast link's mode by name. An unknown mode under -j is tipc's crash.
func linkGetBcast(r *runner, c *cmdl, cmd *cmdEntry) error {
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	return r.doit(xtcpnl.TIPCCmdLinkGet, xtcpnl.TIPCAttrLink, putName(xtcpnl.TIPCLinkName, "broadcast-link"),
		tipcCB(func(m xtcpnl.TIPCMessage) error {
			if m.Link == nil || m.Link.Prop == nil || m.Link.Prop.Broadcast == nil {
				return errCBError
			}
			r.o.newJSONObj()
			r.o.openJSONObject(nil)
			var err error
			switch *m.Link.Prop.Broadcast {
			case 0x1:
				err = r.o.printString(printAny, ptr("method"), ptr("%s\n"), ptr("BROADCAST"))
			case 0x2:
				err = r.o.printString(printAny, ptr("method"), ptr("%s\n"), ptr("REPLICAST"))
			case 0x4:
				if err = r.o.printString(printAny, ptr("method"), ptr("%s"), ptr("AUTOSELECT")); err != nil {
					return err
				}
				r.o.closeJSONObject()
				r.o.openJSONObject(nil)
				ratio, nerr := need(m.Link.Prop.BroadcastRatio)
				if nerr != nil {
					return nerr
				}
				r.o.printUint(printAny, ptr("ratio"), " ratio:%u\n", ratio)
			default:
				err = r.o.printString(printAny, nil, ptr("UNKNOWN\n"), nil)
			}
			if err != nil {
				return err
			}
			r.o.closeJSONObject()
			r.o.deleteJSONObj()
			return nil
		}))
}

// linkStatShow is cmd_link_stat_show: with a link word, ask the dump for
// the broadcast links too and print only the links whose names hold it.
func linkStatShow(r *runner, c *cmdl, cmd *cmdEntry) error {
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	opts := []opt{{key: wLink, keyval: true}}
	if err := r.parseOpts(opts, c); err != nil {
		return err
	}
	link := ""
	var top uint16
	var put func(*xtcpnl.AttrBuilder) error
	if v := getOpt(opts, wLink); v != nil {
		if *v != "all" {
			link = *v
		}
		top = xtcpnl.TIPCAttrLink
		put = func(ab *xtcpnl.AttrBuilder) error { return ab.PutBytes(xtcpnl.TIPCLinkBroadcast, nil) }
	}
	r.o.newJSONObj()
	err := r.dumpit(xtcpnl.TIPCCmdLinkGet, top, put, tipcCB(func(m xtcpnl.TIPCMessage) error {
		l := m.Link
		if l == nil || l.Name == nil || l.Prop == nil || l.Stats == nil {
			return errCBError
		}
		if link != "" && !strings.Contains(*l.Name, link) {
			return nil
		}
		if l.Broadcast {
			return r.showBcLinkStat(*l.Name, l.Prop, l.Stats)
		}
		return r.showLinkStat(l)
	}))
	if isFatal(err) {
		return err
	}
	r.o.deleteJSONObj()
	return err
}

// stat is mnl_attr_get_u32(stats[id]): an absent counter is tipc's crash.
func stat(st map[uint16]uint32, id uint16) (uint32, error) {
	v, ok := st[id]
	if !ok {
		return 0, errCrash
	}
	return v, nil
}

// statLine prints one print_uint per (key, format, counter), stopping at the
// first absent counter.
type statField struct {
	key, format string
	id          uint16
}

func (r *runner) statFields(st map[uint16]uint32, fs []statField) error {
	for _, f := range fs {
		v, err := stat(st, f.id)
		if err != nil {
			return err
		}
		r.o.printUint(printAny, ptr(f.key), f.format, v)
	}
	return nil
}

// statGroup is an open_json_object(name) ... close_json_object() group.
func (r *runner) statGroup(name string, first uint32, firstFormat string, st map[uint16]uint32, rest []statField) error {
	r.o.openJSONObject(ptr(name))
	r.o.printUint(printAny, ptr(name), firstFormat, first)
	if err := r.statFields(st, rest); err != nil {
		return err
	}
	r.o.closeJSONObject()
	return nil
}

// pktIDs are one direction's packet counters.
type pktIDs struct{ info, frag, fragd, bund, bundd uint16 }

var (
	rxIDs = pktIDs{xtcpnl.TIPCStatsRxInfo, xtcpnl.TIPCStatsRxFragments, xtcpnl.TIPCStatsRxFragmented,
		xtcpnl.TIPCStatsRxBundles, xtcpnl.TIPCStatsRxBundled}
	txIDs = pktIDs{xtcpnl.TIPCStatsTxInfo, xtcpnl.TIPCStatsTxFragments, xtcpnl.TIPCStatsTxFragmented,
		xtcpnl.TIPCStatsTxBundles, xtcpnl.TIPCStatsTxBundled}
)

// pktRest is a packets group's fields after its first.
func pktRest(i pktIDs) []statField {
	return []statField{
		{"fragments", " fragments:%u", i.frag},
		{"fragmented", "/%u", i.fragd},
		{"bundles", " bundles:%u", i.bund},
		{"bundled", "/%u\n", i.bundd},
	}
}

// pktGroups prints the rx and tx packet groups, each first value from
// first (the INFO counter, or the link's total less it).
func (r *runner) pktGroups(st map[uint16]uint32, first func(i pktIDs) (uint32, error)) error {
	for _, g := range []struct {
		name, format string
		ids          pktIDs
	}{
		{"rx packets", "  RX packets:%u", rxIDs},
		{"tx packets", "  TX packets:%u", txIDs},
	} {
		v, err := first(g.ids)
		if err != nil {
			return err
		}
		if err := r.statGroup(g.name, v, g.format, st, pktRest(g.ids)); err != nil {
			return err
		}
	}
	return nil
}

// statGroups prints groups whose first value is a counter too.
func (r *runner) statGroups(st map[uint16]uint32, groups []statGroupDef) error {
	for _, g := range groups {
		first, err := stat(st, g.id)
		if err != nil {
			return err
		}
		if err := r.statGroup(g.name, first, g.format, st, g.rest); err != nil {
			return err
		}
	}
	return nil
}

type statGroupDef struct {
	name, format string
	id           uint16
	rest         []statField
}

// showBcLinkStat is _show_bc_link_stat.
func (r *runner) showBcLinkStat(name string, prop *xtcpnl.TIPCProp, st map[uint16]uint32) error {
	r.o.openJSONObject(nil)
	r.o.prints(printAny, ptr(wLink), "Link <%s>\n", name)
	win, err := need(prop.Win)
	if err != nil {
		return err
	}
	r.o.printUint(printAny, ptr(wWindow), "  Window:%u packets\n", win)
	if err := r.pktGroups(st, func(i pktIDs) (uint32, error) { return stat(st, i.info) }); err != nil {
		return err
	}
	if err := r.statGroups(st, []statGroupDef{
		{"rx naks", "  RX naks:%u", xtcpnl.TIPCStatsRxNacks, []statField{
			{"defs", " defs:%u", xtcpnl.TIPCStatsRxDeferred},
			{"dups", " dups:%u\n", xtcpnl.TIPCStatsDuplicates}}},
		{"tx naks", "  TX naks:%u", xtcpnl.TIPCStatsTxNacks, []statField{
			{"acks", " acks:%u", xtcpnl.TIPCStatsTxAcks},
			{"retrans", " retrans:%u\n", xtcpnl.TIPCStatsRetransmitted}}},
	}); err != nil {
		return err
	}
	if err := r.statFields(st, congestionFields()); err != nil {
		return err
	}
	r.o.closeJSONObject()
	return nil
}

func congestionFields() []statField {
	return []statField{
		{"congestion link", "  Congestion link:%u", xtcpnl.TIPCStatsLinkCongs},
		{"send queue max", "  Send queue max:%u", xtcpnl.TIPCStatsMaxQueue},
		{"avg", " avg:%u\n\n", xtcpnl.TIPCStatsAvgQueue},
	}
}

// perc is link.c's perc, in u32 arithmetic; a zero total is SIGFPE.
func perc(count, total uint32) (uint32, error) {
	if total == 0 {
		return 0, errFPE
	}
	return (count*100 + total/2) / total, nil
}

// showLinkStat is _show_link_stat. RX/TX packets are LINK_RX/LINK_TX less
// the INFO counters, in u32 arithmetic as C does it.
func (r *runner) showLinkStat(l *xtcpnl.TIPCLink) error {
	st := l.Stats
	r.o.openJSONObject(nil)
	if err := r.showLinkHead(l); err != nil {
		return err
	}
	totals := map[uint16]*uint32{xtcpnl.TIPCStatsRxInfo: l.RX, xtcpnl.TIPCStatsTxInfo: l.TX}
	if err := r.pktGroups(st, func(i pktIDs) (uint32, error) {
		total, err := need(totals[i.info])
		if err != nil {
			return 0, err
		}
		info, err := stat(st, i.info)
		return total - info, err
	}); err != nil {
		return err
	}
	if err := r.showLinkProfile(st); err != nil {
		return err
	}
	if err := r.statGroups(st, []statGroupDef{
		{"rx states", "  RX states:%u", xtcpnl.TIPCStatsRxStates, []statField{
			{"probes", " probes:%u", xtcpnl.TIPCStatsRxProbes},
			{"naks", " naks:%u", xtcpnl.TIPCStatsRxNacks},
			{"defs", " defs:%u", xtcpnl.TIPCStatsRxDeferred},
			{"dups", " dups:%u\n", xtcpnl.TIPCStatsDuplicates}}},
		{"tx states", "  TX states:%u", xtcpnl.TIPCStatsTxStates, []statField{
			{"probes", " probes:%u", xtcpnl.TIPCStatsTxProbes},
			{"naks", " naks:%u", xtcpnl.TIPCStatsTxNacks},
			{"acks", " acks:%u", xtcpnl.TIPCStatsTxAcks},
			{"retrans", " retrans:%u\n", xtcpnl.TIPCStatsRetransmitted}}},
	}); err != nil {
		return err
	}
	if err := r.statFields(st, congestionFields()); err != nil {
		return err
	}
	r.o.closeJSONObject()
	return nil
}

// showLinkHead is _show_link_stat's first line: the name, the state (a
// one-element JSON array), the MTU and the properties.
func (r *runner) showLinkHead(l *xtcpnl.TIPCLink) error {
	r.o.prints(printAny, ptr(wLink), "Link <%s>\n", *l.Name)
	if err := r.o.printString(printJSON, ptr("state"), nil, nil); err != nil {
		return err
	}
	r.o.openJSONArray(printJSON, nil)
	state := "DEFUNCT"
	switch {
	case l.Active:
		state = "ACTIVE"
	case l.Up:
		state = "STANDBY"
	}
	r.o.prints(printAny, nil, "  %s", state)
	r.o.closeJSONArray(printJSON, nil)
	mtu, err := need(l.MTU)
	if err != nil {
		return err
	}
	r.o.printUint(printAny, ptr(wMTU), "  MTU:%u", mtu)
	for _, f := range []struct {
		key, format string
		v           *uint32
	}{
		{wPriority, "  Priority:%u", l.Prop.Prio},
		{wTolerance, "  Tolerance:%u ms", l.Prop.Tol},
		{wWindow, "  Window:%u packets\n", l.Prop.Win},
	} {
		v, err := need(f.v)
		if err != nil {
			return err
		}
		r.o.printUint(printAny, ptr(f.key), f.format, v)
	}
	return nil
}

// showLinkProfile is the TX profile: the sample count, the average size
// (LEN_TOT over PROF_TOT) and the size buckets as percentages of PROF_TOT.
func (r *runner) showLinkProfile(st map[uint16]uint32) error {
	proft, err := stat(st, xtcpnl.TIPCStatsMsgProfTot)
	if err != nil {
		return err
	}
	cnt, err := stat(st, xtcpnl.TIPCStatsMsgLenCnt)
	if err != nil {
		return err
	}
	r.o.printUint(printAny, ptr("tx profile sample"), "  TX profile sample:%u", cnt)
	tot, err := stat(st, xtcpnl.TIPCStatsMsgLenTot)
	if err != nil {
		return err
	}
	if proft == 0 {
		return errFPE
	}
	r.o.printUint(printAny, ptr("packets average"), " packets average:%u octets\n", tot/proft)
	for i, f := range []struct{ key, format string }{
		{"0-64", "  0-64:%u%%"}, {"-256", " -256:%u%%"}, {"-1024", " -1024:%u%%"}, {"-4096", " -4096:%u%%"},
		{"-16384", " -16384:%u%%"}, {"-32768", " -32768:%u%%"}, {"-66000", " -66000:%u%%\n"},
	} {
		v, err := stat(st, xtcpnl.TIPCStatsMsgLenP0+uint16(i))
		if err != nil {
			return err
		}
		p, err := perc(v, proft)
		if err != nil {
			return err
		}
		r.o.printUint(printAny, ptr(f.key), f.format, p)
	}
	return nil
}

// linkMonSummary is cmd_link_mon_summary and link_mon_summary_cb.
func linkMonSummary(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s monitor summary\n", c.argv[0])
		return errFail
	}
	r.o.newJSONObj()
	err := r.dumpit(xtcpnl.TIPCCmdMonGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		mo := m.Mon
		if mo == nil {
			return errCBError
		}
		r.o.openJSONObject(nil)
		if err := r.o.printString(printAny, ptr("bearer"), ptr("\nbearer %s\n"), mo.BearerName); err != nil {
			return err
		}
		if mo.BearerName == nil {
			return errCrash
		}
		gen, err := need(mo.ListGen)
		if err != nil {
			return err
		}
		r.o.printUint(printAny, ptr("table_generation"), "    table_generation %u\n", gen)
		cnt, err := need(mo.PeerCnt)
		if err != nil {
			return err
		}
		r.o.printUint(printAny, ptr("cluster_size"), "    cluster_size %u\n", cnt)
		alg := "full-mesh"
		if mo.Active {
			alg = "overlapping-ring"
		}
		if err := r.o.printString(printAny, ptr("algorithm"), ptr("    algorithm %s\n"), &alg); err != nil {
			return err
		}
		r.o.closeJSONObject()
		return nil
	}))
	if isFatal(err) {
		return err
	}
	r.o.deleteJSONObj()
	return err
}

// linkMonGetProp is cmd_link_mon_get_prop and link_mon_get_cb: its JSON
// key goes straight into the outer array, as tipc writes it.
func linkMonGetProp(r *runner, _ *cmdl, _ *cmdEntry) error {
	return r.doit(xtcpnl.TIPCCmdMonGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Mon == nil || m.Mon.ActivationThreshold == nil {
			return errCBError
		}
		r.o.newJSONObj()
		r.o.printUint(printAny, ptr("threshold"), "%u\n", *m.Mon.ActivationThreshold)
		r.o.deleteJSONObj()
		return nil
	}))
}

// linkMonList is cmd_link_mon_list: an optional bearer to list, then the
// monitor dump, each bearer's peers queried on a fresh genl socket.
func linkMonList(r *runner, c *cmdl, cmd *cmdEntry) error {
	opts := []opt{{key: wMedia, keyval: true}, {key: wDevice, keyval: true}, {key: wName, keyval: true}}
	if err := r.parseOpts(opts, c); err != nil {
		return err
	}
	bname := ""
	if getOpt(opts, wMedia) != nil {
		var err error
		bname, err = r.uniqueBearerName(c, cmd, opts, []supMedia{
			{wUDP, wName, linkMonListUDPHelp}, {"eth", wDevice, linkMonListL2Help}, {"ib", wDevice, linkMonListL2Help},
		})
		if err != nil {
			return err
		}
	}
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	const title = "node          status monitored generation applied_node_status [non_applied_node:status]"
	r.o.newJSONObj()
	err := r.dumpit(xtcpnl.TIPCCmdMonGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		mo := m.Mon
		if mo == nil {
			return errCBError
		}
		name, err := needStr(mo.BearerName)
		if err != nil {
			return err
		}
		if bname != "" && bname != name {
			return nil
		}
		r.o.openJSONObject(nil)
		if err := r.o.printString(printAny, ptr("bearer"), ptr("\nbearer %s\n"), &name); err != nil {
			return err
		}
		if err := r.o.printString(printFP, nil, ptr("%s\n"), ptr(title)); err != nil {
			return err
		}
		r.o.openJSONArray(printJSON, &name)
		cnt, err := need(mo.PeerCnt)
		if err != nil {
			return err
		}
		if cnt != 0 {
			ref, err := need(mo.Ref)
			if err != nil {
				return err
			}
			if err := r.linkMonPeerList(ref); isFatal(err) {
				return err
			}
		}
		r.o.closeJSONArray(printJSON, &name)
		r.o.closeJSONObject()
		return nil
	}))
	if isFatal(err) {
		return err
	}
	r.o.deleteJSONObj()
	return err
}

// linkMonPeerList is link_mon_peer_list: a fresh genl socket (its own
// family lookup) and a MON_PEER_GET dump for one monitor. Its result is
// ignored by its caller, as tipc ignores it.
func (r *runner) linkMonPeerList(ref uint32) error {
	if err := r.initFamily(); err != nil {
		return err
	}
	return r.dumpit(xtcpnl.TIPCCmdMonPeerGet, xtcpnl.TIPCAttrMon, putU32(xtcpnl.TIPCMonRef, ref),
		tipcCB(r.linkMonPeerCB))
}

// mapGet is map_get: (up_map & (1 << i)) >> i with C's int 1, so bit 31
// sign-extends and a shift of 32 or more wraps as x86 masks it, the result
// then truncated to the int map_get returns.
func mapGet(upMap uint64, i int) bool {
	bit := uint64(int64(int32(1) << (uint(i) & 31)))
	return int32(uint32((upMap&bit)>>uint(i))) != 0
}

// linkMonPeerCB is link_mon_peer_list_cb.
func (r *runner) linkMonPeerCB(m xtcpnl.TIPCMessage) error {
	p := m.MonPeer
	if p == nil {
		return errCBError
	}
	r.o.openJSONObject(nil)
	monitored := "indirect"
	if p.Local || p.Head {
		monitored = "direct"
	}
	status := "down"
	if p.Up {
		status = "up"
	}
	var domGen uint32
	if p.DomGen != nil {
		domGen = *p.DomGen
	}
	addr, err := need(p.Addr)
	if err != nil {
		return err
	}
	addrStr := fmt.Sprintf("%d.%d.%d", addr>>24, (addr>>12)&0xfff, addr&0xfff)
	if r.o.jw != nil {
		r.o.prints(printJSON, ptr("node"), "", addrStr)
		r.o.prints(printJSON, ptr("status"), "", status)
		r.o.prints(printJSON, ptr("monitored"), "", monitored)
		r.o.printUint(printJSON, ptr("generation"), "", domGen)
	} else {
		r.o.printf("%-14s%-7s%-10s%-11d", addrStr, status, monitored, domGen)
	}
	applied, err := need(p.Applied)
	if err != nil {
		return err
	}
	if applied != 0 {
		if p.UpMap == nil || p.Members == nil {
			return errCrash
		}
		upMap := *p.UpMap
		r.o.openJSONArray(printJSON, ptr("applied_node_status"))
		for i := 0; i < int(uint16(applied)); i++ {
			if i != 0 && i%5 == 0 {
				r.o.prints(printFP, nil, "%s", ",")
			}
			s := "D"
			if mapGet(upMap, i) {
				s = "U"
			}
			r.o.prints(printAny, nil, "%s", s)
		}
		r.o.closeJSONArray(printJSON, ptr("applied_node_status"))
		r.o.openJSONArray(printJSON, ptr("[non_applied_node:status]"))
		r.o.prints(printFP, nil, " %s", "[")
		for i := int(uint16(applied)); i < len(p.Members); i++ {
			if i != int(uint16(applied)) {
				r.o.prints(printFP, nil, "%s", ",")
			}
			s := "D"
			if mapGet(upMap, i) {
				s = "U"
			}
			full := fmt.Sprintf("%x:%s", p.Members[i], s)
			r.o.prints(printAny, nil, "%s", full)
		}
		r.o.prints(printFP, nil, "%s", "]")
		r.o.closeJSONArray(printJSON, ptr("[non_applied_node:status]"))
	}
	r.o.prints(printFP, nil, "\n", "")
	r.o.closeJSONObject()
	return nil
}

// --- nametable.c ---

// scopeNames is nametable_show_cb's scope table, indexed by the kernel's
// u32 with no range check.
var scopeNames = []string{"", "zone", "cluster", "node"}

// nametableShow is cmd_nametable_show and nametable_show_cb.
func nametableShow(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s nametable show\n", c.argv[0])
		return errFail
	}
	iteration := 0
	r.o.newJSONObj()
	err := r.dumpit(xtcpnl.TIPCCmdNameTableGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		p := m.NameTable
		if p == nil || p.Type == nil {
			return errCBError
		}
		if iteration == 0 && r.o.jw == nil {
			r.o.printf("%-10s %-10s %-10s %-8s %-10s %-33s\n", "Type", "Lower", "Upper", "Scope", "Port", "Node")
		}
		iteration++
		vals := make([]uint32, 0, 5)
		for _, f := range []*uint32{p.Node, p.Lower, p.Upper, p.Scope, p.Ref} {
			v, err := need(f)
			if err != nil {
				return err
			}
			vals = append(vals, v)
		}
		node, lower, upper, scope, ref := vals[0], vals[1], vals[2], vals[3], vals[4]
		str := r.src.NodeName(node)
		if int(scope) >= len(scopeNames) {
			return errCrash // a read past the table: undefined, modeled as the crash it may be
		}
		r.o.openJSONObject(nil)
		const sp = " "
		r.o.printUint(printAny, ptr("type"), "%-10u", *p.Type)
		r.o.prints(printFP, nil, sp, "")
		r.o.printUint(printAny, ptr("lower"), "%-10u", lower)
		r.o.prints(printFP, nil, sp, "")
		r.o.printUint(printAny, ptr("upper"), "%-10u", upper)
		r.o.prints(printFP, nil, sp, "")
		r.o.prints(printAny, ptr("scope"), "%-8s", scopeNames[scope])
		r.o.prints(printFP, nil, sp, "")
		r.o.printUint(printAny, ptr("port"), "%-10u", ref)
		r.o.prints(printFP, nil, sp, "")
		r.o.prints(printAny, ptr("node"), "%s", str)
		r.o.prints(printFP, nil, "\n", "")
		r.o.closeJSONObject()
		return nil
	}))
	if isFatal(err) {
		return err
	}
	r.o.deleteJSONObj()
	return err
}

// --- node.c ---

// nodeList is cmd_node_list and node_list_cb.
func nodeList(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s node list\n", c.argv[0])
		return errFail
	}
	r.o.printf("Node Identity                    Hash     State\n")
	return r.dumpit(xtcpnl.TIPCCmdNodeGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Node == nil || m.Node.Addr == nil {
			return errCBError
		}
		addr := *m.Node.Addr
		r.o.printf("%-32s %08x ", r.src.NodeName(addr), addr)
		if m.Node.Up {
			r.o.printf("up\n")
		} else {
			r.o.printf("down\n")
		}
		return nil
	}))
}

// nodeGetAddr is cmd_node_get_addr: the node's address, from a TIPC
// socket's getsockname, not netlink.
func nodeGetAddr(r *runner, _ *cmdl, _ *cmdEntry) error {
	r.o.printf("%08x\n", r.src.OwnAddr())
	return nil
}

// nodeGetNodeID is cmd_node_get_nodeid and nodeid_get_cb: the identity from
// NET_GET, then the address as node get address prints it.
func nodeGetNodeID(r *runner, c *cmdl, cmd *cmdEntry) error {
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	return r.dumpit(xtcpnl.TIPCCmdNetGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		n := m.Net
		if n == nil || n.ID == nil {
			return errCBError
		}
		if n.NodeID == nil || n.NodeIDW1 == nil {
			return errCrash
		}
		var id [16]byte
		binary.LittleEndian.PutUint64(id[:8], *n.NodeID)
		binary.LittleEndian.PutUint64(id[8:], *n.NodeIDW1)
		r.o.printf("Node Identity                    Hash\n")
		r.o.printf("%-33s", nodeIDString(id))
		return nodeGetAddr(r, c, cmd)
	}))
}

// nodeGetNetID is cmd_node_get_netid and netid_get_cb.
func nodeGetNetID(r *runner, c *cmdl, cmd *cmdEntry) error {
	if r.help {
		return r.cmdHelp(c, cmd)
	}
	return r.dumpit(xtcpnl.TIPCCmdNetGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		if m.Net == nil || m.Net.ID == nil {
			return errCBError
		}
		r.o.printf("%d\n", *m.Net.ID)
		return nil
	}))
}

// --- socket.c ---

// socketList is cmd_socket_list and sock_list_cb: a socket's peer when it
// is connected, else its bindings, queried on a fresh genl socket.
func socketList(r *runner, c *cmdl, _ *cmdEntry) error {
	if r.help {
		r.errf("Usage: %s socket list\n", c.argv[0])
		return errFail
	}
	return r.dumpit(xtcpnl.TIPCCmdSockGet, 0, nil, tipcCB(func(m xtcpnl.TIPCMessage) error {
		s := m.Sock
		if s == nil || s.Ref == nil {
			return errCBError
		}
		r.o.printf("socket %d\n", *s.Ref)
		switch {
		case s.Con != nil:
			node, err := need(s.Con.Node)
			if err != nil {
				return err
			}
			sock, err := need(s.Con.Sock)
			if err != nil {
				return err
			}
			r.o.printf("  connected to %x:%d", node, sock)
			if !s.Con.Flag {
				r.o.printf("\n")
				return nil
			}
			typ, err := need(s.Con.Type)
			if err != nil {
				return err
			}
			inst, err := need(s.Con.Inst)
			if err != nil {
				return err
			}
			r.o.printf(" via {%d,%d}\n", typ, inst)
		case s.HasPubl:
			if err := r.publList(*s.Ref); isFatal(err) {
				return err
			}
		}
		return nil
	}))
}

// publList is publ_list and publ_list_cb: a fresh genl socket and a
// PUBL_GET dump for one socket. Its caller ignores the result.
func (r *runner) publList(ref uint32) error {
	if err := r.initFamily(); err != nil {
		return err
	}
	return r.dumpit(xtcpnl.TIPCCmdPublGet, xtcpnl.TIPCAttrSock, putU32(xtcpnl.TIPCSockRef, ref),
		tipcCB(func(m xtcpnl.TIPCMessage) error {
			p := m.Publ
			if p == nil {
				return errCBError
			}
			vals := make([]uint32, 0, 3)
			for _, f := range []*uint32{p.Type, p.Lower, p.Upper} {
				v, err := need(f)
				if err != nil {
					return err
				}
				vals = append(vals, v)
			}
			r.o.printf("  bound to {%d,%d,%d}\n", vals[0], vals[1], vals[2])
			return nil
		}))
}
