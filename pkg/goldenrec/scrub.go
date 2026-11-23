package goldenrec

import (
	"fmt"
	"net/netip"
	"reflect"
	"strconv"
	"time"
)

const (
	// ScrubHostname replaces the capture host's name.
	ScrubHostname = "golden-host"
	// scrubPlaceholder replaces every other host-identifying string.
	scrubPlaceholder = "golden"
	// scrubEpoch is where scrubbed timestamps start: 2026-01-01T00:00:00Z.
	scrubEpoch int64 = 1_767_225_600_000_000_000

	hostnameColumn  = "hostname"
	timestampColumn = "timestamp_ns"
	eventDateColumn = "event_date"
	// versionColumn is kept: the build that wrote a capture is not a secret,
	// and the manifest names it anyway.
	versionColumn = "daemon_version"
)

// Report lists what Scrub could not map.
type Report struct {
	// Invalid names each address cell that is neither empty nor a 4- or
	// 16-byte address ("column row N: invalid:3"). Such a cell is left as
	// is: it is a decoder bug worth seeing, and it holds no full address.
	Invalid []string
}

// Scrub rewrites rows (a slice of structs, modified in place) so they can be
// committed: addresses become documentation addresses, identities become
// small ordinals, the hostname becomes ScrubHostname, other host strings
// become "golden", and timestamps keep their offsets from 2026-01-01.
// Normalize gives the same result before and after, and Scrub is idempotent.
func Scrub(rows any, table Table) (Report, error) {
	p, err := newPlan(rows, table)
	if err != nil {
		return Report{}, err
	}
	base := p.minTimestamp()
	tsField, dateField := -1, -1
	for _, c := range p.cols {
		switch c.name {
		case timestampColumn:
			tsField = c.field
		case eventDateColumn:
			dateField = c.field
		}
	}
	var rep Report
	for i := range p.rows.Len() {
		row := p.rows.Index(i)
		for j, col := range p.cols {
			f := row.Field(col.field)
			c := p.cells[i][j]
			switch p.classes[j] {
			case Ordinal:
				if c.pending {
					setOrdinal(f, p.ordinals[col.name][c.raw])
				}
			case Netns:
				if c.pending {
					f.SetString(netnsName(netnsKind(c.raw), p.netns[c.raw]))
				}
			case Address:
				switch c.addr.kind {
				case addrV4, addrV6:
					setAddr(f, p.docAddr(c.addr), c.addr.n)
				case addrInvalid:
					rep.Invalid = append(rep.Invalid, fmt.Sprintf("%s row %d: %s", col.name, i, addrString(c.addr)))
				default:
				}
			case Host:
				if !isZero(f) && f.Kind() == reflect.String {
					if col.name == hostnameColumn {
						f.SetString(ScrubHostname)
					} else {
						f.SetString(scrubPlaceholder)
					}
				}
			case Volatile:
				scrubVolatile(col.name, f, base)
			default:
			}
		}
		// event_date is the UTC day of the rebased timestamp_ns.
		if tsField >= 0 && dateField >= 0 {
			row.Field(dateField).SetString(UTCDate(row.Field(tsField).Int()))
		}
	}
	return rep, nil
}

// minTimestamp is the earliest timestamp_ns, the base the others are
// rebased from; 0 when there is none.
func (p *plan) minTimestamp() int64 {
	var base int64
	first := true
	for _, c := range p.cols {
		if c.name != timestampColumn {
			continue
		}
		for i := range p.rows.Len() {
			ts := p.rows.Index(i).Field(c.field).Int()
			if first || ts < base {
				base, first = ts, false
			}
		}
	}
	return base
}

// scrubVolatile rebases timestamps and replaces host-derived identities.
func scrubVolatile(name string, f reflect.Value, base int64) {
	switch {
	case name == timestampColumn || name == "treatment_applied_timestamp_ns":
		if f.Int() != 0 {
			f.SetInt(scrubEpoch + f.Int() - base)
		}
	case name == eventDateColumn, name == versionColumn:
		// event_date is set from timestamp_ns once the row is done.
	case f.Kind() == reflect.String && f.String() != "":
		f.SetString(scrubPlaceholder)
	default:
	}
}

// UTCDate is the event_date of a timestamp: its UTC day, as pkg/xtcp's
// utcDateFromNs computes it.
func UTCDate(ns int64) string {
	return time.Unix(0, ns).UTC().Format("2006-01-02")
}

// setOrdinal writes ordinal n into an identity column: the number itself,
// or for a string (a container id) n as 64 hex digits.
func setOrdinal(f reflect.Value, n int) {
	switch f.Kind() {
	case reflect.String:
		f.SetString(fmt.Sprintf("%064x", n))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		f.SetInt(int64(n))
	case reflect.Slice:
		f.SetBytes([]byte(strconv.Itoa(n)))
	default:
		f.SetUint(uint64(n))
	}
}

// setAddr writes a documentation address in the column's original layout: a
// string, 4 bytes, or 16 bytes with a v4 address in the first 4.
func setAddr(f reflect.Value, doc netip.Addr, origLen int) {
	if f.Kind() == reflect.String {
		f.SetString(doc.String())
		return
	}
	out := make([]byte, origLen)
	copy(out, doc.AsSlice())
	f.SetBytes(out)
}

// CheckScrubbed lists every cell of rows (a slice of structs) that Scrub
// would have changed: an address outside loopback, unspecified and the
// documentation ranges, or a host or volatile string that is not a
// placeholder. A committed fixture must list none.
func CheckScrubbed(rows any, table Table) ([]string, error) {
	p, err := newPlan(rows, table)
	if err != nil {
		return nil, err
	}
	var bad []string
	for i := range p.rows.Len() {
		row := p.rows.Index(i)
		for j, col := range p.cols {
			f := row.Field(col.field)
			c := p.cells[i][j]
			switch p.classes[j] {
			case Address:
				if (c.addr.kind == addrV4 || c.addr.kind == addrV6) && !isDocAddr(c.addr.addr) {
					bad = append(bad, fmt.Sprintf("%s row %d: unscrubbed address %s", col.name, i, c.addr.addr))
				}
			case Host, Volatile:
				if f.Kind() != reflect.String || col.name == eventDateColumn || col.name == versionColumn {
					continue
				}
				if s := f.String(); s != "" && s != ScrubHostname && s != scrubPlaceholder {
					bad = append(bad, fmt.Sprintf("%s row %d: unscrubbed %q", col.name, i, s))
				}
			default:
			}
		}
	}
	return bad, nil
}

var docPrefixes = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("2001:db8::/32")}

func isDocAddr(a netip.Addr) bool {
	for _, p := range docPrefixes {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
