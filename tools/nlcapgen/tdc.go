package main

// The tdc converter. tdc (tools/testing/selftests/tc-testing) is the
// kernel's own tc test suite: 1334 cases, each a setup list, one command
// under test with its expected exit status, and a verify command whose
// output must match a regexp a given number of times. tdc runs every case
// in a namespace holding a veth pair, a dummy and an ifb (nsPlugin.py), so
// a converted row creates the same devices, under the same names, before
// its command runs. The names are kept so that tdc's match patterns, which
// often name a device, still match.
//
// What does not carry over: tdc's teardown (the row's namespace is thrown
// away), its scapy traffic (so a pattern that counts packets is left
// unchecked, and says so in source.unchecked) and its matchJSON checks.
// Cases that need what the workbench cannot provide are skipped, each with
// a reason, never dropped silently.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type tdcCase struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Setup        []json.RawMessage `json:"setup"`
	CmdUnderTest string            `json:"cmdUnderTest"`
	ExpExitCode  json.RawMessage   `json:"expExitCode"`
	VerifyCmd    string            `json:"verifyCmd"`
	MatchPattern *string           `json:"matchPattern"`
	MatchCount   json.RawMessage   `json:"matchCount"`
	MatchJSON    json.RawMessage   `json:"matchJSON"`
	Scapy        json.RawMessage   `json:"scapy"`
	Plugins      json.RawMessage   `json:"plugins"`
	DependsOn    string            `json:"dependsOn"`
}

// tdcKernelGaps are what tdc cases use that the workbench kernel (7.3.0,
// the capture flavor's config) is built without. A case that uses one
// would fail, or pass for the wrong reason, so it is skipped instead.
var tdcKernelGaps = []struct {
	file, word, reason string
}{
	{"actions/gact.json", "random", "gact's random needs CONFIG_GACT_PROB, which the workbench kernel lacks"},
}

// tdcPredates are cases that test a kernel change the workbench kernel
// (7.3.0) predates. The row still runs, so its capture records the older
// behavior, but its match pattern, which describes the newer one, is left
// unchecked with the reason. Keyed by file and tdc id.
var tdcPredates = map[string]string{
	"actions/batch-delete.json/8135": "tests f67b3ed1308d (net/sched: act_api: reject duplicate actions in a batch, " +
		"7.4 merge window); 7.3.0 deletes the first action before refusing the duplicate",
}

// tdcTooBig are cases whose capture the workbench cannot take whole,
// keyed like tdcPredates. Measured on the workbench, alone (-j 1).
var tdcTooBig = map[string]string{
	"filters/u32.json/1234": "its 2048-iteration tc -b loop sends 4096 requests in one burst, and nlmon dropped " +
		"3337 of 4858 datagrams even with the row alone",
}

// tdcNames is tdc_config.py's NAMES, as the workbench provides them.
var tdcNames = map[string]string{
	"TC":      "tc",
	"IP":      "ip",
	"DEV0":    "v0p0",
	"DEV1":    "v0p1",
	"DUMMY":   "dummy1",
	"IFB":     "ifbtdc0",
	"ETH":     "eth0",
	"ETHTOOL": "ethtool",
}

// tdcUnavailable names the tdc variables the workbench has no answer for.
var tdcUnavailable = map[string]string{
	"EBPFDIR":    "needs a compiled eBPF object ($EBPFDIR)",
	"DEV2":       "needs a second physical device ($DEV2)",
	"BATCH_DIR":  "needs tdc_multibatch.py and its batch files ($BATCH_DIR)",
	"BATCH_FILE": "needs a batch file written by tdc ($BATCH_FILE)",
}

var tdcVarRE = regexp.MustCompile(`\$([A-Z_][A-Z0-9_]*)`)

// tdcGuestModules is net/sched of the workbench guest's kernel (7.3.0):
// the modules a converted row may ask the batch runner to load up front.
// Everything else a case uses is built in or autoloaded by the kernel.
var tdcGuestModules = map[string]bool{}

func init() {
	for _, m := range strings.Fields(`act_bpf act_connmark act_csum act_ctinfo act_ct act_gact
		act_gate act_ife act_mirred act_mpls act_nat act_pedit act_police act_sample act_simple
		act_skbedit act_skbmod act_tunnel_key act_vlan cls_basic cls_bpf cls_cgroup cls_flower
		cls_flow cls_fw cls_matchall cls_route cls_u32 sch_cake sch_cbs sch_choke sch_codel
		sch_drr sch_dualpi2 sch_etf sch_ets sch_fq_codel sch_fq sch_fq_pie sch_gred sch_hfsc
		sch_hhf sch_htb sch_ingress sch_mqprio sch_multiq sch_netem sch_pie sch_plug sch_prio
		sch_qfq sch_red sch_sfb sch_sfq sch_skbprio sch_taprio sch_tbf sch_teql`) {
		tdcGuestModules[m] = true
	}
}

// tdcModuleAlias maps a tdc file to its module where the file is not named
// after the kind it tests.
var tdcModuleAlias = map[string]string{
	"qdiscs/cake_mq":         "sch_cake",
	"qdiscs/hhf_flows_limit": "sch_hhf",
	"actions/gact-rollback":  "act_gact",
	"actions/batch-delete":   "act_gact",
}

// tdcModule is the module a tdc file's kind lives in, or "" when it is
// built in or unknown.
func tdcModule(file string) string {
	stem := strings.TrimSuffix(file, ".json")
	m, ok := tdcModuleAlias[stem]
	if !ok {
		dir, base := path.Split(stem)
		prefix := map[string]string{"qdiscs/": "sch_", "actions/": "act_", "filters/": "cls_"}[dir]
		if prefix == "" {
			return ""
		}
		m = prefix + base
	}
	if !tdcGuestModules[m] {
		return ""
	}
	return m
}

func tdcHeader(rev string, rows, skips int) []string {
	if rev == "" {
		rev = "(revision not recorded)"
	}
	return []string{
		"Generated by `nlcapgen tdc` from linux " + rev + ",",
		"tools/testing/selftests/tc-testing/tc-tests. Do not edit; regenerate.",
		fmt.Sprintf("%d rows; %d tdc cases skipped, each with its reason, in tdc.skipped.jsonl.", rows, skips),
	}
}

// convertTdcTree converts every tc-tests/**/*.json under root, in path
// order and case order within a file.
func convertTdcTree(root fs.FS) ([]row, []skip, error) {
	files, err := fs.Glob(root, "*/*.json")
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	var rows []row
	var skips []skip
	seen := map[string]bool{}
	for _, f := range files {
		data, err := fs.ReadFile(root, f)
		if err != nil {
			return nil, nil, err
		}
		var cases []tdcCase
		if err := json.Unmarshal(data, &cases); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", f, err)
		}
		for i := range cases {
			c := &cases[i]
			r, reason := convertTdcCase(f, *c)
			if reason == "" && seen[r.ID] {
				reason = "repeats the id of an earlier case in " + f
			}
			if reason != "" {
				skips = append(skips, skip{Tdc: f, ID: c.ID, Name: c.Name, Reason: reason})
				continue
			}
			seen[r.ID] = true
			rows = append(rows, r)
		}
	}
	if err := checkRows(rows); err != nil {
		return nil, nil, err
	}
	return rows, skips, nil
}

// convertTdcCase turns one case into a row, or says why it cannot.
func convertTdcCase(file string, c tdcCase) (row, string) {
	if c.ID == "" || strings.TrimSpace(c.CmdUnderTest) == "" {
		return row{}, "no id or no command under test"
	}
	if reason, ok := tdcTooBig[file+"/"+c.ID]; ok {
		return row{}, reason
	}
	if strings.TrimSpace(c.CmdUnderTest) == "/bin/true" {
		return row{}, "the command under test is /bin/true: the case tests its setup, which runs outside the capture"
	}
	setup, err := tdcSetup(c.Setup)
	if err != nil {
		return row{}, err.Error()
	}
	texts := append([]string{c.CmdUnderTest, c.VerifyCmd, c.DependsOn}, setupCmds(setup)...)
	if reason := tdcUnusable(texts); reason != "" {
		return row{}, reason
	}
	for _, g := range tdcKernelGaps {
		if file == g.file && tdcUsesWord(texts, g.word) {
			return row{}, g.reason
		}
	}
	rc, err := tdcInt(c.ExpExitCode)
	if err != nil {
		return row{}, "expExitCode: " + err.Error()
	}
	r := row{
		ID:     "tdc/" + strings.TrimSuffix(file, ".json") + "/" + strings.ToLower(c.ID),
		Desc:   tdcDesc(rc, c.Name),
		Cmd:    tdcSubst(c.CmdUnderTest),
		Expect: expect{RC: &rc},
		Source: &source{Tdc: file, ID: c.ID},
	}
	r.Pre, r.Modules, r.Serial = tdcEnv(file, texts, tdcNeedsNs(c.Plugins))
	if c.DependsOn != "" {
		r.Pre = append(r.Pre, preStep{Cmd: tdcSubst(c.DependsOn)})
	}
	for _, s := range setup {
		r.Pre = append(r.Pre, preStep{Cmd: tdcSubst(s.Cmd), OK: s.OK})
	}
	if c.VerifyCmd != "" {
		r.Readback = []readback{{Name: "verify", Cmd: tdcSubst(c.VerifyCmd)}}
		r.Expect.Match, r.Source.Unchecked = tdcMatch(c)
	}
	if reason, ok := tdcPredates[file+"/"+c.ID]; ok {
		r.Expect.Match, r.Source.Unchecked = nil, reason
	}
	if err := r.validate(); err != nil {
		return row{}, err.Error()
	}
	return r, ""
}

func setupCmds(s []preStep) []string {
	out := make([]string, 0, len(s))
	for _, p := range s {
		out = append(out, p.Cmd)
	}
	return out
}

// tdcSetup reads tdc's setup list: each step is a command, or a list of a
// command and the exit statuses it may end with.
func tdcSetup(raw []json.RawMessage) ([]preStep, error) {
	out := make([]preStep, 0, len(raw))
	for _, m := range raw {
		var s string
		if json.Unmarshal(m, &s) == nil {
			out = append(out, preStep{Cmd: s})
			continue
		}
		var l []json.RawMessage
		if err := json.Unmarshal(m, &l); err != nil || len(l) == 0 || json.Unmarshal(l[0], &s) != nil {
			return nil, fmt.Errorf("setup step %s is neither a command nor [command, rc...]", m)
		}
		p := preStep{Cmd: s}
		for _, e := range l[1:] {
			rc, err := tdcInt(e)
			if err != nil {
				return nil, fmt.Errorf("setup step %s: %w", m, err)
			}
			if rc != 0 && !containsInt(p.OK, rc) {
				p.OK = append(p.OK, rc)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// tdcInt reads a number tdc writes either as a JSON number or a string.
func tdcInt(raw json.RawMessage) (int, error) {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("%s is not a number", raw)
	}
	return strconv.Atoi(strings.TrimSpace(s))
}

// tdcUnusable says why a case's commands cannot run on the workbench, or "".
func tdcUnusable(texts []string) string {
	var vars []string
	for _, t := range texts {
		for _, m := range tdcVarRE.FindAllStringSubmatch(t, -1) {
			vars = append(vars, m[1])
		}
		if strings.Contains(t, "./tdc_") || strings.Contains(t, "./scripts/") {
			return "runs a tdc helper script (" + t + ")"
		}
	}
	sort.Strings(vars)
	for _, v := range vars {
		if reason, ok := tdcUnavailable[v]; ok {
			return reason
		}
		if _, ok := tdcNames[v]; !ok {
			return "uses $" + v + ", which tdc_config.py does not define"
		}
	}
	return ""
}

// tdcBins maps the absolute paths tdc cases call to what the NixOS guest,
// which has no /bin beyond sh, runs instead.
var tdcBins = strings.NewReplacer("/bin/true", "true", "/bin/false", "false")

func tdcSubst(s string) string {
	return tdcVarRE.ReplaceAllStringFunc(tdcBins.Replace(s), func(v string) string {
		if n, ok := tdcNames[v[1:]]; ok {
			return n
		}
		return v
	})
}

func tdcDesc(rc int, name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if rc == 0 {
		return "positive: " + name
	}
	return "negative: " + name
}

func tdcUses(texts []string, vars ...string) bool {
	for _, t := range texts {
		for _, v := range vars {
			if strings.Contains(t, "$"+v) {
				return true
			}
		}
	}
	return false
}

// tdcUsesWord says whether a command uses word as a whole argument.
func tdcUsesWord(texts []string, word string) bool {
	for _, t := range texts {
		for _, f := range strings.Fields(t) {
			if f == word {
				return true
			}
		}
	}
	return false
}

// tdcNeedsNs says whether a case requires nsPlugin, the plugin that makes
// tdc's devices. tdc runs a case without it alone, in the root namespace,
// where the case makes whatever devices it needs itself.
func tdcNeedsNs(raw json.RawMessage) bool {
	var p struct {
		Requires json.RawMessage `json:"requires"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil || len(p.Requires) == 0 {
		return false
	}
	var one string
	if json.Unmarshal(p.Requires, &one) == nil {
		return one == "nsPlugin"
	}
	var many []string
	return json.Unmarshal(p.Requires, &many) == nil && containsString(many, "nsPlugin")
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// tdcEnv builds the devices nsPlugin.py would have made, for the ones a
// case names; a case that does not require nsPlugin gets none, and runs
// alone as tdc runs it. A netdevsim case ($ETH) runs alone too: netdevsim
// device ids are global, and a device outlives the namespace it was made
// in, so the row first removes the one an earlier row left behind.
func tdcEnv(file string, texts []string, ns bool) ([]preStep, []string, bool) {
	var pre []preStep
	var mods []string
	if m := tdcModule(file); m != "" {
		mods = append(mods, m)
	}
	if !ns {
		return nil, mods, true
	}
	if tdcUses(texts, "DEV0", "DEV1") {
		pre = append(pre, preStep{Cmd: "ip link add v0p1 type veth peer name v0p0 && ip link set v0p1 up && ip link set v0p0 up"})
	}
	if tdcUses(texts, "DUMMY") {
		pre = append(pre, preStep{Cmd: "ip link add dummy1 type dummy && ip link set dummy1 up"})
	}
	if tdcUses(texts, "IFB") {
		mods = append(mods, "ifb")
		pre = append(pre, preStep{Cmd: "ip link add ifbtdc0 type ifb && ip link set ifbtdc0 up"})
	}
	serial := tdcUses(texts, "ETH")
	if serial {
		mods = append(mods, "netdevsim")
		pre = append([]preStep{{Cmd: "echo 1 > /sys/bus/netdevsim/del_device", OK: []int{1, 2}}}, pre...)
	}
	return pre, mods, serial
}

// tdcMatch carries matchPattern and matchCount over, or says why not.
func tdcMatch(c tdcCase) (*match, string) {
	switch {
	case c.MatchPattern == nil && len(c.MatchJSON) > 0:
		return nil, "matchJSON is not checked"
	case c.MatchPattern == nil:
		return nil, ""
	case len(c.Scapy) > 0:
		return nil, "the pattern counts scapy traffic, which is not replayed"
	}
	n, err := tdcInt(c.MatchCount)
	if err != nil {
		return nil, "matchCount: " + err.Error()
	}
	// tdc compiles every pattern with re.DOTALL | re.MULTILINE (tdc.py,
	// verify), so . crosses lines and ^ and $ match at each one: say so in
	// the pattern, where the corpus test's plain regexp.Compile will see it.
	pat := "(?sm)" + tdcSubst(*c.MatchPattern)
	if _, err := regexp.Compile(pat); err != nil {
		return nil, "the pattern is not RE2: " + err.Error()
	}
	return &match{Readback: "verify", Pattern: pat, Count: n}, ""
}
