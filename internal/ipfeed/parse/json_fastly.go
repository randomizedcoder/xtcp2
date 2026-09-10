package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("fastly", fastlyFeed{}) }

// fastlyFeed parses https://api.fastly.com/public-ip-list, which returns two
// flat arrays of CIDRs.
type fastlyFeed struct{}

type fastlyDoc struct {
	Addresses     []string `json:"addresses"`
	IPv6Addresses []string `json:"ipv6_addresses"`
}

func (fastlyFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d fastlyDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("fastly: %w", err)
	}
	out := make([]model.Record, 0, len(d.Addresses)+len(d.IPv6Addresses))
	for _, c := range append(append([]string{}, d.Addresses...), d.IPv6Addresses...) {
		r := meta.Base(retrievedAt)
		r.Prefix = c
		out = append(out, r)
	}
	return out, nil
}
