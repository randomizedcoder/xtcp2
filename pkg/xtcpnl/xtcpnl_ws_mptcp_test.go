package xtcpnl

// The mptcp rows' O3 and O4 (tools/nlcapgen matrix_mptcp.go, genl-clones
// 4C). Each row runs in its own fresh namespace (mptcp_pm is netnsok true,
// its endpoints and limits per-netns) and captures generic+route: the
// mptcp_pm exchange and, for an endpoint show that prints, the RTM_GETLINK
// dump ip's ll_init_map made to name a dev.
//
// O3 holds each row's mptcp_pm request to its command line, byte for byte:
// BuildGenlRequest (given the captured seq) must produce exactly what ip
// put on the wire, over the family resolved from the capture's
// CTRL_CMD_NEWFAMILY reply: a GET_ADDR dump for `endpoint show`, a GET_ADDR
// whose nested MPTCP_PM_ATTR_ADDR holds only the id for `endpoint show id
// N`, and a GET_LIMITS for `limits show` (with MPTCP_PM_ATTR_SUBFLOWS when
// a set's word is spelled into it). Each answer decodes through
// ParseMPTCPPM. A row that sends no mptcp_pm request is held to sending
// none.
//
// O4 holds stdout to the decoded answers: a -j endpoint show is an array,
// one object per endpoint in dump order, whose address, port, id, flags
// and dev equal the decoded ones (dev by the link dump's name for the
// endpoint's ifindex); a limits show is text, under -j too
// (ipmptcp.c's limits path never opens a JSON object), whose two numbers
// equal the decoded limits.

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The mptcp_pm request a row's command sends.
const (
	mptcpReqNone   = "none"
	mptcpReqDump   = "dump"   // GET_ADDR, NLM_F_DUMP
	mptcpReqGet    = "get"    // GET_ADDR by id
	mptcpReqLimits = "limits" // GET_LIMITS
)

// mptcpCmd is what a row's command line asks for.
type mptcpCmd struct {
	req      string
	json     bool
	id       uint8   // the get's id
	subflows *uint32 // a limits show's stray `subflows N`
}

// mptcpParseCmd classifies an `ip [-j] mptcp ...` row command. Only the
// forms the space captures succeed are a request: `endpoint [show]`,
// `endpoint show id N` (N from 1 to 255), `limits [show]` and `limits show
// subflows N`; every other spelling ip refuses before it sends one.
func mptcpParseCmd(cmd string) (mptcpCmd, error) {
	var c mptcpCmd
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "ip" {
		return c, fmt.Errorf("not an ip command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		c.json = c.json || f[0] == "-j"
		f = f[1:]
	}
	if len(f) == 0 || f[0] != "mptcp" {
		return c, fmt.Errorf("not an ip mptcp command: %q", cmd)
	}
	words := strings.Join(f[1:], " ")
	c.req = mptcpReqNone
	switch {
	case words == "endpoint" || words == "endpoint show":
		c.req = mptcpReqDump
	case strings.HasPrefix(words, "endpoint show id "):
		n, err := strconv.ParseUint(strings.TrimPrefix(words, "endpoint show id "), 10, 8)
		if err == nil && n != 0 {
			c.req, c.id = mptcpReqGet, uint8(n)
		}
	case words == "limits" || words == "limits show":
		c.req = mptcpReqLimits
	case strings.HasPrefix(words, "limits show subflows "):
		n, err := strconv.ParseUint(strings.TrimPrefix(words, "limits show subflows "), 10, 32)
		if err == nil {
			v := uint32(n)
			c.req, c.subflows = mptcpReqLimits, &v
		}
	}
	return c, nil
}

// TestMptcpParseCmd pins the command classifier the corpus test relies on.
func TestMptcpParseCmd(t *testing.T) {
	rows := []struct {
		description string
		cmd         string
		expected    string // req/json/id/subflows, or "error"
	}{
		{"positive: endpoint show dumps", "ip mptcp endpoint show", "dump/false/0/-"},
		{"positive: -j endpoint show dumps", "ip -j mptcp endpoint show", "dump/true/0/-"},
		{"corner: bare endpoint dumps", "ip mptcp endpoint", "dump/false/0/-"},
		{"positive: show id gets", "ip mptcp endpoint show id 2", "get/false/2/-"},
		{"boundary: id 255 gets", "ip mptcp endpoint show id 255", "get/false/255/-"},
		{"boundary: id 0 sends nothing", "ip mptcp endpoint show id 0", "none/false/0/-"},
		{"boundary: id 256 sends nothing", "ip mptcp endpoint show id 256", "none/false/0/-"},
		{"positive: limits show", "ip mptcp limits show", "limits/false/0/-"},
		{"corner: bare limits", "ip -j mptcp limits", "limits/true/0/-"},
		{"corner: a set's word in limits show", "ip mptcp limits show subflows 3", "limits/false/0/3"},
		{"negative: help sends nothing", "ip mptcp help", "none/false/0/-"},
		{"negative: show junk sends nothing", "ip mptcp endpoint show junk", "none/false/0/-"},
		{"negative: limits junk sends nothing", "ip mptcp limits junk", "none/false/0/-"},
		{"negative: not an ip command", "vdpa dev show", "error"},
		{"negative: not an ip mptcp command", "ip fou show", "error"},
	}
	for _, r := range rows {
		t.Run(r.description, func(t *testing.T) {
			c, err := mptcpParseCmd(r.cmd)
			s := "error"
			if err == nil {
				sub := "-"
				if c.subflows != nil {
					sub = strconv.FormatUint(uint64(*c.subflows), 10)
				}
				s = fmt.Sprintf("%s/%t/%d/%s", c.req, c.json, c.id, sub)
			}
			if s != r.expected {
				t.Fatalf("got %q, want %q", s, r.expected)
			}
		})
	}
}

// mptcpGenlMsgs is the capture's generic netlink messages alone: a
// generic+route capture also holds the link dump, whose RTM_NEWLINK (16)
// is GENL_ID_CTRL's number.
func mptcpGenlMsgs(msgs []capturedNlMsg) []capturedNlMsg {
	var out []capturedNlMsg
	for _, m := range msgs {
		if m.proto == unix.NETLINK_GENERIC {
			out = append(out, m)
		}
	}
	return out
}

// mptcpGenlID is mptcp_pm's id, from the CTRL_CMD_NEWFAMILY reply in msgs.
func mptcpGenlID(msgs []capturedNlMsg) (uint16, bool) {
	for _, m := range msgs {
		if m.h.Type != ethGenlCtrl || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == MPTCPPMGenlName {
			return f.ID, true
		}
	}
	return 0, false
}

// mptcpLinkNames is the capture's link dump, ifindex to name.
func mptcpLinkNames(t *testing.T, msgs []capturedNlMsg) map[int32]string {
	t.Helper()
	names := map[int32]string{}
	for _, m := range msgs {
		if m.proto != unix.NETLINK_ROUTE || m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		li, err := ParseNewLink(m.body)
		if err != nil {
			t.Fatalf("ParseNewLink: %v", err)
		}
		names[li.Index] = li.Name
	}
	return names
}

// mptcpRequest is what ip should have sent for c, at seq.
func mptcpRequest(c mptcpCmd, id uint16, seq uint32) ([]byte, error) {
	switch c.req {
	case mptcpReqDump:
		return BuildGenlRequest(MPTCPPMGenlName, id, MPTCPPMCmdGetAddr, MPTCPPMGenlVersion, unix.NLM_F_DUMP, seq, nil)
	case mptcpReqGet:
		var nraw, raw [64]byte
		n := NewAttrBuilder(nraw[:])
		if err := n.PutU8(MPTCPAddrAttrID, c.id); err != nil {
			return nil, err
		}
		b := NewAttrBuilder(raw[:])
		if err := b.PutBytes(MPTCPPMAttrAddr|unix.NLA_F_NESTED, n.Bytes()); err != nil {
			return nil, err
		}
		return BuildGenlRequest(MPTCPPMGenlName, id, MPTCPPMCmdGetAddr, MPTCPPMGenlVersion, 0, seq, b.Bytes())
	case mptcpReqLimits:
		var attrs []byte
		if c.subflows != nil {
			var raw [16]byte
			b := NewAttrBuilder(raw[:])
			if err := b.PutU32(MPTCPPMAttrSubflows, *c.subflows); err != nil {
				return nil, err
			}
			attrs = b.Bytes()
		}
		return BuildGenlRequest(MPTCPPMGenlName, id, MPTCPPMCmdGetLimits, MPTCPPMGenlVersion, 0, seq, attrs)
	}
	return nil, fmt.Errorf("no request for %q", c.req)
}

// mptcpCounts is how much the mptcp rows checked.
type mptcpCounts struct{ requests, endpoints, limits, json, text int }

func TestWsMptcpCorpus(t *testing.T) {
	var c mptcpCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "mptcp/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsMptcpRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 matched %d mptcp_pm requests byte for byte, decoding %d endpoints and %d limits; O4 %d JSON and %d text outputs",
		c.requests, c.endpoints, c.limits, c.json, c.text)
	if c.requests == 0 || c.endpoints == 0 || c.limits == 0 || c.json == 0 || c.text == 0 {
		t.Error("O3 or O4 checked nothing: are there mptcp rows under testdata/*/ws?")
	}
}

func wsMptcpRow(t *testing.T, dir string, row wsRow, c *mptcpCounts) {
	cmd, err := mptcpParseCmd(row.Cmd)
	if err != nil {
		t.Fatalf("%s: %v", row.ID, err)
	}
	all := pcapNlMsgs(t, dir+"/ip.pcap")
	msgs := mptcpGenlMsgs(all)
	id, resolved := mptcpGenlID(msgs)
	var reqs, replies []capturedNlMsg
	if resolved {
		reqs, replies = dlMsgs(msgs, id)
	}
	if cmd.req == mptcpReqNone {
		// No wire at all (help) or the family resolved and the command
		// refused: no mptcp_pm request.
		if len(reqs) != 0 || len(replies) != 0 {
			t.Errorf("%s: %d mptcp_pm requests, %d replies, want none", row.ID, len(reqs), len(replies))
		}
		return
	}
	if !resolved {
		t.Fatalf("%s: mptcp_pm not resolved from the capture", row.ID)
	}

	// O3: exactly one request, byte for byte what the builder makes.
	if len(reqs) != 1 {
		t.Fatalf("%s: %d mptcp_pm requests, want 1", row.ID, len(reqs))
	}
	req := reqs[0]
	built, err := mptcpRequest(cmd, id, req.h.Seq)
	if err != nil {
		t.Fatalf("%s: build: %v", row.ID, err)
	}
	var b NlMsgHdr
	if _, err := DeserializeNlMsgHdr(built, &b); err != nil {
		t.Fatal(err)
	}
	gotHdr := fmt.Sprintf("len=%d type=%d flags=%#x pid=%d", req.h.Len, req.h.Type, req.h.Flags, req.h.Pid)
	wantHdr := fmt.Sprintf("len=%d type=%d flags=%#x pid=%d", b.Len, b.Type, b.Flags, b.Pid)
	if gotHdr != wantHdr || string(req.body) != string(built[NlMsgHdrSizeCst:]) {
		t.Fatalf("%s: request\n got %s % x\nwant %s % x", row.ID, gotHdr, req.body, wantHdr, built[NlMsgHdrSizeCst:])
	}
	c.requests++

	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	if row.Result.RC == nil || *row.Result.RC != 0 {
		// The kernel refused (no endpoint with that id): an NLMSG_ERROR,
		// no answer, nothing printed.
		if len(replies) != 0 || len(out) != 0 {
			t.Errorf("%s: a refused request with %d answers and stdout %q", row.ID, len(replies), out)
		}
		return
	}

	if cmd.req == mptcpReqLimits {
		if len(replies) != 1 {
			t.Fatalf("%s: %d GET_LIMITS answers, want 1", row.ID, len(replies))
		}
		pm, err := ParseMPTCPPM(replies[0].body)
		if err != nil {
			t.Fatalf("%s: ParseMPTCPPM: %v", row.ID, err)
		}
		if pm.Command != MPTCPPMCmdGetLimits || pm.RcvAddAddrs == nil || pm.Subflows == nil {
			t.Fatalf("%s: answer cmd %d add_addr %v subflows %v", row.ID, pm.Command, pm.RcvAddAddrs, pm.Subflows)
		}
		c.limits++
		// Text under -j too.
		want := fmt.Sprintf("add_addr_accepted %d subflows %d \n", *pm.RcvAddAddrs, *pm.Subflows)
		if string(out) != want {
			t.Fatalf("%s: stdout %q, decoded %q", row.ID, out, want)
		}
		c.text++
		return
	}

	eps := make([]MPTCPAddr, 0, len(replies))
	for _, m := range replies {
		pm, err := ParseMPTCPPM(m.body)
		if err != nil {
			t.Fatalf("%s: ParseMPTCPPM: %v", row.ID, err)
		}
		if pm.Command != MPTCPPMCmdGetAddr || pm.Addr == nil || pm.Addr.ID == nil || pm.Addr.Flags == nil ||
			pm.Addr.Family == nil || !pm.Addr.Addr.IsValid() {
			t.Fatalf("%s: an answer without its endpoint: cmd %d addr %+v", row.ID, pm.Command, pm.Addr)
		}
		eps = append(eps, *pm.Addr)
	}
	if cmd.req == mptcpReqGet && len(eps) != 1 {
		t.Fatalf("%s: %d answers to a get, want 1", row.ID, len(eps))
	}
	c.endpoints += len(eps)
	if !cmd.json {
		return
	}
	if err := mptcpJSONMatches(out, eps, mptcpLinkNames(t, all)); err != nil {
		t.Fatalf("%s: %v\n%s", row.ID, err, out)
	}
	c.json++
}

// mptcpFlagNames are print_mptcp_addr_flags' keys, in its table order.
var mptcpFlagNames = []struct {
	flag uint32
	name string
}{
	{MPTCPAddrFlagSignal, "signal"},
	{MPTCPAddrFlagSubflow, "subflow"},
	{MPTCPAddrFlagBackup, "backup"},
	{MPTCPAddrFlagFullmesh, "fullmesh"},
	{MPTCPAddrFlagImplicit, "implicit"},
	{MPTCPAddrFlagLaminar, "laminar"},
}

// mptcpJSONMatches checks `ip -j mptcp endpoint show`'s stdout against
// the decoded endpoints, positionally (the array is in dump order): the
// keys each object holds, in ip's order, and their values.
func mptcpJSONMatches(out []byte, eps []MPTCPAddr, links map[int32]string) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(out, &arr); err != nil {
		return fmt.Errorf("not a JSON array: %w", err)
	}
	if len(arr) != len(eps) {
		return fmt.Errorf("%d JSON objects for %d answers", len(arr), len(eps))
	}
	for i, raw := range arr {
		ep := eps[i]
		want := fmt.Sprintf(`{"address":%q`, ep.Addr.String())
		if ep.Port != nil && *ep.Port != 0 {
			want += fmt.Sprintf(`,"port":%d`, *ep.Port)
		}
		want += fmt.Sprintf(`,"id":%d`, *ep.ID)
		for _, f := range mptcpFlagNames {
			if *ep.Flags&f.flag != 0 {
				want += fmt.Sprintf(`,%q:true`, f.name)
			}
		}
		if ep.IfIndex != nil && *ep.IfIndex != 0 {
			name, ok := links[*ep.IfIndex]
			if !ok {
				return fmt.Errorf("object %d: ifindex %d is not in the link dump", i, *ep.IfIndex)
			}
			want += fmt.Sprintf(`,"dev":%q`, name)
		}
		want += "}"
		if string(raw) != want {
			return fmt.Errorf("object %d\n got %s\nwant %s", i, raw, want)
		}
	}
	return nil
}
