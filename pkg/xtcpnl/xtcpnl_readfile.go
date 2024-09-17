package xtcpnl

import (
	"bufio"
	"errors"
	"os"
)

//import "github.com/randomizedcoder/xtcp2/xtcpnl" // netlink related functions

// const (
// 	debugLevel int = 11
// )

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

	size := stats.Size()
	bytes := make([]byte, size)

	bufr := bufio.NewReader(file)
	n, err := bufr.Read(bytes)

	if int64(n) != size {
		return nil, errors.New("readfile read n bytes miss match")
	}

	return bytes, err

}
