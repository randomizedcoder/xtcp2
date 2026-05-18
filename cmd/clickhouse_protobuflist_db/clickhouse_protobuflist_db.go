package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
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
	clickhouseConnectStringCst = "127.0.0.1:19001"
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
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + config build + primaryFunction. Extracted so
// tests can drive it with synthetic args without exiting.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clickhouse_protobuflist_db", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("filename", "protoBytes.bin", "filename")
	valueStr := fs.String("values", "1", "values uints -> uint32, comma separated")
	envelope := fs.Bool("envelope", false, "envelope")
	db := fs.Bool("db", false, "db")
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

	return primaryFunction(c, stderr)
}

func primaryFunction(c config, stderr io.Writer) int {
	binaryData := prepareBinary(c)
	return fileOrDB(c, binaryData, stderr)
}

func prepareBinary(c config) (binaryData []byte) {

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

// func encodeRecord(value uint) []byte {
// 	r := &clickhouse_protolist.Record{
// 		MyUint32: uint32(value),
// 	}
// 	serializedData, err := proto.Marshal(r)
// 	if err != nil {
// 		log.Fatal("serializedData, err:= proto.Marshal(r):", err)
// 	}

// 	buf := make([]byte, binary.MaxVarintLen64)
// 	n := binary.PutUvarint(buf, uint64(len(serializedData)))
// 	return append(buf[:n], serializedData...)
// }

func fileOrDB(c config, binaryData []byte, stderr io.Writer) int {
	if c.db {
		fmt.Fprintln(stderr, "db not implemented, cos it's hard to insert protobuf with the clickhouse library")
		return 1
	}
	if err := writeDataToFile(c.filename, binaryData); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}

func writeDataToFile(filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0600) // 0600 permissions (rw-------) per gosec G306
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil

}

// func insertIntoCH(c config, binaryData []byte) error {
// 	ctx := context.Background()

// 	// Connect using ch-go
// 	conn, err := chproto.Dial(
// 		ctx, c.connectStr,
// 		chproto.WithCompression(chproto.CompressionZSTD),
// 	)
// 	if err != nil {
// 		log.Fatalf("Failed to connect to ClickHouse: %v", err)
// 	}
// 	defer conn.Close()

// 	// Determine the format based on the 'envelope' flag
// 	format := chproto.FormatProtobuf
// 	if c.envelope {
// 		format = proto.FormatProtobufList
// 	}

// 	// Insert the data
// 	err = conn.Do(ctx, chproto.Query{
// 		Body: binaryData, // Send the prepared binary data
// 		OnInput: func(ctx context.Context) error {
// 			return conn.Do(ctx, proto.Insert{
// 				Table:  "clickhouse_protolist.clickhouse_protolist",
// 				Format: format, // Use the appropriate format
// 				Columns: string{
// 					"my_uint32", // Assuming your column name is 'my_uint32'
// 				},
// 			})
// 		},
// 	})
// 	if err != nil {
// 		return fmt.Errorf("failed to insert data: %w", err)
// 	}

// 	return nil
// }

// func insertIntoCH(c config, binaryData []byte) error {

// 	ctx := context.TODO()

// 	conn, err := clickhouse.Open(&clickhouse.Options{
// 		Addr: []string{c.connectStr}, // Update with your ClickHouse address
// 		Auth: clickhouse.Auth{
// 			Database: "",
// 			Username: c.user,
// 			Password: c.pass,
// 		},
// 		// https://pkg.go.dev/github.com/ClickHouse/clickhouse-go/v2@v2.32.0#pkg-constants
// 		// https://github.com/ClickHouse/clickhouse-go?tab=readme-ov-file#clickhouse-interface-formally-native-interface
// 		Compression: &clickhouse.Compression{
// 			Method: clickhouse.CompressionZSTD,
// 		},
// 		Debug: true,
// 	})
// 	if err != nil {
// 		log.Fatalf("Failed to connect to ClickHouse: %v", err)
// 	}
// 	defer conn.Close()

// 	insertQuery := `
// INSERT INTO clickhouse_protolist.clickhouse_protolist
// SETTINGS format_schema = 'clickhouse_protolist.proto:Record'
// `
// 	//SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:Record;
// 	//SETTINGS format_schema = '/var/lib/clickhouse/format_schemas/clickhouse_protolist.proto:clickhouse_protolist.v1.Record;

// 	format := "FORMAT Protobuf"
// 	if c.envelope {
// 		format = "FORMAT ProtobufList"
// 	}

// 	insertQuery = insertQuery + format

// 	log.Printf("insertQuery:%s", insertQuery)

// 	ctxT, cancel := context.WithTimeout(ctx, 5*time.Second)
// 	defer cancel()

// 	start := time.Now()
// 	// if err := conn.Exec(ctxT, insertQuery, binaryData); err != nil {
// 	// 	log.Fatalf("Failed to insert Protobuf data: %v", err)
// 	// }

// 	batch, err := conn.PrepareBatch(ctxT, insertQuery)
// 	if err != nil {
// 		log.Fatalf("Failed to prepare batch: %v", err)
// 	}

// 	if err := batch.Append(binaryData); err != nil {
// 		log.Fatalf("Failed to append Protobuf data: %v", err)
// 	}
// 	if err := batch.Send(); err != nil {
// 		log.Fatalf("Failed to send batch: %v", err)
// 	}

// 	log.Printf("insertQuery complete, after:%0.3fs", time.Since(start).Seconds())

// 	return nil
// }
