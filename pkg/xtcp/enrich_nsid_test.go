package xtcp

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// nsidStamp is what applyEnrichment wrote for the nsid pair.
type nsidStamp struct {
	nsid     uint32
	assigned bool
}

// nsidInput is one stamping scenario: the published snapshot (nil when
// -populateNsid is off) and the inodes stamped, in order, onto one pooled
// record that is recycled between stamps. The last stamp is checked.
type nsidInput struct {
	snapshot map[uint64]int32
	inodes   []uint64
}

// go test ./pkg/xtcp/ -run TestApplyEnrichmentNsid
func TestApplyEnrichmentNsid(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       nsidInput
		expected    nsidStamp
	}{
		{"assigned_nonzero", "positive", "an assigned id is stamped with presence",
			nsidInput{map[uint64]int32{7: 3}, []uint64{7}}, nsidStamp{3, true}},
		{"assigned_zero", "positive", "the kernel's first id is told apart from none",
			nsidInput{map[uint64]int32{7: 0}, []uint64{7}}, nsidStamp{0, true}},
		{"unassigned", "negative", "a namespace missing from the map",
			nsidInput{map[uint64]int32{7: 3}, []uint64{9}}, nsidStamp{0, false}},
		{"populate_off", "negative", "-populateNsid off, no snapshot",
			nsidInput{nil, []uint64{7}}, nsidStamp{0, false}},
		{"max_id", "boundary", "the largest id the kernel hands out",
			nsidInput{map[uint64]int32{7: 0x7fffffff}, []uint64{7}}, nsidStamp{0x7fffffff, true}},
		{"recycled_record", "corner", "a pooled record doesn't carry presence into the next socket",
			nsidInput{map[uint64]int32{7: 0}, []uint64{7, 9}}, nsidStamp{0, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := new(XTCP)
			if tc.input.snapshot != nil {
				m := tc.input.snapshot
				x.nsidByInode.Store(&m)
			}
			r := new(xtcp_flat_record.XtcpFlatRecord)
			for i, inode := range tc.input.inodes {
				if i > 0 {
					recycleRecord(r)
				}
				r.NetnsInode = inode
				x.applyEnrichment(r)
			}
			got := nsidStamp{r.Nsid, r.NsidAssigned}
			if got != tc.expected {
				t.Errorf("%s (%s): %s\ninput %+v\nexpected %+v\nactual   %+v",
					tc.name, tc.category, tc.description, tc.input, tc.expected, got)
			}
		})
	}
}

// TestApplyEnrichmentNsidNoAllocs guards the hot path: stamping the nsid
// pair allocates nothing.
func TestApplyEnrichmentNsidNoAllocs(t *testing.T) {
	x := new(XTCP)
	m := map[uint64]int32{7: 0}
	x.nsidByInode.Store(&m)
	r := &xtcp_flat_record.XtcpFlatRecord{NetnsInode: 7}
	if n := testing.AllocsPerRun(100, func() { x.applyEnrichment(r) }); n != 0 {
		t.Errorf("applyEnrichment with an nsid snapshot: %v allocs/op, want 0", n)
	}
}

// go test ./pkg/xtcp/ -run '^$' -bench BenchmarkApplyEnrichmentNsid -benchmem
func BenchmarkApplyEnrichmentNsid(b *testing.B) {
	x := new(XTCP)
	m := map[uint64]int32{7: 0, 8: 1, 9: 2}
	x.nsidByInode.Store(&m)
	r := &xtcp_flat_record.XtcpFlatRecord{NetnsInode: 8}
	b.ReportAllocs()
	for b.Loop() {
		x.applyEnrichment(r)
	}
}
