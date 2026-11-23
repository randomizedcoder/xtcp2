package xtcpnl

// The fou rows' O3 and O4 (tools/nlcapgen matrix_fou.go, 3C). Each row runs
// in its own fresh namespace (the family is netnsok true, per-netns) with
// its own `ip fou add` setup, so `ip fou show` dumps only that namespace's
// ports.
//
// O3 holds each `ip fou show` row's request to its command line, byte for
// byte: BuildGetFOURequest (given the captured seq) must produce exactly the
// FOU_CMD_GET dump ip put on the wire, over the family resolved from the
// capture's CTRL_CMD_NEWFAMILY reply. Each reply then decodes through
// ParseFOU into one FOU_CMD_GET mapping with a port. A row that sends no fou
// request (help, an unknown object, `show` with a stray arg) is held to
// sending none.
//
// O4 holds each `-j` row's stdout to the decoded replies: the JSON is an
// array, one object per mapping in dump order, and the port, gue/ipproto and
// family each object prints equal the decoded values.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// fouParseCmd reports whether a fou row's command sends a FOU_CMD_GET dump,
// and whether it asked for JSON. Only `ip [-j] fou show` with nothing after
// "show" sends a dump; `help`, an unknown object, and `show junk` send no
// fou request (ip refuses them, two of them after resolving the family).
func fouParseCmd(cmd string) (sendsGet, jsonOut bool, err error) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "ip" {
		return false, false, fmt.Errorf("not an ip command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		jsonOut = jsonOut || f[0] == "-j"
		f = f[1:]
	}
	if len(f) == 0 || f[0] != "fou" {
		return false, false, fmt.Errorf("not an ip fou command: %q", cmd)
	}
	f = f[1:]
	sendsGet = len(f) == 1 && f[0] == "show"
	return sendsGet, jsonOut, nil
}

// TestFouParseCmd pins the command classifier the corpus test relies on.
func TestFouParseCmd(t *testing.T) {
	rows := []struct {
		description string
		cmd         string
		expected    string // sendsGet/json, or "error"
	}{
		{"positive: a plain show dumps", "ip fou show", "true/false"},
		{"positive: a json show dumps", "ip -j fou show", "true/true"},
		{"negative: help sends nothing", "ip fou help", "false/false"},
		{"negative: an unknown object sends nothing", "ip fou badobj", "false/false"},
		{"negative: show with a stray arg sends nothing", "ip fou show junk", "false/false"},
		{"corner: -j before a non-show is still json", "ip -j fou help", "false/true"},
		{"negative: not an ip command", "vdpa dev show", "error"},
		{"negative: not an ip fou command", "ip link show", "error"},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			sendsGet, jsonOut, err := fouParseCmd(r.cmd)
			s := "error"
			if err == nil {
				s = fmt.Sprintf("%t/%t", sendsGet, jsonOut)
			}
			if s != r.expected {
				t.Fatalf("got %q, want %q", s, r.expected)
			}
		})
	}
}

// fouGenlID is the fou family's id, from the CTRL_CMD_NEWFAMILY reply in
// msgs (modeled on vdpaGenlID).
func fouGenlID(msgs []capturedNlMsg) (uint16, bool) {
	for _, m := range msgs {
		if m.h.Type != ethGenlCtrl || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == FOUGenlName {
			return f.ID, true
		}
	}
	return 0, false
}

// fouCounts is how much the fou rows checked.
type fouCounts struct{ requests, replies, json int }

func TestWsFouCorpus(t *testing.T) {
	var c fouCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "fou/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsFouRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 matched %d fou requests byte for byte and decoded %d replies, O4 %d JSON dumps",
		c.requests, c.replies, c.json)
	if c.requests == 0 || c.replies == 0 || c.json == 0 {
		t.Error("O3 or O4 checked nothing: are there fou rows under testdata/*/ws?")
	}
}

func wsFouRow(t *testing.T, dir string, row wsRow, c *fouCounts) {
	sendsGet, jsonOut, err := fouParseCmd(row.Cmd)
	if err != nil {
		t.Fatalf("%s: %v", row.ID, err)
	}
	msgs := pcapNlMsgs(t, dir+"/ip.pcap")
	id, resolved := fouGenlID(msgs)

	if !sendsGet {
		// Either no wire at all (help) or the family was resolved then
		// the command was refused (show junk, badobj): no fou request.
		if resolved {
			reqs, replies := dlMsgs(msgs, id)
			if len(reqs) != 0 || len(replies) != 0 {
				t.Errorf("%s: %d fou requests, %d replies, want none", row.ID, len(reqs), len(replies))
			}
		}
		return
	}
	if !resolved {
		t.Fatalf("%s: fou family not resolved from capture", row.ID)
	}
	reqs, replies := dlMsgs(msgs, id)

	// O3: exactly one request, byte for byte what the builder makes.
	if len(reqs) != 1 {
		t.Fatalf("%s: %d fou requests, want 1", row.ID, len(reqs))
	}
	req := reqs[0]
	built, err := BuildGetFOURequest(id, req.h.Seq)
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

	// Every reply is a FOU_CMD_GET mapping with a port. An empty dump has
	// none, which is fine (the empty fixture).
	var maps []FOU
	for _, m := range replies {
		fm, err := ParseFOU(m.body)
		if err != nil {
			t.Fatalf("%s: ParseFOU: %v", row.ID, err)
		}
		if fm.Command != FOUCmdGet {
			t.Fatalf("%s: reply command %d, want %d", row.ID, fm.Command, FOUCmdGet)
		}
		if fm.Port == nil {
			t.Fatalf("%s: a fou reply with no port", row.ID)
		}
		maps = append(maps, fm)
	}
	c.replies += len(maps)

	if !jsonOut {
		return
	}
	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	if err := fouJSONMatches(out, maps); err != nil {
		t.Fatalf("%s: %v\n%s", row.ID, err, out)
	}
	c.json++
}

// fouJSONMatches checks `ip -j fou show`'s stdout against the decoded
// mappings, positionally (the JSON array is in the kernel's dump order,
// which is the order the replies decoded in).
func fouJSONMatches(out []byte, maps []FOU) error {
	var arr []map[string]json.RawMessage
	if err := json.Unmarshal(out, &arr); err != nil {
		return fmt.Errorf("not a JSON array: %w", err)
	}
	if len(arr) != len(maps) {
		return fmt.Errorf("%d JSON objects for %d replies", len(arr), len(maps))
	}
	for i, o := range arr {
		fm := maps[i]
		raw, ok := o["port"]
		if !ok || fm.Port == nil || string(raw) != strconv.FormatUint(uint64(*fm.Port), 10) {
			return fmt.Errorf("object %d port %s, decoded %v", i, raw, fm.Port)
		}
		// ip prints "gue":null for a GUE encap, else "ipproto":N.
		if fm.Type != nil && *fm.Type == FOUEncapGUE {
			if _, ok := o["gue"]; !ok {
				return fmt.Errorf("object %d: a GUE reply has no gue key", i)
			}
		} else if fm.IPProto != nil {
			raw, ok := o["ipproto"]
			if !ok || string(raw) != strconv.FormatUint(uint64(*fm.IPProto), 10) {
				return fmt.Errorf("object %d ipproto %s, decoded %v", i, raw, fm.IPProto)
			}
		}
		if fm.AF != nil {
			raw, ok := o["family"]
			if !ok {
				return fmt.Errorf("object %d has no family", i)
			}
			var fam string
			if err := json.Unmarshal(raw, &fam); err != nil {
				return err
			}
			want := map[uint8]string{unix.AF_INET: "inet", unix.AF_INET6: "inet6"}[*fm.AF]
			if fam != want {
				return fmt.Errorf("object %d family %q, decoded af %d (%q)", i, fam, *fm.AF, want)
			}
		}
	}
	return nil
}
