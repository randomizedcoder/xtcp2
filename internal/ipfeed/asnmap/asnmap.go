// Package asnmap maps a feed's network owner / provider name to a
// representative BGP autonomous-system number (ASN).
//
// The IP-range feeds ipfeed-collector parses identify the owning provider of a
// prefix (network_owner / provider), not its BGP origin ASN. For coarse
// enrichment we map each well-known provider to its primary public ASN using
// the small curated table below.
//
// IMPORTANT — this is a *representative* ASN, not a per-prefix BGP-origin ASN.
// Large providers announce prefixes from several ASNs (e.g. AWS also uses
// AS14618/AS8987; Google also AS36040/AS36384), so a name→ASN map is lossy by
// design. True per-prefix origin (and next-hop) ASN requires a BGP RIB (MRT)
// source, which is a separate, later phase. Values here are best-effort and
// intended to be easy to extend as feeds are added.
package asnmap

import (
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// table maps a normalized (lowercased) owner/provider name to its
// representative ASN. Keys cover both the network_owner and provider spellings
// that appear in sources/*.yaml.
var table = map[string]uint32{
	"google":       15169, // Google LLC
	"gcp":          15169,
	"microsoft":    8075, // Microsoft Corporation
	"azure":        8075,
	"aws":          16509, // Amazon.com (primary; AWS also uses 14618, 8987, …)
	"amazon":       16509,
	"cloudflare":   13335,  // Cloudflare, Inc.
	"fastly":       54113,  // Fastly, Inc.
	"apple":        714,    // Apple Inc.
	"digitalocean": 14061,  // DigitalOcean, LLC
	"github":       36459,  // GitHub, Inc.
	"oracle":       31898,  // Oracle Cloud (OCI)
	"salesforce":   14340,  // Salesforce.com, Inc.
	"atlassian":    133530, // Atlassian Pty Ltd
}

// Lookup returns the representative ASN for an owner/provider pair. It tries
// networkOwner first, then provider; names are matched case-insensitively.
// The bool is false (and asn 0) when neither name is known.
func Lookup(networkOwner, provider string) (uint32, bool) {
	if asn, ok := table[strings.ToLower(strings.TrimSpace(networkOwner))]; ok {
		return asn, true
	}
	if asn, ok := table[strings.ToLower(strings.TrimSpace(provider))]; ok {
		return asn, true
	}
	return 0, false
}

// Annotate sets r.ASN for every record whose owner/provider maps to a known
// ASN, leaving the rest at 0. It mutates records in place.
func Annotate(records []model.Record) {
	for i := range records {
		if asn, ok := Lookup(records[i].NetworkOwner, records[i].Provider); ok {
			records[i].ASN = asn
		}
	}
}
