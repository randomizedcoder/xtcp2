package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// tdcRow converts one case written as JSON, as if read from file.
func tdcRow(t *testing.T, file, js string) (row, string) {
	t.Helper()
	var c tdcCase
	if err := json.Unmarshal([]byte(js), &c); err != nil {
		t.Fatal(err)
	}
	return convertTdcCase(file, c)
}

func rowJSON(t *testing.T, r row) string {
	t.Helper()
	var b bytes.Buffer
	if err := writeRows(&b, nil, []row{r}); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func TestConvertTdcCase(t *testing.T) {
	tests := []struct {
		description string
		file        string
		js          string
		want        string // the row's JSON line, when a row is expected
		wantSkip    string // a substring of the skip reason, when one is
	}{
		{
			description: "positive: fq default case",
			file:        "qdiscs/fq.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"983b","name":"Create FQ with default setting","setup":[],
				"cmdUnderTest":"$TC qdisc add dev $DUMMY handle 1: root fq","expExitCode":"0",
				"verifyCmd":"$TC qdisc show dev $DUMMY","matchPattern":"qdisc fq 1: root refcnt [0-9]+ limit",
				"matchCount":"1","teardown":["$TC qdisc del dev $DUMMY handle 1: root"]}`,
			want: `{"id":"tdc/qdiscs/fq/983b","desc":"positive: Create FQ with default setting",` +
				`"cmd":"tc qdisc add dev dummy1 handle 1: root fq",` +
				`"pre":["ip link add dummy1 type dummy && ip link set dummy1 up"],` +
				`"readback":[{"name":"verify","cmd":"tc qdisc show dev dummy1"}],"modules":["sch_fq"],` +
				`"expect":{"rc":0,"match":{"readback":"verify","pattern":"(?sm)qdisc fq 1: root refcnt [0-9]+ limit","count":1}},` +
				`"source":{"tdc":"qdiscs/fq.json","id":"983b"}}`,
		},
		{
			description: "positive: a negative tdc case",
			file:        "actions/gact.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"A1B2","name":"Add action with bad index","setup":[],
				"cmdUnderTest":"$TC actions add action pass index 4294967296","expExitCode":"255",
				"verifyCmd":"","teardown":[]}`,
			want: `{"id":"tdc/actions/gact/a1b2","desc":"negative: Add action with bad index",` +
				`"cmd":"tc actions add action pass index 4294967296","modules":["act_gact"],` +
				`"expect":{"rc":255},"source":{"tdc":"actions/gact.json","id":"A1B2"}}`,
		},
		{
			description: "corner: setup with an expected rc",
			file:        "actions/gact.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"e89a","name":"Add valid pass action","setup":[["$TC actions flush action gact",0,1,255]],
				"cmdUnderTest":"$TC actions add action pass index 8","expExitCode":0,"verifyCmd":""}`,
			want: `{"id":"tdc/actions/gact/e89a","desc":"positive: Add valid pass action",` +
				`"cmd":"tc actions add action pass index 8","pre":[["tc actions flush action gact",[1,255]]],` +
				`"modules":["act_gact"],"expect":{"rc":0},"source":{"tdc":"actions/gact.json","id":"e89a"}}`,
		},
		{
			description: "boundary: empty setup and a builtin kind",
			file:        "infra/qdiscs.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0001","name":"x","setup":[],"cmdUnderTest":"$TC qdisc show","expExitCode":"0"}`,
			want: `{"id":"tdc/infra/qdiscs/0001","desc":"positive: x","cmd":"tc qdisc show",` +
				`"expect":{"rc":0},"source":{"tdc":"infra/qdiscs.json","id":"0001"}}`,
		},
		{
			description: "positive: a netdevsim case runs alone and clears the last device",
			file:        "qdiscs/mq.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"ce7d","name":"mq","setup":["echo \"1 1 4\" > /sys/bus/netdevsim/new_device"],
				"cmdUnderTest":"$TC qdisc add dev $ETH root handle 1: mq","expExitCode":"0"}`,
			want: `{"id":"tdc/qdiscs/mq/ce7d","desc":"positive: mq","cmd":"tc qdisc add dev eth0 root handle 1: mq",` +
				`"pre":[["echo 1 > /sys/bus/netdevsim/del_device",[1,2]],"echo \"1 1 4\" > /sys/bus/netdevsim/new_device"],` +
				`"modules":["netdevsim"],"serial":true,"expect":{"rc":0},"source":{"tdc":"qdiscs/mq.json","id":"ce7d"}}`,
		},
		{
			description: "positive: a veth case and an ifb case get both devices",
			file:        "infra/qdiscs.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"0002","name":"y","setup":[],"cmdUnderTest":"$TC qdisc add dev $DEV1 root handle 1: prio",
				"expExitCode":"0","verifyCmd":"$TC qdisc show dev $IFB"}`,
			want: `{"id":"tdc/infra/qdiscs/0002","desc":"positive: y","cmd":"tc qdisc add dev v0p1 root handle 1: prio",` +
				`"pre":["ip link add v0p1 type veth peer name v0p0 && ip link set v0p1 up && ip link set v0p0 up",` +
				`"ip link add ifbtdc0 type ifb && ip link set ifbtdc0 up"],` +
				`"readback":[{"name":"verify","cmd":"tc qdisc show dev ifbtdc0"}],"modules":["ifb"],` +
				`"expect":{"rc":0},"source":{"tdc":"infra/qdiscs.json","id":"0002"}}`,
		},
		{
			description: "corner: scapy traffic leaves the pattern unchecked, with the reason",
			file:        "qdiscs/fq.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"0003","name":"z","setup":[],"cmdUnderTest":"$TC qdisc add dev $DUMMY root fq",
				"expExitCode":"0","verifyCmd":"$TC -s qdisc show dev $DUMMY","matchPattern":"Sent 98","matchCount":"1",
				"scapy":[{"iface":"$DEV0","count":1}]}`,
			// scapy's own $DEV0 makes no veth: no traffic is sent.
			want: `{"id":"tdc/qdiscs/fq/0003","desc":"positive: z","cmd":"tc qdisc add dev dummy1 root fq",` +
				`"pre":["ip link add dummy1 type dummy && ip link set dummy1 up"],` +
				`"readback":[{"name":"verify","cmd":"tc -s qdisc show dev dummy1"}],"modules":["sch_fq"],` +
				`"expect":{"rc":0},"source":{"tdc":"qdiscs/fq.json","id":"0003",` +
				`"unchecked":"the pattern counts scapy traffic, which is not replayed"}}`,
		},
		{
			description: "corner: a pattern RE2 cannot compile is left unchecked",
			file:        "qdiscs/fq.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"0004","name":"w","setup":[],"cmdUnderTest":"$TC qdisc show","expExitCode":"0",
				"verifyCmd":"$TC qdisc show","matchPattern":"(?=fq)","matchCount":"1"}`,
			want: `{"id":"tdc/qdiscs/fq/0004","desc":"positive: w","cmd":"tc qdisc show",` +
				`"readback":[{"name":"verify","cmd":"tc qdisc show"}],"modules":["sch_fq"],"expect":{"rc":0},` +
				`"source":{"tdc":"qdiscs/fq.json","id":"0004","unchecked":"the pattern is not RE2: ` +
				"error parsing regexp: invalid or unsupported Perl syntax: `(?=`\"}}",
		},
		{
			description: "negative: needs an eBPF object",
			file:        "actions/bpf.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0005","name":"v","setup":[],"cmdUnderTest":"$TC actions add action bpf object-file $EBPFDIR/a.o","expExitCode":"0"}`,
			wantSkip:    "needs a compiled eBPF object",
		},
		{
			description: "negative: runs a tdc helper script",
			file:        "filters/flower.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0006","name":"u","setup":["./tdc_gso.py 1 2"],"cmdUnderTest":"$TC filter show","expExitCode":"0"}`,
			wantSkip:    "runs a tdc helper script",
		},
		{
			description: "negative: a variable tdc does not define",
			file:        "filters/flower.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0007","name":"t","setup":[],"cmdUnderTest":"$TC filter show dev $NOSUCH","expExitCode":"0"}`,
			wantSkip:    "uses $NOSUCH",
		},
		{
			description: "negative: an expExitCode that is not a number",
			file:        "filters/flower.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0008","name":"s","setup":[],"cmdUnderTest":"$TC filter show","expExitCode":"x"}`,
			wantSkip:    "expExitCode",
		},
		{
			description: "negative: a setup step of the wrong shape",
			file:        "filters/flower.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0009","name":"r","setup":[{"a":1}],"cmdUnderTest":"$TC filter show","expExitCode":"0"}`,
			wantSkip:    "neither a command nor",
		},
		{
			description: "negative: a desc tdc gave no command",
			file:        "filters/flower.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"000a","name":"q","setup":[],"cmdUnderTest":"  ","expExitCode":"0"}`,
			wantSkip:    "no command under test",
		},
		{
			description: "corner: a case without nsPlugin gets no devices and runs alone",
			file:        "qdiscs/teql.json",
			js: `{"id":"84a0","name":"teql","setup":["$IP link add dev $DUMMY type dummy"],
				"cmdUnderTest":"$TC qdisc add dev $DUMMY handle 1: root teql0","expExitCode":"0"}`,
			want: `{"id":"tdc/qdiscs/teql/84a0","desc":"positive: teql","cmd":"tc qdisc add dev dummy1 handle 1: root teql0",` +
				`"pre":["ip link add dev dummy1 type dummy"],"modules":["sch_teql"],"serial":true,` +
				`"expect":{"rc":0},"source":{"tdc":"qdiscs/teql.json","id":"84a0"}}`,
		},
		{
			description: "positive: nsPlugin named in a list of plugins",
			file:        "qdiscs/fq.json",
			js: `{"plugins":{"requires":["nsPlugin","scapyPlugin"]},"id":"0011","name":"p","setup":[],
				"cmdUnderTest":"$TC qdisc add dev $DUMMY root fq","expExitCode":"0"}`,
			want: `{"id":"tdc/qdiscs/fq/0011","desc":"positive: p","cmd":"tc qdisc add dev dummy1 root fq",` +
				`"pre":["ip link add dummy1 type dummy && ip link set dummy1 up"],"modules":["sch_fq"],` +
				`"expect":{"rc":0},"source":{"tdc":"qdiscs/fq.json","id":"0011"}}`,
		},
		{
			description: "corner: plugins that do not include nsPlugin",
			file:        "infra/qdiscs.json",
			js: `{"plugins":{"requires":["scapyPlugin"]},"id":"0012","name":"o","setup":[],
				"cmdUnderTest":"$TC qdisc show dev $DUMMY","expExitCode":"0"}`,
			want: `{"id":"tdc/infra/qdiscs/0012","desc":"positive: o","cmd":"tc qdisc show dev dummy1",` +
				`"serial":true,"expect":{"rc":0},"source":{"tdc":"infra/qdiscs.json","id":"0012"}}`,
		},
		{
			description: "positive: /bin/true becomes the guest's true",
			file:        "qdiscs/qfq.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"0013","name":"n","setup":["$IP link set dev $DUMMY mtu 256 || /bin/true"],
				"cmdUnderTest":"$TC qdisc show dev $DUMMY","expExitCode":"0","verifyCmd":"/bin/true"}`,
			want: `{"id":"tdc/qdiscs/qfq/0013","desc":"positive: n","cmd":"tc qdisc show dev dummy1",` +
				`"pre":["ip link add dummy1 type dummy && ip link set dummy1 up","ip link set dev dummy1 mtu 256 || true"],` +
				`"readback":[{"name":"verify","cmd":"true"}],"modules":["sch_qfq"],` +
				`"expect":{"rc":0},"source":{"tdc":"qdiscs/qfq.json","id":"0013"}}`,
		},
		{
			description: "negative: runs one of tdc's scripts/",
			file:        "qdiscs/sfq.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0014","name":"m","setup":[],"cmdUnderTest":"./scripts/sfq_rejects_limit_1.py $DUMMY","expExitCode":"0"}`,
			wantSkip:    "runs a tdc helper script",
		},
		{
			description: "negative: a command under test of /bin/true captures nothing",
			file:        "qdiscs/taprio.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0017","name":"j","setup":["$TC qdisc add dev $ETH root taprio || /bin/true"],"cmdUnderTest":"/bin/true","expExitCode":"0"}`,
			wantSkip:    "tests its setup",
		},
		{
			description: "corner: a case testing a newer kernel keeps its row, its pattern unchecked",
			file:        "actions/batch-delete.json",
			js: `{"plugins":{"requires":"nsPlugin"},"id":"8135","name":"i","setup":[],
				"cmdUnderTest":"$TC actions del action gact index 1 action gact index 1","expExitCode":"255",
				"verifyCmd":"$TC actions ls action gact","matchPattern":"total acts 1","matchCount":"1"}`,
			want: `{"id":"tdc/actions/batch-delete/8135","desc":"negative: i","cmd":"tc actions del action gact index 1 action gact index 1",` +
				`"readback":[{"name":"verify","cmd":"tc actions ls action gact"}],"modules":["act_gact"],"expect":{"rc":255},` +
				`"source":{"tdc":"actions/batch-delete.json","id":"8135","unchecked":"tests f67b3ed1308d (net/sched: act_api: ` +
				`reject duplicate actions in a batch, 7.4 merge window); 7.3.0 deletes the first action before refusing the duplicate"}}`,
		},
		{
			description: "negative: a burst too big to capture whole",
			file:        "filters/u32.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"1234","name":"h","setup":[],"cmdUnderTest":"$TC -b x","expExitCode":"0"}`,
			wantSkip:    "nlmon dropped",
		},
		{
			description: "negative: gact random, which the workbench kernel lacks",
			file:        "actions/gact.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0015","name":"l","setup":[],"cmdUnderTest":"$TC actions add action pass random determ drop 2 index 90","expExitCode":"0"}`,
			wantSkip:    "CONFIG_GACT_PROB",
		},
		{
			description: "boundary: random in another file, or inside a word, is not gact's random",
			file:        "actions/skbedit.json",
			js:          `{"plugins":{"requires":"nsPlugin"},"id":"0016","name":"k","setup":[],"cmdUnderTest":"$TC actions add action skbedit mark 1 cookie randomx","expExitCode":"0"}`,
			want: `{"id":"tdc/actions/skbedit/0016","desc":"positive: k","cmd":"tc actions add action skbedit mark 1 cookie randomx",` +
				`"modules":["act_skbedit"],"expect":{"rc":0},"source":{"tdc":"actions/skbedit.json","id":"0016"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, reason := tdcRow(t, tc.file, tc.js)
			if tc.wantSkip != "" {
				if !strings.Contains(reason, tc.wantSkip) {
					t.Fatalf("skip reason %q, want it to contain %q (row %+v)", reason, tc.wantSkip, r)
				}
				return
			}
			if reason != "" {
				t.Fatalf("skipped: %s", reason)
			}
			if got := rowJSON(t, r); got != tc.want {
				t.Errorf("row\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestConvertTdcTree(t *testing.T) {
	ok := `{"id":"aaaa","name":"a","setup":[],"cmdUnderTest":"$TC qdisc show","expExitCode":"0"}`
	bpf := `{"id":"bbbb","name":"b","setup":[],"cmdUnderTest":"$TC x $EBPFDIR","expExitCode":"0"}`
	tests := []struct {
		description string
		files       map[string]string
		wantRows    []string
		wantSkips   []string
		wantErr     string
	}{
		{
			description: "positive: files in path order, a skip recorded",
			files: map[string]string{
				"qdiscs/z.json": "[" + ok + "]",
				"actions/a.json": "[" + bpf + "," +
					strings.Replace(ok, "aaaa", "cccc", 1) + "]",
			},
			wantRows:  []string{"tdc/actions/a/cccc", "tdc/qdiscs/z/aaaa"},
			wantSkips: []string{"bbbb: needs a compiled eBPF object ($EBPFDIR)"},
		},
		{
			description: "corner: a repeated tdc id in one file is skipped, not a duplicate row",
			files:       map[string]string{"qdiscs/z.json": "[" + ok + "," + ok + "]"},
			wantRows:    []string{"tdc/qdiscs/z/aaaa"},
			wantSkips:   []string{"aaaa: repeats the id of an earlier case in qdiscs/z.json"},
		},
		{
			description: "corner: every case skipped is an error, not an empty file",
			files:       map[string]string{"actions/a.json": "[" + bpf + "]"},
			wantErr:     "no rows",
		},
		{
			description: "negative: a file that is not JSON",
			files:       map[string]string{"actions/a.json": "{"},
			wantErr:     "actions/a.json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m := fstest.MapFS{}
			for k, v := range tc.files {
				m[k] = &fstest.MapFile{Data: []byte(v)}
			}
			rows, skips, err := convertTdcTree(m)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var ids, sk []string
			for _, r := range rows {
				ids = append(ids, r.ID)
			}
			for _, s := range skips {
				sk = append(sk, s.ID+": "+s.Reason)
			}
			if strings.Join(ids, " ") != strings.Join(tc.wantRows, " ") {
				t.Errorf("rows %v, want %v", ids, tc.wantRows)
			}
			if strings.Join(sk, "|") != strings.Join(tc.wantSkips, "|") {
				t.Errorf("skips %v, want %v", sk, tc.wantSkips)
			}
		})
	}
}

func TestCheckRows(t *testing.T) {
	rc := 0
	good := row{ID: "a/b", Desc: "positive: x", Cmd: "ip link", Expect: expect{RC: &rc}}
	with := func(f func(*row)) row {
		r := good
		f(&r)
		return r
	}
	tests := []struct {
		description string
		rows        []row
		wantErr     string // "" for none
	}{
		{"positive: a valid row", []row{good}, ""},
		{"corner: duplicate row ids", []row{good, good}, "id a/b repeats row 1"},
		{"boundary: no rows", nil, "no rows"},
		{"negative: an id escaping the out dir", []row{with(func(r *row) { r.ID = "../x" })}, "bad id"},
		{"negative: an upper-case id", []row{with(func(r *row) { r.ID = "A/b" })}, "bad id"},
		{"negative: a desc without its prefix", []row{with(func(r *row) { r.Desc = "x" })}, "lacks a positive:"},
		{"negative: a blank cmd", []row{with(func(r *row) { r.Cmd = " " })}, "no cmd"},
		{"negative: a readback named ip", []row{with(func(r *row) { r.Readback = []readback{{Name: "ip", Cmd: "x"}} })}, "readback name"},
		{"negative: a repeated readback", []row{with(func(r *row) {
			r.Readback = []readback{{Name: "v", Cmd: "x"}, {Name: "v", Cmd: "y"}}
		})}, "readback name"},
		{"negative: a match on a readback the row lacks", []row{with(func(r *row) {
			r.Expect.Match = &match{Readback: "verify", Pattern: "x"}
		})}, "which the row lacks"},
		{"negative: a bad module name", []row{with(func(r *row) { r.Modules = []string{"sch fq"} })}, "bad module"},
		{"positive: a generic netlink readback", []row{with(func(r *row) { r.Readback = []readback{{Name: "v", Cmd: "x", Proto: "generic"}} })}, ""},
		{"negative: a readback on a protocol the workbench lacks", []row{with(func(r *row) {
			r.Readback = []readback{{Name: "v", Cmd: "x", Proto: "nosuch"}}
		})}, "readback v: proto"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := checkRows(tc.rows)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestTdcModule(t *testing.T) {
	tests := []struct {
		description string
		file        string
		expected    string
	}{
		{"positive: a qdisc file", "qdiscs/htb.json", "sch_htb"},
		{"positive: an action file", "actions/mirred.json", "act_mirred"},
		{"positive: a filter file", "filters/u32.json", "cls_u32"},
		{"positive: an aliased file", "qdiscs/cake_mq.json", "sch_cake"},
		{"boundary: a builtin qdisc", "qdiscs/mq.json", ""},
		{"corner: an infra file", "infra/actions.json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tdcModule(tc.file); got != tc.expected {
				t.Errorf("tdcModule(%q) = %q, want %q", tc.file, got, tc.expected)
			}
		})
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		expected    int
		wantErr     string
	}{
		{"negative: no subcommand", nil, 2, "usage:"},
		{"negative: an unknown subcommand", []string{"nosuch"}, 2, "unknown subcommand"},
		{"negative: tdc without its flags", []string{"tdc"}, 2, "are required"},
		{"negative: tdc with a missing tree", []string{"tdc", "-src", "/nonexistent", "-out", "x", "-skipped", "y"}, 1, "no rows"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errb bytes.Buffer
			if got := run(tc.args, &out, &errb); got != tc.expected {
				t.Errorf("rc %d, want %d", got, tc.expected)
			}
			if !strings.Contains(errb.String(), tc.wantErr) {
				t.Errorf("stderr %q, want it to contain %q", errb.String(), tc.wantErr)
			}
		})
	}
}
