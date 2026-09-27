package xtcpnl

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// This file holds the ARPHRD_* hardware-type name table.
//
// `ip link show` puts this name at the start of a link's second line —
// "link/ether", "link/loopback", "link/netlink" — so nothing can render a link
// without it. The values come from ifi_type in the ifinfomsg header, not from
// an attribute, which is why they are available even on a truncated message.
//
// # The table is iproute2's, deliberately including what it leaves out
//
// Transcribed from lib/ll_types.c ll_type_n2a, entry for entry and spelling for
// spelling: ARPHRD_TUNNEL renders as "ipip" not "tunnel", ARPHRD_IPDDP as
// "ip/ddp", ARPHRD_IEEE80211_RADIOTAP as "ieee802.11/radiotap". A plausible
// name is the wrong name here — the point of the table is that goip's output
// matches `ip`'s.
//
// golang.org/x/sys/unix exports several ARPHRD_* constants that iproute2's
// table does NOT list — ARPHRD_RAWIP, ARPHRD_MCTP, ARPHRD_VSOCKMON,
// ARPHRD_EUI64 among them. They are omitted here on purpose: `ip` prints
// "[519]" for a raw-IP link, so a goip that printed "rawip" would diverge.
// TestARPHRDNameMatchesIproute2Omissions pins that.
//
// ARPHRD_CISCO and ARPHRD_HDLC are the same value (513) in unix; iproute2 names
// it "hdlc", so only that spelling appears.

// arphrdNames maps ifi_type to the name `ip` prints.
//
// iproute2 lib/ll_types.c ll_type_n2a
var arphrdNames = map[uint16]string{
	unix.ARPHRD_NETROM:             "netrom",
	unix.ARPHRD_ETHER:              "ether",
	unix.ARPHRD_EETHER:             "eether",
	unix.ARPHRD_AX25:               "ax25",
	unix.ARPHRD_PRONET:             "pronet",
	unix.ARPHRD_CHAOS:              "chaos",
	unix.ARPHRD_IEEE802:            "ieee802",
	unix.ARPHRD_ARCNET:             "arcnet",
	unix.ARPHRD_APPLETLK:           "atalk",
	unix.ARPHRD_DLCI:               "dlci",
	unix.ARPHRD_ATM:                "atm",
	unix.ARPHRD_METRICOM:           "metricom",
	unix.ARPHRD_IEEE1394:           "ieee1394",
	unix.ARPHRD_INFINIBAND:         "infiniband",
	unix.ARPHRD_SLIP:               "slip",
	unix.ARPHRD_CSLIP:              "cslip",
	unix.ARPHRD_SLIP6:              "slip6",
	unix.ARPHRD_CSLIP6:             "cslip6",
	unix.ARPHRD_RSRVD:              "rsrvd",
	unix.ARPHRD_ADAPT:              "adapt",
	unix.ARPHRD_ROSE:               "rose",
	unix.ARPHRD_X25:                "x25",
	unix.ARPHRD_HWX25:              "hwx25",
	unix.ARPHRD_CAN:                "can",
	unix.ARPHRD_PPP:                "ppp",
	unix.ARPHRD_HDLC:               "hdlc",
	unix.ARPHRD_LAPB:               "lapb",
	unix.ARPHRD_DDCMP:              "ddcmp",
	unix.ARPHRD_RAWHDLC:            "rawhdlc",
	unix.ARPHRD_TUNNEL:             "ipip",
	unix.ARPHRD_TUNNEL6:            "tunnel6",
	unix.ARPHRD_FRAD:               "frad",
	unix.ARPHRD_SKIP:               "skip",
	unix.ARPHRD_LOOPBACK:           "loopback",
	unix.ARPHRD_LOCALTLK:           "ltalk",
	unix.ARPHRD_FDDI:               "fddi",
	unix.ARPHRD_BIF:                "bif",
	unix.ARPHRD_SIT:                "sit",
	unix.ARPHRD_IPDDP:              "ip/ddp",
	unix.ARPHRD_IPGRE:              "gre",
	unix.ARPHRD_PIMREG:             "pimreg",
	unix.ARPHRD_HIPPI:              "hippi",
	unix.ARPHRD_ASH:                "ash",
	unix.ARPHRD_ECONET:             "econet",
	unix.ARPHRD_IRDA:               "irda",
	unix.ARPHRD_FCPP:               "fcpp",
	unix.ARPHRD_FCAL:               "fcal",
	unix.ARPHRD_FCPL:               "fcpl",
	unix.ARPHRD_FCFABRIC:           "fcfb0",
	unix.ARPHRD_FCFABRIC + 1:       "fcfb1",
	unix.ARPHRD_FCFABRIC + 2:       "fcfb2",
	unix.ARPHRD_FCFABRIC + 3:       "fcfb3",
	unix.ARPHRD_FCFABRIC + 4:       "fcfb4",
	unix.ARPHRD_FCFABRIC + 5:       "fcfb5",
	unix.ARPHRD_FCFABRIC + 6:       "fcfb6",
	unix.ARPHRD_FCFABRIC + 7:       "fcfb7",
	unix.ARPHRD_FCFABRIC + 8:       "fcfb8",
	unix.ARPHRD_FCFABRIC + 9:       "fcfb9",
	unix.ARPHRD_FCFABRIC + 10:      "fcfb10",
	unix.ARPHRD_FCFABRIC + 11:      "fcfb11",
	unix.ARPHRD_FCFABRIC + 12:      "fcfb12",
	unix.ARPHRD_IEEE802_TR:         "tr",
	unix.ARPHRD_IEEE80211:          "ieee802.11",
	unix.ARPHRD_IEEE80211_PRISM:    "ieee802.11/prism",
	unix.ARPHRD_IEEE80211_RADIOTAP: "ieee802.11/radiotap",
	unix.ARPHRD_IEEE802154:         "ieee802.15.4",
	unix.ARPHRD_IEEE802154_MONITOR: "ieee802.15.4/monitor",
	unix.ARPHRD_PHONET:             "phonet",
	unix.ARPHRD_PHONET_PIPE:        "phonet_pipe",
	unix.ARPHRD_CAIF:               "caif",
	unix.ARPHRD_IP6GRE:             "gre6",
	unix.ARPHRD_NETLINK:            "netlink",
	unix.ARPHRD_6LOWPAN:            "6lowpan",
	unix.ARPHRD_NONE:               "none",
	unix.ARPHRD_VOID:               "void",
}

// ARPHRDName returns the name `ip link show` prints for an ifi_type, or
// "[<decimal>]" for a type iproute2's table does not name.
//
// The bracketed decimal is iproute2's own fallback (`snprintf(buf, len, "[%d]",
// type)`), not a placeholder chosen here — which is why it is decimal rather
// than hex, and why the brackets are part of the string.
//
// ARPHRD_VOID (0xffff) and ARPHRD_NONE (0xfffe) are real named entries, so
// neither is a useful "unknown" probe; a type genuinely absent from the table
// is something like ARPHRD_RAWIP (519).
//
// `ip -N` (numeric) skips the table entirely and always prints the number.
// Nothing here models that, because the flag is a renderer concern.
func ARPHRDName(ifiType uint16) string {
	if n, ok := arphrdNames[ifiType]; ok {
		return n
	}
	return "[" + strconv.FormatUint(uint64(ifiType), 10) + "]"
}
