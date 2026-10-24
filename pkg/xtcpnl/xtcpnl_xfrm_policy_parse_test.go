package xtcpnl

import (
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func xfrmRPol(p *XfrmUserpolicyInfo) string {
	return "pol{dir=" + strconv.Itoa(int(p.Dir)) + " action=" + strconv.Itoa(int(p.Action)) +
		" priority=" + u32s(p.Priority) + " index=" + u32s(p.Index) + " flags=" + strconv.Itoa(int(p.Flags)) +
		" share=" + strconv.Itoa(int(p.Share)) + " " + xfrmRSel(&p.Sel) + " lft=" + xfrmRLft(&p.Lft) +
		" cur=" + xfrmRCur(&p.Curlft) + "}"
}

func xfrmRTmpl(t *XfrmUserTmpl) string {
	return "tmpl{dst=" + xfrmRAddr(t.ID.Daddr, t.Family) + " spi=0x" + strconv.FormatUint(uint64(t.ID.SPI), 16) +
		" proto=" + strconv.Itoa(int(t.ID.Proto)) + " family=" + strconv.Itoa(int(t.Family)) +
		" src=" + xfrmRAddr(t.Saddr, t.Family) + " reqid=" + u32s(t.Reqid) + " mode=" + strconv.Itoa(int(t.Mode)) +
		" share=" + strconv.Itoa(int(t.Share)) + " optional=" + strconv.Itoa(int(t.Optional)) +
		" algos=0x" + strconv.FormatUint(uint64(t.AAlgos), 16) + "/0x" + strconv.FormatUint(uint64(t.EAlgos), 16) +
		"/0x" + strconv.FormatUint(uint64(t.CAlgos), 16) + "}"
}

// xfrmPolicyRender renders a decoded policy message: its type, then
// whichever struct it carries, then its attributes, then the error, if
// any.
func xfrmPolicyRender(p *XfrmPolicy, err error) string {
	w := []string{"type=" + strconv.Itoa(int(p.Type))}
	var family uint16
	if p.Dump {
		w = append(w, "dump")
	}
	if p.Info != nil {
		w = append(w, xfrmRPol(p.Info))
		family = p.Info.Sel.Family
	}
	if p.ID != nil {
		w = append(w, "id{"+xfrmRSel(&p.ID.Sel)+" index="+u32s(p.ID.Index)+" dir="+strconv.Itoa(int(p.ID.Dir))+"}")
		family = p.ID.Sel.Family
	}
	if p.Expire != nil {
		w = append(w, "hard="+strconv.Itoa(int(p.Expire.Hard)), xfrmRPol(&p.Expire.Pol))
		family = p.Expire.Pol.Sel.Family
	}
	if p.Default != nil {
		w = append(w, "default{in="+strconv.Itoa(int(p.Default.In))+" fwd="+strconv.Itoa(int(p.Default.Fwd))+
			" out="+strconv.Itoa(int(p.Default.Out))+"}")
	}
	w = append(w, xfrmRAttrs(&p.Attrs, family)...)
	if err != nil {
		w = append(w, "error: "+err.Error())
	}
	return strings.Join(w, " ")
}

// xfrmTestPolicy is a hand-built xfrm_userpolicy_info whose every field is
// distinct, so a field read from the wrong offset shows.
func xfrmTestPolicy() []byte {
	b := make([]byte, XfrmUserpolicyInfoSizeCst)
	copy(b, xfrmTestInfo()[:XfrmSelectorSizeCst])
	for i := range 8 {
		binary.LittleEndian.PutUint64(b[56+8*i:], uint64(101+i))
	}
	binary.LittleEndian.PutUint64(b[64:], ^uint64(0))
	for i := range 4 {
		binary.LittleEndian.PutUint64(b[120+8*i:], uint64(201+i))
	}
	binary.LittleEndian.PutUint32(b[152:], 301)
	binary.LittleEndian.PutUint32(b[156:], 302)
	b[160], b[161], b[162], b[163] = 2, 1, 4, 3
	return b
}

// xfrmTestPolicyID is a hand-built xfrm_userpolicy_id: xfrmTestInfo's
// selector, index 77 and direction 2.
func xfrmTestPolicyID() []byte {
	b := make([]byte, XfrmUserpolicyIDSizeCst)
	copy(b, xfrmTestInfo()[:XfrmSelectorSizeCst])
	binary.LittleEndian.PutUint32(b[56:], 77)
	b[60] = 2
	return b
}

// xfrmTestTmpl is a hand-built xfrm_user_tmpl whose every field is
// distinct, its reqid r.
func xfrmTestTmpl(r uint32) []byte {
	b := make([]byte, XfrmUserTmplSizeCst)
	copy(b, []byte{0x20, 0x01, 0x0d, 0xb8, 15: 2})
	binary.BigEndian.PutUint32(b[16:], 0xabcd)
	b[20] = unix.IPPROTO_AH
	binary.LittleEndian.PutUint16(b[24:], unix.AF_INET6)
	copy(b[28:], []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1})
	binary.LittleEndian.PutUint32(b[44:], r)
	b[48], b[49], b[50] = 1, 2, 1
	binary.LittleEndian.PutUint32(b[52:], 0x11)
	binary.LittleEndian.PutUint32(b[56:], 0x22)
	binary.LittleEndian.PutUint32(b[60:], 0x33)
	return b
}

func TestParseXfrmPolicy(t *testing.T) {
	pol := xfrmTestPolicy()
	sel := "sel{203.0.113.0/25-198.51.100.0/24 dport=443/65535 sport=1024/65280 family=2 proto=6 ifindex=7 user=1000}"
	polText := "pol{dir=2 action=1 priority=301 index=302 flags=4 share=3 " + sel +
		" lft=101,inf,103,104,105,106,107,108 cur=201B/202P/203/204}"
	idText := "id{" + sel + " index=77 dir=2}"
	tmplText := func(r string) string {
		return "tmpl{dst=2001:db8::2 spi=0xabcd proto=51 family=10 src=2001:db8::1 reqid=" + r +
			" mode=1 share=2 optional=1 algos=0x11/0x22/0x33}"
	}
	newpol := NlMsgHdr{Type: XfrmMsgNewPolicy}
	withPol := func(attrs ...[]byte) []byte { return cat(append([][]byte{pol}, attrs...)...) }
	dumpFlags := uint16(unix.NLM_F_REQUEST | unix.NLM_F_DUMP)
	tests := []struct {
		description string
		h           NlMsgHdr
		body        []byte
		cap         string // a fixture pcap the body comes from instead
		request     bool
		nth         int
		expected    string
	}{
		{
			description: "positive: ip's add, a NEWPOLICY with a selector and a template and no index (policy_add)",
			h:           newpol, cap: "policy_add/ip.pcap", request: true,
			expected: "type=19 pol{dir=1 action=0 priority=0 index=0 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/0/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: the kernel's dump of it, with index 1 (policy_add)",
			h:           newpol, cap: "policy_add/netlink_xfrm_policy.pcap",
			expected: "type=19 pol{dir=1 action=0 priority=0 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536561/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: an IPv6 block with a port and no template, read back with index 8 (policy_add_v6_block)",
			h:           newpol, cap: "policy_add_v6_block/netlink_xfrm_policy.pcap",
			expected: "type=19 pol{dir=0 action=1 priority=10 index=8 flags=0 share=0 sel{2001:db8::1/128-2001:db8::2/128 dport=443/65535 sport=0/0 family=10 proto=6 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536563/0}",
		},
		{
			description: "positive: ip's update, an UPDPOLICY with priority 5 (policy_update)",
			h:           NlMsgHdr{Type: XfrmMsgUpdPolicy}, cap: "policy_update/ip.pcap", request: true,
			expected: "type=25 pol{dir=1 action=0 priority=5 index=0 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/0/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: the kernel's UPDPOLICY event (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgUpdPolicy}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=25 pol{dir=1 action=0 priority=5 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536608/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: ip's get, a GETPOLICY by selector and direction (policy_get)",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy}, cap: "policy_get/ip.pcap", request: true,
			expected: "type=21 id{sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} index=0 dir=1}",
		},
		{
			description: "positive: ip's get by index, with an empty IPv4 selector (policy_get_index)",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy}, cap: "policy_get_index/ip.pcap", request: true,
			expected: "type=21 id{sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} index=1 dir=1}",
		},
		{
			description: "boundary: ip's list, a GETPOLICY dump with an empty body (policy_list_empty)",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy}, cap: "policy_list_empty/ip.pcap", request: true,
			expected: "type=21 dump",
		},
		{
			description: "positive: ip's delete, a DELPOLICY by selector (policy_delete)",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy}, cap: "policy_delete/ip.pcap", request: true,
			expected: "type=20 id{sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} index=0 dir=1}",
		},
		{
			description: "positive: the kernel's DELPOLICY event nests the deleted policy (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=20 id{sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} index=0 dir=1} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff} policy=pol{dir=1 action=0 priority=5 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536608/0}",
		},
		{
			description: "boundary: ip's flush, a FLUSHPOLICY with no body (policy_flush)",
			h:           NlMsgHdr{Type: XfrmMsgFlushPolicy}, cap: "policy_flush/ip.pcap", request: true,
			expected: "type=29",
		},
		{
			description: "boundary: the kernel's FLUSHPOLICY event, no body either (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgFlushPolicy}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=29",
		},
		{
			description: "positive: the kernel's soft POLEXPIRE (monitor_polexpire)",
			h:           NlMsgHdr{Type: XfrmMsgPolExpire}, cap: "monitor_polexpire/ip.pcap",
			expected: "type=27 hard=0 pol{dir=1 action=0 priority=0 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,1,3,0,0 cur=0B/0P/1792536615/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: then its hard one (monitor_polexpire)",
			h:           NlMsgHdr{Type: XfrmMsgPolExpire}, cap: "monitor_polexpire/ip.pcap", nth: 1,
			expected: "type=27 hard=1 pol{dir=1 action=0 priority=0 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,1,3,0,0 cur=0B/0P/1792536615/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: ip's getdefault, every direction unspecified (policy_getdefault)",
			h:           NlMsgHdr{Type: XfrmMsgGetDefault}, cap: "policy_getdefault/ip.pcap", request: true,
			expected: "type=40 default{in=0 fwd=0 out=0}",
		},
		{
			description: "positive: the kernel's answer, accept in each, its 3 bytes padded to 4 (policy_getdefault)",
			h:           NlMsgHdr{Type: XfrmMsgGetDefault}, cap: "policy_getdefault/ip.pcap",
			expected: "type=40 default{in=2 fwd=2 out=2}",
		},
		{
			description: "positive: ip's setdefault, block in and fwd and accept out (policy_setdefault)",
			h:           NlMsgHdr{Type: XfrmMsgSetDefault}, cap: "policy_setdefault/ip.pcap", request: true,
			expected: "type=39 default{in=1 fwd=1 out=2}",
		},
		{
			description: "corner: every field of the userpolicy_info decodes from its own offset",
			h:           newpol, body: pol,
			expected: "type=19 " + polText,
		},
		{
			description: "corner: an UPDPOLICY has the NEWPOLICY's body",
			h:           NlMsgHdr{Type: XfrmMsgUpdPolicy}, body: pol,
			expected: "type=25 " + polText,
		},
		{
			description: "corner: every field of the userpolicy_id decodes from its own offset",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy}, body: xfrmTestPolicyID(),
			expected: "type=21 " + idText,
		},
		{
			description: "corner: a GETPOLICY with only one NLM_F_DUMP bit is not a dump",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy, Flags: unix.NLM_F_REQUEST | unix.NLM_F_ROOT}, body: xfrmTestPolicyID(),
			expected: "type=21 " + idText,
		},
		{
			description: "corner: a DELPOLICY with both NLM_F_DUMP bits still has its id",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy, Flags: dumpFlags}, body: xfrmTestPolicyID(),
			expected: "type=20 " + idText,
		},
		{
			description: "corner: a GETPOLICY dump's attributes start at the body",
			h:           NlMsgHdr{Type: XfrmMsgGetPolicy, Flags: dumpFlags}, body: rtattr(XfrmAttrPolicyType, []byte{1, 0, 0, 0, 0, 0}),
			expected: "type=21 dump ptype=1",
		},
		{
			description: "corner: a FLUSHPOLICY's attributes start at the body",
			h:           NlMsgHdr{Type: XfrmMsgFlushPolicy}, body: cat(rtattr(XfrmAttrPolicyType, []byte{1, 0, 0, 0, 0, 0}), rtattr(XfrmAttrIfID, le32(9))),
			expected: "type=29 ptype=1 if_id=9",
		},
		{
			description: "corner: a POLEXPIRE's hard flag follows its policy, and its attributes start 8 bytes on",
			h:           NlMsgHdr{Type: XfrmMsgPolExpire}, body: cat(pol, []byte{1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, rtattr(XfrmAttrIfID, le32(9))),
			expected: "type=27 hard=1 " + polText + " if_id=9",
		},
		{
			description: "corner: a SETDEFAULT's attributes start at the NLMSG_ALIGN of its 3 bytes",
			h:           NlMsgHdr{Type: XfrmMsgSetDefault}, body: cat([]byte{1, 2, 0, 0xff}, rtattr(XfrmAttrIfID, le32(9))),
			expected: "type=39 default{in=1 fwd=2 out=0} if_id=9",
		},
		{
			description: "corner: every field of a template decodes from its own offset; two in one attribute",
			h:           newpol, body: withPol(rtattr(XfrmAttrTmpl, cat(xfrmTestTmpl(5), xfrmTestTmpl(6)))),
			expected: "type=19 " + polText + " " + tmplText("5") + " " + tmplText("6"),
		},
		{
			description: "corner: a policy's other attributes: its type, mark, security context and if_id",
			h:           newpol, body: withPol(rtattr(XfrmAttrPolicyType, []byte{1, 0xaa, 0xbb, 0xcc, 0xdd, 0xee}), rtattr(XfrmAttrMark, cat(le32(7), le32(0xff))),
				rtattr(XfrmAttrSecCtx, cat(le16(13), le16(1), []byte{1, 2}, le16(5), []byte("u:r:x"))), rtattr(XfrmAttrIfID, le32(3))),
			expected: "type=19 " + polText + " ptype=1 sec_ctx=1/1/2:u:r:x mark=7/0xff if_id=3",
		},
		{
			description: "corner: a DELPOLICY's nested policy decodes as a NEWPOLICY's does",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy}, body: cat(xfrmTestPolicyID(), rtattr(XfrmAttrPolicy, pol)),
			expected: "type=20 " + idText + " policy=" + polText,
		},
		{
			description: "corner: a repeated template attribute keeps the last",
			h:           newpol, body: withPol(rtattr(XfrmAttrTmpl, cat(xfrmTestTmpl(5), xfrmTestTmpl(6))), rtattr(XfrmAttrTmpl, xfrmTestTmpl(7))),
			expected: "type=19 " + polText + " " + tmplText("7"),
		},
		{
			description: "boundary: a template attribute of 127 bytes holds one, the rest left out",
			h:           newpol, body: withPol(rtattr(XfrmAttrTmpl, cat(xfrmTestTmpl(5), make([]byte, XfrmUserTmplSizeCst-1)))),
			expected: "type=19 " + polText + " " + tmplText("5"),
		},
		{
			description: "negative: an SA type",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, body: pol,
			expected: "type=16 error: " + ErrXfrmType.Error() + ": 16",
		},
		{
			description: "negative: a NEWPOLICY one byte short of its userpolicy_info",
			h:           newpol, body: pol[:XfrmUserpolicyInfoSizeCst-1],
			expected: "type=19 error: " + ErrXfrmSmall.Error() + ": type 19 needs 168 bytes, has 167",
		},
		{
			description: "negative: a DELPOLICY one byte short of its userpolicy_id",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy}, body: make([]byte, XfrmUserpolicyIDSizeCst-1),
			expected: "type=20 error: " + ErrXfrmSmall.Error() + ": type 20 needs 64 bytes, has 63",
		},
		{
			description: "negative: a POLEXPIRE one byte short of its polexpire",
			h:           NlMsgHdr{Type: XfrmMsgPolExpire}, body: make([]byte, XfrmUserPolexpireSizeCst-1),
			expected: "type=27 error: " + ErrXfrmSmall.Error() + ": type 27 needs 176 bytes, has 175",
		},
		{
			description: "negative: a GETDEFAULT one byte short of its userpolicy_default",
			h:           NlMsgHdr{Type: XfrmMsgGetDefault}, body: []byte{1, 2},
			expected: "type=40 error: " + ErrXfrmSmall.Error() + ": type 40 needs 3 bytes, has 2",
		},
		{
			description: "negative: a template attribute one byte short, the attributes before it kept",
			h:           newpol, body: withPol(rtattr(XfrmAttrIfID, le32(1)), rtattr(XfrmAttrTmpl, make([]byte, XfrmUserTmplSizeCst-1))),
			expected: "type=19 " + polText + " if_id=1 error: " + ErrXfrmAttr.Error() + ": 5 is 63 bytes, under 64",
		},
		{
			description: "negative: a nested policy one byte short",
			h:           NlMsgHdr{Type: XfrmMsgDelPolicy}, body: cat(xfrmTestPolicyID(), rtattr(XfrmAttrPolicy, pol[:XfrmUserpolicyInfoSizeCst-1])),
			expected: "type=20 " + idText + " error: " + ErrXfrmAttr.Error() + ": 7 is 167 bytes, under 168",
		},
		{
			description: "negative: a policy type one byte short",
			h:           newpol, body: withPol(rtattr(XfrmAttrPolicyType, []byte{1, 0, 0, 0, 0})),
			expected: "type=19 " + polText + " error: " + ErrXfrmAttr.Error() + ": 16 is 5 bytes, under 6",
		},
		{
			description: "negative: an attribute running past the body",
			h:           newpol, body: withPol([]byte{0x40, 0, 5, 0}),
			expected: "type=19 " + polText + " error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			h, body := tt.h, tt.body
			if tt.cap != "" {
				h, body = xfrmCaptured(t, tt.cap, tt.h.Type, tt.request, tt.nth)
			}
			p, err := ParseXfrmPolicy(h, body)
			if got := xfrmPolicyRender(&p, err); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseXfrmPolicyCorpus decodes every policy message in the corpus.
func TestParseXfrmPolicyCorpus(t *testing.T) {
	tally := func(types []uint16, request bool) string {
		var n, errs, info, id, expire, def, dump, tmpl, nested int
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_XFRM || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			want := false
			for _, typ := range types {
				want = want || m.h.Type == typ
			}
			if !want {
				continue
			}
			n++
			p, err := ParseXfrmPolicy(m.h, m.body)
			if err != nil {
				errs++
				continue
			}
			for _, k := range []struct {
				ok bool
				c  *int
			}{
				{p.Info != nil, &info}, {p.ID != nil, &id}, {p.Expire != nil, &expire}, {p.Default != nil, &def},
				{p.Dump, &dump}, {len(p.Attrs.Tmpl) > 0, &tmpl}, {p.Attrs.Policy != nil, &nested},
			} {
				if k.ok {
					*k.c++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(info) + " info, " +
			strconv.Itoa(id) + " id, " + strconv.Itoa(expire) + " expire, " + strconv.Itoa(def) + " default, " +
			strconv.Itoa(dump) + " dump, " + strconv.Itoa(tmpl) + " with a template, " + strconv.Itoa(nested) + " nested policy"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's NEWPOLICY and UPDPOLICY requests decode, all but the block with a template", got: tally([]uint16{XfrmMsgNewPolicy, XfrmMsgUpdPolicy}, true), expected: "9 decoded, 0 errors, 9 info, 0 id, 0 expire, 0 default, 0 dump, 8 with a template, 0 nested policy"},
		{description: "positive: the kernel's NEWPOLICY and UPDPOLICY messages decode", got: tally([]uint16{XfrmMsgNewPolicy, XfrmMsgUpdPolicy}, false), expected: "15 decoded, 0 errors, 15 info, 0 id, 0 expire, 0 default, 0 dump, 11 with a template, 0 nested policy"},
		{description: "positive: ip's GETPOLICY and DELPOLICY requests decode, the dumps without an id", got: tally([]uint16{XfrmMsgGetPolicy, XfrmMsgDelPolicy}, true), expected: "21 decoded, 0 errors, 0 info, 8 id, 0 expire, 0 default, 13 dump, 0 with a template, 0 nested policy"},
		{description: "positive: the kernel's DELPOLICY event decodes with its nested policy and template", got: tally([]uint16{XfrmMsgDelPolicy}, false), expected: "1 decoded, 0 errors, 0 info, 1 id, 0 expire, 0 default, 0 dump, 1 with a template, 1 nested policy"},
		{description: "positive: the FLUSHPOLICY requests and event decode, with nothing in them", got: tally([]uint16{XfrmMsgFlushPolicy}, true) + "|" + tally([]uint16{XfrmMsgFlushPolicy}, false), expected: "3 decoded, 0 errors, 0 info, 0 id, 0 expire, 0 default, 0 dump, 0 with a template, 0 nested policy|1 decoded, 0 errors, 0 info, 0 id, 0 expire, 0 default, 0 dump, 0 with a template, 0 nested policy"},
		{description: "positive: the kernel's two POLEXPIRE events decode", got: tally([]uint16{XfrmMsgPolExpire}, false), expected: "2 decoded, 0 errors, 0 info, 0 id, 2 expire, 0 default, 0 dump, 2 with a template, 0 nested policy"},
		{description: "positive: ip's SETDEFAULT and GETDEFAULT requests and the kernel's GETDEFAULTs decode", got: tally([]uint16{XfrmMsgSetDefault, XfrmMsgGetDefault}, true) + "|" + tally([]uint16{XfrmMsgSetDefault, XfrmMsgGetDefault}, false), expected: "4 decoded, 0 errors, 0 info, 0 id, 0 expire, 4 default, 0 dump, 0 with a template, 0 nested policy|3 decoded, 0 errors, 0 info, 0 id, 0 expire, 3 default, 0 dump, 0 with a template, 0 nested policy"},
		{description: "negative: the kernel sends no GETPOLICY and no SETDEFAULT", got: tally([]uint16{XfrmMsgGetPolicy, XfrmMsgSetDefault}, false), expected: "0 decoded, 0 errors, 0 info, 0 id, 0 expire, 0 default, 0 dump, 0 with a template, 0 nested policy"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
