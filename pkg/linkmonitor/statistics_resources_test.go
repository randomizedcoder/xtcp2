package linkmonitor

import (
	"context"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

func TestStatisticResources(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		openError                                    bool
	}{
		{"open failure", "negative", "socket construction fails", "error returned and caller retains base", true},
		{"close failure", "corner", "socket and delegated close fail", "all resources closed and errors joined", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			cfg, err := validateConfig(DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			closed, delegated := false, false
			baseErr := errors.New("base close")
			base := testWorkerCollector{CollectorFunc: func(context.Context, model.Job) model.Result {
				delegated = true
				return model.Result{Support: model.Unsupported}
			}, closeFunc: func() error { closed = true; return baseErr }}
			c, err := openStatisticsCollector(base, cfg, func() (*ethtoolIoctl, error) {
				if tc.openError {
					return nil, unix.EMFILE
				}
				return &ethtoolIoctl{fd: -1}, nil
			})
			if tc.openError {
				if !errors.Is(err, unix.EMFILE) || closed || c != nil {
					t.Fatal(tc.expectedOutcome)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.source.buffer.bytes(40); err != nil {
				t.Fatal(err)
			}
			result := c.Collect(t.Context(), statisticTestJob(model.CollectorChannels))
			if !delegated || result.Support != model.Unsupported {
				t.Fatal("unhandled collector not delegated")
			}
			err = c.Close()
			if !errors.Is(err, unix.EBADF) || !errors.Is(err, baseErr) || !closed || c.source.buffer.mapping != nil {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
