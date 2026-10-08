package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

var errCarrierValue = errors.New("invalid carrier sysfs value")

var carrierFiles = [...]string{carrierCollectorName, "carrier_changes", "carrier_up_count", "carrier_down_count"}

type carrierFilesystem struct {
	root     string
	readFile func(context.Context, string) (uint64, error)
}

func readCarrierFile(ctx context.Context, path string) (uint64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 33))
	if err = errors.Join(err, file.Close(), ctx.Err()); err != nil {
		return 0, err
	}
	if len(data) > 32 {
		return 0, errCarrierValue
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return 0, errCarrierValue
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errCarrierValue
		}
	}
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, errors.Join(errCarrierValue, err)
	}
	return n, nil
}

func (s carrierFilesystem) read(ctx context.Context, name string, index uint32, missing uint8) (model.CarrierValues, [4]error) {
	var values model.CarrierValues
	var failures [4]error
	if s.readFile == nil {
		s.readFile = readCarrierFile
	}
	if !validName(name, 255) || name == "." || name == ".." || index == 0 {
		return values, allCarrierErrors(fmt.Errorf("invalid carrier identity"))
	}
	path := filepath.Join(s.root, name)
	if err := s.checkIndex(ctx, path, index); err != nil {
		return values, allCarrierErrors(err)
	}
	for i, file := range carrierFiles {
		if missing&(1<<i) == 0 {
			continue
		}
		value, err := s.readFile(ctx, filepath.Join(path, file))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err == nil && i == 0 && value > 1 {
			err = errCarrierValue
		}
		if err != nil {
			failures[i] = err
			continue
		}
		values[i] = presentValue(value)
	}
	if err := s.checkIndex(ctx, path, index); err != nil {
		return model.CarrierValues{}, allCarrierErrors(err)
	}
	return values, failures
}

func (s carrierFilesystem) checkIndex(ctx context.Context, path string, index uint32) error {
	got, err := s.readFile(ctx, filepath.Join(path, "ifindex"))
	if err != nil {
		return err
	}
	if got != uint64(index) {
		return fmt.Errorf("carrier interface replaced")
	}
	return nil
}

func allCarrierErrors(err error) [4]error { return [4]error{err, err, err, err} }
