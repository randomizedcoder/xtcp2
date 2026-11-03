package goipparity

import (
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The GW rows' sources: the workbench corpus and its O5 golden
// (goip-write-keywords.md, "GW5 — matrix rows and the VM gate").
const (
	gwWsDirCst      = "../../pkg/xtcpnl/testdata/7_3_0/ws"
	gwO5GoldenCst   = "../goip/testdata/ws-differential.txt"
	gwSlugPrefixCst = "gw_"
	gwIPPrefixCst   = "ip "
	gwStepSepCst    = " && "
)

// gwRcs are the ip exit statuses a GW row takes: 0, and 2 for a request the
// kernel refused. Index 0 and 1 of gwForm.expected.
var gwRcs = [2]int{0, 2}

// gwWsRow is the part of a workbench row the GW rows read.
type gwWsRow struct {
	ID     string            `json:"id"`
	Cmd    string            `json:"cmd"`
	Pre    []json.RawMessage `json:"pre"`
	Result struct {
		RC int `json:"rc"`
	} `json:"result"`
	floor int
}

// gwForm is one keyword form: the rows whose cmd (and, when set, whose pre)
// match, and the row ids expected for ip rc 0 and rc 2, "" for none.
type gwForm struct {
	description string
	cmd         string
	pre         string
	expected    [2]string
}

// gwSlug is a workbench id's GW slug: gw_, then the id with its / and - as _.
func gwSlug(id string) string {
	return gwSlugPrefixCst + strings.NewReplacer("/", "_", "-", "_").Replace(id)
}

// gwCorpusDir is the workbench row directory a GW slug names.
func gwCorpusDir(t *testing.T, slug string) string {
	t.Helper()
	fams, err := os.ReadDir(gwWsDirCst)
	if err != nil {
		t.Fatal(err)
	}
	for _, fam := range fams {
		rest, ok := strings.CutPrefix(slug, gwSlug(fam.Name())+"_")
		if !ok {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(gwWsDirCst, fam.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			if gwSlug(fam.Name()+"/"+e.Name()) == slug {
				return filepath.Join(gwWsDirCst, fam.Name(), e.Name())
			}
		}
		t.Fatalf("%s: no %s row %s", slug, fam.Name(), rest)
	}
	t.Fatalf("%s: no workbench family", slug)
	return ""
}

// gwPcapRecords counts a classic pcap's records.
func gwPcapRecords(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const fileHdr, recHdr = 24, 16
	n := 0
	for off := fileHdr; off+recHdr <= len(b); n++ {
		off += recHdr + int(binary.LittleEndian.Uint32(b[off+8:off+12]))
	}
	return n
}

// gwLoad reads the families' rows, sorted by id.
func gwLoad(t *testing.T, families []string) []gwWsRow {
	t.Helper()
	var rows []gwWsRow
	for _, fam := range families {
		ents, err := os.ReadDir(filepath.Join(gwWsDirCst, fam))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			dir := filepath.Join(gwWsDirCst, fam, e.Name())
			var r gwWsRow
			if err := json.Unmarshal([]byte(mustRead(t, filepath.Join(dir, "cmd.json"))), &r); err != nil {
				t.Fatalf("%s: %v", dir, err)
			}
			r.floor = gwPcapRecords(t, filepath.Join(dir, "ip.pcap"))
			rows = append(rows, r)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows
}

// gwNotSame is the set of row ids the O5 golden does not find the same.
func gwNotSame(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, l := range strings.Split(mustRead(t, gwO5GoldenCst), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || strings.HasPrefix(l, "#") {
			continue
		}
		switch f[1] {
		case "refuses", "sends", "differs":
			out[f[0]] = true
		}
	}
	return out
}

// gwPreText is a row's pre steps as one line, for a form's pre pattern.
func gwPreText(r *gwWsRow) string {
	parts := make([]string, 0, len(r.Pre))
	for _, p := range r.Pre {
		var s string
		if json.Unmarshal(p, &s) == nil {
			parts = append(parts, s)
			continue
		}
		var tuple []json.RawMessage
		if json.Unmarshal(p, &tuple) == nil && len(tuple) > 0 && json.Unmarshal(tuple[0], &s) == nil {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, gwStepSepCst)
}

// gwSetup is a row's pre as Setup: each step split on " && ", without its
// leading ip. ok is false when a step is anything else (a sysctl, a module)
// or tolerates a failure, so the row can't be a matrix row.
func gwSetup(r *gwWsRow) (setup []string, ok bool) {
	for _, p := range r.Pre {
		var s string
		if json.Unmarshal(p, &s) != nil {
			return nil, false
		}
		for _, step := range strings.Split(s, gwStepSepCst) {
			rest, found := strings.CutPrefix(strings.TrimSpace(step), gwIPPrefixCst)
			if !found {
				return nil, false
			}
			setup = append(setup, rest)
		}
	}
	return setup, true
}

// gwSelect picks each form's rows: for each rc, the first row by id that O5
// finds the same, whose setup is ip alone, and whose cmd no other picked row
// has, since two rows with one Name would share an allowlist entry.
func gwSelect(rows []gwWsRow, notSame map[string]bool, forms []gwForm) [][2]string {
	picked := map[string]string{}
	out := make([][2]string, len(forms))
	for fi := range forms {
		cmdRe := regexp.MustCompile(forms[fi].cmd)
		var preRe *regexp.Regexp
		if forms[fi].pre != "" {
			preRe = regexp.MustCompile(forms[fi].pre)
		}
		for ri, rc := range gwRcs {
			for i := range rows {
				r := &rows[i]
				if notSame[r.ID] || r.Result.RC != rc || !cmdRe.MatchString(r.Cmd) ||
					(preRe != nil && !preRe.MatchString(gwPreText(r))) {
					continue
				}
				if _, ok := gwSetup(r); !ok {
					continue
				}
				if id, dup := picked[r.Cmd]; dup && id != r.ID {
					continue
				}
				out[fi][ri] = r.ID
				picked[r.Cmd] = r.ID
				break
			}
		}
	}
	return out
}

// runGwMatrix checks the GW rows of families against the corpus: each form
// picks the expected rows, and the table holds exactly those, each with the
// row's cmd, setup and floor.
func runGwMatrix(t *testing.T, families []string, forms []gwForm) {
	t.Helper()
	rows := gwLoad(t, families)
	notSame := gwNotSame(t)
	bySlug := map[string]*gwWsRow{}
	for i := range rows {
		bySlug[gwSlug(rows[i].ID)] = &rows[i]
	}
	got := gwSelect(rows, notSame, forms)
	want := map[string]bool{}
	for fi := range forms {
		t.Run(forms[fi].description, func(t *testing.T) {
			if got[fi] != forms[fi].expected {
				t.Errorf("picked %q, expected %q", got[fi], forms[fi].expected)
			}
		})
		for _, id := range got[fi] {
			if id != "" {
				want[gwSlug(id)] = true
			}
		}
	}
	all := Commands()
	var mine []Command
	for i := range all {
		for _, fam := range families {
			if strings.HasPrefix(all[i].Slug, gwSlugPrefixCst+fam+"_") {
				mine = append(mine, all[i])
			}
		}
	}
	t.Run("positive: the table holds the forms' rows and no others", func(t *testing.T) {
		have := map[string]bool{}
		for i := range mine {
			c := &mine[i]
			have[c.Slug] = true
		}
		for s := range want {
			if !have[s] {
				t.Errorf("%s: a form's row, not in the table", s)
			}
		}
		for s := range have {
			if !want[s] {
				t.Errorf("%s: in the table, no form's row", s)
			}
		}
	})
	t.Run("negative: no row ip refused before sending", func(t *testing.T) {
		for i := range mine {
			c := &mine[i]
			r := bySlug[c.Slug]
			if r == nil {
				t.Errorf("%s: no workbench row", c.Slug)
				continue
			}
			if !slices.Contains(gwRcs[:], r.Result.RC) || notSame[r.ID] {
				t.Errorf("%s: rc %d, not same %v; expected rc 0 or 2 and same", c.Slug, r.Result.RC, notSame[r.ID])
			}
		}
	})
	t.Run("boundary: each floor is the row's ip.pcap record count", func(t *testing.T) {
		for i := range mine {
			c := &mine[i]
			if r := bySlug[c.Slug]; r != nil && c.Floor != r.floor {
				t.Errorf("%s: Floor %d, expected %d", c.Slug, c.Floor, r.floor)
			}
		}
	})
	t.Run("corner: each setup is the row's pre, split, without ip", func(t *testing.T) {
		for i := range mine {
			c := &mine[i]
			r := bySlug[c.Slug]
			if r == nil {
				continue
			}
			if s, ok := gwSetup(r); !ok || !slices.Equal(c.Setup, s) {
				t.Errorf("%s: Setup %q, expected %q", c.Slug, c.Setup, s)
			}
		}
	})
	t.Run("corner: each row is the row's cmd, an implemented write", func(t *testing.T) {
		for i := range mine {
			c := &mine[i]
			r := bySlug[c.Slug]
			if r == nil {
				continue
			}
			if c.Name != strings.TrimPrefix(r.Cmd, gwIPPrefixCst) || !c.Write || !c.Implemented {
				t.Errorf("%s: %q Write=%v Implemented=%v, expected %q, true, true", c.Slug, c.Name, c.Write, c.Implemented, r.Cmd)
			}
		}
	})
	t.Run("corner: each name and slug is unique in the table", func(t *testing.T) {
		for i := range mine {
			c := &mine[i]
			n := 0
			for j := range all {
				if all[j].Name == c.Name || all[j].Slug == c.Slug {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%s (%q): %d rows share its name or slug", c.Slug, c.Name, n)
			}
		}
	})
}

// TestGwRouteMatrixRows pins GW5-T01's rows (goip-write-keywords.md, "GW5-T01
// the route rows"): one per route keyword form GW1 built and ip rc.
//
// go test ./internal/goipparity/ -run TestGwRouteMatrixRows
func TestGwRouteMatrixRows(t *testing.T) {
	runGwMatrix(t, []string{"route4", "route6", "fibt", "fibnh"}, []gwForm{
		{"corner: nhid on a v4 unicast, past 7 fibnh rows that run sysctl", `^ip (-4 )?route \w+ (unicast )?[\d.]+(/\d+)? .*\bnhid \d+$`, "",
			[2]string{"route4/pw-023", ""}},
		{"corner: nhid on a v4 blackhole; rc 2's dup-2 repeats ec-006's argv", `^ip (-4 )?route \w+ blackhole .*\bnhid \d+`, "",
			[2]string{"route4/ec-006", ""}},
		{"positive: nhid on IPv6", `^ip -6 route .*\bnhid \d+`, "", [2]string{"route6/ec-006", "route6/gone-3"}},
		{"positive: pref low", `\bpref low\b`, "", [2]string{"route6/ec-002", "route6/gone-3"}},
		{"positive: pref medium", `\bpref medium\b`, "", [2]string{"route6/ec-003", "route6/pw-030"}},
		{"positive: pref high, rc 0 alone", `\bpref high\b`, "", [2]string{"route6/ec-004", ""}},
		{"corner: pref by number, every row runs sysctl", `\bpref \d`, "", [2]string{"", ""}},
		{"positive: expires", `\bexpires \d`, "", [2]string{"route6/ec-002", "route6/pw-029"}},
		{"boundary: tos, no route row", `^ip .*route .*\btos\b`, "", [2]string{"", ""}},
		{"corner: dsfield, every row runs sysctl", `\bdsfield\b`, "", [2]string{"", ""}},
		{"boundary: realms TO, no route row", `\brealms? [^/ ]+( |$)`, "", [2]string{"", ""}},
		{"boundary: realms FROM/TO, no route row", `\brealms? \S+/\S+`, "", [2]string{"", ""}},
		{"corner: vrf, a table under 256, every row runs sysctl", `\bvrf \S+`, `type vrf table ([1-9]|[1-9]\d|1\d\d|2[0-4]\d|25[0-5])\b`,
			[2]string{"", ""}},
		{"corner: vrf, a table over 255, every row runs sysctl", `\bvrf \S+`, `type vrf table (25[6-9]|2[6-9]\d|[3-9]\d\d|\d{4,})\b`,
			[2]string{"", ""}},
		{"corner: two hops, past the fib rows that run sysctl", `nexthop .*nexthop `, "", [2]string{"route4/pw-004", "route4/ec-005"}},
		{"boundary: hop weights 1 and 256", `weight 1 .*weight 256`, "", [2]string{"route4/pw-004", "route4/ec-005"}},
		{"boundary: a hop onlink, no row", `^ip .*route .*nexthop .*onlink`, "", [2]string{"", ""}},
		{"boundary: a hop via inet6 on IPv4, no row", `^ip (-4 )?route .*nexthop .*via inet6`, "", [2]string{"", ""}},
		{"boundary: a hop's realms, no row", `nexthop .*realms`, "", [2]string{"", ""}},
	})
}

// TestGwSetup pins gwSetup, which decides which workbench rows can be
// matrix rows.
//
// go test ./internal/goipparity/ -run TestGwSetup
func TestGwSetup(t *testing.T) {
	type result struct {
		setup []string
		ok    bool
	}
	tests := []struct {
		description string
		pre         string
		expected    result
	}{
		{"positive: ip steps split and trimmed", `["ip link add d0 type dummy && ip link set d0 up"]`,
			result{[]string{"link add d0 type dummy", "link set d0 up"}, true}},
		{"positive: two pre entries in order", `["ip link add d0 type dummy", "ip -6 nexthop add id 10 dev d0"]`,
			result{[]string{"link add d0 type dummy", "-6 nexthop add id 10 dev d0"}, true}},
		{"negative: a sysctl step", `["ip link add d0 type dummy && sysctl -qw net.ipv4.ip_forward=1"]`, result{nil, false}},
		{"negative: a step tolerating a failure", `[["ip link add d0 type dummy", [2]]]`, result{nil, false}},
		{"boundary: no pre", `[]`, result{nil, true}},
		{"corner: ip as a word's prefix is not ip", `["ipset create s hash:ip"]`, result{nil, false}},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var r gwWsRow
			if err := json.Unmarshal([]byte(tt.pre), &r.Pre); err != nil {
				t.Fatal(err)
			}
			s, ok := gwSetup(&r)
			if ok != tt.expected.ok || !slices.Equal(s, tt.expected.setup) {
				t.Errorf("gwSetup = %q, %v; expected %q, %v", s, ok, tt.expected.setup, tt.expected.ok)
			}
		})
	}
}
