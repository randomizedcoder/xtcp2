// Command idiag-extprobe verifies, against the real running kernel, that the
// inet_diag "extension" request bitmask (inet_diag_req_v2.idiag_ext) actually
// gates which INET_DIAG_* attributes the kernel returns — i.e. "what we ask for
// is what we get". It is the microvm integration counterpart to the host unit
// tests for IDiagExtFromEnabled: those pin the bitmask we compute, this proves the
// kernel honors it.
//
// For each idiag_ext value it opens a NETLINK_INET_DIAG socket, dumps AF_INET/TCP
// sockets, walks the raw reply, and records which attribute TYPES appear across
// all returned sockets. Then it asserts:
//
//   - Negative (always holds): for every ext-GATED extension N (1..7; NOT 8 —
//     see below), if bit (N-1) is CLEAR in the request, attribute N is ABSENT from
//     every socket. This is the core contract behind dropping meminfo — not
//     requesting it means the kernel never sends it.
//   - Positive (for unconditionally-emitted-when-requested attrs {MEMINFO, INFO,
//     CONG, SKMEMINFO}): if the bit is SET, the attribute appears on at least one
//     socket. Vegas/TOS/TCLASS are intentionally NOT positive-asserted — the
//     kernel only emits them for sockets with the relevant property (Vegas cong
//     control, IPv6 tclass, …), so their absence on a plain IPv4 cubic socket is
//     legitimate.
//
// INET_DIAG_SHUTDOWN (ext 8) is deliberately EXCLUDED from both checks: the kernel
// emits it UNCONDITIONALLY (net/ipv4/inet_diag.c: inet_diag_msg_attrs_fill calls
// nla_put_u8(skb, INET_DIAG_SHUTDOWN, …) with no `ext &` guard), so bit 7 of
// idiag_ext is a no-op — SHUTDOWN is present regardless of whether it's requested.
// Verified against the kernel source; the gated attributes checked here are all
// under an explicit `if (ext & (1 << (INET_DIAG_X - 1)))` guard.
//
// A guaranteed ESTABLISHED loopback TCP connection is held open for the duration
// so the dump is never empty (otherwise the positive assertions would be vacuous).
//
// Exit 0 + "IDIAG_EXT_PROBE_OK" on success; non-zero + "IDIAG_EXT_PROBE_FAIL: …"
// on any mismatch. The microvm self-test greps these into its
// XTCP2_SELF_TEST_IDIAG_EXT_PROBE_{PASS,FAIL} sentinel.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// closeOrLog closes c and reports (but does not fail on) a close error. The
// probe's verdict comes from the dump contract, not teardown, so a Close error
// is diagnostic noise — but errcheck (check-blank) forbids discarding it to `_`.
func closeOrLog(name string, c io.Closer) {
	if cerr := c.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "idiag-extprobe: closing %s: %v\n", name, cerr)
	}
}

// attrName maps an addressable INET_DIAG extension number to a human label for
// diagnostics. Index by attribute type (1..8).
var attrName = map[int]string{
	xtcpnl.MemInfoEmumValueCst:       "MEMINFO",
	xtcpnl.TCPInfoEmumValueCst:       "INFO",
	xtcpnl.VegasInfoEnumValueCst:     "VEGASINFO",
	xtcpnl.CongInfoEmumValueCst:      "CONG",
	xtcpnl.TypeOfServiceEmumValueCst: "TOS",
	xtcpnl.TrafficClassEmumValueCst:  "TCLASS",
	xtcpnl.SkMemInfoEnumValueCst:     "SKMEMINFO",
	xtcpnl.ShutdownEmumValueCst:      "SHUTDOWN",
}

// gatedByExt lists the addressable extensions the kernel actually gates on the
// idiag_ext bit (verified in net/ipv4/inet_diag.c). SHUTDOWN(8) is NOT here — it
// is emitted unconditionally — so the negative direction is checked only over
// these. (TCLASS(6) is gated too but IPv6-only, so it's simply never present on
// the IPv4 sockets we dump; including it here keeps the negative check honest.)
var gatedByExt = map[int]bool{
	xtcpnl.MemInfoEmumValueCst:       true, // 1
	xtcpnl.TCPInfoEmumValueCst:       true, // 2
	xtcpnl.VegasInfoEnumValueCst:     true, // 3
	xtcpnl.CongInfoEmumValueCst:      true, // 4
	xtcpnl.TypeOfServiceEmumValueCst: true, // 5
	xtcpnl.TrafficClassEmumValueCst:  true, // 6 (IPv6-only in practice)
	xtcpnl.SkMemInfoEnumValueCst:     true, // 7
}

// positiveAssert lists the extensions the kernel emits whenever requested for a
// normal ESTABLISHED IPv4/TCP socket, so a set bit MUST yield the attribute. The
// rest (Vegas needs vegas cong control; TOS may be there but we keep this set to
// the certain ones; TCLASS is IPv6-only) are checked only in the negative
// direction.
var positiveAssert = map[int]bool{
	xtcpnl.MemInfoEmumValueCst:   true, // 1
	xtcpnl.TCPInfoEmumValueCst:   true, // 2
	xtcpnl.CongInfoEmumValueCst:  true, // 4
	xtcpnl.SkMemInfoEnumValueCst: true, // 7
}

// allStates requests every TCP state so the held-open loopback socket (and any
// others) is always returned. Written host-endian by BuildNetlinkSockDiagRequest.
const allStates = 0xFFFFFFFF

func main() {
	if err := run(); err != nil {
		fmt.Printf("IDIAG_EXT_PROBE_FAIL: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("IDIAG_EXT_PROBE_OK")
}

func run() error {
	// Hold an ESTABLISHED loopback TCP connection open so every dump is non-empty.
	closeConns, err := openLoopbackConn()
	if err != nil {
		return fmt.Errorf("establishing loopback TCP socket: %w", err)
	}
	defer closeConns()

	// Each case: the requested idiag_ext and the assertions derived from it below.
	// ext values: none; meminfo-only (bit 0); skmem-only (bit 6=64); default
	// (254 = all addressable minus meminfo); all (255).
	cases := []struct {
		name string
		ext  uint8
	}{
		{"none", 0},
		{"meminfo-only", 1 << (xtcpnl.MemInfoEmumValueCst - 1)},
		{"skmem-only", 1 << (xtcpnl.SkMemInfoEnumValueCst - 1)},
		{"default", 254},
		{"all", 255},
	}

	for _, c := range cases {
		seen, sockets, err := dumpAttrTypes(c.ext)
		if err != nil {
			return fmt.Errorf("case %q (ext=%d): dump: %w", c.name, c.ext, err)
		}
		if sockets == 0 {
			return fmt.Errorf("case %q (ext=%d): kernel returned 0 sockets; cannot verify", c.name, c.ext)
		}
		if err := assertContract(c.name, c.ext, seen, sockets); err != nil {
			return err
		}
	}
	return nil
}

// assertContract enforces the bit⟺attribute contract for one request.
func assertContract(name string, ext uint8, seen map[int]bool, sockets int) error {
	for n := 1; n <= 8; n++ {
		requested := ext&(1<<uint(n-1)) != 0
		present := seen[n]
		switch {
		case !requested && present && gatedByExt[n]:
			// The definitive failure: kernel returned an ext-gated attribute we did
			// NOT ask for. This is exactly what must never happen for dropped meminfo.
			return fmt.Errorf("case %q (ext=%d): attribute %s(%d) present but NOT requested "+
				"(bit %d clear); kernel returned data we didn't ask for", name, ext, attrName[n], n, n-1)
		case requested && !present && positiveAssert[n]:
			return fmt.Errorf("case %q (ext=%d): attribute %s(%d) requested (bit %d set) but ABSENT "+
				"from all %d sockets; kernel did not honor the request", name, ext, attrName[n], n, n-1, sockets)
		}
	}
	fmt.Printf("  case %-13s ext=%-3d sockets=%-3d attrs=%s OK\n", name, ext, sockets, fmtSeen(seen))
	return nil
}

func fmtSeen(seen map[int]bool) string {
	ns := make([]int, 0, len(seen))
	for n := range seen {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	labels := make([]string, 0, len(ns))
	for _, n := range ns {
		label := attrName[n]
		if label == "" {
			label = fmt.Sprintf("attr%d", n)
		}
		labels = append(labels, label)
	}
	return "[" + strings.Join(labels, " ") + "]"
}

// openLoopbackConn establishes a real ESTABLISHED IPv4/TCP connection on the
// loopback interface and returns a closer that tears down both ends.
func openLoopbackConn() (func(), error) {
	ctx := context.Background()
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	type acc struct {
		c net.Conn
		e error
	}
	ch := make(chan acc, 1)
	go func() {
		c, e := ln.Accept()
		ch <- acc{c, e}
	}()
	var d net.Dialer
	client, err := d.DialContext(ctx, "tcp4", ln.Addr().String())
	if err != nil {
		closeOrLog("listener", ln)
		return nil, err
	}
	a := <-ch
	if a.e != nil {
		closeOrLog("client conn", client)
		closeOrLog("listener", ln)
		return nil, a.e
	}
	return func() {
		closeOrLog("accepted conn", a.c)
		closeOrLog("client conn", client)
		closeOrLog("listener", ln)
	}, nil
}

// dumpAttrTypes sends one inet_diag dump request with the given idiag_ext and
// returns the set of attribute types seen across all returned sockets plus the
// socket count.
func dumpAttrTypes(ext uint8) (seen map[int]bool, sockets int, err error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM, unix.NETLINK_INET_DIAG)
	if err != nil {
		return nil, 0, fmt.Errorf("socket: %w", err)
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil {
			fmt.Fprintf(os.Stderr, "idiag-extprobe: closing netlink fd: %v\n", cerr)
		}
	}()

	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, 0, fmt.Errorf("bind: %w", err)
	}
	// 5s recv timeout so a lost DONE can't hang the probe forever.
	tv := unix.Timeval{Sec: 5}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		return nil, 0, fmt.Errorf("set recv timeout: %w", err)
	}

	req := xtcpnl.BuildNetlinkSockDiagRequest(xtcpnl.BuildNLRequest{
		AddressFamily: 2, // AF_INET
		MakeSize:      xtcpnl.InetDiagRequestSizeCst,
		NlMsgLen:      xtcpnl.InetDiagRequestSizeCst,
		NlMsgSeq:      1,
		IDiagExt:      ext,
		States:        allStates,
	})
	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, 0, fmt.Errorf("sendto: %w", err)
	}

	seen = make(map[int]bool)
	buf := make([]byte, 1<<16)
	for {
		n, _, rerr := unix.Recvfrom(fd, buf, 0)
		if rerr != nil {
			return nil, 0, fmt.Errorf("recvfrom: %w", rerr)
		}
		done, socks, perr := parseReply(buf[:n], seen)
		if perr != nil {
			return nil, 0, perr
		}
		sockets += socks
		if done {
			return seen, sockets, nil
		}
	}
}

// parseReply walks one received datagram of one-or-more netlink messages,
// recording each socket's attribute types into seen. Returns done=true when the
// NLMSG_DONE terminator is reached, and the number of inet_diag socket records in
// this datagram.
func parseReply(data []byte, seen map[int]bool) (done bool, sockets int, err error) {
	offset := 0
	for offset+xtcpnl.NlMsgHdrSizeCst <= len(data) {
		var nlh xtcpnl.NlMsgHdr
		if _, e := xtcpnl.DeserializeNlMsgHdr(data[offset:], &nlh); e != nil {
			return false, sockets, fmt.Errorf("nlmsghdr parse at offset %d: %w", offset, e)
		}
		msgLen := int(nlh.Len)
		if msgLen < xtcpnl.NlMsgHdrSizeCst || offset+msgLen > len(data) {
			return false, sockets, fmt.Errorf("bad nlmsg len %d at offset %d (buf %d)", msgLen, offset, len(data))
		}

		switch int(nlh.Type) {
		case xtcpnl.NlMsgHdrTypeDoneCst: // NLMSG_DONE (3)
			return true, sockets, nil
		case 2: // NLMSG_ERROR
			// Body starts with a negative errno (int32). 0 == ACK.
			body := data[offset+xtcpnl.NlMsgHdrSizeCst:]
			if len(body) >= 4 {
				errno := int32(uint32(body[0]) | uint32(body[1])<<8 | uint32(body[2])<<16 | uint32(body[3])<<24)
				if errno != 0 {
					return false, sockets, fmt.Errorf("kernel NLMSG_ERROR errno=%d", errno)
				}
			}
		default:
			// inet_diag_msg (SOCK_DIAG_BY_FAMILY reply). Attributes follow the
			// fixed 72-byte inet_diag_msg header.
			sockets++
			attrStart := offset + xtcpnl.NlMsgHdrSizeCst + xtcpnl.InetDiagMsgSizeCst
			msgEnd := offset + msgLen
			walkAttrs(data, attrStart, msgEnd, seen)
		}

		// netlink messages are 4-byte aligned.
		offset += msgLen + xtcpnl.FourByteAlignPadding(msgLen)
	}
	return false, sockets, nil
}

// walkAttrs records the type of every rtattr in [start,end) into seen.
func walkAttrs(data []byte, start, end int, seen map[int]bool) {
	o := start
	for o+xtcpnl.RTAttrSizeCst <= end {
		var rta xtcpnl.RTAttr
		if _, e := xtcpnl.DeserializeRTAttr(data[o:end], &rta); e != nil {
			return
		}
		rlen := int(rta.Len)
		if rlen < xtcpnl.RTAttrSizeCst || o+rlen > end {
			return
		}
		seen[int(rta.Type)] = true
		o += rlen + xtcpnl.FourByteAlignPadding(rlen)
	}
}
