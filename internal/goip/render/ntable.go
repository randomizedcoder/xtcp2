package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NeighTblView is one neighbor table of `ip ntable show`, transcribed from
// print_ntable (ip/ipntable.c:533-637). One view per RTM_NEWNEIGHTBL message:
// either a table's base entry (carrying NDTA_CONFIG/NDTA_STATS) or a
// device-specific parameter set (NDTPA_IFINDEX set, no config/stats).
//
// # Two gates, one clock
//
// The CONFIG and STATS blocks print only under `-s` (show_stats, :624/:630);
// the PARMS block always prints (:627). So Config and Stats are built only when
// showStats is set, Parms whenever the message carries NDTA_PARMS.
//
// print_ndtconfig renders ndtc_last_flush/ndtc_last_rand as absolute dates
// computed from `now` (ntable_strtime_delta reads gettimeofday). The clock is
// taken at NeighTblViewOf time and the dates stored as strings, so the view is
// pure data afterwards; obj_ntable passes time.Now, the `-s` replay test passes
// the capture instant so the dates reproduce byte-for-byte.
type NeighTblView struct {
	Family string

	Name    string
	HasName bool

	Thresh1    uint32
	HasThresh1 bool
	Thresh2    uint32
	HasThresh2 bool
	Thresh3    uint32
	HasThresh3 bool

	GcInterval    uint64
	HasGcInterval bool

	Config *NtConfigView
	Parms  *NtParmsView
	Stats  *NtStatsView
}

// NtConfigView is print_ndtconfig's block (ip/ipntable.c:338-369). LastFlush and
// LastRand are already-formatted dates (or "(error)"), HashMask kept raw because
// text and JSON format it differently (see MarshalJSON).
type NtConfigView struct {
	KeyLen      uint32
	EntrySize   uint32
	Entries     uint32
	LastFlush   string
	LastRand    string
	HashRnd     uint32
	HashMask    uint32
	HashChainGc uint32
	ProxyQlen   uint32
}

// NtParmsView is print_ndtparams's block (ip/ipntable.c:371-491). Dev is the
// resolved NDTPA_IFINDEX, "" when absent (the base message). Each param is a
// pointer because presence is not value: a present-but-zero param prints its
// token, an absent one prints nothing.
type NtParmsView struct {
	Dev string

	Refcnt        *uint32
	Reachable     *uint64
	BaseReachable *uint64
	Retrans       *uint64
	GcStale       *uint64
	DelayProbe    *uint64
	Queue         *uint32
	AppProbes     *uint32
	UcastProbes   *uint32
	McastProbes   *uint32
	McastReprobes *uint32
	AnycastDelay  *uint64
	ProxyDelay    *uint64
	ProxyQueue    *uint32
	Locktime      *uint64
}

// NtStatsView is print_ndtstats's block (ip/ipntable.c:493-531): 11 counters
// the kernel always sends in full, so no per-field presence is tracked.
type NtStatsView struct {
	Allocs         uint64
	Destroys       uint64
	HashGrows      uint64
	ResFailed      uint64
	Lookups        uint64
	Hits           uint64
	RcvProbesMcast uint64
	RcvProbesUcast uint64
	PeriodicGcRuns uint64
	ForcedGcRuns   uint64
	TableFulls     uint64
}

// NeighTblViewOf derives the view from a decoded reply. names resolves a
// device-specific entry's NDTPA_IFINDEX (ll_index_to_name, :379); showStats
// gates the config/stats blocks; now dates the config timestamps.
func NeighTblViewOf(ti xtcpnl.NeighTblInfo, names NameTab, showStats bool, now time.Time) NeighTblView {
	v := NeighTblView{
		Family:        viaFamilyName(ti.Family),
		Name:          ti.Name,
		HasName:       ti.HasName,
		Thresh1:       ti.Thresh1,
		HasThresh1:    ti.HasThresh1,
		Thresh2:       ti.Thresh2,
		HasThresh2:    ti.HasThresh2,
		Thresh3:       ti.Thresh3,
		HasThresh3:    ti.HasThresh3,
		GcInterval:    ti.GcInterval,
		HasGcInterval: ti.HasGcInterval,
	}
	if ti.HasConfig && showStats {
		c := ti.Config
		v.Config = &NtConfigView{
			KeyLen:      uint32(c.KeyLen),
			EntrySize:   uint32(c.EntrySize),
			Entries:     c.Entries,
			LastFlush:   ndtStrtimeDelta(now, c.LastFlush),
			LastRand:    ndtStrtimeDelta(now, c.LastRand),
			HashRnd:     c.HashRnd,
			HashMask:    c.HashMask,
			HashChainGc: c.HashChainGc,
			ProxyQlen:   c.ProxyQlen,
		}
	}
	if ti.HasParms {
		v.Parms = parmsViewOf(ti.Parms, names)
	}
	if ti.HasStats && showStats {
		s := ti.Stats
		v.Stats = &NtStatsView{
			Allocs:         s.Allocs,
			Destroys:       s.Destroys,
			HashGrows:      s.HashGrows,
			ResFailed:      s.ResFailed,
			Lookups:        s.Lookups,
			Hits:           s.Hits,
			RcvProbesMcast: s.RcvProbesMcast,
			RcvProbesUcast: s.RcvProbesUcast,
			PeriodicGcRuns: s.PeriodicGcRuns,
			ForcedGcRuns:   s.ForcedGcRuns,
			TableFulls:     s.TableFulls,
		}
	}
	return v
}

func parmsViewOf(p xtcpnl.NeighTblParms, names NameTab) *NtParmsView {
	pv := &NtParmsView{}
	// print_ndtparams prints the dev line only for NDTPA_IFINDEX (:374); index 0
	// is the base message, which carries no such line.
	if p.Ifindex != 0 {
		pv.Dev = names.IndexToName(p.Ifindex)
	}
	if p.HasRefcnt {
		pv.Refcnt = &p.Refcnt
	}
	if p.HasReachableTime {
		pv.Reachable = &p.ReachableTime
	}
	if p.HasBaseReachableTime {
		pv.BaseReachable = &p.BaseReachableTime
	}
	if p.HasRetransTime {
		pv.Retrans = &p.RetransTime
	}
	if p.HasGcStaletime {
		pv.GcStale = &p.GcStaletime
	}
	if p.HasDelayProbeTime {
		pv.DelayProbe = &p.DelayProbeTime
	}
	if p.HasQueueLen {
		pv.Queue = &p.QueueLen
	}
	if p.HasAppProbes {
		pv.AppProbes = &p.AppProbes
	}
	if p.HasUcastProbes {
		pv.UcastProbes = &p.UcastProbes
	}
	if p.HasMcastProbes {
		pv.McastProbes = &p.McastProbes
	}
	if p.HasMcastReprobes {
		pv.McastReprobes = &p.McastReprobes
	}
	if p.HasAnycastDelay {
		pv.AnycastDelay = &p.AnycastDelay
	}
	if p.HasProxyDelay {
		pv.ProxyDelay = &p.ProxyDelay
	}
	if p.HasProxyQlen {
		pv.ProxyQueue = &p.ProxyQlen
	}
	if p.HasLocktime {
		pv.Locktime = &p.Locktime
	}
	return pv
}

// ndtStrtimeDelta reproduces ip/ipntable.c:310-336: a zero delta is "(error)",
// else the date of now minus (msec/1000) whole seconds, formatted "%Y-%m-%d %T".
// The result's location is now's, so obj_ntable's time.Now gives localtime (as
// iproute2 does) and the test's UTC instant reproduces the UTC-captured golden.
func ndtStrtimeDelta(now time.Time, msec uint32) string {
	if msec == 0 {
		return "(error)"
	}
	t := now.Add(-time.Duration(msec/1000) * time.Second)
	return t.Format("2006-01-02 15:04:05")
}

// Text renders one table, reproducing print_ntable's whitespace exactly:
// print_nl() is a newline, each token carries its own trailing space, the
// `_SL_` run-separators are "\n    ", and a trailing newline closes the record
// (:633).
func (v NeighTblView) Text() string {
	var b bytes.Buffer
	b.WriteString(v.Family)
	b.WriteByte(' ')
	if v.HasName {
		b.WriteString(v.Name)
		b.WriteByte(' ')
	}
	b.WriteByte('\n')

	if v.HasThresh1 || v.HasThresh2 || v.HasThresh3 || v.HasGcInterval {
		b.WriteString("    ")
		if v.HasThresh1 {
			fmt.Fprintf(&b, "thresh1 %d ", v.Thresh1)
		}
		if v.HasThresh2 {
			fmt.Fprintf(&b, "thresh2 %d ", v.Thresh2)
		}
		if v.HasThresh3 {
			fmt.Fprintf(&b, "thresh3 %d ", v.Thresh3)
		}
		if v.HasGcInterval {
			fmt.Fprintf(&b, "gc_int %d ", v.GcInterval)
		}
		b.WriteByte('\n')
	}

	if v.Config != nil {
		v.Config.writeText(&b)
	}
	if v.Parms != nil {
		v.Parms.writeText(&b)
	}
	if v.Stats != nil {
		v.Stats.writeText(&b)
	}
	b.WriteByte('\n')
	return b.String()
}

func (c *NtConfigView) writeText(b *bytes.Buffer) {
	fmt.Fprintf(b, "    config key_len %d ", c.KeyLen)
	fmt.Fprintf(b, "entry_size %d ", c.EntrySize)
	fmt.Fprintf(b, "entries %d ", c.Entries)
	b.WriteByte('\n')
	fmt.Fprintf(b, "        last_flush %s ", c.LastFlush)
	fmt.Fprintf(b, "last_rand %s ", c.LastRand)
	b.WriteByte('\n')
	fmt.Fprintf(b, "        hash_rnd %d ", c.HashRnd)
	fmt.Fprintf(b, "hash_mask %08x ", c.HashMask)
	fmt.Fprintf(b, "hash_chain_gc %d ", c.HashChainGc)
	fmt.Fprintf(b, "proxy_qlen %d ", c.ProxyQlen)
	b.WriteByte('\n')
}

func (p *NtParmsView) writeText(b *bytes.Buffer) {
	if p.Dev != "" {
		fmt.Fprintf(b, "    dev %s ", p.Dev)
		b.WriteByte('\n')
	}
	b.WriteString("    ")
	writeTokU32(b, "refcnt", p.Refcnt)
	writeTokU64(b, "reachable", p.Reachable)
	writeTokU64(b, "base_reachable", p.BaseReachable)
	writeTokU64(b, "retrans", p.Retrans)
	b.WriteString("\n    ")
	writeTokU64(b, "gc_stale", p.GcStale)
	writeTokU64(b, "delay_probe", p.DelayProbe)
	writeTokU32(b, "queue", p.Queue)
	b.WriteString("\n    ")
	writeTokU32(b, "app_probes", p.AppProbes)
	writeTokU32(b, "ucast_probes", p.UcastProbes)
	writeTokU32(b, "mcast_probes", p.McastProbes)
	writeTokU32(b, "mcast_reprobes", p.McastReprobes)
	b.WriteString("\n    ")
	writeTokU64(b, "anycast_delay", p.AnycastDelay)
	writeTokU64(b, "proxy_delay", p.ProxyDelay)
	writeTokU32(b, "proxy_queue", p.ProxyQueue)
	writeTokU64(b, "locktime", p.Locktime)
	b.WriteByte('\n')
}

func (s *NtStatsView) writeText(b *bytes.Buffer) {
	b.WriteString("    stats ")
	fmt.Fprintf(b, "allocs %d ", s.Allocs)
	fmt.Fprintf(b, "destroys %d ", s.Destroys)
	fmt.Fprintf(b, "hash_grows %d ", s.HashGrows)
	b.WriteString("\n    ")
	fmt.Fprintf(b, "res_failed %d ", s.ResFailed)
	fmt.Fprintf(b, "lookups %d ", s.Lookups)
	fmt.Fprintf(b, "hits %d ", s.Hits)
	b.WriteString("\n    ")
	fmt.Fprintf(b, "rcv_probes_mcast %d ", s.RcvProbesMcast)
	fmt.Fprintf(b, "rcv_probes_ucast %d ", s.RcvProbesUcast)
	b.WriteString("\n    ")
	fmt.Fprintf(b, "periodic_gc_runs %d ", s.PeriodicGcRuns)
	fmt.Fprintf(b, "forced_gc_runs %d ", s.ForcedGcRuns)
	b.WriteString("\n    ")
	fmt.Fprintf(b, "table_fulls %d ", s.TableFulls)
	b.WriteByte('\n')
}

func writeTokU32(b *bytes.Buffer, name string, v *uint32) {
	if v != nil {
		fmt.Fprintf(b, "%s %d ", name, *v)
	}
}

func writeTokU64(b *bytes.Buffer, name string, v *uint64) {
	if v != nil {
		fmt.Fprintf(b, "%s %d ", name, *v)
	}
}

// MarshalJSON writes the keys in print_ntable's emission order, one flat object
// per table (open_json_object(NULL), :580): the top-level fields, then the
// config, params and stats keys as siblings. Several JSON keys differ from their
// text token — gc_int/gc_interval (:618), key_len/key_length (:341) — and
// hash_mask is a `%#llx` string in JSON against a `%08x` number-looking token in
// text (print_color_0xhex, lib/json_print.c:271-275).
func (v NeighTblView) MarshalJSON() ([]byte, error) {
	w := newJSONObj()
	w.str("family", v.Family)
	if v.HasName {
		w.str("name", v.Name)
	}
	if v.HasThresh1 {
		w.num("thresh1", uint64(v.Thresh1))
	}
	if v.HasThresh2 {
		w.num("thresh2", uint64(v.Thresh2))
	}
	if v.HasThresh3 {
		w.num("thresh3", uint64(v.Thresh3))
	}
	if v.HasGcInterval {
		w.num("gc_interval", v.GcInterval)
	}
	if v.Config != nil {
		v.Config.marshal(w)
	}
	if v.Parms != nil {
		v.Parms.marshal(w)
	}
	if v.Stats != nil {
		v.Stats.marshal(w)
	}
	return w.done()
}

func (c *NtConfigView) marshal(w *jsonObj) {
	w.num("key_length", uint64(c.KeyLen))
	w.num("entry_size", uint64(c.EntrySize))
	w.num("entries", uint64(c.Entries))
	w.str("last_flush", c.LastFlush)
	w.str("last_rand", c.LastRand)
	w.num("hash_rnd", uint64(c.HashRnd))
	w.str("hash_mask", cHex(c.HashMask))
	w.num("hash_chain_gc", uint64(c.HashChainGc))
	w.num("proxy_qlen", uint64(c.ProxyQlen))
}

func (p *NtParmsView) marshal(w *jsonObj) {
	if p.Dev != "" {
		w.str("dev", p.Dev)
	}
	w.numP("refcnt", u64p(p.Refcnt))
	w.numP("reachable", p.Reachable)
	w.numP("base_reachable", p.BaseReachable)
	w.numP("retrans", p.Retrans)
	w.numP("gc_stale", p.GcStale)
	w.numP("delay_probe", p.DelayProbe)
	w.numP("queue", u64p(p.Queue))
	w.numP("app_probes", u64p(p.AppProbes))
	w.numP("ucast_probes", u64p(p.UcastProbes))
	w.numP("mcast_probes", u64p(p.McastProbes))
	w.numP("mcast_reprobes", u64p(p.McastReprobes))
	w.numP("anycast_delay", p.AnycastDelay)
	w.numP("proxy_delay", p.ProxyDelay)
	w.numP("proxy_queue", u64p(p.ProxyQueue))
	w.numP("locktime", p.Locktime)
}

func (s *NtStatsView) marshal(w *jsonObj) {
	w.num("allocs", s.Allocs)
	w.num("destroys", s.Destroys)
	w.num("hash_grows", s.HashGrows)
	w.num("res_failed", s.ResFailed)
	w.num("lookups", s.Lookups)
	w.num("hits", s.Hits)
	w.num("rcv_probes_mcast", s.RcvProbesMcast)
	w.num("rcv_probes_ucast", s.RcvProbesUcast)
	w.num("periodic_gc_runs", s.PeriodicGcRuns)
	w.num("forced_gc_runs", s.ForcedGcRuns)
	w.num("table_fulls", s.TableFulls)
}

// cHex mirrors C's "%#llx": "0x1f" for nonzero, bare "0" for zero (the # flag
// adds the prefix only to a nonzero value).
func cHex(v uint32) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprintf("%#x", v)
}

func u64p(p *uint32) *uint64 {
	if p == nil {
		return nil
	}
	x := uint64(*p)
	return &x
}

// jsonObj is an ordered JSON-object writer: keys are emitted in call order, each
// preceded by a comma after the first, so a view can reproduce print order
// without struct tags (the AddrLabelView.MarshalJSON technique).
type jsonObj struct {
	b     bytes.Buffer
	first bool
	err   error
}

func newJSONObj() *jsonObj {
	w := &jsonObj{first: true}
	w.b.WriteByte('{')
	return w
}

func (w *jsonObj) sep(key string) {
	if !w.first {
		w.b.WriteByte(',')
	}
	w.first = false
	w.b.WriteByte('"')
	w.b.WriteString(key)
	w.b.WriteString(`":`)
}

func (w *jsonObj) str(key, val string) {
	w.sep(key)
	raw, err := json.Marshal(val)
	if err != nil {
		w.err = err
		return
	}
	w.b.Write(raw)
}

func (w *jsonObj) num(key string, val uint64) {
	w.sep(key)
	w.b.WriteString(strconv.FormatUint(val, 10))
}

// boolean emits a JSON true/false, the type print_on_off/print_bool write under
// PRINT_JSON (lib/json_print.c) where the text form prints on/off.
func (w *jsonObj) boolean(key string, val bool) {
	w.sep(key)
	if val {
		w.b.WriteString("true")
		return
	}
	w.b.WriteString("false")
}

func (w *jsonObj) numP(key string, val *uint64) {
	if val == nil {
		return
	}
	w.num(key, *val)
}

func (w *jsonObj) done() ([]byte, error) {
	if w.err != nil {
		return nil, w.err
	}
	w.b.WriteByte('}')
	return w.b.Bytes(), nil
}
