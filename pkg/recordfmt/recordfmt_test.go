package recordfmt

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func sampleRecord() *xtcp_flat_record.XtcpFlatRecord {
	return &xtcp_flat_record.XtcpFlatRecord{
		Hostname:                    "host-a",
		InetDiagMsgFamily:           afInet,
		InetDiagMsgSocketSource:     []byte(net.ParseIP("10.0.0.5").To4()),
		InetDiagMsgSocketSourcePort: 443,
		InetDiagMsgState:            10, // LISTEN
		TcpInfoState:                10,
		CongestionAlgorithmEnum:     xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC,
	}
}

func sampleEnvelope() *xtcp_flat_record.Envelope {
	return &xtcp_flat_record.Envelope{
		Row: []*xtcp_flat_record.XtcpFlatRecord{
			sampleRecord(),
			{Hostname: "host-b", InetDiagMsgFamily: afInet, InetDiagMsgState: 1},
		},
	}
}

func TestIPString(t *testing.T) {
	cases := []struct {
		family uint32
		in     []byte
		want   string
	}{
		{afInet, nil, ""},
		{afInet, []byte{192, 168, 0, 1}, "192.168.0.1"},
		{afInet, append([]byte{192, 168, 122, 1}, make([]byte, 12)...), "192.168.122.1"}, // v4 in 16-byte slot
		{afInet6, net.IPv6loopback, "::1"},
	}
	for _, c := range cases {
		if got := IPString(c.family, c.in); got != c.want {
			t.Errorf("IPString(%d,%v)=%q want %q", c.family, c.in, got, c.want)
		}
	}
}

func TestTCPStateAndCongestionNames(t *testing.T) {
	if TCPStateName(10) != "LISTEN" || TCPStateName(1) != "ESTABLISHED" || TCPStateName(99) != "99" {
		t.Error("TCPStateName mismatch")
	}
	if CongestionAlgorithmName(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC) != "CUBIC" {
		t.Error("congestion name mismatch")
	}
	if CongestionAlgorithmName(xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED) != "" {
		t.Error("unspecified congestion should be empty")
	}
}

func TestSelectColumns(t *testing.T) {
	if all, err := SelectColumns(""); err != nil || len(all) != len(AllColumns()) {
		t.Fatalf("empty spec: %v len=%d", err, len(all))
	}
	got, err := SelectColumns("hostname, inetDiagMsgState")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(Header(got), ",") != "hostname,inetDiagMsgState" {
		t.Errorf("subset header = %v", Header(got))
	}
	if _, err := SelectColumns("hostname,nope"); err == nil {
		t.Error("expected error for unknown column")
	}
}

func TestMarshalJSON(t *testing.T) {
	b, err := MarshalJSON(sampleRecord())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
	if bytes.HasSuffix(b, []byte("\n")) {
		t.Error("per-record JSON must not have a trailing newline")
	}
}

func TestMarshalHumanizedJSON(t *testing.T) {
	b, err := MarshalHumanizedJSON(sampleRecord())
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	if m["inetDiagMsgSocketSource"] != "10.0.0.5" {
		t.Errorf("address not humanized: %v", m["inetDiagMsgSocketSource"])
	}
	if m["inetDiagMsgState"] != "LISTEN" {
		t.Errorf("state not humanized: %v", m["inetDiagMsgState"])
	}
	if m["congestionAlgorithmEnum"] != "CUBIC" {
		t.Errorf("congestion not humanized: %v", m["congestionAlgorithmEnum"])
	}
	// A non-special numeric field stays a JSON number.
	if _, ok := m["inetDiagMsgSocketSourcePort"].(float64); !ok {
		t.Errorf("port should remain numeric, got %T", m["inetDiagMsgSocketSourcePort"])
	}
}

func TestMarshalEnvelopeJSONL(t *testing.T) {
	b, err := MarshalEnvelopeJSONL(sampleEnvelope())
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines want 2", len(lines))
	}
	for _, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Errorf("line not JSON: %v", err)
		}
	}
}

func TestMarshalEnvelopeTable(t *testing.T) {
	cols, _ := SelectColumns("hostname,inetDiagMsgState")
	b, err := MarshalEnvelopeTable(sampleEnvelope(), cols, ',', true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(bytes.NewReader(b)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0][0] != "hostname" {
		t.Fatalf("rows=%v", rows)
	}
	if rows[1][1] != "LISTEN" || rows[2][1] != "ESTABLISHED" {
		t.Errorf("humanized state cells = %q %q", rows[1][1], rows[2][1])
	}
	// TSV path: header omitted.
	tb, err := MarshalEnvelopeTable(sampleEnvelope(), cols, '\t', false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tb), "\t") {
		t.Error("tsv should contain tabs")
	}
}

func TestMarshalEnvelopeProtobufList_binaryNoNewline(t *testing.T) {
	b, err := MarshalEnvelopeProtobufList(sampleEnvelope())
	if err != nil || len(b) == 0 {
		t.Fatalf("protobufList: %v len=%d", err, len(b))
	}
}
