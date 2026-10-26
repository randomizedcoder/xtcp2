package xtcp

import "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"

// SetTreatmentStamper attaches the optional controller provenance hook before
// RunWithPoller. The hook must be concurrency-safe and must not block on I/O.
func (x *XTCP) SetTreatmentStamper(fn func(*xtcp_flat_record.XtcpFlatRecord)) {
	x.treatmentStamp = fn
}
