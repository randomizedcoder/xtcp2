package goipparity

import (
	"encoding/binary"
	"fmt"
)

// The dump-file half of the stdout comparison: `ip route save`, `ip address
// save` and `ip rule save` print no text, they write a 4-byte magic and then the dump's
// replies whole. Run through textFacets, that is a line count of however many
// 0x0a bytes the sequence numbers happened to hold, so each side's output is
// recognized by its magic and compared record by record instead.
//
// # Why this facet compares bytes, when the text facets refuse to
//
// The header's objection to byte parity is about rendering: column widths,
// flag order and counters that move between two runs. A save file renders
// nothing. Its records are the kernel's replies, which pkg/nlparity already
// compares on the wire, and what the facet adds is that goip wrote the replies
// its filter kept, in order, unaltered. The two fields that differ between any
// two runs, nlmsg_seq and nlmsg_pid, are left out of each element; a reply
// value that moves between the two `ip` runs, as an expiry would, moves the
// facet in the control as well and is subtracted with it.

// The dump files' magics, in host order (ip/iproute.c:1673,
// ip/ipaddress.c:1782, ip/iprule.c:605). The guest is little-endian, as is every host the
// harness runs on.
const (
	routeDumpMagicCst = 0x45311224
	addrDumpMagicCst  = 0x47361222
	ruleDumpMagicCst  = 0x71706986
	dumpMagicLenCst   = 4
	nlmsgHdrLenCst    = 16
)

// FacetDumpRecords is a dump file's records, one element per record in file
// order: its index, type, flags and body, seq and pid left out. A trailing
// fragment shorter than its header says is one more element. Presence class,
// so unsuppressible: a save that wrote the wrong routes, or none, is not an
// allowlist decision.
const FacetDumpRecords StdoutFacet = "dumprecords"

// dumpFileRecords reports whether s is a dump file and, if so, its elements:
// the magic, then the records.
func dumpFileRecords(s string) ([]string, bool) {
	b := []byte(s)
	if len(b) < dumpMagicLenCst {
		return nil, false
	}
	magic := binary.LittleEndian.Uint32(b)
	if magic != routeDumpMagicCst && magic != addrDumpMagicCst && magic != ruleDumpMagicCst {
		return nil, false
	}
	out := []string{fmt.Sprintf("magic:%08x", magic)}
	b = b[dumpMagicLenCst:]
	for i := 0; len(b) > 0; i++ {
		n := 0
		if len(b) >= nlmsgHdrLenCst {
			n = int(binary.LittleEndian.Uint32(b))
		}
		if n < nlmsgHdrLenCst || n > len(b) {
			out = append(out, fmt.Sprintf("%d:fragment:%x", i, b))
			break
		}
		typ := binary.LittleEndian.Uint16(b[4:6])
		flags := binary.LittleEndian.Uint16(b[6:8])
		out = append(out, fmt.Sprintf("%d:type=%d:flags=%#x:%x", i, typ, flags, b[nlmsgHdrLenCst:n]))
		b = b[n:]
	}
	return out, true
}

// dumpFacets fills FacetDumpRecords from a dump file's elements; every other
// facet stays empty, since there is no text.
func dumpFacets(elems []string, sets map[StdoutFacet]multiset) {
	for _, e := range elems {
		sets[FacetDumpRecords].add(e)
	}
}
