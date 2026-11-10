package main

// The stats family: `ip stats show` and its kin (ip/ipstats.c), every row
// a read of RTM_GETSTATS (goip-parity-next.md, NC4-T01). The rows are
// named: a show in four forms (text, -j, -s, -j -s) on each of four
// topologies, every other show on the topology that holds all of them,
// `ip link afstats` and `ip link xstats`, `ip stats set`, and the errors.
// The batch loads mpls_router before any row runs, so every device has
// AF_MPLS stats (afstats's mpls subgroup). Nothing in a topology sends
// traffic, so the counters stay still: no device has a link-local
// address, and the bond stays down.

// statsModules: bonding for the bond topology, mpls_router for afstats.
var statsModules = []string{"bonding", "mpls_router"}

const statsDummy = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up"

// statsTopologies are the setups, by name, in the order the rows use them.
var statsTopologies = []struct{ name, setup string }{
	{"clean", statsDummy},
	{"bridge", statsDummy + " && ip link add br0 type bridge && ip link set br0 addrgenmode none" +
		" && ip link set d0 master br0 && ip link set br0 up"},
	{"bond", statsDummy + " && ip link add bond0 type bond mode 802.3ad" +
		" && ip link add d2 type dummy && ip link add d3 type dummy && ip link set d2 master bond0 && ip link set d3 master bond0"},
	{"l3", statsDummy + " && ip stats set dev d0 l3_stats on"},
}

// statsFull holds every topology's devices at once, for the shows that
// select a group.
const statsFull = statsDummy + " && ip stats set dev d0 l3_stats on" +
	" && ip link add br0 type bridge && ip link set br0 addrgenmode none" +
	" && ip link add d1 type dummy && ip link set d1 addrgenmode none && ip link set d1 master br0 && ip link set br0 up && ip link set d1 up" +
	" && ip link add bond0 type bond mode 802.3ad" +
	" && ip link add d2 type dummy && ip link add d3 type dummy && ip link set d2 master bond0 && ip link set d3 master bond0"

// statsForms are a show's four forms: the options before `stats` and the
// id suffix.
var statsForms = []struct{ opts, suffix string }{
	{"", "text"}, {"-j ", "json"}, {"-s ", "stats"}, {"-j -s ", "json-stats"},
}

// statsShows are the shows on statsFull, by id, with the filter they
// ask for: the group mask, and the offload sub-mask when the show narrows
// it (IFLA_STATS_GET_FILTERS; ip sends none for a full sub-mask, so
// cpu_hit with l3_stats, 0x7, sends none). byDev is a get of d0 alone
// rather than a dump: ipstats_show_one; `link afstats dev` dumps and
// filters itself.
var statsShows = []struct {
	id, args, mask, offload string
	byDev                   bool
}{
	{"dev", "stats show dev d0", "31", "", true},
	{"link", "stats show group link", "1", "", false},
	{"offload", "stats show group offload", "8", "", false},
	{"xstats", "stats show group xstats", "2", "", false},
	{"xstats-slave", "stats show group xstats_slave", "4", "", false},
	{"afstats", "stats show group afstats", "16", "", false},
	{"cpu-hit", "stats show group offload subgroup cpu_hit", "8", "1", false},
	{"hw-stats-info", "stats show group offload subgroup hw_stats_info", "8", "2", false},
	{"l3-stats", "stats show group offload subgroup l3_stats", "8", "6", false},
	{"l3-stats-dev", "stats show dev d0 group offload subgroup l3_stats", "8", "6", true},
	{"two-groups", "stats show group link group afstats", "17", "", false},
	{"two-subgroups", "stats show group offload subgroup cpu_hit subgroup l3_stats", "8", "", false},
	{"link-afstats", "link afstats", "16", "", false},
	{"link-afstats-dev", "link afstats dev d0", "16", "", false},
	{"xstats-bond", "link xstats type bond", "2", "", false},
	{"xstats-bond-slave", "link xstats type bond_slave", "4", "", false},
	{"xstats-bridge", "link xstats type bridge", "2", "", false},
	{"xstats-bridge-slave", "link xstats type bridge_slave", "4", "", false},
}

func statsRow(id, desc, cmd, setup string, exp expect, axes map[string]string, rb ...readback) row {
	return row{
		ID: "stats/" + id, Desc: desc, Cmd: cmd, Pre: []preStep{{Cmd: setup}},
		Readback: rb, Modules: statsModules, Expect: exp, Axes: axes,
	}
}

func statsFamily() []row {
	ok := expect{RC: intp(0), RefusedBy: "none"}
	errs := []struct {
		id, args string
		exp      expect
		why      string
	}{
		{"help", "stats help", ok, "the usage, rc 0"},
		{"show-help", "stats show help", ok, "the usage, rc 0"},
		{"group-bare", "stats show group", expect{RC: intp(255), RefusedBy: "iproute2"}, "group with no name (Command line is not complete)"},
		{"orphan-subgroup", "stats show subgroup l3_stats", expect{RC: intp(22), RefusedBy: "iproute2"},
			"a subgroup with no group (requested without selecting a group first)"},
		{"leaf-subgroup", "stats show group link subgroup x", expect{RC: intp(22), RefusedBy: "iproute2"},
			"a subgroup inside a leaf group (requested inside leaf group link)"},
		{"suite-no-subgroup", "stats show group xstats suite bond", expect{RC: intp(22), RefusedBy: "iproute2"},
			"a suite with no subgroup (requested without selecting a subgroup first)"},
		{"nosuch-group", "stats show group nosuch", expect{RC: intp(22), RefusedBy: "iproute2"}, "an unknown group (no group named nosuch)"},
		{"dev-twice", "stats show dev d0 dev d0", expect{RC: intp(255), RefusedBy: "ack-then-fail"}, "dev twice, after the first's lookup"},
		{"nosuch-word", "stats show nosuch", expect{RC: intp(22), RefusedBy: "iproute2"}, `an unknown word (What is "nosuch"?)`},
		{"nosuch-dev", "stats show dev nosuch", expect{RC: intp(1), Errno: "ENODEV", RefusedBy: "kernel"}, "a device that isn't there"},
	}
	const sets = 3
	rows := make([]row, 0, len(statsForms)*(len(statsTopologies)+len(statsShows))+sets+len(errs))
	for _, topo := range statsTopologies {
		for _, form := range statsForms {
			rows = append(rows, statsRow("show-"+topo.name+"-"+form.suffix,
				"positive: the bare show on the "+topo.name+" topology, "+form.suffix,
				"ip "+form.opts+"stats show", topo.setup, ok, f("mask", "31")))
		}
	}
	for _, s := range statsShows {
		for _, form := range statsForms {
			axes := f("mask", s.mask)
			if s.offload != "" {
				axes["offload"] = s.offload
			}
			if s.byDev {
				axes["dev"] = "d0"
			}
			rows = append(rows, statsRow(s.id+"-"+form.suffix, "positive: ip "+s.args+", "+form.suffix,
				"ip "+form.opts+s.args, statsFull, ok, axes))
		}
	}
	setRB := readback{Name: "l3", Cmd: "ip stats show dev d0 group offload subgroup l3_stats"}
	rows = append(rows,
		statsRow("set-on", "positive: stats set l3_stats on", "ip stats set dev d0 l3_stats on", statsDummy, ok, f("l3_stats", "1"), setRB),
		statsRow("set-off", "positive: stats set l3_stats off after on", "ip stats set dev d0 l3_stats off",
			statsDummy+" && ip stats set dev d0 l3_stats on", ok, f("l3_stats", "0"), setRB),
		statsRow("set-x", "negative: stats set l3_stats neither on nor off", "ip stats set dev d0 l3_stats x", statsDummy,
			expect{RC: intp(22), RefusedBy: "iproute2"}, nil, setRB),
	)
	for _, e := range errs {
		kind := "negative: "
		if *e.exp.RC == 0 {
			kind = "corner: "
		}
		rows = append(rows, statsRow("err-"+e.id, kind+e.why, "ip "+e.args, statsFull, e.exp, nil))
	}
	return rows
}
