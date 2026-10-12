package xtcpnl

import (
	"bufio"
	"bytes"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// lacpStates are the link-write readback states, each with its own link dump
// and its own sysfs oracle: Tier 1's three on veth ports, then Tier 2's three
// on virtio-net ports with the partner in a second VM. portKind is the
// ports' own IFLA_INFO_KIND: veth has rtnl_link_ops, virtio_net does not, so
// a Tier 2 port carries only the slave half of IFLA_LINKINFO.
var lacpStates = []struct {
	name     string
	dir      string
	getlink  string
	portKind string
}{
	{"lacp_no_partner", tdLinkWrite_7_1_4, tdLacpNoPartnerGetLink_7_1_4, "veth"},
	{"lacp_converged", tdLinkWrite_7_1_4, tdLacpConvergedGetLink_7_1_4, "veth"},
	{"lacp_partner_down", tdLinkWrite_7_1_4, tdLacpPartnerDownGetLink_7_1_4, "veth"},
	{"lacp-pair/lacp_converged", tdLacpPair_7_1_4, tdLacpPairConvergedGetLink_7_1_4, ""},
	{"lacp-pair/lacp_link_pulled", tdLacpPair_7_1_4, tdLacpPairPulledGetLink_7_1_4, ""},
	{"lacp-pair/lacp_link_restored", tdLacpPair_7_1_4, tdLacpPairRestoredGetLink_7_1_4, ""},
}

// lacpLinksByName decodes one state's link dump, keyed by name. The first
// reply for a name wins, so a side lookup `ip` made during the dump cannot
// shadow the dump's own reply.
func lacpLinksByName(t *testing.T, path string) map[string]LinkInfo {
	t.Helper()
	links, done := linksIn(t, path)
	if !done {
		t.Fatalf("%s: dump not terminated by NLMSG_DONE", path)
	}
	by := make(map[string]LinkInfo, len(links))
	for i := range links {
		if _, ok := by[links[i].Name]; !ok {
			by[links[i].Name] = links[i]
		}
	}
	return by
}

func mustMAC(t *testing.T, s string) []byte {
	t.Helper()
	hw, err := net.ParseMAC(s)
	if err != nil {
		t.Fatalf("ParseMAC(%q): %v", s, err)
	}
	return hw
}

// TestDecodeBondRealFixture pins the bond master's decode against the
// captured LACP states, both tiers. Every expected value is transcribed from the state's
// ip_link_n sidecar (`bond mode 802.3ad miimon 100 … ad_partner_mac …`).
//
// The rows that differ between states are the point: AD_INFO is present in
// all three, and what changes is the partner (zero MAC and key 1 with no
// partner, 02:…:0b and key 15 once converged) and the port count (1, 2, 1).
//
// go test ./pkg/xtcpnl/ -run TestDecodeBondRealFixture
func TestDecodeBondRealFixture(t *testing.T) {
	type adWant struct {
		agg, ports, actorKey, partnerKey uint16
		partnerMAC                       string
	}
	tests := []struct {
		description string
		getlink     string
		expected    adWant
	}{
		{
			description: "boundary: no partner still has AD_INFO - one-port aggregator, partner MAC zero, partner key from the admin defaults",
			getlink:     tdLacpNoPartnerGetLink_7_1_4,
			expected:    adWant{1, 1, 15, 1, "00:00:00:00:00:00"},
		},
		{
			description: "positive: converged - both ports in aggregator 1, partner is lacpb's ad_actor_system with the same key",
			getlink:     tdLacpConvergedGetLink_7_1_4,
			expected:    adWant{1, 2, 15, 15, "02:00:00:00:00:0b"},
		},
		{
			description: "positive: partner down - the aggregator keeps its partner and drops to one port",
			getlink:     tdLacpPartnerDownGetLink_7_1_4,
			expected:    adWant{1, 1, 15, 15, "02:00:00:00:00:0b"},
		},
		{
			description: "positive: tier 2 converged - the same aggregator as tier 1, the partner now a second VM",
			getlink:     tdLacpPairConvergedGetLink_7_1_4,
			expected:    adWant{1, 2, 15, 15, "02:00:00:00:00:0b"},
		},
		{
			description: "positive: tier 2 link pulled - one port left, partner kept",
			getlink:     tdLacpPairPulledGetLink_7_1_4,
			expected:    adWant{1, 1, 15, 15, "02:00:00:00:00:0b"},
		},
		{
			description: "positive: tier 2 link restored - back to two ports in the same aggregator",
			getlink:     tdLacpPairRestoredGetLink_7_1_4,
			expected:    adWant{1, 2, 15, 15, "02:00:00:00:00:0b"},
		},
	}

	// The options the topology set, and the defaults it did not, are the same
	// in every state: link-write-topology.exp bond_args.
	optWant := []struct {
		id   uint16
		u    uint32
		name string
	}{
		{unix.IFLA_BOND_MODE, 4, "802.3ad"},
		{unix.IFLA_BOND_MIIMON, 100, ""},
		{unix.IFLA_BOND_AD_LACP_RATE, 1, "fast"},
		{unix.IFLA_BOND_XMIT_HASH_POLICY, 0, "layer2"},
		{unix.IFLA_BOND_AD_SELECT, 0, "stable"},
		{unix.IFLA_BOND_AD_LACP_ACTIVE, 1, "on"},
		{unix.IFLA_BOND_ARP_VALIDATE, 0, "none"},
		{unix.IFLA_BOND_MISSED_MAX, 2, ""},
		{unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, 65535, ""},
		{unix.IFLA_BOND_COUPLED_CONTROL, 1, "on"},
		{IflaBondBroadcastNeigh, 0, "off"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			by := lacpLinksByName(t, tc.getlink)
			b := by["bond0"].Bond
			if b == nil {
				t.Fatalf("bond0.Bond = nil")
			}
			if len(b.Unknown) != 0 {
				t.Errorf("Unknown = %v, want none: the 7.1.4 kernel sends nothing BondOptions lacks", b.Unknown)
			}
			for _, o := range optWant {
				v := b.Value(o.id)
				spec := BondOptionSpecByID(o.id)
				if !v.Present || v.U != o.u {
					t.Errorf("%s = %+v, want %d present", spec.Keyword, v, o.u)
				}
				if got := spec.ValueName(v.U); got != o.name {
					t.Errorf("%s name = %q, want %q", spec.Keyword, got, o.name)
				}
			}
			if v := b.Value(unix.IFLA_BOND_AD_ACTOR_SYSTEM); !bytes.Equal(v.Bytes, mustMAC(t, "02:00:00:00:00:0a")) {
				t.Errorf("ad_actor_system = %x", v.Bytes)
			}
			for _, absent := range []uint16{unix.IFLA_BOND_ACTIVE_SLAVE, unix.IFLA_BOND_PRIMARY, IflaBondLacpStrict} {
				if b.Value(absent).Present {
					t.Errorf("%s present, want absent (802.3ad has no active slave or primary; 7.1.4 predates lacp_strict)",
						BondOptionSpecByID(absent).Keyword)
				}
			}
			if b.ArpIPTargets != nil || b.NsIP6Targets != nil {
				t.Errorf("targets = %v / %v, want nil: none configured, so the kernel omits both nests", b.ArpIPTargets, b.NsIP6Targets)
			}

			ai := b.AdInfo
			if ai == nil {
				t.Fatalf("AdInfo = nil")
			}
			got := adWant{ai.Aggregator.Value, ai.NumPorts.Value, ai.ActorKey.Value, ai.PartnerKey.Value, net.HardwareAddr(ai.PartnerMAC).String()}
			if got != tc.expected {
				t.Errorf("AdInfo = %+v, want %+v", got, tc.expected)
			}
			if !ai.Aggregator.Present || !ai.NumPorts.Present || !ai.ActorKey.Present || !ai.PartnerKey.Present {
				t.Errorf("AdInfo has an absent field: %+v", *ai)
			}
		})
	}
}

// TestDecodeBondSlaveRealFixture pins each port's decode in each state,
// transcribed from the `bond_slave …` line of the state's ip_link_n sidecar.
//
// lacp_partner_down's la2 row is the one this corpus exists for: no carrier,
// mii_status DOWN, a backup slave - and actor and partner state still 0x3f.
// The LACP bits on a dead port are stale (link-write-path.md, "Phase 0 as
// built"), so a consumer that reads "in sync" off them alone is wrong.
// lacp_link_pulled's la2 is the same row on a virtio-net port whose carrier
// went down as a device event: the bits stay 0x3f there too, so the staleness
// is the bond's and not an artifact of how veth drops carrier.
//
// Tier 2's la2 has one link failure where Tier 1's had two: Tier 1's veth
// ports also lost carrier while lacpb's ends were down in lacp_no_partner.
// Both drivers wait out the 60 s churn detection timer before each state
// (lwtopo::wait_churn), so no row reads AD_CHURN_MONITOR (0). A converged
// port, or one whose partner went away after converging, ends at
// AD_NO_CHURN (2). lacp_no_partner's silent partner never syncs, so both
// partner machines end at AD_CHURN (1), and so does la2's actor, which is
// out of sync in its own aggregator; la1's actor is in sync and ends at 2.
//
// go test ./pkg/xtcpnl/ -run TestDecodeBondSlaveRealFixture
func TestDecodeBondSlaveRealFixture(t *testing.T) {
	type slaveWant struct {
		state, mii           uint8
		failures             uint32
		agg                  uint16
		actor                uint8
		partner              uint16
		churnActor, churnPar uint8
		perm                 string
	}
	tests := []struct {
		description string
		getlink     string
		port        string
		expected    slaveWant
	}{
		{"positive: no partner, la1 is the active port, in sync with the defaulted partner (0x4f); its actor settles, its partner churns",
			tdLacpNoPartnerGetLink_7_1_4, "la1", slaveWant{0, 0, 0, 1, 0x4f, 0, 2, 1, "02:00:00:00:0a:01"}},
		{"corner: no partner, la2 is a backup in its own aggregator 2 (0x47, no SYNC), so both its churn machines churn",
			tdLacpNoPartnerGetLink_7_1_4, "la2", slaveWant{1, 0, 0, 2, 0x47, 0, 1, 1, "02:00:00:00:0a:02"}},
		{"positive: converged la1 - actor and partner 0x3f, the partner decoded at the kernel's u16 width",
			tdLacpConvergedGetLink_7_1_4, "la1", slaveWant{0, 0, 1, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:01"}},
		{"positive: converged la2 - same aggregator as la1, both active",
			tdLacpConvergedGetLink_7_1_4, "la2", slaveWant{0, 0, 1, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:02"}},
		{"positive: partner down, la1 untouched",
			tdLacpPartnerDownGetLink_7_1_4, "la1", slaveWant{0, 0, 1, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:01"}},
		{"corner: partner down, la2 mii DOWN (2) and backup with a second failure, LACP bits still 0x3f",
			tdLacpPartnerDownGetLink_7_1_4, "la2", slaveWant{1, 2, 2, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:02"}},
		{"positive: tier 2 converged la1 - 0x3f both ends, no failures: the ports had no carrier to lose before",
			tdLacpPairConvergedGetLink_7_1_4, "la1", slaveWant{0, 0, 0, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:01"}},
		{"positive: tier 2 converged la2",
			tdLacpPairConvergedGetLink_7_1_4, "la2", slaveWant{0, 0, 0, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:02"}},
		{"positive: tier 2 link pulled, la1 untouched",
			tdLacpPairPulledGetLink_7_1_4, "la1", slaveWant{0, 0, 0, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:01"}},
		{"corner: tier 2 link pulled, la2 mii DOWN and backup with one failure, LACP bits still 0x3f",
			tdLacpPairPulledGetLink_7_1_4, "la2", slaveWant{1, 2, 1, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:02"}},
		{"positive: tier 2 link restored, la1 untouched",
			tdLacpPairRestoredGetLink_7_1_4, "la1", slaveWant{0, 0, 0, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:01"}},
		{"boundary: tier 2 link restored, la2 active again and the failure stays counted",
			tdLacpPairRestoredGetLink_7_1_4, "la2", slaveWant{0, 0, 1, 1, 0x3f, 0x3f, 2, 2, "02:00:00:00:0a:02"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li := lacpLinksByName(t, tc.getlink)[tc.port]
			s := li.BondSlave
			if s == nil {
				t.Fatalf("%s.BondSlave = nil (SlaveKind %q)", tc.port, li.SlaveKind)
			}
			got := slaveWant{s.State.Value, s.MiiStatus.Value, s.LinkFailureCount.Value, s.AdAggregatorID.Value,
				s.AdActorOperState.Value, s.AdPartnerOperState.Value, s.AdChurnActorState.Value, s.AdChurnPartnerState.Value,
				net.HardwareAddr(s.PermHwaddr).String()}
			if got != tc.expected {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
			// Every attribute bond_fill_slave_info sends for an 802.3ad port
			// with an aggregator must have arrived; a missing one would decode
			// as a zero that matches several rows above by accident.
			for name, present := range map[string]bool{
				"State": s.State.Present, "MiiStatus": s.MiiStatus.Present, "LinkFailureCount": s.LinkFailureCount.Present,
				"QueueID": s.QueueID.Present, "Prio": s.Prio.Present, "AdAggregatorID": s.AdAggregatorID.Present,
				"AdActorOperState": s.AdActorOperState.Present, "AdPartnerOperState": s.AdPartnerOperState.Present,
				"AdChurnActorState": s.AdChurnActorState.Present, "AdChurnPartnerState": s.AdChurnPartnerState.Present,
				"ActorPortPrio": s.ActorPortPrio.Present,
			} {
				if !present {
					t.Errorf("%s absent", name)
				}
			}
			if s.ActorPortPrio.Value != 255 || s.PrioInt32() != 0 || s.QueueID.Value != 0 {
				t.Errorf("actor_port_prio/prio/queue_id = %d/%d/%d, want 255/0/0", s.ActorPortPrio.Value, s.PrioInt32(), s.QueueID.Value)
			}
		})
	}
}

// TestDecodeBondChurnSettled pins the churn wait itself: every port of every
// captured state has both churn machines out of AD_CHURN_MONITOR, which is
// where a capture taken inside the 60 s detection window would leave them.
// A recapture that dropped lwtopo::wait_churn would pass
// TestDecodeBondSlaveRealFixture only by editing its expected values; this
// fails instead.
//
// go test ./pkg/xtcpnl/ -run TestDecodeBondChurnSettled
func TestDecodeBondChurnSettled(t *testing.T) {
	const adChurnMonitor = 0
	tests := []struct {
		description string
		getlink     string
		expected    int // ports with both machines settled
	}{
		{"corner: no partner, settled to a mix of NO_CHURN and CHURN", tdLacpNoPartnerGetLink_7_1_4, 2},
		{"positive: converged", tdLacpConvergedGetLink_7_1_4, 2},
		{"boundary: partner down, the dead port settled too", tdLacpPartnerDownGetLink_7_1_4, 2},
		{"positive: tier 2 converged", tdLacpPairConvergedGetLink_7_1_4, 2},
		{"boundary: tier 2 link pulled", tdLacpPairPulledGetLink_7_1_4, 2},
		{"positive: tier 2 link restored, after the restore re-armed the machine", tdLacpPairRestoredGetLink_7_1_4, 2},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			settled := 0
			for _, port := range []string{"la1", "la2"} {
				s := lacpLinksByName(t, tc.getlink)[port].BondSlave
				if s == nil {
					t.Fatalf("%s.BondSlave = nil", port)
				}
				if !s.AdChurnActorState.Present || !s.AdChurnPartnerState.Present {
					t.Errorf("%s churn state absent", port)
					continue
				}
				if s.AdChurnActorState.Value == adChurnMonitor || s.AdChurnPartnerState.Value == adChurnMonitor {
					t.Errorf("%s churn actor/partner = %d/%d: a machine is still in MONITOR, so the capture did not wait",
						port, s.AdChurnActorState.Value, s.AdChurnPartnerState.Value)
					continue
				}
				settled++
			}
			if settled != tc.expected {
				t.Errorf("%d ports settled, want %d", settled, tc.expected)
			}
		})
	}
}

// TestDecodeBondKindGating pins that the decode fires on the kind, not on the
// presence of a blob: lo and nlmon0 carry no bond data, bond0 is a master and
// not a port, and the ports' own kind is not bond. In Tier 2 the ports have
// no kind at all, so the slave decode must not depend on IFLA_INFO_KIND.
//
// go test ./pkg/xtcpnl/ -run TestDecodeBondKindGating
func TestDecodeBondKindGating(t *testing.T) {
	tests := []struct {
		description  string
		port         string
		expectBond   bool
		expectSlave  bool
		expectKind   string // portKind stands for the state's port kind
		expectSlvKnd string
	}{
		{"negative: lo has no IFLA_LINKINFO", "lo", false, false, "", ""},
		{"negative: nlmon0 has a kind but not bond", "nlmon0", false, false, "nlmon", ""},
		{"positive: bond0 is the master, so Bond and not BondSlave", "bond0", true, false, "bond", ""},
		{"positive: la1 is enslaved to a bond, so BondSlave and not Bond", "la1", false, true, "portKind", "bond"},
		{"positive: la2 likewise", "la2", false, true, "portKind", "bond"},
	}
	for _, st := range lacpStates {
		by := lacpLinksByName(t, st.getlink)
		for _, tc := range tests {
			t.Run(st.name+"/"+tc.description, func(t *testing.T) {
				li, ok := by[tc.port]
				if !ok {
					t.Fatalf("no link %q", tc.port)
				}
				kind := tc.expectKind
				if kind == "portKind" {
					kind = st.portKind
				}
				if li.Kind != kind || li.SlaveKind != tc.expectSlvKnd {
					t.Errorf("kind/slave kind = %q/%q, want %q/%q", li.Kind, li.SlaveKind, kind, tc.expectSlvKnd)
				}
				if (li.Bond != nil) != tc.expectBond {
					t.Errorf("Bond != nil is %v, want %v", li.Bond != nil, tc.expectBond)
				}
				if (li.BondSlave != nil) != tc.expectSlave {
					t.Errorf("BondSlave != nil is %v, want %v", li.BondSlave != nil, tc.expectSlave)
				}
			})
		}
	}
}

// readSysfsOracle parses one state's sysfs_bonding sidecar, written by
// `grep -s -H . …`, into "<dev>/<attr>" -> value.
func readSysfsOracle(t *testing.T, dir, state string) map[string]string {
	t.Helper()
	return readSysfsGrep(t, filepath.Join(dir, filepath.Base(state), "sysfs_bonding"))
}

// readSysfsGrep parses a `grep -s -H . /sys/class/net/<dev>/[<dir>/]<attr>`
// sidecar into "<dev>/<attr>" -> value. The device is the path element below
// /sys/class/net/, so a file directly under it (Tier 2's sysfs_ports speed,
// carrier) keys the same way as one under bonding/ or bonding_slave/.
func readSysfsGrep(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	out := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		file, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		parts := strings.Split(file, "/") // "", sys, class, net, <dev>, [<dir>,] <attr>
		if len(parts) < 6 {
			continue
		}
		out[parts[4]+"/"+parts[len(parts)-1]] = val
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	if len(out) == 0 {
		t.Fatalf("%s: empty oracle", path)
	}
	return out
}

// sysfsBondFileFor maps a sysfs bonding/ file to its BondOptions keyword
// where the two differ.
var sysfsBondFileFor = map[string]string{
	"peer_notif_delay": "peer_notify_delay",
}

// TestDecodeBondMatchesSysfsOracle checks the netlink decode against sysfs,
// which reads struct bonding and struct port with no netlink in between.
//
// That independence is the reason the sidecar was captured: a decoder test
// whose expected values came from `ip -d` inherits every bug `ip` has, and
// iproute2 reading the u16 partner state one byte wide is exactly such a bug.
// sysfs also prints each enum as "<name> <value>" from the kernel's own
// name table, so this pins BondOptions' Names against the kernel's.
//
// Every option file sysfs shows must have a table row and a decoded value;
// the files that are not options (slaves, queue_id, mii_status) and the ones
// that are not on the wire as options (num_unsol_na is num_grat_arp's alias)
// are listed in skip with the reason.
//
// go test ./pkg/xtcpnl/ -run TestDecodeBondMatchesSysfsOracle
func TestDecodeBondMatchesSysfsOracle(t *testing.T) {
	skip := map[string]string{
		"slaves":       "a list of names, carried by IFLA_MASTER on each port",
		"queue_id":     "per-port, carried by IFLA_BOND_SLAVE_QUEUE_ID",
		"mii_status":   "the bond's own carrier, not an option",
		"num_unsol_na": "alias of num_grat_arp, one attribute",
	}
	byKeyword := make(map[string]*BondOptionSpec, len(BondOptions))
	for i := range BondOptions {
		byKeyword[BondOptions[i].Keyword] = &BondOptions[i]
	}

	for _, st := range lacpStates {
		t.Run(st.name, func(t *testing.T) {
			oracle := readSysfsOracle(t, st.dir, st.name)
			by := lacpLinksByName(t, st.getlink)
			b := by["bond0"].Bond
			if b == nil || b.AdInfo == nil {
				t.Fatalf("bond0 Bond/AdInfo nil")
			}
			adInfo := map[string]string{
				"ad_aggregator":  strconv.Itoa(int(b.AdInfo.Aggregator.Value)),
				"ad_num_ports":   strconv.Itoa(int(b.AdInfo.NumPorts.Value)),
				"ad_actor_key":   strconv.Itoa(int(b.AdInfo.ActorKey.Value)),
				"ad_partner_key": strconv.Itoa(int(b.AdInfo.PartnerKey.Value)),
				"ad_partner_mac": net.HardwareAddr(b.AdInfo.PartnerMAC).String(),
			}
			checked := 0
			for key, want := range oracle {
				dev, attr, _ := strings.Cut(key, "/")
				if dev != "bond0" {
					continue
				}
				if _, ok := skip[attr]; ok {
					continue
				}
				if got, ok := adInfo[attr]; ok {
					if got != want {
						t.Errorf("AD_INFO %s = %s, sysfs %s", attr, got, want)
					}
					checked++
					continue
				}
				kw := attr
				if k, ok := sysfsBondFileFor[attr]; ok {
					kw = k
				}
				spec := byKeyword[kw]
				if spec == nil {
					t.Errorf("sysfs bonding/%s has no BondOptions row", attr)
					continue
				}
				v := b.Value(spec.ID)
				if !v.Present {
					t.Errorf("%s: sysfs %q, decode absent", kw, want)
					continue
				}
				checked++
				if spec.Wire == BondWireMAC {
					if got := net.HardwareAddr(v.Bytes).String(); got != want {
						t.Errorf("%s = %s, sysfs %s", kw, got, want)
					}
					continue
				}
				// "<name> <value>" for an enum, "<value>" otherwise.
				name, num, isEnum := strings.Cut(want, " ")
				if !isEnum {
					num = name
				}
				if got := strconv.FormatUint(uint64(v.U), 10); got != num {
					t.Errorf("%s = %s, sysfs %s", kw, got, want)
				}
				if isEnum && spec.ValueName(v.U) != name {
					t.Errorf("%s name = %q, kernel's is %q", kw, spec.ValueName(v.U), name)
				}
			}
			if checked < 25 {
				t.Errorf("checked %d bond attributes against sysfs, want at least 25", checked)
			}

			for _, port := range []string{"la1", "la2"} {
				s := by[port].BondSlave
				if s == nil {
					t.Fatalf("%s.BondSlave nil", port)
				}
				states := []string{"active", "backup"}
				miis := []string{"up", "going down", "down", "going back"}
				for attr, got := range map[string]string{
					"state":                      states[s.State.Value],
					"mii_status":                 miis[s.MiiStatus.Value],
					"link_failure_count":         strconv.Itoa(int(s.LinkFailureCount.Value)),
					"perm_hwaddr":                net.HardwareAddr(s.PermHwaddr).String(),
					"queue_id":                   strconv.Itoa(int(s.QueueID.Value)),
					"ad_aggregator_id":           strconv.Itoa(int(s.AdAggregatorID.Value)),
					"ad_actor_oper_port_state":   strconv.Itoa(int(s.AdActorOperState.Value)),
					"ad_partner_oper_port_state": strconv.Itoa(int(s.AdPartnerOperState.Value)),
				} {
					want, ok := oracle[port+"/"+attr]
					if !ok {
						t.Errorf("sysfs has no %s/%s", port, attr)
						continue
					}
					if got != want {
						t.Errorf("%s %s = %s, sysfs %s", port, attr, got, want)
					}
				}
			}
		})
	}
}

// TestDecodeLacpPairPortsMatchSysfs checks what a Tier 2 port carries that a
// veth does not, against the two sidecars capture-lacp-pair.exp ports_set
// writes: the parent device (IFLA_PARENT_DEV_BUS_NAME / _NAME, from
// sysfs_ports_parent's readlink of device), and carrier, operstate and the
// bond's link failure count (sysfs_ports). The carrier rows are the point of
// lacp_link_pulled: la2's loss arrives as IFLA_CARRIER 0 and IF_OPER_DOWN
// while the port stays administratively up.
//
// go test ./pkg/xtcpnl/ -run TestDecodeLacpPairPortsMatchSysfs
func TestDecodeLacpPairPortsMatchSysfs(t *testing.T) {
	type portWant struct {
		carrier   uint8
		operstate uint8
		parentBus string
		parentDev string
	}
	tests := []struct {
		description string
		state       string
		getlink     string
		port        string
		expected    portWant
	}{
		{"positive: converged la1 - carrier up, a virtio device below the PCI function",
			"lacp_converged", tdLacpPairConvergedGetLink_7_1_4, "la1", portWant{1, IfOperUp, "virtio", "virtio4"}},
		{"positive: converged la2 - the next virtio device",
			"lacp_converged", tdLacpPairConvergedGetLink_7_1_4, "la2", portWant{1, IfOperUp, "virtio", "virtio5"}},
		{"positive: link pulled la1 untouched",
			"lacp_link_pulled", tdLacpPairPulledGetLink_7_1_4, "la1", portWant{1, IfOperUp, "virtio", "virtio4"}},
		{"corner: link pulled la2 - no carrier, IF_OPER_DOWN, the parent unchanged",
			"lacp_link_pulled", tdLacpPairPulledGetLink_7_1_4, "la2", portWant{0, IfOperDown, "virtio", "virtio5"}},
		{"boundary: link restored la2 - carrier back",
			"lacp_link_restored", tdLacpPairRestoredGetLink_7_1_4, "la2", portWant{1, IfOperUp, "virtio", "virtio5"}},
	}
	operName := map[uint8]string{IfOperUp: "up", IfOperDown: "down"}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li, ok := lacpLinksByName(t, tc.getlink)[tc.port]
			if !ok {
				t.Fatalf("no link %q", tc.port)
			}
			if !li.HasCarrier || !li.HasOperState {
				t.Fatalf("carrier/operstate present = %v/%v, want both", li.HasCarrier, li.HasOperState)
			}
			got := portWant{li.Carrier, li.OperState, li.Detail.ParentDevBusName, li.Detail.ParentDevName}
			if got != tc.expected {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected)
			}
			if li.Flags&unix.IFF_UP == 0 {
				t.Errorf("flags %#x lack IFF_UP: a pulled link is still administratively up", li.Flags)
			}

			dir := filepath.Join(tdLacpPair_7_1_4, tc.state)
			sysfs := readSysfsGrep(t, filepath.Join(dir, "sysfs_ports"))
			for attr, want := range map[string]string{
				"carrier":            strconv.Itoa(int(li.Carrier)),
				"operstate":          operName[li.OperState],
				"speed":              "10000",
				"duplex":             "full",
				"link_failure_count": strconv.Itoa(int(li.BondSlave.LinkFailureCount.Value)),
			} {
				if v := sysfs[tc.port+"/"+attr]; v != want {
					t.Errorf("sysfs %s/%s = %q, decode says %q", tc.port, attr, v, want)
				}
			}

			raw, err := os.ReadFile(filepath.Join(dir, "sysfs_ports_parent"))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for line := range strings.Lines(string(raw)) {
				f := strings.Fields(line) // <port> <pci function> <virtio device>
				if len(f) == 3 && f[0] == tc.port {
					found = true
					if f[2] != li.Detail.ParentDevName {
						t.Errorf("sysfs parent of %s is %s, IFLA_PARENT_DEV_NAME %s", tc.port, f[2], li.Detail.ParentDevName)
					}
				}
			}
			if !found {
				t.Errorf("sysfs_ports_parent has no %s line", tc.port)
			}
		})
	}
}

// bondNest builds an IFLA_INFO_DATA-style nest from attributes.
func bondNest(attrs ...[]byte) []byte {
	return bytes.Join(attrs, nil)
}

// TestParseBondInfoData drives the master decoder on hand-built nests, for
// the shapes the capture does not contain: wide values, short attributes,
// target lists, an unknown id, a truncated nest.
//
// go test ./pkg/xtcpnl/ -run TestParseBondInfoData
func TestParseBondInfoData(t *testing.T) {
	v4 := netip.MustParseAddr
	tests := []struct {
		description string
		nest        []byte
		check       func(t *testing.T, b *BondInfo)
	}{
		{
			description: "boundary: a nil nest is no BondInfo",
			nest:        nil,
			check: func(t *testing.T, b *BondInfo) {
				if b != nil {
					t.Errorf("got %+v, want nil", b)
				}
			},
		},
		{
			description: "boundary: an empty nest is a BondInfo with nothing present",
			nest:        []byte{},
			check: func(t *testing.T, b *BondInfo) {
				if b == nil || b.Value(unix.IFLA_BOND_MODE).Present || b.AdInfo != nil {
					t.Errorf("got %+v", b)
				}
			},
		},
		{
			description: "positive: arp_validate is u32 on the wire - 256 survives, which a u8 read would turn into 0",
			nest:        rtattr(unix.IFLA_BOND_ARP_VALIDATE, u32le(256)),
			check: func(t *testing.T, b *BondInfo) {
				if v := b.Value(unix.IFLA_BOND_ARP_VALIDATE); v.U != 256 || !v.Present {
					t.Errorf("arp_validate = %+v", v)
				}
				if n := BondOptionSpecByID(unix.IFLA_BOND_ARP_VALIDATE).ValueName(256); n != "" {
					t.Errorf("name past the table = %q, want \"\" (printed as a number)", n)
				}
			},
		},
		{
			description: "positive: ad_actor_sys_prio is u16 - 0x1234 reads whole",
			nest:        rtattr(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, le16(0x1234)),
			check: func(t *testing.T, b *BondInfo) {
				if v := b.Value(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO); v.U != 0x1234 || !v.Present {
					t.Errorf("ad_actor_sys_prio = %+v", v)
				}
			},
		},
		{
			description: "negative: a u32 option sent two bytes long is absent, not a truncated value",
			nest:        rtattr(unix.IFLA_BOND_MIIMON, []byte{100, 0}),
			check: func(t *testing.T, b *BondInfo) {
				if b.Value(unix.IFLA_BOND_MIIMON).Present {
					t.Errorf("miimon present from a short attribute")
				}
			},
		},
		{
			description: "negative: ad_actor_system shorter than ETH_ALEN is absent",
			nest:        rtattr(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0, 0}),
			check: func(t *testing.T, b *BondInfo) {
				if b.Value(unix.IFLA_BOND_AD_ACTOR_SYSTEM).Present {
					t.Errorf("ad_actor_system present from 3 bytes")
				}
			},
		},
		{
			description: "corner: an id the table does not know is recorded in Unknown, and its neighbors still decode",
			nest:        bondNest(rtattr(unix.IFLA_BOND_MODE, []byte{4}), rtattr(99, []byte{1}), rtattr(unix.IFLA_BOND_MIIMON, u32le(100))),
			check: func(t *testing.T, b *BondInfo) {
				if len(b.Unknown) != 1 || b.Unknown[0] != 99 {
					t.Errorf("Unknown = %v, want [99]", b.Unknown)
				}
				if b.Value(unix.IFLA_BOND_MODE).U != 4 || b.Value(unix.IFLA_BOND_MIIMON).U != 100 {
					t.Errorf("neighbors lost: %+v", b)
				}
			},
		},
		{
			description: "positive: arp_ip_target entries are rebuilt by position (the sub-attribute type), not wire order",
			nest: rtattr(unix.IFLA_BOND_ARP_IP_TARGET, bondNest(
				rtattr(1, []byte{192, 0, 2, 2}), rtattr(0, []byte{192, 0, 2, 1}))),
			check: func(t *testing.T, b *BondInfo) {
				want := []netip.Addr{v4("192.0.2.1"), v4("192.0.2.2")}
				if len(b.ArpIPTargets) != 2 || b.ArpIPTargets[0] != want[0] || b.ArpIPTargets[1] != want[1] {
					t.Errorf("ArpIPTargets = %v, want %v", b.ArpIPTargets, want)
				}
			},
		},
		{
			description: "corner: a target of the wrong size and a position past 16 are skipped",
			nest: rtattr(unix.IFLA_BOND_ARP_IP_TARGET, bondNest(
				rtattr(0, []byte{192, 0, 2, 1}), rtattr(1, []byte{1, 2}), rtattr(16, []byte{192, 0, 2, 9}))),
			check: func(t *testing.T, b *BondInfo) {
				if len(b.ArpIPTargets) != 1 || b.ArpIPTargets[0] != v4("192.0.2.1") {
					t.Errorf("ArpIPTargets = %v", b.ArpIPTargets)
				}
			},
		},
		{
			description: "positive: ns_ip6_target holds 16-byte entries",
			nest: rtattr(unix.IFLA_BOND_NS_IP6_TARGET,
				rtattr(0, netip.MustParseAddr("2001:db8::1").AsSlice())),
			check: func(t *testing.T, b *BondInfo) {
				if len(b.NsIP6Targets) != 1 || b.NsIP6Targets[0] != netip.MustParseAddr("2001:db8::1") {
					t.Errorf("NsIP6Targets = %v", b.NsIP6Targets)
				}
			},
		},
		{
			description: "boundary: an empty AD_INFO nest is present with every field absent",
			nest:        rtattr(unix.IFLA_BOND_AD_INFO, nil),
			check: func(t *testing.T, b *BondInfo) {
				if b.AdInfo == nil || b.AdInfo.Aggregator.Present || b.AdInfo.PartnerMAC != nil {
					t.Errorf("AdInfo = %+v", b.AdInfo)
				}
			},
		},
		{
			description: "negative: AD_INFO truncated mid-nest keeps what came before and does not panic",
			nest: func() []byte {
				inner := bondNest(rtattr(unix.IFLA_BOND_AD_INFO_AGGREGATOR, le16(7)), rtattr(unix.IFLA_BOND_AD_INFO_PARTNER_MAC, mustMACNoT("02:00:00:00:00:0b")))
				return rtattr(unix.IFLA_BOND_AD_INFO, inner[:len(inner)-4])
			}(),
			check: func(t *testing.T, b *BondInfo) {
				if b.AdInfo == nil || b.AdInfo.Aggregator != (U16Attr{7, true}) || b.AdInfo.PartnerMAC != nil {
					t.Errorf("AdInfo = %+v", b.AdInfo)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tc.check(t, ParseBondInfoData(tc.nest))
		})
	}
}

func mustMACNoT(s string) []byte {
	hw, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return hw
}

// TestParseBondSlaveData drives the port decoder on hand-built nests. The
// partner-state rows are the width hazard: the kernel sends a u16, iproute2
// reads a u8, and the decode is at the kernel's width.
//
// go test ./pkg/xtcpnl/ -run TestParseBondSlaveData
func TestParseBondSlaveData(t *testing.T) {
	tests := []struct {
		description string
		nest        []byte
		expected    BondSlaveInfo
	}{
		{
			description: "positive: partner state at the kernel's u16 width - a set high byte is kept, which a u8 read would drop",
			nest:        rtattr(unix.IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE, le16(0x013f)),
			expected:    BondSlaveInfo{AdPartnerOperState: U16Attr{0x013f, true}},
		},
		{
			description: "corner: a one-byte partner state is accepted as the octet",
			nest:        rtattr(unix.IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE, []byte{0x3f}),
			expected:    BondSlaveInfo{AdPartnerOperState: U16Attr{0x3f, true}},
		},
		{
			description: "negative: an empty partner state attribute is absent",
			nest:        rtattr(unix.IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE, nil),
			expected:    BondSlaveInfo{},
		},
		{
			description: "boundary: state 0 (ACTIVE) and mii_status 0 (UP) are present-and-zero, not absent",
			nest:        bondNest(rtattr(unix.IFLA_BOND_SLAVE_STATE, []byte{0}), rtattr(unix.IFLA_BOND_SLAVE_MII_STATUS, []byte{0})),
			expected:    BondSlaveInfo{State: U8Attr{0, true}, MiiStatus: U8Attr{0, true}},
		},
		{
			description: "positive: churn states and actor_port_prio, the ids x/sys/unix does not name",
			nest: bondNest(rtattr(IflaBondSlaveAdChurnActorState, []byte{2}), rtattr(IflaBondSlaveAdChurnPartnerState, []byte{1}),
				rtattr(IflaBondSlaveActorPortPrio, le16(255))),
			expected: BondSlaveInfo{AdChurnActorState: U8Attr{2, true}, AdChurnPartnerState: U8Attr{1, true}, ActorPortPrio: U16Attr{255, true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := ParseBondSlaveData(tc.nest)
			if got == nil {
				t.Fatalf("nil")
			}
			if got.State != tc.expected.State || got.MiiStatus != tc.expected.MiiStatus ||
				got.AdPartnerOperState != tc.expected.AdPartnerOperState ||
				got.AdChurnActorState != tc.expected.AdChurnActorState || got.AdChurnPartnerState != tc.expected.AdChurnPartnerState ||
				got.ActorPortPrio != tc.expected.ActorPortPrio {
				t.Errorf("got  %+v\nwant %+v", *got, tc.expected)
			}
		})
	}

	t.Run("corner: prio is s32 on the wire, so 0xffffffff is -1", func(t *testing.T) {
		s := ParseBondSlaveData(rtattr(unix.IFLA_BOND_SLAVE_PRIO, u32le(0xffffffff)))
		if s.PrioInt32() != -1 {
			t.Errorf("PrioInt32 = %d, want -1", s.PrioInt32())
		}
	})
	t.Run("boundary: a nil nest is no BondSlaveInfo", func(t *testing.T) {
		if s := ParseBondSlaveData(nil); s != nil {
			t.Errorf("got %+v", s)
		}
	})
}

// TestBondOptionsTable pins the table's own invariants: the properties every
// later phase relies on and nothing else checks.
//
// go test ./pkg/xtcpnl/ -run TestBondOptionsTable
func TestBondOptionsTable(t *testing.T) {
	ids := map[uint16]string{}
	words := map[string]bool{}
	for _, s := range BondOptions {
		if prev, dup := ids[s.ID]; dup {
			t.Errorf("id %d is both %q and %q", s.ID, prev, s.Keyword)
		}
		ids[s.ID] = s.Keyword
		for _, w := range append([]string{s.Keyword}, s.Aliases...) {
			if words[w] {
				t.Errorf("keyword %q appears twice", w)
			}
			words[w] = true
		}
		if s.Names != nil && s.Wire != BondWireU8 && s.Wire != BondWireU32 {
			t.Errorf("%s has names but wire %d", s.Keyword, s.Wire)
		}
		if !s.ReadOnly && s.Match == 0 {
			t.Errorf("%s has no match rule", s.Keyword)
		}
	}
	// Every IFLA_BOND_* id from MODE to LACP_STRICT has a row: 1..34.
	for id := uint16(1); id <= IflaBondLacpStrict; id++ {
		if _, ok := ids[id]; !ok {
			t.Errorf("IFLA_BOND id %d has no row", id)
		}
	}

	tests := []struct {
		description string
		keyword     string
		expected    BondMatch
	}{
		{"positive: ns_ip6_target is strcmp (iplink_bond.c:260)", "ns_ip6_target", BondMatchExact},
		{"positive: arp_missed_max is strcmp (:291)", "arp_missed_max", BondMatchExact},
		{"positive: lacp_active is strcmp (:373)", "lacp_active", BondMatchExact},
		{"positive: coupled_control is strcmp (:380)", "coupled_control", BondMatchExact},
		{"positive: broadcast_neighbor is strcmp (:386)", "broadcast_neighbor", BondMatchExact},
		{"negative: mode is matches(), a prefix", "mode", BondMatchPrefix},
		{"negative: lacp_rate is matches(), a prefix", "lacp_rate", BondMatchPrefix},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			for _, s := range BondOptions {
				if s.Keyword == tc.keyword {
					if s.Match != tc.expected {
						t.Errorf("%s match = %d, want %d", tc.keyword, s.Match, tc.expected)
					}
					return
				}
			}
			t.Errorf("no row %q", tc.keyword)
		})
	}

	names := []struct {
		description string
		id          uint16
		v           uint32
		expected    string
	}{
		{"positive: mode 4 is 802.3ad", unix.IFLA_BOND_MODE, 4, "802.3ad"},
		{"boundary: mode 6 is the last name", unix.IFLA_BOND_MODE, 6, "balance-alb"},
		{"boundary: mode 7 is past the table, printed as a number", unix.IFLA_BOND_MODE, 7, ""},
		{"positive: ad_select 3 is actor_port_prio, which the usage text omits", unix.IFLA_BOND_AD_SELECT, 3, "actor_port_prio"},
		{"negative: miimon is numeric, never named", unix.IFLA_BOND_MIIMON, 0, ""},
	}
	for _, tc := range names {
		t.Run(tc.description, func(t *testing.T) {
			if got := BondOptionSpecByID(tc.id).ValueName(tc.v); got != tc.expected {
				t.Errorf("ValueName(%d) = %q, want %q", tc.v, got, tc.expected)
			}
		})
	}
	t.Run("corner: a nil spec names nothing", func(t *testing.T) {
		if got := BondOptionSpecByID(250).ValueName(0); got != "" {
			t.Errorf("got %q", got)
		}
	})
}
