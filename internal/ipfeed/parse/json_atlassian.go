package parse

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("atlassian", atlassianFeed{}) }

// atlassianFeed parses https://ip-ranges.atlassian.com/, whose items each
// carry a cidr plus arrays of product, region, and direction metadata.
type atlassianFeed struct{}

type atlassianDoc struct {
	CreationDate string `json:"creationDate"`
	Items        []struct {
		CIDR      string   `json:"cidr"`
		Region    []string `json:"region"`
		Product   []string `json:"product"`
		Direction []string `json:"direction"`
	} `json:"items"`
}

func (atlassianFeed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d atlassianDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("atlassian: %w", err)
	}
	out := make([]model.Record, 0, len(d.Items))
	for _, it := range d.Items {
		r := meta.Base(retrievedAt)
		r.Prefix = it.CIDR
		r.Product = strings.Join(it.Product, ",")
		r.Region = strings.Join(it.Region, ",")
		r.Direction = strings.Join(it.Direction, ",")
		r.SourceTimestamp = d.CreationDate
		out = append(out, r)
	}
	return out, nil
}
