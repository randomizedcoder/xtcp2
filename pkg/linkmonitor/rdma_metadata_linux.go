package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func rdmaStateCompatibility(p model.RDMAPort) []model.Sample {
	samples := make([]model.Sample, 0, 2)
	for _, field := range [...]struct {
		name  string
		value model.Optional[uint8]
	}{{"state_id", p.State}, {"physical_state_id", p.Physical}} {
		if field.value.Present {
			samples = append(samples, model.Sample{Descriptor: "go_link_monitor_infiniband_" + field.name, Kind: model.SampleGauge, Number: model.Unsigned(uint64(field.value.Value)), Labels: rdmaLabels(p), NoInterfaceLabel: true})
		}
	}
	return samples
}

func (c *rdmaOptionalCollector) portMetadata(ctx context.Context, path string, p model.RDMAPort, owners []string) ([]model.Sample, error) {
	samples := make([]model.Sample, 0, 2)
	text, err := rdmaScalar(ctx, filepath.Join(path, "rate"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		rate, err := rdmaRate(text)
		if err != nil {
			return nil, err
		}
		samples = append(samples, model.Sample{Descriptor: "go_link_monitor_infiniband_rate_bytes_per_second", Kind: model.SampleGauge, Number: model.Unsigned(rate), Labels: rdmaLabels(p), NoInterfaceLabel: true})
	}
	if slices.Contains(owners, rdmaSelector(p)) {
		info, err := c.deviceMetadata(ctx, p.Device)
		if err != nil {
			return nil, err
		}
		samples = append(samples, info)
	}
	return samples, nil
}

func (c *rdmaOptionalCollector) deviceMetadata(ctx context.Context, device string) (model.Sample, error) {
	labels := []model.Label{{Name: "device", Value: device}}
	for _, field := range [...]struct{ file, label string }{{"board_id", "board_id"}, {"fw_ver", "firmware_version"}, {"hca_type", "hca_type"}} {
		value, err := rdmaScalar(ctx, filepath.Join(c.files.root, device, field.file))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return model.Sample{}, err
		}
		labels = append(labels, model.Label{Name: field.label, Value: value})
	}
	return model.Sample{Descriptor: "go_link_monitor_infiniband_info", Kind: model.SampleGauge, Number: model.Unsigned(1), Labels: labels, NoInterfaceLabel: true}, nil
}

// Parse nominal Gbit/sec without a float round trip. The ABI uses integer or
// one-decimal rates (e.g. 2.5); this is never evidence of supported maximum.
func rdmaRate(text string) (uint64, error) {
	number, suffix, ok := strings.Cut(text, " ")
	if !ok || (suffix != "Gb/sec" && !strings.HasPrefix(suffix, "Gb/sec (")) {
		return 0, linuxio.ErrReply
	}
	whole, fraction, decimal := strings.Cut(number, ".")
	n, err := strconv.ParseUint(whole, 10, 64)
	if err != nil || (decimal && (len(fraction) != 1 || fraction[0] < '0' || fraction[0] > '9')) {
		return 0, linuxio.ErrReply
	}
	var part uint64
	if decimal {
		part = uint64(fraction[0]-'0') * 12500000
	}
	if n > (math.MaxUint64-part)/125000000 {
		return 0, linuxio.ErrLimit
	}
	return n*125000000 + part, nil
}
