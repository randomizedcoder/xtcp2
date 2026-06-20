package xtcp

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Tabular (CSV/TSV) row encoding for XtcpFlatRecord. The column set is derived
// once from the protobuf descriptor via reflection, so it never drifts from
// the schema — adding a field to the .proto adds a column automatically. The
// few fields that are machine values in the wire format (IP-address bytes, the
// congestion enum, TCP-state integers, the nanosecond timestamp) are rendered
// human-readably when humanize is set; see humanize.go.

type flatCol struct {
	name string // protojson camelCase name — the CSV header cell
	fd   protoreflect.FieldDescriptor
}

var (
	flatColsOnce  sync.Once
	flatColsAll   []flatCol
	flatColsIndex map[string]flatCol
)

// flatColumns returns the full ordered column list (proto declaration order),
// computed once from the XtcpFlatRecord descriptor.
func flatColumns() []flatCol {
	flatColsOnce.Do(func() {
		fields := (&xtcp_flat_record.XtcpFlatRecord{}).ProtoReflect().Descriptor().Fields()
		flatColsAll = make([]flatCol, 0, fields.Len())
		flatColsIndex = make(map[string]flatCol, fields.Len())
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			c := flatCol{name: fd.JSONName(), fd: fd}
			flatColsAll = append(flatColsAll, c)
			flatColsIndex[c.name] = c
		}
	})
	return flatColsAll
}

// selectColumns resolves a comma-separated `-columns` spec to an ordered
// column list. Empty (or whitespace) selects all columns. Unknown names are
// an error so a typo fails fast rather than silently dropping a column.
func selectColumns(spec string) ([]flatCol, error) {
	all := flatColumns()
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return all, nil
	}
	parts := strings.Split(spec, ",")
	out := make([]flatCol, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		c, ok := flatColsIndex[name]
		if !ok {
			return nil, fmt.Errorf("unknown -columns field %q (expect an XtcpFlatRecord json name, e.g. hostname, inetDiagMsgSocketSourcePort, tcpInfoRtt)", name)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return all, nil
	}
	return out, nil
}

func flatRecordHeader(cols []flatCol) []string {
	h := make([]string, len(cols))
	for i, c := range cols {
		h[i] = c.name
	}
	return h
}

func flatRecordValues(r *xtcp_flat_record.XtcpFlatRecord, cols []flatCol, humanize bool) []string {
	m := r.ProtoReflect()
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = formatField(r, m, c, humanize)
	}
	return out
}

// formatField renders one column. When humanize is set, the handful of
// machine-valued fields are formatted via the humanize.go helpers; everything
// else (and everything when humanize is false) goes through formatScalar.
func formatField(r *xtcp_flat_record.XtcpFlatRecord, m protoreflect.Message, c flatCol, humanize bool) string {
	if humanize {
		switch c.name {
		case "inetDiagMsgSocketSource":
			return ipString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketSource())
		case "inetDiagMsgSocketDestination":
			return ipString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketDestination())
		case "inetDiagMsgState":
			return tcpStateName(r.GetInetDiagMsgState())
		case "tcpInfoState":
			return tcpStateName(r.GetTcpInfoState())
		case "congestionAlgorithmEnum":
			return congestionAlgorithmName(r.GetCongestionAlgorithmEnum())
		case "timestampNs":
			return timestampRFC3339(r.GetTimestampNs())
		}
	}
	return formatScalar(c.fd, m.Get(c.fd))
}

// formatScalar renders a protoreflect scalar value as a string. XtcpFlatRecord
// has no nested-message or repeated fields, so scalar coverage is sufficient;
// the two bytes fields are IP addresses (base64 here, humanized elsewhere).
func formatScalar(fd protoreflect.FieldDescriptor, v protoreflect.Value) string {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return strconv.FormatBool(v.Bool())
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return strconv.FormatInt(v.Int(), 10)
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind,
		protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return strconv.FormatUint(v.Uint(), 10)
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case protoreflect.StringKind:
		return v.String()
	case protoreflect.BytesKind:
		return base64.StdEncoding.EncodeToString(v.Bytes())
	case protoreflect.EnumKind:
		return strconv.FormatInt(int64(v.Enum()), 10)
	default:
		return v.String()
	}
}
