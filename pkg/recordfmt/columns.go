package recordfmt

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Column identifies one XtcpFlatRecord field for tabular (CSV/TSV) output.
// The set is derived once from the protobuf descriptor via reflection, so it
// never drifts from the schema.
type Column struct {
	Name string // protojson camelCase name — the CSV header cell
	fd   protoreflect.FieldDescriptor
}

var (
	colsOnce  sync.Once
	colsAll   []Column
	colsIndex map[string]Column
)

func initColumns() {
	colsOnce.Do(func() {
		fields := (&xtcp_flat_record.XtcpFlatRecord{}).ProtoReflect().Descriptor().Fields()
		colsAll = make([]Column, 0, fields.Len())
		colsIndex = make(map[string]Column, fields.Len())
		for i := 0; i < fields.Len(); i++ {
			fd := fields.Get(i)
			c := Column{Name: fd.JSONName(), fd: fd}
			colsAll = append(colsAll, c)
			colsIndex[c.Name] = c
		}
	})
}

// AllColumns returns the full ordered column list (proto declaration order).
func AllColumns() []Column {
	initColumns()
	return colsAll
}

// SelectColumns resolves a comma-separated column spec to an ordered list.
// Empty (or whitespace) selects all columns. Unknown names are an error so a
// typo fails fast rather than silently dropping a column.
func SelectColumns(spec string) ([]Column, error) {
	initColumns()
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return colsAll, nil
	}
	parts := strings.Split(spec, ",")
	out := make([]Column, 0, len(parts))
	for _, p := range parts {
		name := strings.TrimSpace(p)
		if name == "" {
			continue
		}
		c, ok := colsIndex[name]
		if !ok {
			return nil, fmt.Errorf("unknown column %q (expect an XtcpFlatRecord json name, e.g. hostname, inetDiagMsgSocketSourcePort, tcpInfoRtt)", name)
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return colsAll, nil
	}
	return out, nil
}

// Header returns the header cells (column names) for the given columns.
func Header(cols []Column) []string {
	h := make([]string, len(cols))
	for i, c := range cols {
		h[i] = c.Name
	}
	return h
}

// Row renders one record as string cells for the given columns. When humanize
// is set, the machine-valued fields (IP addresses, TCP state, congestion enum,
// timestamp) are rendered human-readably; everything else is the scalar value.
func Row(r *xtcp_flat_record.XtcpFlatRecord, cols []Column, humanize bool) []string {
	m := r.ProtoReflect()
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = formatField(r, m, c, humanize)
	}
	return out
}

func formatField(r *xtcp_flat_record.XtcpFlatRecord, m protoreflect.Message, c Column, humanize bool) string {
	if humanize {
		switch c.Name {
		case "inetDiagMsgSocketSource":
			return IPString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketSource())
		case "inetDiagMsgSocketDestination":
			return IPString(r.GetInetDiagMsgFamily(), r.GetInetDiagMsgSocketDestination())
		case "inetDiagMsgState":
			return TCPStateName(r.GetInetDiagMsgState())
		case "tcpInfoState":
			return TCPStateName(r.GetTcpInfoState())
		case "congestionAlgorithmEnum":
			return CongestionAlgorithmName(r.GetCongestionAlgorithmEnum())
		case "timestampNs":
			return TimestampRFC3339(r.GetTimestampNs())
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
