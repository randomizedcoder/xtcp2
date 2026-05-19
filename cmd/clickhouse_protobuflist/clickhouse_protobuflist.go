package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/randomizedcoder/xtcp2/pkg/clickhouse_protolist"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func encodeLengthDelimitedProtobufList(r *clickhouse_protolist.Envelope_Record) (result []byte, err error) {

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
	// protowire.AppendVarint returns the appended slice — the previous
	// code dropped the return value, so the length prefix was never
	// actually written. Non-envelope mode emitted raw record bytes
	// without the length-delim wrapper its name advertises (ClickHouse
	// readers expecting LengthDelimited misparsed the file). Use the
	// return value the way encodeLengthDelimitedEnvelope below already
	// does.
	result = protowire.AppendVarint(result, uint64(len(recordBytes)))

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
	err := os.WriteFile(filename, data, 0600) // 0600 permissions (rw-------) per gosec G306
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
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + encode + writeDataToFile. Extracted so tests
// can drive it with synthetic args + capture buffers (without exiting).
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clickhouse_protobuflist", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("filename", "protoBytes.bin", "filename")
	value := fs.Uint("value", 1, "value uint -> uint32")
	envelope := fs.Bool("envelope", false, "envelope")
	v := fs.Bool("v", false, "show version")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *v {
		fmt.Fprintf(stdout, "commit:%s\tdate(UTC):%s\tversion:%s\n", commit, date, version)
		return 0
	}

	r := &clickhouse_protolist.Envelope_Record{MyUint32: uint32(*value)}
	encodedData, err := encodeLengthDelimitedProtobufList(r)
	if err != nil {
		fmt.Fprintln(stderr, "Error encoding:", err)
		return 1
	}

	if !*envelope {
		if err := writeDataToFile(*filename, encodedData); err != nil {
			fmt.Fprintln(stderr, "Error:", err)
			return 1
		}
		return 0
	}

	encodedEnvelope, err := encodeLengthDelimitedEnvelope(encodedData)
	if err != nil {
		fmt.Fprintln(stderr, "Error encoding Envelope:", err)
		return 1
	}
	if err := writeDataToFile(*filename, encodedEnvelope); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	return 0
}
