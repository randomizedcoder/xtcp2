package linkmonitor

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

type hostFinalReader struct {
	data string
	err  error
}

func (r hostFinalReader) Read(buffer []byte) (int, error) { return copy(buffer, r.data), r.err }

func TestHostReaderBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		reader                                       io.Reader
		want                                         string
		err                                          error
	}{
		{"final bytes", "corner", "read returns bytes and EOF together", "bytes preserved", hostFinalReader{"Tcp: A\nTcp: 1", io.EOF}, "Tcp: A\nTcp: 1", nil},
		{"partial error", "negative", "read returns bytes and error together", "no partial result", hostFinalReader{"Tcp: A\nTcp: 1", syscall.EIO}, "", syscall.EIO},
		{"no progress", "negative", "reader repeatedly returns zero nil", "bounded error", hostFinalReader{}, "", io.ErrNoProgress},
		{"short reads", "positive", "sequential small reads", "complete bytes", io.MultiReader(strings.NewReader("Tcp: A\n"), strings.NewReader("Tcp: 1")), "Tcp: A\nTcp: 1", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := hostTestCollector(t, nil)
			data, err := c.readFile(t.Context(), tc.reader)
			if !errors.Is(err, tc.err) || string(data) != tc.want {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}

func TestHostNoMatchValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		data                                         string
		readErr                                      error
		want                                         model.ErrorReason
	}{
		{"valid", "positive", "no matching fields", "empty supported result", "Tcp: A\nTcp: 1", nil, model.ErrorNone},
		{"malformed", "negative", "hidden malformed value", "malformed failure", "Tcp: A\nTcp: nope", nil, model.ErrorMalformed},
		{"unreadable", "negative", "no-match filter on unreadable source", "permission failure", "", syscall.EACCES, model.ErrorPermission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := hostTestCollector(t, map[string]string{"snmp": tc.data, "netstat": ""})
			c.parser.filter = regexp.MustCompile("^$")
			if tc.readErr != nil {
				c.open = func(string) (io.ReadCloser, error) { return nil, tc.readErr }
			}
			result := c.Collect(t.Context(), hostTestJob())
			if result.Reason != tc.want || len(result.Samples) != 0 || (result.Support == model.Supported) != (tc.want == model.ErrorNone) {
				t.Fatal(tc.expectedOutcome, result)
			}
		})
	}
}

func TestHostParserCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ipv6                                         bool
	}{
		{"pairs", "negative", "context canceled before paired parsing", "canceled without publication", false},
		{"IPv6", "negative", "context canceled before IPv6 parsing", "canceled without publication", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			p := hostParser{filter: regexp.MustCompile(".*")}
			p.reset()
			var err error
			if tc.ipv6 {
				err = p.ipv6(ctx, []byte("Ip6A 1"))
			} else {
				err = p.paired(ctx, []byte("Tcp: A\nTcp: 1"))
			}
			if !errors.Is(err, context.Canceled) || len(p.fields) != 0 {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}

func TestHostLateResults(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       func(*model.Job)
	}{
		{"attempt", "negative", "wrong attempt token", "no samples or diagnostics", func(j *model.Job) { j.Token.Attempt++ }},
		{"epoch", "negative", "wrong source epoch", "no samples or diagnostics", func(j *model.Job) { j.Token.SourceEpoch++ }},
		{"revision", "negative", "wrong schema revision", "no samples or diagnostics", func(j *model.Job) { j.SchemaRevision++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := newReducer(1)
			job, err := r.startCollection(hostTestJob().Key, model.Stamp{})
			if err != nil {
				t.Fatal(err)
			}
			tc.change(&job)
			accepted, err := r.finishCollection(model.Result{Job: job, Support: model.Supported})
			if err != nil || accepted || r.host.attempted || r.host.block != nil {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}
