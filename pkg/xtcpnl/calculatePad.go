package xtcpnl

const (
	byteAlignmentCst = 4
)

// These are x2 padding implmentations
// FourByteAlignPadding is branchless, so it's faster

// goos: linux
// goarch: amd64
// pkg: github.com/randomizedcoder/xtcp2/pkg/xtcp
// cpu: Intel(R) Core(TM) i9-10885H CPU @ 2.40GHz
// BenchmarkPadCalculatePadding-16        	453125890	        2.730 ns/op
// BenchmarkPadFourByteAlignPadding-16    	703146751	        1.552 ns/op
// PASS
// ok  	github.com/randomizedcoder/xtcp2/pkg/xtcp	2.776s

func FourByteAlignPadding(size int) int {
	return (4 - (size & 3)) & 3
}

func CalculatePadding(size int) (padSize int) {

	if size == 0 {
		return padSize
	}

	rem := size % byteAlignmentCst
	if rem == 0 {
		return padSize
	}
	padSize = byteAlignmentCst - rem

	return padSize
}
