package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"sync"
)

const (
	startPort = 4000

	countCst = 10

	bindCst = "0.0.0.0"
)

func main() {

	count := flag.Int("count", countCst, "count")
	bind := flag.String("bind", bindCst, "bind")

	flag.Parse()

	var wg sync.WaitGroup

	for i := 0; i < *count; i++ {
		wg.Add(1)
		go server(&wg, *bind, startPort+i)
	}

	wg.Wait()
}

func server(wg *sync.WaitGroup, bind string, port int) {

	defer wg.Done()

	lc := net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", fmt.Sprintf("%s:%d", bind, port)) // this DOES bind to "::" because of "tcp"
	if err != nil {
		panic(err)
	}

	defer func() { _ = ln.Close() }() //nolint:errcheck // demo server teardown

	for {
		conn, aerr := ln.Accept()
		if aerr != nil {
			panic(aerr)
		}
		go func(conn net.Conn) {
			_, cerr := io.Copy(conn, conn)
			defer func() { _ = conn.Close() }() //nolint:errcheck // demo server teardown
			if cerr != nil {
				panic(cerr)
			}
		}(conn)
	}

}
