package gotc

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// fmsg is a filter dump's RTM_NEWTFILTER; chmsg a chain's RTM_NEWCHAIN.
func fmsg(body []byte) Msg {
	return Msg{Type: unix.RTM_NEWTFILTER, Flags: unix.NLM_F_MULTI, Body: body}
}
func chmsg(body []byte) Msg {
	return Msg{Type: unix.RTM_NEWCHAIN, Flags: unix.NLM_F_MULTI, Body: body}
}

// finfo is tcm_info for pref and an ethertype: TC_H_MAKE(prio << 16,
// htons(proto)) read back as the host u32.
func finfo(pref uint32, proto uint16) uint32 { return pref<<16 | uint32(proto>>8|proto<<8) }

func nest(t uint16, parts ...[]byte) []byte { return attr(t|unix.NLA_F_NESTED, cat(parts...)) }

// gactAct is an action table holding one gact of action, with tm as
// TCA_GACT_TM when given.
func gactAct(action uint32, tm ...uint64) []byte {
	parms := cat(le32(1), le32(0), le32(action), le32(1), le32(1))
	opts := attr(xtcpnl.TcaGactParms, parms)
	if len(tm) > 0 {
		b := make([]byte, 0, 8*len(tm))
		for _, v := range tm {
			b = append(b, le64(v)...)
		}
		opts = cat(attr(xtcpnl.TcaGactTm, b), opts)
	}
	return nest(1, attr(xtcpnl.TcaActKind, str0("gact")), nest(xtcpnl.TcaActOptions, opts))
}

// u32Key is a struct tc_u32_key: mask and val big-endian, off host.
func u32KeyB(mask, val uint32, off int32) []byte {
	return cat(binary.BigEndian.AppendUint32(nil, mask), binary.BigEndian.AppendUint32(nil, val),
		le32(uint32(off)), le32(0))
}

// u32SelB is a struct tc_u32_sel and its keys.
func u32SelB(flags, nkeys uint8, extra func([]byte), keys ...[]byte) []byte {
	b := make([]byte, 16)
	b[0], b[2] = flags, nkeys
	if extra != nil {
		extra(b)
	}
	return cat(append([][]byte{b}, keys...)...)
}

// cmpEm is one cmp ematch: struct tcf_ematch_hdr, then struct tcf_em_cmp.
func cmpEm(flags uint16, val uint32) []byte {
	hdr := cat(le16(0), le16(xtcpnl.TcfEmKindCmp), le16(flags), le16(0))
	return cat(hdr, le32(val), le32(0xff), le16(0), []byte{0x02, 0x01})
}

func le16(v uint16) []byte { return binary.LittleEndian.AppendUint16(nil, v) }

// ematchTree is a basic filter's TCA_BASIC_EMATCHES of matches.
func ematchTree(matches ...[]byte) []byte {
	list := make([]byte, 0, 64*len(matches))
	idx := uint16(1)
	for _, m := range matches {
		list = append(list, attr(idx, m)...)
		idx++
	}
	return nest(xtcpnl.TcaBasicEmatches, attr(xtcpnl.TcaEmatchTreeHdr, cat(le16(idx-1), le16(0))),
		nest(xtcpnl.TcaEmatchTreeList, list))
}

// shareSys is a root whose CONF_USR_DIR has the workbench tc's ematch_map.
func shareSys(t *testing.T) sysFS {
	t.Helper()
	s := emptySys(t)
	b, err := os.ReadFile(filepath.Join("testdata", "share-iproute2", ematchMapName))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, s.root, map[string]string{filepath.Join(confUsrDirCst, ematchMapName): string(b)})
	return s
}

// TestGotcFilterUnit runs tc_filter_list, tc_filter_get and print_filter,
// with each kind's printer, on scripted replies.
//
// go test ./internal/gotc/ -run TestGotcFilterUnit
func TestGotcFilterUnit(t *testing.T) {
	d0 := rep(linkMsg(2, "d0"))
	ing := func(handle, pref uint32, proto uint16, k string, attrs ...[]byte) Msg {
		return fmsg(tcBody(2, handle, tcHClsactIngress, finfo(pref, proto),
			append([][]byte{kind(k), attr(xtcpnl.TcaChain, le32(0))}, attrs...)...))
	}
	matchall := func(flags uint32, act ...[]byte) Msg {
		opts := []byte{}
		if flags != 0 {
			opts = attr(xtcpnl.TcaMatchallFlags, le32(flags))
		}
		if len(act) > 0 {
			opts = cat(opts, nest(xtcpnl.TcaMatchallAct, act...))
		}
		return ing(1, 3, 0x0003, "matchall", nest(xtcpnl.TcaOptions, opts))
	}
	twoKeys := nest(xtcpnl.TcaOptions, attr(xtcpnl.TcaU32Hash, le32(0x80000000)),
		attr(xtcpnl.TcaU32Sel, u32SelB(xtcpnl.TcU32Terminal, 2, nil,
			u32KeyB(0xff000000, 0x0a000000, 12), u32KeyB(0xffff, 80, 20))))
	eat := nest(xtcpnl.TcaOptions, attr(xtcpnl.TcaU32Hash, le32(0x80000000)),
		attr(xtcpnl.TcaU32Sel, u32SelB(xtcpnl.TcU32VarOffset|xtcpnl.TcU32Eat, 0, func(b []byte) {
			b[1], b[4], b[5] = 6, 0x0f, 0x00 // offshift 6, offmask 0x0f00
		})))
	tests := []struct {
		description string
		args        []string
		stdin       string
		sys         func(*testing.T) sysFS
		replies     []Reply
		info        uint32 // when non-zero, the last request's tcm_info
		expected    result
	}{
		{
			description: "positive: the header line",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(ing(0, 1, ethPIP, "u32"), done())},
			expected:    result{stdout: "filter protocol ip pref 1 u32 chain 0 \n"},
		},
		{
			description: "positive: a gact action block",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(matchall(0, gactAct(0)), done())},
			expected: result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n" +
				"\taction order 1: gact action pass\n\t random type none pass val 0\n\t index 1 ref 1 bind 1\n\n"},
		},
		{
			description: "positive: an ematch's text as C prints it",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			sys:         shareSys,
			replies: []Reply{d0, rep(ing(1, 1, 0x0003, "basic",
				nest(xtcpnl.TcaOptions, ematchTree(cmpEm(0, 8)))), done())},
			expected: result{stdout: "filter protocol all pref 1 basic chain 0 handle 0x1 \n  cmp(u16 at 0 layer 1 mask 0xff eq 8)\n\n"},
		},
		{
			description: "positive: -s gact's timers over USER_HZ",
			args:        []string{"-s", "filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(matchall(0, gactAct(2, 500, 300, 0, 0)), done())},
			expected: result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n" +
				"\taction order 1: gact action drop\n\t random type none pass val 0\n\t index 1 ref 1 bind 1 installed 5 sec used 3 sec\n\n"},
		},
		{
			description: "negative: show pref 70000 asks for pref 0x1170 (L40)",
			args:        []string{"filter", "show", "dev", "d0", "ingress", "pref", "70000"},
			replies:     []Reply{d0, rep(done())},
			info:        0x11700000,
			expected:    result{},
		},
		{
			description: "negative: get with no protocol",
			args:        []string{"filter", "get", "dev", "d0", "ingress", "pref", "1", "handle", "1", "matchall"},
			expected:    result{stderr: "Must specify filter protocol\n", rc: 255},
		},
		{
			description: "negative: get of an invalid u32 handle, after the link lookup (F15)",
			args:        []string{"filter", "get", "dev", "d0", "ingress", "protocol", "ip", "pref", "1", "handle", "zz", "u32"},
			replies:     []Reply{d0},
			expected:    result{stderr: "Illegal filter ID\n", rc: 1},
		},
		{
			description: "negative: get the kernel refuses is 2 (F15)",
			args:        []string{"filter", "get", "dev", "d0", "ingress", "protocol", "all", "pref", "3", "handle", "9", "matchall"},
			replies:     []Reply{d0, rep(nlErr(unix.ENOENT))},
			expected:    result{stderr: "RTNETLINK answers: No such file or directory\nWe have an error talking to the kernel\n", rc: 2},
		},
		{
			description: "corner: a pedit with no options in a matchall prints its kind alone",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(matchall(0, nest(1, attr(xtcpnl.TcaActKind, str0("pedit")))), done())},
			expected:    result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n\taction order 1:  pedit \n"},
		},
		{
			description: "boundary: a bpf filter with empty options prints its handle alone (AC66)",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(ing(1, 2, ethPIP, "bpf", nest(xtcpnl.TcaOptions)), done())},
			expected:    result{stdout: "filter protocol ip pref 2 bpf chain 0 handle 0x1 \n"},
		},
		{
			description: "boundary: no dev is ifindex 0, an empty dump (F8)",
			args:        []string{"filter", "show"},
			replies:     []Reply{links(), rep(done())},
			expected:    result{},
		},
		{
			description: "boundary: cmp eq 0x80000000 prints signed (F10)",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			sys:         shareSys,
			replies: []Reply{d0, rep(ing(4, 4, 0x0003, "basic",
				nest(xtcpnl.TcaOptions, ematchTree(cmpEm(0, 0x80000000)))), done())},
			expected: result{stdout: "filter protocol all pref 4 basic chain 0 handle 0x4 \n  cmp(u16 at 0 layer 1 mask 0xff eq -2147483648)\n\n"},
		},
		{
			description: "boundary: a short u32 SEL stops the printer after its handle (F17)",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, rep(ing(0x80000001, 1, ethPIP, "u32",
				nest(xtcpnl.TcaOptions, attr(xtcpnl.TcaU32Sel, make([]byte, 15)), gactAct(0))), done())},
			expected: result{stdout: "filter protocol ip pref 1 u32 chain 0 fh 800::1 order 1 \n"},
		},
		{
			description: "corner: -j u32's two keys are two \"match\" keys (L2)",
			args:        []string{"-j", "filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(ing(0x80000800, 2, ethPIP, "u32", twoKeys), done())},
			expected: result{stdout: `[{"protocol":"ip","pref":2,"kind":"u32","chain":0,"options":{"fh":"800::800","order":2048,` +
				`"key_ht":"800","bkt":"0","match":{"value":"a000000","mask":"ff000000","offmask":"","off":12},` +
				`"match":{"value":"50","mask":"ffff","offmask":"","off":20}}}]` + "\n"},
		},
		{
			description: "corner: -j eat is a string with no key (L1)",
			args:        []string{"-j", "filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(ing(0x80000800, 1, ethPIP, "u32", eat), done())},
			expected: result{stdout: `[{"protocol":"ip","pref":1,"kind":"u32","chain":0,"options":{"fh":"800::800","order":2048,` +
				`"key_ht":"800","bkt":"0","offset_mask":"f00","offset_shift":6,"offset_off":0," eat "}}]` + "\n"},
		},
		{
			description: "corner: -j route's to and from are both \"name\" (F3)",
			args:        []string{"-j", "filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, rep(ing(0x10002, 20, ethPIP, "route", nest(xtcpnl.TcaOptions,
				attr(xtcpnl.TcaRoute4To, le32(2)), attr(xtcpnl.TcaRoute4From, le32(1)))), done())},
			expected: result{stdout: `[{"protocol":"ip","pref":20,"kind":"route","chain":0,"options":{"fh":"0x10002","name":"2","name":"1"}}]` + "\n"},
		},
		{
			description: "corner: a chain line every field of which is filtered (F22)",
			args:        []string{"chain", "show", "dev", "d0", "ingress", "chain", "5"},
			replies:     []Reply{d0, rep(chmsg(tcBody(2, 0, tcHClsactIngress, 0, attr(xtcpnl.TcaChain, le32(5)))), done())},
			expected:    result{stdout: "chain \n"},
		},
		{
			description: "corner: a second chain in a batch is a duparg (Q29f)",
			args:        []string{"-batch", "-"},
			stdin:       "filter show dev d0 ingress chain 5\nfilter show dev d0 ingress chain 5\n",
			replies:     []Reply{links(), rep(done())},
			expected:    result{stderr: "Error: duplicate \"chain\": \"5\" is the second value.\n", rc: 255},
		},
		{
			description: "corner: -o keeps matchall's hard newline (L47)",
			args:        []string{"-o", "filter", "show", "dev", "d0", "ingress"},
			replies:     []Reply{d0, rep(matchall(xtcpnl.TcaClsFlagsSkipHw|xtcpnl.TcaClsFlagsNotInHw), done())},
			expected:    result{stdout: "filter protocol all pref 3 matchall chain 0 handle 0x1 \n  skip_hw\n  not_in_hw\\"},
		},
		{
			description: "corner: no ematch_map names no ematch",
			args:        []string{"filter", "show", "dev", "d0", "ingress"},
			replies: []Reply{d0, rep(ing(1, 1, 0x0003, "basic",
				nest(xtcpnl.TcaOptions, ematchTree(cmpEm(0, 8)))), done())},
			expected: result{stdout: "filter protocol all pref 1 basic chain 0 handle 0x1 \n  [unknown ematch 1]\n\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			sys := emptySys
			if tc.sys != nil {
				sys = tc.sys
			}
			cfg := config{src: src, stdin: strings.NewReader(tc.stdin), sys: sys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
			if tc.info != 0 {
				last := src.reqs[len(src.reqs)-1]
				if info := binary.LittleEndian.Uint32(last[nlHeaderLen+16:]); info != tc.info {
					t.Errorf("tcm_info %#x, expected %#x", info, tc.info)
				}
			}
		})
	}
}
