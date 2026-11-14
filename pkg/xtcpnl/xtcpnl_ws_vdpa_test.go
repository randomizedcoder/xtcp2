package xtcpnl

// The vdpa rows' O3 and O4 (tools/nlcapgen matrix_vdpa.go, 2C). Each row runs
// in init_net (the family is netnsok false) against one vdpasim_net device,
// vdpa0, bound to virtio_vdpa.
//
// O3 holds each row's request to its command line, byte for byte: the row's
// own words are parsed into a VdpaKind, device and queue index, and
// BuildGetVdpaRequest (given the captured seq) must produce exactly the
// request vdpa put on the wire, over the family resolved from the capture's
// CTRL_CMD_NEWFAMILY reply. Each positive's replies must then decode through
// ParseVdpa into the one section its kind names, naming the device asked for.
// The negatives' errno/extack are TestWsCorpusRows' job; here a negative
// is held to its request (or, for `nosuch`, to sending none) and to
// decoding no section.
//
// O4 holds each `-j` row's stdout to the decoded reply: the JSON's top-level
// key is the section's, its one object is keyed by the decoded name, and the
// numbers vdpa prints from the reply (max vqs, queue sizes, MTU, MAC, every
// vendor counter by name) equal the decoded values.

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// vdReq is what a row's command line asks of the kernel: ok is false for a
// command vdpa refuses before sending a vdpa request (an unknown object).
type vdReq struct {
	ok   bool
	kind VdpaKind
	dev  string
	qidx *uint32
	json bool
}

// vdParseCmd parses a vdpa row's command into the request vdpa sends.
func vdParseCmd(cmd string) (vdReq, error) {
	var r vdReq
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "vdpa" {
		return r, fmt.Errorf("not a vdpa command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		r.json = r.json || f[0] == "-j"
		f = f[1:]
	}
	var rest []string
	switch {
	case len(f) >= 2 && f[0] == "mgmtdev" && f[1] == "show":
		r.kind, rest = VdpaMgmtdevKind, f[2:]
	case len(f) >= 2 && f[0] == "dev" && f[1] == "show":
		r.kind, rest = VdpaDevKind, f[2:]
	case len(f) >= 3 && f[0] == "dev" && f[1] == "config" && f[2] == "show":
		r.kind, rest = VdpaConfigKind, f[3:]
	case len(f) >= 3 && f[0] == "dev" && f[1] == "vstats" && f[2] == "show":
		r.kind, rest = VdpaVstatsKind, f[3:]
	default:
		return r, nil // vdpa's command table lacks it: no vdpa request.
	}
	r.ok = true
	if len(rest) > 0 {
		r.dev, rest = rest[0], rest[1:]
	}
	if len(rest) == 2 && rest[0] == "qidx" {
		q, err := strconv.ParseUint(rest[1], 10, 32)
		if err != nil {
			return r, err
		}
		q32 := uint32(q)
		r.qidx = &q32
	} else if len(rest) != 0 {
		return r, fmt.Errorf("unmodeled vdpa arguments %q", rest)
	}
	return r, nil
}

// TestVdpaParseCmd pins the command parser the corpus test relies on.
func TestVdpaParseCmd(t *testing.T) {
	rows := []struct {
		description string
		cmd         string
		expected    string // kind/dev/qidx/json, "none" for no request, "error"
	}{
		{"positive: a mgmtdev dump", "vdpa mgmtdev show", "1//-/false"},
		{"positive: a JSON dev dump", "vdpa -j dev show", "2//-/true"},
		{"positive: a named dev show", "vdpa dev show vdpa0", "2/vdpa0/-/false"},
		{"positive: a config dump", "vdpa -j dev config show", "3//-/true"},
		{"positive: vstats on a queue", "vdpa dev vstats show vdpa0 qidx 2", "4/vdpa0/2/false"},
		{"boundary: vstats on the largest u32 queue", "vdpa dev vstats show vdpa0 qidx 4294967295", "4/vdpa0/4294967295/false"},
		{"corner: vstats with no qidx", "vdpa dev vstats show vdpa0", "4/vdpa0/-/false"},
		{"negative: an unknown object sends no request", "vdpa nosuch show", "none"},
		{"negative: a qidx past u32", "vdpa dev vstats show vdpa0 qidx 4294967296", "error"},
		{"negative: not a vdpa command", "ip link", "error"},
		{"negative: a stray trailing word", "vdpa dev show vdpa0 extra words", "error"},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			got, err := vdParseCmd(r.cmd)
			var s string
			switch {
			case err != nil:
				s = "error"
			case !got.ok:
				s = "none"
			default:
				q := "-"
				if got.qidx != nil {
					q = strconv.FormatUint(uint64(*got.qidx), 10)
				}
				s = fmt.Sprintf("%d/%s/%s/%t", got.kind, got.dev, q, got.json)
			}
			if s != r.expected {
				t.Fatalf("got %q, want %q", s, r.expected)
			}
		})
	}
}

// vdpaGenlID is the vdpa family's id, from the CTRL_CMD_NEWFAMILY reply in
// msgs (modeled on devlinkGenlID).
func vdpaGenlID(msgs []capturedNlMsg) (uint16, bool) {
	for _, m := range msgs {
		if m.h.Type != ethGenlCtrl || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == VdpaGenlName {
			return f.ID, true
		}
	}
	return 0, false
}

// vdCounts is how much the vdpa rows checked.
type vdCounts struct{ requests, sections, json int }

func TestWsVdpaCorpus(t *testing.T) {
	var c vdCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "vdpa/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsVdpaRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 matched %d vdpa requests byte for byte and %d decoded sections, O4 %d JSON replies",
		c.requests, c.sections, c.json)
	if c.requests == 0 || c.sections == 0 || c.json == 0 {
		t.Error("O3 or O4 checked nothing: are there vdpa rows under testdata/*/ws?")
	}
}

func wsVdpaRow(t *testing.T, dir string, row wsRow, c *vdCounts) {
	want, err := vdParseCmd(row.Cmd)
	if err != nil {
		t.Fatalf("%s: %v", row.ID, err)
	}
	msgs := pcapNlMsgs(t, dir+"/ip.pcap")
	id, ok := vdpaGenlID(msgs)
	if !ok {
		t.Fatalf("%s: vdpa family not resolved from capture", row.ID)
	}
	reqs, replies := dlMsgs(msgs, id)

	if !want.ok {
		// The family was resolved (ack-then-fail), but no vdpa request.
		if len(reqs) != 0 || len(replies) != 0 {
			t.Errorf("%s: %d vdpa requests, %d replies, want none", row.ID, len(reqs), len(replies))
		}
		return
	}

	// O3: exactly one request, byte for byte what the builder makes.
	if len(reqs) != 1 {
		t.Fatalf("%s: %d vdpa requests, want 1", row.ID, len(reqs))
	}
	req := reqs[0]
	built, err := BuildGetVdpaRequest(id, want.kind, want.dev, want.qidx, req.h.Seq)
	if err != nil {
		t.Fatalf("%s: build: %v", row.ID, err)
	}
	gotHdr := fmt.Sprintf("len=%d type=%d flags=%#x pid=%d", req.h.Len, req.h.Type, req.h.Flags, req.h.Pid)
	var b NlMsgHdr
	if _, err := DeserializeNlMsgHdr(built, &b); err != nil {
		t.Fatal(err)
	}
	wantHdr := fmt.Sprintf("len=%d type=%d flags=%#x pid=%d", b.Len, b.Type, b.Flags, b.Pid)
	if gotHdr != wantHdr || string(req.body) != string(built[NlMsgHdrSizeCst:]) {
		t.Fatalf("%s: request\n got %s % x\nwant %s % x", row.ID, gotHdr, req.body, wantHdr, built[NlMsgHdrSizeCst:])
	}
	c.requests++

	var sections []VdpaMessage
	for _, m := range replies {
		vm, err := ParseVdpa(m.body, m.h.Flags)
		if err != nil {
			t.Fatalf("%s: ParseVdpa: %v", row.ID, err)
		}
		sections = append(sections, vm)
	}
	if row.Expect.RC == nil || *row.Expect.RC != 0 {
		if len(sections) != 0 {
			t.Errorf("%s: a refused request decoded %d replies", row.ID, len(sections))
		}
		return
	}
	if len(sections) == 0 {
		t.Fatalf("%s: no vdpa reply", row.ID)
	}
	for i := range sections {
		if err := vdSectionFor(want, &sections[i]); err != nil {
			t.Fatalf("%s: %v", row.ID, err)
		}
	}
	c.sections += len(sections)

	if !want.json {
		return
	}
	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	if err := vdJSONMatches(out, want.kind, sections); err != nil {
		t.Fatalf("%s: %v\n%s", row.ID, err, out)
	}
	c.json++
}

// vdSectionFor checks a reply decoded to the section its request's kind
// names, about the device (and queue) asked for.
func vdSectionFor(want vdReq, vm *VdpaMessage) error {
	var ok bool
	switch want.kind {
	case VdpaMgmtdevKind:
		ok = vm.Mgmtdev != nil && vm.MgmtdevDevName != ""
	case VdpaDevKind:
		ok = vm.Dev != nil
	case VdpaConfigKind:
		ok = vm.Config != nil
	case VdpaVstatsKind:
		ok = vm.Vstats != nil && want.qidx != nil && eqU32(vm.QueueIndex, *want.qidx)
	}
	if !ok {
		return fmt.Errorf("reply %+v is not the kind-%d section", vm, want.kind)
	}
	if want.dev != "" && vm.DevName != want.dev {
		return fmt.Errorf("reply names %q, asked for %q", vm.DevName, want.dev)
	}
	return nil
}

// vdJSONMatches checks vdpa's -j stdout against the decoded replies.
func vdJSONMatches(out []byte, kind VdpaKind, sections []VdpaMessage) error {
	var top map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(out, &top); err != nil {
		return fmt.Errorf("not the {section: {name: {...}}} shape: %w", err)
	}
	key := map[VdpaKind]string{VdpaMgmtdevKind: "mgmtdev", VdpaDevKind: "dev", VdpaConfigKind: "config", VdpaVstatsKind: "vstats"}[kind]
	objs, ok := top[key]
	if !ok || len(top) != 1 {
		return fmt.Errorf("top-level keys are not just %q", key)
	}
	if len(objs) != len(sections) {
		return fmt.Errorf("%d objects for %d replies", len(objs), len(sections))
	}
	for i := range sections {
		vm := &sections[i]
		name := vm.DevName
		if kind == VdpaMgmtdevKind {
			name = vm.MgmtdevDevName
		}
		o, ok := objs[name]
		if !ok {
			return fmt.Errorf("no %q object for the decoded name", name)
		}
		var nums map[string]uint64
		switch kind {
		case VdpaMgmtdevKind:
			nums = map[string]uint64{"max_supported_vqs": uint64(*vm.Mgmtdev.MaxVQs)}
		case VdpaDevKind:
			nums = map[string]uint64{"max_vqs": uint64(*vm.Dev.MaxVQs), "max_vq_size": uint64(*vm.Dev.MaxVQSize), "vendor_id": uint64(*vm.Dev.VendorID)}
		case VdpaConfigKind:
			nums = map[string]uint64{"mtu": uint64(*vm.Config.MTU)}
			var mac string
			if err := json.Unmarshal(o["mac"], &mac); err != nil || mac != net.HardwareAddr(vm.Config.MAC).String() {
				return fmt.Errorf("mac %s, decoded %s", o["mac"], net.HardwareAddr(vm.Config.MAC))
			}
		case VdpaVstatsKind:
			nums = map[string]uint64{}
			for _, s := range vm.Vstats.Stats {
				if s.Name != nil {
					nums[*s.Name] = s.Value
				}
			}
			if len(nums) == 0 {
				return fmt.Errorf("no named counters decoded")
			}
		}
		for k, v := range nums {
			raw, ok := o[k]
			if !ok {
				return fmt.Errorf("%q has no %q", name, k)
			}
			if got := string(raw); got != strconv.FormatUint(v, 10) {
				return fmt.Errorf("%q %q is %s, decoded %d", name, k, got, v)
			}
		}
	}
	return nil
}
