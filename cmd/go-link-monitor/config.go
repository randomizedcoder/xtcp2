package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

const (
	exceptionEnvironment   = "GO_LINK_MONITOR_MAX_SPEED_EXCEPTIONS"
	coreIncludeEnvironment = "GO_LINK_MONITOR_METRICS_CORE_INCLUDE"
	coreExcludeEnvironment = "GO_LINK_MONITOR_METRICS_CORE_EXCLUDE"
	allPathEnvironment     = "GO_LINK_MONITOR_METRICS_ALL_PATH"
	allAddressEnvironment  = "GO_LINK_MONITOR_METRICS_ALL_ADDR"

	metricsPath        = "/metrics"
	defaultAllPath     = "/metrics-all"
	defaultCoreInclude = monitorprom.CoreMetricsPattern
)

type commandOptions struct {
	monitor  linkmonitor.Config
	address  string
	exposure exposureOptions
	version  bool
}

// exposureOptions selects which metrics each endpoint serves.
type exposureOptions struct {
	coreInclude, coreExclude string
	allPath, allAddress      string
}

func parseOptions(args []string, output io.Writer, getenv func(string) string) (commandOptions, error) {
	opts := commandOptions{monitor: linkmonitor.DefaultConfig()}
	flags := flag.NewFlagSet("go-link-monitor", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&opts.address, "metrics-addr", ":9101", "HTTP listen address")
	flags.StringVar(&opts.monitor.BaselineFile, "baseline-file", opts.monitor.BaselineFile, "Persistent baseline file")
	flags.DurationVar(&opts.monitor.Settle, "settle", opts.monitor.Settle, "Initial stable interval (nonnegative)")
	flags.DurationVar(&opts.monitor.Resync, "resync", opts.monitor.Resync, "Full reconciliation interval (positive)")
	flags.DurationVar(&opts.monitor.StatsInterval, "stats-interval", opts.monitor.StatsInterval, "Statistics interval (positive)")
	flags.StringVar(&opts.monitor.StatsInclude, "stats-include", opts.monitor.StatsInclude, "Driver/PHY names to include (regexp)")
	flags.StringVar(&opts.monitor.StatsExclude, "stats-exclude", opts.monitor.StatsExclude, "Driver/PHY names to exclude (regexp)")
	flags.StringVar(&opts.monitor.NetstatFields, "collector.netstat.fields", opts.monitor.NetstatFields, "Host protocol fields (regexp)")
	var exceptions string
	flags.StringVar(&exceptions, "max-speed-exceptions", "", "Comma-separated exact selectors; overrides "+exceptionEnvironment)
	flags.StringVar(&opts.exposure.coreInclude, "metrics.core-include", defaultCoreInclude,
		"Metric names served on "+metricsPath+" (anchored regexp); overrides "+coreIncludeEnvironment)
	flags.StringVar(&opts.exposure.coreExclude, "metrics.core-exclude", "",
		"Metric names removed from "+metricsPath+" (anchored regexp, empty excludes nothing); overrides "+coreExcludeEnvironment)
	flags.StringVar(&opts.exposure.allPath, "metrics-all-path", defaultAllPath,
		"Path serving every metric (empty disables); overrides "+allPathEnvironment)
	flags.StringVar(&opts.exposure.allAddress, "metrics-all-addr", "",
		"Separate listen address for the all-metrics path (empty shares -metrics-addr); overrides "+allAddressEnvironment)
	backend := string(opts.monitor.IOBackend)
	flags.StringVar(&backend, "io-backend", backend, "Socket backend: poller or io_uring (requires available implementation)")
	flags.BoolVar(&opts.version, "version", false, "Print build identity and exit")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, fmt.Errorf("unexpected positional arguments: %q", flags.Args())
	}
	opts.monitor.IOBackend = linkmonitor.IOBackend(backend)
	// An explicit flag, even an empty one, overrides its environment variable.
	// An unset or empty variable keeps the flag default.
	explicit := make(map[string]bool)
	flags.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	for _, fallback := range []struct {
		flag, environment string
		target            *string
	}{
		{"max-speed-exceptions", exceptionEnvironment, &exceptions},
		{"metrics.core-include", coreIncludeEnvironment, &opts.exposure.coreInclude},
		{"metrics.core-exclude", coreExcludeEnvironment, &opts.exposure.coreExclude},
		{"metrics-all-path", allPathEnvironment, &opts.exposure.allPath},
		{"metrics-all-addr", allAddressEnvironment, &opts.exposure.allAddress},
	} {
		if value := getenv(fallback.environment); !explicit[fallback.flag] && value != "" {
			*fallback.target = value
		}
	}
	if exceptions != "" {
		opts.monitor.MaxSpeedExceptions = strings.Split(exceptions, ",")
	}
	return opts, nil
}

// coreMatcher validates the exposure options and compiles the core selection
// exactly once, before any listener exists.
func (o exposureOptions) coreMatcher(address string) (func(string) bool, error) {
	if err := o.validate(address); err != nil {
		return nil, err
	}
	return monitorprom.NameMatcher(o.coreInclude, o.coreExclude)
}

func (o exposureOptions) validate(address string) error {
	if o.allPath == "" {
		if o.allAddress != "" {
			return errors.New("metrics-all-addr requires a nonempty metrics-all-path")
		}
		return nil
	}
	// ServeMux patterns treat spaces, braces and trailing slashes specially;
	// accept only one exact path that does not collide with fixed routes.
	if !strings.HasPrefix(o.allPath, "/") || strings.HasSuffix(o.allPath, "/") || strings.ContainsAny(o.allPath, " \t{}") {
		return fmt.Errorf("metrics-all-path %q must be an exact absolute path without a trailing slash", o.allPath)
	}
	switch o.allPath {
	case "/healthz", "/readyz":
		return fmt.Errorf("metrics-all-path %q collides with a health endpoint", o.allPath)
	case metricsPath:
		if o.shared(address) {
			return fmt.Errorf("metrics-all-path %q collides with the core endpoint on the same listener", o.allPath)
		}
	}
	return nil
}

func (o exposureOptions) shared(address string) bool {
	return o.allAddress == "" || o.allAddress == address
}
