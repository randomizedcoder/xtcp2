package controller

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
)

//go:embed analysis.sql
var analysisSQL string
var tableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*$`)

type ClickHouse struct {
	URL, Username, Password, Table string
	Client                         *http.Client
	Now                            func() time.Time
}

func (c *ClickHouse) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *ClickHouse) query(ctx context.Context, query string, params map[string]string) ([]byte, error) {
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
	client := c.Client
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

func (c *ClickHouse) Analyze(ctx context.Context, e *treatment.Experiment) (*treatment.Evidence, error) {
	p := e.CurrentPhase()
	if p == nil {
		end := c.now().Add(-20 * time.Second)
		return c.Evidence(ctx, e.Location, e.Treatment.Destination, e.ID, e.CohortOrder, end.Add(-time.Hour), end)
	}
	start := p.IssuedAt
	if !p.ObservationStartedAt.IsZero() {
		start = p.ObservationStartedAt
	}
	return c.Evidence(ctx, e.Location, e.Treatment.Destination, e.ID, e.CohortOrder, start, c.now().Add(-20*time.Second))
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

func (c *ClickHouse) Evidence(ctx context.Context, location string, destination netip.Prefix, assignment string, agents []string, start, end time.Time) (*treatment.Evidence, error) {
	if !end.After(start) || end.Sub(start) > 7*24*time.Hour || len(agents) == 0 || len(agents) > treatment.MaxAgents {
		return nil, errors.New("invalid analysis window/population")
	}
	quoted := make([]string, len(agents))
	for i, id := range agents {
		if !treatment.ValidID(id) {
			return nil, errors.New("invalid agent")
		}
		quoted[i] = "'" + id + "'"
	}
	if !destination.IsValid() {
		return nil, errors.New("invalid destination")
	}
	addr := destination.Addr().AsSlice()
	if destination.Addr().Is4() {
		addr = append(addr, make([]byte, 12)...)
	}
	params := map[string]string{"start": strconv.FormatInt(start.UnixNano(), 10), "end": strconv.FormatInt(end.UnixNano(), 10), "location": location, "assignment": assignment, "agents": "[" + strings.Join(quoted, ",") + "]", "destination_hex": strings.ToUpper(hex.EncodeToString(addr))}
	b, e := c.query(ctx, analysisSQL, params)
	if e != nil {
		return nil, e
	}
	digest := sha256.Sum256(b)
	sha := hex.EncodeToString(digest[:])
	analyzed := c.now()
	v := &treatment.Evidence{ID: "ev-" + sha[:24] + "-" + strconv.FormatInt(analyzed.UnixNano(), 10), AnalyzedAt: analyzed, WindowStart: start, WindowEnd: end, QueryVersion: "tcp-host-deltas-v1", EnrichmentVersion: "xtcp-schema-2", ArtifactSHA256: sha}
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
