package main

import (
	"encoding/binary"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func TestUapiEnum(t *testing.T) {
	cases := []struct {
		description string
		src         string
		prefix      string
		expected    string // name=value pairs for the names asked, or the error's text
		names       []string
	}{
		{"positive: entries count from 0", "enum {\n\tX_UNSPEC,\n\tX_A,\n\tX_B,\n\t__X_MAX\n};", "X_", "X_UNSPEC=0 X_A=1 X_B=2", []string{"X_UNSPEC", "X_A", "X_B"}},
		{"positive: comments and #defines are skipped", "enum {\n\tX_UNSPEC,\n\tX_A, /* a */\n#define X_A X_A\n\tX_B, // b\n};", "X_", "X_A=1 X_B=2", []string{"X_A", "X_B"}},
		{"corner: a number resets the count", "enum {\n\tX_UNSPEC,\n\tX_A = 10,\n\tX_B,\n};", "X_", "X_A=10 X_B=11", []string{"X_A", "X_B"}},
		{"corner: an alias takes an earlier entry's number", "enum {\n\tX_UNSPEC,\n\tX_PAD = X_UNSPEC,\n\tX_A,\n};", "X_", "X_PAD=0 X_A=1", []string{"X_PAD", "X_A"}},
		{"boundary: the MAX sentinel is left out", "enum {\n\tX_UNSPEC,\n\t__X_MAX\n};", "X_", "__X_MAX=", []string{"__X_MAX"}},
		{"corner: a longer prefix's enum is not taken", "enum {\n\tX_SUB_UNSPEC,\n\tX_SUB_A,\n};\nenum {\n\tX_UNSPEC,\n\tX_A,\n};", "X_", "X_A=1 X_SUB_A=", []string{"X_A", "X_SUB_A"}},
		{"negative: no such enum", "enum {\n\tY_UNSPEC,\n};", "X_", "no enum starting X_UNSPEC", nil},
		{"negative: a value that is neither", "enum {\n\tX_UNSPEC,\n\tX_A = Y_B,\n};", "X_", "neither a number", nil},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			got, err := uapiEnum(tc.src, tc.prefix)
			if err != nil {
				if tc.names != nil || !strings.Contains(err.Error(), tc.expected) {
					t.Fatalf("err %v, want %q", err, tc.expected)
				}
				return
			}
			parts := make([]string, 0, len(tc.names))
			for _, n := range tc.names {
				v, ok := got[n]
				s := n + "="
				if ok {
					s += strconv.Itoa(v)
				}
				parts = append(parts, s)
			}
			if s := strings.Join(parts, " "); s != tc.expected {
				t.Errorf("got %q, want %q", s, tc.expected)
			}
		})
	}
}

func TestPolicyNames(t *testing.T) {
	src := "static const struct nla_policy p[X_MAX + 1] = {\n" +
		"\t[X_UNSPEC] = { .strict_start_type = X_B + 1 },\n" +
		"\t[X_A] = { .type = NLA_U32 }, /* [X_C] = in a comment */\n" +
		"\t[X_B] = NLA_POLICY_MAX(NLA_U8, 3),\n" +
		"};\nstatic const struct nla_policy q[] = {\n\t[X_D] = { .type = NLA_U8 },\n};\n"
	cases := []struct {
		description string
		name        string
		expected    string
		ok          bool
	}{
		{"positive: every designator but UNSPEC, not those in comments", "p", "X_A X_B", true},
		{"positive: an unsized table", "q", "X_D", true},
		{"negative: a table the source doesn't define", "r", "", false},
		{"corner: a name that only prefixes a table's", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			names, ok, err := policyNames(src, tc.name)
			if err != nil || ok != tc.ok || strings.Join(names, " ") != tc.expected {
				t.Errorf("got %v %v %v, want %q %v", names, ok, err, tc.expected, tc.ok)
			}
		})
	}
	if _, _, err := policyNames("struct nla_policy p[] = {\n\t[X_A] = { .type = NLA_U8 },\n", "p"); err == nil {
		t.Error("an unclosed table: want an error")
	}
}

func TestFindPolicy(t *testing.T) {
	pol := "static const struct nla_policy p[] = {\n\t[X_A] = { .type = NLA_U8 },\n};\n"
	cases := []struct {
		description string
		fsys        fstest.MapFS
		expected    string
	}{
		{"positive: the one file that defines it", fstest.MapFS{"net/a.c": {Data: []byte(pol)}, "net/b.c": {Data: []byte("int x;")}}, "X_A"},
		{"negative: defined twice", fstest.MapFS{"net/a.c": {Data: []byte(pol)}, "net/b.c": {Data: []byte(pol)}}, "defined in 2 files"},
		{"negative: defined nowhere", fstest.MapFS{"net/b.c": {Data: []byte("int x;")}}, "defined in 0 files"},
		{"corner: a header is not read", fstest.MapFS{"net/a.h": {Data: []byte(pol)}, "net/b.c": {Data: []byte("int x;")}}, "defined in 0 files"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			names, err := findPolicy(tc.fsys, "net", "p")
			got := strings.Join(names, " ")
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// covMsg is a request of type typ: a family header of hdrLen bytes whose
// first byte is family, then one attribute of each type in attrs.
func covMsg(typ uint16, flags uint16, family byte, hdrLen int, attrs ...uint16) nlparity.Msg {
	body := make([]byte, hdrLen)
	body[0] = family
	for _, a := range attrs {
		body = binary.LittleEndian.AppendUint16(body, 8)
		body = binary.LittleEndian.AppendUint16(body, a)
		body = append(body, 0, 0, 0, 0)
	}
	return nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags}, Body: body}
}

func TestCovTally(t *testing.T) {
	req := uint16(unix.NLM_F_REQUEST)
	cases := []struct {
		description string
		msg         nlparity.Msg
		expected    string // family:requests:attrs
	}{
		{"positive: an IPv4 route add counts its attributes", covMsg(unix.RTM_NEWROUTE, req, unix.AF_INET, 12, unix.RTA_DST, unix.RTA_OIF), "route4:1:1,4"},
		{"positive: an IPv6 one counts for route6", covMsg(unix.RTM_DELROUTE, req, unix.AF_INET6, 12, unix.RTA_GATEWAY), "route6:1:5"},
		{"corner: a nested flag is masked off", covMsg(unix.RTM_NEWROUTE, req, unix.AF_INET, 12, unix.RTA_METRICS|unix.NLA_F_NESTED), "route4:1:8"},
		{"positive: a neighbor of any family", covMsg(unix.RTM_NEWNEIGH, req, unix.AF_INET6, 12, unix.NDA_DST), "neigh:1:1"},
		{"negative: a reply is not a request", covMsg(unix.RTM_NEWROUTE, 0, unix.AF_INET, 12, unix.RTA_DST), ":0:"},
		{"positive: a route get is judged by the same policy", covMsg(unix.RTM_GETROUTE, req, unix.AF_INET, 12, unix.RTA_IIF), "route4:1:3"},
		{"negative: a rule dump is no family's", covMsg(unix.RTM_GETRULE, req, unix.AF_INET, 12, unix.FRA_TABLE), ":0:"},
		{"corner: an AF_UNSPEC address dump is neither family's", covMsg(unix.RTM_GETADDR, req, unix.AF_UNSPEC, 8), ":0:"},
		{"negative: a nexthop delete is judged by another policy", covMsg(unix.RTM_DELNEXTHOP, req, 0, 8, unix.NHA_ID), ":0:"},
		{"boundary: a body shorter than its header", covMsg(unix.RTM_NEWLINK, req, 0, 12), ":0:"},
		{"corner: an odd-length attribute is padded to four bytes before the next", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWROUTE, Flags: req},
			Body: append(append([]byte{unix.AF_INET, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 5, 0, unix.RTA_PRIORITY, 0, 9, 0, 0, 0), 8, 0, unix.RTA_OIF, 0, 2, 0, 0, 0)}, "route4:1:4,6"},
		{"corner: an attribute overrunning the body ends the walk", nlparity.Msg{Hdr: xtcpnl.NlMsgHdr{Type: unix.RTM_NEWADDR, Flags: req},
			Body: append(append(make([]byte, 0, 16), unix.AF_INET, 0, 0, 0, 0, 0, 0, 0), 40, 0, 2, 0)}, "addr4:1:"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			tl := newCovTally()
			tl.add(tc.msg)
			var fam string
			for k := range tl.requests {
				fam = k
			}
			var attrs []string
			for a := 0; a < 64; a++ {
				if tl.sent[fam][a] > 0 {
					attrs = append(attrs, strconv.Itoa(a))
				}
			}
			got := fam + ":" + strconv.Itoa(tl.requests[fam]) + ":" + strings.Join(attrs, ",")
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestWriteCoverage(t *testing.T) {
	pol := covPolicy{fam: covFamily{Key: "route4", Policy: "rtm_ipv4_policy", Dir: "net/ipv4"},
		names: map[int]string{1: "RTA_DST", 4: "RTA_OIF", 5: "RTA_GATEWAY"},
		enum:  map[int]string{1: "RTA_DST", 4: "RTA_OIF", 5: "RTA_GATEWAY", 18: "RTA_VIA"}}
	tl := newCovTally()
	tl.add(covMsg(unix.RTM_NEWROUTE, unix.NLM_F_REQUEST, unix.AF_INET, 12, unix.RTA_DST, 18, 99))
	var b strings.Builder
	if err := writeCoverage(&b, []string{"h"}, []covPolicy{pol}, tl); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	cases := []struct {
		description string
		expected    string
	}{
		{"positive: the share sent", "# route4: 1/3 (1 requests), from rtm_ipv4_policy in net/ipv4\n"},
		{"positive: the never-sent list, in number order", "route4 never sent: RTA_OIF RTA_GATEWAY\n"},
		{"corner: an attribute outside the policy by name, or by number when unnamed", "route4 sent outside the policy: RTA_VIA 99\n"},
		{"positive: the header", "# h\n"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			if !strings.Contains(out, tc.expected) {
				t.Errorf("report:\n%s\nwant it to contain %q", out, tc.expected)
			}
		})
	}
	tl = newCovTally()
	b.Reset()
	if err := writeCoverage(&b, nil, []covPolicy{pol}, tl); err != nil || !strings.Contains(b.String(), "route4 sent outside the policy: none") {
		t.Errorf("boundary: no requests: %v\n%s", err, b.String())
	}
}

func TestLoadPolicies(t *testing.T) {
	fsys := fstest.MapFS{}
	for _, fam := range covFamilies {
		hdr := "enum {\n\t" + fam.Prefix + "UNSPEC,\n\t" + fam.Prefix + "A,\n\t" + fam.Prefix + "B,\n};\n"
		fsys[uapiDir+"/"+strings.ToLower(strings.TrimSuffix(fam.Prefix, "_"))+".h"] = &fstest.MapFile{Data: []byte(hdr)}
		pol := "static const struct nla_policy " + fam.Policy + "[] = {\n\t[" + fam.Prefix + "B] = { .type = NLA_U8 },\n};\n"
		fsys[fam.Dir+"/"+fam.Key+".c"] = &fstest.MapFile{Data: []byte(pol)}
	}
	pols, err := loadPolicies(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(pols) != len(covFamilies) || pols[0].names[2] != covFamilies[0].Prefix+"B" || len(pols[0].names) != 1 {
		t.Errorf("got %+v", pols[0])
	}
	bad := fstest.MapFS{}
	for k, v := range fsys {
		bad[k] = v
	}
	bad[covFamilies[0].Dir+"/"+covFamilies[0].Key+".c"] = &fstest.MapFile{Data: []byte("static const struct nla_policy " +
		covFamilies[0].Policy + "[] = {\n\t[NOT_NUMBERED] = { .type = NLA_U8 },\n};\n")}
	if _, err := loadPolicies(bad); err == nil || !strings.Contains(err.Error(), "does not number") {
		t.Errorf("negative: a policy naming an attribute the enum lacks: %v", err)
	}
}

func TestFindEnum(t *testing.T) {
	enum := func(prefix string) []byte { return []byte("enum {\n\t" + prefix + "UNSPEC,\n\t" + prefix + "A,\n};\n") }
	cases := []struct {
		description string
		fsys        fstest.MapFS
		expected    string // the header found, or the error's text
	}{
		{"positive: the one header that starts it", fstest.MapFS{"inc/a.h": {Data: enum("X_")}, "inc/b.h": {Data: enum("Y_")}}, "inc/a.h"},
		{"negative: started twice", fstest.MapFS{"inc/a.h": {Data: enum("X_")}, "inc/b.h": {Data: enum("X_")}}, "started in 2 headers"},
		{"negative: started nowhere", fstest.MapFS{"inc/b.h": {Data: enum("Y_")}}, "started in 0 headers"},
		{"corner: a mention that starts no enum is not a definition", fstest.MapFS{"inc/a.h": {Data: enum("X_")},
			"inc/b.h": {Data: []byte("#define FOO X_UNSPEC\n")}}, "inc/a.h"},
		{"corner: a C file is not read", fstest.MapFS{"inc/a.c": {Data: enum("X_")}}, "started in 0 headers"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			_, hdr, err := findEnum(tc.fsys, "inc", "X_")
			got := hdr
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tc.expected) {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
