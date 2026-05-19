package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
)

const (
	schemaRegistryURLCst = "http://localhost:18081" // Update to your Redpanda schema registry URL
	topicCst             = "protobuf_list"          // Subject name for schema
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
	// Schema Registries return a JSON error body on 4xx/5xx (e.g.
	// {"error_code":40402,"message":"Subject not found."}). The previous
	// code skipped the status check and decoded the error body straight
	// into the {id int} struct, producing a silent id:0 success — the
	// CLI printed "id:0" for a missing subject. Reject non-2xx upfront.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("getLatestSchemaIDAt %s: unexpected status:%d", url, resp.StatusCode)
	}
	var result struct {
		ID int `json:"id"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, err
	}
	return result.ID, nil
}

func main() {
	os.Exit(runMain(os.Args[1:], schemaRegistryURLCst, http.DefaultClient, os.Stdout, os.Stderr))
}

// runMain wires flag parsing + the two HTTP calls. Extracted so tests can
// drive it with synthetic args, a fake baseURL, and an httptest.Server's
// client. Returns the process exit code.
func runMain(args []string, baseURL string, client *http.Client, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("register_schema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	filename := fs.String("filename", "my_proto.proto", "filename")
	topic := fs.String("topic", topicCst, "topic")
	register := fs.Bool("register", false, "register")
	get := fs.Bool("get", true, "get")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	schema, err := readProtobufFromFile(*filename)
	if err != nil {
		fmt.Fprintf(stderr, "Error reading schema: %v\n", err)
		return 1
	}

	subject := fmt.Sprintf("%s-value", *topic)

	if *register {
		if err := registerProtobufSchemaAt(client, baseURL, subject, schema); err != nil {
			fmt.Fprintf(stderr, "Error registering schema: %v\n", err)
			return 1
		}
	}

	if *get {
		id, err := getLatestSchemaIDAt(client, baseURL, subject)
		if err != nil {
			// Previously said "Error registering schema" — copy-paste from
			// the register branch above. This path is the GET, so misleading
			// for operators trying to figure out which call failed.
			fmt.Fprintf(stderr, "Error getting schema: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "id:%d", id)
	}
	return 0
}
