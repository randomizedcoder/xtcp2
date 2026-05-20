package main

import (
	"strings"
	"sync"
	"testing"
)

// helpers_test.go covers the two helpers extracted from
// collectProtoFields in the gocyclo-15 → 5 refactor
// (updateProtoMessageDepth + extractFieldsFromProto) with the standard
// five-category matrix, plus race + benchmarks.
// Pre-existing main_test.go drives the orchestrator end-to-end.

// ───────────────────────────────────────────────────────────────────────
// updateProtoMessageDepth
// ───────────────────────────────────────────────────────────────────────

func TestUpdateProtoMessageDepth_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		line      string
		inDepth   int
		wantDepth int
		wantSkip  bool
	}{
		{"positive_message_decl_increments", "positive", "message Foo {", 0, 1, true},
		{"positive_message_one_level_nested", "positive", "message Inner {", 1, 2, true},
		{"positive_close_brace_decrements", "positive", "}", 1, 0, true},
		{"positive_open_brace_only_increments", "positive", "{", 1, 2, false},
		{"positive_field_line_passes_through", "positive", "string x = 1;", 1, 1, false},
		{"negative_brace_at_depth_zero_ignored", "negative", "{", 0, 0, false},
		{"negative_close_brace_at_depth_zero", "negative", "}", 0, 0, false},
		{"negative_random_line_at_depth_zero", "negative", "comment text", 0, 0, false},
		{"boundary_message_at_max_int", "boundary", "message X {", 1 << 30, 1<<30 + 1, true},
		{"boundary_close_brings_to_zero", "boundary", "}", 1, 0, true},
		{"corner_message_prefix_but_not_keyword", "corner", "messageButNotKeyword", 0, 0, false},
		{"corner_brace_inside_string_literal", "corner", `string s = "has { and }";`, 1, 1, true}, // doc: heuristic misclassifies — pinned
		{"corner_both_braces_same_line", "corner", "} message { ", 1, 1, true},                    // open fires (depth+1), then close (depth-1) → net unchanged + skip
		{"adversarial_message_followed_by_extra_brace", "adversarial", "message Foo { extra {", 0, 1, true},
		{"adversarial_only_whitespace", "adversarial", "   ", 1, 1, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			gotDepth, gotSkip := updateProtoMessageDepth(tc.line, tc.inDepth)
			if gotDepth != tc.wantDepth {
				t.Errorf("depth = %d, want %d", gotDepth, tc.wantDepth)
			}
			if gotSkip != tc.wantSkip {
				t.Errorf("skip = %v, want %v", gotSkip, tc.wantSkip)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// extractFieldsFromProto
// ───────────────────────────────────────────────────────────────────────

func TestExtractFieldsFromProto_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		src       string
		wantNames []string
	}{
		{
			name:     "positive_single_message",
			category: "positive",
			src: `syntax = "proto3";
message Foo {
  string a = 1;
  int32  b = 2;
}`,
			wantNames: []string{"a", "b"},
		},
		{
			name:     "positive_repeated_and_optional",
			category: "positive",
			src: `message M {
  repeated string a = 1;
  optional int32 b = 2;
  required bytes c = 3;
}`,
			wantNames: []string{"a", "b", "c"},
		},
		{
			name:     "positive_multiple_messages",
			category: "positive",
			src: `message A {
  string a1 = 1;
}
message B {
  string b1 = 1;
}`,
			wantNames: []string{"a1", "b1"},
		},
		{
			name:     "negative_single_line_message_fields_not_captured",
			category: "negative",
			// Document a known limitation: when a message decl, fields,
			// and closing brace are all on one line, the line is consumed
			// by the "HasPrefix message " skip, so no fields are
			// extracted. Real .proto files are multi-line; this pin
			// just records the existing behavior.
			src:       `message A { string a1 = 1; }`,
			wantNames: nil,
		},
		{
			name:     "negative_field_outside_message_ignored",
			category: "negative",
			src: `string outside = 1;
message Foo {
  string inside = 2;
}`,
			wantNames: []string{"inside"},
		},
		{
			name:     "negative_comments_skipped",
			category: "negative",
			src: `message M {
  // string a = 1;
  string b = 2;
}`,
			wantNames: []string{"b"},
		},
		{
			name:      "negative_empty_file",
			category:  "negative",
			src:       "",
			wantNames: nil,
		},
		{
			name:     "boundary_only_message_no_fields",
			category: "boundary",
			src: `message Empty {
}`,
			wantNames: nil,
		},
		{
			name:     "boundary_message_decl_with_brace_on_next_line",
			category: "boundary",
			src: `message Foo
{
  string a = 1;
}`,
			// "message Foo" → depth 1, skip. "{" with depth>0 → depth 2,
			// no skip. "  string a = 1;" with depth 2 (>0) → regex
			// matches, append. "}" → depth 1, skip. EOF.
			// We still capture the field. Pin this boundary.
			wantNames: []string{"a"},
		},
		{
			name:     "corner_nested_message",
			category: "corner",
			src: `message Outer {
  string a = 1;
  message Inner {
    string b = 1;
  }
  string c = 2;
}`,
			wantNames: []string{"a", "b", "c"},
		},
		{
			name:     "corner_field_with_dotted_type",
			category: "corner",
			src: `message M {
  google.protobuf.Timestamp t = 1;
}`,
			wantNames: []string{"t"},
		},
		{
			name:     "corner_map_type_field_with_space_not_matched",
			category: "corner",
			// Documented limitation: the regex's `[\w.<>,]+` set does
			// not include space, so `map<string, int32>` (with space
			// after the comma) doesn't match. `map<string,int32>` would.
			// Pin the current behaviour so a future regex tweak surfaces
			// this case.
			src: `message M {
  map<string, int32> m = 1;
}`,
			wantNames: nil,
		},
		{
			name:     "corner_map_type_field_no_space_matched",
			category: "corner",
			src: `message M {
  map<string,int32> m = 1;
}`,
			wantNames: []string{"m"},
		},
		{
			name:      "adversarial_huge_file_many_fields",
			category:  "adversarial",
			src:       bigProtoMessage(500),
			wantNames: bigProtoFieldNames(500),
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractFieldsFromProto("test.proto", []byte(tc.src))
			if len(got) != len(tc.wantNames) {
				t.Fatalf("got %d fields (%v), want %d (%v)", len(got), fieldNames(got), len(tc.wantNames), tc.wantNames)
			}
			for i := range got {
				if got[i].name != tc.wantNames[i] {
					t.Errorf("got[%d].name = %q, want %q", i, got[i].name, tc.wantNames[i])
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race
// ───────────────────────────────────────────────────────────────────────

func TestProtoFieldHelpers_concurrent(t *testing.T) {
	src := `message M {
  string a = 1;
  int32  b = 2;
  message Inner { string c = 1; }
  string d = 3;
}`
	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_, _ = updateProtoMessageDepth("message X {", 0)
				_, _ = updateProtoMessageDepth("}", 1)
				_ = extractFieldsFromProto("t.proto", []byte(src))
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkUpdateProtoMessageDepth_field(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = updateProtoMessageDepth("string x = 1;", 1)
	}
}

func BenchmarkExtractFieldsFromProto_small(b *testing.B) {
	src := []byte(`message M {
  string a = 1;
  int32  b = 2;
  string c = 3;
}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractFieldsFromProto("bench.proto", src)
	}
}

func BenchmarkExtractFieldsFromProto_big(b *testing.B) {
	src := []byte(bigProtoMessage(100))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractFieldsFromProto("bench.proto", src)
	}
}

// helpers ----------------------------------------------------------------

func fieldNames(fs []field) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.name
	}
	return out
}

func bigProtoMessage(n int) string {
	var b strings.Builder
	b.WriteString("message Big {\n")
	for i := 0; i < n; i++ {
		b.WriteString("  string f")
		b.WriteString(intToStr(i))
		b.WriteString(" = ")
		b.WriteString(intToStr(i + 1))
		b.WriteString(";\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func bigProtoFieldNames(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = "f" + intToStr(i)
	}
	return out
}

// intToStr is a tiny strconv shim so this test file stays imports-tight.
func intToStr(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
