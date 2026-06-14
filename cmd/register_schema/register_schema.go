package main

import (
	"bytes"
	"context"
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
	return registerProtobufSchemaAt(http.DefaultClient, schemaRegistryURLCst, subject, schema)
}

// registerProtobufSchemaAt POSTs the schema document to <baseURL>/subjects/<subject>/versions
// via the supplied HTTP client. Extracted so tests can drive it against
// an httptest.Server instead of the hardcoded schemaRegistryURLCst.
func registerProtobufSchemaAt(client *http.Client, baseURL, subject, schema string) error {
	url := fmt.Sprintf("%s/subjects/%s/versions", baseURL, subject)

	bodyBytes, err := json.Marshal(SchemaRequest{Schema: schema, SchemaType: "PROTOBUF"})
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/vnd.schemaregistry.v1+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

func getLatestSchemaID(subject string) (int, error) {
	return getLatestSchemaIDAt(http.DefaultClient, schemaRegistryURLCst, subject)
}

// getLatestSchemaIDAt fetches the latest schema ID for `subject` via the
// supplied HTTP client. Same extraction pattern as registerProtobufSchemaAt.
func getLatestSchemaIDAt(client *http.Client, baseURL, subject string) (int, error) {
	url := fmt.Sprintf("%s/subjects/%s/versions/latest", baseURL, subject)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var result struct {
		ID int `json:"id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
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
		var id int
		id, err = getLatestSchemaID(subject)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error registering schema: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("id:%d", id)

	}
}
