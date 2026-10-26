package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
)

type evidence struct{ *runner }

var hostColumns = []string{"evidence_id", "position", "agent_id", "sockets", "bytes_acked", "goodput_bps", "retransmission_ratio", "rtt_p95_ms", "exposed_sockets", "collection_healthy"}

// digest is a content hash over the domain value so Put can detect reuse of
// an evidence ID for different content without reloading every host row.
// Timestamps hash at microsecond precision because that is what timestamptz
// keeps: evidence read back from the database must digest to the same value
// it was stored with.
func digest(e *treatment.Evidence) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s\x00%d\x00", e.ID, e.AnalyzedAt.UnixMicro(), e.WindowStart.UnixMicro(), e.WindowEnd.UnixMicro(), e.QueryVersion, e.EnrichmentVersion, e.ArtifactSHA256, len(e.Hosts))
	for i := range e.Hosts {
		v := &e.Hosts[i]
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%d\x00%t\x00", v.AgentID, v.Sockets, v.BytesAcked, math.Float64bits(v.GoodputBps), math.Float64bits(v.RetransmissionRatio), math.Float64bits(v.RttP95Ms), v.ExposedSockets, v.CollectionHealthy)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (v *evidence) Get(ctx context.Context, id string) (*treatment.Evidence, error) {
	return getEvidence(ctx, v.q(), id)
}

func getEvidence(ctx context.Context, q querier, id string) (*treatment.Evidence, error) {
	e := &treatment.Evidence{ID: id}
	err := q.QueryRowContext(ctx, "SELECT analyzed_at,window_start,window_end,query_version,enrichment_version,artifact_sha256 FROM xtcp_treatment_evidence WHERE id=$1", id).Scan(&e.AnalyzedAt, &e.WindowStart, &e.WindowEnd, &e.QueryVersion, &e.EnrichmentVersion, &e.ArtifactSHA256)
	if err != nil {
		return nil, notFound(err)
	}
	rows, err := q.QueryContext(ctx, "SELECT agent_id,sockets,bytes_acked,goodput_bps,retransmission_ratio,rtt_p95_ms,exposed_sockets,collection_healthy FROM xtcp_treatment_evidence_hosts WHERE evidence_id=$1 ORDER BY position", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h treatment.HostEvidence
		var sockets, bytes, exposed int64
		if err = rows.Scan(&h.AgentID, &sockets, &bytes, &h.GoodputBps, &h.RetransmissionRatio, &h.RttP95Ms, &exposed, &h.CollectionHealthy); err != nil {
			rows.Close()
			return nil, err
		}
		h.Sockets, h.BytesAcked, h.ExposedSockets = uint64(sockets), uint64(bytes), uint64(exposed)
		e.Hosts = append(e.Hosts, h)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return e, nil
}

func (v *evidence) Put(ctx context.Context, experimentID string, e *treatment.Evidence) error {
	return v.atomic(ctx, func(q querier) error { return putEvidence(ctx, q, experimentID, e) })
}

func putEvidence(ctx context.Context, q querier, experimentID string, e *treatment.Evidence) error {
	if e == nil {
		return errors.New("nil evidence")
	}
	want := digest(e)
	var owner, have string
	err := q.QueryRowContext(ctx, "SELECT experiment_id,digest FROM xtcp_treatment_evidence WHERE id=$1", e.ID).Scan(&owner, &have)
	if err == nil {
		if owner != experimentID || have != want {
			return store.ErrEvidenceConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err = q.ExecContext(ctx, `INSERT INTO xtcp_treatment_evidence(id,experiment_id,analyzed_at,window_start,window_end,query_version,enrichment_version,artifact_sha256,digest)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.ID, experimentID, e.AnalyzedAt, e.WindowStart, e.WindowEnd, e.QueryVersion, e.EnrichmentVersion, e.ArtifactSHA256, want); err != nil {
		if isUniqueViolation(err) {
			return store.ErrEvidenceConflict
		}
		return err
	}
	rows := make([][]any, len(e.Hosts))
	for i := range e.Hosts {
		h := &e.Hosts[i]
		rows[i] = []any{e.ID, i, h.AgentID, int64(h.Sockets), int64(h.BytesAcked), h.GoodputBps, h.RetransmissionRatio, h.RttP95Ms, int64(h.ExposedSockets), h.CollectionHealthy}
	}
	return batchInsert(ctx, q, "xtcp_treatment_evidence_hosts", hostColumns, rows)
}
