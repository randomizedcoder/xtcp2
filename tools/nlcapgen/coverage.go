package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// The netlink coverage report (coverage-status.md, "W4"): for each
// message family the rows write, which attributes the kernel's policy
// table accepts and which of them some request in the workbench corpus
// sent. The never-sent list is the to-do list for the next axis values.
//
// The policy tables and the attribute numbers come from a Linux tree: the
// policy is the set of designators in a `struct nla_policy NAME[...] = {`
// initializer, and each name's number is its place in the uapi enum, found
// in whichever header under include/uapi/linux starts it.

// covFamily is one policy table and the requests it judges: every request
// type whose kernel handler parses with it. The route, address, link and
// neighbor tables judge their family's gets and dumps too (inet_rtm_getroute
// and its IPv6 twin, the address dump checks, rtnl_getlink and rtnl_dump_ifinfo,
// the neighbor get and dump checks); the rule and new-nexthop tables only
// the writes. rtnl_linkprop parses an altname add or delete with
// ifla_policy too.
type covFamily struct {
	Key     string   // the report's name for it
	Prefix  string   // the enum's prefix; its first entry is Prefix+"UNSPEC"
	Dir     string   // the directory whose .c files define the policy
	Policy  string   // the policy table's name
	Types   []uint16 // the request types it judges
	Family  uint8    // the family header's family, or 0 for any
	HdrSize int      // the family header's size
}

var covFamilies = []covFamily{
	{"route4", "RTA_", "net/ipv4", "rtm_ipv4_policy",
		[]uint16{unix.RTM_NEWROUTE, unix.RTM_DELROUTE, unix.RTM_GETROUTE}, unix.AF_INET, unix.SizeofRtMsg},
	{"route6", "RTA_", "net/ipv6", "rtm_ipv6_policy",
		[]uint16{unix.RTM_NEWROUTE, unix.RTM_DELROUTE, unix.RTM_GETROUTE}, unix.AF_INET6, unix.SizeofRtMsg},
	{"rule", "FRA_", "net/core", "fib_rule_policy",
		[]uint16{unix.RTM_NEWRULE, unix.RTM_DELRULE}, 0, 12},
	{"nexthop", "NHA_", "net/ipv4", "rtm_nh_policy_new",
		[]uint16{unix.RTM_NEWNEXTHOP}, 0, 8},
	{"addr4", "IFA_", "net/ipv4", "ifa_ipv4_policy",
		[]uint16{unix.RTM_NEWADDR, unix.RTM_DELADDR, unix.RTM_GETADDR}, unix.AF_INET, unix.SizeofIfAddrmsg},
	{"addr6", "IFA_", "net/ipv6", "ifa_ipv6_policy",
		[]uint16{unix.RTM_NEWADDR, unix.RTM_DELADDR, unix.RTM_GETADDR}, unix.AF_INET6, unix.SizeofIfAddrmsg},
	{"neigh", "NDA_", "net/core", "nda_policy",
		[]uint16{unix.RTM_NEWNEIGH, unix.RTM_DELNEIGH, unix.RTM_GETNEIGH}, 0, unix.SizeofNdMsg},
	{"link", "IFLA_", "net/core", "ifla_policy",
		[]uint16{unix.RTM_NEWLINK, unix.RTM_DELLINK, unix.RTM_GETLINK, unix.RTM_NEWLINKPROP, unix.RTM_DELLINKPROP}, 0, unix.SizeofIfInfomsg},
}

var (
	cBlockCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	cLineCommentRE  = regexp.MustCompile(`//[^\n]*`)
	cPreprocRE      = regexp.MustCompile(`(?m)^[ \t]*#.*$`)
	designatorRE    = regexp.MustCompile(`\[\s*([A-Z][A-Z0-9_]*)\s*\]\s*=`)
)

// stripC removes comments and preprocessor lines.
func stripC(src string) string {
	return cPreprocRE.ReplaceAllString(cLineCommentRE.ReplaceAllString(cBlockCommentRE.ReplaceAllString(src, " "), " "), " ")
}

// uapiEnum numbers the enum whose first entry is prefix+"UNSPEC", as C
// does: each entry one past the last, unless given a number or an earlier
// entry's name. Entries starting "__" (the MAX sentinels) are left out.
func uapiEnum(src, prefix string) (map[string]int, error) {
	src = stripC(src)
	anchor := regexp.MustCompile(`\{\s*` + regexp.QuoteMeta(prefix) + `UNSPEC\b`)
	loc := anchor.FindStringIndex(src)
	if loc == nil {
		return nil, fmt.Errorf("no enum starting %sUNSPEC", prefix)
	}
	end := strings.IndexByte(src[loc[0]:], '}')
	if end < 0 {
		return nil, fmt.Errorf("enum %sUNSPEC is not closed", prefix)
	}
	out := map[string]int{}
	next := 0
	for _, entry := range strings.Split(src[loc[0]+1:loc[0]+end], ",") {
		name, val, hasVal := strings.Cut(strings.TrimSpace(entry), "=")
		name, val = strings.TrimSpace(name), strings.TrimSpace(val)
		if name == "" {
			continue
		}
		if hasVal {
			if n, err := strconv.ParseInt(val, 0, 32); err == nil {
				next = int(n)
			} else if n, ok := out[val]; ok {
				next = n
			} else {
				return nil, fmt.Errorf("enum %s: %s = %q is neither a number nor an earlier entry", prefix, name, val)
			}
		}
		if !strings.HasPrefix(name, "__") {
			out[name] = next
		}
		next++
	}
	return out, nil
}

// policyNames returns the designators of `struct nla_policy name[...] = {`
// in src, leaving out the UNSPEC entry, which holds no attribute's policy
// (only strict_start_type). ok is false when src defines no such table.
func policyNames(src, name string) (names []string, ok bool, err error) {
	src = stripC(src)
	re := regexp.MustCompile(`nla_policy\s+` + regexp.QuoteMeta(name) + `\s*\[[^\]]*\]\s*=\s*\{`)
	loc := re.FindStringIndex(src)
	if loc == nil {
		return nil, false, nil
	}
	depth, i := 1, loc[1]
	for ; i < len(src) && depth > 0; i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
	}
	if depth != 0 {
		return nil, true, fmt.Errorf("policy %s is not closed", name)
	}
	for _, m := range designatorRE.FindAllStringSubmatch(src[loc[1]:i], -1) {
		if !strings.HasSuffix(m[1], "_UNSPEC") {
			names = append(names, m[1])
		}
	}
	return names, true, nil
}

// findPolicy reads every .c file in dir for the table, and fails unless
// exactly one defines it.
func findPolicy(fsys fs.FS, dir, name string) ([]string, error) {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var found []string
	var names []string
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".c") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		n, ok, err := policyNames(string(b), name)
		if err != nil {
			return nil, err
		}
		if ok {
			found = append(found, e.Name())
			names = n
		}
	}
	if len(found) != 1 {
		return nil, fmt.Errorf("%s in %s: defined in %d files %v, want 1", name, dir, len(found), found)
	}
	return names, nil
}

// uapiDir is where the attribute enums live.
const uapiDir = "include/uapi/linux"

// findEnum reads every header in dir for the enum starting prefix+"UNSPEC",
// and fails unless exactly one starts it.
func findEnum(fsys fs.FS, dir, prefix string) (map[string]int, string, error) {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, "", err
	}
	var found []string
	var enum map[string]int
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".h") {
			continue
		}
		b, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, "", err
		}
		if !strings.Contains(string(b), prefix+"UNSPEC") {
			continue
		}
		if m, err := uapiEnum(string(b), prefix); err == nil {
			found = append(found, e.Name())
			enum = m
		}
	}
	if len(found) != 1 {
		return nil, "", fmt.Errorf("enum %sUNSPEC in %s: started in %d headers %v, want 1", prefix, dir, len(found), found)
	}
	return enum, path.Join(dir, found[0]), nil
}

// covPolicy is a family's policy, by attribute number.
type covPolicy struct {
	fam   covFamily
	names map[int]string // every attribute the policy accepts
	enum  map[int]string // every name the uapi enum gives a number
}

// loadPolicies reads each family's policy and enum from a Linux tree.
func loadPolicies(linux fs.FS) ([]covPolicy, error) {
	out := make([]covPolicy, 0, len(covFamilies))
	for _, fam := range covFamilies {
		enum, hdr, err := findEnum(linux, uapiDir, fam.Prefix)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fam.Key, err)
		}
		names, err := findPolicy(linux, fam.Dir, fam.Policy)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fam.Key, err)
		}
		p := covPolicy{fam: fam, names: map[int]string{}, enum: map[int]string{}}
		for n, v := range enum {
			if old, ok := p.enum[v]; !ok || n < old {
				p.enum[v] = n
			}
		}
		for _, n := range names {
			v, ok := enum[n]
			if !ok {
				return nil, fmt.Errorf("%s: policy names %s, which %s does not number", fam.Key, n, hdr)
			}
			p.names[v] = n
		}
		out = append(out, p)
	}
	return out, nil
}

// covTally counts, per family, the requests seen and each top-level
// attribute type they sent.
type covTally struct {
	requests map[string]int
	sent     map[string]map[int]int
}

func newCovTally() *covTally {
	return &covTally{requests: map[string]int{}, sent: map[string]map[int]int{}}
}

// add counts one message, if it is a request some family judges.
func (t *covTally) add(m nlparity.Msg) {
	if !m.IsRequest() || m.Proto != unix.NETLINK_ROUTE {
		return
	}
	for i := range covFamilies {
		fam := &covFamilies[i]
		if !slices.Contains(fam.Types, m.Hdr.Type) || len(m.Body) < fam.HdrSize {
			continue
		}
		if fam.Family != 0 && m.Body[0] != fam.Family {
			continue
		}
		t.requests[fam.Key]++
		if t.sent[fam.Key] == nil {
			t.sent[fam.Key] = map[int]int{}
		}
		for b := m.Body[fam.HdrSize:]; len(b) >= 4; {
			alen := int(binary.LittleEndian.Uint16(b[0:2]))
			if alen < 4 || alen > len(b) {
				break
			}
			t.sent[fam.Key][int(binary.LittleEndian.Uint16(b[2:4])&^(unix.NLA_F_NESTED|unix.NLA_F_NET_BYTEORDER))]++
			b = b[min((alen+3)&^3, len(b)):]
		}
		return
	}
}

// tallyCorpus reads every row's ip.pcap under root (the ws directory of
// one kernel version). The readbacks are dumps, which no family judges.
func tallyCorpus(root fs.FS) (*covTally, int, error) {
	t := newCovTally()
	files := 0
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Base(p) != "ip.pcap" {
			return err
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		c, err := nlparity.ParseCapture(b, unix.NETLINK_ROUTE)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		files++
		for _, m := range c.Msgs() {
			t.add(m)
		}
		return nil
	})
	return t, files, err
}

// writeCoverage writes the report: per family, the share of the policy's
// attributes some request sent, the never-sent list, and what was sent
// that the policy doesn't name.
func writeCoverage(w io.Writer, header []string, pols []covPolicy, t *covTally) error {
	var b strings.Builder
	for _, h := range header {
		b.WriteString("# " + h + "\n")
	}
	b.WriteString("#\n# family: policy attributes sent / in the policy (requests seen)\n")
	var body strings.Builder
	for _, p := range pols {
		key := p.fam.Key
		sent := t.sent[key]
		var got, never, extra []string
		for _, v := range slices.Sorted(maps.Keys(p.names)) {
			if sent[v] > 0 {
				got = append(got, p.names[v])
			} else {
				never = append(never, p.names[v])
			}
		}
		for _, v := range slices.Sorted(maps.Keys(sent)) {
			if _, ok := p.names[v]; !ok {
				name := p.enum[v]
				if name == "" {
					name = strconv.Itoa(v)
				}
				extra = append(extra, name)
			}
		}
		fmt.Fprintf(&b, "# %s: %d/%d (%d requests), from %s in %s\n", key, len(got), len(p.names), t.requests[key], p.fam.Policy, p.fam.Dir)
		fmt.Fprintf(&body, "%s never sent: %s\n", key, joinOrNone(never))
		fmt.Fprintf(&body, "%s sent outside the policy: %s\n", key, joinOrNone(extra))
	}
	b.WriteString(body.String())
	_, err := io.WriteString(w, b.String())
	return err
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, " ")
}
