package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("salesforce", salesforceFeed{}) }

// salesforceFeed parses https://ip-ranges.salesforce.com/ip-ranges.json
// (Hyperforce), an AWS-style document with ip_prefix / ipv6_prefix entries.
// Each entry's prefix was a string with a direction (inbound/outbound); by
// 2026-10 it is a list of prefixes per region and the direction is gone.
// Both shapes parse, one record per prefix.
type salesforceFeed struct{}

type salesforceDoc struct {
	CreateDate string `json:"createDate"`
	Prefixes   []struct {
		IPPrefix  stringOrList `json:"ip_prefix"`
		Region    string       `json:"region"`
		Direction string       `json:"direction"`
	} `json:"prefixes"`
	IPv6Prefixes []struct {
		IPv6Prefix stringOrList `json:"ipv6_prefix"`
		Region     string       `json:"region"`
		Direction  string       `json:"direction"`
	} `json:"ipv6_prefixes"`
}

// stringOrList is a JSON string or an array of strings. A missing key or
// null decodes to one empty string, as a missing string field did, so the
// record still reaches validation and is counted as rejected.
type stringOrList []string

func (l *stringOrList) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '[' {
		var list []string
		if err := json.Unmarshal(b, &list); err != nil {
			return err
		}
		*l = list
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*l = stringOrList{s}
	return nil
}

// prefixes returns the decoded prefixes, or one empty prefix when the key
// was absent.
func (l stringOrList) prefixes() []string {
	if l == nil {
		return []string{""}
	}
	return l
}

func (salesforceFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d salesforceDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("salesforce: %w", err)
	}
	out := make([]model.Record, 0, len(d.Prefixes)+len(d.IPv6Prefixes))
	for _, p := range d.Prefixes {
		for _, prefix := range p.IPPrefix.prefixes() {
			r := meta.Base(retrievedAt)
			r.Prefix = prefix
			r.Region = p.Region
			r.Direction = p.Direction
			r.SourceTimestamp = d.CreateDate
			out = append(out, r)
		}
	}
	for _, p := range d.IPv6Prefixes {
		for _, prefix := range p.IPv6Prefix.prefixes() {
			r := meta.Base(retrievedAt)
			r.Prefix = prefix
			r.Region = p.Region
			r.Direction = p.Direction
			r.SourceTimestamp = d.CreateDate
			out = append(out, r)
		}
	}
	return out, nil
}
