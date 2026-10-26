// Package clickhouse is the warehouse adapter: it implements evidence.Source
// and evidence.CandidateSource over the ClickHouse HTTP interface with two
// embedded, parameterized queries. It is the only package that knows the
// telemetry table's columns; the controller sees evidence.Window and
// evidence.Candidate.
package clickhouse

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/evidence"
)

//go:embed analysis.sql
var analysisSQL string

//go:embed discovery.sql
var discoverySQL string

var tableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*$`)

// QueryVersion names the analysis query so evidence records which SQL
// produced them; bump it when analysis.sql changes meaning.
const QueryVersion = "tcp-host-deltas-v1"

// EnrichmentVersion names the telemetry schema the queries assume.
const EnrichmentVersion = "xtcp-schema-2"

// Client talks to one ClickHouse HTTP(S) endpoint and one table.
type Client struct {
	URL, Username, Password, Table string
	HTTP                           *http.Client
	Now                            func() time.Time
}

var _ evidence.Warehouse = (*Client)(nil)

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// query POSTs one parameterized statement and returns the raw response.
// Parameters travel as ClickHouse `param_*` query parameters, never by
// string interpolation; the table name is validated before substitution.
func (c *Client) query(ctx context.Context, query string, params map[string]string) ([]byte, error) {
	if !tableName.MatchString(c.Table) {
		return nil, errors.New("ClickHouse table must be database.table")
	}
	u, e := url.Parse(c.URL)
	if e != nil {
		return nil, e
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("ClickHouse URL must be HTTP(S)")
	}
	v := u.Query()
	for k, val := range params {
		v.Set("param_"+k, val)
	}
	v.Set("max_execution_time", "30")
	v.Set("max_result_rows", "10000")
	v.Set("result_overflow_mode", "throw")
	v.Set("output_format_json_quote_64bit_integers", "0")
	u.RawQuery = v.Encode()
	body := strings.ReplaceAll(query, "{{TABLE}}", c.Table)
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(body))
	if e != nil {
		return nil, e
	}
	r.SetBasicAuth(c.Username, c.Password)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 35 * time.Second}
	}
	response, e := client.Do(r)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if e != nil {
		return nil, e
	}
	if len(b) >= 8<<20 {
		return nil, errors.New("ClickHouse result exceeds 8 MiB")
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClickHouse status %d: %.1024s", response.StatusCode, b)
	}
	return b, nil
}

// hostRow is one JSONEachRow line of analysis.sql.
type hostRow struct {
	AgentID             string  `json:"agent_id"`
	Sockets             uint64  `json:"sockets"`
	BytesAcked          uint64  `json:"bytes_acked"`
	GoodputBps          float64 `json:"goodput_bps"`
	RetransmissionRatio float64 `json:"retransmission_ratio"`
	RttP95Ms            float64 `json:"rtt_p95_ms"`
	ExposedSockets      uint64  `json:"exposed_sockets"`
	CollectionHealthy   bool    `json:"collection_healthy"`
}

// destinationHex renders an address the way the telemetry stores it: a
// 16-byte FixedString, IPv4 left-aligned and zero padded.
func destinationHex(a netip.Addr) string {
	raw := a.AsSlice()
	if a.Is4() {
		raw = append(raw, make([]byte, 12)...)
	}
	return strings.ToUpper(hex.EncodeToString(raw))
}

// Evidence runs analysis.sql for the window. ArtifactSHA256 is the digest of
// the raw response bytes as received; the response itself is not archived,
// so reproducing it means re-running QueryVersion over the same window.
func (c *Client) Evidence(ctx context.Context, w evidence.Window) (*treatment.Evidence, error) {
	if e := w.Validate(); e != nil {
		return nil, e
	}
	quoted := make([]string, len(w.Agents))
	for i, id := range w.Agents {
		quoted[i] = "'" + id + "'"
	}
	params := map[string]string{"start": strconv.FormatInt(w.Start.UnixNano(), 10), "end": strconv.FormatInt(w.End.UnixNano(), 10), "location": w.Location, "assignment": w.ExperimentID, "agents": "[" + strings.Join(quoted, ",") + "]", "destination_hex": destinationHex(w.Destination.Addr())}
	b, e := c.query(ctx, analysisSQL, params)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(b)
	sha := hex.EncodeToString(digest[:])
	analyzed := c.now()
	v := &treatment.Evidence{ID: "ev-" + sha[:24] + "-" + strconv.FormatInt(analyzed.UnixNano(), 10), AnalyzedAt: analyzed, WindowStart: w.Start, WindowEnd: w.End, QueryVersion: QueryVersion, EnrichmentVersion: EnrichmentVersion, ArtifactSHA256: sha}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		var h hostRow
		e := dec.Decode(&h)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		v.Hosts = append(v.Hosts, treatment.HostEvidence(h))
	}
	if e = treatment.ValidateEvidence(v); e != nil {
		return nil, e
	}
	return v, nil
}

// candidateRow is one JSONEachRow line of discovery.sql.
type candidateRow struct {
	DestinationHex string   `json:"destination_hex"`
	Family         uint32   `json:"family"`
	ASN            uint64   `json:"asn"`
	Agents         []string `json:"agents"`
	RTT            float64  `json:"rtt_ms"`
	Retransmission float64  `json:"retransmission_ratio"`
}

// Candidates runs discovery.sql. A row whose destination does not decode is
// an error, not a skipped row: it means the table is not the schema this
// adapter was written for.
func (c *Client) Candidates(ctx context.Context, q evidence.CandidateQuery) ([]evidence.Candidate, error) {
	if e := q.Validate(); e != nil {
		return nil, e
	}
	params := map[string]string{"location": q.Location, "start": strconv.FormatInt(q.Start.UnixNano(), 10), "end": strconv.FormatInt(q.End.UnixNano(), 10), "minimum_rtt": fmt.Sprint(q.MinimumRTTMs), "maximum_rtt": fmt.Sprint(q.MaximumRTTMs), "minimum_retransmission": fmt.Sprint(q.MinimumRetransmissionRatio)}
	data, e := c.query(ctx, discoverySQL, params)
	if e != nil {
		return nil, e
	}
	var out []evidence.Candidate
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var row candidateRow
		if e = dec.Decode(&row); e == io.EOF {
			return out, nil
		}
		if e != nil {
			return nil, e
		}
		raw, e := hex.DecodeString(row.DestinationHex)
		if e != nil {
			return nil, fmt.Errorf("candidate destination %q: %w", row.DestinationHex, e)
		}
		if row.Family == 2 && len(raw) >= 4 {
			raw = raw[:4]
		}
		ip, ok := netip.AddrFromSlice(raw)
		if !ok {
			return nil, fmt.Errorf("candidate destination %q: not an address for family %d", row.DestinationHex, row.Family)
		}
		for _, id := range row.Agents {
			if !treatment.ValidID(id) {
				return nil, fmt.Errorf("candidate %s: invalid agent id %q", ip, id)
			}
		}
		out = append(out, evidence.Candidate{Destination: netip.PrefixFrom(ip, ip.BitLen()), ASN: row.ASN, Agents: row.Agents, RTTMs: row.RTT, RetransmissionRatio: row.Retransmission})
	}
}
