package main

import (
	"fmt"
	"strings"
	"testing"
)

// TestVdpaSpace pins the vdpa rows: every row runs serial in init_net
// (vdpa_nl_family is netnsok false), loads vdpa_sim_net and virtio_vdpa, captures generic
// netlink, and carries the expected outcome for its axes.
func TestVdpaSpace(t *testing.T) {
	rows, err := matrixRows(families["vdpa"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	byCmd := map[string]row{}
	for _, r := range rows {
		if !r.InitNS || !r.Serial {
			t.Errorf("%s: initns %t serial %t, want both", r.ID, r.InitNS, r.Serial)
		}
		if r.Proto != "generic" || strings.Join(r.Modules, ",") != "vdpa_sim_net,virtio_vdpa" {
			t.Errorf("%s: proto %q modules %v", r.ID, r.Proto, r.Modules)
		}
		byCmd[r.Cmd] = r
	}
	tests := []struct {
		description string
		cmd         string
		expected    string // rc refused-by errno
	}{
		{"positive: mgmtdev show", "vdpa mgmtdev show", "0 none "},
		{"positive: dev show, json", "vdpa -j dev show", "0 none "},
		{"positive: config show", "vdpa dev config show", "0 none "},
		{"boundary: vstats on queue 0", "vdpa dev vstats show vdpa0 qidx 0", "0 none "},
		{"positive: vstats on the control queue", "vdpa dev vstats show vdpa0 qidx 2", "0 none "},
		{"positive: a named device show", "vdpa dev show vdpa0", "0 none "},
		{"negative: one queue past the last", "vdpa dev vstats show vdpa0 qidx 3", "1 kernel EINVAL"},
		{"boundary: the last index the core passes", "vdpa dev vstats show vdpa0 qidx 32768", "1 kernel EINVAL"},
		{"boundary: one past the core's range", "vdpa dev vstats show vdpa0 qidx 32769", "1 kernel ERANGE"},
		{"corner: vstats with no queue index", "vdpa dev vstats show vdpa0", "1 kernel EINVAL"},
		{"negative: an unknown object", "vdpa nosuch show", "1 ack-then-fail "},
		{"negative: a device that does not exist", "vdpa dev show vdpanosuch", "1 kernel ENODEV"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byCmd[tc.cmd]
			if !ok {
				t.Fatalf("no row runs %q", tc.cmd)
			}
			got := fmt.Sprintf("%d %s %s", *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno)
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}
	if len(rows) != 20 {
		t.Errorf("%d rows, want 20", len(rows))
	}
}
