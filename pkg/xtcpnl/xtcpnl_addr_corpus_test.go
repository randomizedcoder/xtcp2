package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The address corpus the link-write capture commits
// (nix/microvms/scripts/addr-corpus.exp, docs/netlink/addr-write-path.md
// "The capture corpus"), written out here as literals: a row dropped from
// the driver and from the capture together still disagrees with this.
var (
	addrWriteSlugs = []string{
		"add_v4", "add_v4_peer", "add_v4_brd_plus", "add_v4_brd_minus", "add_v4_brd_31",
		"add_v4_label", "add_v4_metric", "add_v4_pref_is_metric", "add_v4_proto_name",
		"add_v4_lifetimes", "add_v4_secondary", "add_v4_loopback_scope", "add_v4_autojoin",
		"add_v6_nodad", "add_v6_flags_wide", "add_v6_nodad_first", "add_v6_mngtmpaddr_48",
		"add_v6_nodad_optimistic", "add_existing", "change_missing_creates",
		"replace_existing", "del_v4", "del_v4_wildcard", "del_missing", "nodev",
		"no_dev_keyword", "brd_plus_v6", "valid_zero", "autojoin_unicast",
		"scope_conflict", "v6_mcast_noautojoin", "label_16",
	}
	addrFlushSlugs = []string{
		"flush_dev", "flush_primary", "flush_primary_flipped", "flush_secondary",
		"flush_scope_link", "flush_v6", "flush_nothing", "flush_stats", "flush_many",
		"flush_noargs", "flush_packet",
	}
	addrSaveSlugs = []string{
		"save_dev", "save_all", "save_empty", "showdump", "showdump_bad_magic",
		"restore_fresh", "restore_existing",
	}
	// Rows whose precondition list is empty, so they have no topology file.
	addrNoPreSlugs = []string{"nodev", "no_dev_keyword", "flush_noargs", "flush_packet", "showdump", "showdump_bad_magic"}

	// Each readback state and the suffixes of its selector captures, on top
	// of the base set every state has.
	addrBaseSuffixes   = []string{"", "_json", "_br", "_br_json", "_o", "_d", "_stats"}
	addrStateSelectors = map[string][]string{
		"v4_secondary":      {"_secondary", "_primary", "_not_secondary", "_to", "_label_dev", "_d0"},
		"v4_peer":           nil,
		"v4_label":          {"_label"},
		"v4_brd":            nil,
		"v4_metric":         nil,
		"v4_proto":          {"_proto"},
		"v4_lifetimes":      {"_dynamic", "_permanent", "_not_dynamic"},
		"v4_deprecated":     nil,
		"v4_host_scope":     {"_scope_host", "_scope_all"},
		"v4_autojoin":       nil,
		"v6_flags":          {"_v6", "_o_v6", "_to"},
		"v6_peer":           nil,
		"v6_tentative":      {"_tentative", "_not_tentative"},
		"v6_stable_privacy": {"_scope_link"},
		"v6_dadfailed":      {"_dadfailed"},
		"v6_optimistic":     {"_optimistic"},
		"v6_temporary":      {"_temporary"},
		"v4_vrf":            {"_vrf", "_master"},
		"mixed_links": {"_up", "_down", "_up_down", "_master", "_v4_master", "_nomaster",
			"_type_dummy", "_type_bond_slave", "_v4", "_group", "_novf"},
	}
	// The two states the design calls honest unknowns: built or dropped.
	addrMeasuredStates = []string{"v6_optimistic", "v6_temporary"}
)

// ipSaveMagic is ipadd_save_prep's header (iproute2 ip/ipaddress.c), a
// host-order u32; the capture guest is little-endian x86_64.
const ipSaveMagic = 0x47361222

func addrFixture(parts ...string) string {
	return filepath.Join(append([]string{tdLinkWrite_7_1_4}, parts...)...)
}

func readFixture(t *testing.T, parts ...string) []byte {
	t.Helper()
	bs, err := os.ReadFile(addrFixture(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return bs
}

func fixtureExists(parts ...string) bool {
	_, err := os.Stat(addrFixture(parts...))
	return err == nil
}

// rowRC is a write row's ip.rc as a trimmed string.
func rowRC(t *testing.T, dir, slug string) string {
	t.Helper()
	return strings.TrimSpace(string(readFixture(t, dir, slug, "ip.rc")))
}

// saveMessages splits an `ip addr save` file into its nlmsghdr type and
// address family pairs, after checking the magic.
func saveMessages(t *testing.T, bs []byte) (types []uint16, families []uint8) {
	t.Helper()
	if len(bs) < 4 || binary.LittleEndian.Uint32(bs) != ipSaveMagic {
		t.Fatalf("no ip.save magic in %d bytes", len(bs))
	}
	p := bs[4:]
	for len(p) > 0 {
		var h NlMsgHdr
		if _, err := DeserializeNlMsgHdr(p, &h); err != nil {
			t.Fatal(err)
		}
		mlen := int(h.Len)
		if mlen < NlMsgHdrSizeCst+1 || mlen > len(p) {
			t.Fatalf("nlmsg_len %d with %d bytes left", mlen, len(p))
		}
		types = append(types, h.Type)
		families = append(families, p[NlMsgHdrSizeCst])
		p = p[min(nlmsgAlign(mlen), len(p)):]
	}
	return types, families
}

// stateBuilt reports whether a readback state's topology records it as
// built: no FAIL step and no note saying it was dropped or not built.
func stateBuilt(t *testing.T, state string) bool {
	t.Helper()
	topo := string(readFixture(t, "addr-show", state, "topology"))
	return !strings.Contains(topo, "\nFAIL") && !strings.HasPrefix(topo, "FAIL") &&
		!strings.Contains(topo, "note  dropped") && !strings.Contains(topo, "note  not built")
}

// go test ./pkg/xtcpnl/ -run TestAddrCaptureCorpusComplete
//
// What the capture promised against what was committed: files, outcomes and
// save bytes. Decoding the requests and replies is Phase 1's and later.
func TestAddrCaptureCorpusComplete(t *testing.T) {
	t.Run("layout", func(t *testing.T) {
		rowFiles := []string{"ip.pcap", "ip.out", "ip.err", "ip.rc"}
		tests := []struct {
			name        string
			description string
			dir         string
			slugs       []string
			files       []string
			expected    bool
		}{
			{name: "write_slug_files", description: "positive: every write row has its pcap, stdout, stderr and status",
				dir: "addr", slugs: addrWriteSlugs, files: rowFiles, expected: true},
			{name: "flush_slug_files", description: "positive: every flush row has the same four files",
				dir: "addr-flush", slugs: addrFlushSlugs, files: rowFiles, expected: true},
			{name: "save_slug_files", description: "positive: every save row has the four files",
				dir: "addr-save", slugs: addrSaveSlugs, files: rowFiles, expected: true},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				for _, slug := range tc.slugs {
					for _, f := range tc.files {
						if got := fixtureExists(tc.dir, slug, f); got != tc.expected {
							t.Errorf("%s: %s/%s/%s exists = %v, want %v", tc.description, tc.dir, slug, f, got, tc.expected)
						}
					}
					// Parses as a netlink pcap, even when it holds nothing.
					pcapDatagrams(t, addrFixture(tc.dir, slug, "ip.pcap"))
				}
			})
		}
	})

	t.Run("save_files_present", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
			slug        string
			file        string
			expected    bool
		}{
			{name: "save_dev", description: "positive: a save row keeps what it saved", slug: "save_dev", file: "ip.save", expected: true},
			{name: "save_all", description: "positive: the file the readers copy", slug: "save_all", file: "ip.save", expected: true},
			{name: "save_empty", description: "boundary: an empty save still writes the magic", slug: "save_empty", file: "ip.save", expected: true},
			{name: "bad_magic", description: "negative: the bad-magic row ships its input", slug: "showdump_bad_magic", file: "bad.save", expected: true},
			{name: "bad_magic_no_ip_save", description: "corner: the bad-magic row reads no real save", slug: "showdump_bad_magic", file: "ip.save", expected: false},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if got := fixtureExists("addr-save", tc.slug, tc.file); got != tc.expected {
					t.Errorf("%s: %s/%s exists = %v, want %v", tc.description, tc.slug, tc.file, got, tc.expected)
				}
			})
		}
	})

	t.Run("pre_has_topology", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
			dir         string
			slugs       []string
			expected    bool
		}{
			{name: "write", description: "negative: a write row with preconditions and no clean transcript", dir: "addr", slugs: addrWriteSlugs, expected: true},
			{name: "flush", description: "negative: a flush row with preconditions and no clean transcript", dir: "addr-flush", slugs: addrFlushSlugs, expected: true},
			{name: "save", description: "negative: a save row with preconditions and no clean transcript", dir: "addr-save", slugs: addrSaveSlugs, expected: true},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				for _, slug := range tc.slugs {
					if slices.Contains(addrNoPreSlugs, slug) {
						continue
					}
					got := false
					if bs, err := os.ReadFile(addrFixture(tc.dir, slug, "topology")); err == nil {
						got = len(bs) > 0 && !bytes.Contains(bs, []byte("FAIL"))
					}
					if got != tc.expected {
						t.Errorf("%s: %s/%s has a clean topology = %v, want %v", tc.description, tc.dir, slug, got, tc.expected)
					}
				}
			})
		}
	})

	t.Run("datagrams", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
			dir         string
			slug        string
			expected    int
		}{
			{name: "no_dev_keyword", description: "boundary: refused before any traffic", dir: "addr", slug: "no_dev_keyword", expected: 0},
			{name: "brd_plus_v6", description: "boundary: refused before the lookup", dir: "addr", slug: "brd_plus_v6", expected: 0},
			{name: "flush_noargs", description: "boundary: flush refuses no arguments before any traffic", dir: "addr-flush", slug: "flush_noargs", expected: 0},
			{name: "flush_packet", description: "boundary: flush refuses -0 before any traffic", dir: "addr-flush", slug: "flush_packet", expected: 0},
			{name: "showdump", description: "corner: showdump opens no netlink conversation", dir: "addr-save", slug: "showdump", expected: 0},
			{name: "showdump_bad_magic", description: "corner: nor does a refused showdump", dir: "addr-save", slug: "showdump_bad_magic", expected: 0},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if got := len(pcapDatagrams(t, addrFixture(tc.dir, tc.slug, "ip.pcap"))); got != tc.expected {
					t.Errorf("%s: %d datagrams, want %d", tc.description, got, tc.expected)
				}
			})
		}
	})

	t.Run("no_stray_slugs", func(t *testing.T) {
		tests := []struct {
			name        string
			description string
			dir         string
			slugs       []string
			expected    []string
		}{
			{name: "addr", description: "corner: no write directory left from an earlier corpus", dir: "addr", slugs: addrWriteSlugs, expected: nil},
			{name: "addr-flush", description: "corner: no flush directory left from an earlier corpus", dir: "addr-flush", slugs: addrFlushSlugs, expected: nil},
			{name: "addr-save", description: "corner: no save directory left from an earlier corpus", dir: "addr-save", slugs: addrSaveSlugs, expected: nil},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				entries, err := os.ReadDir(addrFixture(tc.dir))
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, e := range entries {
					if e.IsDir() && !slices.Contains(tc.slugs, e.Name()) {
						got = append(got, e.Name())
					}
				}
				if !slices.Equal(got, tc.expected) {
					t.Errorf("%s: stray %v, want %v", tc.description, got, tc.expected)
				}
			})
		}
	})

	t.Run("outcomes", func(t *testing.T) {
		type outcome struct {
			ok     bool   // ip.rc is 0
			stderr string // a substring of ip.err; empty means not checked
		}
		tests := []struct {
			name        string
			description string
			dir         string
			slug        string
			expected    outcome
		}{
			{name: "success_add_v4", description: "positive: a plain IPv4 add is accepted", dir: "addr", slug: "add_v4", expected: outcome{ok: true}},
			{name: "success_add_v6_flags_wide", description: "positive: the worked example is accepted", dir: "addr", slug: "add_v6_flags_wide", expected: outcome{ok: true}},
			{name: "success_change_missing_creates", description: "positive: change of a missing address adds it", dir: "addr", slug: "change_missing_creates", expected: outcome{ok: true}},
			{name: "success_del_v4", description: "positive: an exact delete is accepted", dir: "addr", slug: "del_v4", expected: outcome{ok: true}},
			{name: "success_flush_dev", description: "positive: a flush of a populated device succeeds", dir: "addr-flush", slug: "flush_dev", expected: outcome{ok: true}},
			{name: "success_save_all", description: "positive: a save to a file succeeds", dir: "addr-save", slug: "save_all", expected: outcome{ok: true}},
			{name: "success_restore_fresh", description: "positive: a restore into a fresh namespace succeeds", dir: "addr-save", slug: "restore_fresh", expected: outcome{ok: true}},
			{name: "success_restore_existing", description: "positive: restore treats EEXIST as success", dir: "addr-save", slug: "restore_existing", expected: outcome{ok: true}},
			{name: "kernel_add_existing", description: "negative: an existing IPv4 address is EEXIST", dir: "addr", slug: "add_existing", expected: outcome{stderr: "Address already assigned"}},
			{name: "kernel_del_missing", description: "negative: deleting a missing address is EADDRNOTAVAIL", dir: "addr", slug: "del_missing", expected: outcome{stderr: "Address not found"}},
			{name: "kernel_scope_conflict", description: "negative: a secondary with another scope is EINVAL", dir: "addr", slug: "scope_conflict", expected: outcome{stderr: "Invalid scope value"}},
			{name: "kernel_v6_mcast_noautojoin", description: "negative: an IPv6 multicast address needs autojoin", dir: "addr", slug: "v6_mcast_noautojoin", expected: outcome{stderr: "IFA_F_MCAUTOJOIN"}},
			{name: "client_nodev", description: "negative: an unknown device stops after the lookup", dir: "addr", slug: "nodev", expected: outcome{stderr: "Cannot find device"}},
			{name: "client_no_dev_keyword", description: "negative: no dev keyword", dir: "addr", slug: "no_dev_keyword", expected: outcome{stderr: "Not enough information"}},
			{name: "client_brd_plus_v6", description: "negative: brd on IPv6", dir: "addr", slug: "brd_plus_v6", expected: outcome{stderr: "Broadcast can be set only for IPv4"}},
			{name: "client_valid_zero", description: "negative: valid_lft 0", dir: "addr", slug: "valid_zero", expected: outcome{stderr: "valid_lft is zero"}},
			{name: "client_autojoin_unicast", description: "negative: autojoin on unicast", dir: "addr", slug: "autojoin_unicast", expected: outcome{stderr: "autojoin needs multicast address"}},
			{name: "client_flush_noargs", description: "negative: flush with no arguments", dir: "addr-flush", slug: "flush_noargs", expected: outcome{stderr: "Flush requires arguments"}},
			{name: "client_flush_packet", description: "negative: flush of link addresses", dir: "addr-flush", slug: "flush_packet", expected: outcome{stderr: "Cannot flush link addresses"}},
			{name: "client_bad_magic", description: "negative: showdump of a file without the magic", dir: "addr-save", slug: "showdump_bad_magic", expected: outcome{stderr: "Magic mismatch"}},
			// M1: the errno is measured, so only the refusal is pinned here.
			{name: "label_16_refused", description: "boundary: a 16-character label is refused by policy", dir: "addr", slug: "label_16", expected: outcome{}},
			{name: "wildcard_warning", description: "corner: a wildcard delete succeeds and warns", dir: "addr", slug: "del_v4_wildcard", expected: outcome{ok: true, stderr: "wildcard deletion"}},
			{name: "nodad_first_warning", description: "corner: a v6 flag before the address warns and is dropped", dir: "addr", slug: "add_v6_nodad_first", expected: outcome{ok: true, stderr: "can be set only for IPv6"}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				gotOK := rowRC(t, tc.dir, tc.slug) == "0"
				if gotOK != tc.expected.ok {
					t.Errorf("%s: rc 0 = %v, want %v", tc.description, gotOK, tc.expected.ok)
				}
				stderr := string(readFixture(t, tc.dir, tc.slug, "ip.err"))
				if !tc.expected.ok && stderr == "" {
					t.Errorf("%s: a refused row with empty stderr", tc.description)
				}
				if tc.expected.stderr != "" && !strings.Contains(stderr, tc.expected.stderr) {
					t.Errorf("%s: stderr %q lacks %q", tc.description, stderr, tc.expected.stderr)
				}
			})
		}
	})

	t.Run("save_bytes", func(t *testing.T) {
		saveAll := readFixture(t, "addr-save", "save_all", "ip.save")
		tests := []struct {
			name        string
			description string
			check       func(t *testing.T) bool
			expected    bool
		}{
			{name: "save_dev_magic", description: "positive: save_dev is the magic then RTM_NEWADDR messages",
				check: func(t *testing.T) bool {
					types, _ := saveMessages(t, readFixture(t, "addr-save", "save_dev", "ip.save"))
					return len(types) > 0 && !slices.ContainsFunc(types, func(ty uint16) bool { return ty != unix.RTM_NEWADDR })
				}, expected: true},
			{name: "save_all_families", description: "positive: save_all holds IPv4 and IPv6 addresses",
				check: func(t *testing.T) bool {
					_, fams := saveMessages(t, saveAll)
					return slices.Contains(fams, unix.AF_INET) && slices.Contains(fams, unix.AF_INET6)
				}, expected: true},
			{name: "bad_magic_input", description: "negative: bad.save is exactly 4 zero bytes",
				check: func(t *testing.T) bool {
					return bytes.Equal(readFixture(t, "addr-save", "showdump_bad_magic", "bad.save"), make([]byte, 4))
				}, expected: true},
			{name: "save_empty_magic_only", description: "boundary: an empty save is the magic alone",
				check: func(t *testing.T) bool {
					bs := readFixture(t, "addr-save", "save_empty", "ip.save")
					return len(bs) == 4 && binary.LittleEndian.Uint32(bs) == ipSaveMagic
				}, expected: true},
			{name: "consumer_copy_identical", description: "corner: every reader ships the exact bytes save_all wrote",
				check: func(t *testing.T) bool {
					for _, s := range []string{"showdump", "restore_fresh", "restore_existing"} {
						if !bytes.Equal(readFixture(t, "addr-save", s, "ip.save"), saveAll) {
							return false
						}
					}
					return true
				}, expected: true},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.check(t); got != tc.expected {
					t.Errorf("%s: got %v, want %v", tc.description, got, tc.expected)
				}
			})
		}
	})

	t.Run("readback", func(t *testing.T) {
		states := make([]string, 0, len(addrStateSelectors))
		for s := range addrStateSelectors {
			states = append(states, s)
		}
		slices.Sort(states)
		tests := []struct {
			name        string
			description string
			check       func(t *testing.T) []string // what is wrong; nil when nothing
			expected    []string
		}{
			{name: "state_has_topology", description: "positive: every state records how it was built",
				check: func(t *testing.T) (bad []string) {
					for _, s := range states {
						if !fixtureExists("addr-show", s, "topology") {
							bad = append(bad, s)
						}
					}
					return bad
				}, expected: nil},
			{name: "built_state_base_files", description: "positive: a built state has every base and selector capture",
				check: func(t *testing.T) (bad []string) {
					for _, s := range states {
						if !stateBuilt(t, s) {
							continue
						}
						for _, sfx := range append(slices.Clone(addrBaseSuffixes), addrStateSelectors[s]...) {
							for _, f := range []string{"netlink_route_getaddr" + sfx + ".pcap", "ip_addr" + sfx} {
								if !fixtureExists("addr-show", s, f) {
									bad = append(bad, s+"/"+f)
								}
							}
						}
					}
					return bad
				}, expected: nil},
			{name: "required_states_built", description: "negative: no required state is missing",
				check: func(t *testing.T) (bad []string) {
					for _, s := range states {
						if !slices.Contains(addrMeasuredStates, s) && !stateBuilt(t, s) {
							bad = append(bad, s)
						}
					}
					return bad
				}, expected: nil},
			{name: "measured_states_resolved", description: "boundary: a measured state is built whole or dropped with nothing captured",
				check: func(t *testing.T) (bad []string) {
					for _, s := range addrMeasuredStates {
						if stateBuilt(t, s) {
							continue
						}
						if fixtureExists("addr-show", s, "netlink_route_getaddr.pcap") {
							bad = append(bad, s+": dropped but captured")
						}
						if !strings.Contains(string(readFixture(t, "addr-show", s, "topology")), "note  dropped "+s) {
							bad = append(bad, s+": no drop note")
						}
					}
					return bad
				}, expected: nil},
			{name: "vrf_d1_write_files", description: "corner: the refused vrf selector keeps its stderr and status",
				check: func(t *testing.T) (bad []string) {
					for _, ext := range []string{".pcap", ".out", ".err", ".rc"} {
						if !fixtureExists("addr-show", "v4_vrf", "ip_addr_vrf_d1"+ext) {
							bad = append(bad, ext)
						}
					}
					if len(bad) > 0 {
						return bad
					}
					if strings.TrimSpace(string(readFixture(t, "addr-show", "v4_vrf", "ip_addr_vrf_d1.rc"))) == "0" {
						bad = append(bad, "rc 0")
					}
					if !strings.Contains(string(readFixture(t, "addr-show", "v4_vrf", "ip_addr_vrf_d1.err")), "Not a valid VRF name") {
						bad = append(bad, "stderr")
					}
					return bad
				}, expected: nil},
			{name: "flush_primary_default_recorded", description: "corner: the M3 probe recorded 0 or 1",
				check: func(t *testing.T) (bad []string) {
					v := strings.TrimSpace(string(readFixture(t, "addr-flush", "flush_primary", "promote_secondaries_default")))
					if v != "0" && v != "1" {
						bad = append(bad, v)
					}
					return bad
				}, expected: nil},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.check(t); !slices.Equal(got, tc.expected) {
					t.Errorf("%s: %v, want %v", tc.description, got, tc.expected)
				}
			})
		}
	})
}
