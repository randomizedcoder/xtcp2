package parse

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func init() { Register("csv", csvParser{}) }

// csvParser handles column-oriented CSV feeds (DigitalOcean google.csv, Apple
// egress-ip-ranges.csv, the AWS geo-ip-feed.csv). Column positions are given
// in parser_opts:
//
//	prefix_column: 0        # 0-based column holding the CIDR (required, default 0)
//	region_column: -1       # optional region column; -1 disables
//	service_column: -1      # optional service column; -1 disables
//	has_header: false       # skip the first row if true
//	comment: ""             # optional single-char comment prefix
//
// Rows with too few columns for prefix_column are skipped (they produce no
// record and are simply not counted as valid).
type csvParser struct{}

func (csvParser) Parse(data []byte, meta SourceMeta, retrievedAt string) ([]model.Record, error) {
	o := opts(meta.Opts)
	prefixCol := o.int("prefix_column", 0)
	regionCol := o.int("region_column", -1)
	serviceCol := o.int("service_column", -1)
	hasHeader := o.boolean("has_header", false)

	data = bytes.TrimPrefix(data, utf8BOM)
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1 // tolerate ragged rows
	r.TrimLeadingSpace = true
	if c := o.str("comment", ""); c != "" {
		r.Comment = rune(c[0])
	}

	var out []model.Record
	first := true
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("csv: %w", err)
		}
		if first && hasHeader {
			first = false
			continue
		}
		first = false
		if prefixCol < 0 || prefixCol >= len(row) {
			continue
		}
		rec := meta.Base(retrievedAt)
		rec.Prefix = row[prefixCol]
		if regionCol >= 0 && regionCol < len(row) {
			rec.Region = row[regionCol]
		}
		if serviceCol >= 0 && serviceCol < len(row) {
			rec.Service = row[serviceCol]
		}
		out = append(out, rec)
	}
	return out, nil
}
