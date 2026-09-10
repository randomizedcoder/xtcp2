package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("m365", m365Feed{}) }

// m365Feed parses the Microsoft 365 endpoints web service
// (endpoints.office.com/endpoints/worldwide), a top-level array of endpoint
// sets, each with a serviceArea, a category, and an ips array (CIDRs).
type m365Feed struct{}

type m365Entry struct {
	ServiceArea string   `json:"serviceArea"`
	Category    string   `json:"category"`
	IPs         []string `json:"ips"`
}

func (m365Feed) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var entries []m365Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("m365: %w", err)
	}
	var out []model.Record
	for _, e := range entries {
		for _, c := range e.IPs {
			r := meta.Base(retrievedAt)
			r.Prefix = c
			r.Service = e.ServiceArea
			r.Product = e.Category
			out = append(out, r)
		}
	}
	return out, nil
}
