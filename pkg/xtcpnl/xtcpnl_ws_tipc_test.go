package xtcpnl

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// tipcWant is the TIPC request a tipc(8) command sends first, as tipc/*.c
// builds it, and the follow-up queries its replies lead to: "sock" is
// socket list's PUBL_GET for each socket with HAS_PUBL, "mon" is monitor
// list's MON_PEER_GET for each bearer's monitor. Each follow-up is preceded
// by its own family lookup (tipc opens a fresh genl socket for it).
type tipcWant struct {
	cmd    uint8
	flags  uint16
	top    uint16
	put    func(*AttrBuilder) error
	follow string
}

// tipcCmd is a parsed tipc command line: its options and the request it
// sends; want is nil for a command that sends no TIPC request (a refusal
// after the family lookup, or `node get address`, which tipc reads from a
// TIPC socket, not netlink).
type tipcCmd struct {
	json, pretty bool
	badOpt       bool
	want         *tipcWant
}

func tipcDump(cmd uint8) *tipcWant { return &tipcWant{cmd: cmd, flags: unix.NLM_F_DUMP} }

func tipcGet(cmd uint8, top uint16, put func(*AttrBuilder) error) *tipcWant {
	return &tipcWant{cmd: cmd, flags: unix.NLM_F_ACK, top: top, put: put}
}

func tipcName(id uint16, name string) func(*AttrBuilder) error {
	return func(ab *AttrBuilder) error { return ab.PutString(id, name) }
}

// tipcParseCmd models tipc's dispatch for the corpus's command lines: the
// options getopt takes, then the read-only commands, spelled out.
func tipcParseCmd(cmd string) (tipcCmd, error) {
	var c tipcCmd
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "tipc" {
		return c, fmt.Errorf("not a tipc command: %q", cmd)
	}
	f = f[1:]
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		switch f[0] {
		case "-j":
			c.json = true
		case "-p":
			c.pretty = true
		default:
			c.badOpt = true
		}
		f = f[1:]
	}
	if c.badOpt {
		return c, nil
	}
	words := strings.Join(f, " ")
	prop := func(rest string) bool {
		for _, p := range []string{"priority", "tolerance", "window", "mtu"} {
			if rest == p {
				return true
			}
		}
		return false
	}
	switch {
	case words == "node get identity", words == "node get netid":
		c.want = tipcDump(TIPCCmdNetGet)
	case words == "node list":
		c.want = tipcDump(TIPCCmdNodeGet)
	case words == "bearer list":
		c.want = tipcDump(TIPCCmdBearerGet)
	case words == "media list":
		c.want = tipcDump(TIPCCmdMediaGet)
	case words == "link list":
		c.want = tipcDump(TIPCCmdLinkGet)
	case words == "nametable show":
		c.want = tipcDump(TIPCCmdNameTableGet)
	case words == "link monitor summary":
		c.want = tipcDump(TIPCCmdMonGet)
	case words == "link monitor get threshold":
		c.want = tipcGet(TIPCCmdMonGet, 0, nil)
	case words == "link monitor list":
		c.want = tipcDump(TIPCCmdMonGet)
		c.want.follow = "mon"
	case words == "socket list":
		c.want = tipcDump(TIPCCmdSockGet)
		c.want.follow = "sock"
	case words == "link statistics show":
		c.want = tipcDump(TIPCCmdLinkGet)
	case strings.HasPrefix(words, "link statistics show link "):
		// A link filter is applied by tipc; the request only asks for the
		// broadcast links too.
		c.want = tipcDump(TIPCCmdLinkGet)
		c.want.top = TIPCAttrLink
		c.want.put = func(ab *AttrBuilder) error { return ab.PutBytes(TIPCLinkBroadcast, nil) }
	case words == "link get broadcast":
		c.want = tipcGet(TIPCCmdLinkGet, TIPCAttrLink, tipcName(TIPCLinkName, "broadcast-link"))
	}
	if c.want != nil {
		return c, nil
	}
	// The forms with a name: PROP then KEY VALUE pairs.
	switch {
	case len(f) == 5 && f[0] == "link" && f[1] == "get" && prop(f[2]) && f[2] != "mtu" && f[3] == "link":
		c.want = tipcGet(TIPCCmdLinkGet, TIPCAttrLink, tipcName(TIPCLinkName, f[4]))
	case len(f) == 7 && f[0] == "bearer" && f[1] == "get" && prop(f[2]) && f[3] == "media" &&
		f[4] == "eth" && f[5] == "device" && f[2] != "mtu":
		c.want = tipcGet(TIPCCmdBearerGet, TIPCAttrBearer, tipcName(TIPCBearerName, "eth:"+f[6]))
	case len(f) == 7 && f[0] == "bearer" && f[1] == "get" && prop(f[2]) && f[3] == "media" &&
		f[4] == "udp" && f[5] == "name":
		c.want = tipcGet(TIPCCmdBearerGet, TIPCAttrBearer, tipcName(TIPCBearerName, "udp:"+f[6]))
	case len(f) == 5 && f[0] == "media" && f[1] == "get" && prop(f[2]) && f[3] == "media" &&
		(f[2] != "mtu" || f[4] != "eth"):
		c.want = tipcGet(TIPCCmdMediaGet, TIPCAttrMedia, tipcName(TIPCMediaName, f[4]))
	}
	return c, nil
}

// TestTipcParseCmd pins the request model the corpus test holds tipc to.
//
// go test ./pkg/xtcpnl/ -run TestTipcParseCmd
func TestTipcParseCmd(t *testing.T) {
	tests := []struct {
		description string
		cmd         string
		expected    string // "none", "badopt", or cmd/flags/top/follow
	}{
		{description: "positive: node list is a NODE_GET dump", cmd: "tipc node list", expected: "13/0x300/0/"},
		{description: "positive: node get identity is a NET_GET dump", cmd: "tipc node get identity", expected: "14/0x300/0/"},
		{description: "positive: -j changes no request", cmd: "tipc -j nametable show", expected: "16/0x300/0/"},
		{description: "positive: a bearer by device is a BEARER_GET get", cmd: "tipc bearer get window media eth device d0",
			expected: "4/0x4/1/"},
		{description: "positive: a udp bearer by name", cmd: "tipc bearer get mtu media udp name u0", expected: "4/0x4/1/"},
		{description: "positive: a media get", cmd: "tipc media get priority media eth", expected: "11/0x4/5/"},
		{description: "positive: a link get by name", cmd: "tipc link get priority link 1001:v0-1002:v1", expected: "8/0x4/4/"},
		{description: "positive: socket list follows with PUBL_GET", cmd: "tipc socket list", expected: "6/0x300/0/sock"},
		{description: "positive: monitor list follows with MON_PEER_GET", cmd: "tipc link monitor list", expected: "18/0x300/0/mon"},
		{description: "corner: link get broadcast asks for broadcast-link by name", cmd: "tipc link get broadcast",
			expected: "8/0x4/4/"},
		{description: "corner: a stats filter asks the dump for broadcast links too", cmd: "tipc link statistics show link x",
			expected: "8/0x300/4/"},
		{description: "corner: monitor get threshold is a get with no attribute", cmd: "tipc link monitor get threshold",
			expected: "18/0x4/0/"},
		{description: "corner: node get address sends no TIPC request", cmd: "tipc node get address", expected: "none"},
		{description: "negative: mtu is refused for eth before the wire", cmd: "tipc bearer get mtu media eth device d0",
			expected: "none"},
		{description: "negative: a link get with no name", cmd: "tipc link get priority", expected: "none"},
		{description: "negative: an unknown object", cmd: "tipc badobj", expected: "none"},
		{description: "boundary: no object", cmd: "tipc", expected: "none"},
		{description: "negative: an unknown option", cmd: "tipc -z node get address", expected: "badopt"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c, err := tipcParseCmd(tc.cmd)
			if err != nil {
				t.Fatal(err)
			}
			got := "none"
			switch {
			case c.badOpt:
				got = "badopt"
			case c.want != nil:
				got = fmt.Sprintf("%d/%#x/%d/%s", c.want.cmd, c.want.flags, c.want.top, c.want.follow)
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// tipcCounts is how much the tipc rows checked.
type tipcCounts struct{ requests, follows, replies, json int }

// TestWsTipcCorpus holds the tipc ws rows to the request model and the
// parser (genl-clones 5P): O3, every request tipc sent, the family lookups
// included, is rebuilt byte for byte (seq aside), in order, the follow-up
// queries from the replies they follow; every reply decodes; O4, each -j
// link list and nametable show prints its replies' fields.
//
// go test ./pkg/xtcpnl/ -run TestWsTipcCorpus
func TestWsTipcCorpus(t *testing.T) {
	var c tipcCounts
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "tipc/") {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			wsTipcRow(t, dir, row, &c)
		})
	}
	t.Logf("O3 rebuilt %d requests (%d follow-ups), %d replies decoded, O4 %d JSON outputs",
		c.requests, c.follows, c.replies, c.json)
	if c.requests < 93 || c.follows == 0 || c.replies == 0 || c.json == 0 {
		t.Errorf("checked too little: %+v", c)
	}
}

func tipcSameRequest(got capturedNlMsg, built []byte) error {
	var b NlMsgHdr
	if _, err := DeserializeNlMsgHdr(built, &b); err != nil {
		return err
	}
	if got.h.Len != b.Len || got.h.Type != b.Type || got.h.Flags != b.Flags ||
		string(got.body) != string(built[NlMsgHdrSizeCst:]) {
		return fmt.Errorf("request\n got len=%d type=%d flags=%#x % x\nwant len=%d type=%d flags=%#x % x",
			got.h.Len, got.h.Type, got.h.Flags, got.body, b.Len, b.Type, b.Flags, built[NlMsgHdrSizeCst:])
	}
	return nil
}

func wsTipcRow(t *testing.T, dir string, row wsRow, c *tipcCounts) {
	cmd, err := tipcParseCmd(row.Cmd)
	if err != nil {
		t.Fatal(err)
	}
	msgs := pcapNlMsgs(t, dir+"/ip.pcap")
	if cmd.badOpt {
		if len(msgs) != 0 {
			t.Fatalf("%s: %d messages, want none before the family lookup", row.ID, len(msgs))
		}
		return
	}
	id := uint16(0)
	var reqs []capturedNlMsg
	var replies []TIPCMessage
	for _, m := range msgs {
		if m.h.Type == ethGenlCtrl && m.h.Flags&unix.NLM_F_REQUEST == 0 {
			if f, err := ParseGenericNetlinkFamily(m.body); err == nil && f.Name == TIPCGenlName {
				id = f.ID
			}
		}
	}
	if id == 0 {
		t.Fatalf("%s: TIPCv2 not resolved from the capture", row.ID)
	}
	for _, m := range msgs {
		switch {
		case m.h.Flags&unix.NLM_F_REQUEST != 0:
			reqs = append(reqs, m)
		case m.h.Type == id:
			tm, err := ParseTIPC(m.body, m.h.Flags)
			if err != nil {
				t.Fatalf("%s: ParseTIPC: %v", row.ID, err)
			}
			replies = append(replies, tm)
		}
	}
	c.replies += len(replies)

	// The expected requests: the lookup, the command's request, then a
	// lookup and a follow-up for each socket or monitor its replies name.
	type exp struct {
		build  func(seq uint32) ([]byte, error)
		follow bool
	}
	want := []exp{{build: BuildTIPCFamilyRequest}}
	if w := cmd.want; w != nil {
		want = append(want, exp{build: func(seq uint32) ([]byte, error) {
			return BuildTIPCRequest(id, w.cmd, w.flags, seq, w.top, w.put)
		}})
		for _, r := range replies {
			var ref *uint32
			var fcmd uint8
			var top uint16
			switch {
			case w.follow == "sock" && r.Sock != nil && r.Sock.HasPubl:
				ref, fcmd, top = r.Sock.Ref, TIPCCmdPublGet, TIPCAttrSock
			case w.follow == "mon" && r.Mon != nil:
				ref, fcmd, top = r.Mon.Ref, TIPCCmdMonPeerGet, TIPCAttrMon
			default:
				continue
			}
			if ref == nil {
				t.Fatalf("%s: a reply to follow up names no ref", row.ID)
			}
			refID := map[uint16]uint16{TIPCAttrSock: TIPCSockRef, TIPCAttrMon: TIPCMonRef}[top]
			v := *ref
			want = append(want, exp{build: BuildTIPCFamilyRequest, follow: true}, exp{follow: true,
				build: func(seq uint32) ([]byte, error) {
					return BuildTIPCRequest(id, fcmd, unix.NLM_F_DUMP, seq, top,
						func(ab *AttrBuilder) error { return ab.PutU32(refID, v) })
				}})
		}
	}
	if len(reqs) != len(want) {
		t.Fatalf("%s: %d requests, want %d", row.ID, len(reqs), len(want))
	}
	for i, e := range want {
		built, err := e.build(reqs[i].h.Seq)
		if err != nil {
			t.Fatalf("%s: build %d: %v", row.ID, i, err)
		}
		if err := tipcSameRequest(reqs[i], built); err != nil {
			t.Fatalf("%s: request %d: %v", row.ID, i, err)
		}
		c.requests++
		if e.follow && i%2 == 1 {
			c.follows++
		}
	}

	if !cmd.json || cmd.want == nil || row.Expect.RC == nil || *row.Expect.RC != 0 {
		return
	}
	out, err := os.ReadFile(dir + "/ip.out")
	if err != nil {
		t.Fatal(err)
	}
	var checked bool
	switch cmd.want.cmd {
	case TIPCCmdLinkGet:
		if cmd.want.flags != unix.NLM_F_DUMP || cmd.want.top != 0 {
			return // link list only
		}
		err, checked = tipcLinkListJSON(out, replies), true
	case TIPCCmdNameTableGet:
		err, checked = tipcNameTableJSON(out, replies), true
	}
	if err != nil {
		t.Fatalf("%s: %v\n%s", row.ID, err, out)
	}
	if checked {
		c.json++
	}
}

// tipcLinkListJSON: `tipc -j link list` prints ONE object holding each
// link as a key, NAME: "up"|"down", in the dump's order (link.c,
// cmd_link_list opens the object, link_list_cb adds a key per link).
func tipcLinkListJSON(out []byte, replies []TIPCMessage) error {
	var got []map[string]string
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	if len(got) != 1 || len(got[0]) != len(replies) {
		return fmt.Errorf("%d objects, the first of %d keys, for %d links", len(got), len(got[0]), len(replies))
	}
	for i, r := range replies {
		if r.Link == nil || r.Link.Name == nil {
			return fmt.Errorf("reply %d has no link name", i)
		}
		state := "down"
		if r.Link.Up {
			state = "up"
		}
		if got[0][*r.Link.Name] != state {
			return fmt.Errorf("key %q is %q, want %q", *r.Link.Name, got[0][*r.Link.Name], state)
		}
	}
	return nil
}

// tipcNameTableJSON: `tipc -j nametable show` prints one object per
// publication, its scope by tipc's name for it (nametable.c,
// nametable_show_cb).
func tipcNameTableJSON(out []byte, replies []TIPCMessage) error {
	var got []struct {
		Type, Lower, Upper, Port uint32
		Scope, Node              string
	}
	if err := json.Unmarshal(out, &got); err != nil {
		return err
	}
	if len(got) != len(replies) {
		return fmt.Errorf("%d objects for %d publications", len(got), len(replies))
	}
	scope := []string{"", "zone", "cluster", "node"}
	for i, r := range replies {
		p := r.NameTable
		if p == nil || p.Type == nil || p.Lower == nil || p.Upper == nil || p.Ref == nil || p.Scope == nil ||
			int(*p.Scope) >= len(scope) {
			return fmt.Errorf("reply %d is no whole publication", i)
		}
		g := got[i]
		if g.Type != *p.Type || g.Lower != *p.Lower || g.Upper != *p.Upper || g.Port != *p.Ref || g.Scope != scope[*p.Scope] {
			return fmt.Errorf("object %d is %+v, want %s", i, g, tipcPublSum(p))
		}
	}
	return nil
}
