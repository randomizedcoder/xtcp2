package render

import (
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TokenView is one line of `ip token list`, print_token
// (ip/iptoken.c:41-87): `token TOKEN dev NAME`, and in JSON the two values.
type TokenView struct {
	Token  string `json:"token"`
	Ifname string `json:"ifname"`
}

// TokenViewOf builds the view of one AF_INET6 reply that print_token prints:
// the token through format_host_rta, the ifindex through ll_index_to_name.
// The caller has applied print_token's skips and checked the token is 16
// bytes.
func TokenViewOf(lt *xtcpnl.LinkToken, names NameTab) TokenView {
	return TokenView{Token: inet6Ntop([16]byte(lt.Token)), Ifname: names.IndexToName(lt.Index)}
}

// Text is the line print_token writes, newline terminated.
func (v *TokenView) Text() string {
	return "token " + v.Token + " dev " + v.Ifname + "\n"
}

// inet6Ntop is glibc's inet_ntop(AF_INET6), which netip agrees with except
// in one place a token reaches: an address whose first six 16-bit words are
// zero and whose seventh is not, which glibc's inet_ntop6 takes for an
// IPv4-compatible address and ends in dotted quad. The kernel keeps only a
// token's low 64 bits, so `ip token set ::a:b` prints `::0.10.0.11`
// (measured). The v4-mapped form, five zero words then 0xffff, netip already
// prints as glibc does, `::ffff:0.0.0.1`. An all-zero address and `::1`,
// whose zero run is longer than six, are glibc's `::` and `::1`.
func inet6Ntop(b [16]byte) string {
	if b[12]|b[13] != 0 && b == [16]byte{12: b[12], 13: b[13], 14: b[14], 15: b[15]} {
		return "::" + netip.AddrFrom4([4]byte(b[12:16])).String()
	}
	return netip.AddrFrom16(b).String()
}
