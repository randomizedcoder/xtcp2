package xtcpnl

import (
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// xfrmEventRun is what differs from one capture run to the next in an
// ACQUIRE: the source port its flow's socket was given, and the kernel's
// acquire sequence number, which is global.
var xfrmEventRun = regexp.MustCompile(`(sport=|acq_seq=)[0-9]+`)

// xfrmEventRender renders a decoded event: its type, then whichever
// struct it carries, then its attributes, then the error, if any.
func xfrmEventRender(e *XfrmEvent, err error) string {
	w := []string{"type=" + strconv.Itoa(int(e.Type))}
	var family uint16
	if a := e.Acquire; a != nil {
		family = a.Sel.Family
		w = append(w, "acquire{dst="+xfrmRAddr(a.ID.Daddr, family)+" spi=0x"+strconv.FormatUint(uint64(a.ID.SPI), 16)+
			" proto="+strconv.Itoa(int(a.ID.Proto))+" src="+xfrmRAddr(a.Saddr, family)+" "+xfrmRSel(&a.Sel)+
			" algos=0x"+strconv.FormatUint(uint64(a.AAlgos), 16)+"/0x"+strconv.FormatUint(uint64(a.EAlgos), 16)+
			"/0x"+strconv.FormatUint(uint64(a.CAlgos), 16)+" acq_seq="+u32s(a.Seq)+"}", xfrmRPol(&a.Policy))
	}
	if x := e.Expire; x != nil {
		w = append(w, "hard="+strconv.Itoa(int(x.Hard)), xfrmRInfo(&x.State))
		family = x.State.Family
	}
	if m := e.Mapping; m != nil {
		family = m.ID.Family
		w = append(w, "mapping{"+xfrmRID(&m.ID)+" reqid="+u32s(m.Reqid)+
			" old="+xfrmRAddr(m.OldSaddr, family)+":"+strconv.Itoa(int(m.OldSport))+
			" new="+xfrmRAddr(m.NewSaddr, family)+":"+strconv.Itoa(int(m.NewSport))+"}")
	}
	w = append(w, xfrmRAttrs(&e.Attrs, family)...)
	if err != nil {
		w = append(w, "error: "+err.Error())
	}
	return strings.Join(w, " ")
}

// xfrmTestAcquire is a hand-built xfrm_user_acquire of the kernel's 280
// bytes, not the constant under test, whose every field is distinct:
// xfrmTestInfo's selector and xfrmTestPolicy's policy.
func xfrmTestAcquire() []byte {
	b := make([]byte, 280)
	copy(b, []byte{192, 0, 2, 12})
	binary.BigEndian.PutUint32(b[16:], 0xc0ffee02)
	b[20] = unix.IPPROTO_ESP
	copy(b[24:], []byte{192, 0, 2, 11})
	copy(b[40:], xfrmTestInfo()[:XfrmSelectorSizeCst])
	copy(b[96:], xfrmTestPolicy())
	binary.LittleEndian.PutUint32(b[264:], 0x11)
	binary.LittleEndian.PutUint32(b[268:], 0x22)
	binary.LittleEndian.PutUint32(b[272:], 0x33)
	binary.LittleEndian.PutUint32(b[276:], 0x44)
	return b
}

// xfrmTestMapping is a hand-built xfrm_user_mapping of the kernel's 64
// bytes: xfrmTestID's SA, from [2001:db8::5]:4500 to [2001:db8::6]:4501.
func xfrmTestMapping() []byte {
	b := make([]byte, 64)
	copy(b, xfrmTestID())
	binary.LittleEndian.PutUint32(b[24:], 55)
	copy(b[28:], []byte{0x20, 0x01, 0x0d, 0xb8, 15: 5})
	copy(b[44:], []byte{0x20, 0x01, 0x0d, 0xb8, 15: 6})
	binary.BigEndian.PutUint16(b[60:], 4500)
	binary.BigEndian.PutUint16(b[62:], 4501)
	return b
}

func TestParseXfrmEvent(t *testing.T) {
	info := xfrmTestInfo()
	infoText := "info{dst=192.0.2.2 spi=0xc0ffee01 proto=50 src=192.0.2.1 family=2 mode=1 reqid=402 seq=401 replay_window=32 flags=8" +
		" sel{203.0.113.0/25-198.51.100.0/24 dport=443/65535 sport=1024/65280 family=2 proto=6 ifindex=7 user=1000}" +
		" lft=101,inf,103,104,105,106,107,108 cur=201B/202P/203/204 stats=301/302/303}"
	sel := "sel{203.0.113.0/25-198.51.100.0/24 dport=443/65535 sport=1024/65280 family=2 proto=6 ifindex=7 user=1000}"
	acqText := "acquire{dst=192.0.2.12 spi=0xc0ffee02 proto=50 src=192.0.2.11 " + sel +
		" algos=0x11/0x22/0x33 acq_seq=68} pol{dir=2 action=1 priority=301 index=302 flags=4 share=3 " + sel +
		" lft=101,inf,103,104,105,106,107,108 cur=201B/202P/203/204}"
	mapText := "mapping{id{dst=2001:db8::9 spi=0x1234 family=10 proto=51} reqid=55 old=2001:db8::5:4500 new=2001:db8::6:4501}"
	expire := func(hard byte, attrs ...[]byte) []byte {
		return cat(append([][]byte{info, {hard, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}}, attrs...)...)
	}
	tests := []struct {
		description string
		h           NlMsgHdr
		body        []byte
		cap         string // a fixture pcap the body comes from instead
		nth         int
		run         bool // mask what differs from run to run
		expected    string
	}{
		{
			description: "positive: the kernel's ACQUIRE for a UDP flow P1 needs an SA for, with P1's template (monitor_acquire)",
			h:           NlMsgHdr{Type: XfrmMsgAcquire}, cap: "monitor_acquire/ip.pcap", run: true,
			expected: "type=23 acquire{dst=192.0.2.2 spi=0x0 proto=50 src=192.0.2.1 sel{192.0.2.1/32-192.0.2.2/32 dport=9/65535 sport=N/65535 family=2 proto=17 ifindex=3 user=0}" +
				" algos=0xffffffff/0xffffffff/0xffffffff acq_seq=N} pol{dir=1 action=0 priority=0 index=1 flags=0 share=0 sel{192.0.2.1/32-192.0.2.2/32 dport=0/0 sport=N/0 family=2 proto=0 ifindex=0 user=0}" +
				" lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536609/0} tmpl{dst=192.0.2.2 spi=0x0 proto=50 family=2 src=192.0.2.1 reqid=1 mode=0 share=0 optional=0 algos=0xffffffff/0xffffffff/0xffffffff}",
		},
		{
			description: "positive: the kernel's soft EXPIRE (monitor_expire)",
			h:           NlMsgHdr{Type: XfrmMsgExpire}, cap: "monitor_expire/ip.pcap",
			expected: "type=24 hard=0 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0" +
				" sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,1,2,0,0 cur=0B/0P/1792536611/0 stats=0/0/0}",
		},
		{
			description: "positive: then its hard one (monitor_expire)",
			h:           NlMsgHdr{Type: XfrmMsgExpire}, cap: "monitor_expire/ip.pcap", nth: 1,
			expected: "type=24 hard=1 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0" +
				" sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,1,2,0,0 cur=0B/0P/1792536611/0 stats=0/0/0}",
		},
		{
			description: "positive: the kernel's MAPPING, the peer's port gone from 4500 to 4501 (monitor_mapping)",
			h:           NlMsgHdr{Type: XfrmMsgMapping}, cap: "monitor_mapping/ip.pcap",
			expected: "type=38 mapping{id{dst=192.0.2.2 spi=0x300 family=2 proto=50} reqid=0 old=192.0.2.1:4500 new=192.0.2.1:4501}",
		},
		{
			description: "corner: every field of the acquire decodes from its own offset",
			h:           NlMsgHdr{Type: XfrmMsgAcquire}, body: xfrmTestAcquire(),
			expected: "type=23 " + acqText,
		},
		{
			description: "corner: an acquire's attributes start at 280: its template, security context and if_id",
			h:           NlMsgHdr{Type: XfrmMsgAcquire}, body: cat(xfrmTestAcquire(), rtattr(XfrmAttrTmpl, xfrmTestTmpl(5)), rtattr(XfrmAttrIfID, le32(9))),
			expected: "type=23 " + acqText + " tmpl{dst=2001:db8::2 spi=0xabcd proto=51 family=10 src=2001:db8::1 reqid=5" +
				" mode=1 share=2 optional=1 algos=0x11/0x22/0x33} if_id=9",
		},
		{
			description: "corner: an expire's hard flag follows its SA, and its attributes start 8 bytes on",
			h:           NlMsgHdr{Type: XfrmMsgExpire}, body: expire(1, rtattr(XfrmAttrMark, cat(le32(7), le32(0xff)))),
			expected: "type=24 hard=1 " + infoText + " mark=7/0xff",
		},
		{
			description: "boundary: an expire of exactly 232 bytes, soft",
			h:           NlMsgHdr{Type: XfrmMsgExpire}, body: expire(0),
			expected: "type=24 hard=0 " + infoText,
		},
		{
			description: "corner: every field of the mapping decodes from its own offset, its ports in host order",
			h:           NlMsgHdr{Type: XfrmMsgMapping}, body: xfrmTestMapping(),
			expected: "type=38 " + mapText,
		},
		{
			description: "corner: a mapping's attributes start at 64",
			h:           NlMsgHdr{Type: XfrmMsgMapping}, body: cat(xfrmTestMapping(), rtattr(XfrmAttrIfID, le32(3))),
			expected: "type=38 " + mapText + " if_id=3",
		},
		{
			description: "negative: a policy expire is ParseXfrmPolicy's",
			h:           NlMsgHdr{Type: XfrmMsgPolExpire}, body: expire(0),
			expected: "type=27 error: " + ErrXfrmType.Error() + ": 27",
		},
		{
			description: "negative: an acquire one byte short",
			h:           NlMsgHdr{Type: XfrmMsgAcquire}, body: xfrmTestAcquire()[:XfrmUserAcquireSizeCst-1],
			expected: "type=23 error: " + ErrXfrmSmall.Error() + ": type 23 needs 280 bytes, has 279",
		},
		{
			description: "negative: an expire of only its SA, the hard flag missing",
			h:           NlMsgHdr{Type: XfrmMsgExpire}, body: info,
			expected: "type=24 error: " + ErrXfrmSmall.Error() + ": type 24 needs 232 bytes, has 224",
		},
		{
			description: "negative: a mapping one byte short",
			h:           NlMsgHdr{Type: XfrmMsgMapping}, body: xfrmTestMapping()[:XfrmUserMappingSizeCst-1],
			expected: "type=38 error: " + ErrXfrmSmall.Error() + ": type 38 needs 64 bytes, has 63",
		},
		{
			description: "negative: a template attribute one byte short, the attributes before it kept",
			h:           NlMsgHdr{Type: XfrmMsgAcquire}, body: cat(xfrmTestAcquire(), rtattr(XfrmAttrIfID, le32(1)), rtattr(XfrmAttrTmpl, make([]byte, XfrmUserTmplSizeCst-1))),
			expected: "type=23 " + acqText + " if_id=1 error: " + ErrXfrmAttr.Error() + ": 5 is 63 bytes, under 64",
		},
		{
			description: "negative: an attribute running past the body",
			h:           NlMsgHdr{Type: XfrmMsgMapping}, body: cat(xfrmTestMapping(), []byte{0x40, 0, 5, 0}),
			expected: "type=38 " + mapText + " error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			h, body := tt.h, tt.body
			if tt.cap != "" {
				h, body = xfrmCaptured(t, tt.cap, tt.h.Type, false, tt.nth)
			}
			e, err := ParseXfrmEvent(h, body)
			got := xfrmEventRender(&e, err)
			if tt.run {
				got = xfrmEventRun.ReplaceAllString(got, "${1}N")
			}
			if got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseXfrmEventCorpus decodes every event in the corpus.
func TestParseXfrmEventCorpus(t *testing.T) {
	tally := func(request bool) string {
		var n, errs, acq, exp, hard, mapping, tmpl int
		for _, c := range corpusAll(t) {
			m := c.m
			if m.proto != unix.NETLINK_XFRM || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
				continue
			}
			if m.h.Type != XfrmMsgAcquire && m.h.Type != XfrmMsgExpire && m.h.Type != XfrmMsgMapping {
				continue
			}
			n++
			e, err := ParseXfrmEvent(m.h, m.body)
			if err != nil {
				errs++
				continue
			}
			for _, k := range []struct {
				ok bool
				c  *int
			}{
				{e.Acquire != nil, &acq}, {e.Expire != nil, &exp}, {e.Expire != nil && e.Expire.Hard == 1, &hard},
				{e.Mapping != nil, &mapping}, {len(e.Attrs.Tmpl) > 0, &tmpl},
			} {
				if k.ok {
					*k.c++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(acq) + " acquire, " +
			strconv.Itoa(exp) + " expire, " + strconv.Itoa(hard) + " hard, " + strconv.Itoa(mapping) + " mapping, " +
			strconv.Itoa(tmpl) + " with a template"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: the kernel's ACQUIRE, EXPIREs and MAPPING decode", got: tally(false), expected: "4 decoded, 0 errors, 1 acquire, 2 expire, 1 hard, 1 mapping, 1 with a template"},
		{description: "negative: ip sends none of them", got: tally(true), expected: "0 decoded, 0 errors, 0 acquire, 0 expire, 0 hard, 0 mapping, 0 with a template"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
