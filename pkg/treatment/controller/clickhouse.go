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

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/protobuf/types/known/timestamppb"
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

func (c *ClickHouse) Analyze(ctx context.Context, e *pb.Experiment) (*pb.Evidence, error) {
	if len(e.Phases) == 0 {
		end := c.now().Add(-20 * time.Second)
		return c.Evidence(ctx, e.Location, e.Treatment.Destination, e.Id, e.CohortOrder, end.Add(-time.Hour), end)
	}
	p := e.Phases[len(e.Phases)-1]
	start := p.IssuedAt.AsTime()
	if p.ObservationStartedAt != nil {
		start = p.ObservationStartedAt.AsTime()
	}
	return c.Evidence(ctx, e.Location, e.Treatment.Destination, e.Id, e.CohortOrder, start, c.now().Add(-20*time.Second))
}

func (c *ClickHouse) Evidence(ctx context.Context, location, destination, assignment string, agents []string, start, end time.Time) (*pb.Evidence, error) {
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
	p, e := netip.ParsePrefix(destination)
	if e != nil {
		return nil, e
	}
	addr := p.Addr().AsSlice()
	if p.Addr().Is4() {
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
	v := &pb.Evidence{Id: "ev-" + sha[:24] + "-" + strconv.FormatInt(analyzed.UnixNano(), 10), AnalyzedAt: timestamppb.New(analyzed), WindowStart: timestamppb.New(start), WindowEnd: timestamppb.New(end), QueryVersion: "tcp-host-deltas-v1", EnrichmentVersion: "xtcp-schema-2", ArtifactSha256: sha}
	dec := json.NewDecoder(bytes.NewReader(b))
	for {
		h := new(pb.HostEvidence)
		e := dec.Decode(h)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		v.Hosts = append(v.Hosts, h)
	}
	if e = treatment.ValidateEvidence(v); e != nil {
		return nil, e
	}
	return v, nil
}
