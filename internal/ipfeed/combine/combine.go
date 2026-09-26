// Package combine validates parsed records and aggregates them into the final
// dataset, tracking the positive/negative boundaries: a "positive" record has
// a valid CIDR and is kept; a "negative" record is rejected and counted with a
// bounded reason so downstream metric cardinality stays safe.
package combine

import (
	"net/netip"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// RejectReason is a closed set of rejection categories. It is deliberately a
// small enum (never raw error text) so it is safe to use as a metric label.
type RejectReason string

const (
	ReasonParseError   RejectReason = "ParseError"   // prefix failed net/netip.ParsePrefix
	ReasonEmpty        RejectReason = "Empty"        // empty prefix string
	ReasonDuplicate    RejectReason = "Duplicate"    // identical record already seen
	ReasonSourceFailed RejectReason = "SourceFailed" // whole source failed upstream
)

// Rejected is one dropped record with its reason.
type Rejected struct {
	Record model.Record
	Reason RejectReason
}

// Result holds the outcome of combining one source's parsed rows.
type Result struct {
	Valid    []model.Record // positive (+) boundary
	Rejected []Rejected     // negative (-) boundary
}

// ValidCount returns the positive-boundary count.
func (r Result) ValidCount() int { return len(r.Valid) }

// RejectedCount returns the negative-boundary count.
func (r Result) RejectedCount() int { return len(r.Rejected) }

// dedupKey identifies a record for duplicate detection. Prefix plus the
// classification fields — the same prefix under a different service/region is
// intentionally kept (overlapping classifications are meaningful).
func dedupKey(r model.Record) string {
	return r.Prefix + "|" + r.NetworkOwner + "|" + r.ServiceOperator + "|" +
		r.Service + "|" + r.Product + "|" + r.Region + "|" + r.Direction + "|" + r.SourceName
}

// Validate canonicalizes and validates records from a single source. seen is a
// cross-source set so identical records from overlapping feeds are counted as
// duplicates once. Each input record's Prefix is parsed; on success the
// canonical form and ip_version are written back and the record is kept.
func Validate(records []model.Record, seen map[string]struct{}) Result {
	var res Result
	for i := range records {
		r := records[i] // local copy: canonicalization must not mutate the caller's slice
		if r.Prefix == "" {
			res.Rejected = append(res.Rejected, Rejected{Record: r, Reason: ReasonEmpty})
			continue
		}
		p, err := netip.ParsePrefix(r.Prefix)
		if err != nil {
			res.Rejected = append(res.Rejected, Rejected{Record: r, Reason: ReasonParseError})
			continue
		}
		// Canonicalize: masked prefix + normalized address text. This turns a
		// host-bits-set input like 1.2.3.4/24 into 1.2.3.0/24.
		p = p.Masked()
		r.Prefix = p.String()
		if p.Addr().Is4() {
			r.IPVersion = 4
		} else {
			r.IPVersion = 6
		}
		key := dedupKey(r)
		if _, dup := seen[key]; dup {
			res.Rejected = append(res.Rejected, Rejected{Record: r, Reason: ReasonDuplicate})
			continue
		}
		seen[key] = struct{}{}
		res.Valid = append(res.Valid, r)
	}
	return res
}
