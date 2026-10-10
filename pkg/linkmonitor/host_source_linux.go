package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const maximumHostFile = 4 << 20

const hostIPv6File = "snmp6"

type hostCollector struct {
	workerCollector
	root    string
	open    func(string) (io.ReadCloser, error)
	parser  hostParser
	scratch []byte
}

func newHostCollector(base workerCollector, root string, cfg configuration) *hostCollector {
	if root == "" {
		root = "/proc/net"
	}
	return &hostCollector{workerCollector: base, root: root, parser: hostParser{filter: cfg.netstat},
		open: func(path string) (io.ReadCloser, error) { return os.Open(path) }}
}

func (c *hostCollector) Collect(ctx context.Context, job model.Job) model.Result {
	if job.Key.Collector != model.CollectorNetstat {
		return c.workerCollector.Collect(ctx, job)
	}
	result := model.Result{Job: job}
	c.parser.reset()
	for _, name := range [...]string{"snmp", "netstat", hostIPv6File} {
		if err := c.read(ctx, name); err != nil {
			result.Err = fmt.Errorf("host %s: %w", name, err)
			result.Reason = hostReason(err)
			return result
		}
	}
	if err := ctx.Err(); err != nil {
		result.Err, result.Reason = err, hostReason(err)
		return result
	}
	total := len(c.parser.fields)
	result.Support, result.Samples = model.Supported, c.parser.samples()
	result.Filtered = presentValue(uint64(total - len(result.Samples)))
	return result
}

func (c *hostCollector) read(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := c.open(filepath.Join(c.root, name))
	if err != nil {
		if name == hostIPv6File && errors.Is(err, os.ErrNotExist) {
			return ctx.Err()
		}
		return err
	}
	data, readErr := c.readFile(ctx, file)
	if err := errors.Join(readErr, file.Close(), ctx.Err()); err != nil {
		return err
	}
	if name == hostIPv6File {
		return c.parser.ipv6(ctx, data)
	}
	return c.parser.paired(ctx, data)
}

func (c *hostCollector) readFile(ctx context.Context, reader io.Reader) ([]byte, error) {
	if c.scratch == nil {
		c.scratch = make([]byte, 32<<10)
	}
	used, empty := 0, 0
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if used == len(c.scratch) {
			next := make([]byte, min(2*len(c.scratch), maximumHostFile+1))
			copy(next, c.scratch)
			c.scratch = next
		}
		n, err := reader.Read(c.scratch[used:])
		used += n
		if used > maximumHostFile {
			return nil, errHostLimit
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return c.scratch[:used], nil
			}
			return nil, err
		}
		if n == 0 {
			empty++
			if empty == 100 {
				return nil, io.ErrNoProgress
			}
		} else {
			empty = 0
		}
	}
}

func hostReason(err error) model.ErrorReason {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.ErrorTimeout
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return model.ErrorPermission
	case errors.Is(err, errHostLimit):
		return model.ErrorOversize
	case errors.Is(err, errHostMalformed):
		return model.ErrorMalformed
	default:
		return model.ErrorIO
	}
}
