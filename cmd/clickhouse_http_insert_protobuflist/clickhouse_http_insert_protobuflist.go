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
// import "google.golang.org/protobuf/encoding/protowire"
// import "google.golang.org/protobuf/encoding/protodelim"

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
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + config build + primaryFunction. Extracted so
// tests can drive it with synthetic args without exiting.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clickhouse_http_insert_protobuflist", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("filename", "protoBytes.bin", "filename")
	valueStr := fs.String("values", "1", "values uints -> uint32, comma separated")
	envelope := fs.Bool("envelope", true, "envelope")
	db := fs.Bool("db", true, "db")
	connect := fs.String("connect", clickhouseConnectStringCst, "clickhouse database connect string")
	user := fs.String("user", clickhouseUserCst, "clickhosue user")
	pass := fs.String("pass", clickhousePasswordCst, "clickhosue pass")
	dump := fs.Bool("dump", false, "dump proto for debug")
	dumpFilename := fs.String("dumpFileName", "dump.bin", "dump file name")
	v := fs.Bool("v", false, "show version")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *v {
		fmt.Fprintf(stdout, "commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	}

	valueStrs := strings.Split(*valueStr, ",")
	values := make([]uint, 0, len(valueStrs))
	for _, str := range valueStrs {
		parsed, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			fmt.Fprintf(stderr, "Invalid value: %v\n", err)
			return 1
		}
		values = append(values, uint(parsed))
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

	return primaryFunction(ctx, c, stderr)
}

func primaryFunction(ctx context.Context, c config, stderr io.Writer) int {
	binaryData := prepareBinary(ctx, c)
	return fileOrDB(ctx, c, binaryData, stderr)
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
			errW := os.WriteFile(c.dumpFilename+".envelope", binaryData, 0600) // gosec G306
			if errW != nil {
				log.Fatalf("Failed to write protobuf envelope data: %v", errW)
			}
		}

	}

	return binaryData
}

func fileOrDB(ctx context.Context, c config, binaryData []byte, stderr io.Writer) int {
	if !c.db {
		if err := writeDataToFile(ctx, c.filename, binaryData); err != nil {
			fmt.Fprintln(stderr, "Error:", err)
			return 1
		}
		return 0
	}
	if err := insertIntoCH(ctx, c, binaryData); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

func writeDataToFile(ctx context.Context, filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0600) // 0600 permissions (rw-------) per gosec G306
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil

}

var ErrClickHouseHTTPPost = errors.New("clickhouse http post failed")

func insertIntoCH(ctx context.Context, c config, binaryData []byte) error {
	return insertIntoCHAt(ctx, http.DefaultClient, "http://"+c.connectStr, binaryData, c.envelope)
}

// insertIntoCHAt POSTs a binary protobuf payload to ClickHouse's HTTP
// endpoint. Extracted so tests can drive it against httptest.Server (the
// production wrapper picks the baseURL from config.connectStr).
//
// useEnvelope picks the ClickHouse format: ProtobufList for envelope mode
// (a single Envelope with a repeated Rows field), Protobuf for the
// length-prefixed per-row mode. The previous code hardwired FORMAT
// Protobuf regardless, so the default envelope=true path mailed an
// Envelope at a Record schema — ClickHouse rejected every insert.
func insertIntoCHAt(ctx context.Context, client *http.Client, baseURL string, binaryData []byte, useEnvelope bool) error {
	format := "Protobuf"
	if useEnvelope {
		format = "ProtobufList"
	}
	clickhouseURL := baseURL + "/?query=INSERT%20INTO%20clickhouse_protolist.clickhouse_protolist%20FORMAT%20" + format + "&format_schema=clickhouse_protolist.proto:clickhouse_protolist.v1.Record"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, clickhouseURL, bytes.NewReader(binaryData))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobufList")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			body = []byte("<body read failed>")
		}
		return fmt.Errorf("%w: status %d: %s", ErrClickHouseHTTPPost, resp.StatusCode, string(body))
	}
	return nil
}
