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
	const (
		v4       = "C0000201000000000000000000000000"
		mapped   = "00000000000000000000FFFFC0000201"
		realV6   = "20010DB80000000000000000C0000201"
		mapped0  = "00000000000000000000FFFF00000000"
		setupSQL = `CREATE TABLE default.sockets (
 agent_id String,host_boot_id String,collection_id String,netns_inode UInt64,inet_diag_msg_socket_cookie UInt64,
 timestamp_ns DateTime64(9),tcp_info_bytes_acked UInt64,tcp_info_bytes_sent UInt64,
 tcp_info_bytes_retrans UInt64,tcp_info_rtt UInt32,tcp_info_min_rtt UInt32,
 treatment_algorithm_exposed Bool,treatment_assignment_id String,location String,
 inet_diag_msg_state UInt8,inet_diag_msg_socket_destination FixedString(16),
 inet_diag_msg_family UInt8,enrich_socket_dest_asn UInt64) ENGINE=Memory;
`
	)
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
			hosts:       six(host{v4, 2}, host{v4, 2}, host{v4, 2}, host{mapped, 10}, host{mapped, 10}, host{mapped, 10}),
			expected:    []string{"192.0.2.1/32 agents=6"},
		},
		{
			name: "real_v6_separate", category: "negative",
			description: "a real IPv6 peer with the same low bytes is a different destination: two groups of three, no candidate",
			hosts:       six(host{v4, 2}, host{v4, 2}, host{v4, 2}, host{realV6, 10}, host{realV6, 10}, host{realV6, 10}),
			expected:    []string{},
		},
		{
			name: "mapped_zero", category: "boundary",
			description: "::ffff:0.0.0.0 is reported as 0.0.0.0",
			hosts:       six(host{mapped0, 10}, host{mapped0, 10}, host{mapped0, 10}, host{mapped0, 10}, host{mapped0, 10}, host{mapped0, 10}),
			expected:    []string{"0.0.0.0/32 agents=6"},
		},
		{
			name: "all_mapped", category: "corner",
			description: "only dual-stack sockets reach the peer: still reported as the IPv4 /32, not ::ffff:192.0.2.1/128",
			hosts:       six(host{mapped, 10}, host{mapped, 10}, host{mapped, 10}, host{mapped, 10}, host{mapped, 10}, host{mapped, 10}),
			expected:    []string{"192.0.2.1/32 agents=6"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sql strings.Builder
			sql.WriteString(setupSQL)
			for i, h := range tc.hosts {
				sql.WriteString(familySocketRows(fmt.Sprintf("h%d", i+1), h.destinationHex, h.family, i+1))
			}
			sql.WriteString(strings.ReplaceAll(discoverySQL, "{{TABLE}}", "default.sockets"))
			dir := t.TempDir()
			file := filepath.Join(dir, "query.sql")
			if err := os.WriteFile(file, []byte(sql.String()), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "local", "--path", filepath.Join(dir, "data"), "--queries-file", file, "--multiquery",
				"--param_start=1700000000000000000", "--param_end=1700000060000000000", "--param_location=dc1",
				"--param_minimum_rtt=0", "--param_maximum_rtt=10", "--param_minimum_retransmission=0",
				"--output_format_json_quote_64bit_integers=0")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("ClickHouse SQL: %v\n%s", err, out)
			}
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
