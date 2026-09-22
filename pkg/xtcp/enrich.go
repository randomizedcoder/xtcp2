package xtcp

import (
	"context"
	"log"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/dockermeta"
	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
	"github.com/randomizedcoder/xtcp2/pkg/lldp"
	"github.com/randomizedcoder/xtcp2/pkg/nicinfo"
	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
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
// make the daemon fatal. Called once from Init.
func (x *XTCP) initEnrichers(ctx context.Context) {
	if x.config == nil {
		return
	}
	x.initDockerEnricher(ctx)
	x.initUplinkEnrichers(ctx)
	x.initAsnEnricher(ctx)
	x.initLocalityEnricher()
}

// initAsnEnricher wires destination IP -> {ASN, network owner} enrichment from
// the ipfeed-collector Parquet artifact (pkg/ipasn, an in-process
// longest-prefix-match trie).
//
// The index is always created and the first load attempted; the outcome only
// decides how failure is handled:
//   - load ok: enrichment is live, and when asn_refresh_interval > 0 a
//     background goroutine re-stats the file every interval and rebuilds the
//     trie only when its size/mtime changed (ipasn.ReloadIfChanged);
//   - load failed, interval > 0: the (empty) index is still installed and the
//     same goroutine retries on every tick, so an artifact that arrives after
//     the daemon started — or a refreshed one — is picked up without a restart;
//     lookups miss until then;
//   - load failed, interval <= 0: nothing would ever load, so enrichment stays
//     disabled (asnIndex nil) exactly as before.
//
// A failed reload never touches the trie in service. Outcomes are counted
// under function="initEnrichers"/"refreshAsn"; the table itself (entries,
// artifact size, load time, build duration) is published by loadAsn.
func (x *XTCP) initAsnEnricher(ctx context.Context) {
	if !x.config.EnrichAsnEnable {
		return
	}
	path := x.config.AsnDbPath
	if path == "" {
		x.pC.WithLabelValues("initEnrichers", "asn", "error").Inc()
		log.Printf("initAsnEnricher: ASN enrichment disabled (best-effort): asn_db_path is empty")
		return
	}
	interval := x.config.GetAsnRefreshInterval().AsDuration()

	idx := &ipasn.Index{}
	if _, err := x.loadAsn(idx, path, true); err != nil {
		x.pC.WithLabelValues("initEnrichers", "asn", "error").Inc()
		if interval <= 0 {
			log.Printf("initAsnEnricher: ASN enrichment disabled (best-effort, asn_refresh_interval is 0 so it will not retry): %v", err)
			return
		}
		log.Printf("initAsnEnricher: ASN artifact not loaded (will retry every %s; lookups miss until then): %v", interval, err)
	} else {
		x.pC.WithLabelValues("initEnrichers", "asn", "enabled").Inc()
		if x.debugLevel > 10 {
			log.Printf("initAsnEnricher: ASN enrichment enabled (db:%s prefixes:%d)", path, idx.Len())
		}
	}
	x.asnIndex = idx

	if interval <= 0 {
		return // load-once; no background refresh
	}
	go x.refreshAsn(ctx, idx, path, interval)
}

// refreshAsn is initAsnEnricher's background loop: every interval it asks the
// index to reload path if the file changed (or was never loaded). Runs until
// ctx is cancelled.
func (x *XTCP) refreshAsn(ctx context.Context, idx *ipasn.Index, path string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reloaded, err := x.loadAsn(idx, path, false)
			switch {
			case err != nil:
				x.pC.WithLabelValues("refreshAsn", "reload", "error").Inc()
				log.Printf("initAsnEnricher: ASN reload failed (keeping current table): %v", err)
			case !reloaded:
				x.pC.WithLabelValues("refreshAsn", "reload", "unchanged").Inc()
			default:
				x.pC.WithLabelValues("refreshAsn", "reload", "ok").Inc()
				if x.debugLevel > 10 {
					log.Printf("initAsnEnricher: ASN artifact reloaded (db:%s prefixes:%d)", path, idx.Len())
				}
			}
		}
	}
}

// loadAsn runs one load attempt against idx — forced (start-up) or stat-gated
// (refresh tick) — and publishes what an operator needs to see about the ASN
// lookup table, all under function="loadAsn" so the start-up load and every
// refresh land on the same series:
//
//   - gauges prefixes (entries in the trie in service), artifactBytes (size of
//     the Parquet file it was built from) and loadedAt (unix seconds of the
//     last successful load, so `time() - loadedAt` is the table's age);
//   - summary build/duration — read + trie build of a successful load — and
//     error/duration — how long a failed attempt took before giving up.
//
// A stat-gated attempt that finds the artifact unchanged publishes nothing: the
// gauges already describe the table in service. The bool reports whether a
// new table was swapped in.
func (x *XTCP) loadAsn(idx *ipasn.Index, path string, force bool) (reloaded bool, err error) {
	start := time.Now()
	if force {
		err = idx.Reload(path)
		reloaded = err == nil
	} else {
		reloaded, err = idx.ReloadIfChanged(path)
	}
	if err != nil {
		x.pH.WithLabelValues("loadAsn", "error", "duration").Observe(time.Since(start).Seconds())
		return false, err
	}
	if !reloaded {
		return false, nil
	}
	st := idx.Stats()
	x.pGV.WithLabelValues("loadAsn", "prefixes", "gauge").Set(float64(st.Prefixes))
	x.pGV.WithLabelValues("loadAsn", "artifactBytes", "gauge").Set(float64(st.ArtifactBytes))
	x.pGV.WithLabelValues("loadAsn", "loadedAt", "gauge").Set(float64(st.LoadedAt.Unix()))
	x.pH.WithLabelValues("loadAsn", "build", "duration").Observe(st.BuildDuration.Seconds())
	return true, nil
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

// initUplinkEnrichers selects the uplink interfaces and captures the static NIC
// + LLDP columns once. Both sources are independently best-effort.
func (x *XTCP) initUplinkEnrichers(ctx context.Context) {
	if !x.config.EnrichNicEnable && !x.config.EnrichLldpEnable {
		return
	}

	count := int(x.config.UplinkCount)
	if count <= 0 {
		count = maxUplinks
	}
	if count > maxUplinks {
		count = maxUplinks
	}

	ifnames := x.config.UplinkInterfaces
	if len(ifnames) == 0 {
		ifnames = nicinfo.DefaultUplinks(count)
	}
	if len(ifnames) == 0 {
		// No explicit override and no default-route uplink (e.g. the default
		// route lives on a separate management NIC, or none is configured).
		// Fall back to the physical interfaces present in sysfs so NIC/LLDP
		// columns still populate.
		ifnames = nicinfo.PhysicalInterfaces("/sys", count)
		if len(ifnames) > 0 {
			x.pC.WithLabelValues("initEnrichers", "uplinks", "physical_fallback").Inc()
		}
	}
	if len(ifnames) > count {
		ifnames = ifnames[:count]
	}
	if len(ifnames) == 0 {
		x.pC.WithLabelValues("initEnrichers", "uplinks", "none").Inc()
		log.Printf("initUplinkEnrichers: no uplink interfaces detected (no override, no default route, no physical NIC in sysfs); NIC/LLDP columns left empty")
		return
	}

	var nics []nicinfo.NIC
	if x.config.EnrichNicEnable {
		nics = nicinfo.Collect("/sys", ifnames)
		x.pC.WithLabelValues("initEnrichers", "nic", "collected").Add(float64(len(nics)))
	}

	var neigh map[string]lldp.Neighbor
	if x.config.EnrichLldpEnable {
		fetchCtx, cancel := context.WithTimeout(ctx, enrichDialTimeout)
		n, err := lldp.Fetch(fetchCtx, x.config.LldpdSocketPath, x.config.LldpdVersionHint)
		cancel()
		if err != nil {
			x.pC.WithLabelValues("initEnrichers", "lldp", "error").Inc()
			log.Printf("initUplinkEnrichers: LLDP enrichment disabled (best-effort): %v", err)
		} else {
			neigh = n
			x.pC.WithLabelValues("initEnrichers", "lldp", "neighbors").Add(float64(len(neigh)))
		}
	}

	x.uplinkStamp = buildUplinkStamp(ifnames, nics, neigh)
	log.Printf("initUplinkEnrichers: uplinks=%v nics=%d lldpNeighbors=%d", ifnames, len(nics), len(neigh))
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
		col.ifname = name
		if n, ok := nicByName[name]; ok {
			col.nicDriver = n.Driver
			col.nicModel = n.Model
			col.nicBusInfo = n.BusInfo
			col.nicFwVersion = n.FwVersion
			col.nicPCIVendor = n.PCIVendor
			col.nicPCIDevice = n.PCIDevice
			col.nicSpeedMbps = n.SpeedMbps
		}
		if nb, ok := neigh[name]; ok {
			col.lldpChassisName = nb.ChassisName
			col.lldpChassisID = nb.ChassisID
			col.lldpMgmtIP = nb.MgmtIP
			col.lldpPortID = nb.PortID
			col.lldpPortDescr = nb.PortDescr
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
	x.uplinkStamp.apply(r)

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
		}
	}

	// Neither destination enricher enabled (no ASN index, no locality snapshot
	// ever published): skip the address conversion so disabled mode is a true
	// no-op on the hot path.
	if x.asnIndex == nil && x.localityByInode.Load() == nil {
		return
	}

	if addr, ok := destAddr(r.InetDiagMsgFamily, r.InetDiagMsgSocketDestination); ok {
		// Classify the destination's locality first. remote defaults to true so
		// that with locality disabled (nil map) or no snapshot for this namespace
		// the ASN lookup runs exactly as before. A self / connected-subnet
		// destination is tagged and skips the internet ASN feed. The same snapshot
		// resolves the destination's egress interface (from the matched route's
		// Oif) and the socket's own bound interface (kernel idiag_if, field 1009).
		remote := true
		if m := x.localityByInode.Load(); m != nil {
			if snap := (*m)[r.NetnsInode]; snap != nil {
				res := snap.Resolve(addr, r.InetDiagMsgSocketInterface)
				r.EnrichSocketDestLocality = xtcp_flat_record.XtcpFlatRecord_Locality(res.Locality)
				r.EnrichSocketDestEgressIfindex = res.EgressIfindex
				r.EnrichSocketDestEgressIfname = res.EgressIfname
				r.EnrichSocketInterfaceName = res.BoundIfname
				remote = res.Remote
			}
		}

		if remote && x.asnIndex != nil {
			if a, found := x.asnIndex.Lookup(addr); found {
				r.EnrichSocketDestAsn = uint64(a.ASN)
				r.EnrichSocketDestNetworkOwner = a.NetworkOwner
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
