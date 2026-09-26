package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// kernel_annotation_test.go covers the kernel-source annotation guard: field
// number + trailing-comment extraction, the needs/has predicates, the
// reporting helper, and runAudit end-to-end. Table-driven across the
// positive / negative / boundary / corner matrix.

// ───────────────────────────────────────────────────────────────────────
// extractFieldsFromProto: number + trailing comment
// ───────────────────────────────────────────────────────────────────────

func TestExtractFieldsFromProto_numberAndComment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		description string
		line        string // one field line, wrapped in a message
		wantNumber  int
		wantComment string
	}{
		{
			name:        "positive_struct_member_comment",
			category:    "positive",
			description: "tab-indented payload field with a struct-member comment keeps number and comment text",
			line:        "\tuint32 tcp_info_rttvar = 1231; // struct tcp_info.tcpi_rttvar (__u32) RTT variance",
			wantNumber:  1231,
			wantComment: "struct tcp_info.tcpi_rttvar (__u32) RTT variance",
		},
		{
			name:        "positive_enum_typed_field",
			category:    "positive",
			description: "custom enum type before the name parses like a scalar",
			line:        "  CongestionAlgorithm inet_diag_cong_enum = 1301; // derived by xtcp from inet_diag_cong",
			wantNumber:  1301,
			wantComment: "derived by xtcp from inet_diag_cong",
		},
		{
			name:        "negative_no_comment",
			category:    "negative",
			description: "a field without a trailing comment yields an empty comment",
			line:        "  uint32 hostname_len = 7;",
			wantNumber:  7,
			wantComment: "",
		},
		{
			name:        "boundary_number_zero_like_small",
			category:    "boundary",
			description: "single-byte tag 1 parses",
			line:        "  uint32 schema_version = 1; // routing epoch",
			wantNumber:  1,
			wantComment: "routing epoch",
		},
		{
			name:        "boundary_max_proto_tag",
			category:    "boundary",
			description: "the protobuf maximum tag 536870911 parses without overflow",
			line:        "  uint32 huge = 536870911; // struct x.y (__u32)",
			wantNumber:  536870911,
			wantComment: "struct x.y (__u32)",
		},
		{
			name:        "corner_comment_with_extra_slashes",
			category:    "corner",
			description: "only the first // starts the comment; later slashes are content",
			line:        "  uint32 a = 1200; // INET_DIAG_TOS (5): inet->tos // see net/ipv4/inet_diag.c",
			wantNumber:  1200,
			wantComment: "INET_DIAG_TOS (5): inet->tos // see net/ipv4/inet_diag.c",
		},
		{
			name:        "corner_option_brackets_before_comment",
			category:    "corner",
			description: "buf.validate option brackets between ; and the comment do not break extraction",
			line:        "  uint32 b = 1500 [(buf.validate.field).uint32.lte = 10]; // SK_MEMINFO_RMEM_ALLOC (__u32)",
			wantNumber:  1500,
			wantComment: "SK_MEMINFO_RMEM_ALLOC (__u32)",
		},
		{
			name:        "corner_comment_without_space_after_slashes",
			category:    "corner",
			description: "//comment glued to the slashes is still trimmed to the text",
			line:        "  uint32 c = 1600; //INET_DIAG_SHUTDOWN (8): sk->sk_shutdown",
			wantNumber:  1600,
			wantComment: "INET_DIAG_SHUTDOWN (8): sk->sk_shutdown",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			src := "message M {\n" + tc.line + "\n}\n"
			got := extractFieldsFromProto("t.proto", []byte(src))
			if len(got) != 1 {
				t.Fatalf("%s: got %d fields, want 1", tc.description, len(got))
			}
			if got[0].number != tc.wantNumber {
				t.Errorf("%s: number = %d, want %d", tc.description, got[0].number, tc.wantNumber)
			}
			if got[0].comment != tc.wantComment {
				t.Errorf("%s: comment = %q, want %q", tc.description, got[0].comment, tc.wantComment)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// field.needsKernelAnnotation / field.hasKernelAnnotation
// ───────────────────────────────────────────────────────────────────────

func TestFieldKernelAnnotation_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		category    string
		description string
		f           field
		wantNeeds   bool
		wantHas     bool
		wantFinding bool // needs && !has
	}{
		{
			name:        "positive_struct_member",
			category:    "positive",
			description: "struct member form is accepted",
			f:           field{name: "tcp_info_rttvar", number: 1231, comment: "struct tcp_info.tcpi_rttvar (__u32)"},
			wantNeeds:   true, wantHas: true, wantFinding: false,
		},
		{
			name:        "positive_nested_struct_member",
			category:    "positive",
			description: "nested member path (id.idiag_sport) is accepted via the struct prefix",
			f:           field{name: "inet_diag_msg_socket_source_port", number: 1005, comment: "struct inet_diag_msg.id.idiag_sport (__be16)"},
			wantNeeds:   true, wantHas: true, wantFinding: false,
		},
		{
			name:        "positive_inet_diag_attribute",
			category:    "positive",
			description: "struct-less INET_DIAG_* attribute with its id is accepted",
			f:           field{name: "inet_diag_tos", number: 1401, comment: "INET_DIAG_TOS (5): inet->tos (__u8, inet_diag.c)"},
			wantNeeds:   true, wantHas: true, wantFinding: false,
		},
		{
			name:        "positive_sk_meminfo_slot",
			category:    "positive",
			description: "SK_MEMINFO_* array slot is accepted",
			f:           field{name: "sk_mem_info_rcvbuf", number: 1502, comment: "SK_MEMINFO_RCVBUF (__u32, sock_diag.h)"},
			wantNeeds:   true, wantHas: true, wantFinding: false,
		},
		{
			name:        "positive_derived_by_xtcp",
			category:    "positive",
			description: "daemon-derived fields declare themselves as such",
			f:           field{name: "inet_diag_cong_enum", number: 1301, comment: "derived by xtcp from inet_diag_cong (not a kernel field)"},
			wantNeeds:   true, wantHas: true, wantFinding: false,
		},
		{
			name:        "negative_payload_without_comment",
			category:    "negative",
			description: "a payload-range field with no comment is a finding",
			f:           field{name: "tcp_info_new_thing", number: 1277, comment: ""},
			wantNeeds:   true, wantHas: false, wantFinding: true,
		},
		{
			name:        "negative_payload_with_prose_comment",
			category:    "negative",
			description: "a free-text comment that names no kernel source is a finding",
			f:           field{name: "tcp_info_new_thing", number: 1277, comment: "how many bytes were retransmitted"},
			wantNeeds:   true, wantHas: false, wantFinding: true,
		},
		{
			name:        "negative_inet_diag_without_id",
			category:    "negative",
			description: "INET_DIAG_* without the (n) attribute id does not satisfy the convention",
			f:           field{name: "inet_diag_tos", number: 1401, comment: "INET_DIAG_TOS byte"},
			wantNeeds:   true, wantHas: false, wantFinding: true,
		},
		{
			name:        "boundary_tag_999_exempt",
			category:    "boundary",
			description: "the last spare tag below the payload range needs no annotation",
			f:           field{name: "spare", number: 999, comment: ""},
			wantNeeds:   false, wantHas: false, wantFinding: false,
		},
		{
			name:        "boundary_tag_1000_required",
			category:    "boundary",
			description: "the first payload tag requires an annotation",
			f:           field{name: "inet_diag_msg_family", number: 1000, comment: ""},
			wantNeeds:   true, wantHas: false, wantFinding: true,
		},
		{
			name:        "corner_metadata_with_kernel_looking_comment",
			category:    "corner",
			description: "a metadata field may mention a struct without being required to",
			f:           field{name: "socket_fd", number: 62, comment: "struct netlinker.fd"},
			wantNeeds:   false, wantHas: true, wantFinding: false,
		},
		{
			name:        "corner_enrichment_range_exempt",
			category:    "corner",
			description: "daemon-computed 300s enrichment fields are outside the payload range",
			f:           field{name: "enrich_socket_dest_locality", number: 310, comment: ""},
			wantNeeds:   false, wantHas: false, wantFinding: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.f.needsKernelAnnotation(); got != tc.wantNeeds {
				t.Errorf("%s: needsKernelAnnotation = %v, want %v", tc.description, got, tc.wantNeeds)
			}
			if got := tc.f.hasKernelAnnotation(); got != tc.wantHas {
				t.Errorf("%s: hasKernelAnnotation = %v, want %v", tc.description, got, tc.wantHas)
			}
			var out bytes.Buffer
			n := reportUnannotatedPayloadFields([]field{tc.f}, &out)
			if (n == 1) != tc.wantFinding {
				t.Errorf("%s: reportUnannotatedPayloadFields = %d finding(s), wantFinding=%v; out=%q",
					tc.description, n, tc.wantFinding, out.String())
			}
			if tc.wantFinding && !strings.Contains(out.String(), tc.f.name) {
				t.Errorf("%s: finding does not name the field; out=%q", tc.description, out.String())
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// runAudit end-to-end
// ───────────────────────────────────────────────────────────────────────

func TestRunAudit_kernelAnnotation(t *testing.T) {
	t.Parallel()
	const goSrc = `
package x
type T struct { TcpInfoRttvar uint32; Hostname string }
func use(t T) { _ = t.TcpInfoRttvar; _ = t.Hostname }
`
	cases := []struct {
		name        string
		category    string
		description string
		proto       string
		wantRC      int
		wantStdout  string // substring that must appear; "" = don't check
	}{
		{
			name:        "positive_annotated_payload_is_clean",
			category:    "positive",
			description: "a written, annotated payload field and a metadata field produce no findings",
			proto: `syntax = "proto3";
message Foo {
  string hostname = 20;
  uint32 tcp_info_rttvar = 1231; // struct tcp_info.tcpi_rttvar (__u32)
}
`,
			wantRC:     0,
			wantStdout: "no findings",
		},
		{
			name:        "negative_unannotated_payload_fails",
			category:    "negative",
			description: "a written payload field without a kernel comment is reported and fails the audit",
			proto: `syntax = "proto3";
message Foo {
  string hostname = 20;
  uint32 tcp_info_rttvar = 1231;
}
`,
			wantRC:     1,
			wantStdout: "lacks a kernel-source trailing comment",
		},
		{
			name:        "boundary_metadata_never_needs_comment",
			category:    "boundary",
			description: "tag 999 without a comment is not a finding",
			proto: `syntax = "proto3";
message Foo {
  string hostname = 999;
  uint32 tcp_info_rttvar = 1231; // struct tcp_info.tcpi_rttvar (__u32)
}
`,
			wantRC:     0,
			wantStdout: "no findings",
		},
		{
			name:        "corner_both_findings_counted",
			category:    "corner",
			description: "an unset field and an unannotated field are both reported in one run",
			proto: `syntax = "proto3";
message Foo {
  string hostname = 20;
  string never_written = 21;
  uint32 tcp_info_rttvar = 1231;
}
`,
			wantRC:     1,
			wantStdout: "never_written",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			protoDir := filepath.Join(dir, "proto")
			goDir := filepath.Join(dir, "go")
			for _, d := range []string{protoDir, goDir} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			writeFile(t, protoDir, "x.proto", tc.proto)
			writeFile(t, goDir, "x.go", goSrc)
			var stdout, stderr bytes.Buffer
			rc := runAudit(protoDir, goDir, &stdout, &stderr)
			if rc != tc.wantRC {
				t.Errorf("%s: rc = %d, want %d; stdout=%q stderr=%q", tc.description, rc, tc.wantRC, stdout.String(), stderr.String())
			}
			if tc.wantStdout != "" && !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("%s: stdout missing %q; got %q", tc.description, tc.wantStdout, stdout.String())
			}
		})
	}
}
