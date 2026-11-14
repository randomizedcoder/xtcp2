package io_uring

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// submitAndCollect submits everything queued and reaps exactly want CQEs,
// keyed by RequestID.
func submitAndCollect(t *testing.T, r *Ring, want int) map[uint32]Result {
	t.Helper()
	got := make(map[uint32]Result, want)
	if _, err := r.SubmitAndWait(uint32(want)); err != nil {
		t.Fatalf("SubmitAndWait: %v", err)
	}
	for len(got) < want {
		res := r.DrainBatch()
		if len(res) == 0 {
			more, err := r.WaitOne()
			if err != nil {
				t.Fatalf("WaitOne: %v", err)
			}
			res = more
		}
		for _, x := range res {
			got[x.RequestID] = x
		}
	}
	return got
}

// TestEnqueueReadAt covers the positioned read the sockstat gate batches.
func TestEnqueueReadAt(t *testing.T) {
	content := []byte("0123456789abcdef")
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		category    string
		description string
		off         uint64
		async       bool
		nilBuf      bool
		expected    []byte // nil: Enqueue must fail
	}{
		{name: "reads_file", category: "P", description: "temp file at offset 0", expected: content},
		{name: "reads_at_offset", category: "P", description: "offset 5 returns bytes from 5", off: 5, expected: content[5:]},
		{name: "async_flag", category: "P", description: "IOSQE_ASYNC still completes with the same bytes", async: true, expected: content},
		{name: "past_eof", category: "B", description: "offset at EOF returns 0 bytes", off: uint64(len(content)), expected: []byte{}},
		{name: "nil_buf", category: "N", description: "nil buffer is refused and nothing is queued", nilBuf: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestRing(t, 0)
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()

			var buf *[]byte
			if !tc.nilBuf {
				b := make([]byte, 64)
				buf = &b
			}
			id, err := r.EnqueueReadAt(int(f.Fd()), buf, tc.off, tc.async)
			if tc.expected == nil {
				if err == nil || r.SQReady() != 0 || r.InFlightLen() != 0 {
					t.Fatalf("%s: err=%v sqReady=%d inFlight=%d, expected an error and nothing queued",
						tc.description, err, r.SQReady(), r.InFlightLen())
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: EnqueueReadAt: %v", tc.description, err)
			}
			res := submitAndCollect(t, r, 1)[id]
			if res.Op != OpReadAt || res.Res < 0 {
				t.Fatalf("%s: op=%d res=%d", tc.description, res.Op, res.Res)
			}
			if got := (*res.Buf)[:res.Res]; !bytes.Equal(got, tc.expected) {
				t.Errorf("%s: got %q, expected %q", tc.description, got, tc.expected)
			}
			if r.InFlightLen() != 0 {
				t.Errorf("%s: %d still in flight", tc.description, r.InFlightLen())
			}
		})
	}
}

// TestEnqueueReadAtSQFull: queueing past the SQ size fails cleanly and
// every ID handed out still completes (none lost).
func TestEnqueueReadAtSQFull(t *testing.T) {
	r := newTestRing(t, 0)
	f, err := os.Open("/proc/self/stat")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	var ids []uint32
	var enqErr error
	for i := 0; i < 1024; i++ {
		b := make([]byte, 512)
		id, err := r.EnqueueReadAt(int(f.Fd()), &b, 0, true)
		if err != nil {
			enqErr = err
			break
		}
		ids = append(ids, id)
	}
	if enqErr == nil {
		t.Fatal("expected SQ full before 1024 SQEs")
	}
	got := submitAndCollect(t, r, len(ids))
	for _, id := range ids {
		if _, ok := got[id]; !ok {
			t.Errorf("request %d lost", id)
		}
	}
}

// TestEnqueueReadAtProcfsReread: the same procfs fd read twice at offset 0
// returns the whole file both times (the sockstat fd is reused each poll).
func TestEnqueueReadAtProcfsReread(t *testing.T) {
	r := newTestRing(t, 0)
	f, err := os.Open("/proc/self/net/sockstat")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	for i := range 2 {
		b := make([]byte, 4096)
		id, err := r.EnqueueReadAt(int(f.Fd()), &b, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		res := submitAndCollect(t, r, 1)[id]
		if res.Res <= 0 || !bytes.Contains((*res.Buf)[:res.Res], []byte("TCP: inuse")) {
			t.Fatalf("read %d: res=%d %q, expected the full sockstat file", i, res.Res, (*res.Buf)[:max(res.Res, 0)])
		}
	}
}

// TestSetIOWQMaxWorkers: limits set are read back as the previous limits
// on the next call (checks the __u32[2] packing).
func TestSetIOWQMaxWorkers(t *testing.T) {
	r := newTestRing(t, 0)
	if _, _, err := r.SetIOWQMaxWorkers(3, 5); err != nil {
		t.Fatalf("SetIOWQMaxWorkers: %v", err)
	}
	b, u, err := r.SetIOWQMaxWorkers(0, 0) // 0 = query, unchanged
	if err != nil {
		t.Fatal(err)
	}
	if b != 3 || u != 5 {
		t.Errorf("limits read back %d/%d, expected 3/5", b, u)
	}
}
