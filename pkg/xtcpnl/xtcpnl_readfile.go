package xtcpnl

import (
	"io"
	"os"
)

// Readfile reads the entire file at filename into memory.
//
// The earlier implementation built a bufio.Reader and called .Read(buf)
// exactly ONCE, then compared n to file size. bufio.Reader.Read is
// documented as "at most one Read on the underlying Reader" — for
// large files (or even smaller files under filesystem stress) the
// single underlying read can return a short count, and Readfile
// would error spuriously. Test fixtures stayed under 4 KB so the bug
// never tripped, but Readfile's name implies a "give me the whole
// file" contract that the bufio approach can't honour.
//
// io.ReadFull loops over the underlying Read until the buffer is full
// or an error / EOF is hit, which is what we actually want.
func Readfile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	stats, statsErr := file.Stat()
	if statsErr != nil {
		return nil, statsErr
	}

	buf := make([]byte, stats.Size())
	if _, err := io.ReadFull(file, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
