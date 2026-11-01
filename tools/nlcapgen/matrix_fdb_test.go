package main

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// fdbVerdict renders a judge's answer as "rc errno refused-by", or "ok".
func fdbVerdict(exp *expect, why string) string {
	if exp == nil {
		return "ok"
	}
	if why == "" {
		return "refused with no reason"
	}
	return strings.TrimSpace(fmt.Sprintf("%d %s %s", *exp.RC, exp.Errno, exp.RefusedBy))
}

func TestFdbBrJudge(t *testing.T) {
	base := map[string]string{"verb": "add", "mac": "uni", "dev": "port", "target": "absent", "state": "absent",
		"flags": "absent", "vlan": "absent", "ext": "absent"}
	with := func(kv ...string) map[string]string {
		p := map[string]string{}
		for k, v := range base {
			p[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			p[kv[i]] = kv[i+1]
		}
		return p
	}
	cases := []struct {
		description string
		pick        map[string]string
		expected    string
	}{
		{"positive: self on the dummy port, permanent", with(), "ok"},
		{"positive: master on the port, in VLAN 10, sticky and static", with("target", "master", "vlan", "10", "flags", "sticky", "state", "static"), "ok"},
		{"positive: master on the port takes use", with("target", "master", "flags", "use"), "ok"},
		{"negative: self on the dummy in a VLAN", with("vlan", "1"), "255 EINVAL kernel"},
		{"negative: self on the dummy, static", with("state", "temp"), "255 EINVAL kernel"},
		{"negative: master on the bridge itself", with("dev", "bridge", "target", "master"), "255 ENOTSUP kernel"},
		{"negative: self on the bridge with use", with("dev", "bridge", "flags", "use"), "255 EINVAL kernel"},
		{"negative: self on the bridge, dynamic", with("dev", "bridge", "state", "dynamic"), "255 EINVAL kernel"},
		{"positive: self on the bridge, extern_learn and permanent", with("dev", "bridge", "flags", "extern_learn"), "ok"},
		{"negative: self on the bridge, extern_learn and static", with("dev", "bridge", "flags", "extern_learn", "state", "static"), "255 EINVAL kernel"},
		{"negative: sticky and permanent on the master path", with("target", "master", "flags", "sticky"), "255 EINVAL kernel"},
		{"negative: inactive without notify on the master path", with("target", "master", "ext", "inactive"), "255 EINVAL kernel"},
		{"corner: inactive is ignored by the dummy's default FDB", with("ext", "inactive"), "ok"},
		{"negative: the zero MAC on the master path", with("target", "both", "mac", "zero"), "255 EINVAL kernel"},
		{"corner: the zero MAC on the dummy is a unicast address", with("mac", "zero"), "ok"},
		{"boundary: VLAN 4094, which d0 lacks", with("target", "master", "vlan", "4094"), "255 EINVAL kernel"},
		{"corner: VLAN 65535 sends nothing", with("vlan", "65535"), "ok"},
		{"negative: VLAN 0 before any path", with("dev", "bridge", "target", "master", "vlan", "0"), "255 EINVAL kernel"},
		{"negative: both on the port fails on the dummy's VLAN", with("target", "both", "vlan", "10"), "255 EINVAL kernel"},
		{"positive: a delete whose add is accepted", with("verb", "del", "target", "master"), "ok"},
		{"negative: a delete on the master path whose add is refused", with("verb", "del", "target", "master", "vlan", "4094"), "255 ENOENT kernel"},
		{"negative: a delete of a static self entry on the dummy", with("verb", "del", "state", "static"), "255 EINVAL kernel"},
		{"negative: a delete on the bridge whose add is refused", with("verb", "del", "dev", "bridge", "state", "static"), "255 ENOENT kernel"},
		{"negative: a delete with master on the bridge", with("verb", "del", "dev", "bridge", "target", "master"), "255 ENOTSUP kernel"},
		{"boundary: a delete in VLAN 4095", with("verb", "del", "vlan", "4095"), "255 EINVAL kernel"},
		{"corner: a delete of self in a VLAN finds no address", with("verb", "del", "vlan", "10"), "255 ENOENT kernel"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbVerdict(fdbBrJudge(tc.pick)); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestFdbVxJudge(t *testing.T) {
	base := map[string]string{"verb": "add", "mac": "uni", "remote": "dst4", "port": "absent", "vni": "absent",
		"srcvni": "absent", "via": "absent", "state": "absent"}
	with := func(kv ...string) map[string]string {
		p := map[string]string{}
		for k, v := range base {
			p[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			p[kv[i]] = kv[i+1]
		}
		return p
	}
	cases := []struct {
		description string
		pick        map[string]string
		expected    string
	}{
		{"positive: a unicast dst", with(), "ok"},
		{"positive: an fdb group, static", with("remote", "nhid", "state", "static"), "ok"},
		{"negative: neither dst nor nhid", with("remote", "absent"), "255 EINVAL kernel"},
		{"negative: nhid with via", with("remote", "nhid", "via", "d1"), "255 EINVAL kernel"},
		{"negative: a dynamic nhid", with("remote", "nhid", "state", "dynamic"), "255 EINVAL kernel"},
		{"corner: a dynamic dst is fine", with("state", "dynamic"), "ok"},
		{"negative: an IPv6 dst", with("remote", "dst6"), "255 EAFNOSUPPORT kernel"},
		{"negative: replace making a zero entry", with("verb", "replace", "mac", "zero"), "255 ENOTSUP kernel"},
		{"corner: append making a multicast entry", with("verb", "append", "mac", "mcast"), "ok"},
		{"negative: an fdb nexthop that is no group", with("remote", "nhsingle"), "255 EINVAL kernel"},
		{"negative: a nexthop that is not fdb", with("remote", "nhnotfdb"), "255 EINVAL kernel"},
		{"boundary: a nexthop that does not exist", with("remote", "nhgone"), "255 EINVAL kernel"},
		{"positive: a delete whose add is accepted", with("verb", "del", "srcvni", "own"), "ok"},
		{"negative: a delete under a foreign src_vni", with("verb", "del", "srcvni", "other"), "255 ENOENT kernel"},
		{"negative: a delete whose add is refused", with("verb", "del", "remote", "dst6"), "255 ENOENT kernel"},
		{"negative: a delete with nhid and via", with("verb", "del", "remote", "nhgone", "via", "d1"), "255 EINVAL kernel"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if got := fdbVerdict(fdbVxJudge(tc.pick)); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestBuildFdb(t *testing.T) {
	br, vx := fdbBrSpace(), fdbVxSpace()
	cases := []struct {
		description string
		sp          *space
		names       map[string]string
		cmd         string
		pre         int    // pre steps
		readback    string // the dump
	}{
		{"positive: the first value of every axis", br, nil, "bridge fdb add 02:00:00:00:00:01 dev d0", 1, "bridge fdb show dev d0"},
		{"positive: a delete adds its entry first", br, map[string]string{"verb": "del", "target": "master"},
			"bridge fdb del 02:00:00:00:00:01 dev d0 master", 2, "bridge fdb show dev d0"},
		{"negative: a delete whose add is refused adds nothing", br, map[string]string{"verb": "del", "dev": "bridge", "target": "master"},
			"bridge fdb del 02:00:00:00:00:01 dev br0 master", 1, "bridge fdb show dev br0"},
		{"positive: a VXLAN row names vx0 last", vx, map[string]string{"remote": "nhid", "state": "static"},
			"bridge fdb add 02:00:00:00:00:01 nhid 10 static dev vx0", 1, "bridge fdb show dev vx0"},
		{"corner: a foreign src_vni's delete still adds first", vx, map[string]string{"verb": "del", "srcvni": "other"},
			"bridge fdb del 02:00:00:00:00:01 dst 192.0.2.1 src_vni 300 dev vx0", 2, "bridge fdb show dev vx0"},
		{"boundary: a port of 0 is spelled but sends nothing", vx, map[string]string{"port": "0"},
			"bridge fdb add 02:00:00:00:00:01 dst 192.0.2.1 port 0 dev vx0", 1, "bridge fdb show dev vx0"},
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
			if _, ok := r.Axes["port"]; ok && tc.names["port"] == "0" {
				t.Error("port 0 has a port fact")
			}
		})
	}
	// A device bridge cannot find dumps d0's FDB instead.
	vals := br.assign(nil)
	vals[2] = value{Name: "nosuch", Arg: "dev nosuch0"}
	if r := br.Build(vals); r.Readback[0].Cmd != "bridge fdb show dev d0" {
		t.Errorf("corner: an unknown device's readback %q", r.Readback[0].Cmd)
	}
}

func TestFdbVxTwice(t *testing.T) {
	out := fdbVxTwice(fdbVxSpace())
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: two rows", strconv.Itoa(len(out)), "2"},
		{"corner: the own src_vni's second replace updates", out[0].ID + " " + fdbVerdict(nilIfOK(out[0].Expect)), "fdbvx/twice-own ok"},
		{"negative: the foreign src_vni's second replace is EEXIST", out[1].ID + " " + out[1].Expect.Errno, "fdbvx/twice-other EEXIST"},
		{"positive: the replace runs first", out[1].Pre[len(out[1].Pre)-1].Cmd, out[1].Cmd},
		{"positive: under src_vni 300", out[1].Axes["src_vni"], "300"},
		{"positive: the negative says so", out[1].Desc[:9], "negative:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// nilIfOK is nil for an accepted row's expect.
func nilIfOK(e expect) (*expect, string) {
	if e.RC != nil && *e.RC == 0 {
		return nil, ""
	}
	return &e, "refused"
}

func TestDeriveFdbDupGone(t *testing.T) {
	mk := func(id, verb, ntf string) row {
		ax := map[string]string{"verb": verb}
		if ntf != "" {
			ax["ntf"] = ntf
		}
		return row{ID: id, Desc: "positive: x", Cmd: "bridge fdb " + verb + " " + id, Pre: []preStep{{Cmd: "setup"}, {Cmd: "add"}},
			Expect: expect{RC: intp(0)}, Axes: ax}
	}
	out := deriveFdbDupGone("fdbbr", []row{mk("a", "add", "1"), mk("b", "add", "16"), mk("c", "add", ""), mk("d", "del", "")})
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: one dup and one gone", strconv.Itoa(len(out)), "2"},
		{"negative: use and extern_learn are not duplicated", out[0].Cmd, "bridge fdb add c"},
		{"negative: the dup is EEXIST, as bridge exits", out[0].Expect.Errno + " " + strconv.Itoa(*out[0].Expect.RC), "EEXIST 255"},
		{"negative: the gone is ENOENT, as bridge exits", out[1].Expect.Errno + " " + strconv.Itoa(*out[1].Expect.RC), "ENOENT 255"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestMatrixFdbRows(t *testing.T) {
	rows, err := matrixRows(families["fdb"](), matrixOpts{Seed: 1, Random: 20})
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
		{"positive: every row is a bridge fdb command on its space's device", func(r row) bool {
			if strings.HasPrefix(r.ID, "fdbvx/") {
				return strings.HasSuffix(r.Cmd, " dev vx0") && r.Axes["dev"] == "vx0"
			}
			return strings.HasPrefix(r.Cmd, "bridge fdb ")
		}},
		{"positive: every refusal exits 255, as bridge does", func(r row) bool {
			return r.Expect.RC != nil && (*r.Expect.RC == 0 || *r.Expect.RC == 255)
		}},
		{"negative: no accepted VXLAN row pairs nhid with dst, port or vni", func(r row) bool {
			return r.Axes["nhid"] == "" || *r.Expect.RC != 0 || (r.Axes["dst"] == "" && r.Axes["port"] == "" && r.Axes["vni"] == "")
		}},
		{"negative: no delete carries flags or ext attributes", func(r row) bool {
			return r.Axes["verb"] != "del" || (r.Axes["ntf"] == "" && r.Axes["notify"] == "" && r.Axes["norefresh"] == "")
		}},
		{"positive: the readback dumps the row's device", func(r row) bool {
			return len(r.Readback) == 1 && strings.HasPrefix(r.Readback[0].Cmd, "bridge fdb show dev ")
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
