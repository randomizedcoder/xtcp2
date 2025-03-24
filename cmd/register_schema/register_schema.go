package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
)

const (
	schemaRegistryURLCst = "http://localhost:18081" // Update to your Redpanda schema registry URL
	topicCst             = "protobuf_list"          // Subject name for schema
	protoFilePathCst     = "my_proto.proto"         // File path to your .proto file
)

// SchemaRequest represents the payload to send to the schema registry
type SchemaRequest struct {
	Schema     string `json:"schema"`
	SchemaType string `json:"schemaType"` // "PROTOBUF"
}

func readProtobufFromFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read proto file: %w", err)
	}
	return string(data), nil
}

func registerProtobufSchema(subject string, schema string) error {
	fmt.Println("registerProtobufSchema")

	url := fmt.Sprintf("%s/subjects/%s/versions", schemaRegistryURLCst, subject)

	reqBody := SchemaRequest{
		Schema:     schema,
		SchemaType: "PROTOBUF",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := http.Post(url, "application/vnd.schemaregistry.v1+json", bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	fmt.Println("Schema registered successfully under subject:", subject)
	return nil
}

func getLatestSchemaID(subject string) (int, error) {
	fmt.Println("getLatestSchemaID")

	url := fmt.Sprintf("%s/subjects/%s/versions/latest", schemaRegistryURLCst, subject)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var result struct {
		ID int `json:"id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}

	return result.ID, nil
}

func main() {

	filename := flag.String("filename", "my_proto.proto", "filename")
	topic := flag.String("topic", topicCst, "topic")

	register := flag.Bool("register", false, "register")
	get := flag.Bool("get", true, "get")

	flag.Parse()

	schema, err := readProtobufFromFile(*filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading schema: %v\n", err)
		os.Exit(1)
	}

	subject := fmt.Sprintf("%s-value", *topic)

	if *register {
		err = registerProtobufSchema(subject, schema)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error registering schema: %v\n", err)
			os.Exit(1)
		}
	}

	if *get {
		id, err := getLatestSchemaID(subject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error registering schema: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("id:%d", id)

	}
}
