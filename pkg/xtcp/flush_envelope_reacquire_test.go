package xtcp

import (
	"context"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// TestFlushEnvelope_reAcquireContract pins the envelope lifecycle contract
// that guards against the envelopePostFlushDrop data-loss bug.
//
// Background: a poll cycle acquires exactly one currentEnvelope in
// pollAllNetlinkSockets and re-acquires the next only at the START of the
// following cycle (up to poll_frequency later — 1h in prod). If a mid-poll
// cap flush (size_cap / rows_cap, triggered inline from processInetDiagRecord)
// leaves currentEnvelope == nil, every record parsed for the rest of that
// cycle hits the nil-envelope branch in processInetDiagRecord and is dropped
// (counted as Deserialize/envelopePostFlushDrop). On busy hosts that is the
// majority of a poll's records.
//
// Contract: a mid-poll cap flush must install a FRESH envelope so the cycle
// keeps accumulating; a terminal flush (poll_end / poll_timeout / shutdown)
// ends the cycle and must leave currentEnvelope nil for the next
// pollAllNetlinkSockets to re-acquire.
//
// Table columns: description (what the case exercises), category
// (positive / negative / boundary / corner), reason, seedRows (rows in the
// in-flight envelope; -1 = no in-flight envelope at all), and the expected
// outcome (wantReAcquired = currentEnvelope non-nil after flush; wantSends =
// dest.Send calls) plus a human-readable outcome string.
func TestFlushEnvelope_reAcquireContract(t *testing.T) {
	tests := []struct {
		description    string
		category       string
		reason         string
		seedRows       int
		wantReAcquired bool
		wantSends      int
		outcome        string
	}{
		{
			description:    "size_cap mid-poll flush installs a fresh envelope so the rest of the poll cycle keeps accumulating",
			category:       "positive",
			reason:         "size_cap",
			seedRows:       3,
			wantReAcquired: true,
			wantSends:      1,
			outcome:        "fresh envelope installed; full batch shipped once",
		},
		{
			description:    "rows_cap mid-poll flush installs a fresh envelope so subsequent records are not dropped",
			category:       "positive",
			reason:         "rows_cap",
			seedRows:       3,
			wantReAcquired: true,
			wantSends:      1,
			outcome:        "fresh envelope installed; full batch shipped once",
		},
		{
			description:    "size_cap flush of an empty (0-row) in-flight envelope still re-acquires but ships nothing",
			category:       "boundary",
			reason:         "size_cap",
			seedRows:       0,
			wantReAcquired: true,
			wantSends:      0,
			outcome:        "fresh envelope installed; nothing to Send",
		},
		{
			description:    "poll_end terminal flush ends the cycle and leaves currentEnvelope nil for the next poll",
			category:       "negative",
			reason:         "poll_end",
			seedRows:       3,
			wantReAcquired: false,
			wantSends:      1,
			outcome:        "no re-acquire; batch shipped once",
		},
		{
			description:    "poll_timeout terminal flush leaves currentEnvelope nil",
			category:       "negative",
			reason:         "poll_timeout",
			seedRows:       3,
			wantReAcquired: false,
			wantSends:      1,
			outcome:        "no re-acquire; batch shipped once",
		},
		{
			description:    "shutdown terminal flush leaves currentEnvelope nil",
			category:       "corner",
			reason:         "shutdown",
			seedRows:       3,
			wantReAcquired: false,
			wantSends:      1,
			outcome:        "no re-acquire; batch shipped once",
		},
		{
			description:    "shutdown flush with no in-flight envelope is a safe no-op",
			category:       "corner",
			reason:         "shutdown",
			seedRows:       -1,
			wantReAcquired: false,
			wantSends:      0,
			outcome:        "no panic; nothing shipped; stays nil",
		},
	}

	for _, tc := range tests {
		t.Run(tc.category+"/"+tc.reason, func(t *testing.T) {
			x := newPollerFixture(t)
			rec := newRecordingDest(x)
			x.dest = rec
			x.EnvelopeMarshaller = func(_ *xtcp_flat_record.Envelope) *[]byte {
				b := []byte("payload")
				return &b
			}

			switch {
			case tc.seedRows < 0:
				x.currentEnvelope = nil
			case tc.seedRows == 0:
				x.currentEnvelope = new(xtcp_flat_record.Envelope)
			default:
				rows := make([]*xtcp_flat_record.XtcpFlatRecord, tc.seedRows)
				for i := range rows {
					rows[i] = &xtcp_flat_record.XtcpFlatRecord{Hostname: "h"}
				}
				x.currentEnvelope = &xtcp_flat_record.Envelope{Row: rows}
			}

			x.flushEnvelope(context.Background(), tc.reason)

			gotReAcquired := x.currentEnvelope != nil
			if gotReAcquired != tc.wantReAcquired {
				t.Errorf("%s [%s]: currentEnvelope re-acquired = %v, want %v (%s)",
					tc.description, tc.category, gotReAcquired, tc.wantReAcquired, tc.outcome)
			}
			if got := rec.Count(); got != tc.wantSends {
				t.Errorf("%s [%s]: dest.Send calls = %d, want %d (%s)",
					tc.description, tc.category, got, tc.wantSends, tc.outcome)
			}
		})
	}
}
