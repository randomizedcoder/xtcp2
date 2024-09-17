package main

import (
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

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", bind, port)) // this DOES bind to "::" because of "tcp"
	if err != nil {
		panic(err)
	}

	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			panic(err)
		}
		go func(conn net.Conn) {
			_, err := io.Copy(conn, conn)
			defer conn.Close()
			if err != nil {
				panic(err)
			}
		}(conn)
	}

}
