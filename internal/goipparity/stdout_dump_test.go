package goipparity

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// routeSaveFile is route_save's committed ip.save: the magic and table 100's
// two routes, 60 and 52 bytes, each NLM_F_MULTI and NLM_F_DUMP_FILTERED.
func routeSaveFile(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "pkg", "xtcpnl", "testdata", "7_1_4", "link-write", "rtnl", "route_save", "ip.save"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// reSeqPid is a save file with every record's seq and pid replaced, as a
// second run's file differs from the first's.
func reSeqPid(t *testing.T, file []byte) []byte {
	t.Helper()
	out := bytes.Clone(file)
	for p := dumpMagicLenCst; p+nlmsgHdrLenCst <= len(out); {
		binary.LittleEndian.PutUint32(out[p+8:p+12], 0xdeadbeef)
		binary.LittleEndian.PutUint32(out[p+12:p+16], 0x0a0a0a0a)
		p += int(binary.LittleEndian.Uint32(out[p : p+4]))
	}
	return out
}

// TestDumpFileRecords is the recognition and the elements.
//
// go test ./internal/goipparity/ -run TestDumpFileRecords
func TestDumpFileRecords(t *testing.T) {
	save := routeSaveFile(t)
	tests := []struct {
		description string
		in          []byte
		ok          bool
		expected    []string // element prefixes, in order
	}{
		{"positive: route_save's file is the magic and two records", save, true,
			[]string{"magic:45311224", "0:type=24:flags=0x22:", "1:type=24:flags=0x22:"}},
		{"positive: seq and pid are not in the elements", reSeqPid(t, save), true,
			[]string{"magic:45311224", "0:type=24:flags=0x22:", "1:type=24:flags=0x22:"}},
		{"positive: address save's magic is a dump file too", []byte{0x22, 0x12, 0x36, 0x47}, true,
			[]string{"magic:47361222"}},
		{"positive: rule save's magic is a dump file too", []byte{0x86, 0x69, 0x70, 0x71}, true,
			[]string{"magic:71706986"}},
		{"boundary: the magic alone has no records", save[:4], true, []string{"magic:45311224"}},
		{"corner: a short tail is one fragment element", save[:4+60+10], true,
			[]string{"magic:45311224", "0:type=24:", "1:fragment:"}},
		{"corner: a record claiming fewer bytes than a header is a fragment", append(bytes.Clone(save[:4]), make([]byte, 16)...), true,
			[]string{"magic:45311224", "0:fragment:"}},
		{"negative: text is not a dump file", []byte("198.51.100.0/24 via 192.0.2.2 dev d0\n"), false, nil},
		{"negative: three bytes of magic are not", save[:3], false, nil},
		{"negative: empty output is not", nil, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := dumpFileRecords(string(tc.in))
			if ok != tc.ok || len(got) != len(tc.expected) {
				t.Fatalf("ok %v, %d elements %q; want ok %v, %d", ok, len(got), got, tc.ok, len(tc.expected))
			}
			for i := range got {
				if !strings.HasPrefix(got[i], tc.expected[i]) {
					t.Errorf("element %d = %q, want prefix %q", i, got[i], tc.expected[i])
				}
			}
		})
	}
}

// TestCompareStdoutDumpFile is CompareStdout over two save files.
//
// go test ./internal/goipparity/ -run TestCompareStdoutDumpFile
func TestCompareStdoutDumpFile(t *testing.T) {
	save := routeSaveFile(t)
	altered := bytes.Clone(save)
	altered[len(altered)-1] ^= 1 // the second route's RTA_OIF
	swapped := slices.Concat(save[:4], save[64:116], save[4:64])
	tests := []struct {
		description string
		ref, sub    []byte
		expected    []string // loci
	}{
		{"positive: another run's seq and pid are not a finding", save, reSeqPid(t, save), nil},
		{"negative: a dropped record is a dumprecords finding", save, save[:64], []string{"stdout:dumprecords"}},
		{"negative: a changed attribute is a dumprecords finding", save, altered, []string{"stdout:dumprecords"}},
		{"negative: the records out of order are a finding", save, swapped, []string{"stdout:dumprecords"}},
		{"negative: the magic alone against two records", save, save[:4], []string{"stdout:dumprecords"}},
		{"corner: text where ip wrote a file is every facet the text fills, and dumprecords", save,
			[]byte("203.0.113.0/24 dev d0 table 100 scope link \n"),
			[]string{"stdout:cidrs", "stdout:devnames", "stdout:dumprecords", "stdout:keyword:scope", "stdout:keyword:table", "stdout:lines"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []string
			for _, d := range CompareStdout("route save table 100", string(tc.ref), string(tc.sub)) {
				got = append(got, d.Locus())
				if d.Facet == FacetDumpRecords && d.Class != nlparity.DivergencePresence {
					t.Errorf("dumprecords class %v, want presence", d.Class)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("loci %q, want %q", got, tc.expected)
			}
		})
	}
}
