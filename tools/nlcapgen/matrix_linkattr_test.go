package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildLinkAttr(t *testing.T) {
	sp := linkAttrSpace()
	cases := []struct {
		description string
		names       map[string]string
		cmd         string
		axes        string // the axes named in want, as k=v
		want        []string
	}{
		{"boundary: every axis absent names only the device", nil, "ip link set dev d0", "", nil},
		{"positive: a bridge master and a carrier", map[string]string{"master": "bridge", "carrier": "on"},
			"ip link set dev d0 master br0 carrier on", "master=br0 carrier=1", []string{"master", "carrier"}},
		{"corner: nomaster's master is empty", map[string]string{"master": "nomaster"}, "ip link set dev d0 nomaster",
			"master=", []string{"master"}},
		{"corner: xdp comes last, after netns and its index", map[string]string{"xdp": "drv", "netns": "self-index", "addrgenmode": "random"},
			"ip link set dev d0 addrgenmode random netns $$ index 100 xdpdrv off",
			"addrgenmode=3 netns=fd new_ifindex=100 xdp_flags=5", []string{"addrgenmode", "netns", "new_ifindex", "xdp_flags"}},
		{"positive: a vrf is a master", map[string]string{"master": "vrf"}, "ip link set dev d0 vrf v0", "master=v0", []string{"master"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(sp.assign(tc.names))
			if r.Cmd != tc.cmd {
				t.Errorf("cmd %q, want %q", r.Cmd, tc.cmd)
			}
			got := make([]string, 0, len(tc.want))
			for _, k := range tc.want {
				got = append(got, k+"="+r.Axes[k])
			}
			if s := strings.Join(got, " "); s != tc.axes {
				t.Errorf("axes %q, want %q", s, tc.axes)
			}
			if r.Pre[0].Cmd != linkAttrSetup || r.Readback[0].Cmd != "ip -d link show" {
				t.Errorf("pre %q readback %q", r.Pre[0].Cmd, r.Readback[0].Cmd)
			}
		})
	}
}

func TestDeriveLinkAttrCorners(t *testing.T) {
	out := deriveLinkAttrCorners(nil)
	byID := map[string]row{}
	for _, r := range out {
		byID[r.ID] = r
	}
	cases := []struct {
		description string
		id          string
		expected    string // cmd, rc, refused by
	}{
		{"negative: xdp off before carrier is refused by iproute2", "linkattr/xdp-not-last", "ip link set dev d0 xdp off carrier on 255 iproute2"},
		{"positive: parentdev on a set is accepted", "linkattr/parentdev", "ip link set dev d0 parentdev br0 0 none"},
		{"corner: no third row", "", " 0 "},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := byID[tc.id]
			rc := 0
			if r.Expect.RC != nil {
				rc = *r.Expect.RC
			}
			if got := fmt.Sprintf("%s %d %s", r.Cmd, rc, r.Expect.RefusedBy); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
			if tc.id != "" && (r.Pre[0].Cmd != linkAttrSetup || r.Axes == nil) {
				t.Errorf("pre %q axes %v", r.Pre[0].Cmd, r.Axes)
			}
		})
	}
	if len(out) != 2 {
		t.Errorf("%d rows, want 2", len(out))
	}
}

func TestLinkAttrSetups(t *testing.T) {
	cases := []struct {
		description string
		setup       string
		expected    []string
	}{
		{"positive: linkattr adds d0, a bridge and a VRF", linkAttrSetup,
			[]string{"ip link add d0 type dummy", "ip link add br0 type bridge", "ip link add v0 type vrf table 10"}},
		{"positive: linkpd adds a macvlan, which can go protodown", linkPDSetup,
			[]string{"ip link add d0 type dummy", "ip link add m0 link d0 type macvlan mode bridge"}},
		{"positive: linkprop adds br0, the taken name", linkPropSetup, []string{"ip link add d0 type dummy", "ip link add br0 type bridge"}},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			for _, want := range tc.expected {
				if !strings.Contains(tc.setup, want) {
					t.Errorf("setup %q lacks %q", tc.setup, want)
				}
			}
		})
	}
}

func TestLinkPDJudge(t *testing.T) {
	cases := []struct {
		description       string
		protodown, reason string
		expected          string // RefusedBy and errno, "" for success
	}{
		{"negative: protodown off with a reason on", "off", "0-on", "kernel EBUSY"},
		{"negative: so with the top reason bit", "off", "31-on", "kernel EBUSY"},
		{"positive: protodown off clearing the reason", "off", "0-off", ""},
		{"positive: protodown on with a reason", "on", "0-on", ""},
		{"boundary: no protodown, a reason on", "absent", "31-on", ""},
		{"corner: protodown off with no reason", "off", "absent", ""},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			exp, why := linkPDJudge(map[string]string{"protodown": tc.protodown, "reason": tc.reason})
			got := ""
			if exp != nil {
				got = strings.TrimSpace(exp.RefusedBy + " " + exp.Errno)
				if why == "" {
					t.Error("a refusal with no reason")
				}
			}
			if got != tc.expected {
				t.Errorf("refused by %q (%s), want %q", got, why, tc.expected)
			}
		})
	}
}

func TestBuildLinkPD(t *testing.T) {
	sp := linkPDSpace()
	cases := []struct {
		description string
		names       map[string]string
		cmd         string
		axes        string // protodown pd_mask pd_value up
	}{
		{"boundary: every axis absent names only m0", nil, "ip link set dev m0", "///"},
		{"positive: protodown on with reason 0", map[string]string{"protodown": "on", "reason": "0-on"},
			"ip link set dev m0 protodown on protodown_reason 0 on", "1/1/1/"},
		{"positive: a reason cleared", map[string]string{"reason": "0-off", "updown": "up"},
			"ip link set dev m0 protodown_reason 0 off up", "/1/0/1"},
		{"boundary: reason 31 is the top bit, in mask and value", map[string]string{"reason": "31-on"},
			"ip link set dev m0 protodown_reason 31 on", "/2147483648/2147483648/"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(sp.assign(tc.names))
			ax := r.Axes["protodown"] + "/" + r.Axes["pd_mask"] + "/" + r.Axes["pd_value"] + "/" + r.Axes["up"]
			if r.Cmd != tc.cmd || ax != tc.axes {
				t.Errorf("cmd %q axes %q, want %q and %q", r.Cmd, ax, tc.cmd, tc.axes)
			}
			if r.Pre[0].Cmd != linkPDSetup || r.Readback[0].Cmd != "ip -d link show dev m0" {
				t.Errorf("pre %q readback %q", r.Pre[0].Cmd, r.Readback[0].Cmd)
			}
		})
	}
}

func TestBuildLinkProp(t *testing.T) {
	sp := linkPropSpace()
	cases := []struct {
		description string
		names       map[string]string
		cmd         string
		pre         string
	}{
		{"positive: an add needs no altname first", nil, "ip link property add altname alt0 dev d0", linkPropSetup},
		{"positive: a del adds its names first", map[string]string{"op": "del", "altname": "two", "dev": "bare"},
			"ip link property del altname alt1 altname alt2 d0", linkPropSetup + " && ip link property add dev d0 altname alt1 altname alt2"},
		{"positive: a show names the device by its altname", map[string]string{"op": "show", "altname": "long"},
			"ip link show dev alternate-name-0", linkPropSetup + " && ip link property add dev d0 altname alternate-name-0"},
		{"corner: a bare show shows the first of two", map[string]string{"op": "show", "altname": "two", "dev": "bare"},
			"ip link show alt1", linkPropSetup + " && ip link property add dev d0 altname alt1 altname alt2"},
		{"boundary: the longest altname", map[string]string{"altname": "127"},
			"ip link property add altname " + strings.Repeat("b", 127) + " dev d0", linkPropSetup},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			r := sp.Build(sp.assign(tc.names))
			if r.Cmd != tc.cmd || r.Pre[0].Cmd != tc.pre {
				t.Errorf("cmd %q pre %q, want %q and %q", r.Cmd, r.Pre[0].Cmd, tc.cmd, tc.pre)
			}
			if r.Readback[0].Cmd != "ip -d link show dev d0" {
				t.Errorf("readback %q", r.Readback[0].Cmd)
			}
		})
	}
}

func TestDeriveLinkPropAbsent(t *testing.T) {
	mk := func(id, op string, rc int) row {
		return row{ID: id, Desc: "corner: x", Cmd: "ip link property " + op + " " + id, Pre: []preStep{{Cmd: linkPropSetup}},
			Expect: expect{RC: intp(rc)}, Axes: map[string]string{"op": op}}
	}
	out := deriveLinkPropAbsent([]row{mk("a", "del", 0), mk("b", "add", 2), mk("c", "add", 0), mk("d", "add", 0)})
	cases := []struct {
		description string
		got, want   string
	}{
		{"positive: two rows", string(rune('0' + len(out))), "2"},
		{"negative: the absent delete", out[0].ID + " " + out[0].Expect.Errno, "linkprop/del-absent ENOENT"},
		{"negative: the first accepted add, repeated", out[1].Cmd + " " + out[1].Expect.Errno, "ip link property add c EEXIST"},
		{"positive: the repeat runs the add first", out[1].Pre[len(out[1].Pre)-1].Cmd, "ip link property add c"},
		{"positive: the repeat says negative", out[1].Desc[:9], "negative:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestMatrixLinkAttrRows(t *testing.T) {
	rows, err := matrixRows(families["linkattr"](), matrixOpts{Seed: 1, Random: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	neg := func(r row) bool {
		return !strings.HasPrefix(r.Desc, "positive:") && r.Expect.RC != nil && *r.Expect.RC != 0
	}
	cases := []struct {
		description string
		check       func(r row) bool
	}{
		{"positive: an xdp off is the last argument, but in the row made to break that", func(r row) bool {
			i := strings.Index(r.Cmd, " xdp")
			return i < 0 || r.ID == "linkattr/xdp-not-last" || strings.HasSuffix(r.Cmd, " off") || strings.HasSuffix(r.Cmd, " none")
		}},
		{"positive: an index only follows netns", func(r row) bool {
			return !strings.Contains(r.Cmd, " index ") || strings.Contains(r.Cmd, "netns $$ index") || neg(r)
		}},
		{"negative: protodown off with a reason on is refused", func(r row) bool {
			return !strings.HasPrefix(r.ID, "linkpd/") || r.Axes["protodown"] != "0" || r.Axes["pd_value"] == "" || r.Axes["pd_value"] == "0" ||
				r.Expect.Errno == "EBUSY"
		}},
		{"positive: each row works on its own space's device", func(r row) bool {
			switch strings.Split(r.ID, "/")[0] {
			case "linkattr":
				return strings.HasPrefix(r.Cmd, "ip link set dev d0")
			case "linkpd":
				return strings.HasPrefix(r.Cmd, "ip link set dev m0")
			default:
				return strings.HasPrefix(r.Cmd, "ip link property ") || strings.HasPrefix(r.Cmd, "ip link show ")
			}
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
