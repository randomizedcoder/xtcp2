package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"google.golang.org/protobuf/encoding/protodelim"
)

// 	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
//import "google.golang.org/protobuf/encoding/protowire"
//import "google.golang.org/protobuf/encoding/protodelim"

const (
	clickhouseConnectStringCst = "127.0.0.1:8123"
	clickhouseUserCst          = "dave"
	clickhousePasswordCst      = "dave"
)

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string
)

type config struct {
	envelope     bool
	db           bool
	connectStr   string
	user         string
	pass         string
	filename     string
	values       []uint
	debugDump    bool
	dumpFilename string
}

func main() {

	filename := flag.String("filename", "protoBytes.bin", "filename")
	valueStr := flag.String("values", "1", "values uints -> uint32, comma seperated")

	envelope := flag.Bool("envelope", true, "envelope")
	db := flag.Bool("db", true, "db")

	connect := flag.String("connect", clickhouseConnectStringCst, "clickhouse database connect string")
	user := flag.String("user", clickhouseUserCst, "clickhosue user")
	pass := flag.String("pass", clickhousePasswordCst, "clickhosue pass")

	dump := flag.Bool("dump", false, "dump proto for debug")
	dumpFilename := flag.String("dumpFileName", "dump.bin", "dump file name")

	v := flag.Bool("v", false, "show version")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

	ctx := context.TODO()

	valueStrs := strings.Split(*valueStr, ",")
	var values []uint
	for _, str := range valueStrs {
		v, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			log.Fatalf("Invalid value: %v", err)
		}
		values = append(values, uint(v))
	}

	for i, v := range values {
		log.Printf("values: %d:%v", i, v)
	}

	c := config{
		filename:     *filename,
		values:       values,
		envelope:     *envelope,
		db:           *db,
		connectStr:   *connect,
		user:         *user,
		pass:         *pass,
		debugDump:    *dump,
		dumpFilename: *dumpFilename,
	}

	primaryFunction(ctx, c)
}

func primaryFunction(ctx context.Context, c config) {

	binaryData := prepareBinary(ctx, c)

	fileOrDB(ctx, c, binaryData)

}

func prepareBinary(ctx context.Context, c config) (binaryData []byte) {

	var b bytes.Buffer

	if !c.envelope {

		r := &clickhouse_protolist.Record{
			MyUint32: uint32(c.values[0]),
		}

		if _, err := protodelim.MarshalTo(&b, r); err != nil {
			log.Fatal("protodelim.MarshalTo(r):", err)
		}

		binaryData = b.Bytes()
		return binaryData
	}

	if c.envelope {
		envelope := &clickhouse_protolist.Envelope{}
		for _, v := range c.values {
			envelope.Rows = append(envelope.Rows,
				&clickhouse_protolist.Envelope_Record{
					MyUint32: uint32(v),
				},
			)
		}

		if _, err := protodelim.MarshalTo(&b, envelope); err != nil {
			log.Fatal("protodelim.MarshalTo(r):", err)
		}

		binaryData = b.Bytes()

		if c.debugDump {
			errW := os.WriteFile(c.dumpFilename+".envelope", binaryData, 0644)
			if errW != nil {
				log.Fatalf("Failed to write protobuf envelope data: %v", errW)
			}
		}

	}

	return binaryData
}

func fileOrDB(ctx context.Context, c config, binaryData []byte) {

	if !c.db {
		errW := writeDataToFile(ctx, c.filename, binaryData)
		if errW != nil {
			log.Println("Error:", errW)
		}
		os.Exit(0)
	}

	// if c.db {
	// 	log.Fatal("db not implemented, cos it's hard to insert protobuf with the clickhouse library")
	// }

	errDB := insertIntoCH(ctx, c, binaryData)
	if errDB != nil {
		log.Println("Error:", errDB)
	}

}

func writeDataToFile(ctx context.Context, filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0644) // 0644 permissions (rw-r--r--)
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil

}

var ErrClickHouseHTTPPost = errors.New("clickhouse http post failed")

func insertIntoCH(ctx context.Context, c config, binaryData []byte) error {

	clickhouseURL := "http://" + clickhouseConnectStringCst + "/?query=INSERT%20INTO%20clickhouse_protolist.clickhouse_protolist%20FORMAT%20Protobuf&format_schema=clickhouse_protolist.proto:clickhouse_protolist.v1.Record"

	log.Printf("clickhouseURL: %v", clickhouseURL)

	req, err := http.NewRequest("POST", clickhouseURL, bytes.NewReader(binaryData))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobufList")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read the response body for detailed error messages
		body, _ := io.ReadAll(resp.Body)
		log.Printf("ClickHouse HTTP Error (Status %d): %s", resp.StatusCode, string(body))

		return fmt.Errorf("%w: %v", ErrClickHouseHTTPPost, err)
	}

	fmt.Println("Data successfully inserted into ClickHouse!")

	return nil
}
