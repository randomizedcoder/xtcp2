package parse

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("azure_service_tags", azureServiceTags{}) }

// azureServiceTags parses the Azure Service Tags JSON (discovered from the
// download page). Each value carries a systemService, region, and an
// addressPrefixes array covering both IPv4 and IPv6.
type azureServiceTags struct{}

type azureDoc struct {
	ChangeNumber int    `json:"changeNumber"`
	Cloud        string `json:"cloud"`
	Values       []struct {
		Name       string `json:"name"`
		Properties struct {
			Region          string   `json:"region"`
			SystemService   string   `json:"systemService"`
			AddressPrefixes []string `json:"addressPrefixes"`
		} `json:"properties"`
	} `json:"values"`
}

func (azureServiceTags) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	var d azureDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("azure_service_tags: %w", err)
	}
	var out []model.Record
	for _, v := range d.Values {
		service := v.Properties.SystemService
		if service == "" {
			service = v.Name
		}
		for _, c := range v.Properties.AddressPrefixes {
			r := meta.Base(retrievedAt)
			r.Prefix = c
			r.Service = service
			r.Region = v.Properties.Region
			out = append(out, r)
		}
	}
	return out, nil
}
