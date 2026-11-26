package main

import (
	"net"
	"net/netip"
	"testing"
)

// TestBuildGratuitousReplyGolden: the exact 42-byte frame. Ethernet
// broadcast dst, the poison MAC as src, ethertype 0x0806; ARP reply,
// sha=poison, spa==tpa=the claimed IP, tha=broadcast.
func TestBuildGratuitousReplyGolden(t *testing.T) {
	poison := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0xff}
	src := []byte{0x02, 0x42, 0xac, 0x11, 0x00, 0x03}
	ip := netip.MustParseAddr("172.17.0.1")
	frame := buildGratuitousReply(src, poison, ip)

	if len(frame) != 42 {
		t.Fatalf("frame len %d, want 42", len(frame))
	}
	want := []byte{
		// Ethernet
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // dst broadcast
		0x02, 0x00, 0x00, 0x00, 0x00, 0xff, // src = the claim
		0x08, 0x06, // ARP
		// ARP
		0x00, 0x01, // htype Ethernet
		0x08, 0x00, // ptype IPv4
		0x06, 0x04, // hlen, plen
		0x00, 0x02, // op REPLY
		0x02, 0x00, 0x00, 0x00, 0x00, 0xff, // sha = poison
		0xac, 0x11, 0x00, 0x01, // spa = 172.17.0.1
		0xff, 0xff, 0xff, 0xff, 0xff, 0xff, // tha broadcast
		0xac, 0x11, 0x00, 0x01, // tpa == spa (gratuitous)
	}
	for i, b := range want {
		if frame[i] != b {
			t.Fatalf("byte %d = %#02x, want %#02x (frame %x)", i, frame[i], b, frame)
		}
	}
}

// TestHtons: the raw-socket sockaddr protocol and the frame ethertype are
// both network-order — a swapped htons would be off the wire entirely.
func TestHtons(t *testing.T) {
	if got := htons(0x0806); got != 0x0608 {
		t.Fatalf("htons(0x0806) = %#04x, want 0x0608 (byte-swapped to network order on LE)", got)
	}
	if got := htons(0x1234); got != 0x3412 {
		t.Fatalf("htons(0x1234) = %#04x, want 0x3412", got)
	}
	if got := htons(htons(0xbeef)); got != 0xbeef {
		t.Fatalf("htons is not an involution: %#04x", got)
	}
}

// TestBuildGratuitousReplyMACs: the poison tool must not leak the sender's
// real interface MAC into the claim (src and sha are the poison MAC).
func TestBuildGratuitousReplyMACs(t *testing.T) {
	poison, _ := net.ParseMAC("02:00:00:00:00:ff")
	src, _ := net.ParseMAC("02:42:ac:11:00:03")
	frame := buildGratuitousReply(src, poison, netip.MustParseAddr("10.0.0.1"))
	if string(frame[6:12]) != string(poison) {
		t.Fatalf("ethernet src is %x, want the poison MAC %x", frame[6:12], poison)
	}
	if string(frame[14+8:14+14]) != string(poison) {
		t.Fatalf("arp sha is %x, want the poison MAC %x", frame[14+8:14+14], poison)
	}
}
