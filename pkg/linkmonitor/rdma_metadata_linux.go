package linkmonitor

import (
	"errors"
	"io/fs"
	"math"
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

// rdmaPortMetadata converts the port's rate and, for the device's info owner,
// the device attributes. A missing rate is skipped and a missing attribute is
// an empty label, as on drivers that do not provide them.
func rdmaPortMetadata(p model.RDMAPort, owner bool, rate rdmaSlotRead, device []rdmaSlotRead) ([]model.Sample, error) {
	samples := make([]model.Sample, 0, 2)
	if rate.err != nil && !errors.Is(rate.err, fs.ErrNotExist) {
		return nil, rate.err
	}
	if rate.err == nil {
		n, err := rdmaRate(rate.text)
		if err != nil {
			return nil, err
		}
		samples = append(samples, model.Sample{Descriptor: "go_link_monitor_infiniband_rate_bytes_per_second", Kind: model.SampleGauge, Number: model.Unsigned(n), Labels: rdmaLabels(p), NoInterfaceLabel: true})
	}
	if !owner {
		return samples, nil
	}
	labels := []model.Label{{Name: "device", Value: p.Device}}
	for i, label := range [...]string{"board_id", "firmware_version", "hca_type"} {
		if err := device[i].err; err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		labels = append(labels, model.Label{Name: label, Value: device[i].text})
	}
	return append(samples, model.Sample{Descriptor: "go_link_monitor_infiniband_info", Kind: model.SampleGauge, Number: model.Unsigned(1), Labels: labels, NoInterfaceLabel: true}), nil
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
