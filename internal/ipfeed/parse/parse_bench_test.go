package parse

import (
	"fmt"
	"strings"
	"testing"
)

// benchMeta is a minimal source meta shared by the parser benchmarks.
var benchMeta = SourceMeta{
	Name:       "bench",
	Provider:   "bench",
	URL:        "https://example/feed",
	SourceType: "provider_feed",
	Confidence: "authoritative",
}

const benchTS = "2026-01-01T00:00:00Z"

// genAWSJSON builds an AWS ip-ranges.json body with n IPv4 prefix entries.
func genAWSJSON(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"createDate":"2026-01-01-00-00-00","prefixes":[`)
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"ip_prefix":"10.%d.%d.0/24","region":"us-east-1","service":"EC2","network_border_group":"us-east-1"}`,
			(i>>8)&0xff, i&0xff)
	}
	b.WriteString(`],"ipv6_prefixes":[]}`)
	return []byte(b.String())
}

// genTextCIDR builds n newline-delimited /24 lines.
func genTextCIDR(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "10.%d.%d.0/24\n", (i>>8)&0xff, i&0xff)
	}
	return []byte(b.String())
}

// genCSV builds n rows of "prefix,region".
func genCSV(n int) []byte {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "10.%d.%d.0/24,us-east-1\n", (i>>8)&0xff, i&0xff)
	}
	return []byte(b.String())
}

// BenchmarkParse measures the decode hot path for the three representative
// feed shapes (JSON, text-CIDR, CSV) across dataset sizes.
func BenchmarkParse(b *testing.B) {
	csvMeta := benchMeta
	csvMeta.Opts = map[string]any{"has_header": false, "prefix_column": 0, "region_column": 1}

	cases := []struct {
		parser string
		meta   SourceMeta
		gen    func(int) []byte
	}{
		{"aws_ip_ranges", benchMeta, genAWSJSON},
		{"text_cidr", benchMeta, genTextCIDR},
		{"csv", csvMeta, genCSV},
	}

	for i := range cases {
		c := &cases[i]
		p, ok := Get(c.parser)
		if !ok {
			b.Fatalf("parser %q not registered", c.parser)
		}
		for _, size := range []int{100, 10_000} {
			data := c.gen(size)
			b.Run(fmt.Sprintf("%s/n=%d", c.parser, size), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(data)))
				b.ResetTimer()
				for range b.N {
					recs, err := p.Parse(data, c.meta, benchTS)
					if err != nil {
						b.Fatalf("Parse: %v", err)
					}
					if len(recs) != size {
						b.Fatalf("got %d records, want %d", len(recs), size)
					}
				}
			})
		}
	}
}
