package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("salesforce", salesforceFeed{}) }

// salesforceFeed parses https://ip-ranges.salesforce.com/ip-ranges.json
// (Hyperforce), an AWS-style document with ip_prefix / ipv6_prefix entries
// that additionally carry a direction (inbound/outbound).
type salesforceFeed struct{}

type salesforceDoc struct {
	CreateDate string `json:"createDate"`
	Prefixes   []struct {
		IPPrefix  string `json:"ip_prefix"`
		Region    string `json:"region"`
		Direction string `json:"direction"`
	} `json:"prefixes"`
	IPv6Prefixes []struct {
		IPv6Prefix string `json:"ipv6_prefix"`
		Region     string `json:"region"`
		Direction  string `json:"direction"`
	} `json:"ipv6_prefixes"`
}

func (salesforceFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d salesforceDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("salesforce: %w", err)
	}
	out := make([]model.Record, 0, len(d.Prefixes)+len(d.IPv6Prefixes))
	for _, p := range d.Prefixes {
		r := meta.Base(retrievedAt)
		r.Prefix = p.IPPrefix
		r.Region = p.Region
		r.Direction = p.Direction
		r.SourceTimestamp = d.CreateDate
		out = append(out, r)
	}
	for _, p := range d.IPv6Prefixes {
		r := meta.Base(retrievedAt)
		r.Prefix = p.IPv6Prefix
		r.Region = p.Region
		r.Direction = p.Direction
		r.SourceTimestamp = d.CreateDate
		out = append(out, r)
	}
	return out, nil
}
