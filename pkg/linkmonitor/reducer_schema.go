package linkmonitor

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const maximumSamples = 65536

type sampleKey struct{ descriptor, labels string }
type sampleDefinition struct {
	noInterfaceLabel bool
	key              sampleKey
	kind             model.SampleKind
	labels           []model.Label
	counter          model.CounterIdentity
}
type sampleSchema struct {
	entries []sampleDefinition
	index   map[sampleKey]int
}

func freezeSamples(previous *collectorBlock, samples []model.Sample) (*collectorBlock, error) {
	if len(samples) > maximumSamples {
		return nil, fmt.Errorf("collector exceeds %d samples", maximumSamples)
	}
	var schema *sampleSchema
	if previous != nil && sameSchema(previous.schema, samples) {
		schema = previous.schema
	} else {
		var err error
		schema, err = newSchema(samples)
		if err != nil {
			return nil, err
		}
	}
	values := make([]model.Number, len(samples))
	for i := range samples {
		if err := validateSampleValue(&samples[i]); err != nil {
			return nil, err
		}
		values[i] = samples[i].Number
	}
	return &collectorBlock{schema: schema, values: values}, nil
}

func sameSchema(schema *sampleSchema, samples []model.Sample) bool {
	if len(schema.entries) != len(samples) {
		return false
	}
	for i := range samples {
		entry, sample := &schema.entries[i], &samples[i]
		if entry.noInterfaceLabel != sample.NoInterfaceLabel || entry.key.descriptor != sample.Descriptor || entry.kind != sample.Kind || entry.counter != sample.Counter || !sameLabels(entry.labels, sample.Labels) {
			return false
		}
	}
	return true
}

func sameLabels(known, incoming []model.Label) bool {
	if len(known) != len(incoming) {
		return false
	}
	// Schemas have unique label names. Equal lengths plus finding every known
	// pair rejects duplicate incoming labels without allocating a temporary map.
	for _, pair := range known {
		if !slices.Contains(incoming, pair) {
			return false
		}
	}
	return true
}

func newSchema(samples []model.Sample) (*sampleSchema, error) {
	schema := &sampleSchema{entries: make([]sampleDefinition, len(samples)), index: make(map[sampleKey]int, len(samples))}
	for i := range samples {
		sample := &samples[i]
		if !hostIdentifier([]byte(sample.Descriptor)) || (sample.Kind != model.SampleCounter && sample.Kind != model.SampleGauge && sample.Kind != model.SampleUntyped) {
			return nil, fmt.Errorf("invalid sample descriptor or kind")
		}
		labels, key, err := canonicalLabels(sample.Labels)
		if err != nil {
			return nil, err
		}
		entry := sampleDefinition{key: sampleKey{sample.Descriptor, key}, kind: sample.Kind, labels: labels, counter: sample.Counter, noInterfaceLabel: sample.NoInterfaceLabel}
		if _, duplicate := schema.index[entry.key]; duplicate {
			return nil, fmt.Errorf("duplicate sample %q", sample.Descriptor)
		}
		schema.entries[i] = entry
		schema.index[entry.key] = i
	}
	return schema, nil
}

func canonicalLabels(input []model.Label) ([]model.Label, string, error) {
	labels := slices.Clone(input)
	slices.SortFunc(labels, func(a, b model.Label) int { return cmp.Compare(a.Name, b.Name) })
	var encoded []byte
	for i, pair := range labels {
		if !hostIdentifier([]byte(pair.Name)) || strings.HasPrefix(pair.Name, "__") || !utf8.ValidString(pair.Value) || (i > 0 && labels[i-1].Name == pair.Name) {
			return nil, "", fmt.Errorf("empty or duplicate label name")
		}
		// Length prefixes avoid collisions even if source strings contain NULs.
		encoded = strconv.AppendInt(encoded, int64(len(pair.Name)), 10)
		encoded = append(encoded, ':')
		encoded = append(encoded, pair.Name...)
		encoded = strconv.AppendInt(encoded, int64(len(pair.Value)), 10)
		encoded = append(encoded, ':')
		encoded = append(encoded, pair.Value...)
	}
	return labels, string(encoded), nil
}

func validateSampleValue(sample *model.Sample) error {
	if sample.Kind != model.SampleCounter {
		return nil
	}
	width := sample.Counter.Width
	if width > 64 {
		return fmt.Errorf("invalid counter source width")
	}
	if sample.Number.Kind() == model.NumberAbsent {
		return nil
	}
	value, unsigned := sample.Number.Uint64()
	if !unsigned || (width != 0 && width < 64 && value >= uint64(1)<<width) {
		return fmt.Errorf("invalid counter representation or source width")
	}
	return nil
}
