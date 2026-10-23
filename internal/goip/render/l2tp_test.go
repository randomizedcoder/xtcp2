package render

import (
	"testing"

	"golang.org/x/sys/unix"
)

// l2tpV4 is an AF_INET L2tpAddr.
func l2tpV4(a, b, c, d byte) L2tpAddr {
	return L2tpAddr{Family: unix.AF_INET, Data: [16]byte{a, b, c, d}}
}

// TestL2tpTunnel is print_tunnel's lines and object.
//
// go test ./internal/goip/render/ -run TestL2tpTunnel
func TestL2tpTunnel(t *testing.T) {
	udp := L2tpParm{TunnelID: 1, PeerTunnelID: 2, LocalPort: 5000, PeerPort: 6000,
		Local: l2tpV4(192, 0, 2, 1), Peer: l2tpV4(192, 0, 2, 2), UDP6CsumTx: true, UDP6CsumRx: true}
	csum := udp
	csum.UDPCsum = true
	v6 := udp
	v6.Local = L2tpAddr{Family: unix.AF_INET6, Data: [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 1}}
	v6.Peer = L2tpAddr{Family: unix.AF_INET6, Data: [16]byte{0x20, 0x01, 0x0d, 0xb8, 15: 2}}
	v6rx := v6
	v6rx.UDP6CsumTx = false
	v6off := v6rx
	v6off.UDP6CsumRx = false
	ipEncap := udp
	ipEncap.Encap = 1
	stale := udp
	stale.Local.Family = unix.AF_INET6
	const head = "Tunnel 1, encap UDP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 2\n  UDP source / dest ports: 5000/6000\n"
	const head6 = "Tunnel 1, encap UDP\n  From 2001:db8::1 to 2001:db8::2\n  Peer tunnel 2\n" +
		"  UDP source / dest ports: 5000/6000\n"
	const objHead = `{"tunnel_id":1,"encap":"UDP","local":"192.0.2.1","peer":"192.0.2.2","peer_tunnel":2,` +
		`"local_port":5000,"peer_port":6000,`
	const objHead6 = `{"tunnel_id":1,"encap":"UDP","local":"2001:db8::1","peer":"2001:db8::2","peer_tunnel":2,` +
		`"local_port":5000,"peer_port":6000,`
	tests := []struct {
		description string
		p           L2tpParm
		json        bool
		text        string
		obj         string
	}{
		{description: "positive: IPv4 UDP, checksum off", p: udp, text: head + "  UDP checksum: disabled\n",
			obj: objHead + `"checksum":false}`},
		{description: "positive: IPv4 UDP, checksum on", p: csum, text: head + "  UDP checksum: enabled\n",
			obj: objHead + `"checksum":true}`},
		{description: "positive: IPv6, both on, enabled", p: v6, text: head6 + "  UDP checksum: enabled\n"},
		{description: "positive: IPv6, rx alone", p: v6rx, text: head6 + "  UDP checksum: rx\n"},
		{description: "boundary: IPv6, both off, disabled", p: v6off, text: head6 + "  UDP checksum: disabled\n"},
		{description: "positive: under -j IPv6's two members, no line", p: v6rx, json: true, text: head6,
			obj: objHead6 + `"checksum_tx":false,"checksum_rx":true}`},
		{description: "positive: IP, no ports and no checksum",
			p:    ipEncap,
			text: "Tunnel 1, encap IP\n  From 192.0.2.1 to 192.0.2.2\n  Peer tunnel 2\n",
			obj:  `{"tunnel_id":1,"encap":"IP","local":"192.0.2.1","peer":"192.0.2.2","peer_tunnel":2}`},
		{description: "corner: an IPv4 address read as AF_INET6, its bytes and the zeros after",
			p: stale, text: "Tunnel 1, encap UDP\n  From c000:201:: to 192.0.2.2\n  Peer tunnel 2\n" +
				"  UDP source / dest ports: 5000/6000\n  UDP checksum: enabled\n"},
		{description: "boundary: the zero state, ids 0 and the IPv4 family 0, AF_INET6's ::",
			p:    L2tpParm{},
			text: "Tunnel 0, encap UDP\n  From :: to ::\n  Peer tunnel 0\n  UDP source / dest ports: 0/0\n",
			obj: `{"tunnel_id":0,"encap":"UDP","local":"::","peer":"::","peer_tunnel":0,"local_port":0,` +
				`"peer_port":0}`},
		{description: "corner: an encapsulation of neither is ??", p: L2tpParm{Encap: 2},
			text: "Tunnel 0, encap ??\n  From :: to ::\n  Peer tunnel 0\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := L2tpTunnel(&tc.p, tc.json)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if tc.obj != "" && string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}

// TestL2tpSession is print_session's lines and object.
//
// go test ./internal/goip/render/ -run TestL2tpSession
func TestL2tpSession(t *testing.T) {
	name := "l2tpeth0"
	base := L2tpParm{SessionID: 10, TunnelID: 1, PeerSessionID: 20, PeerTunnelID: 2, Ifname: &name}
	cookies := base
	cookies.Cookie, cookies.CookieLen = [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, 8
	cookies.PeerCookie, cookies.PeerCookieLen = [8]byte{0xa, 0xb, 0xc, 0xd, 0xe}, 4
	seq := base
	seq.SendSeq = true
	recv := base
	recv.RecvSeq, recv.ReorderTimeout = true, 100
	noName := base
	noName.Ifname = nil
	const head = "Session 10 in tunnel 1\n  Peer session 20, tunnel 2\n  interface name: l2tpeth0\n" +
		"  offset 0, peer offset 0\n"
	const objHead = `{"session_id":10,"tunnel_id":1,"peer_session_id":20,"peer_tunnel_id":2,"interface":"l2tpeth0"`
	tests := []struct {
		description string
		p           L2tpParm
		text        string
		obj         string
	}{
		{description: "positive: a plain session, a blank line after it", p: base, text: head + "\n",
			obj: objHead + "}"},
		{description: "positive: an 8-byte cookie and a 4-byte one, run together on one line", p: cookies,
			text: head + "  cookie 0102030405060708  peer cookie 0a0b0c0d\n",
			obj:  objHead + `,"cookie":"0102030405060708","peer_cookie":"0a0b0c0d"}`},
		{description: "positive: send alone, on its own line", p: seq, text: head + "\n  sequence numbering: send\n",
			obj: objHead + `,"send_seq":null}`},
		{description: "positive: a reorder timeout ahead of recv", p: recv,
			text: head + "  reorder timeout: 100\n  sequence numbering: recv\n",
			obj:  objHead + `,"reorder_timeout":100,"recv_seq":null}`},
		{description: "boundary: no name, no interface line or member", p: noName,
			text: "Session 10 in tunnel 1\n  Peer session 20, tunnel 2\n  offset 0, peer offset 0\n\n",
			obj:  `{"session_id":10,"tunnel_id":1,"peer_session_id":20,"peer_tunnel_id":2}`},
		{description: "corner: a negative timeout is %u of the int", p: L2tpParm{ReorderTimeout: -2},
			text: "Session 0 in tunnel 0\n  Peer session 0, tunnel 0\n  offset 0, peer offset 0\n" +
				"  reorder timeout: 4294967294\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			text, obj := L2tpSession(&tc.p)
			if text != tc.text {
				t.Errorf("text\n got %q\nwant %q", text, tc.text)
			}
			if tc.obj != "" && string(obj) != tc.obj {
				t.Errorf("obj\n got %s\nwant %s", obj, tc.obj)
			}
		})
	}
}
