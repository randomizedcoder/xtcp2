package main

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// ethRow builds a row of sp from value names by axis, the other axes
// taking their first value.
func ethRow(sp *space, kv ...string) row {
	return sp.Build(sp.assign(f(kv...)))
}

func TestEthBuild(t *testing.T) {
	tests := []struct {
		description string
		sp          *space
		pick        []string
		cmd         string
		axes        map[string]string
	}{
		{description: "positive: a ring set names its arguments and its readback", sp: ethRingSpace(),
			pick: []string{"rx", "1024", "tx", "256"}, cmd: "ethtool -G eth0 rx 1024 tx 256",
			axes: map[string]string{"set": "16", "get": "15", "rx": "1024", "tx": "256"}},
		{description: "corner: tcp-data-split auto reads back on", sp: ethRingSpace(), pick: []string{"tds", "auto"},
			cmd:  "ethtool -G eth0 tcp-data-split auto",
			axes: map[string]string{"set": "16", "get": "15", "tcp-data-split": "0", "rt.tcp-data-split": "2"}},
		{description: "boundary: a channel count of 0 reads back absent", sp: ethChanSpace(), pick: []string{"combined", "4", "rx", "0"},
			cmd:  "ethtool -L eth0 combined 4 rx 0",
			axes: map[string]string{"set": "18", "get": "17", "combined": "4", "rx": "0", "rt.rx": "absent"}},
		{description: "positive: pause flags are u8 booleans", sp: ethPauseSpace(), pick: []string{"autoneg", "off", "rx", "on", "tx", "off"},
			cmd:  "ethtool -A eth0 autoneg off rx on tx off",
			axes: map[string]string{"set": "22", "get": "21", "autoneg": "0", "rx": "1", "tx": "0"}},
		{description: "corner: two FEC modes, the higher active", sp: ethFecSpace(), pick: []string{"enc", "two"},
			cmd: "ethtool --set-fec eth0 encoding rs baser",
			axes: map[string]string{"set": "30", "get": "29", "fec-names": "RS,BASER", "fec-modes": "50,51", "fec-auto": "0",
				"fec-active": "51"}},
		{description: "corner: off is None, the NONE bit active", sp: ethFecSpace(), pick: []string{"enc", "off"},
			cmd:  "ethtool --set-fec eth0 encoding off",
			axes: map[string]string{"set": "30", "get": "29", "fec-names": "None", "fec-modes": "49", "fec-auto": "0", "fec-active": "49"}},
		{description: "corner: a coalescing value netdevsim drops reads back 0", sp: ethCoalSpace(), pick: []string{"cqe", "rxon"},
			cmd:  "ethtool -C eth0 cqe-mode-rx on",
			axes: map[string]string{"set": "20", "get": "19", "cqe-mode-rx": "1", "rt.cqe-mode-rx": "0"}},
		{description: "positive: -I asks pause for statistics", sp: ethShowSpace(), pick: []string{"opt", "stats", "cmd", "pause"},
			cmd: "ethtool -I -a eth0", axes: map[string]string{"get": "21", "stats": "1"}},
		{description: "negative: -I on rings asks nothing", sp: ethShowSpace(), pick: []string{"opt", "stats", "cmd", "rings"},
			cmd: "ethtool -I -g eth0", axes: map[string]string{"get": "15", "stats": "0"}},
		{description: "boundary: --json first", sp: ethShowSpace(), pick: []string{"opt", "json", "cmd", "coalesce", "dev", "lo"},
			cmd: "ethtool --json -c lo", axes: map[string]string{"get": "19", "json": "1"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := ethRow(tc.sp, tc.pick...)
			if r.Cmd != tc.cmd || !maps.Equal(r.Axes, tc.axes) {
				t.Errorf("got %q %v\nwant %q %v", r.Cmd, r.Axes, tc.cmd, tc.axes)
			}
			if r.Proto != "generic" || len(r.Pre) != 1 || r.Pre[0].Cmd != ethSetup || !slices.Equal(r.Modules, ethModules) {
				t.Errorf("proto %q, pre %v, modules %v", r.Proto, r.Pre, r.Modules)
			}
			if _, set := tc.axes["set"]; set != (len(r.Readback) == 1) {
				t.Errorf("readback %v for a row with set %v", r.Readback, set)
			}
			for _, rb := range r.Readback {
				if rb.Proto != "generic" || !strings.HasSuffix(rb.Cmd, " eth0") {
					t.Errorf("readback %+v", rb)
				}
			}
		})
	}
}

func TestEthShowJudge(t *testing.T) {
	tests := []struct {
		description string
		pick        map[string]string
		expected    string // "" when Judge passes the row, else the refusal
	}{
		{description: "positive: rings of eth0", pick: map[string]string{"opt": "none", "cmd": "rings", "dev": "eth0"}},
		{description: "positive: features of lo", pick: map[string]string{"opt": "json", "cmd": "features", "dev": "lo"}},
		{description: "positive: tsinfo of lo", pick: map[string]string{"opt": "none", "cmd": "tsinfo", "dev": "lo"}},
		{description: "negative: rings of lo", pick: map[string]string{"opt": "none", "cmd": "rings", "dev": "lo"}, expected: "kernel ENOTSUP 1"},
		{description: "negative: --json -T", pick: map[string]string{"opt": "json", "cmd": "tsinfo", "dev": "eth0"}, expected: "iproute2  1"},
		{description: "corner: --json -T of lo is refused before the device matters",
			pick: map[string]string{"opt": "json", "cmd": "tsinfo", "dev": "lo"}, expected: "iproute2  1"},
		{description: "boundary: -I pause of lo", pick: map[string]string{"opt": "stats", "cmd": "pause", "dev": "lo"}, expected: "kernel ENOTSUP 1"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := ethShowJudge(tc.pick)
			got := ""
			if exp != nil {
				got = exp.RefusedBy + " " + exp.Errno + " " + strconv.Itoa(*exp.RC)
				if why == "" {
					t.Error("a refusal without a reason")
				}
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestEthShowJSON(t *testing.T) {
	sp := ethShowSpace()
	all := []string{"rings", "channels", "pause", "coalesce", "fec", "features"}
	jsonRow := func(cmd string) row { return ethRow(sp, "opt", "json", "cmd", cmd, "dev", "eth0") }
	tests := []struct {
		description string
		pos         []row
		expected    []string // the derived rows' ids
	}{
		{description: "positive: every JSON command without a row gets one", expected: []string{
			"ethshow/json-rings", "ethshow/json-channels", "ethshow/json-pause", "ethshow/json-coalesce",
			"ethshow/json-fec", "ethshow/json-features"}},
		{description: "negative: a command pairwise covered gets none", pos: []row{jsonRow("rings")}, expected: []string{
			"ethshow/json-channels", "ethshow/json-pause", "ethshow/json-coalesce", "ethshow/json-fec", "ethshow/json-features"}},
		{description: "corner: a row of lo does not count", pos: []row{ethRow(sp, "opt", "json", "cmd", "rings", "dev", "lo")},
			expected: []string{"ethshow/json-rings", "ethshow/json-channels", "ethshow/json-pause", "ethshow/json-coalesce",
				"ethshow/json-fec", "ethshow/json-features"}},
		{description: "boundary: all covered, none derived", pos: func() []row {
			out := make([]row, 0, len(all))
			for _, c := range all {
				out = append(out, jsonRow(c))
			}
			return out
		}()},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			derived := ethShowJSON(sp, tc.pos)
			ids := make([]string, 0, len(derived))
			for _, r := range derived {
				ids = append(ids, r.ID)
				if r.Axes["json"] != "1" || !strings.HasPrefix(r.Desc, "positive:") || !strings.HasSuffix(r.Cmd, " eth0") {
					t.Errorf("%s: %q %v %q", r.ID, r.Cmd, r.Axes, r.Desc)
				}
			}
			if !slices.Equal(ids, tc.expected) {
				t.Errorf("got %v, want %v", ids, tc.expected)
			}
		})
	}
}

func TestMatrixEthtoolRows(t *testing.T) {
	rows, err := matrixRows(families["ethtool"](), matrixOpts{Seed: 1, Random: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	refusalRC := map[string][]int{"ethring": {1, 81}, "ethchan": {1}, "ethpause": {1, 76}, "ethfec": {1, 83}, "ethcoal": {1}, "ethshow": {1}}
	cases := []struct {
		description string
		check       func(r *row) bool
	}{
		{"positive: every row runs ethtool on generic netlink with its netdevsim", func(r *row) bool {
			return strings.HasPrefix(r.Cmd, "ethtool ") && r.Proto == "generic" && slices.Equal(r.Modules, ethModules) &&
				len(r.Pre) == 1 && r.Pre[0].Cmd == ethSetup
		}},
		{"positive: every refusal has its command's exit status", func(r *row) bool {
			rc := *r.Expect.RC
			return rc == 0 || slices.Contains(refusalRC[strings.Split(r.ID, "/")[0]], rc)
		}},
		{"positive: every kernel refusal names an errno", func(r *row) bool {
			return r.Expect.RefusedBy != "kernel" || r.Expect.Errno != ""
		}},
		{"negative: no accepted row expects an errno or a refusal", func(r *row) bool {
			return *r.Expect.RC != 0 || (r.Expect.Errno == "" && r.Expect.RefusedBy == "none")
		}},
		{"corner: every set row names its commands and reads back once", func(r *row) bool {
			if strings.HasPrefix(r.ID, "ethshow/") {
				return r.Axes["get"] != "" && len(r.Readback) == 0
			}
			return r.Axes["set"] != "" && r.Axes["get"] != "" && len(r.Readback) == 1
		}},
		{"boundary: every rt fact belongs to an argument the row sets", func(r *row) bool {
			for k := range r.Axes {
				if name, ok := strings.CutPrefix(k, "rt."); ok && r.Axes[name] == "" {
					return false
				}
			}
			return true
		}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for i := range rows {
				if !tc.check(&rows[i]) {
					t.Errorf("%s: %s %+v", rows[i].ID, rows[i].Cmd, rows[i].Expect)
				}
			}
		})
	}
}
