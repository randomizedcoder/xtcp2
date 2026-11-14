package xtcpnl

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The stats rows' oracles (tools/nlcapgen/matrix_stats.go, NC4-T02). O3
// checks each show's RTM_GETSTATS: the filter mask, the
// IFLA_STATS_GET_FILTERS nest (sent only for a partial offload sub-mask),
// and a dump or a get of one device; and each `stats set`'s RTM_SETSTATS.
// The order test pins the order the bare show prints a device's groups in.

// wsIflaStatsSetL3Stats is IFLA_STATS_SET_OFFLOAD_XSTATS_L3_STATS
// (include/uapi/linux/if_link.h), which golang.org/x/sys/unix doesn't name.
const wsIflaStatsSetL3Stats uint16 = 2

// statsRequestProblem checks one RTM_GETSTATS against a show's axes. d0
// is d0's index.
func statsRequestProblem(ax map[string]string, flags uint16, body []byte, d0 int32) error {
	if len(body) < IfStatsMsgSizeCst {
		return fmt.Errorf("body of %d bytes", len(body))
	}
	if body[0] != unix.AF_UNSPEC {
		return fmt.Errorf("family %d, want AF_UNSPEC", body[0])
	}
	index := int32(binary.LittleEndian.Uint32(body[4:8]))
	dump := flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP
	if ax["dev"] != "" {
		if dump || index != d0 {
			return fmt.Errorf("ifindex %d, dump %t, want a get of %d", index, dump, d0)
		}
	} else if !dump || index != 0 {
		return fmt.Errorf("ifindex %d, dump %t, want a dump", index, dump)
	}
	if got := strconv.FormatUint(uint64(binary.LittleEndian.Uint32(body[8:12])), 10); got != ax["mask"] {
		return fmt.Errorf("filter_mask %s, want %s", got, ax["mask"])
	}
	types, vals, err := v730Attrs(body, IfStatsMsgSizeCst)
	if err != nil {
		return err
	}
	if ax["offload"] == "" {
		if len(types) != 0 {
			return fmt.Errorf("attributes %v, want none", types)
		}
		return nil
	}
	if !slices.Equal(types, []uint16{unix.IFLA_STATS_GET_FILTERS}) {
		return fmt.Errorf("attributes %v, want IFLA_STATS_GET_FILTERS alone", types)
	}
	ntypes, nest, err := v730Attrs(vals[unix.IFLA_STATS_GET_FILTERS], 0)
	if err != nil {
		return err
	}
	sub := nest[unix.IFLA_STATS_LINK_OFFLOAD_XSTATS]
	if !slices.Equal(ntypes, []uint16{unix.IFLA_STATS_LINK_OFFLOAD_XSTATS}) || len(sub) != 4 ||
		strconv.FormatUint(uint64(binary.LittleEndian.Uint32(sub)), 10) != ax["offload"] {
		return fmt.Errorf("filters %v %x, want the offload sub-mask %s", ntypes, sub, ax["offload"])
	}
	return nil
}

// statsSetProblem checks a `stats set dev d0 l3_stats` request.
func statsSetProblem(ax map[string]string, body []byte, d0 int32) error {
	if len(body) < IfStatsMsgSizeCst {
		return fmt.Errorf("body of %d bytes", len(body))
	}
	if index := int32(binary.LittleEndian.Uint32(body[4:8])); index != d0 {
		return fmt.Errorf("ifindex %d, want %d", index, d0)
	}
	types, vals, err := v730Attrs(body, IfStatsMsgSizeCst)
	if err != nil {
		return err
	}
	if !slices.Equal(types, []uint16{wsIflaStatsSetL3Stats}) {
		return fmt.Errorf("attributes %v, want the l3_stats u8 alone", types)
	}
	return v730U8Problem("IFLA_STATS_SET_OFFLOAD_XSTATS_L3_STATS", vals, wsIflaStatsSetL3Stats, ax["l3_stats"])
}

func TestWsStatsCorpus(t *testing.T) {
	var shows, sets int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "stats/") || row.Axes == nil {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
			d0 := wsLinkIndexOf(msgs, "d0")
			typ := uint16(unix.RTM_GETSTATS)
			if row.Axes["l3_stats"] != "" {
				typ = unix.RTM_SETSTATS
			}
			reqs := wsRouteRequests(msgs, typ)
			if len(reqs) != 1 {
				t.Fatalf("O3: %d requests of type %d, want 1\n  cmd: %s", len(reqs), typ, row.Cmd)
			}
			var err error
			if typ == unix.RTM_SETSTATS {
				err = statsSetProblem(row.Axes, reqs[0].body, d0)
				sets++
			} else {
				err = statsRequestProblem(row.Axes, reqs[0].h.Flags, reqs[0].body, d0)
				shows++
			}
			if err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
		})
	}
	t.Logf("O3 checked %d shows and %d sets", shows, sets)
	if shows == 0 || sets == 0 {
		t.Error("O3 checked nothing: are there stats rows under testdata/*/ws?")
	}
}

// wsStatsOrder is the order ip 7.2.0 prints a device's groups in on a bare
// `ip stats show`: ipstats_stat_descs sorted by pointer, the same on every
// device, topology and boot captured (V025).
var wsStatsOrder = []string{
	"afstats mpls", "link", "offload l3_stats", "offload hw_stats_info", "offload cpu_hit",
	"xstats_slave bond 802.3ad", "xstats bond 802.3ad",
	"xstats_slave bridge vlan", "xstats_slave bridge mcast", "xstats_slave bridge stp",
	"xstats bridge vlan", "xstats bridge mcast", "xstats bridge stp",
}

// statsGroupLine is a heading of the bare show's text: the device, then
// the group, and its subgroup and suite when it has them.
var statsGroupLine = regexp.MustCompile(`^\d+: (\S+): group (\S+)(?: subgroup (\S+))?(?: suite (\S+))?`)

// statsGroupOrder reads each device's group headings, in order.
func statsGroupOrder(lines []string) (devs []string, order map[string][]string) {
	order = map[string][]string{}
	for _, l := range lines {
		m := statsGroupLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if _, ok := order[m[1]]; !ok {
			devs = append(devs, m[1])
		}
		order[m[1]] = append(order[m[1]], strings.Join(slices.DeleteFunc(m[2:], func(s string) bool { return s == "" }), " "))
	}
	return devs, order
}

func TestWsStatsGroupOrder(t *testing.T) {
	var checked int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "stats/show-") || !strings.HasSuffix(row.ID, "-text") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			var lines []string
			for sc := bufio.NewScanner(f); sc.Scan(); {
				lines = append(lines, sc.Text())
			}
			devs, order := statsGroupOrder(lines)
			if len(devs) < 2 {
				t.Fatalf("%d devices in the show, want lo, d0 and more", len(devs))
			}
			for _, d := range devs {
				if !slices.Equal(order[d], wsStatsOrder) {
					t.Errorf("%s: groups %q, want %q", d, order[d], wsStatsOrder)
				}
				checked++
			}
		})
	}
	if checked == 0 {
		t.Error("no bare stats show under testdata/*/ws")
	}
}

func TestStatsProblems(t *testing.T) {
	u32 := func(n uint32) []byte { return binary.LittleEndian.AppendUint32(nil, n) }
	msg := func(index int32, mask uint32, attrs ...[]byte) []byte {
		h := make([]byte, IfStatsMsgSizeCst)
		binary.LittleEndian.PutUint32(h[4:8], uint32(index))
		binary.LittleEndian.PutUint32(h[8:12], mask)
		return cat(append([][]byte{h}, attrs...)...)
	}
	filters := func(sub uint32) []byte {
		return rtattr(unix.IFLA_STATS_GET_FILTERS, rtattr(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS, u32(sub)))
	}
	const dump = unix.NLM_F_REQUEST | unix.NLM_F_DUMP
	cases := []struct {
		description string
		ax          map[string]string
		flags       uint16
		body        []byte
		expected    string // "" or a substring of the error
	}{
		{"positive: group offload subgroup l3_stats", map[string]string{"mask": "8", "offload": "6"}, dump, msg(0, 8, filters(6)), ""},
		{"positive: group offload", map[string]string{"mask": "8"}, dump, msg(0, 8), ""},
		{"boundary: a bare show", map[string]string{"mask": "31"}, dump, msg(0, 31), ""},
		{"negative: a nest on a full sub-mask", map[string]string{"mask": "8"}, dump, msg(0, 8, filters(7)), "want none"},
		{"negative: the wrong sub-mask", map[string]string{"mask": "8", "offload": "6"}, dump, msg(0, 8, filters(1)), "want the offload sub-mask 6"},
		{"negative: the wrong mask", map[string]string{"mask": "16"}, dump, msg(0, 1), "filter_mask 1, want 16"},
		{"negative: a dump for one device", map[string]string{"mask": "31", "dev": "d0"}, dump, msg(0, 31), "want a get of 3"},
		{"negative: one device's index, but dumped", map[string]string{"mask": "31", "dev": "d0"}, dump, msg(3, 31), "dump true"},
		{"positive: a get of one device", map[string]string{"mask": "31", "dev": "d0"}, unix.NLM_F_REQUEST, msg(3, 31), ""},
		{"corner: a short body", map[string]string{"mask": "31"}, dump, []byte{0}, "body of 1"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, statsRequestProblem(tc.ax, tc.flags, tc.body, 3), tc.expected)
		})
	}
	sets := []struct {
		description string
		ax          map[string]string
		body        []byte
		expected    string
	}{
		{"positive: l3_stats on", map[string]string{"l3_stats": "1"}, msg(3, 0, rtattr(wsIflaStatsSetL3Stats, []byte{1})), ""},
		{"negative: l3_stats as a u32", map[string]string{"l3_stats": "1"}, msg(3, 0, rtattr(wsIflaStatsSetL3Stats, u32(1))), "want the u8 1"},
		{"negative: another device", map[string]string{"l3_stats": "0"}, msg(4, 0, rtattr(wsIflaStatsSetL3Stats, []byte{0})), "ifindex 4"},
	}
	for _, tc := range sets {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, statsSetProblem(tc.ax, tc.body, 3), tc.expected)
		})
	}
	orders := []struct {
		description string
		lines       []string
		expected    string // each device's groups
	}{
		{"positive: a group, a subgroup, a suite", []string{"1: lo: group link", "      12 0", "1: lo: group xstats subgroup bond suite 802.3ad",
			"1: lo: group offload subgroup l3_stats off used off"}, "lo=link,xstats bond 802.3ad,offload l3_stats"},
		{"corner: two devices", []string{"1: lo: group link", "3: d0: group link"}, "lo=link;d0=link"},
	}
	for _, tc := range orders {
		t.Run(tc.description, func(t *testing.T) {
			devs, order := statsGroupOrder(tc.lines)
			parts := make([]string, 0, len(devs))
			for _, d := range devs {
				parts = append(parts, d+"="+strings.Join(order[d], ","))
			}
			if got := strings.Join(parts, ";"); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
