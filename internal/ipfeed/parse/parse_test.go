package parse

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

var testMeta = SourceMeta{
	Name:       "t",
	Provider:   "prov",
	URL:        "https://example/feed",
	SourceType: "provider_feed",
	Confidence: "authoritative",
	Defaults:   Defaults{NetworkOwner: "owner", ServiceOperator: "op"},
}

const ts = "2026-01-01T00:00:00Z"

func TestTextCIDR(t *testing.T) {
	p, _ := Get("text_cidr")
	tests := []struct {
		name    string
		desc    string
		class   string
		in      string
		want    []string
		wantErr bool
	}{
		{"positive", "positive: two plain CIDR lines become two records", "positive",
			"1.2.3.0/24\n2001:db8::/32\n", []string{"1.2.3.0/24", "2001:db8::/32"}, false},
		{"negative_passthrough", "negative: an invalid CIDR line is passed through for the combine stage to reject", "negative",
			"garbage-line\n", []string{"garbage-line"}, false},
		{"boundary_empty", "boundary: empty body yields zero records", "boundary",
			"", nil, false},
		{"boundary_bom_and_blank", "boundary: leading UTF-8 BOM and blank lines are stripped", "boundary",
			"\xEF\xBB\xBF\n1.1.1.0/24\n\n", []string{"1.1.1.0/24"}, false},
		{"corner_comments_and_crlf", "corner: comments are skipped and CRLF is trimmed", "corner",
			"# header\r\n9.9.9.0/24\r\n", []string{"9.9.9.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), testMeta, ts)
			checkPrefixes(t, tc.desc, got, err, tc.want, tc.wantErr)
		})
	}
}

func TestCSV(t *testing.T) {
	p, _ := Get("csv")
	// prefix in col0, region in col1 (AWS geo-feed style), no header.
	meta := testMeta
	meta.Opts = map[string]any{"has_header": false, "prefix_column": 0, "region_column": 1}
	tests := []struct {
		name    string
		desc    string
		class   string
		in      string
		want    []string
		wantErr bool
	}{
		{"positive", "positive: two rows with prefix+region", "positive",
			"1.2.3.0/24,US\n4.4.0.0/16,DE\n", []string{"1.2.3.0/24", "4.4.0.0/16"}, false},
		{"negative_badcsv", "negative: an unterminated quote is a CSV error", "negative",
			"1.2.3.0/24,\"US\n", nil, true},
		{"boundary_empty", "boundary: empty input yields no records", "boundary",
			"", nil, false},
		{"boundary_short_row", "boundary: a row missing the prefix column is skipped", "boundary",
			"\n2.2.2.0/24,US\n", []string{"2.2.2.0/24"}, false},
		{"corner_ragged_and_bom", "corner: ragged extra columns tolerated, BOM stripped", "corner",
			"\xEF\xBB\xBF3.3.3.0/24,US,extra,cols\n", []string{"3.3.3.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), meta, ts)
			checkPrefixes(t, tc.desc, got, err, tc.want, tc.wantErr)
			if tc.name == "positive" && err == nil {
				if got[0].Region != "US" {
					t.Errorf("%s: region = %q, want US", tc.desc, got[0].Region)
				}
			}
		})
	}
}

func TestAWSIPRanges(t *testing.T) {
	p, _ := Get("aws_ip_ranges")
	tests := []struct {
		name    string
		desc    string
		class   string
		in      string
		want    []string
		wantErr bool
	}{
		{"positive", "positive: v4 and v6 prefixes with metadata", "positive",
			`{"createDate":"2026-01-01","prefixes":[{"ip_prefix":"13.248.0.0/16","region":"GLOBAL","service":"AMAZON","network_border_group":"g"}],"ipv6_prefixes":[{"ipv6_prefix":"2600:1f00::/24","region":"us-east-1","service":"EC2"}]}`,
			[]string{"13.248.0.0/16", "2600:1f00::/24"}, false},
		{"negative_badjson", "negative: malformed JSON errors", "negative",
			`{"prefixes":[`, nil, true},
		{"boundary_empty_arrays", "boundary: empty arrays yield no records", "boundary",
			`{"prefixes":[],"ipv6_prefixes":[]}`, nil, false},
		{"corner_unknown_fields", "corner: unknown top-level fields are ignored", "corner",
			`{"syncToken":"x","extra":true,"prefixes":[{"ip_prefix":"1.0.0.0/24","region":"r","service":"S"}]}`,
			[]string{"1.0.0.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), testMeta, ts)
			checkPrefixes(t, tc.desc, got, err, tc.want, tc.wantErr)
			if tc.name == "positive" && err == nil {
				if got[0].Service != "AMAZON" || got[0].NetworkBorderGroup != "g" || got[0].SourceTimestamp != "2026-01-01" {
					t.Errorf("%s: metadata not carried: %+v", tc.desc, got[0])
				}
			}
		})
	}
}

func TestPrefixesFeed(t *testing.T) {
	p, _ := Get("gcp_ipranges")
	tests := []struct {
		name    string
		desc    string
		class   string
		in      string
		want    []string
		wantErr bool
	}{
		{"positive", "positive: mixed ipv4Prefix/ipv6Prefix entries", "positive",
			`{"creationTime":"2026","prefixes":[{"ipv4Prefix":"34.0.0.0/8","service":"Google Cloud","scope":"us"},{"ipv6Prefix":"2600::/16"}]}`,
			[]string{"34.0.0.0/8", "2600::/16"}, false},
		{"negative_badjson", "negative: malformed JSON errors", "negative", `{`, nil, true},
		{"boundary_empty", "boundary: empty prefixes array yields nothing", "boundary",
			`{"prefixes":[]}`, nil, false},
		{"corner_empty_entry", "corner: an entry with neither v4 nor v6 is skipped", "corner",
			`{"prefixes":[{"service":"x"},{"ipv4Prefix":"8.8.8.0/24"}]}`, []string{"8.8.8.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), testMeta, ts)
			checkPrefixes(t, tc.desc, got, err, tc.want, tc.wantErr)
		})
	}
}

func TestGithubMeta(t *testing.T) {
	p, _ := Get("github_meta")
	tests := []struct {
		name    string
		desc    string
		class   string
		in      string
		want    []string
		wantErr bool
	}{
		{"positive", "positive: service arrays flattened, key becomes service (sorted keys)", "positive",
			`{"api":["1.1.1.0/24"],"hooks":["2.2.2.0/24"]}`,
			[]string{"1.1.1.0/24", "2.2.2.0/24"}, false},
		{"negative_badjson", "negative: malformed JSON errors", "negative", `not json`, nil, true},
		{"boundary_empty", "boundary: empty object yields nothing", "boundary", `{}`, nil, false},
		{"corner_nonarray_values", "corner: non-array values (bool/object) are ignored", "corner",
			`{"verifiable_password_authentication":false,"ssh_key_fingerprints":{"a":"b"},"web":["3.3.3.0/24"]}`,
			[]string{"3.3.3.0/24"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := p.Parse([]byte(tc.in), testMeta, ts)
			checkPrefixes(t, tc.desc, got, err, tc.want, tc.wantErr)
			if tc.name == "positive" && err == nil {
				if got[0].Service != "api" {
					t.Errorf("%s: service = %q, want api", tc.desc, got[0].Service)
				}
			}
		})
	}
}

func TestBaseMetadataApplied(t *testing.T) {
	p, _ := Get("text_cidr")
	got, err := p.Parse([]byte("1.2.3.0/24\n"), testMeta, ts)
	if err != nil || len(got) != 1 {
		t.Fatalf("unexpected: %v %d", err, len(got))
	}
	r := got[0]
	if r.NetworkOwner != "owner" || r.Provider != "prov" || r.SourceURL != "https://example/feed" ||
		r.SourceType != "provider_feed" || r.Confidence != "authoritative" || r.RetrievedAt != ts {
		t.Errorf("base metadata not applied: %+v", r)
	}
}

// checkPrefixes asserts the error expectation and the ordered prefix list.
func checkPrefixes(t *testing.T, desc string, got []model.Record, err error, want []string, wantErr bool) {
	t.Helper()
	if wantErr {
		if err == nil {
			t.Fatalf("%s: expected error, got nil", desc)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", desc, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: got %d records, want %d", desc, len(got), len(want))
	}
	for i, w := range want {
		if got[i].Prefix != w {
			t.Errorf("%s: record[%d].Prefix = %q, want %q", desc, i, got[i].Prefix, w)
		}
	}
}
