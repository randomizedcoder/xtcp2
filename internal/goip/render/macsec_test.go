package render

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

func macsecP8(v uint8) *uint8    { return &v }
func macsecP32(v uint32) *uint32 { return &v }
func macsecP64(v uint64) *uint64 { return &v }

// macsecBase is a SecY with only what process requires and print_attrs
// prints: ifindex 7, SCI 1, encoding SA 0, GCM-AES-128, ICV 16, every flag
// off but protect, validate strict, empty lists and no offload nest.
func macsecBase() xtcpnl.Macsec {
	return xtcpnl.Macsec{
		Ifindex: macsecP32(7),
		SecY: &xtcpnl.MacsecSecY{
			SCI: macsecP64(1), EncodingSA: macsecP8(0), CipherSuite: macsecP64(xtcpnl.MacsecCipherGCMAES128),
			ICVLen: macsecP8(16), Protect: macsecP8(1), Replay: macsecP8(0), Oper: macsecP8(1),
			Validate: macsecP8(2), Encrypt: macsecP8(0), IncSCI: macsecP8(0), ES: macsecP8(0), SCB: macsecP8(0),
		},
		TxSAs: []xtcpnl.MacsecSA{},
		RxSCs: []xtcpnl.MacsecRxSC{},
	}
}

// TestMacsecSecY is process()'s lines and object over decoded SecYs, for
// what the captures do not hold.
//
// go test ./internal/goip/render/ -run TestMacsecSecY
func TestMacsecSecY(t *testing.T) {
	const (
		flags   = "sc off sa off encrypt off send_sci off end_station off scb off replay off "
		head    = "7: e7: protect on validate strict " + flags + "\n    cipher suite: GCM-AES-128, using ICV length 16\n"
		txsc    = "    TXSC: 0000000000000001 on SA 0\n"
		objHead = `{"ifindex":7,"ifname":"e7","protect":true,"validate":"strict","sc":false,"sa":false,` +
			`"encrypt":false,"send_sci":false,"end_station":false,"scb":false,"replay":false,`
		objSuite = `"cipher_suite":"GCM-AES-128","icv_length":16,"sci":"0x1","encoding_sa":0,`
	)
	window := macsecBase()
	window.SecY.Replay, window.SecY.Window = macsecP8(1), macsecP32(32)
	odd := macsecBase()
	odd.SecY.CipherSuite, odd.SecY.Validate = macsecP64(9), macsecP8(3)
	odd.HasOffload, odd.OffloadType = true, macsecP8(3)
	check := macsecBase()
	check.SecY.Validate, check.SecY.Protect = macsecP8(1), nil
	xpn := macsecBase()
	xpn.SecY.CipherSuite = macsecP64(xtcpnl.MacsecCipherGCMAESXPN128)
	xpn.TxSAs = []xtcpnl.MacsecSA{{AN: macsecP8(1), PN: macsecP64(1 << 32), Active: macsecP8(0), KeyID: []byte{0xab}}}
	wide := macsecBase()
	wide.TxSAs = []xtcpnl.MacsecSA{{AN: macsecP8(2), PN: macsecP64(1<<32 + 5), Active: macsecP8(1)}}
	noRx := macsecBase()
	noRx.RxSCs = nil
	stats := macsecBase()
	stats.TxscStats = []*uint64{macsecP64(1), nil, macsecP64(7), macsecP64(0)}
	stats.TxSAs = []xtcpnl.MacsecSA{{AN: macsecP8(0), PN: macsecP64(1), Active: macsecP8(1), KeyID: []byte{1},
		Stats: []*uint32{nil, nil, nil, nil, nil, macsecP32(3), nil}}}
	tests := []struct {
		description string
		m           xtcpnl.Macsec
		stats       bool
		text        string
		obj         string
	}{
		{"positive: the minimum, no SA, no channel, no offload", macsecBase(), false, head + txsc,
			objHead + objSuite + `"sa_list":[],"rx_sc":[]}`},
		{"positive: a window, after replay", window, false,
			"7: e7: protect on validate strict sc off sa off encrypt off send_sci off end_station off scb off " +
				"replay on window 32 \n    cipher suite: GCM-AES-128, using ICV length 16\n" + txsc,
			`{"ifindex":7,"ifname":"e7","protect":true,"validate":"strict","sc":false,"sa":false,"encrypt":false,` +
				`"send_sci":false,"end_station":false,"scb":false,"replay":true,"window":32,` + objSuite +
				`"sa_list":[],"rx_sc":[]}`},
		{"corner: a suite, validation and offload past the tables are (unknown)", odd, false,
			"7: e7: protect on validate (unknown) " + flags + "\n    cipher suite: (unknown), using ICV length 16\n" +
				txsc + "    offload: (unknown) \n",
			`{"ifindex":7,"ifname":"e7","protect":true,"validate":"(unknown)","sc":false,"sa":false,` +
				`"encrypt":false,"send_sci":false,"end_station":false,"scb":false,"replay":false,` +
				`"cipher_suite":"(unknown)","icv_length":16,"sci":"0x1","encoding_sa":0,"sa_list":[],"rx_sc":[],` +
				`"offload":"(unknown)"}`},
		{"corner: a flag absent prints nothing, validate check", check, false,
			"7: e7: validate check " + flags + "\n    cipher suite: GCM-AES-128, using ICV length 16\n" + txsc,
			`{"ifindex":7,"ifname":"e7","validate":"check","sc":false,"sa":false,"encrypt":false,` +
				`"send_sci":false,"end_station":false,"scb":false,"replay":false,` + objSuite +
				`"sa_list":[],"rx_sc":[]}`},
		{"corner: XPN, a u64 PN and an absent SSCI, %#llx's 0", xpn, false,
			"7: e7: protect on validate strict " + flags +
				"\n    cipher suite: GCM-AES-XPN-128, using ICV length 16\n" + txsc +
				"        1: PN 4294967296,SSCI 00000000 state off, key ab\n",
			objHead + `"cipher_suite":"GCM-AES-XPN-128","icv_length":16,"sci":"0x1","encoding_sa":0,` +
				`"sa_list":[{"an":1,"pn":4294967296,"ssci":"0","active":false,"key":"ab"}],"rx_sc":[]}`},
		{"boundary: a PN past u32 on a non-XPN SecY reads its low half, no key id an empty key", wide, false,
			head + txsc + "        2: PN 5, state on, key \n",
			objHead + objSuite + `"sa_list":[{"an":2,"pn":5,"active":true,"key":""}],"rx_sc":[]}`},
		{"corner: no receive list prints no rx_sc", noRx, false, head + txsc,
			objHead + objSuite + `"sa_list":[]}`},
		{"positive: -s, an absent counter is '-' and no member, an absent nest nothing", stats, true,
			head + txsc + "    stats: OutPktsProtected OutPktsEncrypted OutOctetsProtected OutOctetsEncrypted\n" +
				"                          1                -                  7                  0\n" +
				"        0: PN 1, state on, key 01\n" +
				"    stats: OutPktsProtected OutPktsEncrypted\n" +
				"                          3                -\n",
			objHead + objSuite + `"OutPktsProtected":1,"OutOctetsProtected":7,"OutOctetsEncrypted":0,` +
				`"sa_list":[{"an":0,"pn":1,"active":true,"key":"01","OutPktsProtected":3}],"rx_sc":[]}`},
		{"corner: without -s the counters print nothing", stats, false,
			head + txsc + "        0: PN 1, state on, key 01\n",
			objHead + objSuite + `"sa_list":[{"an":0,"pn":1,"active":true,"key":"01"}],"rx_sc":[]}`},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			text, obj := MacsecSecY(&tc.m, "e7", tc.stats)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}

// TestMacsecIsXPN is ciphersuite_is_xpn.
//
// go test ./internal/goip/render/ -run TestMacsecIsXPN
func TestMacsecIsXPN(t *testing.T) {
	tests := []struct {
		description string
		cid         uint64
		expected    bool
	}{
		{"positive: XPN-128", xtcpnl.MacsecCipherGCMAESXPN128, true},
		{"positive: XPN-256", xtcpnl.MacsecCipherGCMAESXPN256, true},
		{"negative: GCM-AES-128", xtcpnl.MacsecCipherGCMAES128, false},
		{"negative: GCM-AES-256", xtcpnl.MacsecCipherGCMAES256, false},
		{"corner: the default's old id", xtcpnl.MacsecCipherDefault, false},
		{"boundary: zero", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := MacsecIsXPN(tc.cid); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
