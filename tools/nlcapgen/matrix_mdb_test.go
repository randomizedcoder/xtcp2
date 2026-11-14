package main

import (
	"strconv"
	"strings"
	"testing"
)

// mdbPick is base with kv's pairs set over it.
func mdbPick(base map[string]string, kv ...string) map[string]string {
	p := make(map[string]string, len(base))
	for k, v := range base {
		p[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		p[kv[i]] = kv[i+1]
	}
	return p
}

func TestMdbBrJudge(t *testing.T) {
	base := map[string]string{"verb": "add", "dev": "br0", "bport": "d0", "grp": "v4", "state": "absent", "vid": "absent",
		"src": "absent", "mode": "absent", "srclist": "absent", "proto": "absent"}
	with := func(kv ...string) map[string]string { return mdbPick(base, kv...) }
	cases := []struct {
		description string
		pick        map[string]string
		expected    string
	}{
		{"positive: a temporary IPv4 group on the port", with(), "ok"},
		{"positive: an (S, G) entry with a protocol", with("src", "v4", "proto", "bgp", "state", "permanent"), "ok"},
		{"positive: (*, G) EXCLUDE with a source list", with("mode", "exclude", "srclist", "two"), "ok"},
		{"positive: a host join", with("bport", "br0"), "ok"},
		{"negative: an IPv6 source on an IPv4 group", with("src", "v6"), "255 EINVAL kernel"},
		{"negative: a source on an L2 group", with("grp", "l2", "src", "v4", "state", "permanent"), "255 EINVAL kernel"},
		{"negative: a filter mode on an (S, G) entry", with("src", "v4", "mode", "exclude"), "255 EINVAL kernel"},
		{"negative: a filter mode on a host group", with("bport", "br0", "mode", "exclude"), "255 EINVAL kernel"},
		{"negative: a source list without a filter mode", with("srclist", "one"), "255 EINVAL kernel"},
		{"negative: an IPv4 source list on an IPv6 group", with("grp", "v6", "mode", "exclude", "srclist", "one"), "255 EINVAL kernel"},
		{"negative: INCLUDE with an empty source list", with("mode", "include"), "255 EINVAL kernel"},
		{"negative: a protocol on a host group", with("bport", "br0", "proto", "static"), "255 EINVAL kernel"},
		{"negative: a permanent host group", with("bport", "br0", "state", "permanent"), "255 EINVAL kernel"},
		{"negative: a host join with a source", with("bport", "br0", "src", "v4"), "255 EINVAL kernel"},
		{"negative: a temporary L2 entry", with("grp", "l2"), "255 EINVAL kernel"},
		{"corner: a permanent L2 host join", with("grp", "l2", "bport", "br0", "state", "permanent"), "ok"},
		{"boundary: VLAN 4094, which d0 lacks, is taken", with("vid", "4094"), "ok"},
		{"positive: a delete whose add is accepted", with("verb", "del", "vid", "10"), "ok"},
		{"negative: a delete whose add is refused finds nothing", with("verb", "del", "grp", "l2"), "255 EINVAL kernel"},
		{"corner: replace is judged as an add", with("verb", "replace", "mode", "include"), "255 EINVAL kernel"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbVerdict(mdbBrJudge(tc.pick)); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestMdbVxJudge(t *testing.T) {
	base := map[string]string{"verb": "add", "dev": "vx0", "bport": "vx0", "grp": "v4", "state": "permanent", "vid": "absent",
		"dst": "v4", "dport": "absent", "vni": "absent", "srcvni": "absent", "via": "absent", "src": "absent",
		"mode": "absent", "srclist": "absent", "proto": "absent"}
	with := func(kv ...string) map[string]string { return mdbPick(base, kv...) }
	cases := []struct {
		description string
		pick        map[string]string
		expected    string
	}{
		{"positive: a permanent IPv4 group to an IPv4 remote", with(), "ok"},
		{"positive: the all-zeros group, the catch-all", with("grp", "zero"), "ok"},
		{"positive: an (S, G) entry with every remote attribute", with("src", "v4", "dport", "4790", "vni", "200", "srcvni", "other", "via", "d1"), "ok"},
		{"positive: (*, G) INCLUDE with a source list", with("mode", "include", "srclist", "one"), "ok"},
		{"negative: an entry that is not permanent", with("state", "absent"), "255 EINVAL kernel"},
		{"negative: no remote destination", with("dst", "absent"), "255 EINVAL kernel"},
		{"negative: an IPv4 source on an IPv6 group", with("grp", "v6", "src", "v4"), "255 EINVAL kernel"},
		{"negative: a source on the all-zeros group", with("grp", "zero", "src", "v4"), "255 EINVAL kernel"},
		{"negative: a filter mode on an (S, G) entry", with("src", "v4", "mode", "exclude"), "255 EINVAL kernel"},
		{"negative: a source list without a filter mode", with("srclist", "one"), "255 EINVAL kernel"},
		{"negative: an IPv4 source list on the IPv6 all-zeros group", with("grp", "v6zero", "mode", "exclude", "srclist", "one"), "255 EINVAL kernel"},
		{"negative: INCLUDE with an empty source list", with("mode", "include"), "255 EINVAL kernel"},
		{"corner: not permanent is reported before a missing remote", with("state", "absent", "dst", "absent"), "255 EINVAL kernel"},
		{"boundary: port 0 and VNI 0 are taken", with("dport", "0", "vni", "0"), "ok"},
		{"positive: a delete whose add is accepted", with("verb", "del"), "ok"},
		{"negative: a delete after a temporary add finds nothing", with("verb", "del", "state", "absent"), "255 ENOENT kernel"},
		{"negative: a delete's attributes are still checked", with("verb", "del", "dst", "absent"), "255 EINVAL kernel"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbVerdict(mdbVxJudge(tc.pick)); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestBuildMdb(t *testing.T) {
	br, vx := mdbBrSpace(), mdbVxSpace()
	cases := []struct {
		description string
		sp          *space
		names       map[string]string
		cmd         string
		pre         int    // pre steps
		readback    string // the dump
	}{
		{"positive: the first value of every axis", br, nil, "bridge mdb add dev br0 port d0 grp 239.1.1.1", 1, "bridge mdb show dev br0"},
		{"positive: a delete adds its entry first", br, map[string]string{"verb": "del", "vid": "10"},
			"bridge mdb del dev br0 port d0 grp 239.1.1.1 vid 10", 2, "bridge mdb show dev br0"},
		{"negative: a delete whose add is refused adds nothing", br, map[string]string{"verb": "del", "grp": "l2"},
			"bridge mdb del dev br0 port d0 grp 01:00:5e:01:01:01", 1, "bridge mdb show dev br0"},
		{"positive: a VXLAN row dumps vx0", vx, map[string]string{"dport": "4790"},
			"bridge mdb add dev vx0 port vx0 grp 239.1.1.1 permanent dst 192.0.2.1 dst_port 4790", 1, "bridge mdb show dev vx0"},
		{"corner: a temporary VXLAN delete adds nothing first", vx, map[string]string{"verb": "del", "state": "absent"},
			"bridge mdb del dev vx0 port vx0 grp 239.1.1.1 dst 192.0.2.1", 1, "bridge mdb show dev vx0"},
		{"boundary: a port of 0 is sent and so is a fact", vx, map[string]string{"dport": "0"},
			"bridge mdb add dev vx0 port vx0 grp 239.1.1.1 permanent dst 192.0.2.1 dst_port 0", 1, "bridge mdb show dev vx0"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := tc.sp.Build(tc.sp.assign(tc.names))
			if r.Cmd != tc.cmd || len(r.Pre) != tc.pre || r.Readback[0].Cmd != tc.readback {
				t.Errorf("cmd %q pre %d readback %q, want %q %d %q", r.Cmd, len(r.Pre), r.Readback[0].Cmd, tc.cmd, tc.pre, tc.readback)
			}
			if len(r.Pre) == 2 && r.Pre[1].Cmd != strings.Replace(r.Cmd, " del ", " add ", 1) {
				t.Errorf("pre add %q", r.Pre[1].Cmd)
			}
			if tc.names["dport"] == "0" && r.Axes["dport"] != "0" {
				t.Error("port 0 has no fact, though mdb_modify sends it")
			}
		})
	}
	// A dummy, or a device bridge cannot find, dumps br0 instead.
	for _, dev := range []value{{Name: "d0", Arg: "dev d0", Facts: map[string]string{"dev": "d0"}}, {Name: "nosuch", Arg: "dev nosuch0"}} {
		vals := br.assign(nil)
		vals[1] = dev
		if r := br.Build(vals); r.Readback[0].Cmd != "bridge mdb show dev br0" {
			t.Errorf("corner: dev %s's readback %q", dev.Name, r.Readback[0].Cmd)
		}
	}
}

func TestMdbBrAllVlans(t *testing.T) {
	r := mdbBrAllVlans(mdbBrSpace())
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: its id", r.ID, "mdbbr/del-all-vlans"},
		{"corner: the delete names no VLAN", r.Cmd, "bridge mdb del dev br0 port d0 grp 239.1.1.1"},
		{"positive: the add is in VLAN 1 only", r.Pre[len(r.Pre)-1].Cmd, "bridge mdb add dev br0 port d0 grp 239.1.1.1 vid 1"},
		{"positive: the setup, then the add", strconv.Itoa(len(r.Pre)) + " " + r.Pre[0].Cmd[:20], "2 " + mdbBrSetup[:20]},
		{"negative: VLAN 10's EINVAL, as bridge exits", r.Expect.Errno + " " + strconv.Itoa(*r.Expect.RC), "EINVAL 255"},
		{"boundary: the desc is a corner", r.Desc[:7], "corner:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestDeriveBridgeDupGone(t *testing.T) {
	mk := func(id, verb string, rc int) row {
		return row{ID: id, Desc: "positive: x", Cmd: "bridge mdb " + verb + " " + id, Pre: []preStep{{Cmd: "setup"}, {Cmd: "add"}},
			Expect: expect{RC: intp(rc)}, Axes: map[string]string{"verb": verb}}
	}
	out := deriveBridgeDupGone("mdbvx", []row{mk("a", "add", 0), mk("b", "add", 255), mk("c", "del", 0)}, "ENOENT")
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: one dup and one gone", strconv.Itoa(len(out)), "2"},
		{"negative: a refused add is not duplicated", out[0].Cmd, "bridge mdb add a"},
		{"negative: the dup is EEXIST, as bridge exits", out[0].Expect.Errno + " " + strconv.Itoa(*out[0].Expect.RC), "EEXIST 255"},
		{"negative: the gone takes the given errno", out[1].Expect.Errno + " " + strconv.Itoa(*out[1].Expect.RC), "ENOENT 255"},
		{"corner: the gone drops its add", strconv.Itoa(len(out[1].Pre)), "1"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
	if got := deriveBridgeDupGone("mdbbr", nil, "EINVAL"); len(got) != 0 {
		t.Errorf("boundary: no positives give %d rows", len(got))
	}
}

func TestMatrixMdbRows(t *testing.T) {
	rows, err := matrixRows(families["mdb"](), matrixOpts{Seed: 1, Random: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: every row is a bridge mdb command naming its space's device", func(r row) bool {
			if strings.HasPrefix(r.ID, "mdbvx/") {
				return strings.HasPrefix(r.Cmd, "bridge mdb ") && r.Axes["dev"] == "vx0"
			}
			return strings.HasPrefix(r.ID, "mdbbr/") && strings.HasPrefix(r.Cmd, "bridge mdb ")
		}},
		{"positive: every refusal exits 255, as bridge does", func(r row) bool {
			return r.Expect.RC != nil && (*r.Expect.RC == 0 || *r.Expect.RC == 255)
		}},
		{"negative: no accepted VXLAN add lacks permanent or dst", func(r row) bool {
			return !strings.HasPrefix(r.ID, "mdbvx/") || *r.Expect.RC != 0 || r.Axes["verb"] == "del" ||
				(r.Axes["state"] == "1" && r.Axes["dst"] != "")
		}},
		{"negative: no accepted row is a host join with a protocol", func(r row) bool {
			return *r.Expect.RC != 0 || r.Axes["bport"] != "br0" || r.Axes["proto"] == ""
		}},
		{"negative: a gone row is EINVAL on the bridge, ENOENT on the VXLAN", func(r row) bool {
			switch {
			case strings.HasPrefix(r.ID, "mdbbr/gone-"):
				return r.Expect.Errno == "EINVAL"
			case strings.HasPrefix(r.ID, "mdbvx/gone-"):
				return r.Expect.Errno == "ENOENT"
			}
			return true
		}},
		{"positive: the readback dumps br0 or vx0", func(r row) bool {
			return len(r.Readback) == 1 &&
				(r.Readback[0].Cmd == "bridge mdb show dev br0" || r.Readback[0].Cmd == "bridge mdb show dev vx0")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for _, r := range rows {
				if !tc.check(r) {
					t.Errorf("%s: %s", r.ID, r.Cmd)
				}
			}
		})
	}
}
