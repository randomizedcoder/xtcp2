package xtcpnl

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// The linkns rows' oracles: `ip link set dev d0 netns …` in each form
// netns_get_fd takes, and one `ip link add … netns $$ index 100`
// (tools/nlcapgen/matrix_linkattr.go, linkNSSpace). Every form sends the
// descriptor ip opened as IFLA_NET_NS_FD, first; a set sends nothing else
// but a link-netnsid, and a create puts its index in ifi_index.

// linkNSRequestProblem checks one linkns RTM_NEWLINK request against the
// row's axes. index is d0's, which a set names; a create names none.
func linkNSRequestProblem(ax map[string]string, flags uint16, body []byte, index int32) error {
	if len(body) < IfInfomsgSizeCst {
		return fmt.Errorf("body of %d bytes", len(body))
	}
	var types []uint16
	if err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(t uint16, _ []byte) { types = append(types, t) }); err != nil {
		return err
	}
	fd, _ := linkReqAttr(body, unix.IFLA_NET_NS_FD)
	if len(fd) != 4 || binary.LittleEndian.Uint32(fd) < 3 {
		return fmt.Errorf("IFLA_NET_NS_FD is %x, want a u32 descriptor past 2", fd)
	}
	gotIndex := int32(binary.LittleEndian.Uint32(body[4:8]))
	create := flags&unix.NLM_F_CREATE != 0
	want := []uint16{unix.IFLA_NET_NS_FD}
	if s, ok := ax["ifi_index"]; ok {
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return fmt.Errorf("axis ifi_index=%q", s)
		}
		index = int32(n)
		want = append(want, unix.IFLA_IFNAME, unix.IFLA_LINKINFO)
		if !create {
			return fmt.Errorf("flags %#x: a create without NLM_F_CREATE", flags)
		}
		li, err := ParseNewLink(body)
		if err != nil {
			return err
		}
		if li.Name != ax["ifname"] || li.Kind != ax["kind"] {
			return fmt.Errorf("creates %q of kind %q, want %q of kind %q", li.Name, li.Kind, ax["ifname"], ax["kind"])
		}
	} else if create {
		return fmt.Errorf("flags %#x: a set with NLM_F_CREATE", flags)
	}
	if gotIndex != index {
		return fmt.Errorf("ifi_index %d, want %d", gotIndex, index)
	}
	if s, ok := ax["link_netnsid"]; ok {
		want = append(want, unix.IFLA_LINK_NETNSID)
		b, _ := linkReqAttr(body, unix.IFLA_LINK_NETNSID)
		if len(b) != 4 || strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(b))), 10) != s {
			return fmt.Errorf("IFLA_LINK_NETNSID is %x, want %s", b, s)
		}
	}
	if !slices.Equal(types, want) {
		return fmt.Errorf("attributes %v, want %v", types, want)
	}
	return nil
}

func TestWsLinkNSCorpus(t *testing.T) {
	var checked, dumps int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if filepath.Base(filepath.Dir(dir)) != "linkns" {
			continue
		}
		t.Run(row.Env.Kernel+"/"+row.ID, func(t *testing.T) {
			if row.Result.Status != "OK" {
				return // TestWsCorpusRows fails it
			}
			msgs := pcapNlMsgs(t, filepath.Join(dir, "ip.pcap"))
			reqs := wsRouteRequests(msgs, unix.RTM_NEWLINK)
			rc := wsReadRC(t, dir)
			if row.Expect.RefusedBy == wsRefusedIproute2 {
				if len(reqs) != 0 {
					t.Errorf("O3: ip refused, yet sent %d RTM_NEWLINK", len(reqs))
				}
				return
			}
			if len(reqs) != 1 {
				t.Fatalf("O3: %d RTM_NEWLINK requests, want 1\n  cmd: %s", len(reqs), row.Cmd)
			}
			index := wsLinkIndexOf(msgs, "d0")
			if _, ok := row.Axes["ifi_index"]; !ok && index == 0 {
				t.Fatalf("O3: no RTM_NEWLINK answers ip's lookup of d0\n  cmd: %s", row.Cmd)
			}
			if err := linkNSRequestProblem(row.Axes, reqs[0].h.Flags, reqs[0].body, index); err != nil {
				t.Errorf("O3: %v\n  cmd: %s", err, row.Cmd)
			}
			checked++
			if rc != 0 {
				return
			}
			dump := pcapNlMsgs(t, filepath.Join(dir, "netlink_route_dump.pcap"))
			if err := linkNSDumpProblem(row.Axes, dump); err != nil {
				t.Errorf("O4: %v\n  cmd: %s", err, row.Cmd)
			}
			dumps++
		})
	}
	t.Logf("O3 checked %d linkns requests, O4 %d dumps", checked, dumps)
	if checked == 0 || dumps == 0 {
		t.Error("O3 or O4 checked nothing: are there linkns rows under testdata/*/ws?")
	}
}

// linkNSDumpProblem checks the dump after a linkns row that succeeded: d0
// stays unless the row moved it to the guest's namespace, and a create's
// link is there at its index.
func linkNSDumpProblem(ax map[string]string, dump []capturedNlMsg) error {
	if got, want := wsLinkIndexOf(dump, "d0") != 0, ax["target"] != "init"; got != want {
		return fmt.Errorf("d0 in the dump: %t, want %t", got, want)
	}
	if s, ok := ax["ifi_index"]; ok {
		if got := wsLinkIndexOf(dump, ax["ifname"]); strconv.Itoa(int(got)) != s {
			return fmt.Errorf("%s at index %d, want %s", ax["ifname"], got, s)
		}
	}
	return nil
}

func TestLinkNSRequestProblem(t *testing.T) {
	fd := func(n uint32) []byte { return rtattr(unix.IFLA_NET_NS_FD, binary.LittleEndian.AppendUint32(nil, n)) }
	nsid := func(n uint32) []byte { return rtattr(unix.IFLA_LINK_NETNSID, binary.LittleEndian.AppendUint32(nil, n)) }
	ifname := rtattr(unix.IFLA_IFNAME, []byte("d9\x00"))
	kind := rtattr(unix.IFLA_LINKINFO, rtattr(unix.IFLA_INFO_KIND, []byte("dummy")))
	set := map[string]string{"netns": "fd", "target": "pid"}
	add := map[string]string{"netns": "fd", "target": "pid", "ifi_index": "100", "ifname": "d9", "kind": "dummy"}
	const req = unix.NLM_F_REQUEST | unix.NLM_F_ACK
	const create = req | unix.NLM_F_CREATE | unix.NLM_F_EXCL
	cases := []struct {
		description string
		ax          map[string]string
		flags       uint16
		body        []byte
		expected    string // "" or a substring of the error
	}{
		{"positive: a set sends the descriptor alone", set, req, ifiBody(3, 0, 0, fd(4)), ""},
		{"positive: with a link-netnsid after it", map[string]string{"netns": "fd", "link_netnsid": "5"}, req, ifiBody(3, 0, 0, fd(4), nsid(5)), ""},
		{"positive: a create's index is ifi_index", add, create, ifiBody(100, 0, 0, fd(4), ifname, kind), ""},
		{"boundary: the first descriptor past stderr", set, req, ifiBody(3, 0, 0, fd(3)), ""},
		{"negative: stderr", set, req, ifiBody(3, 0, 0, fd(2)), "past 2"},
		{"negative: no descriptor", set, req, ifiBody(3, 0, 0), "past 2"},
		{"negative: an attribute ip doesn't send", set, req, ifiBody(3, 0, 0, nsid(5), fd(4)), "attributes"},
		{"negative: the descriptor after the link-netnsid", map[string]string{"netns": "fd", "link_netnsid": "5"}, req,
			ifiBody(3, 0, 0, nsid(5), fd(4)), "attributes"},
		{"negative: another link's index", set, req, ifiBody(4, 0, 0, fd(4)), "ifi_index 4"},
		{"negative: a create's index as IFLA_NEW_IFINDEX", add, create,
			ifiBody(0, 0, 0, fd(4), ifname, kind, rtattr(unix.IFLA_NEW_IFINDEX, binary.LittleEndian.AppendUint32(nil, 100))), "ifi_index 0"},
		{"negative: the wrong link-netnsid", map[string]string{"netns": "fd", "link_netnsid": "5"}, req, ifiBody(3, 0, 0, fd(4), nsid(6)), "want 5"},
		{"negative: a set with NLM_F_CREATE", set, create, ifiBody(3, 0, 0, fd(4)), "a set with"},
		{"negative: a create of the wrong kind", add, create,
			ifiBody(100, 0, 0, fd(4), ifname, rtattr(unix.IFLA_LINKINFO, rtattr(unix.IFLA_INFO_KIND, []byte("veth")))), `kind "veth"`},
		{"corner: a short body", set, req, []byte{0}, "body of 1"},
	}
	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			wantErrContaining(t, linkNSRequestProblem(tc.ax, tc.flags, tc.body, 3), tc.expected)
		})
	}
}
