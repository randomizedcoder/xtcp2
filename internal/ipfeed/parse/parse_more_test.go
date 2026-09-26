package parse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// The parsers under test here (azure, atlassian, fastly, m365, oci, salesforce)
// deliberately do no CIDR validation, canonicalisation, or de-duplication: they
// copy the feed's prefix string verbatim and leave rejection to the combine
// stage. The corner rows below pin that pass-through contract so a future
// change to it is a conscious one.

// parserCase is one table row shared by all six parser tests. expected is the
// full ordered record list (compared field-for-field against the parser's
// output); expectedErr, when non-empty, is a substring the returned error must
// contain and implies no records are checked.
type parserCase struct {
	description string
	in          string
	expected    []model.Record
	expectedErr string
}

// checkRecords asserts the error expectation and the exact ordered record list.
// A nil and an empty slice are both accepted for "zero records".
func checkRecords(t *testing.T, desc string, got []model.Record, err error, want []model.Record, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if err == nil {
			t.Fatalf("%s: expected error containing %q, got nil", desc, wantErr)
		}
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("%s: error %q does not contain %q", desc, err.Error(), wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", desc, err)
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if len(got) != len(want) {
		t.Fatalf("%s: got %d records, want %d\n got: %+v\nwant: %+v", desc, len(got), len(want), got, want)
	}
	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Errorf("%s: record[%d] mismatch\n got: %+v\nwant: %+v", desc, i, got[i], want[i])
		}
	}
}

func runParserCases(t *testing.T, parserName string, meta SourceMeta, tests []parserCase) {
	t.Helper()
	p, ok := Get(parserName)
	if !ok {
		t.Fatalf("parser %q is not registered", parserName)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), meta, ts)
			checkRecords(t, tc.description, got, err, tc.expected, tc.expectedErr)
		})
	}
}

// TestAzureServiceTags covers the Azure Service Tags parser: one record per
// addressPrefixes entry, Service = systemService (falling back to the tag
// name), Region = properties.region.
func TestAzureServiceTags(t *testing.T) {
	meta := SourceMeta{
		Name: "azure-service-tags", Provider: "azure",
		URL:        "https://www.microsoft.com/en-us/download/details.aspx?id=56519",
		SourceType: "provider_feed", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "microsoft", ServiceOperator: "microsoft"},
	}
	mk := func(prefix, service, region string) model.Record {
		r := meta.Base(ts)
		r.Prefix, r.Service, r.Region = prefix, service, region
		return r
	}
	tests := []parserCase{
		// positive
		{
			description: "positive: realistic tag with v4+v6 prefixes yields one record per prefix with systemService and region",
			in: `{"changeNumber":363,"cloud":"Public","values":[{"name":"ActionGroup","id":"ActionGroup",
			  "properties":{"changeNumber":52,"region":"","regionId":0,"platform":"Azure","systemService":"ActionGroup",
			  "addressPrefixes":["4.145.74.52/30","2603:1000:4::140/123"],"networkFeatures":["API","NSG","UDR","FW"]}},
			 {"name":"AzureCloud.eastus","id":"AzureCloud.eastus",
			  "properties":{"region":"eastus","platform":"Azure","systemService":"AzureCloud",
			  "addressPrefixes":["13.68.128.0/17"]}}]}`,
			expected: []model.Record{
				mk("4.145.74.52/30", "ActionGroup", ""),
				mk("2603:1000:4::140/123", "ActionGroup", ""),
				mk("13.68.128.0/17", "AzureCloud", "eastus"),
			},
		},
		{
			description: "positive: empty systemService falls back to the tag name",
			in:          `{"values":[{"name":"ApiManagement.WestUS","properties":{"region":"westus","systemService":"","addressPrefixes":["13.64.39.16/32"]}}]}`,
			expected:    []model.Record{mk("13.64.39.16/32", "ApiManagement.WestUS", "westus")},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `{"values":[`,
			expectedErr: "azure_service_tags:",
		},
		{
			description: "negative: top-level array instead of object errors",
			in:          `[{"name":"x"}]`,
			expectedErr: "azure_service_tags:",
		},
		{
			description: "negative: addressPrefixes as a scalar instead of an array errors",
			in:          `{"values":[{"name":"x","properties":{"addressPrefixes":"1.2.3.0/24"}}]}`,
			expectedErr: "azure_service_tags:",
		},
		// boundary
		{
			description: "boundary: empty values array yields zero records",
			in:          `{"changeNumber":1,"cloud":"Public","values":[]}`,
			expected:    nil,
		},
		{
			description: "boundary: a tag with an empty addressPrefixes array contributes no records",
			in:          `{"values":[{"name":"Empty","properties":{"systemService":"Empty","addressPrefixes":[]}},{"name":"B","properties":{"systemService":"B","addressPrefixes":["10.0.0.0/8"]}}]}`,
			expected:    []model.Record{mk("10.0.0.0/8", "B", "")},
		},
		{
			description: "boundary: JSON null decodes to an empty document and zero records",
			in:          `null`,
			expected:    nil,
		},
		{
			description: "boundary: a prefix without a mask is passed through verbatim for combine to judge",
			in:          `{"values":[{"name":"S","properties":{"systemService":"S","addressPrefixes":["20.1.2.3"]}}]}`,
			expected:    []model.Record{mk("20.1.2.3", "S", "")},
		},
		// corner
		{
			description: "corner: duplicate prefixes within and across tags are all retained (no dedup)",
			in:          `{"values":[{"name":"A","properties":{"systemService":"A","addressPrefixes":["10.0.0.0/8","10.0.0.0/8"]}},{"name":"B","properties":{"systemService":"B","addressPrefixes":["10.0.0.0/8"]}}]}`,
			expected: []model.Record{
				mk("10.0.0.0/8", "A", ""), mk("10.0.0.0/8", "A", ""), mk("10.0.0.0/8", "B", ""),
			},
		},
		{
			description: "corner: whitespace and host bits are not canonicalised",
			in:          `{"values":[{"name":"A","properties":{"systemService":"A","addressPrefixes":[" 10.0.0.1/8 "]}}]}`,
			expected:    []model.Record{mk(" 10.0.0.1/8 ", "A", "")},
		},
		{
			description: "corner: unknown fields at every level are ignored",
			in:          `{"extra":1,"values":[{"name":"A","bogus":true,"properties":{"systemService":"A","addressPrefixes":["10.0.0.0/8"],"networkFeatures":["API"]}}]}`,
			expected:    []model.Record{mk("10.0.0.0/8", "A", "")},
		},
	}
	runParserCases(t, "azure_service_tags", meta, tests)
}

// TestAtlassian covers the Atlassian parser: one record per item, with the
// product/region/direction arrays comma-joined and creationDate stamped as
// SourceTimestamp.
func TestAtlassian(t *testing.T) {
	meta := SourceMeta{
		Name: "atlassian", Provider: "atlassian", URL: "https://ip-ranges.atlassian.com/",
		SourceType: "provider_feed", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "atlassian", ServiceOperator: "atlassian"},
	}
	mk := func(prefix, product, region, direction, sourceTS string) model.Record {
		r := meta.Base(ts)
		r.Prefix, r.Product, r.Region, r.Direction, r.SourceTimestamp = prefix, product, region, direction, sourceTS
		return r
	}
	const created = "2026-01-01T00:00:00.000000Z"
	tests := []parserCase{
		// positive
		{
			description: "positive: realistic items with multi-valued product/region/direction are comma-joined and creationDate stamped",
			in: `{"creationDate":"` + created + `","syncToken":1735689600,"items":[
			  {"network":"3.26.128.128","mask_len":26,"cidr":"3.26.128.128/26","mask":"255.255.255.192",
			   "region":["ap-southeast-2"],"product":["jira","confluence"],"direction":["egress"],"perimeter":"commercial"},
			  {"network":"2401:1d80:3000::","mask_len":36,"cidr":"2401:1d80:3000::/36",
			   "region":["global","us-east-1"],"product":["bitbucket"],"direction":["ingress","egress"]}]}`,
			expected: []model.Record{
				mk("3.26.128.128/26", "jira,confluence", "ap-southeast-2", "egress", created),
				mk("2401:1d80:3000::/36", "bitbucket", "global,us-east-1", "ingress,egress", created),
			},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `{"items":[{`,
			expectedErr: "atlassian:",
		},
		{
			description: "negative: top-level array instead of object errors",
			in:          `[]`,
			expectedErr: "atlassian:",
		},
		{
			description: "negative: product as a scalar instead of an array errors",
			in:          `{"items":[{"cidr":"1.0.0.0/24","product":"jira"}]}`,
			expectedErr: "atlassian:",
		},
		// boundary
		{
			description: "boundary: empty items array yields zero records",
			in:          `{"creationDate":"` + created + `","items":[]}`,
			expected:    nil,
		},
		{
			description: "boundary: missing creationDate leaves SourceTimestamp empty",
			in:          `{"items":[{"cidr":"1.0.0.0/24","region":["r"],"product":["p"],"direction":["egress"]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "p", "r", "egress", "")},
		},
		{
			description: "boundary: item with no metadata arrays yields empty joined fields",
			in:          `{"items":[{"cidr":"1.0.0.0/24"}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "", "", "", "")},
		},
		{
			description: "boundary: a bare address without a mask is passed through verbatim",
			in:          `{"items":[{"cidr":"1.2.3.4"}]}`,
			expected:    []model.Record{mk("1.2.3.4", "", "", "", "")},
		},
		// corner
		{
			description: "corner: duplicate items are retained in feed order",
			in:          `{"items":[{"cidr":"1.0.0.0/24","product":["jira"]},{"cidr":"1.0.0.0/24","product":["jira"]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "jira", "", "", ""), mk("1.0.0.0/24", "jira", "", "", "")},
		},
		{
			description: "corner: an item with an empty cidr still produces a record (combine rejects it)",
			in:          `{"items":[{"product":["jira"]}]}`,
			expected:    []model.Record{mk("", "jira", "", "", "")},
		},
		{
			description: "corner: unknown fields are ignored",
			in:          `{"foo":"bar","items":[{"cidr":"1.0.0.0/24","perimeter":"commercial","nested":{"x":1}}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "", "", "", "")},
		},
	}
	runParserCases(t, "atlassian", meta, tests)
}

// TestFastly covers the Fastly public-ip-list parser: addresses then
// ipv6_addresses, one record each, with no per-record metadata.
func TestFastly(t *testing.T) {
	meta := SourceMeta{
		Name: "fastly", Provider: "fastly", URL: "https://api.fastly.com/public-ip-list",
		SourceType: "provider_api", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "fastly", ServiceOperator: "fastly"},
	}
	mk := func(prefix string) model.Record {
		r := meta.Base(ts)
		r.Prefix = prefix
		return r
	}
	tests := []parserCase{
		// positive
		{
			description: "positive: v4 addresses come first, then v6, each as a bare record",
			in:          `{"addresses":["23.235.32.0/20","43.249.72.0/22"],"ipv6_addresses":["2a04:4e40::/32","2a04:4e42::/32"]}`,
			expected: []model.Record{
				mk("23.235.32.0/20"), mk("43.249.72.0/22"), mk("2a04:4e40::/32"), mk("2a04:4e42::/32"),
			},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `{"addresses":["1.0.0.0/24"`,
			expectedErr: "fastly:",
		},
		{
			description: "negative: top-level array instead of object errors",
			in:          `["1.0.0.0/24"]`,
			expectedErr: "fastly:",
		},
		{
			description: "negative: a non-string element in addresses errors",
			in:          `{"addresses":[1]}`,
			expectedErr: "fastly:",
		},
		// boundary
		{
			description: "boundary: both arrays empty yields zero records",
			in:          `{"addresses":[],"ipv6_addresses":[]}`,
			expected:    nil,
		},
		{
			description: "boundary: empty object yields zero records",
			in:          `{}`,
			expected:    nil,
		},
		{
			description: "boundary: only v6 present",
			in:          `{"ipv6_addresses":["2a04:4e40::/32"]}`,
			expected:    []model.Record{mk("2a04:4e40::/32")},
		},
		{
			description: "boundary: a host address without a mask is passed through verbatim",
			in:          `{"addresses":["23.235.32.1"]}`,
			expected:    []model.Record{mk("23.235.32.1")},
		},
		// corner
		{
			description: "corner: duplicates across the two arrays are retained",
			in:          `{"addresses":["1.0.0.0/24","1.0.0.0/24"],"ipv6_addresses":["1.0.0.0/24"]}`,
			expected:    []model.Record{mk("1.0.0.0/24"), mk("1.0.0.0/24"), mk("1.0.0.0/24")},
		},
		{
			description: "corner: a v6 prefix listed under addresses is not re-sorted; order is v4 array then v6 array",
			in:          `{"addresses":["2a04::/32"],"ipv6_addresses":["1.0.0.0/24"]}`,
			expected:    []model.Record{mk("2a04::/32"), mk("1.0.0.0/24")},
		},
		{
			description: "corner: unknown fields are ignored",
			in:          `{"addresses":["1.0.0.0/24"],"version":2,"meta":{"a":1}}`,
			expected:    []model.Record{mk("1.0.0.0/24")},
		},
	}
	runParserCases(t, "fastly", meta, tests)
}

// TestM365 covers the Microsoft 365 endpoints parser: a top-level array of
// endpoint sets, one record per ips entry, Service = serviceArea and Product =
// category (which overrides the source default product).
func TestM365(t *testing.T) {
	meta := SourceMeta{
		Name: "m365-worldwide", Provider: "microsoft",
		URL:        "https://endpoints.office.com/endpoints/worldwide?clientrequestid=00000000-0000-0000-0000-000000000000",
		SourceType: "provider_api", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "microsoft", ServiceOperator: "microsoft", Product: "microsoft-365"},
	}
	mk := func(prefix, service, product string) model.Record {
		r := meta.Base(ts)
		r.Prefix, r.Service, r.Product = prefix, service, product
		return r
	}
	tests := []parserCase{
		// positive
		{
			description: "positive: realistic endpoint sets yield one record per ip with serviceArea and category",
			in: `[{"id":1,"serviceArea":"Exchange","serviceAreaDisplayName":"Exchange Online","urls":["outlook.office.com"],
			   "ips":["13.107.6.152/31","2603:1006::/40"],"tcpPorts":"80,443","expressRoute":true,"category":"Optimize","required":true},
			  {"id":31,"serviceArea":"Skype","serviceAreaDisplayName":"Skype for Business Online and Microsoft Teams",
			   "ips":["52.112.0.0/14"],"udpPorts":"3478,3479","category":"Allow","required":true}]`,
			expected: []model.Record{
				mk("13.107.6.152/31", "Exchange", "Optimize"),
				mk("2603:1006::/40", "Exchange", "Optimize"),
				mk("52.112.0.0/14", "Skype", "Allow"),
			},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `[{"serviceArea":"Exchange","ips":[`,
			expectedErr: "m365:",
		},
		{
			description: "negative: top-level object instead of array errors",
			in:          `{"serviceArea":"Exchange","ips":["1.0.0.0/24"]}`,
			expectedErr: "m365:",
		},
		{
			description: "negative: ips as a scalar instead of an array errors",
			in:          `[{"serviceArea":"Exchange","ips":"1.0.0.0/24"}]`,
			expectedErr: "m365:",
		},
		// boundary
		{
			description: "boundary: empty array yields zero records",
			in:          `[]`,
			expected:    nil,
		},
		{
			description: "boundary: URL-only endpoint sets (no ips key) contribute no records",
			in:          `[{"id":2,"serviceArea":"Exchange","urls":["*.outlook.com"],"category":"Default"},{"id":3,"serviceArea":"SharePoint","ips":["13.107.136.0/22"],"category":"Optimize"}]`,
			expected:    []model.Record{mk("13.107.136.0/22", "SharePoint", "Optimize")},
		},
		{
			description: "boundary: an endpoint set with an empty ips array contributes no records",
			in:          `[{"serviceArea":"Exchange","ips":[],"category":"Default"}]`,
			expected:    nil,
		},
		{
			description: "boundary: a host address without a mask is passed through verbatim",
			in:          `[{"serviceArea":"Exchange","ips":["13.107.6.152"],"category":"Optimize"}]`,
			expected:    []model.Record{mk("13.107.6.152", "Exchange", "Optimize")},
		},
		// corner
		{
			description: "corner: a missing category clears the source default product rather than keeping it",
			in:          `[{"serviceArea":"Exchange","ips":["13.107.6.152/31"]}]`,
			expected:    []model.Record{mk("13.107.6.152/31", "Exchange", "")},
		},
		{
			description: "corner: duplicate ips across endpoint sets are retained",
			in:          `[{"serviceArea":"Exchange","ips":["1.0.0.0/24"],"category":"Optimize"},{"serviceArea":"Skype","ips":["1.0.0.0/24"],"category":"Allow"}]`,
			expected:    []model.Record{mk("1.0.0.0/24", "Exchange", "Optimize"), mk("1.0.0.0/24", "Skype", "Allow")},
		},
		{
			description: "corner: unknown fields (ports, urls, notes) are ignored",
			in:          `[{"id":9,"serviceArea":"Common","ips":["1.0.0.0/24"],"tcpPorts":"443","urls":["a.b"],"notes":"x","category":"Allow","extra":{"k":1}}]`,
			expected:    []model.Record{mk("1.0.0.0/24", "Common", "Allow")},
		},
	}
	runParserCases(t, "m365", meta, tests)
}

// TestOCI covers the Oracle Cloud parser: regions -> cidrs, one record per
// cidr, Region from the group, Service = comma-joined tags, and
// last_updated_timestamp stamped as SourceTimestamp.
func TestOCI(t *testing.T) {
	meta := SourceMeta{
		Name: "oci", Provider: "oracle", URL: "https://docs.oracle.com/iaas/tools/public_ip_ranges.json",
		SourceType: "provider_feed", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "oracle", ServiceOperator: "oracle"},
	}
	mk := func(prefix, region, service, sourceTS string) model.Record {
		r := meta.Base(ts)
		r.Prefix, r.Region, r.Service, r.SourceTimestamp = prefix, region, service, sourceTS
		return r
	}
	const updated = "2026-01-01T00:00:00.000000"
	tests := []parserCase{
		// positive
		{
			description: "positive: realistic regions with tagged cidrs yield per-cidr records with region, joined tags, and timestamp",
			in: `{"last_updated_timestamp":"` + updated + `","regions":[
			  {"region":"us-phoenix-1","cidrs":[{"cidr":"129.146.0.0/21","tags":["OCI"]},{"cidr":"129.146.8.0/22","tags":["OSN","OBJECT_STORAGE"]}]},
			  {"region":"eu-frankfurt-1","cidrs":[{"cidr":"2603:c020:0:8000::/50","tags":["OCI"]}]}]}`,
			expected: []model.Record{
				mk("129.146.0.0/21", "us-phoenix-1", "OCI", updated),
				mk("129.146.8.0/22", "us-phoenix-1", "OSN,OBJECT_STORAGE", updated),
				mk("2603:c020:0:8000::/50", "eu-frankfurt-1", "OCI", updated),
			},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `{"regions":[{"region":"x","cidrs":[`,
			expectedErr: "oci:",
		},
		{
			description: "negative: top-level array instead of object errors",
			in:          `[{"region":"x"}]`,
			expectedErr: "oci:",
		},
		{
			description: "negative: cidrs as an array of strings (not objects) errors",
			in:          `{"regions":[{"region":"x","cidrs":["1.0.0.0/24"]}]}`,
			expectedErr: "oci:",
		},
		// boundary
		{
			description: "boundary: empty regions array yields zero records",
			in:          `{"last_updated_timestamp":"` + updated + `","regions":[]}`,
			expected:    nil,
		},
		{
			description: "boundary: a region with an empty cidrs array contributes no records",
			in:          `{"regions":[{"region":"empty","cidrs":[]},{"region":"r","cidrs":[{"cidr":"1.0.0.0/24","tags":["OCI"]}]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "OCI", "")},
		},
		{
			description: "boundary: a cidr with no tags yields an empty service",
			in:          `{"regions":[{"region":"r","cidrs":[{"cidr":"1.0.0.0/24"}]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "", "")},
		},
		{
			description: "boundary: a bare address without a mask is passed through verbatim",
			in:          `{"regions":[{"region":"r","cidrs":[{"cidr":"129.146.0.1","tags":["OCI"]}]}]}`,
			expected:    []model.Record{mk("129.146.0.1", "r", "OCI", "")},
		},
		// corner
		{
			description: "corner: the same cidr in two regions yields two records (no dedup)",
			in:          `{"regions":[{"region":"a","cidrs":[{"cidr":"1.0.0.0/24","tags":["OCI"]}]},{"region":"b","cidrs":[{"cidr":"1.0.0.0/24","tags":["OCI"]}]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "a", "OCI", ""), mk("1.0.0.0/24", "b", "OCI", "")},
		},
		{
			description: "corner: tag order is preserved in the joined service string",
			in:          `{"regions":[{"region":"r","cidrs":[{"cidr":"1.0.0.0/24","tags":["OSN","OCI","OBJECT_STORAGE"]}]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "OSN,OCI,OBJECT_STORAGE", "")},
		},
		{
			description: "corner: unknown fields are ignored",
			in:          `{"extra":true,"regions":[{"region":"r","key":"k","cidrs":[{"cidr":"1.0.0.0/24","tags":["OCI"],"note":"n"}]}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "OCI", "")},
		},
	}
	runParserCases(t, "oci", meta, tests)
}

// TestSalesforce covers the Salesforce (Hyperforce) parser: AWS-style
// prefixes / ipv6_prefixes with region and direction, createDate stamped as
// SourceTimestamp, and the source default Service left untouched.
func TestSalesforce(t *testing.T) {
	meta := SourceMeta{
		Name: "salesforce", Provider: "salesforce", URL: "https://ip-ranges.salesforce.com/ip-ranges.json",
		SourceType: "provider_feed", Confidence: "authoritative",
		Defaults: Defaults{NetworkOwner: "salesforce", ServiceOperator: "salesforce", Service: "hyperforce"},
	}
	mk := func(prefix, region, direction, sourceTS string) model.Record {
		r := meta.Base(ts)
		r.Prefix, r.Region, r.Direction, r.SourceTimestamp = prefix, region, direction, sourceTS
		return r
	}
	const created = "2026-01-01-00-00-00"
	tests := []parserCase{
		// positive
		{
			description: "positive: v4 then v6 prefixes with region/direction; default service hyperforce is retained",
			in: `{"syncToken":"1735689600","createDate":"` + created + `",
			  "prefixes":[{"ip_prefix":"13.108.0.0/14","region":"GLOBAL","direction":"inbound"},{"ip_prefix":"96.43.144.0/20","region":"us-east-1","direction":"outbound"}],
			  "ipv6_prefixes":[{"ipv6_prefix":"2600:1f14:4f7:2000::/56","region":"us-west-2","direction":"outbound"}]}`,
			expected: []model.Record{
				mk("13.108.0.0/14", "GLOBAL", "inbound", created),
				mk("96.43.144.0/20", "us-east-1", "outbound", created),
				mk("2600:1f14:4f7:2000::/56", "us-west-2", "outbound", created),
			},
		},
		// negative
		{
			description: "negative: malformed JSON errors with the parser prefix",
			in:          `{"prefixes":[{"ip_prefix":`,
			expectedErr: "salesforce:",
		},
		{
			description: "negative: top-level array instead of object errors",
			in:          `[]`,
			expectedErr: "salesforce:",
		},
		{
			description: "negative: prefixes as an array of strings (not objects) errors",
			in:          `{"prefixes":["1.0.0.0/24"]}`,
			expectedErr: "salesforce:",
		},
		// boundary
		{
			description: "boundary: both arrays empty yields zero records",
			in:          `{"createDate":"` + created + `","prefixes":[],"ipv6_prefixes":[]}`,
			expected:    nil,
		},
		{
			description: "boundary: empty object yields zero records",
			in:          `{}`,
			expected:    nil,
		},
		{
			description: "boundary: only v6 present, missing createDate leaves SourceTimestamp empty",
			in:          `{"ipv6_prefixes":[{"ipv6_prefix":"2600::/16","region":"r","direction":"inbound"}]}`,
			expected:    []model.Record{mk("2600::/16", "r", "inbound", "")},
		},
		{
			description: "boundary: an entry missing region and direction yields empty fields",
			in:          `{"prefixes":[{"ip_prefix":"1.0.0.0/24"}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "", "", "")},
		},
		{
			description: "boundary: a host address without a mask is passed through verbatim",
			in:          `{"prefixes":[{"ip_prefix":"13.108.0.1","region":"GLOBAL","direction":"inbound"}]}`,
			expected:    []model.Record{mk("13.108.0.1", "GLOBAL", "inbound", "")},
		},
		// corner
		{
			description: "corner: the same prefix listed inbound and outbound yields two records",
			in:          `{"prefixes":[{"ip_prefix":"1.0.0.0/24","region":"r","direction":"inbound"},{"ip_prefix":"1.0.0.0/24","region":"r","direction":"outbound"}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "inbound", ""), mk("1.0.0.0/24", "r", "outbound", "")},
		},
		{
			description: "corner: an ipv6_prefixes entry using the v4 key name yields an empty prefix (not cross-read)",
			in:          `{"ipv6_prefixes":[{"ip_prefix":"2600::/16","region":"r","direction":"inbound"}]}`,
			expected:    []model.Record{mk("", "r", "inbound", "")},
		},
		{
			description: "corner: unknown fields (syncToken, service, network_border_group) are ignored",
			in:          `{"syncToken":"x","prefixes":[{"ip_prefix":"1.0.0.0/24","region":"r","direction":"inbound","service":"S","network_border_group":"g"}]}`,
			expected:    []model.Record{mk("1.0.0.0/24", "r", "inbound", "")},
		},
	}
	runParserCases(t, "salesforce", meta, tests)
}
