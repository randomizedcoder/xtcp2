package render

import (
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The prefixmsg flag bits print_prefix names (include/net/addrconf.h).
const (
	prefixOnlinkCst   = 0x01 // IF_PREFIX_ONLINK
	prefixAutoconfCst = 0x02 // IF_PREFIX_AUTOCONF
)

// PrefixText is print_prefix's line for an RTM_NEWPREFIX (ip/ipprefix.c),
// after its family and ND type checks, which the caller makes:
// `prefix ADDR/LEN`, with no space before `dev`, then `dev NAME `, the two
// flags, and the cache info's valid and preferred times, each with its
// trailing space. The address is inet_ntop's of the attribute as AF_INET6.
func PrefixText(pi *xtcpnl.PrefixInfo, names NameTab) string {
	var b strings.Builder
	if pi.Address != nil {
		b.WriteString("prefix ")
		b.WriteString(addrString(pi.Address, unix.AF_INET6))
		b.WriteByte('/')
		b.WriteString(strconv.Itoa(int(pi.Len)))
	}
	b.WriteString("dev ")
	b.WriteString(names.IndexToName(pi.Ifindex))
	b.WriteByte(' ')
	if pi.Flags&prefixOnlinkCst != 0 {
		b.WriteString("onlink ")
	}
	if pi.Flags&prefixAutoconfCst != 0 {
		b.WriteString("autoconf ")
	}
	if pi.HasCacheinfo {
		b.WriteString("valid ")
		b.WriteString(strconv.FormatUint(uint64(pi.ValidTime), 10))
		b.WriteString(" preferred ")
		b.WriteString(strconv.FormatUint(uint64(pi.PreferredTime), 10))
		b.WriteByte(' ')
	}
	b.WriteByte('\n')
	return b.String()
}
