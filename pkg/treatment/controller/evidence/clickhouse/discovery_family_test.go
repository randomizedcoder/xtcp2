package clickhouse

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
)

// familySocketRows inserts one socket's three polls to destination hex
// with the given family, as host agent.
func familySocketRows(agent, destinationHex string, family, cookie int) string {
	return fmt.Sprintf(`INSERT INTO default.sockets SELECT '%s','boot','',1,%d,
 fromUnixTimestamp64Nano(1700000000000000000+toInt64(number)*10000000000),
 1000000+number*1000,1000000+number*1000,number*10,1000,500,false,'','dc1',1,
 unhex('%s'),%d,64512 FROM numbers(3);
`, agent, cookie, destinationHex, family)
}

// Destination bytes as the telemetry stores them (FixedString(16)), and the
// sockets table the production queries read, for clickhouse local.
const (
	v4Hex      = "C0000201000000000000000000000000" // 192.0.2.1, AF_INET
	mappedHex  = "00000000000000000000FFFFC0000201" // ::ffff:192.0.2.1, AF_INET6
	realV6Hex  = "20010DB80000000000000000C0000201" // 2001:db8::c000:201
	mapped0Hex = "00000000000000000000FFFF00000000" // ::ffff:0.0.0.0
	socketsSQL = `CREATE TABLE default.sockets (
 agent_id String,host_boot_id String,collection_id String,netns_inode UInt64,inet_diag_msg_socket_cookie UInt64,
 timestamp_ns DateTime64(9),tcp_info_bytes_acked UInt64,tcp_info_bytes_sent UInt64,
 tcp_info_bytes_retrans UInt64,tcp_info_rtt UInt32,tcp_info_min_rtt UInt32,
 treatment_algorithm_exposed Bool,treatment_assignment_id String,location String,
 inet_diag_msg_state UInt8,inet_diag_msg_socket_destination FixedString(16),
 inet_diag_msg_family UInt8,enrich_socket_dest_asn UInt64) ENGINE=Memory;
`
)

// runClickHouse runs sql in an isolated clickhouse local with the window,
// location and threshold parameters both production queries take, plus
// extra parameters.
func runClickHouse(t *testing.T, binary, sql string, extra ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "query.sql")
	if err := os.WriteFile(file, []byte(sql), 0o600); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"local", "--path", filepath.Join(dir, "data"), "--queries-file", file, "--multiquery",
		"--param_start=1700000000000000000", "--param_end=1700000060000000000", "--param_location=dc1",
		"--param_minimum_rtt=0", "--param_maximum_rtt=10", "--param_minimum_retransmission=0",
		"--output_format_json_quote_64bit_integers=0"}, extra...)
	out, err := exec.Command(binary, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ClickHouse SQL: %v\n%s", err, out)
	}
	return out
}

// TestDiscoveryV4Mapped runs discovery.sql in clickhouse local and pins
// that an IPv4 peer seen through dual-stack sockets (AF_INET6, v4-mapped
// destination) is the same candidate as through AF_INET sockets: one
// destination, its agents pooled, reported as the IPv4 address. Before,
// one_peer_one_candidate, mapped_zero and all_mapped failed: the mapped
// form grouped apart and decoded as an IPv6 /128.
func TestDiscoveryV4Mapped(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse is not on PATH; the Nix quality check supplies it")
	}
	type host struct {
		destinationHex string
		family         int
	}
	six := func(hs ...host) []host { return hs }
	tests := []struct {
		name        string
		category    string
		description string
		hosts       []host // one socket per host, agents h1..hN
		expected    []string
	}{
		{
			name: "one_peer_one_candidate", category: "positive",
			description: "three hosts reach 192.0.2.1 over AF_INET and three over dual-stack sockets: one candidate with six agents",
			hosts:       six(host{v4Hex, 2}, host{v4Hex, 2}, host{v4Hex, 2}, host{mappedHex, 10}, host{mappedHex, 10}, host{mappedHex, 10}),
			expected:    []string{"192.0.2.1/32 agents=6"},
		},
		{
			name: "real_v6_separate", category: "negative",
			description: "a real IPv6 peer with the same low bytes is a different destination: two groups of three, no candidate",
			hosts:       six(host{v4Hex, 2}, host{v4Hex, 2}, host{v4Hex, 2}, host{realV6Hex, 10}, host{realV6Hex, 10}, host{realV6Hex, 10}),
			expected:    []string{},
		},
		{
			name: "mapped_zero", category: "boundary",
			description: "::ffff:0.0.0.0 is reported as 0.0.0.0",
			hosts:       six(host{mapped0Hex, 10}, host{mapped0Hex, 10}, host{mapped0Hex, 10}, host{mapped0Hex, 10}, host{mapped0Hex, 10}, host{mapped0Hex, 10}),
			expected:    []string{"0.0.0.0/32 agents=6"},
		},
		{
			name: "all_mapped", category: "corner",
			description: "only dual-stack sockets reach the peer: still reported as the IPv4 /32, not ::ffff:192.0.2.1/128",
			hosts:       six(host{mappedHex, 10}, host{mappedHex, 10}, host{mappedHex, 10}, host{mappedHex, 10}, host{mappedHex, 10}, host{mappedHex, 10}),
			expected:    []string{"192.0.2.1/32 agents=6"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sql strings.Builder
			sql.WriteString(socketsSQL)
			for i, h := range tc.hosts {
				sql.WriteString(familySocketRows(fmt.Sprintf("h%d", i+1), h.destinationHex, h.family, i+1))
			}
			sql.WriteString(strings.ReplaceAll(discoverySQL, "{{TABLE}}", "default.sockets"))
			out := runClickHouse(t, binary, sql.String())
			got := []string{}
			for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if line == "" {
					continue
				}
				var row candidateRow
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatalf("row %q: %v", line, err)
				}
				c, err := decodeCandidate(row)
				if err != nil {
					t.Fatalf("row %q: %v", line, err)
				}
				got = append(got, fmt.Sprintf("%s agents=%d", c.Destination, len(c.Agents)))
			}
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s (%s): %s\n got      %v\n expected %v", tc.name, tc.category, tc.description, got, tc.expected)
			}
		})
	}
}

// boundSocketRows inserts one host-1 socket of the reported session, six
// polls ten seconds apart, to destination hex with the given family.
func boundSocketRows(destinationHex string, family, cookie int) string {
	return fmt.Sprintf(`INSERT INTO default.sockets SELECT 'host-1','boot-1','00000000000000000000000000000001',1,%d,
 fromUnixTimestamp64Nano(1700000000000000000+toInt64(number)*10000000000),
 1000000+number*1000,1000000+number*1000,number*10,1000,500,true,'trial','dc1',1,
 unhex('%s'),%d,64512 FROM numbers(6);
`, cookie, destinationHex, family)
}

// TestAnalysisV4Mapped runs analysis.sql in clickhouse local and pins that
// a candidate's IPv4 destination also measures the dual-stack sockets that
// reach it (AF_INET6, destination ::ffff:a.b.c.d): discovery reports such
// a peer as the IPv4 address, so analysis must count both forms. Before,
// v4_and_mapped and mapped_only failed: the mapped sockets were not counted.
func TestAnalysisV4Mapped(t *testing.T) {
	binary, err := exec.LookPath("clickhouse")
	if err != nil {
		t.Skip("clickhouse is not on PATH; the Nix quality check supplies it")
	}
	type socket struct {
		destinationHex string
		family         int
	}
	tests := []struct {
		name        string
		category    string
		description string
		sockets     []socket // all of host-1's reported session
		expected    uint64   // measured sockets for candidate 192.0.2.1
	}{
		{
			name: "v4_and_mapped", category: "positive",
			description: "an AF_INET and a dual-stack socket to 192.0.2.1 are both measured",
			sockets:     []socket{{v4Hex, 2}, {mappedHex, 10}},
			expected:    2,
		},
		{
			name: "real_v6_excluded", category: "negative",
			description: "a real IPv6 peer with the same low bytes is not the candidate",
			sockets:     []socket{{v4Hex, 2}, {realV6Hex, 10}},
			expected:    1,
		},
		{
			name: "mapped_only", category: "boundary",
			description: "only dual-stack sockets reach the candidate: all are measured",
			sockets:     []socket{{mappedHex, 10}},
			expected:    1,
		},
		{
			name: "mapped_bytes_on_inet", category: "corner",
			description: "an AF_INET row whose bytes happen to look v4-mapped is read as AF_INET (0.0.0.0), not the candidate",
			sockets:     []socket{{v4Hex, 2}, {mappedHex, 2}},
			expected:    1,
		},
	}
	session := sessionsParam([]treatment.Session{{AgentID: "host-1", BootID: "boot-1", CollectionID: "00000000000000000000000000000001"}})
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sql strings.Builder
			sql.WriteString(socketsSQL)
			for i, s := range tc.sockets {
				sql.WriteString(boundSocketRows(s.destinationHex, s.family, i+1))
			}
			sql.WriteString(strings.ReplaceAll(analysisSQL, "{{TABLE}}", "default.sockets"))
			out := runClickHouse(t, binary, sql.String(), "--param_agents=['host-1']", "--param_sessions="+session,
				"--param_destination_hex="+v4Hex, "--param_assignment=trial")
			var h hostRow
			if err := json.Unmarshal(out, &h); err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, out)
			}
			if h.Sockets != tc.expected {
				t.Errorf("%s (%s): %s\n got %d measured sockets, expected %d", tc.name, tc.category, tc.description, h.Sockets, tc.expected)
			}
		})
	}
}
