package main

import (
	"slices"
	"strings"
)

// The ethtool spaces (W5d): ethtool's set and show commands over generic
// netlink's "ethtool" family. ethtool's side is netlink/*.c in ethtool
// 7.1: each set command (nl_sring, nl_schannels, nl_spause, nl_sfec,
// nl_scoalesce) sends one ETHTOOL_MSG_*_SET with the device's name in the
// header and an attribute per argument given, then waits for the ack; a
// show command sends the matching *_GET. Before either, netlink_init
// resolves the family with CTRL_CMD_GETFAMILY, which the kernel acks, so
// an argument ethtool refuses afterwards is ack-then-fail.
//
// The kernel's side is net/ethtool/: the *_set_validate hooks refuse what
// the driver cannot do (EOPNOTSUPP), and the set handlers refuse values
// outside the driver's limits (EINVAL), each with an extack message.
//
// Every row's namespace holds one netdevsim port, eth0 (ethSetup), whose
// ethtool_ops (drivers/net/netdevsim/ethtool.c) implement rings, channels,
// pause, FEC and coalescing. A value's facts are the attributes the
// request must carry, by ethtool's argument name, which the corpus test
// (pkg/xtcpnl/xtcpnl_ws_ethtool_test.go) maps to attribute numbers; get is
// the *_GET the readback (or the show command) sends; rt.<name> is a value
// the device reads back other than as set, with the reason at the value.

// ethSetup creates the row's netdevsim device: one port with four queues,
// which new_device creates in the writer's namespace (bus.c
// new_device_store records current->nsproxy->net_ns) and names eth0 there.
// The bus ID is 900 plus the slot's number (the namespace is ws<slot>), so
// rows running at once do not collide; the row before on the same slot
// left its device in init_net when its namespace went (default_device_exit
// moves it), so the same ID is deleted first.
const ethSetup = `i=$((900+$(ip netns identify | tr -dc 0-9))); echo $i > /sys/bus/netdevsim/del_device 2>/dev/null;` +
	` echo "$i 1 4" > /sys/bus/netdevsim/new_device && ip link show eth0 >/dev/null`

var ethModules = []string{"netdevsim"}

// ethENOTSUP is EOPNOTSUPP, which unix.ErrnoName names ENOTSUP.
const ethENOTSUP = "ENOTSUP"

// ethKernel is a set the kernel refuses with errno and its extack message;
// rc is the exit status the command gives a refusal (nl_sring 81, nl_spause
// 76, nl_sfec 83, nl_schannels and nl_scoalesce 1).
func ethKernel(rc int, errno, extack string) expect {
	return expect{RC: intp(rc), Errno: errno, Extack: extack, RefusedBy: refKernel}
}

// ethParse is an argument ethtool's nl_parser refuses (exit 1), after the
// family lookup was acked.
var ethParse = expect{RC: intp(1), RefusedBy: refAckThenFail}

// ethNum is an axis of a numeric argument: absent, then each value, the
// fact named for the argument. Values in bound are boundaries.
func ethNum(axisName, argName string, vals []string, bound ...string) axis {
	ax := axis{Name: axisName, Values: []value{v("absent", "", nil)}}
	for _, n := range vals {
		val := v(n, argName+" "+n, f(argName, n))
		val.Boundary = slices.Contains(bound, n)
		ax.Values = append(ax.Values, val)
	}
	return ax
}

// ethOnOff is an axis of an on/off argument, sent as a u8 1 or 0.
func ethOnOff(axisName, argName string) axis {
	return axis{Name: axisName, Values: []value{
		v("absent", "", nil),
		v("on", argName+" on", f(argName, "1")),
		v("off", argName+" off", f(argName, "0")),
	}}
}

// ethDev is the device axis: eth0, with lo and a missing name as bad
// values. lo has no ethtool_ops of these kinds, so ethnl's validate hook
// refuses it; a missing name fails the header's lookup
// (ethnl_parse_header_dev_get).
func ethDev(rc int) (axis, []badValue) {
	return axis{Name: "dev", Values: []value{v("eth0", "eth0", nil)}}, []badValue{
		{Axis: "dev", Value: v("nosuch", "nosuch0", nil), Expect: ethKernel(rc, "ENODEV", "no device matches name"),
			Why: "a device that does not exist"},
		{Axis: "dev", Value: v("lo", "lo", nil), Expect: ethKernel(rc, ethENOTSUP, ""),
			Why: "lo, whose ethtool_ops lack the operation"},
	}
}

// ethBuild makes a space's Build: ethtool, the command word, then the
// values' arguments in axis order; the readback is the matching show, of
// eth0 whatever device the command named.
func ethBuild(word, show, set, get string) func(vals []value) row {
	return func(vals []value) row {
		args := make([]string, 0, len(vals))
		for _, val := range vals {
			args = append(args, val.Arg)
		}
		axes := mergeFacts(vals)
		axes["set"], axes["get"] = set, get
		return row{
			Cmd:      "ethtool " + word + " " + joinArgs(args...),
			Proto:    "generic",
			Pre:      []preStep{{Cmd: ethSetup}},
			Modules:  ethModules,
			Readback: []readback{{Name: "show", Cmd: "ethtool " + show + " eth0", Proto: "generic"}},
			Expect:   expect{RC: intp(0), RefusedBy: refNone},
			Axes:     axes,
		}
	}
}

// ethNegBase picks a base row for the bad values: one where the named axes
// are absent, so a bad value on the last axis ends the command line.
func ethNegBase(sp *space, absent ...string) func(vals []value) bool {
	return func(vals []value) bool {
		p := pickOf(sp.Axes, vals)
		for _, a := range absent {
			if p[a] != "absent" {
				return false
			}
		}
		return true
	}
}

const ethRingMax = "requested ring size exceeds maximum"

// ethRingSpace: ethtool -G, ETHTOOL_MSG_RINGS_SET (16). netdevsim's limits
// are 4096 for each ring (nsim_ethtool_ring_init) and 1024 for the header
// data split threshold (NSIM_HDS_THRESHOLD_MAX); its supported_ring_params
// are TCP_DATA_SPLIT and HDS_THRS only.
func ethRingSpace() *space {
	dev, devBad := ethDev(81)
	unsupported := func(arg, val, msg string) badValue {
		name := strings.Fields(arg)[0]
		return badValue{Axis: "other", Value: v(name, arg, f(name, val)),
			Expect: ethKernel(81, ethENOTSUP, msg), Why: "a parameter netdevsim's supported_ring_params lacks"}
	}
	sp := &space{
		Name: "ethring",
		Axes: []axis{
			dev,
			ethNum("rx", "rx", []string{"1", "1024", "4096"}, "4096"),
			ethNum("tx", "tx", []string{"1", "256", "4096"}, "4096"),
			ethNum("rxmini", "rx-mini", []string{"0", "4096"}, "4096"),
			ethNum("rxjumbo", "rx-jumbo", []string{"100"}),
			// tcp-data-split: ETHTOOL_TCP_DATA_SPLIT_UNKNOWN 0, DISABLED
			// 1, ENABLED 2. auto stores UNKNOWN in dev->cfg, which
			// nsim_get_ringparam reports as ENABLED (ethtool.c:77).
			{Name: "tds", Values: []value{
				v("absent", "", nil),
				v("on", "tcp-data-split on", f("tcp-data-split", "2")),
				v("off", "tcp-data-split off", f("tcp-data-split", "1")),
				cv("auto", "tcp-data-split auto", f("tcp-data-split", "0", "rt.tcp-data-split", "2")),
			}},
			ethNum("hds", "hds-thresh", []string{"0", "1024"}, "1024"),
			{Name: "other", Values: []value{v("absent", "", nil)}},
		},
		Bad: append(devBad,
			badValue{Axis: "rx", Value: bv("4097", "rx 4097", f("rx", "4097")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "one more RX descriptor than rx_max_pending"},
			badValue{Axis: "rx", Value: bv("u32max", "rx 4294967295", f("rx", "4294967295")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "the largest u32, over the maximum"},
			badValue{Axis: "rx", Value: v("u32wrap", "rx 4294967296", nil), Expect: ethParse,
				Why: "a count over the u32 range (invalid value)"},
			badValue{Axis: "rx", Value: v("neg", "rx -1", nil), Expect: ethParse, Why: "a negative count"},
			badValue{Axis: "tx", Value: bv("4097", "tx 4097", f("tx", "4097")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "one more TX descriptor than tx_max_pending"},
			badValue{Axis: "rxmini", Value: bv("4097", "rx-mini 4097", f("rx-mini", "4097")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "one more mini descriptor than rx_mini_max_pending"},
			badValue{Axis: "rxjumbo", Value: bv("4097", "rx-jumbo 4097", f("rx-jumbo", "4097")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "one more jumbo descriptor than rx_jumbo_max_pending"},
			badValue{Axis: "tds", Value: v("bogus", "tcp-data-split bogus", nil), Expect: ethParse,
				Why: "a split mode tcp_data_split_values lacks"},
			badValue{Axis: "hds", Value: bv("1025", "hds-thresh 1025", f("hds-thresh", "1025")), Expect: ethKernel(81, "EINVAL", ethRingMax),
				Why: "a threshold over hds_thresh_max"},
			unsupported("rx-buf-len 2048", "2048", "setting rx buf len not supported"),
			unsupported("cqe-size 64", "64", "setting cqe size not supported"),
			unsupported("tx-push on", "1", "setting tx push not supported"),
			unsupported("rx-push on", "1", "setting rx push not supported"),
			unsupported("tx-push-buf-len 64", "64", "setting tx push buf len is not supported"),
			// hds-thresh is the one sring_params entry with min_argc 0
			// (netlink/rings.c), so as the last word it reaches
			// nl_parse_direct_u32 with no value: invalid value '(null)'.
			badValue{Axis: "other", Value: v("bare", "hds-thresh", nil), Expect: ethParse,
				Why: "hds-thresh with no value, which min_argc 0 lets through to the u32 parser"},
			badValue{Axis: "other", Value: v("dup", "rx 1 rx 2", nil), Expect: ethParse, Why: "rx given twice (duplicate parameter)"},
		),
	}
	sp.Build = ethBuild("-G", "-g", "16", "15")
	sp.NegBase = ethNegBase(sp, "rx", "hds")
	return sp
}

// ethChanSpace: ethtool -L, ETHTOOL_MSG_CHANNELS_SET (18). netdevsim has
// combined channels only: max_combined is the device's queue count (4) and
// the other maximums are 0 (nsim_get_channels). channels_fill_reply
// reports a count only with a non-zero maximum, so rx, tx and other 0 read
// back absent.
func ethChanSpace() *space {
	zero := func(axisName string) axis {
		return axis{Name: axisName, Values: []value{
			v("absent", "", nil),
			v("0", axisName+" 0", f(axisName, "0", "rt."+axisName, "absent")),
		}}
	}
	dev, devBad := ethDev(1)
	over := "requested channel count exceeds maximum"
	sp := &space{
		Name: "ethchan",
		Axes: []axis{
			dev,
			ethNum("combined", "combined", []string{"1", "2", "4"}, "4"),
			zero("rx"),
			zero("tx"),
			zero("other"),
		},
		Bad: append(devBad,
			badValue{Axis: "combined", Value: bv("0", "combined 0", f("combined", "0")),
				Expect: ethKernel(1, "EINVAL", "requested channel counts would result in no RX or TX channel being configured"),
				Why:    "no combined channel and no RX channel"},
			badValue{Axis: "combined", Value: bv("5", "combined 5", f("combined", "5")), Expect: ethKernel(1, "EINVAL", over),
				Why: "one more combined channel than the device's queues"},
			badValue{Axis: "combined", Value: v("neg", "combined -1", nil), Expect: ethParse, Why: "a negative count"},
			badValue{Axis: "rx", Value: bv("1", "rx 1", f("rx", "1")), Expect: ethKernel(1, "EINVAL", over),
				Why: "an RX channel, over max_rx 0"},
			badValue{Axis: "tx", Value: bv("1", "tx 1", f("tx", "1")), Expect: ethKernel(1, "EINVAL", over),
				Why: "a TX channel, over max_tx 0"},
			badValue{Axis: "other", Value: bv("1", "other 1", f("other", "1")), Expect: ethKernel(1, "EINVAL", over),
				Why: "an other channel, over max_other 0"},
		),
	}
	sp.Build = ethBuild("-L", "-l", "18", "17")
	sp.NegBase = ethNegBase(sp, "combined")
	return sp
}

// ethPauseSpace: ethtool -A, ETHTOOL_MSG_PAUSE_SET (22). netdevsim keeps rx
// and tx and refuses autoneg on (nsim_set_pauseparam), which
// ethnl_set_pause passes to it only when something changed.
func ethPauseSpace() *space {
	dev, devBad := ethDev(76)
	sp := &space{
		Name: "ethpause",
		Axes: []axis{
			dev,
			{Name: "autoneg", Values: []value{
				v("absent", "", nil),
				v("off", "autoneg off", f("autoneg", "0")),
			}},
			ethOnOff("rx", "rx"),
			ethOnOff("tx", "tx"),
		},
		Bad: append(devBad,
			badValue{Axis: "autoneg", Value: v("on", "autoneg on", f("autoneg", "1")), Expect: ethKernel(76, "EINVAL", ""),
				Why: "pause autonegotiation, which netdevsim refuses without a message"},
			badValue{Axis: "rx", Value: v("bogus", "rx bogus", nil), Expect: ethParse, Why: "a flag that is neither on nor off"},
		),
	}
	sp.Build = ethBuild("-A", "-a", "22", "21")
	sp.NegBase = ethNegBase(sp, "autoneg")
	return sp
}

// The FEC link-mode bits (uapi ethtool.h): ETHTOOL_LINK_MODE_FEC_NONE_BIT
// 49, which "off" names, RS 50, BASER 51 and LLRS 74.
const (
	ethFecNone  = "49"
	ethFecRS    = "50"
	ethFecBaseR = "51"
	ethFecLLRS  = "74"
)

// ethFec is an encoding value. names are the bit names ethtool sends:
// fec_parse_bitset (netlink/fec.c) upper-cases each word, except off,
// which it sends as "None", and auto, which sets ETHTOOL_A_FEC_AUTO
// instead. modes are their link-mode bits, which the readback's
// configured modes repeat. active is the bit netdevsim reports active:
// nsim_set_fecparam ORs in FEC_NONE, and FEC_OFF when auto, and takes the
// highest bit (OFF < RS < BASER < LLRS), which fec_prepare_data maps to
// its link mode.
func ethFec(name, words, names, modes, auto, active string) value {
	return v(name, "encoding "+words, f("fec-names", names, "fec-modes", modes, "fec-auto", auto, "fec-active", active))
}

func ethFecSpace() *space {
	dev, devBad := ethDev(83)
	sp := &space{
		Name: "ethfec",
		Axes: []axis{
			dev,
			{Name: "enc", Values: []value{
				ethFec("off", "off", "None", ethFecNone, "0", ethFecNone),
				ethFec("auto", "auto", "", "", "1", ethFecNone),
				ethFec("rs", "rs", "RS", ethFecRS, "0", ethFecRS),
				ethFec("baser", "baser", "BASER", ethFecBaseR, "0", ethFecBaseR),
				ethFec("llrs", "llrs", "LLRS", ethFecLLRS, "0", ethFecLLRS),
				cvv(ethFec("two", "rs baser", "RS,BASER", ethFecRS+","+ethFecBaseR, "0", ethFecBaseR)),
				cvv(ethFec("rsauto", "auto rs", "RS", ethFecRS, "1", ethFecRS)),
				cvv(ethFec("offrs", "off rs", "None,RS", ethFecNone+","+ethFecRS, "0", ethFecRS)),
				cvv(ethFec("mixed", "Rs", "RS", ethFecRS, "0", ethFecRS)),
			}},
		},
		Bad: append(devBad,
			// "none" is upper-cased to NONE, which link_mode_names (where
			// the bit is "None") does not hold; only off sends None.
			// ethnl_parse_bit refuses a name it cannot find with
			// EOPNOTSUPP (bitset.c), not EINVAL.
			badValue{Axis: "enc", Value: v("none", "encoding none", f("fec-names", "NONE", "fec-auto", "0")),
				Expect: ethKernel(83, ethENOTSUP, "bit name not found"), Why: "none, which ethtool sends as NONE, a name no link mode has"},
			// TP is a link mode (ETHTOOL_LINK_MODE_TP_BIT 7) but no FEC
			// mode, which ethtool_link_modes_to_fecparam refuses.
			badValue{Axis: "enc", Value: v("tp", "encoding tp", f("fec-names", "TP", "fec-auto", "0")),
				Expect: ethKernel(83, "EINVAL", "invalid FEC modes requested"), Why: "TP, a link mode that is no FEC mode"},
			badValue{Axis: "enc", Value: v("novalue", "encoding", nil), Expect: ethParse, Why: "encoding with no mode"},
			badValue{Axis: "enc", Value: v("noparam", "", nil), Expect: ethParse, Why: "no parameter at all (parameters missing)"},
		),
	}
	sp.Build = ethBuild("--set-fec", "--show-fec", "30", "29")
	return sp
}

// cvv marks a built value a corner.
func cvv(val value) value {
	val.Corner = true
	return val
}

// The coalescing values netdevsim cannot keep: nsim_set_coalesce copies
// struct ethtool_coalesce only (ethtool.c:56), so what ethnl_set_coalesce
// puts in struct kernel_ethtool_coalesce (the CQE modes, TX aggregation and
// RX CQE moderation) is accepted, since supported_coalesce_params is
// ETHTOOL_COALESCE_ALL_PARAMS, and reads back as 0.
func ethLost(name, arg, spelled, val string) value {
	return cv(name, arg+" "+spelled, f(arg, val, "rt."+arg, "0"))
}

// ethPick is an axis whose values each set one argument.
func ethPick(axisName string, pairs ...string) axis {
	ax := axis{Name: axisName, Values: []value{v("absent", "", nil)}}
	for i := 0; i+1 < len(pairs); i += 2 {
		arg, val := pairs[i], pairs[i+1]
		ax.Values = append(ax.Values, v(arg, arg+" "+val, f(arg, val)))
	}
	return ax
}

// ethCoalSpace: ethtool -C, ETHTOOL_MSG_COALESCE_SET (20).
func ethCoalSpace() *space {
	dev, devBad := ethDev(1)
	sp := &space{
		Name: "ethcoal",
		Axes: []axis{
			dev,
			ethOnOff("arx", "adaptive-rx"),
			ethOnOff("atx", "adaptive-tx"),
			ethNum("rxu", "rx-usecs", []string{"0", "10", "4294967295"}, "4294967295"),
			ethNum("rxf", "rx-frames", []string{"64"}),
			ethNum("txu", "tx-usecs", []string{"50"}),
			ethNum("txf", "tx-frames", []string{"32"}),
			ethPick("irq", "rx-usecs-irq", "5", "rx-frames-irq", "3", "tx-usecs-irq", "6", "tx-frames-irq", "7"),
			ethPick("rate", "pkt-rate-low", "100", "pkt-rate-high", "200000", "sample-interval", "5", "stats-block-usecs", "1000"),
			ethPick("lowhigh", "rx-usecs-low", "1", "rx-frames-low", "2", "tx-usecs-low", "3", "tx-frames-low", "4",
				"rx-usecs-high", "5", "rx-frames-high", "6", "tx-usecs-high", "7", "tx-frames-high", "8"),
			{Name: "cqe", Values: []value{
				v("absent", "", nil),
				ethLost("rxon", "cqe-mode-rx", "on", "1"),
				ethLost("txon", "cqe-mode-tx", "on", "1"),
				v("rxoff", "cqe-mode-rx off", f("cqe-mode-rx", "0")),
			}},
			{Name: "aggr", Values: []value{
				v("absent", "", nil),
				ethLost("bytes", "tx-aggr-max-bytes", "4096", "4096"),
				ethLost("frames", "tx-aggr-max-frames", "8", "8"),
				ethLost("usecs", "tx-aggr-time-usecs", "100", "100"),
			}},
			{Name: "rxcqe", Values: []value{
				v("absent", "", nil),
				ethLost("frames", "rx-cqe-frames", "4", "4"),
				ethLost("nsecs", "rx-cqe-nsecs", "1000", "1000"),
			}},
		},
		Bad: append(devBad,
			badValue{Axis: "rxu", Value: v("u32wrap", "rx-usecs 4294967296", nil), Expect: ethParse,
				Why: "a count over the u32 range"},
			badValue{Axis: "arx", Value: v("bogus", "adaptive-rx bogus", nil), Expect: ethParse,
				Why: "a flag that is neither on nor off"},
		),
	}
	sp.Build = ethBuild("-C", "-c", "20", "19")
	sp.NegBase = ethNegBase(sp, "rxu", "arx")
	return sp
}

// ethShowSpace: the show commands, each one *_GET (get) with the device's
// name. -I sets ETHTOOL_FLAG_STATS (4) in the header's flags, but only for
// the commands that call get_stats_flag (netlink/netlink.c), pause, FEC and
// tsinfo here, and only once the kernel's header policy for the command,
// which get_stats_flag reads with CTRL_CMD_GETPOLICY first, takes it. The
// other commands ignore -I.
func ethShowSpace() *space {
	show := func(name, arg, get string) value { return v(name, arg, f("get", get)) }
	sp := &space{
		Name: "ethshow",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("none", "", nil),
				v("json", "--json", f("json", "1")),
				v("stats", "-I", f("stats", "1")),
			}},
			{Name: "cmd", Values: []value{
				show("rings", "-g", "15"),
				show("channels", "-l", "17"),
				show("pause", "-a", "21"),
				show("coalesce", "-c", "19"),
				show("fec", "--show-fec", "29"),
				cv("tsinfo", "-T", f("get", "25")),
				cv("features", "-k", f("get", "11")),
			}},
			{Name: "dev", Values: []value{
				v("eth0", "eth0", nil),
				cv("lo", "lo", nil),
			}},
		},
		Bad: []badValue{
			{Axis: "dev", Value: v("nosuch", "nosuch0", nil), Expect: expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				Why: "a device that does not exist"},
		},
	}
	sp.Build = func(vals []value) row {
		args := make([]string, 0, len(vals))
		for _, val := range vals {
			args = append(args, val.Arg)
		}
		axes := mergeFacts(vals)
		if axes["stats"] == "1" && !slices.Contains(ethStatsGets, axes["get"]) {
			axes["stats"] = "0"
		}
		return row{
			Cmd:     "ethtool " + joinArgs(args...),
			Proto:   "generic",
			Pre:     []preStep{{Cmd: ethSetup}},
			Modules: ethModules,
			Expect:  expect{RC: intp(0), RefusedBy: refNone},
			Axes:    axes,
		}
	}
	sp.Judge = ethShowJudge
	sp.Derive = func(pos []row) []row { return ethShowJSON(sp, pos) }
	return sp
}

// ethStatsGets are the *_GETs ethtool asks statistics of: PAUSE_GET,
// FEC_GET and TSINFO_GET.
var ethStatsGets = []string{"21", "29", "25"}

// ethShowJSON adds a --json row of eth0 for each command pairwise left
// without one, so that every JSON printer is held to its reply.
func ethShowJSON(sp *space, pos []row) []row {
	var out []row
	for _, val := range sp.Axes[1].Values {
		if val.Name == "tsinfo" {
			continue
		}
		r := sp.Build(sp.assign(map[string]string{"opt": "json", "cmd": val.Name, "dev": "eth0"}))
		if ethHasCmd(pos, r.Cmd) {
			continue
		}
		r.ID = sp.Name + "/json-" + val.Name
		r.Desc = "positive: opt=json cmd=" + val.Name + " dev=eth0: the JSON printer against its reply"
		out = append(out, r)
	}
	return out
}

// ethShowJudge: ethtool's command table marks which commands print JSON,
// and refuses --json on the rest while parsing its arguments, before any
// request (ethtool.c main: JSON output not available for this
// subcommand). lo has none of the ring, channel, pause, coalescing or FEC
// operations, so each of those gets EOPNOTSUPP from its prepare_data.
func ethShowJudge(p map[string]string) (*expect, string) {
	if p["opt"] == "json" && p["cmd"] == "tsinfo" {
		return &expect{RC: intp(1), RefusedBy: refIproute2}, "-T, which prints no JSON"
	}
	if p["dev"] != "lo" {
		return nil, ""
	}
	switch p["cmd"] {
	case "tsinfo", "features":
		return nil, ""
	}
	return &expect{RC: intp(1), Errno: ethENOTSUP, RefusedBy: refKernel}, "lo lacks the " + p["cmd"] + " operation"
}

// ethHasCmd says whether a row of rows runs cmd.
func ethHasCmd(rows []row, cmd string) bool {
	for i := range rows {
		if rows[i].Cmd == cmd {
			return true
		}
	}
	return false
}
