package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
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

	filename := flag.String("filename", "protoBytes.bin", "filename")
	valueStr := flag.String("values", "1", "values uints -> uint32, comma seperated")

	envelope := flag.Bool("envelope", false, "envelope")
	db := flag.Bool("db", false, "db")

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

	valueStrs := strings.Split(*valueStr, ",")
	var values []uint
	for _, str := range valueStrs {
		v, err := strconv.ParseUint(str, 10, 32)
		if err != nil {
			log.Fatalf("Invalid value: %v", err)
		}
		values = append(values, uint(v))
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

	primaryFunction(c)
}

func primaryFunction(c config) {

	binaryData := prepareBinary(c)

	fileOrDB(c, binaryData)

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
			errW := os.WriteFile(c.dumpFilename+".envelope", binaryData, 0644)
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

func fileOrDB(c config, binaryData []byte) {

	if !c.db {
		errW := writeDataToFile(c.filename, binaryData)
		if errW != nil {
			log.Println("Error:", errW)
		}
		os.Exit(0)
	}

	if c.db {
		log.Fatal("db not implemented, cos it's hard to insert protobuf with the clickhouse library")
	}

	// errDB := insertIntoCH(c, binaryData)
	// if errDB != nil {
	// 	log.Println("Error:", errDB)
	// }

}

func writeDataToFile(filename string, data []byte) error {

	err := os.WriteFile(filename, data, 0644) // 0644 permissions (rw-r--r--)
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
