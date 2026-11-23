package xtcpnl

// The devlink rows' O3 and O4 (tools/nlcapgen matrix_devlink.go, W5-devlink).
// Each row's namespace holds one netdevsim device, and the row runs a
// read-only devlink dump over the generic-netlink "devlink" family.
//
// O3 holds each row's request to its facts: the dump goes over the devlink
// family (resolved from the capture's CTRL_CMD_NEWFAMILY reply, the way
// ethGenlID resolves ethtool), carries NLM_F_DUMP, and the reply decodes —
// through ParseDevlink's typed sections — to the one section the row's
// `words` axis names. The row is tied to its operation by that decoded
// section, NOT by the request's DEVLINK_CMD number: those numbers are
// kernel-specific (commands inserted mid-enum shift later ones, so the
// capture kernel's wire command differs from the upstream number the
// `cmd` axis records — e.g. `resource show` is 36 here, 40 upstream),
// whereas the DEVLINK_ATTR_* tree a section is built from is append-only
// and stable. This is the same attribute-driven classification ParseDevlink
// itself uses.
//
// O4 holds each `-j` row's stdout to the decoded reply: the JSON's
// top-level key is the section's key, and every identifier ParseDevlink
// pulls from the reply (bus/dev handle, port netdev, resource/param/region/
// reporter/trap/group names, info driver) appears in that JSON. A decoder
// that dropped or misread one of those would not be found in devlink's own
// output.
//
// The two negatives pin the no-reply paths: `nosuch` is refused by iproute2
// before any request reaches the wire (no devlink request, no reply);
// `nodev` reaches the kernel as a targeted (non-dump) GET that ENODEVs (a
// request, but no decodable section reply).

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// dlExpect is what a row's `words` axis implies about its reply: the
// section ParseDevlink must classify the reply into, that section's
// top-level JSON key (empty when the dump is empty, like `sb show`), and
// the identifiers the section carries (checked present in `-j` stdout).
type dlExpect struct {
	jsonKey string
	present func(DevlinkMessage) bool
	ids     func(DevlinkMessage) []string
}

// dlSections maps each captured `words` axis to its section expectation.
var dlSections = map[string]dlExpect{
	"dev show": {
		jsonKey: "dev",
		// A bare dev entry: a handle with no sub-section attribute.
		present: func(m DevlinkMessage) bool {
			return m.BusName != "" && m.Port == nil && len(m.Resources) == 0 &&
				m.Param == nil && m.Region == nil && m.Info == nil &&
				m.HealthReporter == nil && m.Trap == nil && m.TrapGroup == nil
		},
		ids: func(m DevlinkMessage) []string { return []string{m.BusName + "/" + m.DevName} },
	},
	"port show": {
		jsonKey: "port",
		present: func(m DevlinkMessage) bool { return m.Port != nil },
		ids: func(m DevlinkMessage) []string {
			if m.Port != nil && m.Port.NetdevName != "" {
				return []string{m.Port.NetdevName}
			}
			return nil
		},
	},
	"sb show": {jsonKey: ""}, // netdevsim registers no shared buffer: empty dump.
	"resource show": {
		jsonKey: "resources",
		present: func(m DevlinkMessage) bool { return len(m.Resources) > 0 },
		ids:     func(m DevlinkMessage) []string { return resourceNames(m.Resources) },
	},
	"dev param show": {
		jsonKey: "param",
		present: func(m DevlinkMessage) bool { return m.Param != nil },
		ids: func(m DevlinkMessage) []string {
			if m.Param != nil {
				return []string{m.Param.Name}
			}
			return nil
		},
	},
	"region show": {
		jsonKey: "regions",
		present: func(m DevlinkMessage) bool { return m.Region != nil },
		ids: func(m DevlinkMessage) []string {
			if m.Region != nil {
				return []string{m.Region.Name}
			}
			return nil
		},
	},
	"dev info": {
		jsonKey: "info",
		present: func(m DevlinkMessage) bool { return m.Info != nil },
		ids: func(m DevlinkMessage) []string {
			if m.Info != nil {
				return []string{m.Info.DriverName}
			}
			return nil
		},
	},
	"health show": {
		jsonKey: "health",
		present: func(m DevlinkMessage) bool { return m.HealthReporter != nil },
		ids: func(m DevlinkMessage) []string {
			if m.HealthReporter != nil {
				return []string{m.HealthReporter.Name}
			}
			return nil
		},
	},
	"trap show": {
		jsonKey: "trap",
		present: func(m DevlinkMessage) bool { return m.Trap != nil },
		ids: func(m DevlinkMessage) []string {
			if m.Trap != nil {
				return []string{m.Trap.Name}
			}
			return nil
		},
	},
	"trap group show": {
		jsonKey: "trap_group",
		present: func(m DevlinkMessage) bool { return m.TrapGroup != nil },
		ids: func(m DevlinkMessage) []string {
			if m.TrapGroup != nil {
				return []string{m.TrapGroup.Name}
			}
			return nil
		},
	},
}

// resourceNames is every resource name in the tree, parents then children,
// so O4 can confirm each one reached devlink's output.
func resourceNames(rs []DevlinkResource) []string {
	var out []string
	for _, r := range rs {
		if r.Name != "" {
			out = append(out, r.Name)
		}
		out = append(out, resourceNames(r.Children)...)
	}
	return out
}

// devlinkGenlID is the devlink family's id, from the CTRL_CMD_NEWFAMILY
// reply in msgs (modeled on ethGenlID).
func devlinkGenlID(msgs []capturedNlMsg) (uint16, bool) {
	for _, m := range msgs {
		if m.h.Type != ethGenlCtrl || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == DevlinkGenlName {
			return f.ID, true
		}
	}
	return 0, false
}

// dlMsgs splits msgs into the devlink family's requests and replies.
func dlMsgs(msgs []capturedNlMsg, id uint16) (reqs, replies []capturedNlMsg) {
	for _, m := range msgs {
		if m.h.Type != id || len(m.body) == 0 {
			continue
		}
		if m.h.Flags&unix.NLM_F_REQUEST != 0 {
			reqs = append(reqs, m)
		} else {
			replies = append(replies, m)
		}
	}
	return reqs, replies
}

// dlCounts is how much the devlink rows checked.
type dlCounts struct{ requests, sections, json int }

func TestWsDevlinkCorpus(t *testing.T) {
	var c dlCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "devlink/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsDevlinkRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 checked %d devlink requests and %d decoded sections, O4 %d JSON replies",
		c.requests, c.sections, c.json)
	if c.requests == 0 || c.sections == 0 || c.json == 0 {
		t.Error("O3 or O4 checked nothing: are there devlink rows under testdata/*/ws?")
	}
}

func wsDevlinkRow(t *testing.T, dir string, row wsRow, c *dlCounts) {
	msgs := pcapNlMsgs(t, dir+"/ip.pcap")
	id, ok := devlinkGenlID(msgs)
	if !ok {
		t.Fatalf("%s: devlink family not resolved from capture", row.ID)
	}
	reqs, replies := dlMsgs(msgs, id)

	// The two negatives: no section is decoded either way.
	switch {
	case strings.Contains(row.Desc, "cmd=nosuch"):
		// iproute2 refuses before the wire: no request, no reply.
		if len(reqs) != 0 || len(replies) != 0 {
			t.Errorf("%s: nosuch reached the wire: %d reqs, %d replies", row.ID, len(reqs), len(replies))
		}
		c.requests++ // the checked (absent) request
		return
	case strings.Contains(row.Desc, "cmd=nodev"):
		// A targeted GET that ENODEVs: a request, but no decodable section.
		if len(reqs) == 0 {
			t.Errorf("%s: nodev sent no devlink request", row.ID)
		}
		for _, m := range reqs {
			if m.h.Flags&unix.NLM_F_DUMP != 0 {
				t.Errorf("%s: nodev GET should be targeted, not a dump", row.ID)
			}
		}
		c.requests++
		return
	}

	words := row.Axes["words"]
	exp, known := dlSections[words]
	if !known {
		t.Fatalf("%s: unmodeled words axis %q", row.ID, words)
	}

	// O3: the dump goes over the devlink family with NLM_F_DUMP.
	if len(reqs) == 0 {
		t.Fatalf("%s: no devlink request", row.ID)
	}
	dump := false
	for _, m := range reqs {
		if m.h.Flags&unix.NLM_F_DUMP != 0 {
			dump = true
		}
	}
	if !dump {
		t.Errorf("%s: no request carried NLM_F_DUMP", row.ID)
	}
	c.requests++

	// Decode every reply; collect the section matches and the identifiers.
	var ids []string
	sectionSeen := false
	for _, m := range replies {
		dm, err := ParseDevlink(m.body, m.h.Flags)
		if err != nil {
			t.Fatalf("%s: ParseDevlink: %v", row.ID, err)
		}
		if exp.present != nil && exp.present(dm) {
			sectionSeen = true
			if exp.ids != nil {
				ids = append(ids, exp.ids(dm)...)
			}
		}
	}

	if exp.jsonKey == "" {
		// Empty dump (sb show): no replies carry a section.
		if len(replies) != 0 {
			t.Errorf("%s: expected an empty dump, got %d replies", row.ID, len(replies))
		}
		return
	}
	if !sectionSeen {
		t.Fatalf("%s: no reply decoded to section %q", row.ID, exp.jsonKey)
	}
	c.sections++

	// O4: the -j stdout is keyed by the section and names every identifier.
	if row.Axes["json"] != "1" {
		return
	}
	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(out, &top); err != nil {
		t.Fatalf("%s: ip.out not JSON: %v", row.ID, err)
	}
	if _, ok := top[exp.jsonKey]; !ok {
		t.Errorf("%s: -j stdout has no %q key (keys %v)", row.ID, exp.jsonKey, topKeys(top))
	}
	for _, s := range ids {
		if s != "" && !strings.Contains(string(out), s) {
			t.Errorf("%s: identifier %q decoded but missing from -j stdout", row.ID, s)
		}
	}
	c.json++
}

func topKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
