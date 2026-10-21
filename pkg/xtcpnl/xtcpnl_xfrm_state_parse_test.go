package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// xfrmRAddr renders an xfrm_address_t as family has it, else 0 when it is
// all zeros, else in hex.
func xfrmRAddr(a XfrmAddress, family uint16) string {
	if ip := a.Addr(family); ip.IsValid() {
		return ip.String()
	}
	if a == (XfrmAddress{}) {
		return "0"
	}
	return hex.EncodeToString(a[:])
}

// xfrmRU64 renders a lifetime value, the kernel's infinity as inf.
func xfrmRU64(v uint64) string {
	if v == ^uint64(0) {
		return "inf"
	}
	return strconv.FormatUint(v, 10)
}

func xfrmRSel(s *XfrmSelector) string {
	return "sel{" + xfrmRAddr(s.Saddr, s.Family) + "/" + strconv.Itoa(int(s.PrefixlenS)) + "-" +
		xfrmRAddr(s.Daddr, s.Family) + "/" + strconv.Itoa(int(s.PrefixlenD)) +
		" dport=" + strconv.Itoa(int(s.Dport)) + "/" + strconv.Itoa(int(s.DportMask)) +
		" sport=" + strconv.Itoa(int(s.Sport)) + "/" + strconv.Itoa(int(s.SportMask)) +
		" family=" + strconv.Itoa(int(s.Family)) + " proto=" + strconv.Itoa(int(s.Proto)) +
		" ifindex=" + strconv.Itoa(int(s.Ifindex)) + " user=" + u32s(s.User) + "}"
}

func xfrmRCur(c *XfrmLifetimeCur) string {
	return strconv.FormatUint(c.Bytes, 10) + "B/" + strconv.FormatUint(c.Packets, 10) + "P/" +
		strconv.FormatUint(c.AddTime, 10) + "/" + strconv.FormatUint(c.UseTime, 10)
}

func xfrmRLft(l *XfrmLifetimeCfg) string {
	return strings.Join([]string{
		xfrmRU64(l.SoftByteLimit), xfrmRU64(l.HardByteLimit), xfrmRU64(l.SoftPacketLimit), xfrmRU64(l.HardPacketLimit),
		xfrmRU64(l.SoftAddExpiresSeconds), xfrmRU64(l.HardAddExpiresSeconds),
		xfrmRU64(l.SoftUseExpiresSeconds), xfrmRU64(l.HardUseExpiresSeconds),
	}, ",")
}

func xfrmRInfo(s *XfrmUsersaInfo) string {
	return "info{dst=" + xfrmRAddr(s.ID.Daddr, s.Family) + " spi=0x" + strconv.FormatUint(uint64(s.ID.SPI), 16) +
		" proto=" + strconv.Itoa(int(s.ID.Proto)) + " src=" + xfrmRAddr(s.Saddr, s.Family) +
		" family=" + strconv.Itoa(int(s.Family)) + " mode=" + strconv.Itoa(int(s.Mode)) + " reqid=" + u32s(s.Reqid) +
		" seq=" + u32s(s.Seq) + " replay_window=" + strconv.Itoa(int(s.ReplayWindow)) +
		" flags=" + strconv.Itoa(int(s.Flags)) + " " + xfrmRSel(&s.Sel) +
		" lft=" + xfrmRLft(&s.Lft) + " cur=" + xfrmRCur(&s.Curlft) +
		" stats=" + u32s(s.Stats.ReplayWindow) + "/" + u32s(s.Stats.Replay) + "/" + u32s(s.Stats.IntegrityFailed) + "}"
}

func xfrmRID(id *XfrmUsersaID) string {
	return "id{dst=" + xfrmRAddr(id.Daddr, id.Family) + " spi=0x" + strconv.FormatUint(uint64(id.SPI), 16) +
		" family=" + strconv.Itoa(int(id.Family)) + " proto=" + strconv.Itoa(int(id.Proto)) + "}"
}

func xfrmRAlgo(a *XfrmAlgo) string {
	return a.Name + "/" + u32s(a.KeyBits) + "/" + u32s(a.ICVTruncBits) + ":" + xfrmKey(a.Key)
}

// xfrmRAttrs renders the attributes that are set, in a fixed order, the
// addresses in them as family has them.
func xfrmRAttrs(a *XfrmAttrs, family uint16) []string {
	var w []string
	add := func(ok bool, s func() string) {
		if ok {
			w = append(w, s())
		}
	}
	add(a.AlgAuth != nil, func() string { return "auth=" + xfrmRAlgo(a.AlgAuth) })
	add(a.AlgCrypt != nil, func() string { return "enc=" + xfrmRAlgo(a.AlgCrypt) })
	add(a.AlgComp != nil, func() string { return "comp=" + xfrmRAlgo(a.AlgComp) })
	add(a.AlgAEAD != nil, func() string { return "aead=" + xfrmRAlgo(a.AlgAEAD) })
	add(a.AlgAuthTrunc != nil, func() string { return "auth_trunc=" + xfrmRAlgo(a.AlgAuthTrunc) })
	add(a.Encap != nil, func() string {
		return "encap=" + strconv.Itoa(int(a.Encap.Type)) + "/" + strconv.Itoa(int(a.Encap.Sport)) + "-" +
			strconv.Itoa(int(a.Encap.Dport)) + "/" + xfrmRAddr(a.Encap.OA, family)
	})
	add(a.SA != nil, func() string { return "sa=" + xfrmRInfo(a.SA) })
	for i := range a.Tmpl {
		w = append(w, xfrmRTmpl(&a.Tmpl[i]))
	}
	add(a.Policy != nil, func() string { return "policy=" + xfrmRPol(a.Policy) })
	add(a.PolicyType != nil, func() string { return "ptype=" + strconv.Itoa(int(*a.PolicyType)) })
	add(a.SecCtx != nil, func() string {
		return "sec_ctx=" + strconv.Itoa(int(a.SecCtx.ExtType)) + "/" + strconv.Itoa(int(a.SecCtx.Alg)) + "/" +
			strconv.Itoa(int(a.SecCtx.DOI)) + ":" + string(a.SecCtx.Ctx)
	})
	add(a.LtimeVal != nil, func() string { return "ltime=" + xfrmRCur(a.LtimeVal) })
	add(a.ReplayVal != nil, func() string {
		return "replay=" + u32s(a.ReplayVal.OSeq) + "/" + u32s(a.ReplayVal.Seq) + "/0x" +
			strconv.FormatUint(uint64(a.ReplayVal.Bitmap), 16)
	})
	add(a.ReplayESNVal != nil, func() string {
		e := a.ReplayESNVal
		bmp := make([]string, 0, len(e.Bmp))
		for _, v := range e.Bmp {
			bmp = append(bmp, "0x"+strconv.FormatUint(uint64(v), 16))
		}
		return "esn=" + u32s(e.OSeq) + "/" + u32s(e.Seq) + "/" + u32s(e.OSeqHi) + "/" + u32s(e.SeqHi) + "/" +
			u32s(e.ReplayWindow) + "[" + strings.Join(bmp, ",") + "]"
	})
	add(a.SrcAddr != nil, func() string { return "srcaddr=" + xfrmRAddr(*a.SrcAddr, family) })
	add(a.CoAddr != nil, func() string { return "coaddr=" + xfrmRAddr(*a.CoAddr, family) })
	add(a.LastUsed != nil, func() string { return "lastused=" + strconv.FormatUint(*a.LastUsed, 10) })
	add(a.Mark != nil, func() string { return "mark=" + u32s(a.Mark.V) + "/0x" + strconv.FormatUint(uint64(a.Mark.M), 16) })
	add(a.Proto != nil, func() string { return "proto=" + strconv.Itoa(int(*a.Proto)) })
	add(a.SADir != nil, func() string { return "sa_dir=" + strconv.Itoa(int(*a.SADir)) })
	add(a.Filter != nil, func() string {
		f := a.Filter
		return "filter=" + xfrmRAddr(f.Saddr, f.Family) + "/" + strconv.Itoa(int(f.SPLen)) + "-" +
			xfrmRAddr(f.Daddr, f.Family) + "/" + strconv.Itoa(int(f.DPLen)) + ",family=" + strconv.Itoa(int(f.Family))
	})
	add(a.OffloadDev != nil, func() string {
		return "offload=" + strconv.Itoa(int(a.OffloadDev.Ifindex)) + "/" + strconv.Itoa(int(a.OffloadDev.Flags))
	})
	add(a.IPTFSReorderWindow != nil, func() string { return "iptfs_reorder=" + strconv.Itoa(int(*a.IPTFSReorderWindow)) })
	add(a.IPTFSDontFrag, func() string { return "iptfs_dont_frag" })
	for _, u := range []struct {
		name string
		v    *uint32
	}{
		{"replay_thresh", a.ReplayThresh}, {"etimer_thresh", a.EtimerThresh}, {"tfcpad", a.TFCPad},
		{"extra_flags", a.ExtraFlags}, {"set_mark", a.SetMark}, {"set_mark_mask", a.SetMarkMask},
		{"if_id", a.IfID}, {"mtimer_thresh", a.MtimerThresh}, {"nat_keepalive", a.NATKeepaliveInterval},
		{"sa_pcpu", a.SAPcpu}, {"iptfs_drop_time", a.IPTFSDropTime}, {"iptfs_init_delay", a.IPTFSInitDelay},
		{"iptfs_max_qsize", a.IPTFSMaxQSize}, {"iptfs_pkt_size", a.IPTFSPktSize},
	} {
		add(u.v != nil, func() string { return u.name + "=" + u32s(*u.v) })
	}
	return w
}

// xfrmStateRender renders a decoded SA message: its type, then whichever
// struct it carries, then its attributes, then the error, if any.
func xfrmStateRender(s *XfrmState, err error) string {
	w := []string{"type=" + strconv.Itoa(int(s.Type))}
	var family uint16
	if s.Dump {
		w = append(w, "dump")
	}
	if s.Info != nil {
		w = append(w, xfrmRInfo(s.Info))
		family = s.Info.Family
	}
	if s.ID != nil {
		w = append(w, xfrmRID(s.ID))
		family = s.ID.Family
	}
	if s.FlushProto != nil {
		w = append(w, "flush_proto="+strconv.Itoa(int(*s.FlushProto)))
	}
	if s.AEvent != nil {
		e := s.AEvent
		w = append(w, "ae{"+xfrmRID(&e.SAID)+" src="+xfrmRAddr(e.Saddr, e.SAID.Family)+
			" flags=0x"+strconv.FormatUint(uint64(e.Flags), 16)+" reqid="+u32s(e.Reqid)+"}")
		family = e.SAID.Family
	}
	w = append(w, xfrmRAttrs(&s.Attrs, family)...)
	if err != nil {
		w = append(w, "error: "+err.Error())
	}
	return strings.Join(w, " ")
}

// xfrmCaptured is the nth message of type typ in an XFRM fixture pcap, a
// request when request is true, else one the kernel sent.
func xfrmCaptured(t *testing.T, rel string, typ uint16, request bool, nth int) (NlMsgHdr, []byte) {
	t.Helper()
	for _, m := range pcapNlMsgs(t, tdXfrmWrite_7_1_4+"/"+rel) {
		if m.h.Type != typ || (m.h.Flags&unix.NLM_F_REQUEST != 0) != request {
			continue
		}
		if nth == 0 {
			return m.h, m.body
		}
		nth--
	}
	t.Fatalf("%s holds no such message", rel)
	return NlMsgHdr{}, nil
}

// xfrmTestInfo is a hand-built xfrm_usersa_info whose every field is
// distinct, so a field read from the wrong offset shows.
func xfrmTestInfo() []byte {
	b := make([]byte, XfrmUsersaInfoSizeCst)
	copy(b[0:], []byte{198, 51, 100, 0}) // sel.daddr
	copy(b[16:], []byte{203, 0, 113, 0}) // sel.saddr
	binary.BigEndian.PutUint16(b[32:], 443)
	binary.BigEndian.PutUint16(b[34:], 0xffff)
	binary.BigEndian.PutUint16(b[36:], 1024)
	binary.BigEndian.PutUint16(b[38:], 0xff00)
	binary.LittleEndian.PutUint16(b[40:], unix.AF_INET)
	b[42], b[43], b[44] = 24, 25, unix.IPPROTO_TCP
	binary.LittleEndian.PutUint32(b[48:], 7)
	binary.LittleEndian.PutUint32(b[52:], 1000)
	copy(b[56:], []byte{192, 0, 2, 2}) // id.daddr
	binary.BigEndian.PutUint32(b[72:], 0xc0ffee01)
	b[76] = unix.IPPROTO_ESP
	copy(b[80:], []byte{192, 0, 2, 1}) // saddr
	for i := range 8 {
		binary.LittleEndian.PutUint64(b[96+8*i:], uint64(101+i))
	}
	binary.LittleEndian.PutUint64(b[104:], ^uint64(0))
	for i := range 4 {
		binary.LittleEndian.PutUint64(b[160+8*i:], uint64(201+i))
	}
	binary.LittleEndian.PutUint32(b[192:], 301)
	binary.LittleEndian.PutUint32(b[196:], 302)
	binary.LittleEndian.PutUint32(b[200:], 303)
	binary.LittleEndian.PutUint32(b[204:], 401)
	binary.LittleEndian.PutUint32(b[208:], 402)
	binary.LittleEndian.PutUint16(b[212:], unix.AF_INET)
	b[214], b[215], b[216] = 1, 32, 8
	return b
}

// xfrmTestID is a hand-built xfrm_usersa_id for 2001:db8::9.
func xfrmTestID() []byte {
	b := make([]byte, XfrmUsersaIDSizeCst)
	copy(b, []byte{0x20, 0x01, 0x0d, 0xb8, 15: 9})
	binary.BigEndian.PutUint32(b[16:], 0x1234)
	binary.LittleEndian.PutUint16(b[20:], unix.AF_INET6)
	b[22] = unix.IPPROTO_AH
	return b
}

// xfrmTestAlgo is an xfrm_algo, or with icv set an xfrm_algo_aead or
// xfrm_algo_auth, holding key.
func xfrmTestAlgo(name string, bits uint32, icv *uint32, key []byte) []byte {
	b := make([]byte, xfrmAlgoNameLen)
	copy(b, name)
	b = append(b, le32(bits)...)
	if icv != nil {
		b = append(b, le32(*icv)...)
	}
	return append(b, key...)
}

func TestParseXfrmState(t *testing.T) {
	info := xfrmTestInfo()
	infoText := "info{dst=192.0.2.2 spi=0xc0ffee01 proto=50 src=192.0.2.1 family=2 mode=1 reqid=402 seq=401 replay_window=32 flags=8" +
		" sel{203.0.113.0/25-198.51.100.0/24 dport=443/65535 sport=1024/65280 family=2 proto=6 ifindex=7 user=1000}" +
		" lft=101,inf,103,104,105,106,107,108 cur=201B/202P/203/204 stats=301/302/303}"
	sa := func(attrs ...[]byte) []byte { return cat(append([][]byte{info}, attrs...)...) }
	newsa := NlMsgHdr{Type: XfrmMsgNewSA}
	u32 := func(id uint16, v uint32) []byte { return rtattr(id, le32(v)) }
	icv := uint32(96)
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
			description: "positive: ip's add, a NEWSA by compat names (state_add)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "state_add/ip.pcap", request: true,
			expected: "type=16 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0" +
				" sel{0/0-0/0 dport=0/0 sport=0/0 family=0 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/0/0 stats=0/0/0}" +
				" auth=sha256/256/0:11*32 enc=aes/128/0:22*16",
		},
		{
			description: "positive: the kernel's dump of it, by its own names, with the replay state (state_add)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "state_add/netlink_xfrm_state.pcap",
			expected: "type=16 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792526876/0 stats=0/0/0} auth=hmac(sha256)/256/0:11*32 enc=cbc(aes)/128/0:22*16 auth_trunc=hmac(sha256)/256/96:11*32 replay=0/0/0x0",
		},
		{
			description: "positive: an IPv6 tunnel SA, read back (state_add_v6_tunnel)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "state_add_v6_tunnel/netlink_xfrm_state.pcap",
			expected: "type=16 info{dst=2001:db8::2 spi=0x101 proto=50 src=2001:db8::1 family=10 mode=1 reqid=2 seq=0 replay_window=0 flags=0 sel{::/0-::/0 dport=0/0 sport=0/0 family=10 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792526878/0 stats=0/0/0} auth=hmac(sha256)/256/0:11*32 enc=cbc(aes)/128/0:22*16 auth_trunc=hmac(sha256)/256/128:11*32 replay=0/0/0x0",
		},
		{
			description: "positive: an AEAD SA, read back (state_add_aead)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "state_add_aead/netlink_xfrm_state.pcap",
			expected: "type=16 info{dst=192.0.2.3 spi=0x102 proto=50 src=192.0.2.1 family=2 mode=0 reqid=3 seq=0 replay_window=0 flags=0 sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792526880/0 stats=0/0/0} aead=rfc4106(gcm(aes))/160/128:33*20 replay=0/0/0x0",
		},
		{
			description: "positive: ip's NEWSA with UDP encapsulation (monitor_mapping)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "monitor_mapping/ip.pcap", request: true,
			expected: "type=16 info{dst=192.0.2.2 spi=0x300 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel{0/0-0/0 dport=0/0 sport=0/0 family=0 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/0/0 stats=0/0/0} enc=cipher_null/0/0: encap=2/4500-4500/0.0.0.0",
		},
		{
			description: "positive: the kernel's NEWSA event for it (monitor_mapping)",
			h:           NlMsgHdr{Type: XfrmMsgNewSA}, cap: "monitor_mapping/ip.pcap",
			expected: "type=16 info{dst=192.0.2.2 spi=0x300 proto=50 src=192.0.2.1 family=2 mode=0 reqid=0 seq=0 replay_window=0 flags=0 sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536623/0 stats=0/0/0} enc=ecb(cipher_null)/0/0: encap=2/4500-4500/0.0.0.0 replay=0/0/0x0",
		},
		{
			description: "positive: ip's update, an UPDSA with a hard add limit (state_update)",
			h:           NlMsgHdr{Type: XfrmMsgUpdSA}, cap: "state_update/ip.pcap", request: true,
			expected: "type=26 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel{0/0-0/0 dport=0/0 sport=0/0 family=0 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,3600,0,0 cur=0B/0P/0/0 stats=0/0/0} auth=sha256/256/0:11*32 enc=aes/128/0:22*16",
		},
		{
			description: "positive: the kernel's UPDSA event (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgUpdSA}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=26 info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536608/0 stats=0/0/0} auth=hmac(sha256)/256/0:11*32 enc=cbc(aes)/128/0:22*16 auth_trunc=hmac(sha256)/256/96:11*32 replay=0/0/0x0",
		},
		{
			description: "positive: ip's get, a GETSA by id with the source as an attribute (state_get)",
			h:           NlMsgHdr{Type: XfrmMsgGetSA}, cap: "state_get/ip.pcap", request: true,
			expected: "type=18 id{dst=192.0.2.2 spi=0x100 family=2 proto=50} srcaddr=192.0.2.1",
		},
		{
			description: "positive: ip's list dst, a GETSA dump of an address filter alone (state_list_dst)",
			h:           NlMsgHdr{Type: XfrmMsgGetSA}, cap: "state_list_dst/ip.pcap", request: true,
			expected: "type=18 dump filter=0.0.0.0/0-192.0.2.2/32,family=2",
		},
		{
			description: "boundary: deleteall's GETSA dump has an empty body (state_deleteall)",
			h:           NlMsgHdr{Type: XfrmMsgGetSA}, cap: "state_deleteall/ip.pcap", request: true,
			expected: "type=18 dump",
		},
		{
			description: "positive: ip's delete, a DELSA by id (state_delete)",
			h:           NlMsgHdr{Type: XfrmMsgDelSA}, cap: "state_delete/ip.pcap", request: true,
			expected: "type=17 id{dst=192.0.2.2 spi=0x100 family=2 proto=50} srcaddr=192.0.2.1",
		},
		{
			description: "positive: the kernel's DELSA event nests the deleted SA (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgDelSA}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=17 id{dst=192.0.2.2 spi=0x100 family=2 proto=50} auth=hmac(sha256)/256/0:11*32 enc=cbc(aes)/128/0:22*16 auth_trunc=hmac(sha256)/256/96:11*32 sa=info{dst=192.0.2.2 spi=0x100 proto=50 src=192.0.2.1 family=2 mode=0 reqid=1 seq=0 replay_window=0 flags=0 sel{0.0.0.0/0-0.0.0.0/0 dport=0/0 sport=0/0 family=2 proto=0 ifindex=0 user=0} lft=inf,inf,inf,inf,0,0,0,0 cur=0B/0P/1792536608/0 stats=0/0/0} replay=0/0/0x0",
		},
		{
			description: "positive: ip's flush, a FLUSHSA of a single byte (state_flush)",
			h:           NlMsgHdr{Type: XfrmMsgFlushSA}, cap: "state_flush/ip.pcap", request: true,
			expected: "type=28 flush_proto=0",
		},
		{
			description: "positive: the kernel's FLUSHSA event, padded to 4 (monitor_sa_policy)",
			h:           NlMsgHdr{Type: XfrmMsgFlushSA}, cap: "monitor_sa_policy/ip.pcap",
			expected: "type=28 flush_proto=0",
		},
		{
			description: "positive: the kernel's NEWAE events, a replay update then a timer one (monitor_aevent)",
			h:           NlMsgHdr{Type: XfrmMsgNewAE}, cap: "monitor_aevent/ip.pcap",
			expected: "type=30 ae{id{dst=192.0.2.2 spi=0x100 family=2 proto=50} src=192.0.2.1 flags=0x20 reqid=1} ltime=0B/0P/1792536619/1792536621 replay=1/0/0x0",
		},
		{
			description: "positive: and the second (monitor_aevent)",
			h:           NlMsgHdr{Type: XfrmMsgNewAE}, cap: "monitor_aevent/ip.pcap", nth: 1,
			expected: "type=30 ae{id{dst=192.0.2.2 spi=0x100 family=2 proto=50} src=192.0.2.1 flags=0x10 reqid=1} ltime=20B/2P/1792536619/1792536621 replay=3/0/0x0",
		},
		{
			description: "corner: every field of the usersa_info decodes from its own offset",
			h:           newsa, body: info,
			expected: "type=16 " + infoText,
		},
		{
			description: "corner: a GETAE has the NEWAE's body",
			h:           NlMsgHdr{Type: XfrmMsgGetAE},
			body:        cat(xfrmTestID(), []byte{0x20, 0x01, 0x0d, 0xb8, 15: 8}, le32(4), le32(77)),
			expected:    "type=31 ae{id{dst=2001:db8::9 spi=0x1234 family=10 proto=51} src=2001:db8::8 flags=0x4 reqid=77}",
		},
		{
			description: "corner: a GETSA without NLM_F_DUMP's both bits is not a dump",
			h:           NlMsgHdr{Type: XfrmMsgGetSA, Flags: unix.NLM_F_REQUEST | unix.NLM_F_ROOT},
			body:        xfrmTestID(),
			expected:    "type=18 id{dst=2001:db8::9 spi=0x1234 family=10 proto=51}",
		},
		{
			description: "corner: a DELSA with NLM_F_DUMP still has its id",
			h:           NlMsgHdr{Type: XfrmMsgDelSA, Flags: unix.NLM_F_REQUEST | unix.NLM_F_DUMP},
			body:        xfrmTestID(),
			expected:    "type=17 id{dst=2001:db8::9 spi=0x1234 family=10 proto=51}",
		},
		{
			description: "corner: a FLUSHSA's attributes start at the struct's aligned end",
			h:           NlMsgHdr{Type: XfrmMsgFlushSA},
			body:        cat([]byte{unix.IPPROTO_AH, 0xff, 0xff, 0xff}, u32(XfrmAttrIfID, 9)),
			expected:    "type=28 flush_proto=51 if_id=9",
		},
		{
			description: "corner: every u32 attribute decodes into its own field",
			h:           newsa,
			body: sa(u32(XfrmAttrReplayThresh, 1), u32(XfrmAttrEtimerThresh, 2), u32(XfrmAttrTFCPad, 3),
				u32(XfrmAttrSAExtraFlags, 4), u32(XfrmAttrSetMark, 5), u32(XfrmAttrSetMarkMask, 6), u32(XfrmAttrIfID, 7),
				u32(XfrmAttrMtimerThresh, 8), u32(XfrmAttrNATKeepaliveInterval, 9), u32(XfrmAttrSAPcpu, 10),
				u32(XfrmAttrIPTFSDropTime, 11), u32(XfrmAttrIPTFSInitDelay, 12), u32(XfrmAttrIPTFSMaxQSize, 13),
				u32(XfrmAttrIPTFSPktSize, 14)),
			expected: "type=16 " + infoText + " replay_thresh=1 etimer_thresh=2 tfcpad=3 extra_flags=4 set_mark=5 set_mark_mask=6" +
				" if_id=7 mtimer_thresh=8 nat_keepalive=9 sa_pcpu=10 iptfs_drop_time=11 iptfs_init_delay=12 iptfs_max_qsize=13 iptfs_pkt_size=14",
		},
		{
			description: "corner: every other scalar and struct attribute decodes",
			h:           newsa,
			body: sa(rtattr(XfrmAttrAlgComp, xfrmTestAlgo("deflate", 0, nil, nil)),
				rtattr(XfrmAttrAlgAuthTrunc, xfrmTestAlgo("hmac(sha1)", 16, &icv, []byte{0x12, 0x34})),
				rtattr(XfrmAttrSecCtx, cat(le16(8+5), le16(8), []byte{1, 2}, le16(5), []byte("u:r:x"), []byte{0, 0, 0})),
				rtattr(XfrmAttrLtimeVal, cat(le64(1), le64(2), le64(3), le64(4))),
				rtattr(XfrmAttrReplayESNVal, cat(le32(2), le32(5), le32(6), le32(7), le32(8), le32(64), le32(0xf0), le32(0x0f))),
				rtattr(XfrmAttrCoAddr, []byte{192, 0, 2, 9, 15: 0}),
				rtattr(XfrmAttrLastUsed, le64(1700000000)),
				rtattr(XfrmAttrMark, cat(le32(3), le32(0xff))),
				rtattr(XfrmAttrSADir, []byte{2}),
				rtattr(XfrmAttrOffloadDev, cat(le32(5), []byte{1, 0, 0, 0})),
				rtattr(XfrmAttrIPTFSReorderWindow, le16(3)),
				rtattr(XfrmAttrIPTFSDontFrag, nil)),
			expected: "type=16 " + infoText + " comp=deflate/0/0: auth_trunc=hmac(sha1)/16/96:1234 sec_ctx=8/1/2:u:r:x ltime=1B/2P/3/4" +
				" esn=5/6/7/8/64[0xf0,0xf] coaddr=192.0.2.9 lastused=1700000000 mark=3/0xff sa_dir=2 offload=5/1 iptfs_reorder=3 iptfs_dont_frag",
		},
		{
			description: "corner: a repeated attribute keeps the last, as nla_parse does",
			h:           newsa, body: sa(u32(XfrmAttrIfID, 1), u32(XfrmAttrIfID, 2)),
			expected: "type=16 " + infoText + " if_id=2",
		},
		{
			description: "corner: an attribute this does not know is skipped",
			h:           newsa, body: sa(rtattr(200, []byte{1, 2, 3}), rtattr(XfrmAttrPad, nil), u32(XfrmAttrIfID, 1)),
			expected: "type=16 " + infoText + " if_id=1",
		},
		{
			description: "boundary: a key exactly as long as its bits, rounded up to a byte",
			h:           newsa, body: sa(rtattr(XfrmAttrAlgCrypt, xfrmTestAlgo("cbc(aes)", 9, nil, []byte{1, 2}))),
			expected: "type=16 " + infoText + " enc=cbc(aes)/9/0:0102",
		},
		{
			description: "boundary: an algorithm name of all 64 bytes, with no NUL",
			h:           newsa, body: sa(rtattr(XfrmAttrAlgAuth, xfrmTestAlgo(strings.Repeat("n", 64), 0, nil, nil))),
			expected: "type=16 " + infoText + " auth=" + strings.Repeat("n", 64) + "/0/0:",
		},
		{
			description: "boundary: an ESN bitmap of no words",
			h:           newsa, body: sa(rtattr(XfrmAttrReplayESNVal, cat(le32(0), le32(1), le32(2), le32(3), le32(4), le32(0)))),
			expected: "type=16 " + infoText + " esn=1/2/3/4/0[]",
		},
		{
			description: "negative: a type this does not parse",
			h:           NlMsgHdr{Type: XfrmMsgNewPolicy}, body: info,
			expected: "type=19 error: " + ErrXfrmType.Error() + ": 19",
		},
		{
			description: "negative: a NEWSA one byte short of its usersa_info",
			h:           newsa, body: info[:XfrmUsersaInfoSizeCst-1],
			expected: "type=16 error: " + ErrXfrmSmall.Error() + ": type 16 needs 224 bytes, has 223",
		},
		{
			description: "negative: a DELSA one byte short of its usersa_id",
			h:           NlMsgHdr{Type: XfrmMsgDelSA}, body: make([]byte, XfrmUsersaIDSizeCst-1),
			expected: "type=17 error: " + ErrXfrmSmall.Error() + ": type 17 needs 24 bytes, has 23",
		},
		{
			description: "negative: an empty FLUSHSA",
			h:           NlMsgHdr{Type: XfrmMsgFlushSA},
			expected:    "type=28 error: " + ErrXfrmSmall.Error() + ": type 28 needs 1 bytes, has 0",
		},
		{
			description: "negative: a NEWAE one byte short of its aevent_id",
			h:           NlMsgHdr{Type: XfrmMsgNewAE}, body: make([]byte, XfrmAeventIDSizeCst-1),
			expected: "type=30 error: " + ErrXfrmSmall.Error() + ": type 30 needs 48 bytes, has 47",
		},
		{
			description: "negative: a u32 attribute of 8 bytes, the attributes before it kept",
			h:           newsa, body: sa(u32(XfrmAttrIfID, 1), rtattr(XfrmAttrTFCPad, le64(1)), u32(XfrmAttrSetMark, 1)),
			expected: "type=16 " + infoText + " if_id=1 error: " + ErrXfrmAttr.Error() + ": 22 needs 4 bytes, has 8",
		},
		{
			description: "negative: a u8 attribute of 4 bytes",
			h:           newsa, body: sa(rtattr(XfrmAttrProto, le32(50))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 25 needs 1 bytes, has 4",
		},
		{
			description: "negative: a flag with a payload",
			h:           newsa, body: sa(rtattr(XfrmAttrIPTFSDontFrag, []byte{1})),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 38 needs 0 bytes, has 1",
		},
		{
			description: "negative: a replay state one byte short",
			h:           newsa, body: sa(rtattr(XfrmAttrReplayVal, make([]byte, XfrmReplayStateSizeCst-1))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 10 is 11 bytes, under 12",
		},
		{
			description: "negative: an xfrm_algo_auth no longer than an xfrm_algo",
			h:           newsa, body: sa(rtattr(XfrmAttrAlgAuthTrunc, xfrmTestAlgo("x", 0, nil, nil))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 20 is 68 bytes, under 72",
		},
		{
			description: "negative: a key one byte shorter than its bits",
			h:           newsa, body: sa(rtattr(XfrmAttrAlgCrypt, xfrmTestAlgo("aes", 128, nil, make([]byte, 15)))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 2 holds 15 key bytes of 16",
		},
		{
			description: "negative: an ESN bitmap one word short",
			h:           newsa, body: sa(rtattr(XfrmAttrReplayESNVal, cat(le32(2), make([]byte, 20), le32(1)))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 23 holds 4 bytes of bitmap, under 2 words",
		},
		{
			description: "negative: a security context one byte short",
			h:           newsa, body: sa(rtattr(XfrmAttrSecCtx, cat(le16(0), le16(0), []byte{0, 0}, le16(3), []byte("ab")))),
			expected: "type=16 " + infoText + " error: " + ErrXfrmAttr.Error() + ": 8 holds 2 context bytes of 3",
		},
		{
			description: "negative: an attribute running past the body",
			h:           newsa, body: sa([]byte{0x40, 0, 1, 0}),
			expected: "type=16 " + infoText + " error: " + ErrRTAttrSmall.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			h, body := tt.h, tt.body
			if tt.cap != "" {
				h, body = xfrmCaptured(t, tt.cap, tt.h.Type, tt.request, tt.nth)
			}
			s, err := ParseXfrmState(h, body)
			if got := xfrmStateRender(&s, err); got != tt.expected {
				t.Errorf("got %q, expected %q", got, tt.expected)
			}
		})
	}
}

// TestParseXfrmStateCorpus decodes every SA message in the corpus.
func TestParseXfrmStateCorpus(t *testing.T) {
	tally := func(types []uint16, request bool) string {
		var n, errs, info, id, flush, ae, dump, sa, algs int
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
			s, err := ParseXfrmState(m.h, m.body)
			if err != nil {
				errs++
				continue
			}
			for _, k := range []struct {
				ok bool
				c  *int
			}{
				{s.Info != nil, &info}, {s.ID != nil, &id}, {s.FlushProto != nil, &flush}, {s.AEvent != nil, &ae},
				{s.Dump, &dump}, {s.Attrs.SA != nil, &sa},
				{s.Attrs.AlgCrypt != nil || s.Attrs.AlgAEAD != nil, &algs},
			} {
				if k.ok {
					*k.c++
				}
			}
		}
		return strconv.Itoa(n) + " decoded, " + strconv.Itoa(errs) + " errors, " + strconv.Itoa(info) + " info, " +
			strconv.Itoa(id) + " id, " + strconv.Itoa(flush) + " flush, " + strconv.Itoa(ae) + " ae, " +
			strconv.Itoa(dump) + " dump, " + strconv.Itoa(sa) + " nested sa, " + strconv.Itoa(algs) + " with a cipher"
	}
	tests := []struct {
		description string
		got         string
		expected    string
	}{
		{description: "positive: ip's NEWSA and UPDSA requests decode", got: tally([]uint16{XfrmMsgNewSA, XfrmMsgUpdSA}, true), expected: "15 decoded, 0 errors, 15 info, 0 id, 0 flush, 0 ae, 0 dump, 0 nested sa, 15 with a cipher"},
		{description: "positive: the kernel's NEWSA and UPDSA messages decode", got: tally([]uint16{XfrmMsgNewSA, XfrmMsgUpdSA}, false), expected: "22 decoded, 0 errors, 22 info, 0 id, 0 flush, 0 ae, 0 dump, 0 nested sa, 20 with a cipher"},
		{description: "positive: ip's GETSA and DELSA requests decode, the dumps without an id", got: tally([]uint16{XfrmMsgGetSA, XfrmMsgDelSA}, true), expected: "25 decoded, 0 errors, 0 info, 7 id, 0 flush, 0 ae, 18 dump, 0 nested sa, 0 with a cipher"},
		{description: "positive: the kernel's DELSA event decodes with its nested SA", got: tally([]uint16{XfrmMsgDelSA}, false), expected: "1 decoded, 0 errors, 0 info, 1 id, 0 flush, 0 ae, 0 dump, 1 nested sa, 1 with a cipher"},
		{description: "positive: the FLUSHSA requests and event decode", got: tally([]uint16{XfrmMsgFlushSA}, true) + "|" + tally([]uint16{XfrmMsgFlushSA}, false), expected: "3 decoded, 0 errors, 0 info, 0 id, 3 flush, 0 ae, 0 dump, 0 nested sa, 0 with a cipher|1 decoded, 0 errors, 0 info, 0 id, 1 flush, 0 ae, 0 dump, 0 nested sa, 0 with a cipher"},
		{description: "positive: the kernel's NEWAE events decode", got: tally([]uint16{XfrmMsgNewAE}, false), expected: "2 decoded, 0 errors, 0 info, 0 id, 0 flush, 2 ae, 0 dump, 0 nested sa, 0 with a cipher"},
		{description: "negative: the kernel sends no GETSA and no GETAE", got: tally([]uint16{XfrmMsgGetSA, XfrmMsgGetAE}, false), expected: "0 decoded, 0 errors, 0 info, 0 id, 0 flush, 0 ae, 0 dump, 0 nested sa, 0 with a cipher"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("got %q, expected %q", tt.got, tt.expected)
			}
		})
	}
}
