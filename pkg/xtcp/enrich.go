package xtcp

import (
	"context"
	"log"
	"net/netip"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/dockermeta"
	"github.com/randomizedcoder/xtcp2/pkg/lldp"
	"github.com/randomizedcoder/xtcp2/pkg/nicinfo"
	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
	"github.com/randomizedcoder/xtcp2/pkg/strpolicy"
)

// enrichDialTimeout bounds the one-shot startup connections to the docker and
// lldpd sockets so a hung/absent daemon can't stall daemon startup — the whole
// point of the best-effort contract.
const enrichDialTimeout = 5 * time.Second

// maxUplinks is the number of uplink slots the flat record carries (uplink1_*,
// uplink2_*). Hosts are typically dual-homed.
const maxUplinks = 2

// uplinkColumns is one uplink slot's static (per-boot) NIC + LLDP columns,
// precomputed once at startup and copied verbatim into every record.
type uplinkColumns struct {
	ifname       string
	nicDriver    string
	nicModel     string
	nicBusInfo   string
	nicFwVersion string
	nicPCIVendor uint32
	nicPCIDevice uint32
	nicSpeedMbps uint32

	lldpChassisName string
	lldpChassisID   string
	lldpMgmtIP      string
	lldpPortID      string
	lldpPortDescr   string
}

// uplinkStamp holds both uplink slots' static columns plus an enabled gate so
// the hot-path apply is a cheap no-op when NIC/LLDP enrichment is off or found
// nothing.
type uplinkStamp struct {
	slots   [maxUplinks]uplinkColumns
	enabled bool
}

// apply copies the precomputed uplink columns onto a record. No-op unless at
// least one uplink slot was populated at startup.
func (s *uplinkStamp) apply(r *xtcp_flat_record.XtcpFlatRecord) {
	if !s.enabled {
		return
	}
	u1 := &s.slots[0]
	r.Uplink1Ifname = u1.ifname
	r.Uplink1NicDriver = u1.nicDriver
	r.Uplink1NicModel = u1.nicModel
	r.Uplink1NicBusInfo = u1.nicBusInfo
	r.Uplink1NicFwVersion = u1.nicFwVersion
	r.Uplink1NicPciVendor = u1.nicPCIVendor
	r.Uplink1NicPciDevice = u1.nicPCIDevice
	r.Uplink1NicSpeedMbps = u1.nicSpeedMbps
	r.Uplink1LldpChassisName = u1.lldpChassisName
	r.Uplink1LldpChassisId = u1.lldpChassisID
	r.Uplink1LldpMgmtIp = u1.lldpMgmtIP
	r.Uplink1LldpPortId = u1.lldpPortID
	r.Uplink1LldpPortDescr = u1.lldpPortDescr

	u2 := &s.slots[1]
	r.Uplink2Ifname = u2.ifname
	r.Uplink2NicDriver = u2.nicDriver
	r.Uplink2NicModel = u2.nicModel
	r.Uplink2NicBusInfo = u2.nicBusInfo
	r.Uplink2NicFwVersion = u2.nicFwVersion
	r.Uplink2NicPciVendor = u2.nicPCIVendor
	r.Uplink2NicPciDevice = u2.nicPCIDevice
	r.Uplink2NicSpeedMbps = u2.nicSpeedMbps
	r.Uplink2LldpChassisName = u2.lldpChassisName
	r.Uplink2LldpChassisId = u2.lldpChassisID
	r.Uplink2LldpMgmtIp = u2.lldpMgmtIP
	r.Uplink2LldpPortId = u2.lldpPortID
	r.Uplink2LldpPortDescr = u2.lldpPortDescr
}

// initEnrichers wires up the best-effort metadata enrichers gated by config.
// Every enricher degrades to leaving its columns empty on any failure; none can
// make the daemon fatal. Called once from Init, after the Prometheus vectors
// exist (see init.go) because every enricher reports its outcome through them.
//
// The one non-best-effort case is a compile-time-gated enricher (ASN,
// locality) that the operator asked for but that is not in this binary. That
// is a property of the artifact rather than of this host, so it is fatal
// before anything else runs — see enrich_core.go.
func (x *XTCP) initEnrichers(ctx context.Context) {
	if x.config == nil {
		return
	}
	if err := x.checkEnrichersCompiledIn(); err != nil {
		x.callFatalf("initEnrichers: %v", err)
		return
	}
	x.initDockerEnricher(ctx)
	x.initUplinkEnrichers(ctx)
	x.initGatedEnrichers(ctx)
}

// initDockerEnricher builds the netns-inode -> container index over the Docker
// Engine API. A dial/list failure disables container enrichment (counter + log)
// without touching the rest of the daemon.
func (x *XTCP) initDockerEnricher(ctx context.Context) {
	if !x.config.EnrichContainerEnable {
		return
	}
	dialCtx, cancel := context.WithTimeout(ctx, enrichDialTimeout)
	defer cancel()

	idx, err := dockermeta.New(dialCtx, x.config.DockerSocketPath)
	if err != nil {
		x.pC.WithLabelValues("initEnrichers", "docker", "error").Inc()
		log.Printf("initDockerEnricher: container enrichment disabled (best-effort): %v", err)
		return
	}
	x.dockerIndex = idx
	x.pC.WithLabelValues("initEnrichers", "docker", "enabled").Inc()
	if x.debugLevel > 10 {
		log.Printf("initDockerEnricher: container enrichment enabled (socket:%s)", x.config.DockerSocketPath)
	}
}

// uplinkSources are where the uplink enricher reads from. A nil field means
// the real source (see orDefault); tests script them.
type uplinkSources struct {
	defaultUplinks func(maxN int) []string
	physical       func(maxN int) []string
	collect        func(ifnames []string) []nicinfo.NIC
	fetchLLDP      func(ctx context.Context) (map[string]lldp.Neighbor, error)
}

// orDefault fills each nil source with the real one.
func (s uplinkSources) orDefault(x *XTCP) uplinkSources {
	if s.defaultUplinks == nil {
		s.defaultUplinks = nicinfo.DefaultUplinks
	}
	if s.physical == nil {
		s.physical = func(maxN int) []string { return nicinfo.PhysicalInterfaces("/sys", maxN) }
	}
	if s.collect == nil {
		s.collect = func(ifnames []string) []nicinfo.NIC { return nicinfo.Collect("/sys", ifnames) }
	}
	if s.fetchLLDP == nil {
		s.fetchLLDP = func(ctx context.Context) (map[string]lldp.Neighbor, error) {
			return lldp.Fetch(ctx, x.config.LldpdSocketPath, x.config.LldpdVersionHint)
		}
	}
	return s
}

// uplinkCount is the number of uplink slots to fill: uplink_count, clamped
// to 1..maxUplinks.
func uplinkCount(c *xtcp_config.XtcpConfig) int {
	count := int(c.GetUplinkCount())
	if count <= 0 || count > maxUplinks {
		count = maxUplinks
	}
	return count
}

// uplinkRefreshInterval is how often the auto-detected uplinks are re-read:
// locality_refresh_interval, or 0 (never) when NIC and LLDP enrichment are
// both off, the uplinks are pinned by uplink_interfaces, or the interval is
// not positive.
func uplinkRefreshInterval(c *xtcp_config.XtcpConfig) time.Duration {
	if !c.GetEnrichNicEnable() && !c.GetEnrichLldpEnable() {
		return 0
	}
	if len(c.GetUplinkInterfaces()) > 0 {
		return 0
	}
	return max(c.GetLocalityRefreshInterval().AsDuration(), 0)
}

// initUplinkEnrichers selects the uplink interfaces and captures their NIC +
// LLDP columns. Both sources are independently best-effort. Auto-detected
// uplinks (no uplink_interfaces) are then re-read every
// locality_refresh_interval, so a route change moves the uplink columns
// with it (see refreshUplinks).
func (x *XTCP) initUplinkEnrichers(ctx context.Context) {
	if !x.config.EnrichNicEnable && !x.config.EnrichLldpEnable {
		return
	}
	src := x.uplinkSrc.orDefault(x)
	count := uplinkCount(x.config)

	st := x.loadUplinks(ctx, src, count)

	if interval := uplinkRefreshInterval(x.config); interval > 0 {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			x.refreshUplinks(ctx, count, st, ticker.C)
		}()
	}
}

// selectUplinks picks up to count uplinks: the default-route interfaces, or
// when there are none (the default route lives on a separate management NIC,
// or none is configured) the physical interfaces in sysfs, so the NIC/LLDP
// columns still populate. The fallback is counted under function fn.
func (x *XTCP) selectUplinks(src uplinkSources, count int, fn string) []string {
	ifnames := src.defaultUplinks(count)
	if len(ifnames) == 0 {
		ifnames = src.physical(count)
		if len(ifnames) > 0 {
			x.pC.WithLabelValues(fn, "uplinks", "physical_fallback").Inc()
		}
	}
	if len(ifnames) > count {
		ifnames = ifnames[:count]
	}
	return ifnames
}

// uplinkState is what the refresh loop carries between ticks: the uplinks in
// service, and whether their LLDP columns are stale (the last lldpd read
// failed), so an unchanged list still retries lldpd.
type uplinkState struct {
	ifnames   []string
	lldpStale bool
}

// loadUplinks is initUplinkEnrichers' startup work: select the uplinks (or
// take the pinned ones), read their NIC + LLDP columns and store the stamp.
func (x *XTCP) loadUplinks(ctx context.Context, src uplinkSources, count int) uplinkState {
	ifnames := x.config.UplinkInterfaces
	if len(ifnames) == 0 {
		ifnames = x.selectUplinks(src, count, "initEnrichers")
	}
	if len(ifnames) > count {
		ifnames = ifnames[:count]
	}
	if len(ifnames) == 0 {
		x.pC.WithLabelValues("initEnrichers", "uplinks", "none").Inc()
		log.Printf("initUplinkEnrichers: no uplink interfaces detected (no override, no default route, no physical NIC in sysfs); NIC/LLDP columns left empty")
	} else {
		s, nics, neigh, err := x.buildStamp(ctx, src, ifnames)
		if x.config.EnrichNicEnable {
			x.pC.WithLabelValues("initEnrichers", "nic", "collected").Add(float64(nics))
		}
		if err != nil {
			x.pC.WithLabelValues("initEnrichers", "lldp", "error").Inc()
			log.Printf("initUplinkEnrichers: LLDP enrichment disabled (best-effort): %v", err)
		} else if x.config.EnrichLldpEnable {
			x.pC.WithLabelValues("initEnrichers", "lldp", "neighbors").Add(float64(neigh))
		}
		x.uplinkStamp.Store(&s)
		log.Printf("initUplinkEnrichers: uplinks=%v nics=%d lldpNeighbors=%d", ifnames, nics, neigh)
		return uplinkState{ifnames: ifnames, lldpStale: err != nil}
	}
	return uplinkState{ifnames: ifnames}
}

// buildStamp reads the NIC and LLDP columns of ifnames from sysfs and lldpd
// (each only when enabled) and returns the stamp, the NIC and neighbor
// counts, and the lldpd error, if any. The stamp keeps the NIC columns when
// lldpd fails.
func (x *XTCP) buildStamp(ctx context.Context, src uplinkSources, ifnames []string) (uplinkStamp, int, int, error) {
	var nics []nicinfo.NIC
	if x.config.EnrichNicEnable {
		nics = src.collect(ifnames)
	}
	var (
		neigh map[string]lldp.Neighbor
		err   error
	)
	if x.config.EnrichLldpEnable {
		fetchCtx, cancel := context.WithTimeout(ctx, enrichDialTimeout)
		neigh, err = src.fetchLLDP(fetchCtx)
		cancel()
		if err != nil {
			neigh = nil
		}
	}
	return buildUplinkStamp(ifnames, nics, neigh), len(nics), len(neigh), err
}

// refreshUplinks is initUplinkEnrichers' background loop: on every tick it
// re-reads the auto-detected uplinks (refreshUplinksOnce), starting from st.
// Runs until ctx is canceled.
func (x *XTCP) refreshUplinks(ctx context.Context, count int, st uplinkState, tick <-chan time.Time) {
	src := x.uplinkSrc.orDefault(x)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
			st = x.refreshUplinksSrc(ctx, src, count, st)
		}
	}
}

// refreshUplinksOnce re-selects the uplinks and returns the state now in
// service. The same list as before, with good LLDP columns, costs only the
// route and lower-device reads of DefaultUplinks: no NIC identity read, no
// lldpd, no new stamp. A different list, or the same one whose last lldpd
// read failed, is read in full and its stamp replaces the old one (an empty
// stamp when no uplink is left). Outcomes are counted under
// function="refreshUplinks": unchanged, changed, lldp_recovered (an
// unchanged list whose retried lldpd read succeeded), or lldp_error (lldpd
// failed: the NIC columns are kept and the next tick retries).
func (x *XTCP) refreshUplinksOnce(ctx context.Context, count int, st uplinkState) uplinkState {
	return x.refreshUplinksSrc(ctx, x.uplinkSrc.orDefault(x), count, st)
}

func (x *XTCP) refreshUplinksSrc(ctx context.Context, src uplinkSources, count int, st uplinkState) uplinkState {
	ifnames := x.selectUplinks(src, count, "refreshUplinks")
	same := slices.Equal(ifnames, st.ifnames)
	if same && !st.lldpStale {
		x.pC.WithLabelValues("refreshUplinks", "uplinks", "unchanged").Inc()
		return st
	}
	s := uplinkStamp{}
	outcome := "changed"
	if same {
		outcome = "lldp_recovered"
	}
	var err error
	if len(ifnames) > 0 {
		s, _, _, err = x.buildStamp(ctx, src, ifnames)
		if err != nil {
			outcome = "lldp_error"
			log.Printf("refreshUplinks: LLDP columns left empty, retrying next tick (best-effort): %v", err)
		}
	}
	x.uplinkStamp.Store(&s)
	x.pC.WithLabelValues("refreshUplinks", "uplinks", outcome).Inc()
	if !same {
		log.Printf("refreshUplinks: uplinks %v -> %v", st.ifnames, ifnames)
	}
	return uplinkState{ifnames: ifnames, lldpStale: err != nil}
}

// buildUplinkStamp folds the per-interface NIC + LLDP lookups into the flat
// per-slot columns. Slot i corresponds to ifnames[i]. Any missing NIC/neighbor
// simply leaves those columns empty.
func buildUplinkStamp(ifnames []string, nics []nicinfo.NIC, neigh map[string]lldp.Neighbor) uplinkStamp {
	nicByName := make(map[string]nicinfo.NIC, len(nics))
	for _, n := range nics {
		nicByName[n.Ifname] = n
	}

	var s uplinkStamp
	for i := 0; i < len(ifnames) && i < maxUplinks; i++ {
		name := ifnames[i]
		col := &s.slots[i]
		col.ifname = strpolicy.Clean(name, strpolicy.MaxIfname)
		// NIC strings come from the driver and firmware, LLDP ones from the
		// link peer: clean each once per refresh, not per row.
		if n, ok := nicByName[name]; ok {
			col.nicDriver = strpolicy.Clean(n.Driver, strpolicy.MaxLen)
			col.nicModel = strpolicy.Clean(n.Model, strpolicy.MaxLen)
			col.nicBusInfo = strpolicy.Clean(n.BusInfo, strpolicy.MaxLen)
			col.nicFwVersion = strpolicy.Clean(n.FwVersion, strpolicy.MaxLen)
			col.nicPCIVendor = n.PCIVendor
			col.nicPCIDevice = n.PCIDevice
			col.nicSpeedMbps = n.SpeedMbps
		}
		if nb, ok := neigh[name]; ok {
			col.lldpChassisName = strpolicy.Clean(nb.ChassisName, strpolicy.MaxLen)
			col.lldpChassisID = strpolicy.Clean(nb.ChassisID, strpolicy.MaxLen)
			col.lldpMgmtIP = strpolicy.Clean(nb.MgmtIP, strpolicy.MaxLen)
			col.lldpPortID = strpolicy.Clean(nb.PortID, strpolicy.MaxLen)
			col.lldpPortDescr = strpolicy.Clean(nb.PortDescr, strpolicy.MaxLen)
		}
		s.enabled = true
	}
	return s
}

// applyEnrichment stamps the best-effort metadata onto a record on the
// deserialize hot path. It reads the record's already-set NetnsInode to resolve
// the owning container and (opt-in) nsid, and copies the static uplink columns.
// All lookups are O(1) and no-op when their enricher is disabled.
func (x *XTCP) applyEnrichment(r *xtcp_flat_record.XtcpFlatRecord) {
	if s := x.uplinkStamp.Load(); s != nil {
		s.apply(r)
	}

	if x.dockerIndex != nil {
		if c, ok := x.dockerIndex.Lookup(r.NetnsInode); ok {
			r.ContainerId = c.ID
			r.ContainerRuntime = "docker"
			r.ContainerName = c.Name
			r.ContainerImage = c.Image
			// Upgrade an anonymous synthetic netns label ("netns:[<inode>]") to
			// the container name, which is far more useful for grouping.
			if c.Name != "" && strings.HasPrefix(r.Netns, "netns:[") {
				r.Netns = c.Name
			}
		}
	}

	if m := x.nsidByInode.Load(); m != nil {
		if id, ok := (*m)[r.NetnsInode]; ok {
			r.Nsid = uint32(id)
			r.NsidAssigned = true
		}
	}

	// Neither destination enricher installed (not compiled in, disabled, or —
	// for locality — no snapshot ever published): skip the address conversion
	// so disabled mode is a true no-op on the hot path. Both fields are
	// interfaces, so in a build without the tags they are permanently nil and
	// the branch predicts perfectly.
	localityOn := x.locality != nil && x.locality.Active()
	if x.asn == nil && !localityOn {
		return
	}

	if addr, ok := destAddr(r.InetDiagMsgFamily, r.InetDiagMsgSocketDestination); ok {
		// Classify the destination's locality first. remote defaults to true so
		// that with locality disabled, or with no snapshot for this namespace,
		// the ASN lookup runs exactly as before. A self / connected-subnet
		// destination is tagged and skips the internet ASN feed. The same snapshot
		// resolves the destination's egress interface (from the matched route's
		// Oif) and the socket's own bound interface (kernel idiag_if, field 1009).
		remote := true
		if localityOn {
			if res, found := x.locality.Resolve(r.NetnsInode, addr, r.InetDiagMsgSocketInterface); found {
				r.EnrichSocketDestLocality = res.Locality
				r.EnrichSocketDestEgressIfindex = res.EgressIfindex
				r.EnrichSocketDestEgressIfname = res.EgressIfname
				r.EnrichSocketInterfaceName = res.BoundIfname
				remote = res.Remote
			}
		}

		if remote && x.asn != nil {
			if asn, owner, found := x.asn.LookupAsn(addr); found {
				r.EnrichSocketDestAsn = uint64(asn)
				r.EnrichSocketDestNetworkOwner = owner
			}
		}
	}
}

// destAddr converts the kernel's 16-byte __be32[4] destination slot to a
// netip.Addr, alloc-free. family is authoritative: the kernel stores an IPv4
// address in the first 4 bytes of the 16-byte slot (rest zero), so it must not
// be read as IPv6. Returns ok=false for a short/absent buffer or unknown family.
func destAddr(family uint32, b []byte) (netip.Addr, bool) {
	switch family {
	case unix.AF_INET:
		if len(b) >= 4 {
			return netip.AddrFrom4([4]byte(b[:4])), true
		}
	case unix.AF_INET6:
		if len(b) >= 16 {
			return netip.AddrFrom16([16]byte(b[:16])).Unmap(), true
		}
	}
	return netip.Addr{}, false
}

// refreshNsids rebuilds the opt-in netns-inode -> nsid snapshot for the current
// namespace set and publishes it atomically for the stamping path. Each nsid is
// queried via RTM_GETNSID against a freshly-opened handle (the bind-mount path
// when available, else /proc/<pid>/ns/net). Best-effort throughout: an
// unopenable handle or an unassigned id is simply omitted (stamps 0). Called
// only from the single-owner reconcile path (discoverNamespaces).
func (x *XTCP) refreshNsids(nss map[uint64]nsIdentity) {
	m := make(map[uint64]int32, len(nss))
	for inode, id := range nss {
		handle := id.path
		if handle == "" {
			handle = procNsPath(id.pid)
		}
		fd, err := unix.Open(handle, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		if nsid, ok := nsdiscover.Nsid(fd); ok {
			m[inode] = nsid
		}
		if err := unix.Close(fd); err != nil {
			// Best-effort: a failed close of a read-only handle is not
			// recoverable here, but surface it for debugging.
			log.Printf("refreshNsids: close ns handle: %v", err)
		}
	}
	x.nsidByInode.Store(&m)
	x.pC.WithLabelValues("refreshNsids", "assigned", "counter").Add(float64(len(m)))
}

// closeEnrichers releases enricher resources at shutdown (best-effort).
func (x *XTCP) closeEnrichers() {
	if x.dockerIndex != nil {
		if err := x.dockerIndex.Close(); err != nil && x.debugLevel > 10 {
			log.Printf("closeEnrichers: docker index close: %v", err)
		}
	}
}
