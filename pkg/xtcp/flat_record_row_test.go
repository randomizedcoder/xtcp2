package xtcp

import (
	"net"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func TestFlatColumns_allFields(t *testing.T) {
	cols := flatColumns()
	// XtcpFlatRecord field count (from the proto descriptor). Guard it so an
	// accidental schema change is noticed; bump deliberately when fields are
	// added to the .proto.
	if len(cols) != 122 {
		t.Errorf("flatColumns len = %d, want 122", len(cols))
	}
	// Header names are the protojson camelCase names.
	hdr := flatRecordHeader(cols)
	if hdr[0] == "" {
		t.Error("empty header cell")
	}
	want := map[string]bool{"hostname": false, "timestampNs": false, "congestionAlgorithmEnum": false}
	for _, h := range hdr {
		if _, ok := want[h]; ok {
			want[h] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("expected column %q in header", name)
		}
	}
}

func TestSelectColumns(t *testing.T) {
	t.Run("empty selects all", func(t *testing.T) {
		got, err := selectColumns("")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(flatColumns()) {
			t.Errorf("empty spec selected %d cols, want all %d", len(got), len(flatColumns()))
		}
	})
	t.Run("subset preserves order", func(t *testing.T) {
		got, err := selectColumns("hostname, tcpInfoRtt ,inetDiagMsgState")
		if err != nil {
			t.Fatal(err)
		}
		names := flatRecordHeader(got)
		wantOrder := []string{"hostname", "tcpInfoRtt", "inetDiagMsgState"}
		if strings.Join(names, ",") != strings.Join(wantOrder, ",") {
			t.Errorf("subset = %v, want %v", names, wantOrder)
		}
	})
	t.Run("unknown column errors", func(t *testing.T) {
		if _, err := selectColumns("hostname,not_a_field"); err == nil {
			t.Fatal("expected error for unknown column")
		}
	})
}

func TestFlatRecordValues_humanize(t *testing.T) {
	r := &xtcp_flat_record.XtcpFlatRecord{
		Hostname:                    "host-a",
		InetDiagMsgFamily:           afInet,
		InetDiagMsgSocketSource:     []byte(net.ParseIP("10.0.0.5").To4()),
		InetDiagMsgSocketSourcePort: 443,
		InetDiagMsgState:            10, // LISTEN
		TcpInfoState:                10,
		CongestionAlgorithmEnum:     xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC,
		TimestampNs:                 1_700_000_000_000_000_000,
	}
	cols, err := selectColumns("hostname,inetDiagMsgSocketSource,inetDiagMsgSocketSourcePort,inetDiagMsgState,congestionAlgorithmEnum")
	if err != nil {
		t.Fatal(err)
	}

	// Humanized: address dotted-quad, state name, congestion name.
	h := flatRecordValues(r, cols, true)
	wantH := []string{"host-a", "10.0.0.5", "443", "LISTEN", "CUBIC"}
	for i := range wantH {
		if h[i] != wantH[i] {
			t.Errorf("humanized[%d] = %q, want %q", i, h[i], wantH[i])
		}
	}

	// Raw: address base64, state/enum numeric.
	raw := flatRecordValues(r, cols, false)
	if raw[1] == "10.0.0.5" {
		t.Errorf("raw address should not be dotted-quad: %q", raw[1])
	}
	if raw[3] != "10" {
		t.Errorf("raw state = %q, want \"10\"", raw[3])
	}
	if raw[4] != "1" {
		t.Errorf("raw congestion enum = %q, want \"1\"", raw[4])
	}
}
