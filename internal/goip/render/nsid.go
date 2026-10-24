package render

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// NsidView is one line of `ip netns list-id`, print_nsid (ip/ipnetns.c):
// `[Deleted ]nsid N [current-nsid C ][(iproute2 netns name: NAME)]`. In
// JSON an unassigned nsid or current-nsid is left out, since print_nsid
// prints "unassigned" to the text stream alone, and so is a name it found
// none for.
type NsidView struct {
	Deleted     bool   `json:"deleted,omitempty"`
	NSID        *int32 `json:"nsid,omitempty"`
	CurrentNSID *int32 `json:"current-nsid,omitempty"`
	Name        string `json:"name,omitempty"`

	ni xtcpnl.NsidInfo
}

// NsidViewOf builds the view of one reply that carries NETNSA_NSID; name
// is the netns name print_nsid found for it, or "". deleted is an
// RTM_DELNSID.
func NsidViewOf(ni *xtcpnl.NsidInfo, deleted bool, name string) NsidView {
	v := NsidView{Deleted: deleted, Name: name, ni: *ni}
	if ni.NSID >= 0 {
		n := ni.NSID
		v.NSID = &n
	}
	if ni.HasCurrentNSID && ni.CurrentNSID >= 0 {
		c := ni.CurrentNSID
		v.CurrentNSID = &c
	}
	return v
}

// Text is the line print_nsid writes, newline terminated.
func (v *NsidView) Text() string {
	var b []byte
	if v.Deleted {
		b = append(b, "Deleted "...)
	}
	b = appendNsid(b, "nsid ", v.ni.NSID)
	if v.ni.HasCurrentNSID {
		b = appendNsid(b, "current-nsid ", v.ni.CurrentNSID)
	}
	if v.Name != "" {
		b = append(b, "(iproute2 netns name: "...)
		b = append(b, v.Name...)
		b = append(b, ')')
	}
	return string(append(b, '\n'))
}

// appendNsid is one id and its trailing space, "unassigned" when negative.
func appendNsid(b []byte, key string, id int32) []byte {
	b = append(b, key...)
	if id < 0 {
		return append(b, "unassigned "...)
	}
	b = strconv.AppendInt(b, int64(id), 10)
	return append(b, ' ')
}
