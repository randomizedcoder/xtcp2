package main

import (
	"flag"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"
)

const (
	startPort = 4000

	countCst = 10

	connectCst = "0.0.0.0"

	writeTimeoutCst = 100 * time.Millisecond
	readTimeoutCst  = 100 * time.Millisecond

	sleepCst = 2 * time.Second

	startsleepCst = 50 * time.Millisecond

	// had to increase this when creating 10k+ sockets
	dialTimeoutCst = 1000 * time.Millisecond

	dialRetryCst = 10

	readBufferSizeCst = 3000
	padSizeCst        = 2048
)

func main() {

	count := flag.Int("count", countCst, "count")
	connect := flag.String("connect", connectCst, "connect")
	sleep := flag.Duration("sleep", sleepCst, "sleep between writes")
	startsleep := flag.Duration("startsleep", startsleepCst, "sleep between client starts")
	wto := flag.Duration("wto", writeTimeoutCst, "write time out")
	rto := flag.Duration("rto", readTimeoutCst, "read time out")
	dialr := flag.Int("dialr", dialRetryCst, "dial retries")
	pads := flag.Int("pads", padSizeCst, "pad size")

	flag.Parse()

	var wg sync.WaitGroup

	for i := 0; i < *count; i++ {
		wg.Add(1)
		go client(&wg, *connect, startPort+i, *sleep, *wto, *rto, *dialr, *pads)
		time.Sleep(*startsleep)
	}

	wg.Wait()
}

func client(wg *sync.WaitGroup,
	bind string,
	port int,
	sleep time.Duration,
	wto time.Duration,
	rto time.Duration,
	dialr int,
	pads int,
) {

	defer wg.Done()

	msg := []byte("client" + fmt.Sprintf("%d", port))

	pad := make([]byte, pads)

	buf := slices.Concat(msg, pad)

	reply := make([]byte, readBufferSizeCst)

	var conn net.Conn
	timeout := dialTimeoutCst
	for r, success := 1, false; r < dialr && !success; r++ {

		var err error
		conn, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", bind, port), timeout)
		if err, ok := err.(net.Error); ok && err.Timeout() {
			timeout = dialTimeoutCst + (dialTimeoutCst * time.Duration(r))
			continue
		} else if err != nil {
			panic(err)
		}
		success = true
	}

	defer conn.Close()

	for i := 0; ; i++ {

		conn.SetWriteDeadline(time.Now().Add(wto))
		_, werr := conn.Write(buf)
		if nerr, ok := werr.(net.Error); ok && nerr.Timeout() {
			fmt.Println("write timeout")
			continue
		}

		conn.SetReadDeadline(time.Now().Add(rto))
		_, rerr := conn.Read(reply)
		if nerr, ok := rerr.(net.Error); ok && nerr.Timeout() {
			fmt.Println("read timeout")
			continue
		}

		fmt.Printf("received from server i:%d : [%s]\n", i, string(reply))

		time.Sleep(sleep)
	}

}
