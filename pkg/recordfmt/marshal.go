package recordfmt

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	msgpack "github.com/vmihailenco/msgpack/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/prototext"
)

// Format names — the single source of truth shared by the daemon's -marshal
// flag and the client's -format flag.
const (
	FormatProtobufList = "protobufList" // length-delimited Envelope (binary; daemon/Kafka/ClickHouse)
	FormatProtoJSON    = "protoJson"    // one JSON object per Envelope
	FormatProtoText    = "protoText"    // protobuf text
	FormatMsgPack      = "msgpack"      // MessagePack (binary)
	FormatJSON         = "json"         // one compact JSON record (per-record; client default)
	FormatJSONL        = "jsonl"        // one raw JSON record per line (NDJSON)
	FormatCSV          = "csv"          // comma-separated, humanized, header once
	FormatTSV          = "tsv"          // tab-separated, humanized, header once
	FormatHumanize     = "humanize"     // one humanized JSON record per line
	FormatNull         = "null"         // discard (benchmark the receive/collect path)
)

// MarshalJSON encodes a single record as compact JSON (no trailing newline).
// Values are raw/machine (addresses base64, state/enum numeric).
func MarshalJSON(r *xtcp_flat_record.XtcpFlatRecord) ([]byte, error) {
	b, err := protojson.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: protojson.Marshal: %w", err)
	}
	return b, nil
}

// MarshalText encodes a single record as protobuf text (no trailing newline).
func MarshalText(r *xtcp_flat_record.XtcpFlatRecord) ([]byte, error) {
	return []byte(prototext.Format(r)), nil
}

// MarshalMsgPack encodes a single record as MessagePack.
func MarshalMsgPack(r *xtcp_flat_record.XtcpFlatRecord) ([]byte, error) {
	b, err := msgpack.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: msgpack.Marshal: %w", err)
	}
	return b, nil
}

// MarshalHumanizedJSON encodes a single record as compact JSON with the
// machine-valued fields rendered human-readably: source/destination addresses
// as dotted-quad/v6, TCP state and congestion as names, timestamp as RFC3339.
// Other fields keep their native JSON types. No trailing newline.
//
// It starts from protojson output (so field presence/omitempty matches the
// other JSON formats) and overwrites only the present special fields.
func MarshalHumanizedJSON(r *xtcp_flat_record.XtcpFlatRecord) ([]byte, error) {
	raw, err := protojson.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: protojson.Marshal: %w", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("recordfmt: json.Unmarshal: %w", err)
	}

	set := func(key, val string) error {
		if _, present := m[key]; !present {
			return nil
		}
		enc, err := json.Marshal(val)
		if err != nil {
			return err
		}
		m[key] = enc
		return nil
	}
	for _, kv := range [][2]string{
		{"inetDiagMsgSocketSource", IPString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketSource())},
		{"inetDiagMsgSocketDestination", IPString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketDestination())},
		{"inetDiagMsgState", TCPStateName(r.GetInetDiagMsgState())},
		{"tcpInfoState", TCPStateName(r.GetTcpInfoState())},
		{"congestionAlgorithmEnum", CongestionAlgorithmName(r.GetCongestionAlgorithmEnum())},
		{"timestampNs", TimestampRFC3339(r.GetTimestampNs())},
	} {
		if err := set(kv[0], kv[1]); err != nil {
			return nil, fmt.Errorf("recordfmt: humanize %s: %w", kv[0], err)
		}
	}

	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("recordfmt: json.Marshal: %w", err)
	}
	return b, nil
}
