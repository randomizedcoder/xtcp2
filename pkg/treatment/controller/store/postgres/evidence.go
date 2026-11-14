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

var hostColumns = []string{"evidence_id", positionColumnCst, agentIDColumnCst, "sockets", "bytes_acked", "goodput_bps", "retransmission_ratio", "rtt_p95_ms", "exposed_sockets", "collection_healthy", "unbound_sockets"}

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
		// Hashed only when set, so evidence stored before the column
		// existed keeps the digest it was stored with.
		if v.UnboundSockets != 0 {
			fmt.Fprintf(h, "u%d\x00", v.UnboundSockets)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (v *evidence) Get(ctx context.Context, id string) (*treatment.Evidence, error) {
	return getEvidence(ctx, v.q(), id)
}

func getEvidence(ctx context.Context, q querier, id string) (*treatment.Evidence, error) {
	loaded, err := getEvidenceMany(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	e, ok := loaded[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return e, nil
}

// getEvidenceMany reads the named evidence documents and their host rows in
// two statements. IDs that do not exist are absent from the result.
func getEvidenceMany(ctx context.Context, q querier, ids []string) (map[string]*treatment.Evidence, error) {
	out := make(map[string]*treatment.Evidence, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, "SELECT id,analyzed_at,window_start,window_end,query_version,enrichment_version,artifact_sha256 FROM xtcp_treatment_evidence WHERE id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		e := new(treatment.Evidence)
		if err = rows.Scan(&e.ID, &e.AnalyzedAt, &e.WindowStart, &e.WindowEnd, &e.QueryVersion, &e.EnrichmentVersion, &e.ArtifactSHA256); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		out[e.ID] = e
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	return out, loadEvidenceHosts(ctx, q, ids, out)
}

// loadEvidenceHosts appends each evidence document's host rows in position
// order.
func loadEvidenceHosts(ctx context.Context, q querier, ids []string, into map[string]*treatment.Evidence) error {
	rows, err := q.QueryContext(ctx, "SELECT evidence_id,agent_id,sockets,bytes_acked,goodput_bps,retransmission_ratio,rtt_p95_ms,exposed_sockets,collection_healthy,unbound_sockets FROM xtcp_treatment_evidence_hosts WHERE evidence_id = ANY($1) ORDER BY evidence_id,position", ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var h treatment.HostEvidence
		var sockets, bytes, exposed, unbound int64
		if err = rows.Scan(&id, &h.AgentID, &sockets, &bytes, &h.GoodputBps, &h.RetransmissionRatio, &h.RttP95Ms, &exposed, &h.CollectionHealthy, &unbound); err != nil {
			return errors.Join(err, rows.Close())
		}
		h.Sockets, h.BytesAcked, h.ExposedSockets, h.UnboundSockets = uint64(sockets), uint64(bytes), uint64(exposed), uint64(unbound)
		if e, ok := into[id]; ok {
			e.Hosts = append(e.Hosts, h)
		}
	}
	return errors.Join(rows.Err(), rows.Close())
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
		rows[i] = []any{e.ID, i, h.AgentID, int64(h.Sockets), int64(h.BytesAcked), h.GoodputBps, h.RetransmissionRatio, h.RttP95Ms, int64(h.ExposedSockets), h.CollectionHealthy, int64(h.UnboundSockets)}
	}
	return batchInsert(ctx, q, "xtcp_treatment_evidence_hosts", hostColumns, rows)
}
