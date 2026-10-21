package xtcpnl

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// xfrmInfoRender renders a decoded SAD or SPD info message: its type and
// flags, then the attributes that are set, in a fixed order, then the
// error, if any.
func xfrmInfoRender(x *XfrmInfo, err error) string {
	w := []string{"type=" + strconv.Itoa(int(x.Type)), "flags=0x" + strconv.FormatUint(uint64(x.Flags), 16)}
	hinfo := func(h *XfrmHInfo) string { return u32s(h.Count) + "/" + u32s(h.MaxCount) }
	thresh := func(h *XfrmSPDHThresh) string { return strconv.Itoa(int(h.LBits)) + "/" + strconv.Itoa(int(h.RBits)) }
	if x.SADCnt != nil {
		w = append(w, "sad_cnt="+u32s(*x.SADCnt))
	}
	if x.SADHInfo != nil {
		w = append(w, "sad_hinfo="+hinfo(x.SADHInfo))
	}
	if i := x.SPDInfo; i != nil {
		w = append(w, "spd_info="+u32s(i.In)+"/"+u32s(i.Out)+"/"+u32s(i.Fwd)+"/"+u32s(i.InS)+"/"+u32s(i.OutS)+"/"+u32s(i.FwdS))
	}
	if x.SPDHInfo != nil {
		w = append(w, "spd_hinfo="+hinfo(x.SPDHInfo))
	}
	if x.IPv4HThresh != nil {
		w = append(w, "hthresh4="+thresh(x.IPv4HThresh))
	}
	if x.IPv6HThresh != nil {
		w = append(w, "hthresh6="+thresh(x.IPv6HThresh))
	}
	if err != nil {
		w = append(w, "error: "+err.Error())
	}
	return strings.Join(w, " ")
}

func TestParseXfrmInfo(t *testing.T) {
	flags := le32(0xffffffff)
	spdInfo := rtattr(XfrmSPDAttrInfo, cat(le32(1), le32(2), le32(3), le32(4), le32(5), le32(6)))
	sadInfo := cat(flags, rtattr(XfrmSADAttrCnt, le32(7)), rtattr(XfrmSADAttrHInfo, cat(le32(8), le32(9))))
	sad := NlMsgHdr{Type: XfrmMsgNewSADInfo}
	spd := NlMsgHdr{Type: XfrmMsgNewSPDInfo}
	tests := []struct {
		description string
		h           NlMsgHdr
		body        []byte
		cap         string // a fixture pcap the body comes from instead
		request     bool
		expected    string
	}{
		{
			description: "boundary: ip's count, a GETSADINFO of only its flags (state_count)",
			h:           NlMsgHdr{Type: XfrmMsgGetSADInfo}, cap: "state_count/ip.pcap", request: true,
			expected: "type=35 flags=0xffffffff",
		},
		{
			description: "positive: the kernel's answer, two SAs in 8 buckets (state_count)",
			h:           sad, cap: "state_count/ip.pcap",
			expected: "type=34 flags=0xffffffff sad_cnt=2 sad_hinfo=8/1048576",
		},
		{
			description: "boundary: an empty SAD counts 0 (state_count_empty)",
			h:           sad, cap: "state_count_empty/ip.pcap",
			expected: "type=34 flags=0xffffffff sad_cnt=0 sad_hinfo=8/1048576",
		},
		{
			description: "boundary: ip's count, a GETSPDINFO of only its flags (policy_count)",
			h:           NlMsgHdr{Type: XfrmMsgGetSPDInfo}, cap: "policy_count/ip.pcap", request: true,
			expected: "type=37 flags=0xffffffff",
		},
		{
			description: "positive: the kernel's answer, the counts, the hash table and its thresholds (policy_count)",
			h:           spd, cap: "policy_count/ip.pcap",
			expected: "type=36 flags=0xffffffff spd_info=1/1/0/0/0/0 spd_hinfo=7/1048576 hthresh4=32/32 hthresh6=128/128",
		},
		{
			description: "positive: ip's set, a NEWSPDINFO with only the thresholds (policy_set_hthresh)",
			h:           spd, cap: "policy_set_hthresh/ip.pcap", request: true,
			expected: "type=36 flags=0xffffffff hthresh4=24/16 hthresh6=64/48",
		},
		{
			description: "positive: which read back with them (policy_set_hthresh)",
			h:           spd, cap: "policy_set_hthresh/netlink_xfrm_spd.pcap",
			expected: "type=36 flags=0xffffffff spd_info=0/0/0/0/0/0 spd_hinfo=7/1048576 hthresh4=24/16 hthresh6=64/48",
		},
		{
			description: "corner: every field of the SAD attributes decodes from its own offset",
			h:           sad, body: sadInfo,
			expected: "type=34 flags=0xffffffff sad_cnt=7 sad_hinfo=8/9",
		},
		{
			description: "corner: every field of the SPD info decodes from its own offset",
			h:           spd, body: cat(le32(5), spdInfo, rtattr(XfrmSPDAttrHInfo, cat(le32(7), le32(8))),
				rtattr(XfrmSPDAttrIPv4HThresh, []byte{9, 10}), rtattr(XfrmSPDAttrIPv6HThresh, []byte{11, 12})),
			expected: "type=36 flags=0x5 spd_info=1/2/3/4/5/6 spd_hinfo=7/8 hthresh4=9/10 hthresh6=11/12",
		},
		{
			description: "corner: a GETSPDINFO's attributes are in the SPD's numbering",
			h:           NlMsgHdr{Type: XfrmMsgGetSPDInfo}, body: cat(flags, spdInfo),
			expected: "type=37 flags=0xffffffff spd_info=1/2/3/4/5/6",
		},
		{
			description: "corner: a SADINFO's attribute 1 is a count, not the SPD's info",
			h:           NlMsgHdr{Type: XfrmMsgGetSADInfo}, body: cat(flags, rtattr(XfrmSADAttrCnt, le32(3))),
			expected: "type=35 flags=0xffffffff sad_cnt=3",
		},
		{
			description: "corner: an SPD attribute 3 or 4 is skipped in a SADINFO",
			h:           sad, body: cat(sadInfo, rtattr(XfrmSPDAttrIPv4HThresh, []byte{1, 2})),
			expected: "type=34 flags=0xffffffff sad_cnt=7 sad_hinfo=8/9",
		},
		{
			description: "corner: a repeated threshold keeps the last",
			h:           spd, body: cat(flags, rtattr(XfrmSPDAttrIPv4HThresh, []byte{1, 2}), rtattr(XfrmSPDAttrIPv4HThresh, []byte{3, 4})),
			expected: "type=36 flags=0xffffffff hthresh4=3/4",
		},
		{
			description: "boundary: a threshold of 4 bytes keeps its first two",
			h:           spd, body: cat(flags, rtattr(XfrmSPDAttrIPv6HThresh, []byte{64, 48, 0xff, 0xff})),
			expected: "type=36 flags=0xffffffff hthresh6=64/48",
		},
		{
			description: "negative: an SA type",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, body: flags,
			expected: "type=16 flags=0x0 error: " + ErrXfrmType.Error() + ": 16",
		},
		{
			description: "negative: a GETSADINFO one byte short of its flags",
			h:           NlMsgHdr{Type: XfrmMsgGetSADInfo}, body: flags[:3],
			expected: "type=35 flags=0x0 error: " + ErrXfrmSmall.Error() + ": type 35 needs 4 bytes, has 3",
		},
		{
			description: "negative: a SAD count of 2 bytes, the attributes before it kept",
			h:           sad, body: cat(flags, rtattr(XfrmSADAttrHInfo, cat(le32(8), le32(9))), rtattr(XfrmSADAttrCnt, le32(1)[:2])),
			expected: "type=34 flags=0xffffffff sad_hinfo=8/9 error: " + ErrXfrmAttr.Error() + ": 1 needs 4 bytes, has 2",
		},
		{
			description: "negative: a SAD count of 8 bytes, a u32 being exactly 4",
			h:           sad, body: cat(flags, rtattr(XfrmSADAttrCnt, cat(le32(1), le32(0)))),
			expected: "type=34 flags=0xffffffff error: " + ErrXfrmAttr.Error() + ": 1 needs 4 bytes, has 8",
		},
		{
			description: "negative: a SAD hash info one byte short",
			h:           sad, body: cat(flags, rtattr(XfrmSADAttrHInfo, make([]byte, XfrmSADHInfoSizeCst-1))),
			expected: "type=34 flags=0xffffffff error: " + ErrXfrmAttr.Error() + ": 2 is 7 bytes, under 8",
		},
		{
			description: "negative: an SPD info one byte short",
			h:           spd, body: cat(flags, rtattr(XfrmSPDAttrInfo, make([]byte, XfrmSPDInfoSizeCst-1))),
			expected: "type=36 flags=0xffffffff error: " + ErrXfrmAttr.Error() + ": 1 is 23 bytes, under 24",
		},
		{
			description: "negative: a threshold one byte short",
			h:           spd, body: cat(flags, rtattr(XfrmSPDAttrIPv4HThresh, []byte{32})),
			expected: "type=36 flags=0xffffffff error: " + ErrXfrmAttr.Error() + ": 3 is 1 bytes, under 2",
		},
		{
			description: "negative: a short threshold stops the walk, the one after it not decoded",
			h:           spd, body: cat(flags, rtattr(XfrmSPDAttrIPv4HThresh, []byte{32}), rtattr(XfrmSPDAttrIPv6HThresh, []byte{64, 48})),
			expected: "type=36 flags=0xffffffff error: " + ErrXfrmAttr.Error() + ": 3 is 1 bytes, under 2",
		},
		{
			description: "negative: an attribute running past the body",
			h:           spd, body: cat(flags, []byte{0x40, 0, 1, 0}),
			expected: "type=36 flags=0xffffffff error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			h, body := tt.h, tt.body
			if tt.cap != "" {
				h, body = xfrmCaptured(t, tt.cap, tt.h.Type, tt.request, 0)
			}
			x, err := ParseXfrmInfo(h, body)
			if got := xfrmInfoRender(&x, err); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseXfrmInfoCorpus decodes every SAD and SPD info message in the
// corpus.
func TestParseXfrmInfoCorpus(t *testing.T) {
	tally := func(types []uint16, request bool) string {
		var n, errs, cnt, spdInfo, thresh int
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
			x, err := ParseXfrmInfo(m.h, m.body)
			if err != nil {
				errs++
				continue
			}
			for _, k := range []struct {
				ok bool
				c  *int
			}{
				{x.SADCnt != nil, &cnt}, {x.SPDInfo != nil, &spdInfo}, {x.IPv4HThresh != nil && x.IPv6HThresh != nil, &thresh},
			} {
				if k.ok {
					*k.c++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(cnt) + " sad count, " +
			strconv.Itoa(spdInfo) + " spd info, " + strconv.Itoa(thresh) + " with both thresholds"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's GETSADINFO and GETSPDINFO requests decode, with no attributes", got: tally([]uint16{XfrmMsgGetSADInfo, XfrmMsgGetSPDInfo}, true), expected: "7 decoded, 0 errors, 0 sad count, 0 spd info, 0 with both thresholds"},
		{description: "positive: the kernel's NEWSADINFO answers decode, each with a count", got: tally([]uint16{XfrmMsgNewSADInfo}, false), expected: "3 decoded, 0 errors, 3 sad count, 0 spd info, 0 with both thresholds"},
		{description: "positive: the kernel's NEWSPDINFO answers decode, each with the counts and both thresholds", got: tally([]uint16{XfrmMsgNewSPDInfo}, false), expected: "4 decoded, 0 errors, 0 sad count, 4 spd info, 4 with both thresholds"},
		{description: "positive: ip's NEWSPDINFO set decodes with both thresholds", got: tally([]uint16{XfrmMsgNewSPDInfo}, true), expected: "1 decoded, 0 errors, 0 sad count, 0 spd info, 1 with both thresholds"},
		{description: "negative: ip sends no NEWSADINFO, and the kernel no GETSADINFO or GETSPDINFO", got: tally([]uint16{XfrmMsgNewSADInfo}, true) + "|" + tally([]uint16{XfrmMsgGetSADInfo, XfrmMsgGetSPDInfo}, false), expected: "0 decoded, 0 errors, 0 sad count, 0 spd info, 0 with both thresholds|0 decoded, 0 errors, 0 sad count, 0 spd info, 0 with both thresholds"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
