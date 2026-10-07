package baseline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// MaxFileBytes bounds startup reads, including malformed or oversized files.
const MaxFileBytes = 64 * 1024

type wireRecord struct {
	Version    *int       `json:"version"`
	Count      *uint64    `json:"expected_up_links"`
	RecordedAt *time.Time `json:"recorded_at"`
}

func decodeRecord(data []byte) (model.Baseline, error) {
	var record model.Baseline
	if err := validateKeys(data); err != nil {
		return record, err
	}
	var wire wireRecord
	if err := json.Unmarshal(data, &wire); err != nil {
		return record, fmt.Errorf("decode baseline: %w", err)
	}
	if wire.Version == nil || wire.Count == nil || wire.RecordedAt == nil {
		return record, fmt.Errorf("baseline lacks required fields")
	}
	record = model.Baseline{Version: *wire.Version, Count: *wire.Count, RecordedAt: *wire.RecordedAt}
	return record, validateRecord(record)
}

func validateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil {
		return err
	}
	if start != json.Delim('{') {
		return fmt.Errorf("baseline must be an object")
	}
	seen := make(map[string]bool, 3)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		if tokenErr != nil {
			return tokenErr
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate or invalid baseline key %v", token)
		}
		switch key {
		case "version", "expected_up_links", "recorded_at":
		default:
			return fmt.Errorf("unknown baseline key %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing baseline data")
	}
	return nil
}

func validateRecord(record model.Baseline) error {
	if record.Version != 1 {
		return fmt.Errorf("unsupported baseline version %d", record.Version)
	}
	if record.RecordedAt.IsZero() {
		return fmt.Errorf("baseline timestamp is zero")
	}
	return nil
}

func encodeRecord(record model.Baseline) ([]byte, error) {
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	utc := record.RecordedAt.UTC()
	data, err := json.Marshal(wireRecord{Version: &record.Version, Count: &record.Count, RecordedAt: &utc})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
