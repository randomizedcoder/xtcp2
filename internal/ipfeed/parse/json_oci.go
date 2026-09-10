package parse

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("oci", ociFeed{}) }

// ociFeed parses https://docs.oracle.com/iaas/tools/public_ip_ranges.json,
// which groups CIDRs by region, each with a list of service tags.
type ociFeed struct{}

type ociDoc struct {
	LastUpdatedTimestamp string `json:"last_updated_timestamp"`
	Regions              []struct {
		Region string `json:"region"`
		CIDRs  []struct {
			CIDR string   `json:"cidr"`
			Tags []string `json:"tags"`
		} `json:"cidrs"`
	} `json:"regions"`
}

func (ociFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d ociDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("oci: %w", err)
	}
	var out []model.Record
	for _, reg := range d.Regions {
		for _, c := range reg.CIDRs {
			r := meta.Base(retrievedAt)
			r.Prefix = c.CIDR
			r.Region = reg.Region
			r.Service = strings.Join(c.Tags, ",")
			r.SourceTimestamp = d.LastUpdatedTimestamp
			out = append(out, r)
		}
	}
	return out, nil
}
