package gofou

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// familyName is lib/utils.c's family_name, cut to the families fou answers.
func familyName(af uint8) string {
	switch af {
	case unix.AF_INET:
		return "inet"
	case unix.AF_INET6:
		return "inet6"
	default:
		return "???"
	}
}

// printFouMapping is ipfou.c's print_fou_mapping: one fou port's fields, in
// its order. In JSON it is one anonymous object (an array element); in text
// it is one line. ParseFOU has already turned the be16 port and peer_port
// into host order and picked the family's local/peer address, so the reads
// here are plain.
//
// ip prints local/peer only when non-zero (its memcmp against an empty
// buffer), peer_port only when non-zero, and dev only when the ifindex
// resolves to a name; the captured corpus has none of these, so those
// branches are not exercised by the parity test.
func (r *runner) printFouMapping(body []byte) {
	m, err := xtcpnl.ParseFOU(body)
	if err != nil {
		return // print_fou_mapping does not fail; a bad body prints nothing.
	}
	o := r.o
	o.openJSONObject(nil)

	if m.Port != nil {
		o.printUint(printAny, ptr("port"), "port %u", uint32(*m.Port))
	}

	if m.Type != nil && *m.Type == xtcpnl.FOUEncapGUE {
		o.printNull(printAny, "gue", " gue")
	} else if m.IPProto != nil {
		o.printUint(printAny, ptr("ipproto"), " ipproto %u", uint32(*m.IPProto))
	}

	if m.AF != nil {
		o.printString(printJSON, ptr("family"), "", familyName(*m.AF))
		if *m.AF == unix.AF_INET6 {
			o.printString(printFP, nil, " -6", "")
		}
	}

	if m.Local.IsValid() && !m.Local.IsUnspecified() {
		o.printString(printAny, ptr("local"), " local %s", m.Local.String())
	}
	if m.Peer.IsValid() && !m.Peer.IsUnspecified() {
		o.printString(printAny, ptr("peer"), " peer %s", m.Peer.String())
	}
	if m.PeerPort != nil && *m.PeerPort != 0 {
		o.printUint(printAny, ptr("peer_port"), " peer_port %u", uint32(*m.PeerPort))
	}
	// dev: ip resolves the ifindex to a name (ll_index_to_name) and prints
	// it only if found; a replay has no name table, and the corpus has no
	// bound device, so nothing is printed here.

	o.printString(printFP, nil, "\n", "") // the mapping's text newline
	o.closeJSONObject()
}
