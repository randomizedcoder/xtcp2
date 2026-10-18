package nlparity

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The write corpora: one `ip link …`, `ip route …` or `ip addr …` write per
// directory, and the `ip addr flush` captures.
const (
	tdLinkWrite  = "../xtcpnl/testdata/7_1_4/link-write/write"
	tdRouteWrite = "../xtcpnl/testdata/7_1_4/link-write/route"
	tdAddrWrite  = "../xtcpnl/testdata/7_1_4/link-write/addr"
	tdAddrFlush  = "../xtcpnl/testdata/7_1_4/link-write/addr-flush"
)

func segmentAt(t *testing.T, dir, slug string) Segmentation {
	t.Helper()
	c, err := ParseRouteCaptureFile(filepath.Join(dir, slug, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	return SegmentCapture(c)
}

func segmentLinkWrite(t *testing.T, slug string) Segmentation {
	t.Helper()
	return segmentAt(t, tdLinkWrite, slug)
}

func segmentFlush(t *testing.T, slug string) Segmentation {
	t.Helper()
	return segmentAt(t, tdAddrFlush, slug)
}

// wireOf lays a message out as the bytes GOIP_WRITE_OUT would hold.
func wireOf(m Msg) []byte {
	b := make([]byte, xtcpnl.NlMsgHdrSizeCst+len(m.Body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b))) //nolint:gosec // a captured message
	binary.LittleEndian.PutUint16(b[4:6], m.Hdr.Type)
	binary.LittleEndian.PutUint16(b[6:8], m.Hdr.Flags)
	binary.LittleEndian.PutUint32(b[8:12], 1) // goip's own seq
	copy(b[xtcpnl.NlMsgHdrSizeCst:], m.Body)
	return b
}

// wireOfAll lays messages out back to back, as goip's flush file holds them.
func wireOfAll(msgs []Msg) []byte {
	b := make([]byte, 0, len(msgs)*xtcpnl.NlMsgHdrSizeCst)
	for _, m := range msgs {
		b = append(b, wireOf(m)...)
	}
	return b
}

// editAttr runs f over the value of m's attribute typ, in place.
func editAttr(t *testing.T, m Msg, typ uint16, f func(val []byte)) {
	t.Helper()
	_, attrs, _ := DecodeAttrs(m.Hdr.Type, m.Body)
	for _, a := range attrs {
		if a.BareType() == typ {
			f(a.Val)
			return
		}
	}
	t.Fatalf("%s has no attribute %d", MsgTypeName(m.Hdr.Type), typ)
}

func loci(ds []Divergence) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.Locus)
	}
	return out
}

// TestSplitWrites pins where the writes are: one contiguous run of the
// row's length, with what came before and after it kept apart.
//
// go test ./pkg/nlparity/ -run TestSplitWrites
func TestSplitWrites(t *testing.T) {
	type result struct {
		before, writes, after int
		datagrams             []int
		writeType             uint16
		err                   error
	}
	link := func(slug string) func(*testing.T) Segmentation {
		return func(t *testing.T) Segmentation { return segmentLinkWrite(t, slug) }
	}
	flush := func(slug string) func(*testing.T) Segmentation {
		return func(t *testing.T) Segmentation { return segmentFlush(t, slug) }
	}
	twoWrites := func(t *testing.T) Segmentation {
		s := segmentLinkWrite(t, "add_dummy")
		s.Txns = append(s.Txns, s.Txns[0])
		return s
	}
	readOnly := func(t *testing.T) Segmentation {
		s := segmentLinkWrite(t, "enslave_down")
		s.Txns = s.Txns[:2]
		return s
	}
	getAmongDels := func(t *testing.T) Segmentation {
		s := segmentFlush(t, "flush_dev")
		s.Txns = slices.Insert(s.Txns, 4, s.Txns[1])
		return s
	}
	tests := []struct {
		description string
		seg         func(t *testing.T) Segmentation
		n           int
		expected    result
	}{
		{description: "positive: flush_dev is lookup and dump, four DELs, then the round-2 dump",
			seg: flush("flush_dev"), n: 4,
			expected: result{before: 2, writes: 4, after: 1, datagrams: []int{4}, writeType: unix.RTM_DELADDR}},
		{description: "boundary: flush_many's batch went out as three datagrams",
			seg: flush("flush_many"), n: 120,
			expected: result{before: 2, writes: 120, after: 1, datagrams: []int{47, 47, 26}, writeType: unix.RTM_DELADDR}},
		{description: "corner: a primary flush stops after one round, so nothing comes after",
			seg: flush("flush_primary"), n: 1,
			expected: result{before: 2, writes: 1, datagrams: []int{1}, writeType: unix.RTM_DELADDR}},
		{description: "corner: one DEL followed by a round-2 dump",
			seg: flush("flush_scope_link"), n: 1,
			expected: result{before: 2, writes: 1, after: 1, datagrams: []int{1}, writeType: unix.RTM_DELADDR}},
		{description: "positive: add resolves nothing, so the write is the only transaction",
			seg: link("add_dummy"), n: 1,
			expected: result{writes: 1, datagrams: []int{1}, writeType: unix.RTM_NEWLINK}},
		{description: "positive: enslave resolves master then dev, then writes",
			seg: link("enslave_down"), n: 1,
			expected: result{before: 2, writes: 1, datagrams: []int{1}, writeType: unix.RTM_NEWLINK}},
		{description: "positive: a refused write is still the last transaction",
			seg: link("enslave_up"), n: 1,
			expected: result{before: 2, writes: 1, datagrams: []int{1}, writeType: unix.RTM_NEWLINK}},
		{description: "positive: delete is an RTM_DELLINK after one lookup",
			seg: link("del_bond"), n: 1,
			expected: result{before: 1, writes: 1, datagrams: []int{1}, writeType: unix.RTM_DELLINK}},
		{description: "negative: nothing to flush sends no write", seg: flush("flush_nothing"), n: 1,
			expected: result{err: ErrNoWriteTxn}},
		{description: "negative: the run is not the row's length", seg: flush("flush_dev"), n: 3,
			expected: result{err: ErrWriteCount}},
		{description: "negative: a GET between two DELs splits the run", seg: getAmongDels, n: 4,
			expected: result{err: ErrNoWriteTxn}},
		{description: "negative: lookups only, the write never sent", seg: readOnly, n: 1,
			expected: result{err: ErrNoWriteTxn}},
		{description: "negative: an empty capture",
			seg: func(*testing.T) Segmentation { return Segmentation{} }, n: 1,
			expected: result{err: ErrNoWriteTxn}},
		{description: "corner: two writes where the row expects one", seg: twoWrites, n: 1,
			expected: result{err: ErrWriteCount}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			w, err := SplitWrites(tc.seg(t), tc.n)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if err != nil {
				return
			}
			got := result{before: len(w.Before.Txns), writes: len(w.Writes), after: len(w.After),
				datagrams: w.Datagrams, writeType: w.Writes[0].Hdr.Type}
			if got.before != tc.expected.before || got.writes != tc.expected.writes ||
				got.after != tc.expected.after || !slices.Equal(got.datagrams, tc.expected.datagrams) ||
				got.writeType != tc.expected.writeType {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestParseWriteRequests covers the GOIP_WRITE_OUT file's shape: one write
// request, or a flush's batch of them back to back.
//
// go test ./pkg/nlparity/ -run TestParseWriteRequests
func TestParseWriteRequests(t *testing.T) {
	one, err := SplitWrites(segmentLinkWrite(t, "add_dummy"), 1)
	if err != nil {
		t.Fatal(err)
	}
	dels, err := SplitWrites(segmentFlush(t, "flush_dev"), 4)
	if err != nil {
		t.Fatal(err)
	}
	good := wireOf(one.Writes[0])
	batch := wireOfAll(dels.Writes)
	second := len(wireOf(dels.Writes[0]))
	mutate := func(src []byte, f func(b []byte) []byte) []byte { return f(append([]byte(nil), src...)) }
	tests := []struct {
		description string
		file        []byte
		expected    int
		err         error
	}{
		{description: "positive: one RTM_NEWLINK request", file: good, expected: 1},
		{description: "positive: flush_dev's four DELs back to back", file: batch, expected: 4},
		{description: "boundary: a header and nothing else is short of nothing but a body",
			file: mutate(good, func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b, xtcpnl.NlMsgHdrSizeCst)
				return b[:xtcpnl.NlMsgHdrSizeCst]
			}), expected: 1},
		{description: "negative: fewer bytes than a header", file: good[:xtcpnl.NlMsgHdrSizeCst-1],
			err: ErrBadWriteFile},
		{description: "negative: trailing bytes after the message",
			file: append(append([]byte(nil), good...), 0, 0, 0, 0), err: ErrBadWriteFile},
		{description: "negative: NLM_F_REQUEST missing",
			file: mutate(good, func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[6:8], unix.NLM_F_ACK)
				return b
			}), err: ErrBadWriteFile},
		{description: "negative: the second message of a batch lacks NLM_F_REQUEST",
			file: mutate(batch, func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[second+6:second+8], 0)
				return b
			}), err: ErrBadWriteFile},
		{description: "negative: a GET is not a write",
			file: mutate(good, func(b []byte) []byte {
				binary.LittleEndian.PutUint16(b[4:6], unix.RTM_GETLINK)
				return b
			}), err: ErrBadWriteFile},
		{description: "negative: nlmsg_len runs past the end of the file",
			file: mutate(batch, func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b[second:second+4], 0xffff)
				return b
			}), err: ErrBadWriteFile},
		{description: "negative: empty", file: nil, err: ErrBadWriteFile},
		{description: "corner: an nlmsg_len below a header is refused rather than looped on",
			file: mutate(good, func(b []byte) []byte {
				binary.LittleEndian.PutUint32(b, 0)
				return b
			}), err: ErrBadWriteFile},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs, err := ParseWriteRequests(tc.file)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if len(msgs) != tc.expected {
				t.Fatalf("%d messages, want %d", len(msgs), tc.expected)
			}
		})
	}
}

// goipFlush builds the goip side of a flush row from ip's capture: the same
// transactions without the DELs, with every round after the first answered
// by round 1's dump, as goip's kernel answers a round it never deleted
// anything in; and the DELs as goip's file holds them, re-read from their
// own bytes so an edit cannot reach ip's side.
func goipFlush(t *testing.T, ref Segmentation, n int) (Segmentation, []Msg) {
	t.Helper()
	w, err := SplitWrites(ref, n)
	if err != nil {
		t.Fatal(err)
	}
	sub := w.Before
	sub.Txns = append([]Txn(nil), w.Before.Txns...)
	round1 := w.Before.Txns[len(w.Before.Txns)-1]
	for _, a := range w.After {
		a.Replies, a.State = round1.Replies, round1.State
		sub.Txns = append(sub.Txns, a)
	}
	writes, err := ParseWriteRequests(wireOfAll(w.Writes))
	if err != nil {
		t.Fatal(err)
	}
	return sub, writes
}

// TestCompareWritesBatch is the comparator over flush captures (table 8's
// comparator rows), with the goip side built by goipFlush and then one thing
// changed.
//
// go test ./pkg/nlparity/ -run TestCompareWritesBatch
func TestCompareWritesBatch(t *testing.T) {
	type result struct {
		err      error
		loci     []string
		refTxns  int
		subTxns  int
		hygienic bool
	}
	tests := []struct {
		description string
		slug        string
		n           int
		compareN    int // 0 means n
		edit        func(t *testing.T, ref, sub *Segmentation, writes *[]Msg)
		expected    result
	}{
		{description: "positive: one round, the batch equal message by message", slug: "flush_dev", n: 4,
			expected: result{refTxns: 7, subTxns: 7, hygienic: true}},
		{description: "boundary: a batch over 3584 bytes, in three datagrams", slug: "flush_many", n: 120,
			expected: result{refTxns: 123, subTxns: 123, hygienic: true}},
		{description: "boundary: the round-2 reply is excluded though goip's holds the whole dump",
			slug: "flush_scope_link", n: 1,
			expected: result{refTxns: 4, subTxns: 4, hygienic: true}},
		{description: "boundary: the round-2 request is still compared", slug: "flush_dev", n: 4,
			edit: func(_ *testing.T, _, sub *Segmentation, _ *[]Msg) {
				sub.Txns[len(sub.Txns)-1].Request.Hdr.Flags ^= unix.NLM_F_ECHO
			},
			expected: result{refTxns: 7, subTxns: 7, hygienic: true,
				loci: []string{"request:RTM_GETADDR:header.flags:dump"}}},
		{description: "negative: a DEL missing from goip's file", slug: "flush_dev", n: 4,
			edit:     func(_ *testing.T, _, _ *Segmentation, writes *[]Msg) { *writes = (*writes)[:3] },
			expected: result{refTxns: 7, subTxns: 6, hygienic: true, loci: []string{"write-count"}}},
		{description: "negative: a DEL deleting a different address", slug: "flush_dev", n: 4,
			edit: func(t *testing.T, _, _ *Segmentation, writes *[]Msg) {
				editAttr(t, (*writes)[1], unix.IFA_LOCAL, func(v []byte) { v[len(v)-1] ^= 0xff })
			},
			expected: result{refTxns: 7, subTxns: 7, hygienic: true,
				loci: []string{"request:RTM_DELADDR:IFA_LOCAL:write"}}},
		{description: "negative: goip putting a DEL on the wire is an error", slug: "flush_dev", n: 4,
			edit:     func(_ *testing.T, ref, sub *Segmentation, _ *[]Msg) { *sub = *ref },
			expected: result{err: ErrGoipSentWrite, refTxns: 7, subTxns: 11, hygienic: false}},
		{description: "negative: ip's capture holds a different count than the row", slug: "flush_dev",
			n: 4, compareN: 3,
			expected: result{err: ErrWriteCount, refTxns: 7, subTxns: 7, hygienic: true}},
		{description: "corner: a primary flush is one round on both sides", slug: "flush_primary", n: 1,
			expected: result{refTxns: 3, subTxns: 3, hygienic: true}},
		{description: "corner: cacheinfo stamps differ in goip's copies", slug: "flush_dev", n: 4,
			edit: func(t *testing.T, _, _ *Segmentation, writes *[]Msg) {
				editAttr(t, (*writes)[0], unix.IFA_CACHEINFO, func(v []byte) { v[8]++; v[12]++ })
			},
			expected: result{refTxns: 7, subTxns: 7, hygienic: true}},
		{description: "corner: but a lifetime differing is a finding", slug: "flush_dev", n: 4,
			edit: func(t *testing.T, _, _ *Segmentation, writes *[]Msg) {
				editAttr(t, (*writes)[0], unix.IFA_CACHEINFO, func(v []byte) { v[0]-- })
			},
			expected: result{refTxns: 7, subTxns: 7, hygienic: true,
				loci: []string{"request:RTM_DELADDR:IFA_CACHEINFO:write"}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ref := segmentFlush(t, tc.slug)
			sub, writes := goipFlush(t, ref, tc.n)
			if tc.edit != nil {
				tc.edit(t, &ref, &sub, &writes)
			}
			n := tc.n
			if tc.compareN != 0 {
				n = tc.compareN
			}
			r := CompareWrites("c", ref, sub, writes, n, nil, nil)
			if !errors.Is(r.Err, tc.expected.err) {
				t.Fatalf("Err = %v, want %v", r.Err, tc.expected.err)
			}
			got := result{err: tc.expected.err, loci: loci(r.Findings), refTxns: r.RefTxns,
				subTxns: r.SubTxns, hygienic: len(r.Hygiene) == 0}
			if !slices.Equal(got.loci, tc.expected.loci) || got.refTxns != tc.expected.refTxns ||
				got.subTxns != tc.expected.subTxns || got.hygienic != tc.expected.hygienic {
				t.Fatalf("got %+v (hygiene %v), want %+v", got, r.Hygiene, tc.expected)
			}
		})
	}
}

// TestCompareWritesSingle is the regression for every single-write row: over
// each slug of the link, route and address write corpora, SplitWrites with
// n=1 cuts where SplitWrite cut (the last transaction, and the only write),
// and CompareWrites reports what CompareWrite reported, computed here the
// way CompareWrite computed it.
//
// go test ./pkg/nlparity/ -run TestCompareWritesSingle
func TestCompareWritesSingle(t *testing.T) {
	type slug struct{ dir, name string }
	var slugs []slug
	for _, dir := range []string{tdLinkWrite, tdRouteWrite, tdAddrWrite} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			slugs = append(slugs, slug{dir, e.Name()})
		}
	}
	// oldSplit is SplitWrite as it was.
	oldSplit := func(s Segmentation) (Segmentation, Txn, bool) {
		n := len(s.Txns)
		if n == 0 || !xtcpnl.IsAdmittedWriteType(s.Txns[n-1].Request.Hdr.Type) {
			return Segmentation{}, Txn{}, false
		}
		for i := range n - 1 {
			if xtcpnl.IsAdmittedWriteType(s.Txns[i].Request.Hdr.Type) {
				return Segmentation{}, Txn{}, false
			}
		}
		lookups := s
		lookups.Txns = s.Txns[:n-1]
		return lookups, s.Txns[n-1], true
	}

	var silent []string
	t.Run("positive: SplitWrites(s, 1) cuts where SplitWrite cut", func(t *testing.T) {
		for _, sl := range slugs {
			s := segmentAt(t, sl.dir, sl.name)
			lookups, write, ok := oldSplit(s)
			w, err := SplitWrites(s, 1)
			if !ok {
				silent = append(silent, filepath.Base(sl.dir)+"/"+sl.name)
				if err == nil {
					t.Errorf("%s: SplitWrite refused it and SplitWrites did not", sl.name)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s: %v", sl.name, err)
				continue
			}
			if len(w.Before.Txns) != len(lookups.Txns) || len(w.After) != 0 ||
				!slices.Equal(w.Datagrams, []int{1}) || len(w.Writes) != 1 ||
				w.Writes[0].Hdr != write.Request.Hdr || string(w.Writes[0].Body) != string(write.Request.Body) {
				t.Errorf("%s: before %d after %d datagrams %v; want %d, 0, [1] and the last request",
					sl.name, len(w.Before.Txns), len(w.After), w.Datagrams, len(lookups.Txns))
			}
		}
	})

	t.Run("positive: a clean goip side compares as CompareWrite compared it", func(t *testing.T) {
		for _, sl := range slugs {
			ref := segmentAt(t, sl.dir, sl.name)
			lookups, write, ok := oldSplit(ref)
			if !ok {
				continue
			}
			subWrites, err := ParseWriteRequests(wireOf(write.Request))
			if err != nil {
				t.Fatal(err)
			}
			r := CompareWrites("c", ref, lookups, subWrites, 1, nil, nil)
			hygiene := append(HygieneDiff("c", "ip", ref), HygieneDiff("c", "goip", lookups)...)
			if r.Err != nil || len(r.Findings) != 0 || r.RefTxns != len(ref.Txns) ||
				r.SubTxns != len(ref.Txns) || !slices.Equal(loci(r.Hygiene), loci(hygiene)) {
				t.Errorf("%s: Err %v, findings %v, txns %d/%d, hygiene %v; want none, none, %d/%d, %v",
					sl.name, r.Err, r.Findings, r.RefTxns, r.SubTxns, loci(r.Hygiene),
					len(ref.Txns), len(ref.Txns), loci(hygiene))
			}
		}
	})

	t.Run("positive: a goip side with other write flags finds what DiffWrite found", func(t *testing.T) {
		for _, sl := range slugs {
			ref := segmentAt(t, sl.dir, sl.name)
			lookups, write, ok := oldSplit(ref)
			if !ok {
				continue
			}
			file := wireOf(write.Request)
			binary.LittleEndian.PutUint16(file[6:8], write.Request.Hdr.Flags^unix.NLM_F_EXCL)
			subWrites, err := ParseWriteRequests(file)
			if err != nil {
				t.Fatal(err)
			}
			sub := lookups
			sub.Txns = lookups.Txns[:len(lookups.Txns)/2]
			want := append(Diff("c", lookups, sub), diffRequest("c", len(lookups.Txns), write.Request, subWrites[0])...)
			r := CompareWrites("c", ref, sub, subWrites, 1, nil, nil)
			var gotS, wantS []string
			for _, d := range r.Findings {
				gotS = append(gotS, d.String())
			}
			for _, d := range want {
				wantS = append(wantS, d.String())
			}
			if !slices.Equal(gotS, wantS) || len(wantS) == 0 {
				t.Errorf("%s: findings %q, want %q", sl.name, gotS, wantS)
			}
		}
	})

	t.Run("negative: the slugs that send no write are refused as before", func(t *testing.T) {
		want := []string{"route/nodev", "addr/autojoin_unicast", "addr/brd_plus_v6", "addr/no_dev_keyword",
			"addr/nodev", "addr/valid_zero"}
		if !slices.Equal(silent, want) {
			t.Errorf("refused slugs %q, want %q", silent, want)
		}
	})

	t.Run("boundary: the rest are the 74 single-write rows of the matrix", func(t *testing.T) {
		if got := len(slugs) - len(silent); got != 74 {
			t.Errorf("%d slugs send one write, want 74", got)
		}
	})
}

// TestCompareWrites runs the single-write comparison over link captures,
// with the goip side built from the same capture: its lookups are ip's
// lookups and its write file is ip's write request, then one thing is
// changed.
//
// go test ./pkg/nlparity/ -run 'TestCompareWrites$'
func TestCompareWrites(t *testing.T) {
	type side struct {
		sub   Segmentation
		write []byte
	}
	goipOf := func(t *testing.T, slug string, edit func(*side)) (Segmentation, side) {
		ref := segmentLinkWrite(t, slug)
		w, err := SplitWrites(ref, 1)
		if err != nil {
			t.Fatal(err)
		}
		s := side{sub: w.Before, write: wireOf(w.Writes[0])}
		if edit != nil {
			edit(&s)
		}
		return ref, s
	}
	openWrite := func(t *testing.T, ref *Segmentation) {
		t.Helper()
		last := len(ref.Txns) - 1
		ref.Txns = slices.Clone(ref.Txns)
		ref.Txns[last].Replies, ref.Txns[last].State = nil, TxnOpen
	}
	type result struct {
		err      error
		loci     []string
		refTxns  int
		subTxns  int
		hygienic bool
	}
	tests := []struct {
		description string
		slug        string
		edit        func(*side)
		openRef     bool // strip the write's ACK from ip's capture
		control     bool // use a control diff that saw the same change
		expected    result
	}{
		{description: "positive: identical lookups and write", slug: "enslave_down",
			expected: result{refTxns: 3, subTxns: 3, hygienic: true}},
		{description: "positive: add has no lookups on either side", slug: "add_bond_8023ad",
			expected: result{refTxns: 1, subTxns: 1, hygienic: true}},
		{description: "positive: a refused write compares on its request", slug: "enslave_up",
			expected: result{refTxns: 3, subTxns: 3, hygienic: true}},
		{description: "negative: different write flags are a write-role L2 finding", slug: "set_up",
			edit: func(s *side) { binary.LittleEndian.PutUint16(s.write[6:8], unix.NLM_F_REQUEST) },
			expected: result{refTxns: 2, subTxns: 2, hygienic: true,
				loci: []string{"request:RTM_NEWLINK:header.flags:write"}}},
		{description: "negative: a missing lookup is a transaction-count finding", slug: "enslave_down",
			edit:     func(s *side) { s.sub.Txns = s.sub.Txns[:1] },
			expected: result{refTxns: 3, subTxns: 2, hygienic: true, loci: []string{"txn-count"}}},
		{description: "negative: goip putting a write on the wire is an error", slug: "set_up",
			edit:     func(s *side) { s.sub = segmentLinkWrite(t, "set_up") },
			expected: result{err: ErrGoipSentWrite, refTxns: 2, subTxns: 3, hygienic: true}},
		{description: "negative: a write that asked for an ACK and got none is still open", slug: "enslave_down",
			openRef:  true,
			expected: result{refTxns: 3, subTxns: 3, hygienic: false}},
		{description: "corner: a change the control also saw is subtracted", slug: "set_up",
			edit:     func(s *side) { binary.LittleEndian.PutUint16(s.write[6:8], unix.NLM_F_REQUEST) },
			control:  true,
			expected: result{refTxns: 2, subTxns: 2, hygienic: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ref, s := goipOf(t, tc.slug, tc.edit)
			if tc.openRef {
				openWrite(t, &ref)
			}
			subWrites, err := ParseWriteRequests(s.write)
			if err != nil {
				t.Fatal(err)
			}
			var control []Divergence
			if tc.control {
				w, serr := SplitWrites(ref, 1)
				if serr != nil {
					t.Fatal(serr)
				}
				control = DiffWrites("c", w, s.sub, subWrites)
			}
			r := CompareWrites("c", ref, s.sub, subWrites, 1, control, nil)
			if !errors.Is(r.Err, tc.expected.err) {
				t.Fatalf("Err = %v, want %v", r.Err, tc.expected.err)
			}
			got := result{err: tc.expected.err, loci: loci(r.Findings), refTxns: r.RefTxns,
				subTxns: r.SubTxns, hygienic: len(r.Hygiene) == 0}
			if !slices.Equal(got.loci, tc.expected.loci) || got.refTxns != tc.expected.refTxns ||
				got.subTxns != tc.expected.subTxns || got.hygienic != tc.expected.hygienic {
				t.Fatalf("got %+v, want %+v", got, tc.expected)
			}
			if tc.openRef && !slices.Contains(loci(r.Hygiene), "hygiene:ip:open") {
				t.Fatalf("hygiene %v, want hygiene:ip:open", loci(r.Hygiene))
			}
		})
	}
}

// TestWritesControl is D_control between two `ip` captures of a write row.
// Only L1 and L2 findings are counted: two captures of fresh namespaces
// differ in reply state (a new bond's random MAC, cacheinfo times), and
// subtracting that from the test diff is what the control is for.
//
// go test ./pkg/nlparity/ -run TestWritesControl
func TestWritesControl(t *testing.T) {
	type result struct {
		findings int
		err      error
	}
	tests := []struct {
		description string
		dir         string
		a, b        string
		n           int
		expected    result
	}{
		{description: "positive: a capture against itself is clean", dir: tdLinkWrite,
			a: "enslave_down", b: "enslave_down", n: 1},
		{description: "positive: up and down requests differ only in ifi_flags", dir: tdLinkWrite,
			a: "set_up", b: "set_down", n: 1, expected: result{findings: 1}},
		{description: "positive: two flush namespaces send the same batch", dir: tdAddrFlush,
			a: "flush_dev", b: "flush_dev", n: 4},
		{description: "corner: the promotion setting does not change a primary flush's requests",
			dir: tdAddrFlush, a: "flush_primary", b: "flush_primary_flipped", n: 1},
		{description: "negative: ip_b with no write", dir: tdLinkWrite, a: "set_up", b: "", n: 1,
			expected: result{err: ErrNoWriteTxn}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			a := segmentAt(t, tc.dir, tc.a)
			var b Segmentation
			if tc.b != "" {
				b = segmentAt(t, tc.dir, tc.b)
			}
			d, err := WritesControl("c", a, b, tc.n)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			n := 0
			for _, x := range d {
				if x.Level == LevelRequest || x.Level == LevelTxnCount {
					n++
				}
			}
			if got := (result{findings: n, err: tc.expected.err}); got != tc.expected {
				t.Fatalf("got %+v (%v), want %+v", got, d, tc.expected)
			}
		})
	}
}
