package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func encodeLengthDelimitedProtobufList(r *clickhouse_protolist.Record) (result []byte, err error) {

	// for _, record := range e.Rows {
	// 	recordBytes, err := proto.Marshal(record)
	// 	if err != nil {
	// 		return nil, fmt.Errorf("error marshaling Record: %v", err)
	// 	}

	recordBytes, err := proto.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("error marshaling Record: %v", err)
	}

	log.Printf("AppendVarint of length:%d", len(recordBytes))
	protowire.AppendVarint(result, uint64(len(recordBytes)))

	result = append(result, recordBytes...)

	// }

	return result, nil
}

func encodeLengthDelimitedEnvelope(encodedData []byte) (result []byte, err error) {

	result = append(result, protowire.AppendVarint(nil, uint64(len(encodedData)))...)
	result = append(result, encodedData...)

	return result, nil
}

func writeDataToFile(filename string, data []byte) error {
	err := os.WriteFile(filename, data, 0644) // 0644 permissions (rw-r--r--)
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err) // Wrap the error
	}
	return nil
}

var (
	// Passed by "go build -ldflags" for the show version
	commit  string
	date    string
	version string
)

func main() {

	filename := flag.String("filename", "protoBytes.bin", "filename")
	value := flag.Uint("value", 1, "value uint -> uint32")

	envelope := flag.Bool("envelope", false, "envelope")

	v := flag.Bool("v", false, "show version")

	flag.Parse()

	// Print version information passed in via ldflags in the Makefile
	if *v {
		log.Printf("commit:%s\tdate(UTC):%s\tversion:%s", commit, date, version)
		os.Exit(0)
	}

	r := &clickhouse_protolist.Record{}
	r.MyUint32 = uint32(*value)

	encodedData, err := encodeLengthDelimitedProtobufList(r)
	if err != nil {
		log.Println("Error encoding:", err)
		return
	}

	if !*envelope {
		errW := writeDataToFile(*filename, encodedData)
		if errW != nil {
			log.Println("Error:", errW)
			return
		}
		os.Exit(0)
	}

	e := &clickhouse_protolist.Envelope{}
	e.Rows = append(e.Rows, r)

	encodedEnvelope, err := encodeLengthDelimitedEnvelope(encodedData)
	if err != nil {
		fmt.Println("Error encoding Envelope:", err)
		return
	}

	errW := writeDataToFile(*filename, encodedEnvelope)
	if errW != nil {
		log.Println("Error:", errW)
		return
	}

}
