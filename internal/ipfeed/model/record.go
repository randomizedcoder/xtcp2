// Package model defines the normalized record produced by ipfeed-collector.
//
// The schema follows the recommended normalized schema from the source
// catalog: an address can carry several independent classifications
// (network ownership vs service operator vs specific service), so overlapping
// records are retained rather than collapsed to one provider per prefix.
package model

// Record is one normalized IP-range classification row.
//
// Struct tags are used both for JSON (debug/NDJSON dumps) and for the Parquet
// writer. Every field is a string (or small int) so the schema stays flat and
// easy to query with DuckDB / parquet tooling. Empty strings mean "not
// provided by this feed" — we do not invent values.
type Record struct {
	Prefix    string `json:"prefix" parquet:"prefix"`
	IPVersion int32  `json:"ip_version" parquet:"ip_version"`
	// ASN is the representative BGP autonomous-system number for this prefix,
	// derived from NetworkOwner via a curated map (internal/ipfeed/asnmap). It is
	// 0 when the owner has no known ASN. This is a per-provider *representative*
	// ASN, not a per-prefix BGP-origin ASN — see the asnmap package docs.
	ASN                uint32 `json:"asn" parquet:"asn"`
	NetworkOwner       string `json:"network_owner" parquet:"network_owner"`
	ServiceOperator    string `json:"service_operator" parquet:"service_operator"`
	Provider           string `json:"provider" parquet:"provider"`
	Service            string `json:"service" parquet:"service"`
	Product            string `json:"product" parquet:"product"`
	Region             string `json:"region" parquet:"region"`
	NetworkBorderGroup string `json:"network_border_group" parquet:"network_border_group"`
	Direction          string `json:"direction" parquet:"direction"`
	SourceName         string `json:"source_name" parquet:"source_name"`
	SourceType         string `json:"source_type" parquet:"source_type"`
	SourceURL          string `json:"source_url" parquet:"source_url"`
	SourceTimestamp    string `json:"source_timestamp" parquet:"source_timestamp"`
	RetrievedAt        string `json:"retrieved_at" parquet:"retrieved_at"`
	Confidence         string `json:"confidence" parquet:"confidence"`
}
