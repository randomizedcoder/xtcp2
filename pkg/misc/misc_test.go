package misc

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// Shared test fixture path.
const testdataNonCopyWriteText = "./testdata/non_copy_write_text"

// https://dave.cheney.net/2016/05/10/test-fixtures-in-go
// t.Log(wd)

// TestMaxLoopsOrForEver performs basic tests on misc.MaxLoopsOrForEver
func TestMaxLoopsOrForEver(t *testing.T) {
	var tests = []struct {
		pollingLoops uint64
		maxLoops     uint64
		expected     bool
	}{
		{10, 0, true}, // test 0
		{100, 0, true},
		{500, 0, true},
		{0, 1, true},
		{15, 16, true},
		{21, 22, true},
		{11, 10, false},
		{110, 100, false}, // test 7
		{10, 0, false},    // test 8 - negative
	}
	for i, test := range tests {

		if debugLevel > 100 {
			fmt.Println("test:\t", test)
		}

		if output := MaxLoopsOrForEver(test.pollingLoops, test.maxLoops); output != test.expected {
			if i < 8 {
				t.Errorf("Faied test:%d\tpollingLoops:%d\tmaxLoops:%d\texpected:%s\tresult:%s",
					i,
					test.pollingLoops,
					test.maxLoops,
					strconv.FormatBool(test.expected),
					strconv.FormatBool(output),
				)
			}
		}
	}
}

// TestscanFile tests xtcpstater.scanFile
// The test is performed using the slower bufio.NewReader.
// We're basically just comparing if the two (2) techniques reach the same result
// The scanFile bufio.NewScanner technique, verse the test bufio.NewReader taken from here
// https://stackoverflow.com/questions/8757389/reading-a-file-line-by-line-in-go
// Please note the benchmarking code below doesn't seem to find much difference
func TestScanFile(t *testing.T) {

	filename := testdataNonCopyWriteText
	scanFileLines := ScanFile(filename)
	readFileLines := ReadFile(filename)

	if !reflect.DeepEqual(scanFileLines, readFileLines) {

		t.Errorf("scanFile Test Failed: scanFileLines: %d readFileLines %d",
			len(scanFileLines),
			len(readFileLines))
	}
}

func BenchmarkScanFile(b *testing.B) {

	filename := testdataNonCopyWriteText
	for n := 0; n < b.N; n++ {
		scanFileLines := ScanFile(filename)
		if debugLevel > 100 {
			fmt.Println("len(scanFileLines):\t", len(scanFileLines))
		}
	}
}

func BenchmarkReadFile(b *testing.B) {

	filename := testdataNonCopyWriteText
	for n := 0; n < b.N; n++ {
		readFileLines := ReadFile(filename)
		if debugLevel > 100 {
			fmt.Println("len(readFileLines):\t", len(readFileLines))
		}
	}
}

func benchmarkFileN(n int, scanType string, b *testing.B) {

	// read in the file
	filename := testdataNonCopyWriteText
	scanFileLines := ScanFile(filename)

	// write out larger file (x100)
	filename = "./testdata/non_copy_write_text_new"
	// Create creates or truncates the named file. If the file already exists, it is truncated.
	f, err := os.Create(filename)
	if err != nil {
		fmt.Println(err)
		_ = f.Close()
		return
	}

	for i := 0; i < n; i++ {
		for _, line := range scanFileLines {
			if _, werr := fmt.Fprintln(f, line); werr != nil {
				fmt.Println(werr)
				return
			}
		}
	}
	err = f.Close()
	if err != nil {
		fmt.Println(err)
		return
	}

	// Benchmark timer RESET here!!!                       <--- Reset
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if scanType == "scan" {
			scanFileLines = ScanFile(filename)
			if debugLevel > 100 {
				fmt.Println("len(scanFileLines):\t", len(scanFileLines))
			}
		} else {
			readFileLines := ReadFile(filename)
			if debugLevel > 100 {
				fmt.Println("len(readFileLines):\t", len(readFileLines))
			}
		}
	}

	// Clean up the test file
	errRemove := os.Remove(filename)
	if errRemove != nil {
		fmt.Println(errRemove)
	}
}

func BenchmarkScanFile100(b *testing.B) {
	benchmarkFileN(100, "scan", b)
}

func BenchmarkReadFile100(b *testing.B) {
	benchmarkFileN(100, "read", b)
}

func BenchmarkScanFile1000(b *testing.B) {
	benchmarkFileN(1000, "scan", b)
}

func BenchmarkReadFile1000(b *testing.B) {
	benchmarkFileN(1000, "read", b)
}

// TestCheckFilePermissions writes a tempdir file with known permissions
// rather than relying on /bin/bash being 0755 (which fails on NixOS where
// the binary is a 0555 symlink into /nix/store).
func TestCheckFilePermissions(t *testing.T) {
	dir := t.TempDir()
	pTrue := filepath.Join(dir, "exec755")
	if err := os.WriteFile(pTrue, []byte("x"), 0o755); err != nil { //nolint:gosec // G306: 0o755 IS the test fixture mode
		t.Fatal(err)
	}
	pFalse := filepath.Join(dir, "exec600")
	if err := os.WriteFile(pFalse, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	var tests = []struct {
		filename    string
		permissions string
		expected    bool
	}{
		{pTrue, "0755", true},
		{pTrue, "0333", false},
		{pFalse, "0600", true},
		// Missing-file case is omitted: CheckFilePermissions calls log.Fatal
		// on os.Stat error, which would kill the test process.
	}
	for i, test := range tests {

		if debugLevel > 10 {
			fmt.Println(i, "\ttest:\t", test)
		}

		if output := CheckFilePermissions(test.filename, test.permissions); output != test.expected {
			t.Errorf("test %d: CheckFilePermissions(%q,%q)=%v, want %v", i, test.filename, test.permissions, output, test.expected)
		}
	}

}
