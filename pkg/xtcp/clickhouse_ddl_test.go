package xtcp

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// The ClickHouse DDL is hand-written beside the proto, and ClickHouse maps a
// record's fields to columns by name, dropping any field without a column.
// Nothing else ties the two together (the AccECN columns went missing that
// way), so this test does: the Kafka engine table and the current epoch's
// MergeTree table must have a column for every proto field. The older
// epochs' tables keep their own names and only the fields those daemons
// wrote, so they are not checked.

const clickhouseSQLDir = "../../build/containers/clickhouse/initdb.d/sql"

// ddlBlock returns the body of the CREATE TABLE that starts with head, up
// to its closing ")" line. Comments may hold ";", so that is no terminator.
func ddlBlock(sql, head string) (string, error) {
	i := strings.Index(sql, head)
	if i < 0 {
		return "", fmt.Errorf("no statement %q", head)
	}
	rest := sql[i+len(head):]
	if j := strings.Index(rest, "\n)"); j >= 0 {
		rest = rest[:j]
	}
	return rest, nil
}

// ddlColumnRE matches a column definition line: an indented identifier,
// then a ClickHouse type (which starts with a capital letter).
var ddlColumnRE = regexp.MustCompile(`^\s*([a-z][a-z0-9_]*)\s+[A-Z]`)

// ddlColumns lists the columns of a CREATE TABLE body, skipping comments.
func ddlColumns(body string) []string {
	var cols []string
	for line := range strings.SplitSeq(body, "\n") {
		if k := strings.Index(line, "--"); k >= 0 {
			line = line[:k]
		}
		if m := ddlColumnRE.FindStringSubmatch(line); m != nil {
			cols = append(cols, m[1])
		}
	}
	return cols
}

// missingColumns names each wanted field cols lacks, as "table t: missing
// column c", in want's order.
func missingColumns(table string, cols, want []string) []string {
	var out []string
	for _, w := range want {
		if !slices.Contains(cols, w) {
			out = append(out, fmt.Sprintf("table %s: missing column %s", table, w))
		}
	}
	return out
}

// protoFieldNames is every XtcpFlatRecord field name, in declaration order.
func protoFieldNames() []string {
	fields := (&xtcp_flat_record.XtcpFlatRecord{}).ProtoReflect().Descriptor().Fields()
	out := make([]string, fields.Len())
	for i := range fields.Len() {
		out[i] = string(fields.Get(i).Name())
	}
	return out
}

func readClickhouseSQL(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(clickhouseSQLDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ddlInput is one parity check: a DDL source (inline, or a file under
// clickhouseSQLDir) and the CREATE statement in it.
type ddlInput struct {
	file  string
	sql   string
	table string
	head  string
}

// go test ./pkg/xtcp/ -run TestClickHouseDDLMatchesProto
func TestClickHouseDDLMatchesProto(t *testing.T) {
	fields := protoFieldNames()
	allMissing := missingColumns("t", nil, fields)
	tests := []struct {
		name        string
		category    string
		description string
		input       ddlInput
		expected    []string // the missing-column messages; nil = none
	}{
		{"proto_fields_in_kafka", "positive", "every proto field has a column in the Kafka engine table",
			ddlInput{file: "xtcp_xtcp_flat_records_kafka.sql", table: "xtcp_flat_records_kafka",
				head: "CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_kafka"}, nil},
		{"proto_fields_in_v2", "positive", "every proto field has a column in the current epoch's table",
			ddlInput{file: "xtcp_xtcp_flat_records.sql", table: "xtcp_flat_records_v2",
				head: "CREATE TABLE IF NOT EXISTS xtcp.xtcp_flat_records_v2\n"}, nil},
		{"missing_column", "negative", "a DDL without a field is reported",
			ddlInput{sql: "CREATE TABLE t\n(\n    netns_inode UInt64,\n)\n", table: "t", head: "CREATE TABLE t"},
			slices.DeleteFunc(slices.Clone(allMissing), func(s string) bool { return s == "table t: missing column netns_inode" })},
		{"empty_ddl", "boundary", "a CREATE with no columns",
			ddlInput{sql: "CREATE TABLE t ()", table: "t", head: "CREATE TABLE t"}, allMissing},
		{"codec_and_comments", "corner", "CODEC clauses, -- comments and trailing commas don't confuse the parser",
			ddlInput{sql: "CREATE TABLE t\n(\n    nsid UInt32 CODEC(LZ4), -- x\n    -- netns String,\n)\n", table: "t", head: "CREATE TABLE t"},
			slices.DeleteFunc(slices.Clone(allMissing), func(s string) bool { return s == "table t: missing column nsid" })},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql := tc.input.sql
			if tc.input.file != "" {
				sql = readClickhouseSQL(t, tc.input.file)
			}
			body, err := ddlBlock(sql, tc.input.head)
			if err != nil {
				t.Fatal(err)
			}
			got := missingColumns(tc.input.table, ddlColumns(body), fields)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s (%s): %s\ninput %s %q\nexpected %q\nactual   %q",
					tc.name, tc.category, tc.description, tc.input.file, tc.input.head, tc.expected, got)
			}
		})
	}
}
