package goip

import (
	"bytes"
	"os"
	"testing"
)

const neighDumpPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneigh.pcap"

func TestRunNeighShowMatchesCapturedSidecar(t *testing.T) {
	want, err := os.ReadFile("../../pkg/xtcpnl/testdata/7_1_4/dumps/ip_neigh_n")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOIP_REPLAY", neighDumpPcap)
	t.Setenv("GOIP_REPLAY_PORTID", "894")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"neigh", "show"}, &stdout, &stderr); code != ExitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("neighbor output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
	}
}
