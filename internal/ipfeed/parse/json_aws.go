package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("aws_ip_ranges", awsIPRanges{}) }

// awsIPRanges parses https://ip-ranges.amazonaws.com/ip-ranges.json. It keeps
// the service, region, and network_border_group metadata AWS publishes; the
// createDate becomes the record source_timestamp.
type awsIPRanges struct{}

type awsFeed struct {
	CreateDate string `json:"createDate"`
	Prefixes   []struct {
		IPPrefix           string `json:"ip_prefix"`
		Region             string `json:"region"`
		Service            string `json:"service"`
		NetworkBorderGroup string `json:"network_border_group"`
	} `json:"prefixes"`
	IPv6Prefixes []struct {
		IPv6Prefix         string `json:"ipv6_prefix"`
		Region             string `json:"region"`
		Service            string `json:"service"`
		NetworkBorderGroup string `json:"network_border_group"`
	} `json:"ipv6_prefixes"`
}

func (awsIPRanges) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var f awsFeed
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("aws_ip_ranges: %w", err)
	}
	out := make([]model.Record, 0, len(f.Prefixes)+len(f.IPv6Prefixes))
	for _, p := range f.Prefixes {
		r := meta.Base(retrievedAt)
		r.Prefix = p.IPPrefix
		r.Region = p.Region
		r.Service = p.Service
		r.NetworkBorderGroup = p.NetworkBorderGroup
		r.SourceTimestamp = f.CreateDate
		out = append(out, r)
	}
	for _, p := range f.IPv6Prefixes {
		r := meta.Base(retrievedAt)
		r.Prefix = p.IPv6Prefix
		r.Region = p.Region
		r.Service = p.Service
		r.NetworkBorderGroup = p.NetworkBorderGroup
		r.SourceTimestamp = f.CreateDate
		out = append(out, r)
	}
	return out, nil
}
