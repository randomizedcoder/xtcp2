package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// The GCP, Applebot, and Google crawler feeds share a shape: a top-level
// "prefixes" array whose entries carry either "ipv4Prefix" or "ipv6Prefix",
// and (for GCP) optional "service"/"scope" metadata. One parser type serves
// all three, registered under three keys.
func init() {
	Register("gcp_ipranges", prefixesFeed{})
	Register("applebot", prefixesFeed{})
	Register("google_crawlers", prefixesFeed{})
}

type prefixesFeed struct{}

type prefixesDoc struct {
	SyncToken    string `json:"syncToken"`
	CreationTime string `json:"creationTime"`
	Prefixes     []struct {
		IPv4Prefix string `json:"ipv4Prefix"`
		IPv6Prefix string `json:"ipv6Prefix"`
		Service    string `json:"service"`
		Scope      string `json:"scope"`
	} `json:"prefixes"`
}

func (prefixesFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d prefixesDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("prefixes feed: %w", err)
	}
	out := make([]model.Record, 0, len(d.Prefixes))
	for _, p := range d.Prefixes {
		cidr := p.IPv4Prefix
		if cidr == "" {
			cidr = p.IPv6Prefix
		}
		if cidr == "" {
			continue
		}
		r := meta.Base(retrievedAt)
		r.Prefix = cidr
		if p.Service != "" {
			r.Service = p.Service
		}
		if p.Scope != "" {
			r.Region = p.Scope
		}
		r.SourceTimestamp = d.CreationTime
		out = append(out, r)
	}
	return out, nil
}
