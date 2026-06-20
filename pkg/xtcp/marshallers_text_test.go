package xtcp

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func sampleEnvelope() *xtcp_flat_record.Envelope {
	return &xtcp_flat_record.Envelope{
		Row: []*xtcp_flat_record.XtcpFlatRecord{
			{Hostname: "host-a", InetDiagMsgFamily: afInet, InetDiagMsgState: 10},
			{Hostname: "host-b", InetDiagMsgFamily: afInet, InetDiagMsgState: 1},
		},
	}
}

func TestEnvelopeJSONLMarshal(t *testing.T) {
	x, _ := newMarshalFixture(t)
	buf := x.envelopeJSONLMarshal(sampleEnvelope())
	lines := strings.Split(strings.TrimRight(string(*buf), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), string(*buf))
	}
	for i, ln := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(ln), &m); err != nil {
			t.Errorf("line %d not valid JSON: %v (%q)", i, err, ln)
		}
	}
	// Trailing newline is part of the framing contract.
	if !bytes.HasSuffix(*buf, []byte("\n")) {
		t.Error("jsonl output must end with a newline")
	}
}

func TestEnvelopeDelimitedMarshal_csv(t *testing.T) {
	x, _ := newMarshalFixture(t)
	cols, err := selectColumns("hostname,inetDiagMsgState")
	if err != nil {
		t.Fatal(err)
	}
	var header atomic.Bool

	// First flush includes the header.
	buf := x.envelopeDelimitedMarshal(sampleEnvelope(), cols, ',', &header)
	rows, err := csv.NewReader(bytes.NewReader(*buf)).ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	if len(rows) != 3 { // header + 2 records
		t.Fatalf("got %d csv rows, want 3 (header+2): %v", len(rows), rows)
	}
	if rows[0][0] != "hostname" || rows[0][1] != "inetDiagMsgState" {
		t.Errorf("header = %v", rows[0])
	}
	// Humanized state: 10 → LISTEN, 1 → ESTABLISHED.
	if rows[1][1] != "LISTEN" || rows[2][1] != "ESTABLISHED" {
		t.Errorf("humanized state cells = %q, %q", rows[1][1], rows[2][1])
	}

	// Second flush omits the header (header-once).
	buf2 := x.envelopeDelimitedMarshal(sampleEnvelope(), cols, ',', &header)
	rows2, err := csv.NewReader(bytes.NewReader(*buf2)).ReadAll()
	if err != nil {
		t.Fatalf("csv parse 2: %v", err)
	}
	if len(rows2) != 2 {
		t.Errorf("second flush rows = %d, want 2 (no header)", len(rows2))
	}
}

func TestEnvelopeDelimitedMarshal_tsv(t *testing.T) {
	x, _ := newMarshalFixture(t)
	cols, _ := selectColumns("hostname,inetDiagMsgState")
	var header atomic.Bool
	buf := x.envelopeDelimitedMarshal(sampleEnvelope(), cols, '\t', &header)
	if !strings.Contains(string(*buf), "\t") {
		t.Errorf("tsv output should contain tabs: %q", string(*buf))
	}
	r := csv.NewReader(bytes.NewReader(*buf))
	r.Comma = '\t'
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("tsv parse: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("tsv rows = %d, want 3", len(rows))
	}
}

func TestRegisterTextEnvelopeMarshallers_selected(t *testing.T) {
	for _, name := range []string{MarshallerJSONL, MarshallerCSV, MarshallerTSV} {
		t.Run(name, func(t *testing.T) {
			x, _ := newMarshalFixture(t)
			x.config.MarshalTo = name
			var wg sync.WaitGroup
			wg.Add(1)
			x.InitEnvelopeMarshallers(&wg)
			wg.Wait()
			if x.EnvelopeMarshaller == nil {
				t.Fatalf("EnvelopeMarshaller nil for %q", name)
			}
			buf := x.EnvelopeMarshaller(sampleEnvelope())
			if buf == nil || len(*buf) == 0 {
				t.Fatalf("%q produced empty output", name)
			}
		})
	}
}
