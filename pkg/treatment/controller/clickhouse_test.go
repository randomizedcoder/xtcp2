package controller

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
)

// Execute the production SQL against an isolated ClickHouse local database.
// No live service or production telemetry is accessed.
func TestClickHouseAnalysisCounterDeltas(t *testing.T) {
	binary := os.Getenv("XTCP_CLICKHOUSE_TEST_BINARY")
	if binary == "" {
		t.Skip("set XTCP_CLICKHOUSE_TEST_BINARY to run real SQL")
	}
	setup := `CREATE TABLE default.sockets (
 agent_id String,host_boot_id String,netns_inode UInt64,inet_diag_msg_socket_cookie UInt64,
 timestamp_ns DateTime64(9),tcp_info_bytes_acked UInt64,tcp_info_bytes_sent UInt64,
 tcp_info_bytes_retrans UInt64,tcp_info_rtt UInt32,tcp_info_min_rtt UInt32,
 treatment_algorithm_exposed Bool,treatment_assignment_id String,location String,
 inet_diag_msg_state UInt8,inet_diag_msg_socket_destination FixedString(16),
 inet_diag_msg_family UInt8,enrich_socket_dest_asn UInt64) ENGINE=Memory;
 INSERT INTO default.sockets SELECT 'host-1','boot-1',1,1,
 fromUnixTimestamp64Nano(1700000000000000000+toInt64(number)*10000000000),
 1000000+number*1000,1000000+number*1000,number*10,1000,500,true,'trial','dc1',1,
 unhex('C0000201000000000000000000000000'),2,64512 FROM numbers(6);
 INSERT INTO default.sockets SELECT * FROM default.sockets;
 `
	query := strings.ReplaceAll(analysisSQL, "{{TABLE}}", "default.sockets")
	run := func(sql string) []byte {
		t.Helper()
		dir := t.TempDir()
		file := filepath.Join(dir, "query.sql")
		if e := os.WriteFile(file, []byte(sql), 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(binary, "local", "--path", filepath.Join(dir, "data"), "--queries-file", file, "--multiquery",
			"--param_start=1700000000000000000", "--param_end=1700000060000000000", "--param_location=dc1",
			"--param_agents=['host-1']", "--param_destination_hex=C0000201000000000000000000000000", "--param_assignment=trial",
			"--param_minimum_rtt=0", "--param_maximum_rtt=10", "--param_minimum_retransmission=0",
			"--output_format_json_quote_64bit_integers=0")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("ClickHouse SQL: %v\n%s", e, out)
		}
		return out
	}
	var h pb.HostEvidence
	if e := json.Unmarshal(run(setup+query), &h); e != nil {
		t.Fatal(e)
	}
	if h.Sockets != 1 || h.BytesAcked != 5000 || h.ExposedSockets != 1 || !h.CollectionHealthy || h.RttP95Ms != 1 || h.RetransmissionRatio != .01 {
		t.Fatalf("bad window deltas/deduplication: %v", &h)
	}
	reset := `INSERT INTO default.sockets SELECT 'host-1','boot-1',1,1,
 fromUnixTimestamp64Nano(1700000055000000000),1,1,0,1000,500,true,'trial','dc1',1,
 unhex('C0000201000000000000000000000000'),2,64512;`
	if e := json.Unmarshal(run(setup+reset+query), &h); e != nil {
		t.Fatal(e)
	}
	if h.CollectionHealthy {
		t.Fatal("counter reset accepted as healthy")
	}
	// Syntax/type-check candidate discovery too; a one-host fixture cannot meet
	// its six-host eligibility threshold.
	if out := run(setup + strings.ReplaceAll(discoverySQL, "{{TABLE}}", "default.sockets")); len(out) != 0 {
		t.Fatalf("unexpected discovery: %s", out)
	}
}
